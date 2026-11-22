package zipcrypt

// Package zipcrypt adds ZIP encryption on top of Go's archive/zip raw entry API:
//   - WinZip AES (AE-1 / AE-2, 128/192/256-bit) read and write
//   - traditional PKWARE "ZipCrypto" read
// Implemented from the public WinZip AES and PKWARE APPNOTE specifications.

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"hash"
	"hash/crc32"
	"io"
)

var (
	ErrPasswordRequired = errors.New("password required")
	ErrBadPassword      = errors.New("wrong password or corrupt data")
	ErrChecksum         = errors.New("checksum error: the data is corrupt")
)

// Decompressors maps a ZIP compression method to a decompressor. Callers may
// add methods; Store and Deflate are built in.
type Decompressors func(method uint16) func(io.Reader) io.ReadCloser

func builtin(method uint16) func(io.Reader) io.ReadCloser {
	switch method {
	case zip.Store:
		return io.NopCloser
	case zip.Deflate:
		return flate.NewReader
	}
	return nil
}

const (
	methodAES      = 99
	aesExtraID     = 0x9901
	aesAuthCodeLen = 10
)

type aesInfo struct {
	version  uint16 // 1 = AE-1 (CRC stored), 2 = AE-2 (no CRC)
	strength byte   // 1,2,3 = 128,192,256 bit
	method   uint16 // real compression method
}

func parseAESExtra(extra []byte) (aesInfo, bool) {
	for len(extra) >= 4 {
		id := binary.LittleEndian.Uint16(extra)
		sz := int(binary.LittleEndian.Uint16(extra[2:]))
		if 4+sz > len(extra) {
			break
		}
		body := extra[4 : 4+sz]
		if id == aesExtraID && sz >= 7 {
			return aesInfo{
				version:  binary.LittleEndian.Uint16(body),
				strength: body[4],
				method:   binary.LittleEndian.Uint16(body[5:]),
			}, true
		}
		extra = extra[4+sz:]
	}
	return aesInfo{}, false
}

func aesKeyLen(strength byte) int {
	switch strength {
	case 1:
		return 16
	case 2:
		return 24
	case 3:
		return 32
	}
	return 0
}

func deriveAESKeys(password string, salt []byte, keyLen int) (encKey, authKey, verifier []byte, err error) {
	k, err := pbkdf2.Key(sha1.New, password, salt, 1000, 2*keyLen+2)
	if err != nil {
		return nil, nil, nil, err
	}
	return k[:keyLen], k[keyLen : 2*keyLen], k[2*keyLen:], nil
}

// winzipCTR is AES in counter mode with a little-endian counter starting at 1.
type winzipCTR struct {
	block   cipher.Block
	counter [aes.BlockSize]byte
	stream  [aes.BlockSize]byte
	pos     int
}

func newWinzipCTR(key []byte) (*winzipCTR, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return &winzipCTR{block: b, pos: aes.BlockSize}, nil
}

func (c *winzipCTR) XORKeyStream(dst, src []byte) {
	for i := range src {
		if c.pos == aes.BlockSize {
			for j := 0; j < aes.BlockSize; j++ { // little-endian increment
				c.counter[j]++
				if c.counter[j] != 0 {
					break
				}
			}
			c.block.Encrypt(c.stream[:], c.counter[:])
			c.pos = 0
		}
		dst[i] = src[i] ^ c.stream[c.pos]
		c.pos++
	}
}

// aesReader decrypts the WinZip AES payload and checks the authentication code at EOF.
type aesReader struct {
	src  io.Reader // ciphertext only (limited)
	tail io.Reader // the 10 byte auth code
	ctr  *winzipCTR
	mac  hash.Hash
	done bool
}

func (r *aesReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n, err := r.src.Read(p)
	if n > 0 {
		r.mac.Write(p[:n])
		r.ctr.XORKeyStream(p[:n], p[:n])
	}
	if err == io.EOF {
		r.done = true
		code := make([]byte, aesAuthCodeLen)
		if _, e := io.ReadFull(r.tail, code); e != nil {
			return n, io.ErrUnexpectedEOF
		}
		if subtle.ConstantTimeCompare(code, r.mac.Sum(nil)[:aesAuthCodeLen]) != 1 {
			return n, ErrChecksum
		}
	}
	return n, err
}

// zipCryptoKeys implements the traditional PKWARE stream cipher.
type zipCryptoKeys [3]uint32

func (k *zipCryptoKeys) update(b byte) {
	k[0] = crc32.IEEETable[byte(k[0])^b] ^ (k[0] >> 8)
	k[1] = (k[1]+(k[0]&0xff))*134775813 + 1
	k[2] = crc32.IEEETable[byte(k[2])^byte(k[1]>>24)] ^ (k[2] >> 8)
}

func (k *zipCryptoKeys) streamByte() byte {
	t := uint16(k[2] | 2)
	return byte((t * (t ^ 1)) >> 8)
}

type zipCryptoReader struct {
	src  io.Reader
	keys zipCryptoKeys
}

func (r *zipCryptoReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	for i := 0; i < n; i++ {
		c := p[i] ^ r.keys.streamByte()
		r.keys.update(c)
		p[i] = c
	}
	return n, err
}

// crcReader verifies the CRC-32 of the decompressed data at EOF.
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

