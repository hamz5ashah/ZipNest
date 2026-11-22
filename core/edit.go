package core

// Editing archives in place: adding, deleting and renaming members of every
// format ZipNest can write. ZIP and TAR members are copied without being
// recompressed; 7z and .nest archives (solid, possibly encrypted) are rebuilt
// through a temporary folder next to the archive, keeping their password.

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ArchiveInfo summarises an archive for the information panel.
type ArchiveInfo struct {
	Path            string `json:"path"`
	Format          Format `json:"format"`
	Size            int64  `json:"size"` // bytes on disk (all volumes)
	Volumes         int    `json:"volumes"`
	Files           int    `json:"files"`
	Dirs            int    `json:"dirs"`
	Unpacked        int64  `json:"unpacked"`
	Packed          int64  `json:"packed"` // -1 if unknown
	Encrypted       bool   `json:"encrypted"`
	HiddenNames     bool   `json:"hiddenNames"`
	Locked          bool   `json:"locked"`
	RecoveryPercent int    `json:"recoveryPercent"`
	Comment         string `json:"comment"`
	SFX             bool   `json:"sfx"`
	Writable        bool   `json:"writable"`
	Modified        int64  `json:"mtime"`
}

// Writable reports whether ZipNest can add to / delete from this format.
func Writable(f Format) bool {
	for _, w := range WritableFormats {
		if w == f {
			return true
		}
	}
	return false
}

// Info describes an archive. ents may be the result of List (nil = list now).
func Info(p, password string, ents []Entry) (*ArchiveInfo, error) {
	f, err := Detect(p)
	if err != nil {
		return nil, err
	}
	if ents == nil {
		if ents, _, err = List(p, password); err != nil && !errors.Is(err, ErrPasswordRequired) {
			return nil, err
		}
	}
	in := &ArchiveInfo{Path: p, Format: f, Packed: 0}
	parts := volumeParts(p)
	if len(parts) == 0 {
		parts = []string{p}
	}
	in.Volumes = len(parts)
	for _, v := range parts {
		if st, err := os.Stat(v); err == nil {
			in.Size += st.Size()
			if m := st.ModTime().UnixMilli(); m > in.Modified {
				in.Modified = m
			}
		}
	}
	for _, e := range ents {
		if e.Dir {
			in.Dirs++
			continue
		}
		in.Files++
		if e.Size > 0 {
			in.Unpacked += e.Size
		}
		if e.Packed >= 0 && in.Packed >= 0 {
			in.Packed += e.Packed
		} else {
			in.Packed = -1
		}
		in.Encrypted = in.Encrypted || e.Encrypted
	}
	switch f {
	case FormatNest:
		if d, err := NestInfo(p, password); err == nil {
			in.Encrypted, in.HiddenNames, in.Locked = d.Encrypted, d.HiddenNames, d.Locked
			in.RecoveryPercent, in.Comment = d.RecoveryPercent, d.Comment
		}
	case Format7z:
		in.Encrypted = sevenZipEncrypted(p)
		in.HiddenNames = len(ents) == 0 && in.Encrypted
	case FormatZip:
		if zr, err := openZip(p); err == nil {
			in.Comment = zr.Comment
			zr.Close()
		}
		head := make([]byte, 2)
		if fh, err := os.Open(p); err == nil {
			io.ReadFull(fh, head)
			fh.Close()
			in.SFX = string(head) == "MZ"
		}
	}
	in.Writable = Writable(f) && in.Volumes == 1 && !in.Locked && !in.SFX
	return in, nil
}

var errFound = errors.New("found")

// sevenZipEncrypted probes whether the file data of a 7z archive needs a password.
func sevenZipEncrypted(p string) bool {
	ents, _, err := List(p, "")
	if err != nil {
		return errors.Is(err, ErrPasswordRequired)
	}
	var small *Entry
	for i, e := range ents {
		if !e.Dir && e.Size > 0 && (small == nil || e.Size < small.Size) {
			small = &ents[i]
		}
	}
	if small == nil {
		return false
	}
	// Read the whole (smallest) file so that the checksum is verified.
	_, _, err = ReadEntry(p, "", small.Path, small.Size+1)
	return errors.Is(err, ErrPasswordRequired) || errors.Is(err, ErrBadPassword) || errors.Is(err, ErrChecksum)
}

