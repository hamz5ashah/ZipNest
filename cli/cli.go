// Package cli is ZipNest's command-line interface. The Android app runs it
// with --json and reads one JSON object per line from stdout.
package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"zipnest/core"
)

const Version = "2.0.0"

// SFXStub is the self-extractor program (set by ZipNest.exe, which embeds it).
var SFXStub []byte

const usage = `ZipNest ` + Version + ` - archive manager

usage:
  zipnest l <archive>                       list contents
  zipnest x <archive> [dest] [paths...]     extract (default dest: folder named after archive)
  zipnest e <archive> [dest] [paths...]     extract into dest without the archive-name folder
  zipnest t <archive>                       test integrity
  zipnest a <archive> <files/folders...>    create / add to archive (.nest .zip .7z .tar.gz .tar.xz .tar.zst)
  zipnest d <archive> <paths...>            delete members from a ZIP/TAR archive
  zipnest c <archive> <new archive>         convert (e.g. photos.rar -> photos.7z)
  zipnest r <archive.nest>                  repair with the recovery record (writes fixed.<name>)

options:
  -p<password>        password (or -p alone to be asked / read from stdin)
  --password-stdin    read the password from the first line of stdin
  -hp<password>       password and encrypted file names (7z)
  -m<0-9>             compression level for 'a' (0 = store, default 6)
  -v<size>            split into volumes, e.g. -v100m, -v700m, -v4g
  --sfx               make a self-extracting .exe (ZipNest.exe on Windows)
  -rr<1-50>           .nest: add a recovery record, e.g. -rr5 (5 %)
  --lock              .nest: lock the archive against changes
  --comment=<text>    .nest: archive comment
  --format=<fmt>      format for 'a' (zip, 7z, tar, tar.gz, tar.xz, tar.zst)
  --base=<dir>        folder inside the archive ('a': add into it, 'x': extract relative to it)
  -o+ / -o- / -or     overwrite / skip / rename existing files when extracting
  --new-password=<p>  password for the new archive ('c')
  --json              machine readable output (one JSON object per line)
`

type options struct {
	json      bool
	encNames  bool
	volume    int64
	newPass   string
	sfx       bool
	recovery  int
	lock      bool
	comment   string
	sfxStub   string
	password  string
	askPass   bool
	level     int
	format    string
	base      string
	overwrite string
}

type emitter struct {
	mu   sync.Mutex
	out  io.Writer
	json bool
	last time.Time
	file string
}

func (e *emitter) send(v map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, _ := json.Marshal(v)
	e.out.Write(append(b, '\n'))
}

func (e *emitter) progress(file string, done, total int64) bool {
	now := time.Now()
	if file == e.file && now.Sub(e.last) < 120*time.Millisecond {
		return true
	}
	e.last, e.file = now, file
	if e.json {
		e.send(map[string]any{"type": "progress", "file": file, "done": done, "total": total})
	} else {
		pct := ""
		if total > 0 {
			pct = fmt.Sprintf("%3d%% ", done*100/total)
		}
		fmt.Fprintf(os.Stderr, "\r%s%-60.60s", pct, file)
	}
	return true
}

func errCode(err error) (string, int) {
	switch {
	case errors.Is(err, core.ErrPasswordRequired):
		return "password_required", 2
	case errors.Is(err, core.ErrBadPassword):
		return "bad_password", 3
	case errors.Is(err, core.ErrCancelled):
		return "cancelled", 4
	}
	return "error", 1
}

