package core

import (
	"crypto/cipher"
	"crypto/rand"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// nestChunkSize is the uncompressed size of one block of the solid stream.
func nestChunkSize(level int) uint64 {
	if level >= 8 {
		return 16 << 20
	}
	return 8 << 20
}

func nestZstdLevel(level int) zstd.EncoderLevel {
	switch {
	case level <= 2:
		return zstd.SpeedFastest
	case level <= 7:
		return zstd.SpeedDefault
	}
	return zstd.SpeedBestCompression
}

type nestJob struct {
	n      uint64
	data   []byte
	out    []byte
	method byte
	err    error
	done   chan struct{}
}

// writeNest writes a complete .nest archive.
func writeNest(f *os.File, out string, srcs []source, total int64, opt CreateOptions) error {
	sink := &progressSink{total: total, fn: opt.Progress}
	h := &nestHeader{}
	var aead cipher.AEAD
	if opt.Password != "" {
		h.flags |= nestFlagEncrypted
		if opt.EncryptNames {
			h.flags |= nestFlagHiddenNames
		}
		if _, err := rand.Read(h.salt[:]); err != nil {
			return err
		}
		h.time, h.memMiB, h.threads = argonTime, argonMemMiB, argonThreads
		var err error
		if aead, err = nestAEAD(nestKey(opt.Password, h)); err != nil {
			return err
		}
	}
	if opt.Lock {
		h.flags |= nestFlagLocked
	}
	if opt.RecoveryPercent > 0 {
		h.flags |= nestFlagRecovery
	}
	if _, err := f.Write(h.marshal()); err != nil {
		return err
	}

	chunk := nestChunkSize(opt.Level)
	ix := &nestIndex{chunk: chunk, comment: opt.Comment}
	var dirs, files []source
	for _, s := range srcs {
		if s.info.IsDir() {
			dirs = append(dirs, s)
		} else {
			files = append(files, s)
		}
	}
	// Files of the same type compress better next to each other.
	sort.SliceStable(files, func(i, j int) bool {
		ei, ej := strings.ToLower(filepath.Ext(files[i].name)), strings.ToLower(filepath.Ext(files[j].name))
		if ei != ej {
			return ei < ej
		}
		return strings.ToLower(files[i].name) < strings.ToLower(files[j].name)
	})
	for _, d := range dirs {
		ix.files = append(ix.files, nestFile{name: d.name, dir: true, mtime: d.info.ModTime().UnixNano(), mode: uint32(d.info.Mode().Perm())})
	}

	// Pipeline: one reader fills chunks, workers compress/encrypt them, and this
	// goroutine writes them in order.
	workers := runtime.NumCPU()
	jobs := make(chan *nestJob)
	queue := make(chan *nestJob, workers)
	level := opt.Level
	encPool := sync.Pool{New: func() any {
		e, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(nestZstdLevel(level)), zstd.WithEncoderConcurrency(1),
			zstd.WithWindowSize(int(min(chunk, 8<<20))))
		return e
	}}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				j.method = nestMethodStore
				j.out = j.data
				if level > 0 {
					enc := encPool.Get().(*zstd.Encoder)
					if c := enc.EncodeAll(j.data, make([]byte, 0, len(j.data)/2)); len(c) < len(j.data) {
						j.out, j.method = c, nestMethodZstd
					}
					encPool.Put(enc)
				}
				if aead != nil {
					j.out, j.err = nestSeal(aead, j.out, nestAAD("block", j.n))
				}
				close(j.done)
			}
		}()
	}

	var readErr error
	go func() {
		defer close(queue)
		defer close(jobs)
		var n uint64
		var pos uint64
		buf := make([]byte, 0, chunk)
		submit := func() {
			j := &nestJob{n: n, data: buf, done: make(chan struct{})}
			n++
			queue <- j
			jobs <- j
			buf = make([]byte, 0, chunk)
		}
		for _, s := range files {
			src, err := os.Open(s.abs)
			if err != nil {
				readErr = err
				return
			}
			fe := nestFile{name: s.name, mtime: s.info.ModTime().UnixNano(), mode: uint32(s.info.Mode().Perm()), offset: pos}
			h := crc32.NewIEEE()
			r := &sinkReader{r: src, name: s.name, p: sink}
			for {
				if len(buf) == cap(buf) {
					submit()
				}
				k, err := r.Read(buf[len(buf):cap(buf)])
				if k > 0 {
					h.Write(buf[len(buf) : len(buf)+k])
					buf = buf[:len(buf)+k]
					pos += uint64(k)
					fe.size += uint64(k)
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					src.Close()
					readErr = err
					return
				}
			}
			src.Close()
			fe.crc = h.Sum32()
			ix.files = append(ix.files, fe)
		}
		if len(buf) > 0 {
			submit()
		}
	}()

	offset := uint64(nestHeaderSize)
	var writeErr error
	for j := range queue {
		<-j.done
		if writeErr != nil {
			continue // drain
		}
		if j.err != nil {
			writeErr = j.err
			continue
		}
		if _, err := f.Write(j.out); err != nil {
			writeErr = err
			continue
		}
		ix.blocks = append(ix.blocks, nestBlock{offset: offset, packed: uint64(len(j.out)), unpacked: uint64(len(j.data)),
			method: j.method, crc: crc32.ChecksumIEEE(j.out)})
		offset += uint64(len(j.out))
	}
	wg.Wait()
	if readErr != nil {
		if sink.stopped.Load() {
			return ErrCancelled
		}
		return readErr
	}
	if writeErr != nil {
		return writeErr
	}

	// Index and trailer.
	raw := ix.marshal()
	enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	stored := enc.EncodeAll(raw, nil)
	enc.Close()
	if h.flags&nestFlagHiddenNames != 0 {
		var err error
		if stored, err = nestSeal(aead, stored, nestAAD("index", 0)); err != nil {
			return err
		}
	}
	if _, err := f.Write(stored); err != nil {
		return err
	}
	tr := nestTrailer{indexOffset: offset, indexSize: uint64(len(stored)), indexRaw: uint64(len(raw))}
	if _, err := f.Write(tr.marshal()); err != nil {
		return err
	}
	if opt.RecoveryPercent > 0 {
		mainSize := offset + uint64(len(stored)) + nestTrailerSize
		return appendRecovery(f, mainSize, opt.RecoveryPercent)
	}
	return nil
}
