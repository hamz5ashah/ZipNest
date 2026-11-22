package core

// The ZipNest archive format (.nest), version 1.
//
//	[header 40 B][block 0][block 1]...[index][trailer 40 B]([recovery section])
//
// All file data forms one "solid stream" (files back to back, grouped by type);
// the stream is cut into chunks of nestChunk bytes and every chunk is compressed
// (Zstandard) and optionally encrypted (AES-256-GCM) on its own, so chunks can
// be processed in parallel and memory use stays bounded.
//
// The index lists blocks and files; it is Zstandard-compressed and, when file
// names are hidden, encrypted. The optional recovery section holds Reed-Solomon
// parity data that can rebuild damaged parts of the archive (see nest_recovery.go).
//
// Keys are derived from the password with Argon2id. Every block and the index
// are authenticated by GCM, so a wrong password or tampering is detected.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"golang.org/x/crypto/argon2"
)

var (
	nestMagic        = []byte("ZNEST\x1a\x0a\x00")
	nestTrailerMagic = []byte("ZNESTEND")
	// ErrCorrupt is returned when an archive's structure is damaged.
	ErrCorrupt = errors.New("the archive is damaged (use Repair if it has a recovery record)")
	// ErrLocked is returned when changing a locked archive.
	ErrLocked = errors.New("this archive is locked and cannot be modified")
)

const (
	nestHeaderSize  = 40
	nestTrailerSize = 40
	nestVersion     = 1

	nestFlagEncrypted   = 1 // file data encrypted
	nestFlagHiddenNames = 2 // index encrypted
	nestFlagLocked      = 4
	nestFlagRecovery    = 8

	nestMethodStore = 0
	nestMethodZstd  = 1

	argonTime    = 3
	argonMemMiB  = 64
	argonThreads = 4
)

type nestHeader struct {
	flags   uint16
	salt    [16]byte
	time    uint8
	memMiB  uint16
	threads uint8
}

func (h *nestHeader) marshal() []byte {
	b := make([]byte, nestHeaderSize)
	copy(b, nestMagic)
	binary.LittleEndian.PutUint16(b[8:], nestVersion)
	binary.LittleEndian.PutUint16(b[10:], h.flags)
	copy(b[12:28], h.salt[:])
	b[28] = h.time
	binary.LittleEndian.PutUint16(b[29:], h.memMiB)
	b[31] = h.threads
	binary.LittleEndian.PutUint32(b[36:], crc32.ChecksumIEEE(b[:36]))
	return b
}

func parseNestHeader(b []byte) (*nestHeader, error) {
	if len(b) < nestHeaderSize || !bytes.Equal(b[:8], nestMagic) {
		return nil, ErrUnknownFormat
	}
	if crc32.ChecksumIEEE(b[:36]) != binary.LittleEndian.Uint32(b[36:]) {
		return nil, ErrCorrupt
	}
	if v := binary.LittleEndian.Uint16(b[8:]); v != nestVersion {
		return nil, fmt.Errorf("this .nest archive needs a newer ZipNest (format version %d)", v)
	}
	h := &nestHeader{flags: binary.LittleEndian.Uint16(b[10:]), time: b[28],
		memMiB: binary.LittleEndian.Uint16(b[29:]), threads: b[31]}
	copy(h.salt[:], b[12:28])
	return h, nil
}

type nestTrailer struct {
	indexOffset, indexSize, indexRaw uint64
}

func (t *nestTrailer) marshal() []byte {
	b := make([]byte, nestTrailerSize)
	copy(b, nestTrailerMagic)
	binary.LittleEndian.PutUint64(b[8:], t.indexOffset)
	binary.LittleEndian.PutUint64(b[16:], t.indexSize)
	binary.LittleEndian.PutUint64(b[24:], t.indexRaw)
	binary.LittleEndian.PutUint32(b[36:], crc32.ChecksumIEEE(b[:36]))
	return b
}

func parseNestTrailer(b []byte) (*nestTrailer, error) {
	if len(b) < nestTrailerSize || !bytes.Equal(b[:8], nestTrailerMagic) ||
		crc32.ChecksumIEEE(b[:36]) != binary.LittleEndian.Uint32(b[36:]) {
		return nil, ErrCorrupt
	}
	return &nestTrailer{binary.LittleEndian.Uint64(b[8:]), binary.LittleEndian.Uint64(b[16:]),
		binary.LittleEndian.Uint64(b[24:])}, nil
}

// ---------------------------------------------------------------- index

type nestBlock struct {
	offset, packed, unpacked uint64
	method                   byte
	crc                      uint32 // CRC-32 of the packed bytes (damage detection)
}