// Run executes the CLI and returns the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opt := options{level: 6, overwrite: "overwrite"}
	var pos []string
	readStdin := false
	for _, a := range args {
		switch {
		case a == "--json":
			opt.json = true
		case a == "--password-stdin":
			readStdin = true
		case a == "-p":
			opt.askPass = true
		case strings.HasPrefix(a, "-hp"):
			opt.password = a[3:]
			opt.encNames = true
			if opt.password == "" {
				opt.askPass = true
			}
		case strings.HasPrefix(a, "--new-password="):
			opt.newPass = a[len("--new-password="):]
		case strings.HasPrefix(a, "-rr"):
			n, err := strconv.Atoi(strings.TrimSuffix(a[3:], "%"))
			if err != nil || n < 1 || n > 50 {
				fmt.Fprintln(stderr, "ERROR: recovery record must be 1-50 %, e.g. -rr5")
				return 1
			}
			opt.recovery = n
		case a == "--lock":
			opt.lock = true
		case strings.HasPrefix(a, "--comment="):
			opt.comment = a[len("--comment="):]
		case a == "--sfx":
			opt.sfx = true
		case strings.HasPrefix(a, "--sfx-stub="):
			opt.sfx = true
			opt.sfxStub = a[len("--sfx-stub="):]
		case a == "--encrypt-names":
			opt.encNames = true
		case strings.HasPrefix(a, "-v") && len(a) > 2 && a[2] >= '0' && a[2] <= '9':
			v, err := parseSize(a[2:])
			if err != nil {
				fmt.Fprintln(stderr, "ERROR: bad volume size", a)
				return 1
			}
			opt.volume = v
		case strings.HasPrefix(a, "-p") && !strings.HasPrefix(a, "--"):
			opt.password = a[2:]
		case strings.HasPrefix(a, "-m") && len(a) == 3 && a[2] >= '0' && a[2] <= '9':
			opt.level = int(a[2] - '0')
		case strings.HasPrefix(a, "--format="):
			opt.format = a[9:]
		case strings.HasPrefix(a, "--base="):
			opt.base = a[7:]
		case a == "-o+":
			opt.overwrite = "overwrite"
		case a == "-o-":
			opt.overwrite = "skip"
		case a == "-or":
			opt.overwrite = "rename"
		case a == "-h" || a == "--help" || a == "/?":
			fmt.Fprint(stdout, usage)
			return 0
		case a == "--version":
			fmt.Fprintln(stdout, Version)
			return 0
		case a == "--":
		default:
			pos = append(pos, a)
		}
	}
	if readStdin || (opt.askPass && opt.json) {
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		opt.password = strings.TrimRight(line, "\r\n")
	} else if opt.askPass {
		fmt.Fprint(stderr, "Password: ")
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		opt.password = strings.TrimRight(line, "\r\n")
	}
	if len(pos) < 2 {
		fmt.Fprint(stderr, usage)
		return 1
	}
	em := &emitter{out: stdout, json: opt.json}
	cmd, archive, rest := strings.ToLower(pos[0]), pos[1], pos[2:]

	result, err := dispatch(cmd, archive, rest, opt, em, stdout)
	if !opt.json {
		fmt.Fprint(stderr, "\r"+strings.Repeat(" ", 66)+"\r")
	}
	if err != nil {
		code, exit := errCode(err)
		if opt.json {
			em.send(map[string]any{"type": "error", "code": code, "message": err.Error()})
		} else {
			fmt.Fprintln(stderr, "ERROR:", err)
		}
		return exit
	}
	if opt.json {
		m := map[string]any{"type": "result", "ok": true}
		if result != nil {
			m["result"] = result
		}
		em.send(m)
	}
	return 0
}