// OpenEntry opens a (possibly encrypted) zip member for reading. dec may be nil.
func OpenEntry(f *zip.File, password string, dec Decompressors) (io.ReadCloser, error) {
	if f.Flags&0x1 == 0 {
		rc, err := f.Open()
		if errors.Is(err, zip.ErrAlgorithm) {
			return nil, errors.New("unsupported compression method in " + f.Name)
		}
		return rc, err
	}
	if password == "" {
		return nil, ErrPasswordRequired
	}
	raw, err := f.OpenRaw()
	if err != nil {
		return nil, err
	}
	var plain io.Reader
	method := f.Method
	checkCRC := true
	if info, ok := parseAESExtra(f.Extra); ok && f.Method == methodAES {
		keyLen := aesKeyLen(info.strength)
		if keyLen == 0 {
			return nil, errors.New("unknown AES strength")
		}
		saltLen := keyLen / 2
		hdr := make([]byte, saltLen+2)
		if _, err := io.ReadFull(raw, hdr); err != nil {
			return nil, err
		}
		encKey, authKey, verifier, err := deriveAESKeys(password, hdr[:saltLen], keyLen)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(verifier, hdr[saltLen:]) {
			return nil, ErrBadPassword
		}
		dataLen := int64(f.CompressedSize64) - int64(saltLen) - 2 - aesAuthCodeLen
		if dataLen < 0 {
			return nil, errors.New("corrupt AES entry")
		}
		ctr, err := newWinzipCTR(encKey)
		if err != nil {
			return nil, err
		}
		plain = &aesReader{src: io.LimitReader(raw, dataLen), tail: raw, ctr: ctr, mac: hmac.New(sha1.New, authKey)}
		method = info.method
		checkCRC = info.version == 1
	} else {
		keys := zipCryptoKeys{305419896, 591751049, 878082192}
		for i := 0; i < len(password); i++ {
			keys.update(password[i])
		}
		zc := &zipCryptoReader{src: raw, keys: keys}
		hdr := make([]byte, 12)
		if _, err := io.ReadFull(zc, hdr); err != nil {
			return nil, err
		}
		check := byte(f.CRC32 >> 24)
		if f.Flags&0x8 != 0 {
			check = byte(f.ModifiedTime >> 8)
		}
		if hdr[11] != check {
			return nil, ErrBadPassword
		}
		plain = zc
	}
	var dcomp func(io.Reader) io.ReadCloser
	if dec != nil {
		dcomp = dec(method)
	}
	if dcomp == nil {
		dcomp = builtin(method)
	}
	if dcomp == nil {
		return nil, errors.New("unsupported compression method in " + f.Name)
	}
	dr := dcomp(plain)
	var out io.Reader = dr
	if checkCRC {
		out = &crcReader{r: dr, h: crc32.NewIEEE(), want: f.CRC32}
	}
	return readCloser{Reader: out, close: dr.Close}, nil
}

// aesWriter encrypts into the zip raw stream and appends the auth code on Close.
// AESWriter encrypts an entry; call Finish after the data.
type AESWriter struct {
	w     io.Writer
	ctr   *winzipCTR
	mac   hash.Hash
	buf   []byte
	count int64
}

func (a *AESWriter) Write(p []byte) (int, error) {
	if cap(a.buf) < len(p) {
		a.buf = make([]byte, len(p))
	}
	b := a.buf[:len(p)]
	a.ctr.XORKeyStream(b, p)
	a.mac.Write(b)
	n, err := a.w.Write(b)
	a.count += int64(n)
	return len(p), err
}

// Finish appends the authentication code.
func (a *AESWriter) Finish() error {
	_, err := a.w.Write(a.mac.Sum(nil)[:aesAuthCodeLen])
	a.count += aesAuthCodeLen
	return err
}

// Count is the number of raw bytes written so far (compressed size).
func (a *AESWriter) Count() int64 { return a.count }

// NewAESEntry starts an AES-256 (AE-2) encrypted entry. The returned header must
// be finalised with the sizes once all data has been written.
func NewAESEntry(zw *zip.Writer, fh *zip.FileHeader, password string, realMethod uint16) (*AESWriter, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	encKey, authKey, verifier, err := deriveAESKeys(password, salt, 32)
	if err != nil {
		return nil, err
	}
	extra := make([]byte, 11)
	binary.LittleEndian.PutUint16(extra[0:], aesExtraID)
	binary.LittleEndian.PutUint16(extra[2:], 7)
	binary.LittleEndian.PutUint16(extra[4:], 2) // AE-2
	extra[6], extra[7] = 'A', 'E'
	extra[8] = 3 // AES-256
	binary.LittleEndian.PutUint16(extra[9:], realMethod)
	fh.Extra = append(fh.Extra, extra...)
	fh.Method = methodAES
	fh.Flags |= 0x1 | 0x8 // encrypted, sizes in data descriptor
	fh.CRC32 = 0
	fh.ReaderVersion = 51
	w, err := zw.CreateRaw(fh)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(append(salt, verifier...)); err != nil {
		return nil, err
	}
	ctr, err := newWinzipCTR(encKey)
	if err != nil {
		return nil, err
	}
	return &AESWriter{w: w, ctr: ctr, mac: hmac.New(sha1.New, authKey), count: int64(len(salt) + 2)}, nil
}