// ReadEntry returns up to limit bytes of one file inside an archive (for
// previews) and its full size.
func ReadEntry(p, password, name string, limit int64) ([]byte, int64, error) {
	var data []byte
	var size int64 = -1
	_, err := walk(p, password, func(e Entry, open func() (io.ReadCloser, error)) error {
		if e.Dir || e.Path != name {
			return nil
		}
		size = e.Size
		rc, err := open()
		if err != nil {
			return err
		}
		defer rc.Close()
		data, err = io.ReadAll(io.LimitReader(rc, limit))
		if err != nil {
			return err
		}
		return errFound
	})
	if err == errFound {
		return data, size, nil
	}
	if err == nil {
		err = fmt.Errorf("%s: not found in the archive", name)
	}
	return nil, 0, err
}

// EditOptions control Update, Delete and Rename on formats that are rebuilt.
type EditOptions struct {
	Password string // password of the archive (kept for the rebuilt archive)
	Level    int    // compression level for rebuilt / new data (default 6)
	Progress ProgressFunc
}

func checkEditable(p, password string) (Format, error) {
	f, err := Detect(p)
	if err != nil {
		return "", err
	}
	if !Writable(f) {
		return f, ErrReadOnlyFormat
	}
	if len(volumeParts(p)) > 1 {
		return f, errors.New("split (multi-volume) archives cannot be modified; convert it first")
	}
	if f == FormatNest {
		if d, err := NestInfo(p, password); err == nil && d.Locked {
			return f, ErrLocked
		}
	}
	return f, nil
}

// rebuildOptions recreates the options an existing archive was made with.
func rebuildOptions(p string, f Format, opt EditOptions) CreateOptions {
	co := CreateOptions{Format: f, Level: opt.Level, Password: opt.Password, Progress: opt.Progress}
	if co.Level <= 0 || co.Level > 9 {
		co.Level = 6
	}
	switch f {
	case FormatNest:
		if d, err := NestInfo(p, opt.Password); err == nil {
			co.EncryptNames = d.HiddenNames
			co.RecoveryPercent = d.RecoveryPercent
			co.Comment = d.Comment
			if !d.Encrypted {
				co.Password = ""
			}
		}
	case Format7z:
		if _, _, err := List(p, ""); errors.Is(err, ErrPasswordRequired) {
			co.EncryptNames = true
		} else if !sevenZipEncrypted(p) {
			co.Password = ""
		}
	}
	return co
}

// rebuild extracts the archive (minus dropped paths) into a temporary folder,
// lets edit change that folder and packs it again with the same settings.
func rebuild(p string, f Format, opt EditOptions, drop []string, edit func(tmp string) error) error {
	abs, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(abs), ".zipnest-edit-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	co := rebuildOptions(abs, f, opt)
	if _, err := Extract(abs, tmp, ExtractOptions{Password: opt.Password, Progress: opt.Progress}); err != nil {
		return err
	}
	for _, d := range drop {
		if d = CleanPath(d); d != "" {
			os.RemoveAll(filepath.Join(tmp, filepath.FromSlash(d)))
		}
	}
	if edit != nil {
		if err := edit(tmp); err != nil {
			return err
		}
	}
	items, err := os.ReadDir(tmp)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("the archive would be empty")
	}
	var inputs []string
	for _, it := range items {
		inputs = append(inputs, filepath.Join(tmp, it.Name()))
	}
	out := abs + ".zipnest-new" + filepath.Ext(abs)
	co.Format = f
	if err := Create(out, inputs, co); err != nil {
		os.Remove(out)
		return err
	}
	return os.Rename(out, abs)
}

