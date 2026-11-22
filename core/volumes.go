package core

// Split archives ("name.zip.001", "name.zip.002", ...): writing and reading.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var volumeRe = regexp.MustCompile(`^(.*)\.(\d{3,})$`)

// volumeParts returns all parts of a split archive given its first part
// (name.001), or nil if p is not the first part of a split set.
func volumeParts(p string) []string {
	m := volumeRe.FindStringSubmatch(p)
	if m == nil {
		return nil
	}
	if n, _ := strconv.Atoi(m[2]); n != 1 {
		return nil
	}
	width := len(m[2])
	var parts []string
	for i := 1; ; i++ {
		name := fmt.Sprintf("%s.%0*d", m[1], width, i)
		if _, err := os.Stat(name); err != nil {
			break
		}
		parts = append(parts, name)
	}
	return parts
}

// multiFile presents the parts of a split archive as one file.
type multiFile struct {
	files   []*os.File
	offsets []int64 // start offset of each part
	size    int64
	pos     int64
}

func openMultiFile(parts []string) (*multiFile, error) {
	m := &multiFile{}
	for _, p := range parts {
		f, err := os.Open(p)
		if err != nil {
			m.Close()
			return nil, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			m.Close()
			return nil, err
		}
		m.files = append(m.files, f)
		m.offsets = append(m.offsets, m.size)
		m.size += st.Size()
	}
	return m, nil
}

func (m *multiFile) Size() int64 { return m.size }

func (m *multiFile) ReadAt(b []byte, off int64) (int, error) {
	if off >= m.size {
		return 0, io.EOF
	}
	total := 0
	for len(b) > 0 && off < m.size {
		i := sort.Search(len(m.offsets), func(i int) bool { return m.offsets[i] > off }) - 1
		end := m.size
		if i+1 < len(m.offsets) {
			end = m.offsets[i+1]
		}
		n := int64(len(b))
		if off+n > end {
			n = end - off
		}
		got, err := m.files[i].ReadAt(b[:n], off-m.offsets[i])
		total += got
		off += int64(got)
		b = b[got:]
		if err != nil && err != io.EOF {
			return total, err
		}
		if int64(got) < n {
			return total, io.ErrUnexpectedEOF
		}
	}
	if len(b) > 0 {
		return total, io.EOF
	}
	return total, nil
}

func (m *multiFile) Read(b []byte) (int, error) {
	n, err := m.ReadAt(b, m.pos)
	m.pos += int64(n)
	if err == io.ErrUnexpectedEOF || (err == io.EOF && n > 0) {
		err = nil
	}
	return n, err
}

func (m *multiFile) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		off += m.pos
	case io.SeekEnd:
		off += m.size
	}
	if off < 0 {
		return 0, errors.New("negative seek")
	}
	m.pos = off
	return off, nil
}

func (m *multiFile) Close() error {
	for _, f := range m.files {
		f.Close()
	}
	return nil
}

// openArchiveFile opens an archive for reading: a normal file, or a split set
// when p is its first part.
func openArchiveFile(p string) (readSeekerAt, int64, error) {
	if parts := volumeParts(p); len(parts) > 0 {
		m, err := openMultiFile(parts)
		if err != nil {
			return nil, 0, err
		}
		return m, m.size, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

type readSeekerAt interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
}

// splitFile cuts a finished archive into parts of size bytes and removes it.
// Returns the part names.
func splitFile(p string, size int64) ([]string, error) {
	if size <= 0 {
		return []string{p}, nil
	}
	in, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return nil, err
	}
	n := int((st.Size() + size - 1) / size)
	if n == 0 {
		n = 1
	}
	width := 3
	if n > 999 {
		width = len(strconv.Itoa(n))
	}
	var parts []string
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("%s.%0*d", p, width, i)
		out, err := os.Create(name)
		if err != nil {
			return parts, err
		}
		_, err = io.CopyN(out, in, size)
		cerr := out.Close()
		if err != nil && err != io.EOF {
			return parts, err
		}
		if cerr != nil {
			return parts, cerr
		}
		parts = append(parts, name)
	}
	// Remove stale parts left over from an earlier, larger split.
	for i := n + 1; ; i++ {
		name := fmt.Sprintf("%s.%0*d", p, width, i)
		if os.Remove(name) != nil {
			break
		}
	}
	in.Close()
	return parts, os.Remove(p)
}

// isSFX reports whether head (start of a file) is a Windows executable that has a
// ZIP archive appended (a self-extracting archive).
func isSFX(p string, head []byte) bool {
	if !bytes.HasPrefix(head, []byte("MZ")) {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false
	}
	n := int64(66000)
	if st.Size() < n {
		n = st.Size()
	}
	tail := make([]byte, n)
	if _, err := f.ReadAt(tail, st.Size()-n); err != nil && err != io.EOF {
		return false
	}
	return bytes.Contains(tail, []byte("PK\x05\x06"))
}

// isVolumeName reports whether name looks like any part of a split set.
func isVolumeName(name string) bool {
	return volumeRe.MatchString(strings.ToLower(name))
}
