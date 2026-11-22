//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/lxn/walk"

	"zipnest/core"
)

// ---- Add ----

func (a *App) cmdAdd() {
	if a.inArchive() {
		fd := walk.FileDialog{Title: "Add files to " + filepath.Base(a.archive), InitialDirPath: filepath.Dir(a.archive)}
		if ok, _ := fd.ShowOpenMultiple(a.mw); ok && len(fd.FilePaths) > 0 {
			a.addToOpenArchive(fd.FilePaths)
		}
		return
	}
	var files []string
	for _, r := range a.selectedRows() {
		files = append(files, r.full)
	}
	if len(files) == 0 {
		walk.MsgBox(a.mw, "ZipNest", "Select the files and folders you want to add to an archive.", walk.MsgBoxIconInformation)
		return
	}
	a.addFiles(files)
}

func (a *App) addFiles(files []string) {
	s, ok := addDialog(a.form(), core.SuggestArchiveName(files, core.FormatZip))
	if !ok {
		return
	}
	if err := createArchive(a.form(), s, files); err != nil {
		a.errorBox(err)
	}
	if a.mw != nil && !a.inArchive() && a.dir != "" {
		a.showDir(a.dir)
		a.selectName(filepath.Base(s.archive))
	}
}

func createArchive(owner walk.Form, s *addSettings, files []string) error {
	out := s.archive
	if !filepath.IsAbs(out) && len(files) > 0 {
		out = filepath.Join(filepath.Dir(files[0]), out)
	}
	if _, err := os.Stat(out); err == nil && (s.format != core.FormatZip || s.sfx) {
		if walk.MsgBox(owner, "ZipNest", filepath.Base(out)+" already exists. Replace it?",
			walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
			return core.ErrCancelled
		}
		os.Remove(out)
	}
	err := runTask(owner, "Creating "+filepath.Base(out), func(p core.ProgressFunc) error {
		opt := core.CreateOptions{
			Format: s.format, Level: s.level, Password: s.password,
			EncryptNames: s.encNames, VolumeSize: s.volume, Progress: p,
			RecoveryPercent: s.recovery, Lock: s.lock,
		}
		if s.sfx {
			opt.SFXStub = sfxStub
		}
		return core.Create(out, files, opt)
	})
	if err != nil {
		return err
	}
	if s.deleteAfter {
		if err := recycle(0, files); err != nil {
			return fmt.Errorf("archive created, but the files could not be deleted: %w", err)
		}
	}
	return nil
}

