package core

// 7z archive writer (written from the public 7z format description, 7zFormat.txt).
//
//   - LZMA2 compression ("Copy" for level 0)
//   - files are grouped into solid blocks that are compressed in parallel on all
//     CPU cores (one 7z "folder" per block)
//   - optional AES-256 encryption (7zAES) of the data and, optionally, of the
//     header so that even the file names are hidden

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/ulikunitz/xz/lzma"
)

const (
	k7zEnd              = 0x00
	k7zHeader           = 0x01
	k7zMainStreamsInfo  = 0x04
	k7zFilesInfo        = 0x05
	k7zPackInfo         = 0x06
	k7zUnpackInfo       = 0x07
	k7zSubStreamsInfo   = 0x08
	k7zSize             = 0x09
	k7zCRC              = 0x0A
	k7zFolder           = 0x0B
	k7zCodersUnpackSize = 0x0C
	k7zNumUnpackStream  = 0x0D
	k7zEmptyStream      = 0x0E
	k7zEmptyFile        = 0x0F
	k7zName             = 0x11
	k7zMTime            = 0x14
	k7zWinAttributes    = 0x15
	k7zEncodedHeader    = 0x17
	k7zDummy            = 0x19

	aesCyclesPower = 19 // 2^19 SHA-256 rounds, as 7-Zip uses
)

// ---------------------------------------------------------------- encoding helpers

type hdrBuf struct{ bytes.Buffer }

func (b *hdrBuf) num(v uint64) {
	first := byte(0)
	mask := byte(0x80)
	i := 0
	for ; i < 8; i++ {
		if v < uint64(1)<<(7*(uint(i)+1)) {
			first |= byte(v >> (8 * uint(i)))
			break
		}
		first |= mask
		mask >>= 1
	}
	b.WriteByte(first)
	for ; i > 0; i-- {
		b.WriteByte(byte(v))
		v >>= 8
	}
}

func (b *hdrBuf) u32(v uint32) {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], v)
	b.Write(t[:])
}

func (b *hdrBuf) u64(v uint64) {
	var t [8]byte
	binary.LittleEndian.PutUint64(t[:], v)
	b.Write(t[:])
}

func (b *hdrBuf) bits(v []bool) {
	var cur byte
	for i, on := range v {
		if on {
			cur |= 0x80 >> uint(i%8)
		}
		if i%8 == 7 {
			b.WriteByte(cur)
			cur = 0
		}
	}
	if len(v)%8 != 0 {
		b.WriteByte(cur)
	}
}

// property writes "id, size, data".
func (b *hdrBuf) property(id byte, data []byte) {
	b.WriteByte(id)
	b.num(uint64(len(data)))
	b.Write(data)
}

// lzma2DictProp returns the LZMA2 dictionary-size property byte for at least size.
func lzma2DictProp(size int) byte {
	for p := byte(0); p < 40; p++ {
		d := (2 | int(p&1)) << (p/2 + 11)
		if d >= size {
			return p
		}
	}
	return 40
}

func lzma2DictSize(level int) int {
	switch {
	case level <= 1:
		return 1 << 20
	case level <= 3:
		return 4 << 20
	case level <= 5:
		return 16 << 20
	case level <= 7:
		return 32 << 20
	}
	return 64 << 20
}

// ---------------------------------------------------------------- 7zAES

type sevenZipKey struct {
	key []byte
	iv  []byte
}

func newSevenZipKey(password string) (*sevenZipKey, error) {
	iv := make([]byte, 16)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	pw := utf16.Encode([]rune(password))
	pwb := make([]byte, 2*len(pw))
	for i, c := range pw {
		binary.LittleEndian.PutUint16(pwb[2*i:], c)
	}
	h := sha256.New()
	var ctr [8]byte
	for round := uint64(0); round < 1<<aesCyclesPower; round++ {
		h.Write(pwb)
		binary.LittleEndian.PutUint64(ctr[:], round)
		h.Write(ctr[:])
	}
	return &sevenZipKey{key: h.Sum(nil), iv: iv}, nil
}

// props are the 7zAES coder properties (no salt, 16-byte IV).
func (k *sevenZipKey) props() []byte {
	p := []byte{aesCyclesPower | 0x40, 0x0F}
	return append(p, k.iv...)
}

// encrypt AES-256-CBC encrypts data padded with zeros to the block size.
func (k *sevenZipKey) encrypt(data []byte) []byte {
	n := (len(data) + 15) / 16 * 16
	out := make([]byte, n)
	copy(out, data)
	block, _ := aes.NewCipher(k.key)
	cipher.NewCBCEncrypter(block, k.iv).CryptBlocks(out, out)
	return out
}