// copyInto copies files/folders into dir (used when adding to rebuilt archives).
func copyInto(dir string, inputs []string, exclude []string) error {
	for _, in := range inputs {
		in, err := filepath.Abs(in)
		if err != nil {
			return err
		}
		root := filepath.Dir(in)
		err = filepath.Walk(in, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			if len(exclude) > 0 && excluded(filepath.ToSlash(rel), exclude) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			dst := filepath.Join(dir, rel)
			if info.IsDir() {
				return os.MkdirAll(dst, 0o755)
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			if st, err := os.Stat(dst); err == nil && st.IsDir() {
				os.RemoveAll(dst)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := copyFile(p, dst); err != nil {
				return err
			}
			return os.Chtimes(dst, info.ModTime(), info.ModTime())
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Update adds files and folders to an existing archive of any writable format.
// baseDir is the folder inside the archive they go into. Members with the same
// name are replaced.
func Update(p string, inputs []string, baseDir string, exclude []string, opt EditOptions) error {
	f, err := checkEditable(p, opt.Password)
	if err != nil {
		return err
	}
	baseDir = CleanPath(baseDir)
	if f == FormatZip {
		lvl := opt.Level
		if lvl <= 0 {
			lvl = 6
		}
		pw := opt.Password
		if pw != "" { // keep plain archives plain
			if ents, _, err := List(p, ""); err == nil {
				enc := false
				for _, e := range ents {
					enc = enc || e.Encrypted
				}
				if !enc && len(ents) > 0 {
					pw = ""
				}
			}
		}
		co := CreateOptions{Format: FormatZip, Level: lvl, Password: pw, BaseDir: baseDir, Exclude: exclude, Progress: opt.Progress}
		if zr, err := openZip(p); err == nil {
			co.Comment = zr.Comment
			zr.Close()
		}
		return Create(p, inputs, co)
	}
	return rebuild(p, f, opt, nil, func(tmp string) error {
		dir := tmp
		if baseDir != "" {
			dir = filepath.Join(tmp, filepath.FromSlash(baseDir))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		return copyInto(dir, inputs, exclude)
	})
}

// DeleteFrom removes members from any writable archive (ZIP/TAR in place,
// 7z/.nest by rebuilding).
func DeleteFrom(p string, paths []string, opt EditOptions) (int, error) {
	f, err := checkEditable(p, opt.Password)
	if err != nil {
		return 0, err
	}
	if f != Format7z && f != FormatNest {
		return Delete(p, paths)
	}
	ents, _, err := List(p, opt.Password)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range ents {
		if selected(e.Path, paths) {
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, rebuild(p, f, opt, paths, nil)
}

// Rename gives a member (file or folder) a new name in the same folder.
func Rename(p, oldPath, newName string, opt EditOptions) error {
	oldPath = CleanPath(oldPath)
	newName = strings.TrimSpace(newName)
	if oldPath == "" || newName == "" || strings.ContainsAny(newName, `/\:*?"<>|`) || newName == "." || newName == ".." {
		return errors.New("invalid name")
	}
	newPath := newName
	if d := path.Dir(oldPath); d != "." {
		newPath = d + "/" + newName
	}
	if newPath == oldPath {
		return nil
	}
	f, err := checkEditable(p, opt.Password)
	if err != nil {
		return err
	}
	ents, _, err := List(p, opt.Password)
	if err != nil {
		return err
	}
	found := false
	for _, e := range ents {
		if e.Path == newPath || strings.HasPrefix(e.Path, newPath+"/") {
			return fmt.Errorf("%q already exists", newName)
		}
		found = found || e.Path == oldPath || strings.HasPrefix(e.Path, oldPath+"/")
	}
	if !found {
		return fmt.Errorf("%s: not found in the archive", oldPath)
	}
	if f == FormatZip {
		return renameZip(p, oldPath, newPath)
	}
	return rebuild(p, f, opt, nil, func(tmp string) error {
		return os.Rename(filepath.Join(tmp, filepath.FromSlash(oldPath)), filepath.Join(tmp, filepath.FromSlash(newPath)))
	})
}

func renameZip(p, oldPath, newPath string) error {
	zr, err := openZip(p)
	if err != nil {
		return err
	}
	tmp := tempFor(p)
	out, err := os.Create(tmp)
	if err != nil {
		zr.Close()
		return err
	}
	zw := zip.NewWriter(out)
	zw.SetComment(zr.Comment)
	werr := func() error {
		for _, zf := range zr.File {
			name := zf.Name
			if zf.NonUTF8 || zf.Flags&0x800 == 0 {
				name = decodeName(name)
			}
			c := CleanPath(name)
			if c != oldPath && !strings.HasPrefix(c, oldPath+"/") {
				if err := zw.Copy(zf); err != nil {
					return err
				}
				continue
			}
			fh := zf.FileHeader
			fh.Name = newPath + strings.TrimPrefix(c, oldPath)
			if strings.HasSuffix(name, "/") {
				fh.Name += "/"
			}
			fh.Flags |= 0x800 // UTF-8 name
			fh.NonUTF8 = false
			raw, err := zf.OpenRaw()
			if err != nil {
				return err
			}
			w, err := zw.CreateRaw(&fh)
			if err != nil {
				return err
			}
			if _, err := io.Copy(w, raw); err != nil {
				return err
			}
		}
		return zw.Close()
	}()
	zr.Close()
	if cerr := out.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	return os.Rename(tmp, p)
}

// SetComment changes the comment of a ZIP or .nest archive.
func SetComment(p, comment string, opt EditOptions) error {
	f, err := checkEditable(p, opt.Password)
	if err != nil {
		return err
	}
	switch f {
	case FormatZip:
		zr, err := openZip(p)
		if err != nil {
			return err
		}
		tmp := tempFor(p)
		out, err := os.Create(tmp)
		if err != nil {
			zr.Close()
			return err
		}
		zw := zip.NewWriter(out)
		zw.SetComment(comment)
		var werr error
		for _, zf := range zr.File {
			if werr = zw.Copy(zf); werr != nil {
				break
			}
		}
		if werr == nil {
			werr = zw.Close()
		}
		zr.Close()
		if cerr := out.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(tmp)
			return werr
		}
		return os.Rename(tmp, p)
	case FormatNest:
		abs, _ := filepath.Abs(p)
		tmp, err := os.MkdirTemp(filepath.Dir(abs), ".zipnest-edit-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		co := rebuildOptions(abs, f, opt)
		co.Comment = comment
		if _, err := Extract(abs, tmp, ExtractOptions{Password: opt.Password, Progress: opt.Progress}); err != nil {
			return err
		}
		items, _ := os.ReadDir(tmp)
		var inputs []string
		for _, it := range items {
			inputs = append(inputs, filepath.Join(tmp, it.Name()))
		}
		out := abs + ".zipnest-new.nest"
		if err := Create(out, inputs, co); err != nil {
			os.Remove(out)
			return err
		}
		return os.Rename(out, abs)
	}
	return errors.New("comments are supported for ZIP and .nest archives")
}

// BenchmarkResult is the outcome of one benchmark method.
type BenchmarkResult struct {
	Method   string  `json:"method"`
	Compress float64 `json:"compress"`   // MB/s
	Decomp   float64 `json:"decompress"` // MB/s
	Ratio    float64 `json:"ratio"`      // packed / unpacked
}

// benchData makes repeatable, moderately compressible data (text-like).
func benchData(n int) []byte {
	words := strings.Fields("the quick brown fox jumps over lazy dog archive zipnest compression data block stream " +
		"file folder window android desktop speed ratio level solid entry header index random value 0123456789")
	b := make([]byte, 0, n+32)
	x := uint32(2463534242)
	for len(b) < n {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		if x%11 == 0 {
			b = append(b, byte(x>>8), byte(x>>16))
		}
		b = append(b, words[x%uint32(len(words))]...)
		b = append(b, ' ')
		if x%17 == 0 {
			b = append(b, '\n')
		}
	}
	return b[:n]
}

// Benchmark measures compression speed of ZipNest's formats on dir (a scratch
// folder). It returns results for .nest, ZIP and 7z; progress reports 0..total.
func Benchmark(dir string, progress ProgressFunc) ([]BenchmarkResult, error) {
	tmp, err := os.MkdirTemp(dir, "zipnest-bench-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	const size = 32 << 20
	src := filepath.Join(tmp, "data.bin")
	if err := os.WriteFile(src, benchData(size), 0o644); err != nil {
		return nil, err
	}
	methods := []struct {
		name string
		f    Format
	}{{"NEST (Zstandard)", FormatNest}, {"ZIP (Deflate)", FormatZip}, {"7Z (LZMA2)", Format7z}}
	var res []BenchmarkResult
	for i, m := range methods {
		if progress != nil && !progress(m.name, int64(i), int64(len(methods))) {
			return nil, ErrCancelled
		}
		out := filepath.Join(tmp, "bench."+string(m.f))
		t0 := time.Now()
		if err := Create(out, []string{src}, CreateOptions{Format: m.f, Level: 5}); err != nil {
			return nil, err
		}
		ct := time.Since(t0).Seconds()
		st, _ := os.Stat(out)
		t1 := time.Now()
		if _, err := Test(out, "", nil); err != nil {
			return nil, err
		}
		dt := time.Since(t1).Seconds()
		mb := float64(size) / (1 << 20)
		res = append(res, BenchmarkResult{Method: m.name, Compress: mb / max(ct, 1e-3), Decomp: mb / max(dt, 1e-3),
			Ratio: float64(st.Size()) / float64(size)})
		os.Remove(out)
	}
	if progress != nil {
		progress("", int64(len(methods)), int64(len(methods)))
	}
	return res, nil
}

// Walk calls fn for every member of an archive, in archive order; open reads
// the member's data (only valid during the call). Returning an error stops it.
func Walk(p, password string, fn func(e Entry, open func() (io.ReadCloser, error)) error) error {
	_, err := walk(p, password, fn)
	return err
}
