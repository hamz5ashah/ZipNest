// Package core is ZipNest's archive engine. It is shared by the Windows
// desktop app (linked directly) and the Android app (shipped as a small
// command-line binary that the Java UI drives).
package core

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"zipnest/core/zipcrypt"
)

// Format identifies an archive type.
type Format string

const (
	FormatZip    Format = "zip"
	Format7z     Format = "7z"
	FormatRar    Format = "rar"
	FormatTar    Format = "tar"
	FormatTarGz  Format = "tar.gz"
	FormatTarBz2 Format = "tar.bz2"
	FormatTarXz  Format = "tar.xz"
	FormatTarZst Format = "tar.zst"
	FormatGz     Format = "gz"
	FormatBz2    Format = "bz2"
	FormatXz     Format = "xz"
	FormatZst    Format = "zst"
	FormatNest   Format = "nest" // ZipNest's own format
)

// WritableFormats are the formats ZipNest can create.
var WritableFormats = []Format{FormatNest, FormatZip, Format7z, FormatTar, FormatTarGz, FormatTarXz, FormatTarZst}

var (
	ErrPasswordRequired = zipcrypt.ErrPasswordRequired
	ErrBadPassword      = zipcrypt.ErrBadPassword
	ErrCancelled        = errors.New("cancelled")
	ErrUnknownFormat    = errors.New("not a supported archive")
	ErrReadOnlyFormat   = errors.New("this archive format can only be extracted, not modified")
	ErrChecksum         = zipcrypt.ErrChecksum
)

// Entry describes one item inside an archive.
type Entry struct {
	Path      string `json:"path"`      // '/'-separated, no leading or trailing '/'
	Dir       bool   `json:"dir"`       // directory entry
	Size      int64  `json:"size"`      // uncompressed size, -1 if unknown
	Packed    int64  `json:"packed"`    // compressed size, -1 if unknown
	Modified  int64  `json:"mtime"`     // unix milliseconds, 0 if unknown
	Encrypted bool   `json:"encrypted"` // data is password-protected
	Link      bool   `json:"link"`      // symbolic/hard link (never extracted)
}

// ProgressFunc reports progress. It returns false to cancel the operation.
// total is 0 when unknown.
type ProgressFunc func(file string, done, total int64) bool

var extFormats = []struct {
	ext string
	f   Format
}{
	{".tar.gz", FormatTarGz}, {".tgz", FormatTarGz}, {".taz", FormatTarGz},
	{".tar.bz2", FormatTarBz2}, {".tbz2", FormatTarBz2}, {".tbz", FormatTarBz2},
	{".tar.xz", FormatTarXz}, {".txz", FormatTarXz},
	{".tar.zst", FormatTarZst}, {".tzst", FormatTarZst},
	{".nest", FormatNest},
	{".zip", FormatZip}, {".zipx", FormatZip}, {".jar", FormatZip}, {".apk", FormatZip},
	{".cbz", FormatZip}, {".epub", FormatZip}, {".docx", FormatZip}, {".xlsx", FormatZip},
	{".7z", Format7z}, {".cb7", Format7z},
	{".rar", FormatRar}, {".cbr", FormatRar},
	{".tar", FormatTar}, {".cbt", FormatTar},
	{".gz", FormatGz}, {".bz2", FormatBz2}, {".xz", FormatXz}, {".zst", FormatZst},
}

// FormatFromName guesses the format from a file name ("" if unknown). The
// first part of a split archive ("x.7z.001") is named after the inner format.
func FormatFromName(name string) Format {
	n := strings.ToLower(name)
	if m := volumeRe.FindStringSubmatch(n); m != nil && m[2] == strings.Repeat("0", len(m[2])-1)+"1" {
		if f := FormatFromName(m[1]); f != "" {
			return f
		}
	}
	for _, e := range extFormats {
		if strings.HasSuffix(n, e.ext) {
			return e.f
		}
	}
	// Multi-volume RAR (.r00, .r01, ...).
	if len(n) > 4 && n[len(n)-4] == '.' && n[len(n)-3] == 'r' &&
		n[len(n)-2] >= '0' && n[len(n)-2] <= '9' && n[len(n)-1] >= '0' && n[len(n)-1] <= '9' {
		return FormatRar
	}
	return ""
}