type nestFile struct {
	name   string
	dir    bool
	size   uint64
	mtime  int64 // unix nanoseconds
	mode   uint32
	offset uint64 // position in the solid stream
	crc    uint32
}

type nestIndex struct {
	chunk   uint64 // uncompressed bytes per block
	blocks  []nestBlock
	files   []nestFile
	comment string
}

func (ix *nestIndex) marshal() []byte {
	var b bytes.Buffer
	var t [binary.MaxVarintLen64]byte
	uv := func(v uint64) { b.Write(t[:binary.PutUvarint(t[:], v)]) }
	sv := func(v int64) { b.Write(t[:binary.PutVarint(t[:], v)]) }
	u32 := func(v uint32) {
		var x [4]byte
		binary.LittleEndian.PutUint32(x[:], v)
		b.Write(x[:])
	}
	str := func(s string) { uv(uint64(len(s))); b.WriteString(s) }
	b.WriteString("IDX1")
	uv(ix.chunk)
	uv(uint64(len(ix.blocks)))
	for _, bl := range ix.blocks {
		uv(bl.offset)
		uv(bl.packed)
		uv(bl.unpacked)
		b.WriteByte(bl.method)
		u32(bl.crc)
	}
	uv(uint64(len(ix.files)))
	for _, f := range ix.files {
		str(f.name)
		if f.dir {
			b.WriteByte(1)
		} else {
			b.WriteByte(0)
		}
		uv(f.size)
		sv(f.mtime)
		uv(uint64(f.mode))
		uv(f.offset)
		u32(f.crc)
	}
	str(ix.comment)
	return b.Bytes()
}

func parseNestIndex(data []byte) (*nestIndex, error) {
	r := bytes.NewReader(data)
	bad := ErrCorrupt
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != "IDX1" {
		return nil, bad
	}
	var err error
	uv := func() uint64 {
		v, e := binary.ReadUvarint(r)
		if e != nil {
			err = bad
		}
		return v
	}
	sv := func() int64 {
		v, e := binary.ReadVarint(r)
		if e != nil {
			err = bad
		}
		return v
	}
	u32 := func() uint32 {
		var x [4]byte
		if _, e := io.ReadFull(r, x[:]); e != nil {
			err = bad
		}
		return binary.LittleEndian.Uint32(x[:])
	}
	str := func() string {
		n := uv()
		if n > uint64(r.Len()) {
			err = bad
			return ""
		}
		s := make([]byte, n)
		io.ReadFull(r, s)
		return string(s)
	}
	ix := &nestIndex{chunk: uv()}
	nb := uv()
	if nb > uint64(len(data)) {
		return nil, bad
	}
	for i := uint64(0); i < nb && err == nil; i++ {
		bl := nestBlock{offset: uv(), packed: uv(), unpacked: uv()}
		m, e := r.ReadByte()
		if e != nil {
			return nil, bad
		}
		bl.method = m
		bl.crc = u32()
		ix.blocks = append(ix.blocks, bl)
	}
	nf := uv()
	if nf > uint64(len(data)) {
		return nil, bad
	}
	for i := uint64(0); i < nf && err == nil; i++ {
		f := nestFile{name: str()}
		k, e := r.ReadByte()
		if e != nil {
			return nil, bad
		}
		f.dir = k == 1
		f.size = uv()
		f.mtime = sv()
		f.mode = uint32(uv())
		f.offset = uv()
		f.crc = u32()
		ix.files = append(ix.files, f)
	}
	ix.comment = str()
	if err != nil {
		return nil, err
	}
	return ix, nil
}

// ---------------------------------------------------------------- crypto

func nestKey(password string, h *nestHeader) []byte {
	return argon2.IDKey([]byte(password), h.salt[:], uint32(h.time), uint32(h.memMiB)*1024, h.threads, 32)
}

func nestAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nestAAD(kind string, n uint64) []byte {
	b := make([]byte, len(kind)+8)
	copy(b, kind)
	binary.LittleEndian.PutUint64(b[len(kind):], n)
	return b
}

func nestSeal(aead cipher.AEAD, plain, aad []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, aad), nil
}

func nestOpen(aead cipher.AEAD, sealed, aad []byte) ([]byte, error) {
	n := aead.NonceSize()
	if len(sealed) < n {
		return nil, ErrCorrupt
	}
	plain, err := aead.Open(nil, sealed[:n], sealed[n:], aad)
	if err != nil {
		return nil, ErrBadPassword
	}
	return plain, nil
}

// NestDetails describes a .nest archive for the information window.
type NestDetails struct {
	Encrypted, HiddenNames, Locked bool
	RecoveryPercent                int
	Comment                        string
}
