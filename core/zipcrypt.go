package core

import (
	"archive/zip"
	"hash"
	"io"

	"zipnest/core/zipcrypt"
)

// readCloser pairs a reader with a close function.
type readCloser struct {
	io.Reader
	close func() error
}

func (r readCloser) Close() error {
	if r.close != nil {
		return r.close()
	}
	return nil
}

// openZipEntry opens a (possibly encrypted) zip member with all of ZIP methods
// ZipNest supports (Store, Deflate, BZip2, Zstandard, XZ).
func openZipEntry(_ *zip.Reader, f *zip.File, password string) (io.ReadCloser, error) {
	return zipcrypt.OpenEntry(f, password, func(m uint16) func(io.Reader) io.ReadCloser { return decompressorFor(nil, m) })
}

func newAESEntry(zw *zip.Writer, fh *zip.FileHeader, password string, realMethod uint16) (*zipcrypt.AESWriter, error) {
	return zipcrypt.NewAESEntry(zw, fh, password, realMethod)
}

// crcReader verifies the CRC-32 of the data at EOF.
type crcReader struct {
	r    io.Reader
	h    hash.Hash32
	want uint32
}

func (c *crcReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.h.Write(p[:n])
	if err == io.EOF && c.h.Sum32() != c.want {
		return n, ErrChecksum
	}
	return n, err
}