// ---------------------------------------------------------------- folders

type szFile struct {
	src   source
	empty bool // directory or zero-length file
	crc   uint32
}

type szFolder struct {
	files    []int  // indexes into the file list (non-empty files, in order)
	unpack   uint64 // total uncompressed bytes
	packed   []byte // compressed (and maybe encrypted) bytes when held in memory
	tmp      *os.File
	packSize uint64
	coderOut uint64 // size of the LZMA2 output (input of AES) when encrypted
	crc      uint32 // CRC of the whole unpacked folder
	err      error
}

func (f *szFolder) cleanup() {
	if f.tmp != nil {
		name := f.tmp.Name()
		f.tmp.Close()
		os.Remove(name)
		f.tmp = nil
	}
}

func (f *szFolder) reader() (io.Reader, error) {
	if f.tmp != nil {
		if _, err := f.tmp.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		return f.tmp, nil
	}
	return bytes.NewReader(f.packed), nil
}

// compressBlock compresses the files of one folder (solid block).
func compressBlock(files []szFile, fold *szFolder, level int, key *sevenZipKey, tmpDir string, sink *progressSink) {
	var out io.Writer
	var mem bytes.Buffer
	if fold.unpack > 32<<20 || key != nil && fold.unpack > 8<<20 {
		f, err := os.CreateTemp(tmpDir, ".zipnest-7z-*")
		if err != nil {
			fold.err = err
			return
		}
		fold.tmp = f
		out = f
	} else {
		out = &mem
	}
	cnt := &countWriter{w: out}
	var enc io.WriteCloser = nopWriteCloser{cnt}
	if level > 0 {
		// HashTable4: the binary-tree matcher of this encoder is far too slow.
		cfg := lzma.Writer2Config{
			DictCap: min(lzma2DictSize(level), max(int(fold.unpack), 1<<16)),
			Matcher: lzma.HashTable4,
		}
		w, err := cfg.NewWriter2(cnt)
		if err != nil {
			fold.err = err
			return
		}
		enc = w
	}
	folderCRC := crc32.NewIEEE()
	buf := make([]byte, 256<<10)
	for _, i := range fold.files {
		f := &files[i]
		src, err := os.Open(f.src.abs)
		if err != nil {
			fold.err = err
			return
		}
		h := crc32.NewIEEE()
		_, err = io.CopyBuffer(io.MultiWriter(enc, h, folderCRC), &sinkReader{r: src, name: f.src.name, p: sink}, buf)
		src.Close()
		if err != nil {
			fold.err = err
			return
		}
		f.crc = h.Sum32()
	}
	if err := enc.Close(); err != nil {
		fold.err = err
		return
	}
	fold.crc = folderCRC.Sum32()
	fold.coderOut = uint64(cnt.n)
	fold.packSize = uint64(cnt.n)
	if key == nil {
		fold.packed = mem.Bytes()
		return
	}
	// Encrypt (7zAES works on whole blocks; the decoder knows the real size).
	var plain []byte
	if fold.tmp != nil {
		if _, err := fold.tmp.Seek(0, io.SeekStart); err != nil {
			fold.err = err
			return
		}
		var err error
		if plain, err = io.ReadAll(fold.tmp); err != nil {
			fold.err = err
			return
		}
		fold.tmp.Truncate(0)
		fold.tmp.Seek(0, io.SeekStart)
		enc := key.encrypt(plain)
		if _, err := fold.tmp.Write(enc); err != nil {
			fold.err = err
			return
		}
		fold.packSize = uint64(len(enc))
		return
	}
	fold.packed = key.encrypt(mem.Bytes())
	fold.packSize = uint64(len(fold.packed))
}

// writeFolderInfo writes one folder's coder description.
func writeFolder(b *hdrBuf, level int, key *sevenZipKey) {
	main := func() {
		if level == 0 {
			b.WriteByte(0x01) // id size 1, simple, no properties
			b.WriteByte(0x00) // Copy
			return
		}
		b.WriteByte(0x21) // id size 1, has properties
		b.WriteByte(0x21) // LZMA2
		b.num(1)
		b.WriteByte(lzma2DictProp(lzma2DictSize(level)))
	}
	if key == nil {
		b.num(1)
		main()
		return
	}
	// Coders in data-flow order: 7zAES (reads the packed stream), then the
	// decompressor, whose input is bound to the AES output.
	b.num(2)
	props := key.props()
	b.WriteByte(0x24) // id size 4, has properties
	b.Write([]byte{0x06, 0xF1, 0x07, 0x01})
	b.num(uint64(len(props)))
	b.Write(props)
	main()
	// Bind pair: input stream 1 (decompressor) <- output stream 0 (AES).
	b.num(1)
	b.num(0)
}

