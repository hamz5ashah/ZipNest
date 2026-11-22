package core

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bodgit/sevenzip"
	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/nwaples/rardecode/v2"
	"github.com/ulikunitz/xz"
)

func zstdReader(r io.Reader) io.ReadCloser {
	d, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(1<<31))
	if err != nil {
		return readCloser{Reader: errReader{err}}
	}
	return readCloser{Reader: d, close: func() error { d.Close(); return nil }}
}

func xzReader(r io.Reader) io.ReadCloser {
	x, err := xz.NewReader(r)
	if err != nil {
		return readCloser{Reader: errReader{err}}
	}
	return io.NopCloser(x)
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func decompressorFor(_ *zip.Reader, method uint16) func(io.Reader) io.ReadCloser {
	switch method {
	case zip.Store:
		return io.NopCloser
	case zip.Deflate:
		return flate.NewReader
	case 12:
		return func(r io.Reader) io.ReadCloser { return io.NopCloser(bzip2.NewReader(r)) }
	case 93:
		return zstdReader
	case 95:
		return xzReader
	}
	return nil
}

// zipArchive is an open ZIP: a normal file, a split set or a self-extracting .exe.
type zipArchive struct {
	*zip.Reader
	c io.Closer
}

func (z *zipArchive) Close() error { return z.c.Close() }

func openZip(p string) (*zipArchive, error) {
	f, size, err := openArchiveFile(p)
	if err != nil {
		return nil, err
	}
	r, err := zip.NewReader(f, size)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		f.Close()
		return nil, err
	}
	zr := &zipArchive{Reader: r, c: f}
	zr.RegisterDecompressor(zip.Deflate, flate.NewReader)
	zr.RegisterDecompressor(12, func(r io.Reader) io.ReadCloser { return io.NopCloser(bzip2.NewReader(r)) })
	zr.RegisterDecompressor(93, zstdReader)
	zr.RegisterDecompressor(95, xzReader)
	return zr, nil
}

// visitFunc is called for each entry; open returns the entry data and may
// only be used during the call.
type visitFunc func(e Entry, open func() (io.ReadCloser, error)) error

func millis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// walk iterates over the archive's members in storage order.
func walk(p string, password string, fn visitFunc) (Format, error) {
	f, err := Detect(p)
	if err != nil {
		return "", err
	}
	switch f {
	case FormatZip:
		return f, walkZip(p, password, fn)
	case Format7z:
		return f, walk7z(p, password, fn)
	case FormatNest:
		return f, walkNest(p, password, fn)
	case FormatRar:
		return f, walkRar(p, password, fn)
	case FormatGz, FormatBz2, FormatXz, FormatZst:
		return f, walkSingle(p, f, fn)
	default:
		return f, walkTar(p, f, fn)
	}
}

func walkZip(p, password string, fn visitFunc) error {
	zr, err := openZip(p)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := f.Name
		if f.NonUTF8 || f.Flags&0x800 == 0 {
			name = decodeName(name)
		}
		clean := CleanPath(name)
		if clean == "" {
			continue
		}
		e := Entry{
			Path:      clean,
			Dir:       strings.HasSuffix(f.Name, "/") || f.Mode().IsDir(),
			Size:      int64(f.UncompressedSize64),
			Packed:    int64(f.CompressedSize64),
			Modified:  millis(f.Modified),
			Encrypted: f.Flags&0x1 != 0,
			Link:      f.Mode()&os.ModeSymlink != 0,
		}
		zf := f
		if err := fn(e, func() (io.ReadCloser, error) { return openZipEntry(zr.Reader, zf, password) }); err != nil {
			return err
		}
	}
	return nil
}

func map7zErr(err error, password string) error {
	var re *sevenzip.ReadError
	if errors.As(err, &re) && re.Encrypted {
		if password == "" {
			return ErrPasswordRequired
		}
		return ErrBadPassword
	}
	return err
}

