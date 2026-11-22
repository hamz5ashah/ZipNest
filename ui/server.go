// Package ui is ZipNest's user interface: a small HTTP server on 127.0.0.1
// that serves the embedded web front-end (web/) and a JSON API backed by the
// archive engine. The same interface runs inside WebView2 on Windows, inside
// Android's WebView, and in any browser on other systems.
package ui

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"zipnest/core"
)

//go:embed web
var webFS embed.FS

// Place is a shortcut in the sidebar.
type Place struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Icon string `json:"icon"` // home, desktop, documents, downloads, pictures, music, videos, drive, sd
}

// Host connects the interface to the platform it runs on. Every method is
// optional: embed BaseHost and override what the platform supports.
type Host interface {
	Platform() string           // "windows", "android", "linux", "darwin"
	Open(path string) error     // open a file with its default program
	Reveal(path string) error   // show a file in the system file manager
	Trash(paths []string) error // move to the recycle bin (or delete)
	Places() []Place            // sidebar locations (nil = defaults)
	Exit(code int)              // close the app with an exit code
	// Integration returns the shell integration settings (nil if unsupported);
	// SetIntegration applies them.
	Integration() map[string]any
	SetIntegration(map[string]any) error
}

// BaseHost implements Host with portable defaults.
type BaseHost struct{}

func (BaseHost) Platform() string { return runtime.GOOS }
func (BaseHost) Open(string) error {
	return errors.New("opening files is not supported here")
}
func (BaseHost) Reveal(string) error { return errors.New("not supported") }
func (BaseHost) Trash(paths []string) error {
	var errs []error
	for _, p := range paths {
		errs = append(errs, os.RemoveAll(p))
	}
	return errors.Join(errs...)
}
func (BaseHost) Places() []Place                     { return nil }
func (BaseHost) Exit(code int)                       { os.Exit(code) }
func (BaseHost) Integration() map[string]any         { return nil }
func (BaseHost) SetIntegration(map[string]any) error { return errors.New("not supported") }

// Options configure a Server.
type Options struct {
	Host     Host
	Version  string
	DataDir  string // where preferences are stored ("" = user config dir)
	TempDir  string // where files opened from archives are extracted
	SFXStub  []byte // self-extractor module (nil = SFX not available)
	Start    string // file or folder to show first
	Action   string // optional start action: "extract", "add", "setup", "convert", "task:<kind>"
	Args     []string
	Insecure bool // allow access without the token (development only)
}

// Server is a running interface server.
type Server struct {
	opt   Options
	host  Host
	token string
	ln    net.Listener
	URL   string // full URL to open, including the access token

	mu       sync.Mutex
	prefs    map[string]any
	cache    map[string]*listing
	blobs    map[string]blob
	tasks    map[string]*task
	lastHB   time.Time
	exitCode int
}

type listing struct {
	key     string
	entries []core.Entry
	format  core.Format
	info    *core.ArchiveInfo
	used    time.Time
}

type blob struct {
	mime string
	data []byte
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Start listens on a random local port and serves the interface.
func Start(opt Options) (*Server, error) {
	if opt.Host == nil {
		opt.Host = BaseHost{}
	}
	if opt.DataDir == "" {
		d, err := os.UserConfigDir()
		if err != nil {
			d = os.TempDir()
		}
		opt.DataDir = filepath.Join(d, "ZipNest")
	}
	if opt.TempDir == "" {
		opt.TempDir = filepath.Join(os.TempDir(), "ZipNest")
	}
	os.MkdirAll(opt.DataDir, 0o755)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{opt: opt, host: opt.Host, token: randHex(16), ln: ln,
		cache: map[string]*listing{}, blobs: map[string]blob{}, tasks: map[string]*task{}, lastHB: time.Now()}
	s.loadPrefs()
	s.URL = "http://" + ln.Addr().String() + "/?t=" + s.token
	go http.Serve(ln, s)
	return s, nil
}

// Close stops the server.
func (s *Server) Close() { s.ln.Close() }

// Addr is the listening address (host:port).
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Token is the access token.
func (s *Server) Token() string { return s.token }

// SinceHeartbeat reports how long ago the page last checked in.
func (s *Server) SinceHeartbeat() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastHB)
}