func (a *App) addToOpenArchive(files []string) {
	if a.format != core.FormatZip {
		walk.MsgBox(a.mw, "ZipNest", "Files can only be added to ZIP archives.\n"+
			strings.ToUpper(string(a.format))+" archives are read-only in ZipNest.", walk.MsgBoxIconWarning)
		return
	}
	pw := ""
	if a.password != "" && walk.MsgBox(a.mw, "ZipNest", "Encrypt the new files with the archive password?",
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes {
		pw = a.password
	}
	inner := a.inner
	err := runTask(a.mw, "Adding to "+filepath.Base(a.archive), func(p core.ProgressFunc) error {
		return core.Create(a.archive, files, core.CreateOptions{
			Format: core.FormatZip, Level: 6, Password: pw, BaseDir: inner, Progress: p,
		})
	})
	a.errorBox(err)
	a.refresh()
}

// ---- Extract ----

func (a *App) selectedArchives() []string {
	var out []string
	for _, r := range a.selectedRows() {
		if r.isArchive {
			out = append(out, r.full)
		}
	}
	if len(out) == 0 {
		if i := a.tv.CurrentIndex(); i >= 0 && i < len(a.model.rows) && a.model.rows[i].isArchive {
			out = append(out, a.model.rows[i].full)
		}
	}
	return out
}

func (a *App) cmdExtract() { a.extract(false) }

func (a *App) cmdExtractHere() { a.extract(true) }

func (a *App) extract(here bool) {
	if a.inArchive() {
		var sel []string
		for _, r := range a.selectedRows() {
			sel = append(sel, r.full)
		}
		dest := filepath.Join(filepath.Dir(a.archive), core.ArchiveStem(a.archive))
		mode := "overwrite"
		if here {
			dest = filepath.Dir(a.archive)
		} else {
			var ok bool
			if dest, mode, ok = extractDialog(a.mw, dest); !ok {
				return
			}
		}
		pw := a.password
		var res *core.ExtractResult
		err := a.withPassword(a.archive, &pw, "Extracting from "+filepath.Base(a.archive), func(pw string, p core.ProgressFunc) error {
			var err error
			res, err = core.Extract(a.archive, dest, core.ExtractOptions{
				Password: pw, Select: sel, BaseDir: a.inner, Overwrite: mode, Progress: p,
			})
			return err
		})
		if err == nil {
			a.password = pw
		}
		a.reportExtract(dest, res, err)
		return
	}
	archives := a.selectedArchives()
	if len(archives) == 0 {
		walk.MsgBox(a.mw, "ZipNest", "Select one or more archives to extract.", walk.MsgBoxIconInformation)
		return
	}
	mode := "overwrite"
	base := a.dir
	if !here {
		def := a.dir
		if len(archives) == 1 {
			def = filepath.Join(a.dir, core.ArchiveStem(archives[0]))
		}
		var ok bool
		if base, mode, ok = extractDialog(a.mw, def); !ok {
			return
		}
	}
	for _, arc := range archives {
		dest := base
		if !here && len(archives) > 1 {
			dest = filepath.Join(base, core.ArchiveStem(arc))
		}
		res, err := extractWithUI(a.form(), arc, dest, mode)
		if errors.Is(err, core.ErrCancelled) {
			break
		}
		a.reportExtract(dest, res, err)
	}
	a.showDir(a.dir)
}

func extractWithUI(owner walk.Form, arc, dest, mode string) (*core.ExtractResult, error) {
	tmp := &App{}
	tmp.mw, _ = owner.(*walk.MainWindow)
	pw := ""
	var res *core.ExtractResult
	err := tmp.withPasswordOwner(owner, arc, &pw, "Extracting "+filepath.Base(arc), func(pw string, p core.ProgressFunc) error {
		var err error
		res, err = core.Extract(arc, dest, core.ExtractOptions{Password: pw, Overwrite: mode, Progress: p})
		return err
	})
	return res, err
}

func (a *App) withPasswordOwner(owner walk.Form, archive string, pw *string, title string, op func(string, core.ProgressFunc) error) error {
	for {
		cur := *pw
		err := runTask(owner, title, func(p core.ProgressFunc) error { return op(cur, p) })
		if errors.Is(err, core.ErrPasswordRequired) || errors.Is(err, core.ErrBadPassword) {
			msg := "Enter password"
			if cur != "" {
				msg = "The password is incorrect. Please try again"
			}
			np, ok := passwordDialog(owner, archive, msg)
			if !ok {
				return core.ErrCancelled
			}
			*pw = np
			continue
		}
		return err
	}
}

func (a *App) reportExtract(dest string, res *core.ExtractResult, err error) {
	if err != nil {
		a.errorBox(err)
		return
	}
	if res != nil && len(res.Skipped) > 0 {
		msg := fmt.Sprintf("Extracted %d file(s) to %s\n\nSkipped %d item(s):\n", res.Files, dest, len(res.Skipped))
		for i, s := range res.Skipped {
			if i == 15 {
				msg += "...\n"
				break
			}
			msg += "  " + s + "\n"
		}
		walk.MsgBox(a.form(), "ZipNest", msg, walk.MsgBoxIconWarning)
	}
}

// ---- Test ----

func (a *App) cmdTest() {
	targets := a.selectedArchives()
	if a.inArchive() {
		targets = []string{a.archive}
	}
	if len(targets) == 0 {
		walk.MsgBox(a.mw, "ZipNest", "Select an archive to test.", walk.MsgBoxIconInformation)
		return
	}
	a.testArchives(a.mw, targets)
}

// testArchives tests each archive with a progress window and shows a summary.
func (a *App) testArchives(owner walk.Form, targets []string) {
	var report []string
	for _, arc := range targets {
		pw := ""
		if arc == a.archive {
			pw = a.password
		}
		var res *core.TestResult
		err := a.withPassword(arc, &pw, "Testing "+filepath.Base(arc), func(pw string, p core.ProgressFunc) error {
			var err error
			res, err = core.Test(arc, pw, p)
			return err
		})
		switch {
		case errors.Is(err, core.ErrCancelled):
			return
		case err != nil:
			report = append(report, filepath.Base(arc)+": "+err.Error())
		case len(res.Errors) > 0:
			report = append(report, fmt.Sprintf("%s: %d damaged file(s)", filepath.Base(arc), len(res.Errors)))
			for i, e := range res.Errors {
				if i == 10 {
					break
				}
				report = append(report, "    "+e)
			}
		default:
			report = append(report, fmt.Sprintf("%s: OK — %d file(s), %s", filepath.Base(arc), res.Files, shortSize(res.Bytes)))
		}
	}
	icon := walk.MsgBoxIconInformation
	for _, r := range report {
		if !strings.Contains(r, ": OK") {
			icon = walk.MsgBoxIconError
		}
	}
	walk.MsgBox(owner, "Test results", strings.Join(report, "\n"), icon)
}

// ---- View ----

func (a *App) cmdView() {
	i := a.tv.CurrentIndex()
	if i < 0 || i >= len(a.model.rows) {
		return
	}
	r := a.model.rows[i]
	if r.up || r.dir {
		a.activate()
		return
	}
	if !a.inArchive() {
		shellOpen(a.mw.Handle(), r.full)
		return
	}
	tmp, err := os.MkdirTemp("", "ZipNest-view-")
	if err != nil {
		a.errorBox(err)
		return
	}
	pw := a.password
	base := ""
	if j := strings.LastIndex(r.full, "/"); j >= 0 {
		base = r.full[:j]
	}
	err = a.withPassword(a.archive, &pw, "Opening "+r.name, func(pw string, p core.ProgressFunc) error {
		_, err := core.Extract(a.archive, tmp, core.ExtractOptions{Password: pw, Select: []string{r.full}, BaseDir: base, Progress: p})
		return err
	})
	if err != nil {
		a.errorBox(err)
		return
	}
	a.password = pw
	shellOpen(a.mw.Handle(), filepath.Join(tmp, r.name))
}

// ---- Delete ----

func (a *App) cmdDelete() {
	sel := a.selectedRows()
	if len(sel) == 0 {
		return
	}
	what := fmt.Sprintf("%d selected items", len(sel))
	if len(sel) == 1 {
		what = "\"" + sel[0].name + "\""
	}
	if a.inArchive() {
		if walk.MsgBox(a.mw, "Delete from archive", "Permanently delete "+what+" from the archive?",
			walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
			return
		}
		var paths []string
		for _, r := range sel {
			paths = append(paths, r.full)
		}
		err := runTask(a.mw, "Deleting from "+filepath.Base(a.archive), func(core.ProgressFunc) error {
			_, err := core.Delete(a.archive, paths)
			return err
		})
		a.errorBox(err)
		a.refresh()
		return
	}
	if walk.MsgBox(a.mw, "Delete", "Move "+what+" to the Recycle Bin?",
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	var paths []string
	for _, r := range sel {
		paths = append(paths, r.full)
	}
	a.errorBox(recycle(a.mw.Handle(), paths))
	a.showDir(a.dir)
}

// ---- Info ----

func (a *App) cmdInfo() {
	arc := a.archive
	entries := a.entries
	format := a.format
	if !a.inArchive() {
		list := a.selectedArchives()
		if len(list) == 0 {
			walk.MsgBox(a.mw, "ZipNest", "Select an archive to see its information.", walk.MsgBoxIconInformation)
			return
		}
		arc = list[0]
		pw := ""
		err := a.withPassword(arc, &pw, "Reading "+filepath.Base(arc), func(pw string, _ core.ProgressFunc) error {
			var err error
			entries, format, err = core.List(arc, pw)
			return err
		})
		if err != nil {
			a.errorBox(err)
			return
		}
	}
	var files, dirs, enc int
	var size, packed int64
	for _, e := range entries {
		if e.Dir {
			dirs++
			continue
		}
		files++
		if e.Size > 0 {
			size += e.Size
		}
		if e.Packed > 0 {
			packed += e.Packed
		}
		if e.Encrypted {
			enc++
		}
	}
	st, _ := os.Stat(arc)
	var arcSize int64
	if st != nil {
		arcSize = st.Size()
	}
	ratio := "-"
	if size > 0 {
		ratio = fmt.Sprintf("%d%%", arcSize*100/size)
	}
	msg := fmt.Sprintf("Archive:\t%s\nFormat:\t%s\nLocation:\t%s\n\nFiles:\t%d\nFolders:\t%d\nTotal size:\t%s (%s bytes)\nArchive size:\t%s\nRatio:\t%s\nEncrypted files:\t%d",
		filepath.Base(arc), strings.ToUpper(string(format)), filepath.Dir(arc), files, dirs,
		shortSize(size), humanSize(size), shortSize(arcSize), ratio, enc)
	walk.MsgBox(a.mw, "Archive information", msg, walk.MsgBoxIconInformation)
}

func (a *App) cmdOpenArchive() {
	fd := walk.FileDialog{
		Title:          "Open archive",
		Filter:         "Archives|*.zip;*.rar;*.7z;*.tar;*.gz;*.tgz;*.bz2;*.tbz2;*.xz;*.txz;*.zst;*.cbz;*.cbr;*.jar;*.apk|All files|*.*",
		InitialDirPath: a.dir,
	}
	if ok, _ := fd.ShowOpen(a.mw); ok {
		a.openArchive(fd.FilePath)
	}
}

// ---- Explorer right-click commands (no main window) ----

func runShellCommand(cmd string, args []string) int {
	if len(args) == 0 {
		return 1
	}
	switch cmd {
	case "--add":
		s, ok := addDialog(nil, core.SuggestArchiveName(args, core.FormatZip))
		if !ok {
			return 1
		}
		if err := createArchive(nil, s, args); err != nil && !errors.Is(err, core.ErrCancelled) {
			walk.MsgBox(nil, "ZipNest", err.Error(), walk.MsgBoxIconError)
			return 1
		}
	case "--add-zip":
		out := core.SuggestArchiveName(args, core.FormatZip)
		if err := createArchive(nil, &addSettings{archive: out, format: core.FormatZip, level: 6}, args); err != nil && !errors.Is(err, core.ErrCancelled) {
			walk.MsgBox(nil, "ZipNest", err.Error(), walk.MsgBoxIconError)
			return 1
		}
	case "--extract-dialog":
		def := filepath.Join(filepath.Dir(args[0]), core.ArchiveStem(args[0]))
		if len(args) > 1 {
			def = filepath.Dir(args[0])
		}
		dest, mode, ok := extractDialog(nil, def)
		if !ok {
			return 1
		}
		for _, arc := range args {
			d := dest
			if len(args) > 1 {
				d = filepath.Join(dest, core.ArchiveStem(arc))
			}
			res, err := extractWithUI(nil, arc, d, mode)
			if errors.Is(err, core.ErrCancelled) {
				return 1
			}
			if err != nil {
				walk.MsgBox(nil, "ZipNest", filepath.Base(arc)+": "+err.Error(), walk.MsgBoxIconError)
				return 1
			}
			if res != nil && len(res.Skipped) > 0 {
				walk.MsgBox(nil, "ZipNest", fmt.Sprintf("%d item(s) were skipped.", len(res.Skipped)), walk.MsgBoxIconWarning)
			}
		}
	case "--convert":
		if convertArchives(nil, args, nil) == 0 {
			return 1
		}
	case "--test":
		tmp := &App{model: &listModel{}}
		tmp.testArchives(nil, args)
	case "--extract-here", "--extract-auto":
		for _, arc := range args {
			dest := filepath.Dir(arc)
			if cmd == "--extract-auto" {
				dest = filepath.Join(dest, core.ArchiveStem(arc))
			}
			res, err := extractWithUI(nil, arc, dest, "overwrite")
			if errors.Is(err, core.ErrCancelled) {
				return 1
			}
			if err != nil {
				walk.MsgBox(nil, "ZipNest", filepath.Base(arc)+": "+err.Error(), walk.MsgBoxIconError)
				return 1
			}
			if res != nil && len(res.Skipped) > 0 {
				walk.MsgBox(nil, "ZipNest", fmt.Sprintf("%d item(s) were skipped (links or unsafe paths).", len(res.Skipped)), walk.MsgBoxIconWarning)
			}
		}
	}
	return 0
}

// attachConsole connects stdout/stderr to the console that started us, so
// "ZipNest.exe x file.rar" prints output when run from cmd.exe or PowerShell.
func attachConsole() {
	k := syscall.NewLazyDLL("kernel32.dll")
	r, _, _ := k.NewProc("AttachConsole").Call(uintptr(^uint32(0))) // ATTACH_PARENT_PROCESS
	if r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = f
	}
}