func writeStreamsInfo(b *hdrBuf, folders []*szFolder, level int, key *sevenZipKey, subStreams func(*hdrBuf)) {
	b.WriteByte(k7zPackInfo)
	b.num(0) // pack position
	b.num(uint64(len(folders)))
	b.WriteByte(k7zSize)
	for _, f := range folders {
		b.num(f.packSize)
	}
	b.WriteByte(k7zEnd)

	b.WriteByte(k7zUnpackInfo)
	b.WriteByte(k7zFolder)
	b.num(uint64(len(folders)))
	b.WriteByte(0) // not external
	for range folders {
		writeFolder(b, level, key)
	}
	b.WriteByte(k7zCodersUnpackSize)
	for _, f := range folders {
		if key != nil {
			b.num(f.coderOut) // AES output = compressed stream
		}
		b.num(f.unpack)
	}
	b.WriteByte(k7zCRC)
	b.WriteByte(1) // all defined
	for _, f := range folders {
		b.u32(f.crc)
	}
	b.WriteByte(k7zEnd)
	if subStreams != nil {
		subStreams(b)
	}
	b.WriteByte(k7zEnd)
}

func fileTime(t time.Time) uint64 {
	return uint64(t.UnixNano()/100) + 116444736000000000
}

// writeSevenZip writes a complete .7z archive.
func writeSevenZip(f *os.File, out string, srcs []source, total int64, opt CreateOptions) error {
	sink := &progressSink{total: total, fn: opt.Progress}
	var key *sevenZipKey
	if opt.Password != "" {
		var err error
		if key, err = newSevenZipKey(opt.Password); err != nil {
			return err
		}
	}
	files := make([]szFile, len(srcs))
	var folders []*szFolder
	var cur *szFolder
	for i, s := range srcs {
		files[i].src = s
		if s.info.IsDir() || s.info.Size() == 0 {
			files[i].empty = true
			continue
		}
		if cur == nil || cur.unpack >= solidBlockSize(opt.Level) {
			cur = &szFolder{}
			folders = append(folders, cur)
		}
		cur.files = append(cur.files, i)
		cur.unpack += uint64(s.info.Size())
	}

	// Compress the folders in parallel.
	workers := runtime.NumCPU()
	jobs := make(chan *szFolder)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fold := range jobs {
				compressBlock(files, fold, opt.Level, key, filepath.Dir(out), sink)
			}
		}()
	}
	for _, fold := range folders {
		jobs <- fold
	}
	close(jobs)
	wg.Wait()
	defer func() {
		for _, fold := range folders {
			fold.cleanup()
		}
	}()
	for _, fold := range folders {
		if fold.err != nil {
			if sink.stopped.Load() {
				return ErrCancelled
			}
			return fold.err
		}
	}

	// Signature header placeholder, then the packed streams.
	if _, err := f.Write(make([]byte, 32)); err != nil {
		return err
	}
	var packedTotal uint64
	for _, fold := range folders {
		r, err := fold.reader()
		if err != nil {
			return err
		}
		n, err := io.Copy(f, r)
		if err != nil {
			return err
		}
		packedTotal += uint64(n)
	}

	header := buildSevenZipHeader(files, folders, opt.Level, key)

	// Optionally hide the header (file names) behind encryption.
	if key != nil && opt.EncryptNames {
		hfold := &szFolder{unpack: uint64(len(header))}
		var comp bytes.Buffer
		w, err := lzma.Writer2Config{DictCap: 1 << 20}.NewWriter2(&comp)
		if err != nil {
			return err
		}
		w.Write(header)
		if err := w.Close(); err != nil {
			return err
		}
		hfold.crc = crc32.ChecksumIEEE(header)
		hfold.coderOut = uint64(comp.Len())
		hfold.packed = key.encrypt(comp.Bytes())
		hfold.packSize = uint64(len(hfold.packed))
		if _, err := f.Write(hfold.packed); err != nil {
			return err
		}
		var eb hdrBuf
		eb.WriteByte(k7zEncodedHeader)
		// The encoded-header stream starts after the data streams.
		eb.WriteByte(k7zPackInfo)
		eb.num(packedTotal)
		eb.num(1)
		eb.WriteByte(k7zSize)
		eb.num(hfold.packSize)
		eb.WriteByte(k7zEnd)
		eb.WriteByte(k7zUnpackInfo)
		eb.WriteByte(k7zFolder)
		eb.num(1)
		eb.WriteByte(0)
		writeFolder(&eb, 1, key) // LZMA2 (1 MiB dictionary) + AES
		eb.WriteByte(k7zCodersUnpackSize)
		eb.num(hfold.coderOut)
		eb.num(hfold.unpack)
		eb.WriteByte(k7zCRC)
		eb.WriteByte(1)
		eb.u32(hfold.crc)
		eb.WriteByte(k7zEnd)
		eb.WriteByte(k7zEnd)
		header = eb.Bytes()
		packedTotal += hfold.packSize
	}
	if _, err := f.Write(header); err != nil {
		return err
	}

	// Signature header.
	var sh [32]byte
	copy(sh[:], []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C, 0, 4})
	binary.LittleEndian.PutUint64(sh[12:], packedTotal)
	binary.LittleEndian.PutUint64(sh[20:], uint64(len(header)))
	binary.LittleEndian.PutUint32(sh[28:], crc32.ChecksumIEEE(header))
	binary.LittleEndian.PutUint32(sh[8:], crc32.ChecksumIEEE(sh[12:32]))
	if _, err := f.WriteAt(sh[:], 0); err != nil {
		return err
	}
	return nil
}

