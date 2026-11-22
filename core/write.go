package core

import (
	"archive/tar"
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// CreateOptions control Create.
type CreateOptions struct {
	Format   Format // zip (default), tar, tar.gz, tar.xz, tar.zst
	Level    int    // 0 = store ... 9 = best; default 6
	Password string // ZIP and 7z: AES-256 encryption
	// EncryptNames also encrypts the file list (7z only), so names are hidden too.
	EncryptNames bool
	// VolumeSize > 0 splits the finished archive into parts of this many bytes
	// (name.ext.001, name.ext.002, ...).
	VolumeSize int64
	// SFXStub, when set, makes a self-extracting archive: the stub program is
	// written first and a ZIP archive follows it (ZIP only).
	SFXStub []byte
	// .nest only:
	RecoveryPercent int    // add a recovery record of this size (1-50 %)
	Lock            bool   // mark the archive as locked (no changes)
	Comment         string // archive comment
	BaseDir         string // folder inside the archive to add the files to
	Progress        ProgressFunc
}

type source struct {
	abs  string
	name string // path inside archive
	info fs.FileInfo
}

func collect(inputs []string, baseDir, exclude string) ([]source, int64, error) {
	baseDir = CleanPath(baseDir)
	var out []source
	var total int64
	excl, _ := filepath.Abs(exclude)
	for _, in := range inputs {
		abs, err := filepath.Abs(in)
		if err != nil {
			return nil, 0, err
		}
		root := filepath.Dir(abs)
		err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == excl || strings.HasPrefix(p, excl+".zipnest-tmp") {
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 || !(d.IsDir() || d.Type().IsRegular()) {
				return nil // skip links, devices, sockets
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(rel)
			if baseDir != "" {
				name = baseDir + "/" + name
			}
			out = append(out, source{abs: p, name: name, info: info})
			if !d.IsDir() {
				total += info.Size()
			}
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

func tempFor(out string) string { return out + ".zipnest-tmp" }

// Create builds a new archive from files and folders. For ZIP archives that
// already exist, the new files are added (replacing members with the same name).
func Create(out string, inputs []string, opt CreateOptions) error {
	if opt.Format == "" {
		opt.Format = FormatFromName(out)
		if opt.Format == "" {
			opt.Format = FormatZip
		}
	}
	if len(opt.SFXStub) > 0 {
		opt.Format = FormatZip
		if opt.VolumeSize > 0 {
			return errors.New("a self-extracting archive cannot be split into volumes")
		}
	}
	if opt.Level < 0 || opt.Level > 9 {
		opt.Level = 6
	}
	srcs, total, err := collect(inputs, opt.BaseDir, out)
	if err != nil {
		return err
	}
	if len(srcs) == 0 {
		return errors.New("nothing to add")
	}
	tmp := tempFor(out)
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	var werr error
	switch opt.Format {
	case FormatZip:
		werr = writeZip(f, out, srcs, total, opt)
	case Format7z:
		werr = writeSevenZip(f, out, srcs, total, opt)
	case FormatNest:
		werr = writeNest(f, out, srcs, total, opt)
	case FormatTar, FormatTarGz, FormatTarXz, FormatTarZst:
		if opt.Password != "" {
			werr = errors.New("password protection is available for ZIP and 7z archives")
		} else {
			werr = writeTar(f, srcs, total, opt)
		}
	default:
		werr = fmt.Errorf("cannot create %s archives", opt.Format)
	}
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	if err := os.Rename(tmp, out); err != nil {
		return err
	}
	if opt.VolumeSize > 0 {
		_, err := splitFile(out, opt.VolumeSize)
		return err
	}
	return nil
}

func zipHeader(name string, info fs.FileInfo) *zip.FileHeader {
	fh := &zip.FileHeader{Name: name, Modified: info.ModTime()}
	fh.SetMode(info.Mode())
	if !utf8.ValidString(name) {
		return fh
	}
	for i := 0; i < len(name); i++ {
		if name[i] >= 0x80 {
			fh.Flags |= 0x800
			break
		}
	}
	return fh
}

// setLegacyTime fills the MS-DOS date/time fields (CreateRaw does not do this).
func setLegacyTime(fh *zip.FileHeader) {
	t := fh.Modified
	if t.Year() < 1980 {
		t = time.Date(1980, 1, 1, 0, 0, 0, 0, time.Local)
	}
	fh.ModifiedDate = uint16(t.Day() + int(t.Month())<<5 + (t.Year()-1980)<<9)
	fh.ModifiedTime = uint16(t.Second()/2 + t.Minute()<<5 + t.Hour()<<11)
}

func writeZip(f *os.File, out string, srcs []source, total int64, opt CreateOptions) error {
	if len(opt.SFXStub) > 0 {
		if _, err := f.Write(opt.SFXStub); err != nil {
			return err
		}
	}
	zw := zip.NewWriter(f)
	if len(opt.SFXStub) > 0 {
		zw.SetOffset(int64(len(opt.SFXStub))) // offsets relative to the start of the .exe
	}
	level := opt.Level
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, level)
	})
	method := uint16(zip.Deflate)
	if level == 0 {
		method = zip.Store
	}

	// Copy existing members (update mode).
	newNames := map[string]bool{}
	for _, s := range srcs {
		n := s.name
		if s.info.IsDir() {
			n += "/"
		}
		newNames[n] = true
	}
	if st, err := os.Stat(out); err == nil && st.Size() > 0 && len(opt.SFXStub) == 0 {
		old, err := openZip(out)
		if err != nil {
			return fmt.Errorf("cannot update %s: %w", filepath.Base(out), err)
		}
		for _, zf := range old.File {
			if newNames[zf.Name] {
				continue
			}
			if err := zw.Copy(zf); err != nil {
				old.Close()
				return err
			}
		}
		old.Close()
	}

	if err := writeZipEntries(zw, out, srcs, total, opt, method, level); err != nil {
		return err
	}
	return zw.Close()
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func compressStream(f Format, w io.Writer, level int) (io.WriteCloser, error) {
	switch f {
	case FormatTarGz:
		return gzip.NewWriterLevel(w, level)
	case FormatTarXz:
		cfg := xz.WriterConfig{DictCap: 1 << (20 + min(level, 6))} // up to 64 MiB
		return cfg.NewWriter(w)
	case FormatTarZst:
		zl := zstd.SpeedDefault
		switch {
		case level <= 2:
			zl = zstd.SpeedFastest
		case level >= 8:
			zl = zstd.SpeedBestCompression
		case level >= 6:
			zl = zstd.SpeedBetterCompression
		}
		return zstd.NewWriter(w, zstd.WithEncoderLevel(zl))
	}
	return nopWriteCloser{w}, nil
}

func writeTar(f *os.File, srcs []source, total int64, opt CreateOptions) error {
	cw, err := compressStream(opt.Format, f, opt.Level)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(cw)
	var done int64
	buf := make([]byte, 256<<10)
	for _, s := range srcs {
		h, err := tar.FileInfoHeader(s.info, "")
		if err != nil {
			return err
		}
		h.Name = s.name
		if s.info.IsDir() {
			h.Name += "/"
		}
		h.Uname, h.Gname, h.Uid, h.Gid = "", "", 0, 0
		h.Format = tar.FormatPAX
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if s.info.IsDir() {
			continue
		}
		src, err := os.Open(s.abs)
		if err != nil {
			return err
		}
		_, err = io.CopyBuffer(&counter{w: tw, name: s.name, done: &done, total: total, progress: opt.Progress}, src, buf)
		src.Close()
		if err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return cw.Close()
}

// Delete removes members (files or whole folders) from a ZIP or TAR archive.
func Delete(p string, paths []string) (int, error) {
	f, err := Detect(p)
	if err != nil {
		return 0, err
	}
	if len(paths) == 0 {
		return 0, nil
	}
	tmp := tempFor(p)
	out, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	removed := 0
	switch f {
	case FormatZip:
		removed, err = deleteZip(p, out, paths)
	case FormatTar, FormatTarGz, FormatTarBz2, FormatTarXz, FormatTarZst:
		removed, err = deleteTar(p, f, out, paths)
	default:
		err = ErrReadOnlyFormat
	}
	cerr := out.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return 0, err
	}
	return removed, os.Rename(tmp, p)
}

func deleteZip(p string, out *os.File, paths []string) (int, error) {
	zr, err := openZip(p)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	zw := zip.NewWriter(out)
	removed := 0
	for _, zf := range zr.File {
		name := zf.Name
		if zf.NonUTF8 || zf.Flags&0x800 == 0 {
			name = decodeName(name)
		}
		if c := CleanPath(name); c != "" && selected(c, paths) {
			removed++
			continue
		}
		if err := zw.Copy(zf); err != nil {
			return 0, err
		}
	}
	return removed, zw.Close()
}

func deleteTar(p string, f Format, out *os.File, paths []string) (int, error) {
	in, err := os.Open(p)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	rs, err := decompressStream(f, in)
	if err != nil {
		return 0, err
	}
	defer rs.Close()
	wf := f
	if f == FormatTarBz2 { // no bzip2 writer in Go: rewrite as plain tar would change the format
		return 0, errors.New("deleting from .tar.bz2 is not supported")
	}
	cw, err := compressStream(wf, out, 6)
	if err != nil {
		return 0, err
	}
	tr := tar.NewReader(rs)
	tw := tar.NewWriter(cw)
	removed := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		if c := CleanPath(h.Name); c != "" && selected(c, paths) {
			removed++
			continue
		}
		if err := tw.WriteHeader(h); err != nil {
			return 0, err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return 0, err
		}
	}
	if err := tw.Close(); err != nil {
		return 0, err
	}
	return removed, cw.Close()
}

// SuggestArchiveName proposes "<dir>/<name>.<ext>" for a set of inputs.
func SuggestArchiveName(inputs []string, f Format) string {
	if f == "" {
		f = FormatZip
	}
	if len(inputs) == 0 {
		return "archive." + string(f)
	}
	first := filepath.Clean(inputs[0])
	dir := filepath.Dir(first)
	name := filepath.Base(first)
	if len(inputs) > 1 {
		name = filepath.Base(dir)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = "archive"
		}
	} else if st, err := os.Stat(first); err == nil && !st.IsDir() {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	return filepath.Join(dir, name+"."+string(f))
}
