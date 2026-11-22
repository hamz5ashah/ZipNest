package core

import (
	"bytes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/klauspost/compress/zstd"
)

type nestArchive struct {
	f       readSeekerAt
	size    int64
	mainEnd int64
	h       *nestHeader
	ix      *nestIndex
	aead    cipher.AEAD
}

func (a *nestArchive) Close() error { return a.f.Close() }

func (a *nestArchive) encrypted() bool { return a.h.flags&nestFlagEncrypted != 0 }

// recoveryStart returns where the recovery section starts (from its trailer).
func recoveryStart(r io.ReaderAt, size int64) (int64, bool) {
	if size < recTrailerSize {
		return 0, false
	}
	t := make([]byte, recTrailerSize)
	if _, err := r.ReadAt(t, size-recTrailerSize); err != nil || !bytes.Equal(t[:8], recTrailerMagic) ||
		crc32.ChecksumIEEE(t[:24]) != binary.LittleEndian.Uint32(t[24:]) {
		return 0, false
	}
	start := int64(binary.LittleEndian.Uint64(t[8:]))
	if start <= 0 || start > size {
		return 0, false
	}
	return start, true
}

func openNest(p, password string) (*nestArchive, error) {
	f, size, err := openArchiveFile(p)
	if err != nil {
		return nil, err
	}
	a := &nestArchive{f: f, size: size, mainEnd: size}
	fail := func(err error) (*nestArchive, error) {
		f.Close()
		return nil, err
	}
	head := make([]byte, nestHeaderSize)
	if _, err := f.ReadAt(head, 0); err != nil {
		return fail(ErrCorrupt)
	}
	if a.h, err = parseNestHeader(head); err != nil {
		return fail(err)
	}
	if start, ok := recoveryStart(f, size); ok {
		a.mainEnd = start
	}
	tb := make([]byte, nestTrailerSize)
	if a.mainEnd < nestHeaderSize+nestTrailerSize {
		return fail(ErrCorrupt)
	}
	if _, err := f.ReadAt(tb, a.mainEnd-nestTrailerSize); err != nil {
		return fail(ErrCorrupt)
	}
	tr, err := parseNestTrailer(tb)
	if err != nil {
		return fail(err)
	}
	if a.encrypted() && password != "" {
		if a.aead, err = nestAEAD(nestKey(password, a.h)); err != nil {
			return fail(err)
		}
	}
	if tr.indexOffset+tr.indexSize > uint64(a.mainEnd) || tr.indexRaw > 1<<31 {
		return fail(ErrCorrupt)
	}
	stored := make([]byte, tr.indexSize)
	if _, err := f.ReadAt(stored, int64(tr.indexOffset)); err != nil {
		return fail(ErrCorrupt)
	}
	if a.h.flags&nestFlagHiddenNames != 0 {
		if a.aead == nil {
			return fail(ErrPasswordRequired)
		}
		if stored, err = nestOpen(a.aead, stored, nestAAD("index", 0)); err != nil {
			return fail(err)
		}
	}
	dec, _ := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	raw, err := dec.DecodeAll(stored, make([]byte, 0, tr.indexRaw))
	dec.Close()
	if err != nil || uint64(len(raw)) != tr.indexRaw {
		return fail(ErrCorrupt)
	}
	if a.ix, err = parseNestIndex(raw); err != nil {
		return fail(err)
	}
	if a.ix.chunk == 0 {
		return fail(ErrCorrupt)
	}
	return a, nil
}

// nestStream reads ranges of the solid stream, caching the current block.
type nestStream struct {
	a      *nestArchive
	cur    int
	plain  []byte
	dec    *zstd.Decoder
	packed []byte
}

