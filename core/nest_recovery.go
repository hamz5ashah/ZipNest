package core

// Recovery record for .nest archives (Reed-Solomon error correction).
//
// The protected area (everything before the recovery section) is cut into
// shards of S bytes. Shard i belongs to group i % G (interleaving), so a long
// run of consecutive damage is spread over all groups: each group can lose up to
// P of its D data shards. With p% recovery, contiguous damage of about p% of the
// archive can be repaired. Every shard has a CRC-32, which tells repair which
// shards are bad.
//
//	section: [header 48 B][CRC tables][parity shards][copy of header+tables][trailer 28 B]

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"

	"github.com/klauspost/reedsolomon"
)

var (
	recMagic        = []byte("ZNESTREC")
	recTrailerMagic = []byte("ZNESTRTL")
)

const (
	recHeaderSize  = 48
	recTrailerSize = 28
	recMaxData     = 200 // data shards per group (data+parity <= 256)
)

type recLayout struct {
	pct             int
	shard           uint64
	mainSize        uint64
	groups, data    int
	parity          int
	chunks          int // number of data shards in the protected area
	dataCRC, parCRC []uint32
}

func newRecLayout(mainSize uint64, pct int) *recLayout {
	pct = max(1, min(pct, 50))
	shard := uint64(4096)
	for shard < 256<<10 && mainSize/shard > 4096 {
		shard *= 2
	}
	chunks := int((mainSize + shard - 1) / shard)
	groups := max(1, (chunks+recMaxData-1)/recMaxData)
	data := (chunks + groups - 1) / groups
	parity := max(1, (data*pct+99)/100)
	if data+parity > 256 {
		parity = 256 - data
	}
	return &recLayout{pct: pct, shard: shard, mainSize: mainSize, groups: groups, data: data, parity: parity, chunks: chunks}
}

func (l *recLayout) headerAndTables() []byte {
	var b bytes.Buffer
	h := make([]byte, recHeaderSize)
	copy(h, recMagic)
	binary.LittleEndian.PutUint16(h[8:], 1)
	binary.LittleEndian.PutUint16(h[10:], uint16(l.pct))
	binary.LittleEndian.PutUint32(h[12:], uint32(l.shard))
	binary.LittleEndian.PutUint64(h[16:], l.mainSize)
	binary.LittleEndian.PutUint32(h[24:], uint32(l.groups))
	binary.LittleEndian.PutUint32(h[28:], uint32(l.data))
	binary.LittleEndian.PutUint32(h[32:], uint32(l.parity))
	binary.LittleEndian.PutUint32(h[36:], uint32(l.chunks))
	var tables bytes.Buffer
	for _, c := range l.dataCRC {
		binary.Write(&tables, binary.LittleEndian, c)
	}
	for _, c := range l.parCRC {
		binary.Write(&tables, binary.LittleEndian, c)
	}
	binary.LittleEndian.PutUint32(h[40:], crc32.ChecksumIEEE(tables.Bytes()))
	binary.LittleEndian.PutUint32(h[44:], crc32.ChecksumIEEE(h[:44]))
	b.Write(h)
	b.Write(tables.Bytes())
	return b.Bytes()
}

// parseRecHeader reads header+tables at off; returns nil if invalid.
func parseRecHeader(r io.ReaderAt, off int64) *recLayout {
	h := make([]byte, recHeaderSize)
	if _, err := r.ReadAt(h, off); err != nil || !bytes.Equal(h[:8], recMagic) ||
		crc32.ChecksumIEEE(h[:44]) != binary.LittleEndian.Uint32(h[44:]) {
		return nil
	}
	l := &recLayout{pct: int(binary.LittleEndian.Uint16(h[10:])), shard: uint64(binary.LittleEndian.Uint32(h[12:])),
		mainSize: binary.LittleEndian.Uint64(h[16:]), groups: int(binary.LittleEndian.Uint32(h[24:])),
		data: int(binary.LittleEndian.Uint32(h[28:])), parity: int(binary.LittleEndian.Uint32(h[32:])),
		chunks: int(binary.LittleEndian.Uint32(h[36:]))}
	if l.shard == 0 || l.groups <= 0 || l.data <= 0 || l.parity <= 0 || l.data+l.parity > 256 || l.chunks > l.groups*l.data {
		return nil
	}
	n := l.chunks + l.groups*l.parity
	tables := make([]byte, 4*n)
	if _, err := r.ReadAt(tables, off+recHeaderSize); err != nil ||
		crc32.ChecksumIEEE(tables) != binary.LittleEndian.Uint32(h[40:]) {
		return nil
	}
	for i := 0; i < n; i++ {
		c := binary.LittleEndian.Uint32(tables[4*i:])
		if i < l.chunks {
			l.dataCRC = append(l.dataCRC, c)
		} else {
			l.parCRC = append(l.parCRC, c)
		}
	}
	return l
}

