package ui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"zipnest/core"
)

// task is a long-running operation the page polls.
type task struct {
	id, kind string
	cancel   atomic.Bool
	started  time.Time

	mu       sync.Mutex
	state    string // running, done, error, cancelled
	stage    string
	file     string
	done     int64
	total    int64
	result   any
	err      error
	finished time.Time
}

func (t *task) progress(file string, done, total int64) bool {
	t.mu.Lock()
	t.file, t.done, t.total = file, done, total
	t.mu.Unlock()
	return !t.cancel.Load()
}

func (t *task) setStage(s string) {
	t.mu.Lock()
	t.stage, t.done, t.total, t.file = s, 0, 0, ""
	t.mu.Unlock()
}

type taskReq struct {
	Kind     string   `json:"kind"`
	Archive  string   `json:"archive"`
	Archives []string `json:"archives"`
	Password string   `json:"password"`

	// extract
	Dest      string   `json:"dest"`
	Select    []string `json:"select"`
	BaseDir   string   `json:"baseDir"`
	Overwrite string   `json:"overwrite"`
	Separate  bool     `json:"separate"` // each archive into its own subfolder

	// create / add / convert
	Output       string   `json:"output"`
	Inputs       []string `json:"inputs"`
	Format       string   `json:"format"`
	Level        int      `json:"level"`
	NewPassword  string   `json:"newPassword"`
	EncryptNames bool     `json:"encryptNames"`
	Volume       int64    `json:"volume"`
	SFX          bool     `json:"sfx"`
	Recovery     int      `json:"recovery"`
	Lock         bool     `json:"lock"`
	Comment      string   `json:"comment"`
	Exclude      []string `json:"exclude"`
	DeleteAfter  bool     `json:"deleteAfter"`
	TestAfter    bool     `json:"testAfter"`
	Mode         string   `json:"mode"` // create: "add" to an existing archive, "replace" it

	// delete / rename
	Paths []string `json:"paths"`
	Path  string   `json:"path"`
	Name  string   `json:"name"`

	// find
	Root     string `json:"root"`
	Pattern  string `json:"pattern"`
	Text     string `json:"text"`
	InArch   bool   `json:"inArchives"`
	MaxFound int    `json:"max"`
}

func (s *Server) apiTaskStart(body []byte) (any, error) {
	req, err := decode[taskReq](body)
	if err != nil {
		return nil, err
	}
	run, ok := taskKinds[req.Kind]
	if !ok {
		return nil, fmt.Errorf("unknown task %q", req.Kind)
	}
	t := &task{id: randHex(6), kind: req.Kind, state: "running", started: time.Now()}
	s.mu.Lock()
	for id, old := range s.tasks { // forget finished tasks after a while
		if old.state != "running" && time.Since(old.finished) > 10*time.Minute {
			delete(s.tasks, id)
		}
	}
	s.tasks[t.id] = t
	s.mu.Unlock()
	go func() {
		var res any
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("internal error: %v", r)
				}
			}()
			res, err = run(s, t, req)
		}()
		for _, a := range append(req.Archives, req.Archive, req.Output) {
			if a != "" {
				s.forget(a)
			}
		}
		t.mu.Lock()
		t.result, t.err, t.finished = res, err, time.Now()
		switch {
		case err == nil:
			t.state = "done"
		case errors.Is(err, core.ErrCancelled) || t.cancel.Load():
			t.state = "cancelled"
		default:
			t.state = "error"
		}
		t.mu.Unlock()
	}()
	return map[string]any{"id": t.id}, nil
}

func (s *Server) getTask(body []byte) (*task, error) {
	v, err := decode[struct{ ID string }](body)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	t := s.tasks[v.ID]
	s.mu.Unlock()
	if t == nil {
		return nil, errors.New("unknown task")
	}
	return t, nil
}

func (s *Server) apiTaskStatus(body []byte) (any, error) {
	t, err := s.getTask(body)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	res := map[string]any{"id": t.id, "kind": t.kind, "state": t.state, "stage": t.stage, "file": t.file,
		"done": t.done, "total": t.total, "elapsed": time.Since(t.started).Milliseconds(), "result": t.result}
	if t.err != nil {
		res["error"], res["code"] = errorText(t.err), errorCode(t.err)
	}
	return res, nil
}

func (s *Server) apiTaskCancel(body []byte) (any, error) {
	t, err := s.getTask(body)
	if err != nil {
		return nil, err
	}
	t.cancel.Store(true)
	return nil, nil
}