// IsArchiveName reports whether the name looks like an archive ZipNest can open.
// First parts of split archives (name.7z.001, name.zip.001) are detected by content.
func IsArchiveName(name string) bool {
	return FormatFromName(name) != "" || strings.HasSuffix(strings.ToLower(name), ".001")
}

// Detect identifies the archive format from the file's signature, falling
// back to the file name.
func Detect(p string) (Format, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	byName := FormatFromName(p)
	switch {
	case bytes.HasPrefix(head, []byte("PK\x03\x04")), bytes.HasPrefix(head, []byte("PK\x05\x06")),
		bytes.HasPrefix(head, []byte("PK\x07\x08")):
		return FormatZip, nil
	case bytes.HasPrefix(head, nestMagic):
		return FormatNest, nil
	case bytes.HasPrefix(head, []byte("Rar!\x1a\x07")):
		return FormatRar, nil
	case bytes.HasPrefix(head, []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C}):
		return Format7z, nil
	case bytes.HasPrefix(head, []byte{0x1F, 0x8B}):
		if byName == FormatGz {
			return FormatGz, nil
		}
		return FormatTarGz, nil
	case bytes.HasPrefix(head, []byte("BZh")):
		if byName == FormatBz2 {
			return FormatBz2, nil
		}
		return FormatTarBz2, nil
	case bytes.HasPrefix(head, []byte{0xFD, '7', 'z', 'X', 'Z', 0}):
		if byName == FormatXz {
			return FormatXz, nil
		}
		return FormatTarXz, nil
	case bytes.HasPrefix(head, []byte{0x28, 0xB5, 0x2F, 0xFD}):
		if byName == FormatZst {
			return FormatZst, nil
		}
		return FormatTarZst, nil
	case len(head) >= 262 && bytes.Equal(head[257:262], []byte("ustar")):
		return FormatTar, nil
	}
	if isSFX(p, head) {
		return FormatZip, nil
	}
	if byName == FormatZip || byName == FormatRar {
		// Self-extracting or damaged archives: let the format reader try.
		return byName, nil
	}
	if byName == FormatTar {
		return FormatTar, nil
	}
	return "", ErrUnknownFormat
}

// CleanPath turns an archive member name into a safe relative path using
// '/' separators. It returns "" for names that cannot be made safe.
func CleanPath(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	// Strip drive letters and leading slashes.
	if len(name) >= 2 && name[1] == ':' {
		name = name[2:]
	}
	name = strings.TrimLeft(name, "/")
	parts := strings.Split(name, "/")
	out := parts[:0]
	for _, p := range parts {
		switch p {
		case "", ".":
			continue
		case "..":
			return "" // never allow escaping the destination
		}
		out = append(out, p)
	}
	return strings.Join(out, "/")
}

// decodeName converts a non-UTF-8 member name (legacy ZIPs) using code page 437.
func decodeName(name string) string {
	if utf8.ValidString(name) {
		return name
	}
	r := make([]rune, 0, len(name))
	for i := 0; i < len(name); i++ {
		r = append(r, cp437[name[i]])
	}
	return string(r)
}

// selected reports whether p is one of sel or inside one of them.
func selected(p string, sel []string) bool {
	if len(sel) == 0 {
		return true
	}
	for _, s := range sel {
		s = strings.Trim(s, "/")
		if s == "" || p == s || strings.HasPrefix(p, s+"/") {
			return true
		}
	}
	return false
}

// Children returns the immediate children of dir ("" = root) in a flat entry
// list, synthesising folders that only exist implicitly. Folders come first.
func Children(all []Entry, dir string) []Entry {
	dir = strings.Trim(dir, "/")
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	byName := map[string]*Entry{}
	var order []string
	for _, e := range all {
		if !strings.HasPrefix(e.Path, prefix) || e.Path == dir {
			continue
		}
		rest := e.Path[len(prefix):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			name := rest[:i]
			d, ok := byName[name]
			if !ok {
				d = &Entry{Path: prefix + name, Dir: true}
				byName[name] = d
				order = append(order, name)
			}
			if !e.Dir && e.Size > 0 {
				d.Size += e.Size
			}
			if !e.Dir && e.Packed > 0 {
				d.Packed += e.Packed
			}
			continue
		}
		if d, ok := byName[rest]; ok {
			// Explicit entry for a folder we already synthesised.
			if e.Modified != 0 {
				d.Modified = e.Modified
			}
			if !e.Dir {
				*d = e
			}
			continue
		}
		c := e
		byName[rest] = &c
		order = append(order, rest)
	}
	out := make([]Entry, 0, len(order))
	for _, n := range order {
		out = append(out, *byName[n])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
	})
	return out
}