func buildSevenZipHeader(files []szFile, folders []*szFolder, level int, key *sevenZipKey) []byte {
	var b hdrBuf
	b.WriteByte(k7zHeader)
	if len(folders) > 0 {
		b.WriteByte(k7zMainStreamsInfo)
		writeStreamsInfo(&b, folders, level, key, func(b *hdrBuf) {
			b.WriteByte(k7zSubStreamsInfo)
			b.WriteByte(k7zNumUnpackStream)
			for _, fold := range folders {
				b.num(uint64(len(fold.files)))
			}
			b.WriteByte(k7zSize)
			for _, fold := range folders {
				for j, i := range fold.files {
					if j < len(fold.files)-1 {
						b.num(uint64(files[i].src.info.Size()))
					}
				}
			}
			b.WriteByte(k7zCRC)
			b.WriteByte(1)
			for _, fold := range folders {
				for _, i := range fold.files {
					b.u32(files[i].crc)
				}
			}
			b.WriteByte(k7zEnd)
		})
	}

	b.WriteByte(k7zFilesInfo)
	b.num(uint64(len(files)))
	anyEmpty := false
	emptyStream := make([]bool, len(files))
	var emptyFile []bool
	for i, f := range files {
		emptyStream[i] = f.empty
		if f.empty {
			anyEmpty = true
			emptyFile = append(emptyFile, !f.src.info.IsDir())
		}
	}
	if anyEmpty {
		var p hdrBuf
		p.bits(emptyStream)
		b.property(k7zEmptyStream, p.Bytes())
		var q hdrBuf
		q.bits(emptyFile)
		b.property(k7zEmptyFile, q.Bytes())
	}
	var names hdrBuf
	names.WriteByte(0) // not external
	for _, f := range files {
		for _, c := range utf16.Encode([]rune(f.src.name)) {
			names.WriteByte(byte(c))
			names.WriteByte(byte(c >> 8))
		}
		names.Write([]byte{0, 0})
	}
	b.property(k7zName, names.Bytes())
	var times hdrBuf
	times.WriteByte(1) // all defined
	times.WriteByte(0) // not external
	for _, f := range files {
		times.u64(fileTime(f.src.info.ModTime()))
	}
	b.property(k7zMTime, times.Bytes())
	var attrs hdrBuf
	attrs.WriteByte(1)
	attrs.WriteByte(0)
	for _, f := range files {
		a := uint32(0x20) // FILE_ATTRIBUTE_ARCHIVE
		if f.src.info.IsDir() {
			a = 0x10
		}
		// High 16 bits: Unix mode (FILE_ATTRIBUTE_UNIX_EXTENSION), as p7zip does.
		a |= 0x8000 | uint32(f.src.info.Mode().Perm())<<16
		if f.src.info.IsDir() {
			a |= 0o040000 << 16
		} else {
			a |= 0o100000 << 16
		}
		attrs.u32(a)
	}
	b.property(k7zWinAttributes, attrs.Bytes())
	b.WriteByte(k7zEnd)
	b.WriteByte(k7zEnd)
	return b.Bytes()
}

var errNoSevenZipUpdate = errors.New("adding to an existing 7z archive is not supported; create a new archive instead")

// solidBlockSize is how much data goes into one solid block. Smaller blocks let
// more CPU cores work in parallel; larger ones compress a little better.
func solidBlockSize(level int) uint64 {
	if level >= 7 {
		return 32 << 20
	}
	return 16 << 20
}