func walk7z(p, password string, fn visitFunc) error {
	r, err := sevenzip.OpenReaderWithPassword(p, password)
	if err != nil {
		return map7zErr(err, password)
	}
	defer r.Close()
	for _, f := range r.File {
		clean := CleanPath(f.Name)
		if clean == "" {
			continue
		}
		info := f.FileInfo()
		e := Entry{
			Path:     clean,
			Dir:      info.IsDir(),
			Size:     int64(f.UncompressedSize),
			Packed:   -1,
			Modified: millis(f.Modified),
			Link:     info.Mode()&os.ModeSymlink != 0,
		}
		sf := f
		err := fn(e, func() (io.ReadCloser, error) {
			rc, err := sf.Open()
			if err != nil {
				return nil, map7zErr(err, password)
			}
			return readCloser{Reader: mapErrReader{rc, password}, close: rc.Close}, nil
		})
		if err != nil {
			return map7zErr(err, password)
		}
	}
	return nil
}

type mapErrReader struct {
	r        io.Reader
	password string
}

func (m mapErrReader) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	if err != nil && err != io.EOF {
		err = map7zErr(err, m.password)
	}
	return n, err
}

func mapRarErr(err error, password string) error {
	switch {
	case errors.Is(err, rardecode.ErrArchiveEncrypted), errors.Is(err, rardecode.ErrArchivedFileEncrypted):
		return ErrPasswordRequired
	case errors.Is(err, rardecode.ErrBadPassword):
		return ErrBadPassword
	case errors.Is(err, rardecode.ErrBadFileChecksum):
		if password != "" {
			return ErrBadPassword
		}
		return ErrChecksum
	}
	return err
}

func walkRar(p, password string, fn visitFunc) error {
	var opts []rardecode.Option
	if password != "" {
		opts = append(opts, rardecode.Password(password))
	}
	r, err := rardecode.OpenReader(p, opts...)
	if err != nil {
		return mapRarErr(err, password)
	}
	defer r.Close()
	for {
		h, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return mapRarErr(err, password)
		}
		clean := CleanPath(h.Name)
		if clean == "" {
			continue
		}
		size := h.UnPackedSize
		if h.UnKnownSize {
			size = -1
		}
		e := Entry{
			Path:      clean,
			Dir:       h.IsDir,
			Size:      size,
			Packed:    h.PackedSize,
			Modified:  millis(h.ModificationTime),
			Encrypted: h.Encrypted,
			Link:      h.LinkType != 0,
		}
		err = fn(e, func() (io.ReadCloser, error) {
			if h.Encrypted && password == "" {
				return nil, ErrPasswordRequired
			}
			return readCloser{Reader: rarErrReader{r, password}}, nil
		})
		if err != nil {
			return mapRarErr(err, password)
		}
	}
}

type rarErrReader struct {
	r        io.Reader
	password string
}

func (m rarErrReader) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	if err != nil && err != io.EOF {
		err = mapRarErr(err, m.password)
	}
	return n, err
}

func decompressStream(f Format, r io.Reader) (io.ReadCloser, error) {
	switch f {
	case FormatTarGz, FormatGz:
		return gzip.NewReader(r)
	case FormatTarBz2, FormatBz2:
		return io.NopCloser(bzip2.NewReader(r)), nil
	case FormatTarXz, FormatXz:
		x, err := xz.NewReader(r)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(x), nil
	case FormatTarZst, FormatZst:
		rc := zstdReader(r)
		return rc, nil
	}
	return io.NopCloser(r), nil
}

