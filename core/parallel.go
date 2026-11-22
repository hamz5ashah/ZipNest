package core

// Parallel ZIP compression. Files are compressed concurrently by a pool of
// workers (one per CPU core) into memory (small files) or temporary files, and
// then written to the archive in their original order with zip.Writer.CreateRaw.
// Very large files are compressed in a single stream when their turn comes.

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/klauspost/compress/flate"
)

const (
	parallelMemLimit  = 8 << 20 // compress into memory below this size
	parallelFileLimit = 2 << 30 // stream (single core) files above this size
)

// progressSink aggregates progress from several goroutines and serialises the
// calls to the user's ProgressFunc.
type progressSink struct {
	mu      sync.Mutex
	done    int64
	total   int64
	fn      ProgressFunc
	stopped atomic.Bool
}

func (p *progressSink) add(name string, n int64) bool {
	if p.stopped.Load() {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done += n
	if p.fn != nil && !p.fn(name, p.done, p.total) {
		p.stopped.Store(true)
		return false
	}
	return true
}

type sinkReader struct {
	r    io.Reader
	name string
	p    *progressSink
}

func (s *sinkReader) Read(b []byte) (int, error) {
	n, err := s.r.Read(b)
	if n > 0 && !s.p.add(s.name, int64(n)) {
		return n, ErrCancelled
	}
	return n, err
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n += int64(n)
	return n, err
}

// packed is one compressed file waiting to be written.
type packed struct {
	mem  *bytes.Buffer
	file *os.File
	size int64
	crc  uint32
	err  error
}

func (p *packed) reader() (io.Reader, error) {
	if p.file != nil {
		if _, err := p.file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		return p.file, nil
	}
	return bytes.NewReader(p.mem.Bytes()), nil
}

func (p *packed) cleanup() {
	if p.file != nil {
		name := p.file.Name()
		p.file.Close()
		os.Remove(name)
	}
}

func compressFile(s source, method uint16, level int, tmpDir string, sink *progressSink) packed {
	src, err := os.Open(s.abs)
	if err != nil {
		return packed{err: err}
	}
	defer src.Close()
	var out packed
	var dst io.Writer
	if s.info.Size() <= parallelMemLimit {
		out.mem = bytes.NewBuffer(make([]byte, 0, s.info.Size()/2+512))
		dst = out.mem
	} else {
		f, err := os.CreateTemp(tmpDir, ".zipnest-part-*")
		if err != nil {
			return packed{err: err}
		}
		out.file = f
		dst = f
	}
	cnt := &countWriter{w: dst}
	var cw io.WriteCloser = nopWriteCloser{cnt}
	if method == zip.Deflate {
		cw, _ = flate.NewWriter(cnt, level)
	}
	h := crc32.NewIEEE()
	buf := make([]byte, 256<<10)
	_, err = io.CopyBuffer(io.MultiWriter(cw, h), &sinkReader{r: src, name: s.name, p: sink}, buf)
	if err == nil {
		err = cw.Close()
	}
	if err != nil {
		out.cleanup()
		return packed{err: err}
	}
	out.size, out.crc = cnt.n, h.Sum32()
	return out
}

// addTimeExtra adds the "extended timestamp" field (CreateRaw does not).
func addTimeExtra(fh *zip.FileHeader) {
	if fh.Modified.IsZero() {
		return
	}
	b := make([]byte, 9)
	binary.LittleEndian.PutUint16(b[0:], 0x5455)
	binary.LittleEndian.PutUint16(b[2:], 5)
	b[4] = 1
	binary.LittleEndian.PutUint32(b[5:], uint32(fh.Modified.Unix()))
	fh.Extra = append(fh.Extra, b...)
}

func setSizes(fh *zip.FileHeader, comp, uncomp int64) {
	fh.CompressedSize64, fh.UncompressedSize64 = uint64(comp), uint64(uncomp)
	if fh.CompressedSize64 >= 1<<32-1 || fh.UncompressedSize64 >= 1<<32-1 {
		fh.CompressedSize, fh.UncompressedSize = 1<<32-1, 1<<32-1
		fh.ReaderVersion = 45
	} else {
		fh.CompressedSize, fh.UncompressedSize = uint32(comp), uint32(uncomp)
	}
}

// writePacked writes an already compressed file as a raw (optionally AES) entry.
func writePacked(zw *zip.Writer, s source, p *packed, opt CreateOptions, method uint16) error {
	r, err := p.reader()
	if err != nil {
		return err
	}
	fh := zipHeader(s.name, s.info)
	setLegacyTime(fh)
	addTimeExtra(fh)
	if opt.Password == "" {
		fh.Method = method
		fh.CRC32 = p.crc
		fh.ReaderVersion = 20
		setSizes(fh, p.size, s.info.Size())
		w, err := zw.CreateRaw(fh)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, r)
		return err
	}
	aw, err := newAESEntry(zw, fh, opt.Password, method)
	if err != nil {
		return err
	}
	if _, err := io.Copy(aw, r); err != nil {
		return err
	}
	if err := aw.Finish(); err != nil {
		return err
	}
	setSizes(fh, aw.Count(), s.info.Size())
	return nil
}