// ExitCode is the code the page asked the app to exit with (setup dialog).
func (s *Server) ExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

// ---------------------------------------------------------------- HTTP plumbing

func (s *Server) authorized(r *http.Request) bool {
	if s.opt.Insecure {
		return true
	}
	if c, err := r.Cookie("zn"); err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.token)) == 1 {
		return true
	}
	if t := r.Header.Get("X-ZipNest-Token"); t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1 {
		return true
	}
	return false
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only answer requests addressed to this server (no DNS rebinding).
	if h := r.Host; h != s.ln.Addr().String() && !strings.HasPrefix(h, "localhost:") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.URL.Path == "/" {
		if t := r.URL.Query().Get("t"); t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1 {
			http.SetCookie(w, &http.Cookie{Name: "zn", Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			q := r.URL.Query()
			q.Del("t")
			target := "/app"
			if e := q.Encode(); e != "" {
				target += "?" + e
			}
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
	}
	if !s.authorized(r) {
		http.Error(w, "ZipNest: this page can only be opened from the ZipNest app.", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == "/" || r.URL.Path == "/app":
		s.serveStatic(w, r, "web/index.html")
	case strings.HasPrefix(r.URL.Path, "/assets/"):
		s.serveStatic(w, r, "web/"+strings.TrimPrefix(r.URL.Path, "/assets/"))
	case strings.HasPrefix(r.URL.Path, "/blob/"):
		s.serveBlob(w, r)
	case r.URL.Path == "/api/upload":
		s.handleUpload(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/"):
		if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		s.handleAPI(w, r, strings.TrimPrefix(r.URL.Path, "/api/"))
	default:
		http.NotFound(w, r)
	}
}

var mimeTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8",
	".svg": "image/svg+xml", ".png": "image/png", ".woff2": "font/woff2", ".ico": "image/x-icon",
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request, name string) {
	b, err := webFS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if m := mimeTypes[filepath.Ext(name)]; m != "" {
		w.Header().Set("Content-Type", m)
	}
	w.Header().Set("Cache-Control", "no-cache")
	if strings.HasSuffix(name, ".html") {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'")
	}
	w.Write(b)
}

func (s *Server) serveBlob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/blob/")
	s.mu.Lock()
	b, ok := s.blobs[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", b.mime)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Write(b.data)
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, core.ErrPasswordRequired):
		return "password_required"
	case errors.Is(err, core.ErrBadPassword):
		return "bad_password"
	case errors.Is(err, core.ErrCancelled):
		return "cancelled"
	case errors.Is(err, core.ErrLocked):
		return "locked"
	case errors.Is(err, core.ErrReadOnlyFormat):
		return "readonly"
	case errors.Is(err, os.ErrNotExist):
		return "not_found"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	}
	return "error"
}

func errorText(err error) string {
	switch errorCode(err) {
	case "password_required":
		return "This archive is protected with a password."
	case "bad_password":
		return "The password is incorrect."
	case "cancelled":
		return "The operation was cancelled."
	case "permission":
		return "Access denied: " + err.Error()
	}
	return err.Error()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request, name string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h, ok := apiHandlers[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	res, err := h(s, body)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		writeJSON(w, apiError{Error: errorText(err), Code: errorCode(err)})
		return
	}
	if res == nil {
		res = map[string]any{"ok": true}
	}
	writeJSON(w, res)
}

type handler func(s *Server, body []byte) (any, error)

var apiHandlers = map[string]handler{}

func init() {
	apiHandlers = map[string]handler{
		"init":            (*Server).apiInit,
		"heartbeat":       (*Server).apiHeartbeat,
		"fs/list":         (*Server).apiFsList,
		"fs/mkdir":        (*Server).apiFsMkdir,
		"fs/rename":       (*Server).apiFsRename,
		"fs/delete":       (*Server).apiFsDelete,
		"fs/stat":         (*Server).apiFsStat,
		"archive/list":    (*Server).apiArchiveList,
		"archive/temp":    (*Server).apiArchiveTemp,
		"preview":         (*Server).apiPreview,
		"open":            (*Server).apiOpen,
		"reveal":          (*Server).apiReveal,
		"suggest":         (*Server).apiSuggest,
		"prefs/set":       (*Server).apiPrefsSet,
		"integration/get": (*Server).apiIntegrationGet,
		"integration/set": (*Server).apiIntegrationSet,
		"exit":            (*Server).apiExit,
		"task/start":      (*Server).apiTaskStart,
		"task/status":     (*Server).apiTaskStatus,
		"task/cancel":     (*Server).apiTaskCancel,
	}
}

func decode[T any](body []byte) (T, error) {
	var v T
	if len(body) == 0 {
		return v, nil
	}
	err := json.Unmarshal(body, &v)
	return v, err
}

// ---------------------------------------------------------------- preferences

func (s *Server) prefsPath() string { return filepath.Join(s.opt.DataDir, "prefs.json") }

func (s *Server) loadPrefs() {
	s.prefs = map[string]any{}
	if b, err := os.ReadFile(s.prefsPath()); err == nil {
		json.Unmarshal(b, &s.prefs)
	}
}

func (s *Server) savePrefs() error {
	b, _ := json.MarshalIndent(s.prefs, "", "  ")
	tmp := s.prefsPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.prefsPath())
}