func walkTar(p string, f Format, fn visitFunc) error {
	file, _, err := openArchiveFile(p)
	if err != nil {
		return err
	}
	defer file.Close()
	stream, err := decompressStream(f, file)
	if err != nil {
		return err
	}
	defer stream.Close()
	tr := tar.NewReader(stream)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		clean := CleanPath(h.Name)
		if clean == "" {
			continue
		}
		switch h.Typeflag {
		case tar.TypeReg, tar.TypeRegA, tar.TypeDir, tar.TypeSymlink, tar.TypeLink, tar.TypeGNUSparse:
		default:
			continue // devices, fifos, pax globals...
		}
		e := Entry{
			Path:     clean,
			Dir:      h.Typeflag == tar.TypeDir,
			Size:     h.Size,
			Packed:   -1,
			Modified: millis(h.ModTime),
			Link:     h.Typeflag == tar.TypeSymlink || h.Typeflag == tar.TypeLink,
		}
		if err := fn(e, func() (io.ReadCloser, error) { return io.NopCloser(tr), nil }); err != nil {
			return err
		}
	}
}

func walkSingle(p string, f Format, fn visitFunc) error {
	file, _, err := openArchiveFile(p)
	if err != nil {
		return err
	}
	defer file.Close()
	st, _ := os.Stat(p)
	name := archiveStem(p)
	var mtime int64
	if st != nil {
		mtime = millis(st.ModTime())
	}
	if f == FormatGz {
		if gz, err := gzip.NewReader(file); err == nil {
			if gz.Name != "" {
				if n := CleanPath(filepath.Base(gz.Name)); n != "" {
					name = n
				}
			}
			if !gz.ModTime.IsZero() {
				mtime = millis(gz.ModTime)
			}
			gz.Close()
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	var packed int64 = -1
	if st != nil {
		packed = st.Size()
	}
	e := Entry{Path: name, Size: -1, Packed: packed, Modified: mtime}
	return fn(e, func() (io.ReadCloser, error) { return decompressStream(f, file) })
}

// List returns all entries of an archive.
func List(p, password string) ([]Entry, Format, error) {
	var out []Entry
	f, err := walk(p, password, func(e Entry, _ func() (io.ReadCloser, error)) error {
		out = append(out, e)
		return nil
	})
	return out, f, err
}

// ExtractOptions control Extract.
type ExtractOptions struct {
	Password  string
	Select    []string // entry paths (files or folders) to extract; empty = all
	BaseDir   string   // archive folder the selection is relative to (stripped from output paths)
	Overwrite string   // "overwrite" (default), "skip" or "rename"
	Progress  ProgressFunc
}

// ExtractResult summarises an extraction.
type ExtractResult struct {
	Files   int      `json:"files"`
	Dirs    int      `json:"dirs"`
	Bytes   int64    `json:"bytes"`
	Skipped []string `json:"skipped,omitempty"`
	Outputs []string `json:"outputs,omitempty"` // top-level created paths
}

func uniqueName(p string) string {
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 1; ; i++ {
		c := base + " (" + strconv.Itoa(i) + ")" + ext
		if _, err := os.Lstat(c); os.IsNotExist(err) {
			return c
		}
	}
}

// Extract unpacks (part of) an archive into dest.
func Extract(p, dest string, opt ExtractOptions) (*ExtractResult, error) {
	if strings.TrimSpace(dest) == "" {
		return nil, errors.New("no destination folder given")
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(destAbs, 0o755); err != nil {
		return nil, err
	}
	base := strings.Trim(opt.BaseDir, "/")
	var total int64
	if fmt, _ := Detect(p); fmt == FormatZip || fmt == Format7z || fmt == FormatRar {
		if all, _, err := List(p, opt.Password); err == nil {
			for _, e := range all {
				if !e.Dir && e.Size > 0 && selected(e.Path, opt.Select) {
					total += e.Size
				}
			}
		}
	}
	res := &ExtractResult{}
	tops := map[string]bool{}
	var done int64
	type dirTime struct {
		path string
		t    int64
	}
	var dirTimes []dirTime
	buf := make([]byte, 256<<10)

	_, err = walk(p, opt.Password, func(e Entry, open func() (io.ReadCloser, error)) error {
		if !selected(e.Path, opt.Select) {
			return nil
		}
		rel := e.Path
		if base != "" {
			if !strings.HasPrefix(rel, base+"/") {
				return nil
			}
			rel = rel[len(base)+1:]
		}
		if e.Link {
			res.Skipped = append(res.Skipped, e.Path+" (link)")
			return nil
		}
		target := filepath.Join(destAbs, filepath.FromSlash(rel))
		if r, err := filepath.Rel(destAbs, target); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			res.Skipped = append(res.Skipped, e.Path+" (unsafe path)")
			return nil
		}
		top := strings.SplitN(rel, "/", 2)[0]
		if e.Dir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			res.Dirs++
			if e.Modified != 0 {
				dirTimes = append(dirTimes, dirTime{target, e.Modified})
			}
			tops[filepath.Join(destAbs, top)] = true
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if _, err := os.Lstat(target); err == nil {
			switch opt.Overwrite {
			case "skip":
				res.Skipped = append(res.Skipped, e.Path+" (exists)")
				return nil
			case "rename":
				target = uniqueName(target)
			default:
				if st, err := os.Lstat(target); err == nil && st.IsDir() {
					res.Skipped = append(res.Skipped, e.Path+" (a folder with this name exists)")
					return nil
				}
				os.Remove(target)
			}
		}
		rc, err := open()
		if err != nil {
			return fmt.Errorf("%s: %w", e.Path, err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		cw := &counter{w: out, name: e.Path, done: &done, total: total, progress: opt.Progress}
		n, err := io.CopyBuffer(cw, rc, buf)
		rc.Close()
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(target)
			if errors.Is(err, ErrCancelled) || errors.Is(err, ErrPasswordRequired) || errors.Is(err, ErrBadPassword) {
				return err
			}
			return fmt.Errorf("%s: %w", e.Path, err)
		}
		if e.Modified != 0 {
			t := time.UnixMilli(e.Modified)
			os.Chtimes(target, t, t)
		}
		res.Files++
		res.Bytes += n
		if strings.Contains(rel, "/") {
			tops[filepath.Join(destAbs, top)] = true
		} else {
			tops[target] = true
		}
		return nil
	})
	for i := len(dirTimes) - 1; i >= 0; i-- {
		t := time.UnixMilli(dirTimes[i].t)
		os.Chtimes(dirTimes[i].path, t, t)
	}
	for t := range tops {
		res.Outputs = append(res.Outputs, t)
	}
	return res, err
}

// TestResult summarises an integrity test.
type TestResult struct {
	Files  int      `json:"files"`
	Bytes  int64    `json:"bytes"`
	Errors []string `json:"errors,omitempty"`
}

// Test decompresses every member and verifies checksums without writing anything.
func Test(p, password string, progress ProgressFunc) (*TestResult, error) {
	res := &TestResult{}
	var total, done int64
	if all, _, err := List(p, password); err == nil {
		for _, e := range all {
			if !e.Dir && e.Size > 0 {
				total += e.Size
			}
		}
	}
	buf := make([]byte, 256<<10)
	_, err := walk(p, password, func(e Entry, open func() (io.ReadCloser, error)) error {
		if e.Dir || e.Link {
			return nil
		}
		rc, err := open()
		if err != nil {
			if errors.Is(err, ErrPasswordRequired) || errors.Is(err, ErrBadPassword) {
				return err
			}
			res.Errors = append(res.Errors, e.Path+": "+err.Error())
			return nil
		}
		cw := &counter{w: io.Discard, name: e.Path, done: &done, total: total, progress: progress}
		n, err := io.CopyBuffer(cw, rc, buf)
		rc.Close()
		res.Bytes += n
		if err != nil {
			if errors.Is(err, ErrCancelled) || errors.Is(err, ErrBadPassword) || errors.Is(err, ErrPasswordRequired) {
				return err
			}
			res.Errors = append(res.Errors, e.Path+": "+err.Error())
			return nil
		}
		res.Files++
		return nil
	})
	return res, err
}