type taskFunc func(s *Server, t *task, r taskReq) (any, error)

var taskKinds = map[string]taskFunc{}

func init() {
	taskKinds = map[string]taskFunc{
		"extract":   (*Server).taskExtract,
		"create":    (*Server).taskCreate,
		"add":       (*Server).taskAdd,
		"delete":    (*Server).taskDelete,
		"rename":    (*Server).taskRename,
		"test":      (*Server).taskTest,
		"convert":   (*Server).taskConvert,
		"repair":    (*Server).taskRepair,
		"comment":   (*Server).taskComment,
		"benchmark": (*Server).taskBenchmark,
		"find":      (*Server).taskFind,
	}
}

func archivesOf(r taskReq) []string {
	if len(r.Archives) > 0 {
		return r.Archives
	}
	if r.Archive != "" {
		return []string{r.Archive}
	}
	return nil
}

func (s *Server) taskExtract(t *task, r taskReq) (any, error) {
	list := archivesOf(r)
	if len(list) == 0 {
		return nil, errors.New("no archive selected")
	}
	total := &core.ExtractResult{}
	var errs []string
	for i, a := range list {
		dest := r.Dest
		if r.Separate || (len(list) > 1 && dest == "") {
			if dest == "" {
				dest = filepath.Dir(a)
			}
			dest = filepath.Join(dest, core.ArchiveStem(a))
		}
		if len(list) > 1 {
			t.setStage(fmt.Sprintf("%d / %d: %s", i+1, len(list), filepath.Base(a)))
		}
		res, err := core.Extract(a, dest, core.ExtractOptions{Password: r.Password, Select: r.Select, BaseDir: r.BaseDir,
			Overwrite: r.Overwrite, Progress: t.progress})
		if res != nil {
			total.Files += res.Files
			total.Dirs += res.Dirs
			total.Bytes += res.Bytes
			total.Skipped = append(total.Skipped, res.Skipped...)
			total.Outputs = append(total.Outputs, res.Outputs...)
		}
		if err != nil {
			if len(list) == 1 || errors.Is(err, core.ErrCancelled) || errors.Is(err, core.ErrPasswordRequired) || errors.Is(err, core.ErrBadPassword) {
				return total, err
			}
			errs = append(errs, filepath.Base(a)+": "+errorText(err))
		}
	}
	if len(errs) > 0 {
		return total, errors.New(strings.Join(errs, "\n"))
	}
	return total, nil
}

func (r taskReq) createOptions(stub []byte) core.CreateOptions {
	co := core.CreateOptions{Format: core.Format(r.Format), Level: r.Level, Password: r.NewPassword,
		EncryptNames: r.EncryptNames, VolumeSize: r.Volume, RecoveryPercent: r.Recovery, Lock: r.Lock,
		Comment: r.Comment, Exclude: r.Exclude, BaseDir: r.BaseDir}
	if co.Password == "" {
		co.Password = r.Password
	}
	if r.SFX {
		co.SFXStub = stub
	}
	return co
}

func (s *Server) taskCreate(t *task, r taskReq) (any, error) {
	if r.Output == "" || len(r.Inputs) == 0 {
		return nil, errors.New("choose an archive name and files to add")
	}
	if r.SFX && len(s.opt.SFXStub) == 0 {
		return nil, errors.New("self-extracting archives can only be created on Windows")
	}
	out, err := filepath.Abs(r.Output)
	if err != nil {
		return nil, err
	}
	co := r.createOptions(s.opt.SFXStub)
	co.Progress = t.progress
	if co.Format == "" {
		co.Format = core.FormatFromName(out)
	}
	if st, err := os.Stat(out); err == nil && !st.IsDir() {
		switch r.Mode {
		case "replace":
			if err := os.Remove(out); err != nil {
				return nil, err
			}
		case "add":
			if co.Format != core.FormatZip || len(co.SFXStub) > 0 || co.VolumeSize > 0 {
				t.setStage("Adding")
				err := core.Update(out, r.Inputs, r.BaseDir, r.Exclude, core.EditOptions{Password: co.Password, Level: co.Level, Progress: t.progress})
				if err != nil {
					return nil, err
				}
				return s.afterCreate(t, r, out, co.Password)
			}
		default:
			return nil, fmt.Errorf("%s already exists", filepath.Base(out))
		}
	}
	t.setStage("Compressing")
	if err := core.Create(out, r.Inputs, co); err != nil {
		return nil, err
	}
	return s.afterCreate(t, r, out, co.Password)
}