func (l *recLayout) tablesSize() int64 {
	return recHeaderSize + 4*int64(l.chunks+l.groups*l.parity)
}

// readShard reads data shard i (zero-padded) from the protected area.
func (l *recLayout) readShard(r io.ReaderAt, i int, buf []byte) error {
	for k := range buf {
		buf[k] = 0
	}
	off := uint64(i) * l.shard
	n := min(l.shard, l.mainSize-off)
	_, err := r.ReadAt(buf[:n], int64(off))
	if err == io.EOF {
		err = nil
	}
	return err
}

// appendRecovery computes parity for f[0:mainSize] and appends the section.
func appendRecovery(f *os.File, mainSize uint64, pct int) error {
	l := newRecLayout(mainSize, pct)
	enc, err := reedsolomon.New(l.data, l.parity)
	if err != nil {
		return err
	}
	l.dataCRC = make([]uint32, l.chunks)
	parity := make([][]byte, l.groups*l.parity)
	for g := 0; g < l.groups; g++ {
		shards := make([][]byte, l.data+l.parity)
		for j := 0; j < l.data; j++ {
			shards[j] = make([]byte, l.shard)
			if i := j*l.groups + g; i < l.chunks {
				if err := l.readShard(f, i, shards[j]); err != nil {
					return err
				}
				l.dataCRC[i] = crc32.ChecksumIEEE(shards[j])
			}
		}
		for j := 0; j < l.parity; j++ {
			shards[l.data+j] = make([]byte, l.shard)
		}
		if err := enc.Encode(shards); err != nil {
			return err
		}
		for j := 0; j < l.parity; j++ {
			parity[g*l.parity+j] = shards[l.data+j]
		}
	}
	for _, p := range parity {
		l.parCRC = append(l.parCRC, crc32.ChecksumIEEE(p))
	}
	if _, err := f.Seek(int64(mainSize), io.SeekStart); err != nil {
		return err
	}
	ht := l.headerAndTables()
	var buf bytes.Buffer
	buf.Write(ht)
	for _, p := range parity {
		buf.Write(p)
	}
	buf.Write(ht) // copy, in case the first one is damaged
	t := make([]byte, recTrailerSize)
	copy(t, recTrailerMagic)
	binary.LittleEndian.PutUint64(t[8:], mainSize)
	binary.LittleEndian.PutUint64(t[16:], uint64(buf.Len()+recTrailerSize))
	binary.LittleEndian.PutUint32(t[24:], crc32.ChecksumIEEE(t[:24]))
	buf.Write(t)
	_, err = f.Write(buf.Bytes())
	return err
}

// findRecovery locates the recovery section of a file of the given size.
// Returns the layout and the offset of the section, or nil.
func findRecovery(r io.ReaderAt, size int64) (*recLayout, int64) {
	if size > recTrailerSize {
		t := make([]byte, recTrailerSize)
		if _, err := r.ReadAt(t, size-recTrailerSize); err == nil && bytes.Equal(t[:8], recTrailerMagic) &&
			crc32.ChecksumIEEE(t[:24]) == binary.LittleEndian.Uint32(t[24:]) {
			start := int64(binary.LittleEndian.Uint64(t[8:]))
			if l := parseRecHeader(r, start); l != nil {
				return l, start
			}
			// First copy damaged: try the copy before the trailer.
			total := int64(binary.LittleEndian.Uint64(t[16:]))
			for off := size - recTrailerSize - recHeaderSize; off > start && off > size-total-1; off-- {
				if l := parseRecHeader(r, off); l != nil {
					return l, int64(l.mainSize)
				}
				if size-recTrailerSize-off > 64<<20 {
					break
				}
			}
		}
	}
	// Trailer damaged: search for a header near the end of the file.
	buf := make([]byte, 1<<20)
	for end := size; end > 0; {
		start := max(0, end-int64(len(buf)))
		n, _ := r.ReadAt(buf[:end-start], start)
		for i := n - len(recMagic); i >= 0; i-- {
			if bytes.Equal(buf[i:i+len(recMagic)], recMagic) {
				if l := parseRecHeader(r, start+int64(i)); l != nil {
					return l, int64(l.mainSize)
				}
			}
		}
		if start == 0 || size-start > 256<<20 {
			break
		}
		end = start + int64(len(recMagic))
	}
	return nil, 0
}