// streamEntry compresses and writes one file in a single pass (for huge files).
func streamEntry(zw *zip.Writer, s source, opt CreateOptions, method uint16, level int, sink *progressSink) error {
	src, err := os.Open(s.abs)
	if err != nil {
		return err
	}
	defer src.Close()
	in := &sinkReader{r: src, name: s.name, p: sink}
	buf := make([]byte, 256<<10)
	fh := zipHeader(s.name, s.info)
	if opt.Password == "" {
		fh.Method = method
		w, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		_, err = io.CopyBuffer(w, in, buf)
		return err
	}
	setLegacyTime(fh)
	aw, err := newAESEntry(zw, fh, opt.Password, method)
	if err != nil {
		return err
	}
	var cw io.WriteCloser = nopWriteCloser{aw}
	if method == zip.Deflate {
		cw, _ = flate.NewWriter(aw, level)
	}
	if _, err := io.CopyBuffer(cw, in, buf); err != nil {
		return err
	}
	if err := cw.Close(); err != nil {
		return err
	}
	if err := aw.Finish(); err != nil {
		return err
	}
	setSizes(fh, aw.Count(), s.info.Size())
	return nil
}

func writeZipEntries(zw *zip.Writer, out string, srcs []source, total int64, opt CreateOptions, method uint16, level int) error {
	sink := &progressSink{total: total, fn: opt.Progress}
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	parallel := func(s source) bool { return !s.info.IsDir() && s.info.Size() < parallelFileLimit }

	results := make([]chan packed, len(srcs))
	for i, s := range srcs {
		if parallel(s) {
			results[i] = make(chan packed, 1)
		}
	}
	jobs := make(chan int)
	sem := make(chan struct{}, 2*workers) // limits finished-but-unwritten files
	stop := make(chan struct{})
	tmpDir := filepath.Dir(out)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] <- compressFile(srcs[i], method, level, tmpDir, sink)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i, s := range srcs {
			if !parallel(s) {
				continue
			}
			select {
			case sem <- struct{}{}:
			case <-stop:
				return
			}
			select {
			case jobs <- i:
			case <-stop:
				return
			}
		}
	}()

	written := make([]bool, len(srcs))
	fail := func(err error) error {
		sink.stopped.Store(true)
		close(stop)
		wg.Wait()
		for i, ch := range results { // remove temp files of finished but unwritten entries
			if ch == nil || written[i] {
				continue
			}
			select {
			case p := <-ch:
				p.cleanup()
			default:
			}
		}
		if sink.stopped.Load() && err == nil {
			err = ErrCancelled
		}
		return err
	}

	for i, s := range srcs {
		switch {
		case s.info.IsDir():
			if _, err := zw.CreateHeader(zipHeader(s.name+"/", s.info)); err != nil {
				return fail(err)
			}
		case !parallel(s):
			if err := streamEntry(zw, s, opt, method, level, sink); err != nil {
				return fail(err)
			}
		default:
			p := <-results[i]
			written[i] = true
			<-sem
			if p.err != nil {
				return fail(p.err)
			}
			err := writePacked(zw, s, &p, opt, method)
			p.cleanup()
			if err != nil {
				return fail(err)
			}
		}
	}
	close(stop)
	wg.Wait()
	return nil
}