func (s *Server) afterCreate(t *task, r taskReq, out, password string) (any, error) {
	res := map[string]any{"path": out}
	if st, err := os.Stat(out); err == nil {
		res["size"] = st.Size()
	} else if r.Volume > 0 {
		var sz int64
		matches, _ := filepath.Glob(out + ".*")
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil {
				sz += st.Size()
			}
		}
		res["size"] = sz
		if len(matches) > 0 {
			res["path"] = matches[0]
			out = matches[0]
		}
	}
	if r.TestAfter {
		t.setStage("Testing")
		tr, err := core.Test(out, password, t.progress)
		if err != nil {
			return res, fmt.Errorf("the archive was created but the test failed: %w", err)
		}
		if len(tr.Errors) > 0 {
			return res, errors.New("the archive was created but the test found errors:\n" + strings.Join(tr.Errors, "\n"))
		}
		res["tested"] = true
	}
	if r.DeleteAfter {
		t.setStage("Deleting files")
		if err := s.host.Trash(r.Inputs); err != nil {
			return res, fmt.Errorf("the archive was created but some files could not be deleted: %w", err)
		}
		res["deleted"] = len(r.Inputs)
	}
	return res, nil
}

func (s *Server) taskAdd(t *task, r taskReq) (any, error) {
	if len(r.Inputs) == 0 {
		return nil, errors.New("nothing to add")
	}
	err := core.Update(r.Archive, r.Inputs, r.BaseDir, r.Exclude, core.EditOptions{Password: r.Password, Level: r.Level, Progress: t.progress})
	return map[string]any{"added": len(r.Inputs)}, err
}

func (s *Server) taskDelete(t *task, r taskReq) (any, error) {
	n, err := core.DeleteFrom(r.Archive, r.Paths, core.EditOptions{Password: r.Password, Progress: t.progress})
	return map[string]any{"deleted": n}, err
}

func (s *Server) taskRename(t *task, r taskReq) (any, error) {
	return nil, core.Rename(r.Archive, r.Path, r.Name, core.EditOptions{Password: r.Password, Progress: t.progress})
}

func (s *Server) taskComment(t *task, r taskReq) (any, error) {
	return nil, core.SetComment(r.Archive, r.Comment, core.EditOptions{Password: r.Password, Progress: t.progress})
}

func (s *Server) taskTest(t *task, r taskReq) (any, error) {
	list := archivesOf(r)
	if len(list) == 0 {
		return nil, errors.New("no archive selected")
	}
	total := &core.TestResult{}
	for i, a := range list {
		if len(list) > 1 {
			t.setStage(fmt.Sprintf("%d / %d: %s", i+1, len(list), filepath.Base(a)))
		}
		res, err := core.Test(a, r.Password, t.progress)
		if res != nil {
			total.Files += res.Files
			total.Bytes += res.Bytes
			for _, e := range res.Errors {
				if len(list) > 1 {
					e = filepath.Base(a) + ": " + e
				}
				total.Errors = append(total.Errors, e)
			}
		}
		if err != nil {
			if len(list) == 1 || errors.Is(err, core.ErrCancelled) || errors.Is(err, core.ErrPasswordRequired) {
				return total, err
			}
			total.Errors = append(total.Errors, filepath.Base(a)+": "+errorText(err))
		}
	}
	return total, nil
}

func (s *Server) taskConvert(t *task, r taskReq) (any, error) {
	list := archivesOf(r)
	if len(list) == 0 {
		return nil, errors.New("no archive selected")
	}
	var outs []string
	for i, a := range list {
		out := r.Output
		if out == "" || len(list) > 1 {
			dir := filepath.Dir(a)
			if r.Output != "" && len(list) > 1 {
				dir = r.Output
			}
			out = filepath.Join(dir, core.ArchiveStem(a)+"."+r.Format)
			if abs, _ := filepath.Abs(a); abs == out {
				out = filepath.Join(dir, core.ArchiveStem(a)+" (converted)."+r.Format)
			}
		}
		if len(list) > 1 {
			t.setStage(fmt.Sprintf("%d / %d: %s", i+1, len(list), filepath.Base(a)))
		}
		co := r.createOptions(s.opt.SFXStub)
		co.Password = r.NewPassword
		co.Progress = t.progress
		if err := core.Convert(a, out, core.ConvertOptions{Password: r.Password, Create: co}); err != nil {
			return map[string]any{"outputs": outs}, fmt.Errorf("%s: %w", filepath.Base(a), err)
		}
		outs = append(outs, out)
		if r.DeleteAfter {
			s.host.Trash([]string{a})
		}
	}
	return map[string]any{"outputs": outs}, nil
}