func (s *Server) apiPrefsSet(body []byte) (any, error) {
	v, err := decode[map[string]any](body)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, x := range v {
		if x == nil {
			delete(s.prefs, k)
		} else {
			s.prefs[k] = x
		}
	}
	return nil, s.savePrefs()
}

// addRecent remembers an opened archive.
func (s *Server) addRecent(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []any
	if l, ok := s.prefs["recent"].([]any); ok {
		list = l
	}
	out := []any{p}
	for _, x := range list {
		if xs, ok := x.(string); ok && !strings.EqualFold(xs, p) && len(out) < 12 {
			out = append(out, xs)
		}
	}
	s.prefs["recent"] = out
	s.savePrefs()
}

// ---------------------------------------------------------------- init & misc

func (s *Server) defaultPlaces() []Place {
	home, _ := os.UserHomeDir()
	var out []Place
	add := func(name, p, icon string) {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, Place{name, p, icon})
		}
	}
	add("Home", home, "home")
	for _, d := range []struct{ name, icon string }{{"Desktop", "desktop"}, {"Documents", "documents"},
		{"Downloads", "downloads"}, {"Pictures", "pictures"}, {"Music", "music"}, {"Videos", "videos"}} {
		add(d.name, filepath.Join(home, d.name), d.icon)
	}
	return out
}