// RepairResult reports what Repair did.
type RepairResult struct {
	Output   string `json:"output"`
	Damaged  int    `json:"damaged"`  // damaged shards found
	Repaired int    `json:"repaired"` // shards rebuilt
	Lost     int    `json:"lost"`     // shards that could not be rebuilt
	Shard    int    `json:"shard"`    // shard size in bytes
}

// ErrNoRecovery is returned by Repair for archives without a recovery record.
var ErrNoRecovery = errors.New("this archive has no recovery record, so it cannot be repaired")

// Repair checks a .nest archive against its recovery record and writes a
// repaired copy to out (default: "fixed.<name>" next to it).
func Repair(p, out string, progress ProgressFunc) (*RepairResult, error) {
	f, size, err := openArchiveFile(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	l, recStart := findRecovery(f, size)
	if l == nil {
		return nil, ErrNoRecovery
	}
	if out == "" {
		out = fixedName(p)
	}
	res := &RepairResult{Output: out, Shard: int(l.shard)}
	enc, err := reedsolomon.New(l.data, l.parity)
	if err != nil {
		return nil, err
	}
	o, err := os.Create(out + ".arcvault-tmp")
	if err != nil {
		return nil, err
	}
	tmp := o.Name()
	fail := func(err error) (*RepairResult, error) {
		o.Close()
		os.Remove(tmp)
		return nil, err
	}
	repaired := make(map[int][]byte)
	for g := 0; g < l.groups; g++ {
		if progress != nil && !progress("Checking", int64(g), int64(l.groups)) {
			return fail(ErrCancelled)
		}
		shards := make([][]byte, l.data+l.parity)
		var missing []int
		for j := 0; j < l.data; j++ {
			shards[j] = make([]byte, l.shard)
			i := j*l.groups + g
			if i >= l.chunks {
				continue // padding: known zeros
			}
			if err := l.readShard(f, i, shards[j]); err != nil || crc32.ChecksumIEEE(shards[j]) != l.dataCRC[i] {
				shards[j] = nil
				missing = append(missing, i)
			}
		}
		parityBad := 0
		for j := 0; j < l.parity; j++ {
			buf := make([]byte, l.shard)
			off := recStart + l.tablesSize() + int64(g*l.parity+j)*int64(l.shard)
			if _, err := f.ReadAt(buf, off); err != nil || crc32.ChecksumIEEE(buf) != l.parCRC[g*l.parity+j] {
				parityBad++
				continue
			}
			shards[l.data+j] = buf
		}
		res.Damaged += len(missing)
		if len(missing) == 0 {
			continue
		}
		if len(missing)+parityBad > l.parity {
			res.Lost += len(missing)
			continue
		}
		if err := enc.ReconstructData(shards); err != nil {
			res.Lost += len(missing)
			continue
		}
		for _, i := range missing {
			repaired[i] = shards[(i-g)/l.groups]
			res.Repaired++
		}
	}
	// Write the protected area with the rebuilt shards, then a fresh recovery record.
	buf := make([]byte, l.shard)
	for i := 0; i < l.chunks; i++ {
		data, ok := repaired[i]
		if !ok {
			if err := l.readShard(f, i, buf); err != nil {
				return fail(err)
			}
			data = buf
		}
		n := min(l.shard, l.mainSize-uint64(i)*l.shard)
		if _, err := o.Write(data[:n]); err != nil {
			return fail(err)
		}
	}
	if res.Damaged > 0 && res.Lost == 0 {
		if err := appendRecovery(o, l.mainSize, l.pct); err != nil {
			return fail(err)
		}
	} else if res.Damaged == 0 {
		o.Close()
		os.Remove(tmp)
		res.Output = ""
		return res, nil
	}
	if err := o.Close(); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp, out); err != nil {
		return nil, err
	}
	if res.Lost > 0 {
		return res, fmt.Errorf("%d part(s) of %d bytes could not be rebuilt: too much damage for the recovery record; "+
			"undamaged files can still be extracted from %s", res.Lost, l.shard, out)
	}
	return res, nil
}

func fixedName(p string) string {
	dir, base := splitDirBase(p)
	if m := volumeRe.FindStringSubmatch(base); m != nil {
		base = m[1]
	}
	return dir + "fixed." + base
}

func splitDirBase(p string) (string, string) {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i+1], p[i+1:]
		}
	}
	return "", p
}