func (s *Server) taskRepair(t *task, r taskReq) (any, error) {
	t.setStage("Repairing")
	return core.Repair(r.Archive, "", t.progress)
}

func (s *Server) taskBenchmark(t *task, r taskReq) (any, error) {
	dir := s.opt.TempDir
	os.MkdirAll(dir, 0o755)
	return core.Benchmark(dir, t.progress)
}

// ---------------------------------------------------------------- find

type found struct {
	Archive string `json:"archive,omitempty"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	Mtime   int64  `json:"mtime"`
}

func nameMatcher(pattern string) func(string) bool {
	var pats []string
	for _, p := range strings.FieldsFunc(pattern, func(r rune) bool { return r == ';' || r == ',' }) {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if !strings.ContainsAny(p, "*?[") {
			p = "*" + p + "*"
		}
		pats = append(pats, p)
	}
	return func(name string) bool {
		if len(pats) == 0 {
			return true
		}
		n := strings.ToLower(name)
		for _, p := range pats {
			if ok, _ := path.Match(p, n); ok {
				return true
			}
		}
		return false
	}
}

func containsFold(r io.Reader, needle []byte) bool {
	buf := make([]byte, 256<<10)
	keep := len(needle) - 1
	var carry []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := bytes.ToLower(append(carry, buf[:n]...))
			if bytes.Contains(chunk, needle) {
				return true
			}
			if keep > 0 && len(chunk) > keep {
				carry = append([]byte(nil), chunk[len(chunk)-keep:]...)
			}
		}
		if err != nil {
			return false
		}
	}
}

func (s *Server) taskFind(t *task, r taskReq) (any, error) {
	match := nameMatcher(r.Pattern)
	needle := []byte(strings.ToLower(r.Text))
	max := r.MaxFound
	if max <= 0 {
		max = 2000
	}
	var out []found
	full := func() bool { return len(out) >= max }
	searchArchive := func(a string) error {
		ents, _, err := core.List(a, r.Password)
		if err != nil {
			return err
		}
		var hits []found
		for _, e := range ents {
			if t.cancel.Load() {
				return core.ErrCancelled
			}
			if match(path.Base(e.Path)) && (len(needle) == 0 || !e.Dir) {
				if r.Archive != "" && r.BaseDir != "" && !strings.HasPrefix(e.Path, r.BaseDir+"/") {
					continue
				}
				hits = append(hits, found{Archive: a, Path: e.Path, Name: path.Base(e.Path), Dir: e.Dir, Size: e.Size, Mtime: e.Modified})
			}
		}
		if len(needle) > 0 && len(hits) > 0 {
			want := map[string]bool{}
			for _, h := range hits {
				want[h.Path] = true
			}
			var keep []found
			err := core.Walk(a, r.Password, func(e core.Entry, open func() (io.ReadCloser, error)) error {
				if t.cancel.Load() {
					return core.ErrCancelled
				}
				if !want[e.Path] {
					return nil
				}
				t.progress(e.Path, 0, 0)
				rc, err := open()
				if err != nil {
					return err
				}
				ok := containsFold(rc, needle)
				rc.Close()
				if ok {
					for _, h := range hits {
						if h.Path == e.Path {
							keep = append(keep, h)
							break
						}
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			hits = keep
		}
		out = append(out, hits...)
		return nil
	}
	if r.Archive != "" {
		err := searchArchive(r.Archive)
		return map[string]any{"results": out}, err
	}
	root := r.Root
	if root == "" {
		return nil, errors.New("choose where to search")
	}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if t.cancel.Load() {
			return core.ErrCancelled
		}
		if full() {
			return filepath.SkipAll
		}
		if p == root {
			return nil
		}
		t.progress(p, int64(len(out)), 0)
		name := d.Name()
		if !d.IsDir() && r.InArch && core.IsArchiveName(name) {
			searchArchive(p)
		}
		if !match(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if len(needle) > 0 {
			if d.IsDir() {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			ok := containsFold(f, needle)
			f.Close()
			if !ok {
				return nil
			}
		}
		out = append(out, found{Path: p, Name: name, Dir: d.IsDir(), Size: info.Size(), Mtime: info.ModTime().UnixMilli()})
		return nil
	})
	if len(out) > max {
		out = out[:max]
	}
	return map[string]any{"results": out, "truncated": full()}, err
}