func drives() []Place {
	var out []Place
	if runtime.GOOS == "windows" {
		for c := 'A'; c <= 'Z'; c++ {
			p := string(c) + `:\`
			if c < 'C' {
				continue // skip floppy drives (slow)
			}
			if _, err := os.Stat(p); err == nil {
				out = append(out, Place{"Local Disk (" + string(c) + ":)", p, "drive"})
			}
		}
		return out
	}
	if runtime.GOOS != "android" {
		out = append(out, Place{"File system", "/", "drive"})
	}
	return out
}

func (s *Server) apiInit(_ []byte) (any, error) {
	places := s.host.Places()
	if places == nil {
		places = s.defaultPlaces()
	}
	home, _ := os.UserHomeDir()
	if s.host.Platform() == "android" && len(places) > 0 {
		home = places[0].Path
	}
	s.mu.Lock()
	prefs := map[string]any{}
	for k, v := range s.prefs {
		prefs[k] = v
	}
	s.mu.Unlock()
	start := s.opt.Start
	if start != "" {
		if a, err := filepath.Abs(start); err == nil {
			start = a
		}
	}
	var formats []string
	for _, f := range core.WritableFormats {
		formats = append(formats, string(f))
	}
	return map[string]any{
		"version":     s.opt.Version,
		"platform":    s.host.Platform(),
		"sep":         string(filepath.Separator),
		"home":        home,
		"places":      places,
		"drives":      drives(),
		"prefs":       prefs,
		"start":       start,
		"action":      s.opt.Action,
		"args":        s.opt.Args,
		"formats":     formats,
		"sfx":         len(s.opt.SFXStub) > 0,
		"integration": s.host.Integration() != nil,
		"cpus":        runtime.NumCPU(),
		"tempDir":     s.opt.TempDir,
	}, nil
}

func (s *Server) apiHeartbeat(_ []byte) (any, error) {
	s.mu.Lock()
	s.lastHB = time.Now()
	s.mu.Unlock()
	return nil, nil
}

func (s *Server) apiExit(body []byte) (any, error) {
	v, _ := decode[struct{ Code int }](body)
	s.mu.Lock()
	s.exitCode = v.Code
	s.mu.Unlock()
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.host.Exit(v.Code)
	}()
	return nil, nil
}

func (s *Server) apiIntegrationGet(_ []byte) (any, error) {
	in := s.host.Integration()
	if in == nil {
		return nil, errors.New("shell integration is not available on this system")
	}
	return in, nil
}

func (s *Server) apiIntegrationSet(body []byte) (any, error) {
	v, err := decode[map[string]any](body)
	if err != nil {
		return nil, err
	}
	return nil, s.host.SetIntegration(v)
}

func (s *Server) apiOpen(body []byte) (any, error) {
	v, err := decode[struct{ Path, Archive, Password, Entry string }](body)
	if err != nil {
		return nil, err
	}
	p := v.Path
	if v.Archive != "" {
		if p, err = s.extractTemp(v.Archive, v.Password, v.Entry); err != nil {
			return nil, err
		}
	}
	err = s.host.Open(p)
	return map[string]any{"path": p, "opened": err == nil, "error": errString(err)}, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *Server) apiReveal(body []byte) (any, error) {
	v, err := decode[struct{ Path string }](body)
	if err != nil {
		return nil, err
	}
	return nil, s.host.Reveal(v.Path)
}

func (s *Server) apiSuggest(body []byte) (any, error) {
	v, err := decode[struct {
		Inputs []string
		Format string
	}](body)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": core.SuggestArchiveName(v.Inputs, core.Format(v.Format))}, nil
}

// ---------------------------------------------------------------- file system

type fsItem struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	Mtime   int64  `json:"mtime"`
	Archive bool   `json:"archive,omitempty"`
	Hidden  bool   `json:"hidden,omitempty"`
}

func (s *Server) apiFsList(body []byte) (any, error) {
	v, err := decode[struct {
		Dir      string
		DirsOnly bool
	}](body)
	if err != nil {
		return nil, err
	}
	if v.Dir == "" {
		// "This PC": drives and places
		var items []fsItem
		for _, d := range drives() {
			items = append(items, fsItem{Name: d.Name, Path: d.Path, Dir: true})
		}
		return map[string]any{"dir": "", "parent": nil, "items": items}, nil
	}
	dir, err := filepath.Abs(v.Dir)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	items := make([]fsItem, 0, len(ents))
	for _, e := range ents {
		full := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		isDir := info.IsDir()
		if info.Mode()&fs.ModeSymlink != 0 {
			if st, err := os.Stat(full); err == nil {
				info, isDir = st, st.IsDir()
			}
		}
		if v.DirsOnly && !isDir {
			continue
		}
		it := fsItem{Name: e.Name(), Path: full, Dir: isDir, Mtime: info.ModTime().UnixMilli(), Hidden: isHidden(full, e.Name(), info)}
		if !isDir {
			it.Size = info.Size()
			it.Archive = core.IsArchiveName(e.Name())
		}
		items = append(items, it)
	}
	var parent any
	if p := filepath.Dir(dir); p != dir {
		parent = p
	} else if runtime.GOOS == "windows" {
		parent = "" // drive root -> This PC
	}
	return map[string]any{"dir": dir, "parent": parent, "items": items}, nil
}

func (s *Server) apiFsStat(body []byte) (any, error) {
	v, err := decode[struct{ Path string }](body)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(v.Path)
	if err != nil {
		return map[string]any{"exists": false}, nil
	}
	return map[string]any{"exists": true, "dir": st.IsDir(), "size": st.Size(), "mtime": st.ModTime().UnixMilli(),
		"archive": !st.IsDir() && core.IsArchiveName(st.Name())}, nil
}

func validName(n string) bool {
	return n != "" && n != "." && n != ".." && !strings.ContainsAny(n, `/\:*?"<>|`+"\x00")
}

func (s *Server) apiFsMkdir(body []byte) (any, error) {
	v, err := decode[struct{ Dir, Name string }](body)
	if err != nil {
		return nil, err
	}
	if !validName(v.Name) {
		return nil, errors.New("invalid folder name")
	}
	p := filepath.Join(v.Dir, v.Name)
	return map[string]any{"path": p}, os.Mkdir(p, 0o755)
}

func (s *Server) apiFsRename(body []byte) (any, error) {
	v, err := decode[struct{ Path, Name string }](body)
	if err != nil {
		return nil, err
	}
	if !validName(v.Name) {
		return nil, errors.New("invalid name")
	}
	np := filepath.Join(filepath.Dir(v.Path), v.Name)
	if _, err := os.Lstat(np); err == nil && !strings.EqualFold(np, v.Path) {
		return nil, errors.New(v.Name + " already exists")
	}
	return map[string]any{"path": np}, os.Rename(v.Path, np)
}

func (s *Server) apiFsDelete(body []byte) (any, error) {
	v, err := decode[struct{ Paths []string }](body)
	if err != nil {
		return nil, err
	}
	if len(v.Paths) == 0 {
		return nil, nil
	}
	return nil, s.host.Trash(v.Paths)
}

// ---------------------------------------------------------------- archives

func cacheKey(p, password string) string {
	st, err := os.Stat(p)
	if err != nil {
		return ""
	}
	return p + "|" + st.ModTime().String() + "|" + itoa(st.Size()) + "|" + password
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// listArchive returns (cached) entries of an archive.
func (s *Server) listArchive(p, password string) (*listing, error) {
	key := cacheKey(p, password)
	s.mu.Lock()
	if l, ok := s.cache[p]; ok && l.key == key && key != "" {
		l.used = time.Now()
		s.mu.Unlock()
		return l, nil
	}
	s.mu.Unlock()
	ents, f, err := core.List(p, password)
	if err != nil {
		return nil, err
	}
	info, _ := core.Info(p, password, ents)
	l := &listing{key: key, entries: ents, format: f, info: info, used: time.Now()}
	s.mu.Lock()
	s.cache[p] = l
	if len(s.cache) > 8 { // forget the least recently used listing
		var old string
		var t time.Time
		for k, v := range s.cache {
			if old == "" || v.used.Before(t) {
				old, t = k, v.used
			}
		}
		delete(s.cache, old)
	}
	s.mu.Unlock()
	return l, nil
}

func (s *Server) forget(p string) {
	s.mu.Lock()
	delete(s.cache, p)
	s.mu.Unlock()
}

func (s *Server) apiArchiveList(body []byte) (any, error) {
	v, err := decode[struct{ Path, Password string }](body)
	if err != nil {
		return nil, err
	}
	p, err := filepath.Abs(v.Path)
	if err != nil {
		return nil, err
	}
	l, err := s.listArchive(p, v.Password)
	if err != nil {
		return nil, err
	}
	s.addRecent(p)
	ents := l.entries
	if ents == nil {
		ents = []core.Entry{}
	}
	return map[string]any{"path": p, "format": l.format, "entries": ents, "info": l.info}, nil
}

// extractTemp extracts one member into a private temporary folder.
func (s *Server) extractTemp(archive, password, entry string) (string, error) {
	dir := filepath.Join(s.opt.TempDir, "open", randHex(4))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := ""
	if i := strings.LastIndex(entry, "/"); i >= 0 {
		base = entry[:i]
	}
	if _, err := core.Extract(archive, dir, core.ExtractOptions{Password: password, Select: []string{entry}, BaseDir: base}); err != nil {
		return "", err
	}
	p := filepath.Join(dir, filepath.FromSlash(entry[len(base):]))
	if _, err := os.Stat(p); err != nil {
		return "", err
	}
	return p, nil
}

func (s *Server) apiArchiveTemp(body []byte) (any, error) {
	v, err := decode[struct{ Archive, Password, Entry string }](body)
	if err != nil {
		return nil, err
	}
	p, err := s.extractTemp(v.Archive, v.Password, v.Entry)
	return map[string]any{"path": p}, err
}

// CleanTemp removes files extracted for viewing (call on exit).
func (s *Server) CleanTemp() { os.RemoveAll(filepath.Join(s.opt.TempDir, "open")) }

// ---------------------------------------------------------------- preview

var imageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
	".bmp": "image/bmp", ".ico": "image/x-icon", ".svg": "image/svg+xml", ".avif": "image/avif",
}

func (s *Server) apiPreview(body []byte) (any, error) {
	v, err := decode[struct{ Path, Archive, Password, Entry string }](body)
	if err != nil {
		return nil, err
	}
	name := v.Path
	if v.Archive != "" {
		name = v.Entry
	}
	ext := strings.ToLower(filepath.Ext(name))
	mime, isImage := imageTypes[ext]
	limit := int64(64 << 10)
	if isImage {
		limit = 24 << 20
	}
	var data []byte
	var size int64
	if v.Archive != "" {
		data, size, err = core.ReadEntry(v.Archive, v.Password, v.Entry, limit)
	} else {
		var f *os.File
		if f, err = os.Open(v.Path); err == nil {
			st, _ := f.Stat()
			size = st.Size()
			data, err = io.ReadAll(io.LimitReader(f, limit))
			f.Close()
		}
	}
	if err != nil {
		return nil, err
	}
	res := map[string]any{"size": size, "truncated": int64(len(data)) < size}
	switch {
	case isImage && int64(len(data)) == size:
		id := randHex(8)
		s.mu.Lock()
		if len(s.blobs) > 16 {
			s.blobs = map[string]blob{}
		}
		s.blobs[id] = blob{mime, data}
		s.mu.Unlock()
		res["kind"], res["url"] = "image", "/blob/"+id
	case looksText(data):
		res["kind"], res["text"] = "text", string(data)
	default:
		if len(data) > 4096 {
			data = data[:4096]
		}
		res["kind"], res["hex"] = "hex", hex.EncodeToString(data)
	}
	return res, nil
}

func looksText(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	n := len(b)
	if n > 8192 {
		n = 8192
	}
	bad := 0
	for _, c := range b[:n] {
		if c == 0 {
			return false
		}
		if c < 9 || (c > 13 && c < 32) {
			bad++
		}
	}
	return bad*100/n < 2
}

// ---------------------------------------------------------------- uploads (drag & drop)

// handleUpload stores dropped files in a temporary folder so they can be
// added to an archive. Fields: "path" (relative path) followed by "file".
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	dir := r.URL.Query().Get("dir")
	if dir == "" || !strings.HasPrefix(filepath.Clean(dir), filepath.Join(s.opt.TempDir, "drop")) {
		dir = filepath.Join(s.opt.TempDir, "drop", randHex(4))
	}
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tops := map[string]bool{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// part.FileName() drops folders; the page sends the relative path.
		_, params, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		rel := core.CleanPath(strings.ReplaceAll(params["filename"], `\`, "/"))
		if rel == "" {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(target), 0o755)
		f, err := os.Create(target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, err = io.Copy(f, part)
		f.Close()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tops[filepath.Join(dir, strings.SplitN(filepath.FromSlash(rel), string(filepath.Separator), 2)[0])] = true
	}
	var out []string
	for t := range tops {
		out = append(out, t)
	}
	sort.Strings(out)
	writeJSON(w, map[string]any{"dir": dir, "paths": out})
}