func (s *nestStream) load(n int) error {
	if s.plain != nil && s.cur == n {
		return nil
	}
	if n >= len(s.a.ix.blocks) {
		return ErrCorrupt
	}
	bl := s.a.ix.blocks[n]
	if bl.packed > 1<<30 || bl.unpacked > 1<<30 {
		return ErrCorrupt
	}
	if uint64(cap(s.packed)) < bl.packed {
		s.packed = make([]byte, bl.packed)
	}
	data := s.packed[:bl.packed]
	if _, err := s.a.f.ReadAt(data, int64(bl.offset)); err != nil {
		return fmt.Errorf("block %d: %w", n, ErrCorrupt)
	}
	if crc32.ChecksumIEEE(data) != bl.crc {
		return fmt.Errorf("block %d is damaged: %w", n, ErrCorrupt)
	}
	if s.a.encrypted() {
		if s.a.aead == nil {
			return ErrPasswordRequired
		}
		var err error
		if data, err = nestOpen(s.a.aead, data, nestAAD("block", uint64(n))); err != nil {
			return err
		}
	}
	switch bl.method {
	case nestMethodStore:
		s.plain = append(s.plain[:0], data...)
	case nestMethodZstd:
		if s.dec == nil {
			s.dec, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(1<<31))
		}
		out, err := s.dec.DecodeAll(data, s.plain[:0])
		if err != nil {
			return fmt.Errorf("block %d: %w", n, ErrCorrupt)
		}
		s.plain = out
	default:
		return fmt.Errorf("unknown compression method %d", bl.method)
	}
	if uint64(len(s.plain)) != bl.unpacked {
		return ErrCorrupt
	}
	s.cur = n
	return nil
}

func (s *nestStream) close() {
	if s.dec != nil {
		s.dec.Close()
	}
}

type nestSection struct {
	s        *nestStream
	pos, end uint64
}

func (r *nestSection) Read(p []byte) (int, error) {
	if r.pos >= r.end {
		return 0, io.EOF
	}
	chunk := r.s.a.ix.chunk
	n := int(r.pos / chunk)
	if err := r.s.load(n); err != nil {
		return 0, err
	}
	in := r.pos - uint64(n)*chunk
	if in >= uint64(len(r.s.plain)) {
		return 0, ErrCorrupt
	}
	avail := uint64(len(r.s.plain)) - in
	want := min(uint64(len(p)), avail, r.end-r.pos)
	copy(p, r.s.plain[in:in+want])
	r.pos += want
	return int(want), nil
}

func walkNest(p, password string, fn visitFunc) error {
	a, err := openNest(p, password)
	if err != nil {
		return err
	}
	defer a.Close()
	st := &nestStream{a: a, cur: -1}
	defer st.close()
	for _, fe := range a.ix.files {
		clean := CleanPath(fe.name)
		if clean == "" {
			continue
		}
		e := Entry{Path: clean, Dir: fe.dir, Size: int64(fe.size), Packed: -1,
			Modified: fe.mtime / 1e6, Encrypted: a.encrypted() && !fe.dir}
		f := fe
		err := fn(e, func() (io.ReadCloser, error) {
			if a.encrypted() && a.aead == nil && f.size > 0 {
				return nil, ErrPasswordRequired
			}
			sec := &nestSection{s: st, pos: f.offset, end: f.offset + f.size}
			return readCloser{Reader: &crcReader{r: sec, h: crc32.NewIEEE(), want: f.crc}}, nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// NestInfo returns details about a .nest archive (comment needs the password
// when file names are hidden).
func NestInfo(p, password string) (*NestDetails, error) {
	f, size, err := openArchiveFile(p)
	if err != nil {
		return nil, err
	}
	head := make([]byte, nestHeaderSize)
	_, rerr := f.ReadAt(head, 0)
	h, err := parseNestHeader(head)
	if rerr != nil || err != nil {
		f.Close()
		return nil, ErrCorrupt
	}
	d := &NestDetails{Encrypted: h.flags&nestFlagEncrypted != 0, HiddenNames: h.flags&nestFlagHiddenNames != 0,
		Locked: h.flags&nestFlagLocked != 0}
	if l, _ := findRecovery(f, size); l != nil {
		d.RecoveryPercent = l.pct
	}
	f.Close()
	if a, err := openNest(p, password); err == nil {
		d.Comment = a.ix.comment
		a.Close()
	}
	return d, nil
}