func dispatch(cmd, archive string, rest []string, opt options, em *emitter, stdout io.Writer) (any, error) {
	switch cmd {
	case "l", "list":
		entries, f, err := core.List(archive, opt.password)
		if err != nil {
			return nil, err
		}
		if opt.json {
			if entries == nil {
				entries = []core.Entry{}
			}
			return map[string]any{"format": f, "entries": entries}, nil
		}
		fmt.Fprintf(stdout, "Archive: %s (%s)\n\n", archive, f)
		fmt.Fprintf(stdout, "%19s %14s %14s  %s\n", "Modified", "Size", "Packed", "Name")
		var files, size int64
		for _, e := range entries {
			t := ""
			if e.Modified != 0 {
				t = time.UnixMilli(e.Modified).Format("2006-01-02 15:04:05")
			}
			s, p := strconv.FormatInt(e.Size, 10), strconv.FormatInt(e.Packed, 10)
			name := e.Path
			if e.Dir {
				s, p, name = "<DIR>", "", name+"/"
			} else {
				files++
				if e.Size > 0 {
					size += e.Size
				}
			}
			if e.Encrypted {
				name = "*" + name
			}
			if p == "-1" {
				p = ""
			}
			if s == "-1" {
				s = "?"
			}
			fmt.Fprintf(stdout, "%19s %14s %14s  %s\n", t, s, p, name)
		}
		fmt.Fprintf(stdout, "\n%d files, %d bytes\n", files, size)
		return nil, nil

	case "x", "e", "extract":
		dest := ""
		sel := rest
		if len(rest) > 0 {
			dest, sel = rest[0], rest[1:]
		}
		if dest == "" {
			dest = core.ArchiveStem(archive)
			if cmd == "e" {
				dest = "."
			}
		}
		res, err := core.Extract(archive, dest, core.ExtractOptions{
			Password: opt.password, Select: sel, BaseDir: opt.base,
			Overwrite: opt.overwrite, Progress: em.progress,
		})
		if err != nil {
			return nil, err
		}
		if !opt.json {
			fmt.Fprintf(stdout, "Extracted %d files, %d folders (%d bytes) to %s\n", res.Files, res.Dirs, res.Bytes, dest)
			for _, s := range res.Skipped {
				fmt.Fprintln(stdout, "  skipped:", s)
			}
		}
		return res, nil

	case "t", "test":
		res, err := core.Test(archive, opt.password, em.progress)
		if err != nil {
			return nil, err
		}
		if !opt.json {
			for _, e := range res.Errors {
				fmt.Fprintln(stdout, "  FAILED:", e)
			}
			if len(res.Errors) == 0 {
				fmt.Fprintf(stdout, "All OK: %d files, %d bytes\n", res.Files, res.Bytes)
			}
		}
		if len(res.Errors) > 0 {
			return res, fmt.Errorf("%d file(s) are damaged: %s", len(res.Errors), res.Errors[0])
		}
		return res, nil

	case "a", "add", "create":
		if len(rest) == 0 {
			return nil, errors.New("no files to add")
		}
		var stub []byte
		if opt.sfx {
			stub = SFXStub
			if opt.sfxStub != "" {
				b, err := os.ReadFile(opt.sfxStub)
				if err != nil {
					return nil, err
				}
				stub = b
			}
			if len(stub) == 0 {
				return nil, errors.New("self-extracting archives can be made with ZipNest.exe (Windows)")
			}
		}
		err := core.Create(archive, rest, core.CreateOptions{
			SFXStub: stub, RecoveryPercent: opt.recovery, Lock: opt.lock, Comment: opt.comment,
			Format: core.Format(opt.format), Level: opt.level, Password: opt.password,
			EncryptNames: opt.encNames, VolumeSize: opt.volume,
			BaseDir: opt.base, Progress: em.progress,
		})
		if err != nil {
			return nil, err
		}
		if !opt.json {
			fmt.Fprintln(stdout, "Created", archive)
		}
		return map[string]any{"archive": archive}, nil

	case "c", "convert":
		if len(rest) != 1 {
			return nil, errors.New("usage: c <source archive> <new archive>")
		}
		err := core.Convert(archive, rest[0], core.ConvertOptions{
			Password: opt.password,
			Create: core.CreateOptions{Format: core.Format(opt.format), Level: opt.level, Password: opt.newPass,
				EncryptNames: opt.encNames, VolumeSize: opt.volume, Progress: em.progress},
		})
		if err != nil {
			return nil, err
		}
		if !opt.json {
			fmt.Fprintln(stdout, "Converted", archive, "->", rest[0])
		}
		return map[string]any{"archive": rest[0]}, nil

	case "r", "repair":
		res, err := core.Repair(archive, "", em.progress)
		if res != nil && !opt.json {
			switch {
			case res.Damaged == 0:
				fmt.Fprintln(stdout, "No damage found: the archive is intact.")
			case res.Lost == 0:
				fmt.Fprintf(stdout, "Repaired %d damaged part(s) -> %s\n", res.Repaired, res.Output)
			}
		}
		if err != nil {
			return res, err
		}
		return res, nil

	case "d", "delete":
		n, err := core.Delete(archive, rest)
		if err != nil {
			return nil, err
		}
		if !opt.json {
			fmt.Fprintf(stdout, "Deleted %d entries\n", n)
		}
		return map[string]any{"deleted": n}, nil
	}
	return nil, fmt.Errorf("unknown command %q", cmd)
}

// parseSize parses "100m", "4g", "700k", "1048576".
func parseSize(s string) (int64, error) {
	mult := int64(1)
	switch strings.ToLower(s[len(s)-1:]) {
	case "k":
		mult, s = 1<<10, s[:len(s)-1]
	case "m":
		mult, s = 1<<20, s[:len(s)-1]
	case "g":
		mult, s = 1<<30, s[:len(s)-1]
	case "b":
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("bad size %q", s)
	}
	return int64(n * float64(mult)), nil
}