// BaseName returns the last path element of an entry path.
func BaseName(p string) string { return path.Base(p) }

// archiveStem returns the archive file name without its archive extension.
func archiveStem(p string) string {
	base := filepath.Base(p)
	if m := volumeRe.FindStringSubmatch(base); m != nil {
		base = m[1] // "photos.zip.001" -> "photos.zip"
	}
	n := strings.ToLower(base)
	for _, e := range extFormats {
		if strings.HasSuffix(n, e.ext) {
			return base[:len(base)-len(e.ext)]
		}
	}
	if ext := filepath.Ext(base); ext != "" {
		return base[:len(base)-len(ext)]
	}
	return base
}

// ArchiveStem is the archive name without extension ("photos.tar.gz" -> "photos"),
// used for "Extract to <name>\" destinations.
func ArchiveStem(p string) string { return archiveStem(p) }

// counter wraps a writer and reports progress.
type counter struct {
	w        io.Writer
	name     string
	done     *int64
	total    int64
	progress ProgressFunc
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	*c.done += int64(n)
	if c.progress != nil && !c.progress(c.name, *c.done, c.total) {
		return n, ErrCancelled
	}
	return n, err
}

var cp437 = [256]rune{
	0x0000, 0x263A, 0x263B, 0x2665, 0x2666, 0x2663, 0x2660, 0x2022, 0x25D8, 0x25CB, 0x25D9, 0x2642, 0x2640, 0x266A, 0x266B, 0x263C,
	0x25BA, 0x25C4, 0x2195, 0x203C, 0x00B6, 0x00A7, 0x25AC, 0x21A8, 0x2191, 0x2193, 0x2192, 0x2190, 0x221F, 0x2194, 0x25B2, 0x25BC,
	' ', '!', '"', '#', '$', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ':', ';', '<', '=', '>', '?',
	'@', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O',
	'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', '[', '\\', ']', '^', '_',
	'`', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', '{', '|', '}', '~', 0x2302,
	0x00C7, 0x00FC, 0x00E9, 0x00E2, 0x00E4, 0x00E0, 0x00E5, 0x00E7, 0x00EA, 0x00EB, 0x00E8, 0x00EF, 0x00EE, 0x00EC, 0x00C4, 0x00C5,
	0x00C9, 0x00E6, 0x00C6, 0x00F4, 0x00F6, 0x00F2, 0x00FB, 0x00F9, 0x00FF, 0x00D6, 0x00DC, 0x00A2, 0x00A3, 0x00A5, 0x20A7, 0x0192,
	0x00E1, 0x00ED, 0x00F3, 0x00FA, 0x00F1, 0x00D1, 0x00AA, 0x00BA, 0x00BF, 0x2310, 0x00AC, 0x00BD, 0x00BC, 0x00A1, 0x00AB, 0x00BB,
	0x2591, 0x2592, 0x2593, 0x2502, 0x2524, 0x2561, 0x2562, 0x2556, 0x2555, 0x2563, 0x2551, 0x2557, 0x255D, 0x255C, 0x255B, 0x2510,
	0x2514, 0x2534, 0x252C, 0x251C, 0x2500, 0x253C, 0x255E, 0x255F, 0x255A, 0x2554, 0x2569, 0x2566, 0x2560, 0x2550, 0x256C, 0x2567,
	0x2568, 0x2564, 0x2565, 0x2559, 0x2558, 0x2552, 0x2553, 0x256B, 0x256A, 0x2518, 0x250C, 0x2588, 0x2584, 0x258C, 0x2590, 0x2580,
	0x03B1, 0x00DF, 0x0393, 0x03C0, 0x03A3, 0x03C3, 0x00B5, 0x03C4, 0x03A6, 0x0398, 0x03A9, 0x03B4, 0x221E, 0x03C6, 0x03B5, 0x2229,
	0x2261, 0x00B1, 0x2265, 0x2264, 0x2320, 0x2321, 0x00F7, 0x2248, 0x00B0, 0x2219, 0x00B7, 0x221A, 0x207F, 0x00B2, 0x25A0, 0x00A0,
}
