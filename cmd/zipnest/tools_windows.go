//go:build windows

package main

// Find (search inside the open archive or below the current folder) and
// Convert (repack archives into another format).

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"zipnest/core"
)

// ---------------------------------------------------------------- Find

type searchState struct {
	active  bool
	pattern string
	inner   string // archive folder to return to
}

// matcher returns a case-insensitive name test: wildcards (* ?) when present,
// otherwise "contains".
func matcher(pattern string) func(name string) bool {
	p := strings.ToLower(strings.TrimSpace(pattern))
	if p == "" || p == "*" || p == "*.*" {
		return func(string) bool { return true }
	}
	if strings.ContainsAny(p, "*?[") {
		return func(name string) bool {
			ok, _ := path.Match(p, strings.ToLower(name))
			return ok
		}
	}
	return func(name string) bool { return strings.Contains(strings.ToLower(name), p) }
}

func findDialog(owner walk.Form, where string) (string, bool) {
	var dlg *walk.Dialog
	var edit *walk.LineEdit
	var okBtn, cancelBtn *walk.PushButton
	value, ok := "", false
	err := Dialog{
		AssignTo:      &dlg,
		Font:          Font{Family: "Segoe UI", PointSize: 9},
		Title:         "Find files",
		Icon:          appIcon(),
		DefaultButton: &okBtn,
		CancelButton:  &cancelBtn,
		MinSize:       Size{Width: 440},
		Layout:        VBox{},
		Children: []Widget{
			Label{Text: "Look in: " + where},
			Label{Text: "File name or part of it (wildcards * and ? allowed):"},
			LineEdit{AssignTo: &edit, Text: getSetting("find.last", "")},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{AssignTo: &okBtn, Text: "Find", OnClicked: func() { value, ok = edit.Text(), true; dlg.Accept() }},
					PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(owner)
	if err != nil {
		return "", false
	}
	edit.SetFocus()
	dlg.Run()
	if ok {
		putString("find.last", value)
		saveSettings()
	}
	return value, ok
}

func (a *App) cmdFind() {
	where := a.dir
	if a.inArchive() {
		where = filepath.Base(a.archive) + " (whole archive)"
	} else if where == "" {
		walk.MsgBox(a.mw, "ZipNest", "Open a folder or an archive first.", walk.MsgBoxIconInformation)
		return
	}
	pattern, ok := findDialog(a.mw, where)
	if !ok {
		return
	}
	match := matcher(pattern)
	var rows []row
	if a.inArchive() {
		for _, e := range a.entries {
			if e.Dir || !match(path.Base(e.Path)) {
				continue
			}
			r := row{name: e.Path, size: e.Size, packed: e.Packed, full: e.Path, encrypted: e.Encrypted,
				icon: iconFor(e.Path, false), typ: typeName(e.Path, false)}
			if e.Modified != 0 {
				r.mtime = time.UnixMilli(e.Modified)
			}
			rows = append(rows, r)
		}
		a.search = searchState{active: true, pattern: pattern, inner: a.inner}
		a.inner = ""
	} else {
		root := a.dir
		const limit = 20000
		err := runTask(a.mw, "Searching", func(p core.ProgressFunc) error {
			return filepath.WalkDir(root, func(full string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil // unreadable folder: skip
				}
				if d.IsDir() {
					if !p(full, 0, 0) {
						return core.ErrCancelled
					}
					return nil
				}
				if !match(d.Name()) {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return nil
				}
				rel, _ := filepath.Rel(root, full)
				rows = append(rows, row{name: rel, size: info.Size(), packed: -1, mtime: info.ModTime(), full: full,
					icon: full, typ: typeName(d.Name(), false), isArchive: core.IsArchiveName(d.Name())})
				if len(rows) >= limit {
					return fs.SkipAll
				}
				return nil
			})
		})
		if err != nil && !errors.Is(err, core.ErrCancelled) {
			a.errorBox(err)
			return
		}
		a.search = searchState{active: true, pattern: pattern}
	}
	back := row{name: "..", up: true, icon: iconFor("", true), typ: "Back"}
	a.setRows(append([]row{back}, rows...))
	a.addr.SetText(fmt.Sprintf("Search results for \"%s\" in %s: %d file(s)", pattern, where, len(rows)))
	a.updateStatus()
}

// endSearch returns from the results to where the search started.
func (a *App) endSearch() {
	s := a.search
	a.search = searchState{}
	if a.inArchive() {
		a.showArchiveDir(s.inner)
	} else {
		a.showDir(a.dir)
	}
}

// activateSearchResult opens the folder that contains the result and selects it.
func (a *App) activateSearchResult(r row) {
	a.search = searchState{}
	if a.inArchive() {
		dir := ""
		if i := strings.LastIndex(r.full, "/"); i >= 0 {
			dir = r.full[:i]
		}
		a.showArchiveDir(dir)
		a.selectName(path.Base(r.full))
		return
	}
	a.showDir(filepath.Dir(r.full))
	a.selectName(filepath.Base(r.full))
}

// ---------------------------------------------------------------- Convert

var convertFormats = []core.Format{core.FormatNest, core.FormatZip, core.Format7z, core.FormatTarGz, core.FormatTarXz, core.FormatTarZst}

type convertSettings struct {
	format      core.Format
	level       int
	password    string
	encNames    bool
	deleteAfter bool
}

func convertDialog(owner walk.Form, count int) (*convertSettings, bool) {
	var dlg *walk.Dialog
	var fmtBox, levelBox *walk.ComboBox
	var pw *walk.LineEdit
	var encChk, delChk *walk.CheckBox
	var okBtn, cancelBtn *walk.PushButton
	var res *convertSettings
	names := []string{"NEST (ZipNest)", "ZIP", "7Z", "TAR.GZ", "TAR.XZ", "TAR.ZST"}
	err := Dialog{
		AssignTo:      &dlg,
		Font:          Font{Family: "Segoe UI", PointSize: 9},
		Title:         fmt.Sprintf("Convert %d archive(s)", count),
		Icon:          appIcon(),
		DefaultButton: &okBtn,
		CancelButton:  &cancelBtn,
		MinSize:       Size{Width: 420},
		Layout:        VBox{Alignment: AlignHNearVNear},
		Children: []Widget{
			Label{Text: "Each archive is repacked next to the original, with the new extension."},
			Composite{
				Layout: Grid{Columns: 2, MarginsZero: true},
				Children: []Widget{
					Label{Text: "New format:"},
					ComboBox{AssignTo: &fmtBox, Model: names, CurrentIndex: intSetting("convert.format2", 2, len(names)),
						OnCurrentIndexChanged: func() {
							f := convertFormats[max(0, fmtBox.CurrentIndex())]
							pw.SetEnabled(f == core.FormatZip || f == core.Format7z || f == core.FormatNest)
							encChk.SetEnabled(f == core.Format7z || f == core.FormatNest)
						}},
					Label{Text: "Compression:"},
					ComboBox{AssignTo: &levelBox, Model: levelNames, CurrentIndex: intSetting("add.level", 3, len(levelNames))},
					Label{Text: "New password (optional):"},
					LineEdit{AssignTo: &pw, PasswordMode: true},
				},
			},
			CheckBox{AssignTo: &encChk, Text: "Encrypt file names (7Z)"},
			CheckBox{AssignTo: &delChk, Text: "Move the original archives to the Recycle Bin after converting"},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{AssignTo: &okBtn, Text: "Convert", OnClicked: func() {
						f := convertFormats[max(0, fmtBox.CurrentIndex())]
						if encChk.Checked() && pw.Text() == "" {
							walk.MsgBox(dlg, "ZipNest", "Enter a password to encrypt the file names.", walk.MsgBoxIconWarning)
							return
						}
						putSetting("convert.format2", max(0, fmtBox.CurrentIndex()))
						saveSettings()
						res = &convertSettings{format: f, level: levelValues[max(0, levelBox.CurrentIndex())],
							password: pw.Text(), encNames: encChk.Checked() && (f == core.Format7z || f == core.FormatNest), deleteAfter: delChk.Checked()}
						dlg.Accept()
					}},
					PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(owner)
	if err != nil {
		return nil, false
	}
	f := convertFormats[max(0, fmtBox.CurrentIndex())]
	pw.SetEnabled(f == core.FormatZip || f == core.Format7z || f == core.FormatNest)
	encChk.SetEnabled(f == core.Format7z || f == core.FormatNest)
	dlg.Run()
	return res, res != nil
}

func (a *App) cmdConvert() {
	targets := a.selectedArchives()
	if a.inArchive() {
		targets = []string{a.archive}
	}
	if len(targets) == 0 {
		walk.MsgBox(a.mw, "ZipNest", "Select the archives you want to convert.", walk.MsgBoxIconInformation)
		return
	}
	if done := convertArchives(a.form(), targets, a); done > 0 && !a.inArchive() {
		a.showDir(a.dir)
	}
}

// convertArchives converts each archive; returns how many succeeded.
func convertArchives(owner walk.Form, targets []string, a *App) int {
	s, ok := convertDialog(owner, len(targets))
	if !ok {
		return 0
	}
	if a == nil {
		a = &App{model: &listModel{}}
	}
	var report []string
	done := 0
	for _, src := range targets {
		dst := filepath.Join(filepath.Dir(src), core.ArchiveStem(src)+"."+string(s.format))
		if strings.EqualFold(dst, src) {
			dst = filepath.Join(filepath.Dir(src), core.ArchiveStem(src)+" (converted)."+string(s.format))
		}
		if _, err := os.Stat(dst); err == nil {
			if walk.MsgBox(owner, "ZipNest", filepath.Base(dst)+" already exists. Replace it?",
				walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
				continue
			}
		}
		pw := ""
		if src == a.archive {
			pw = a.password
		}
		err := a.withPasswordOwner(owner, src, &pw, "Converting "+filepath.Base(src), func(pw string, p core.ProgressFunc) error {
			return core.Convert(src, dst, core.ConvertOptions{Password: pw, Create: core.CreateOptions{
				Format: s.format, Level: s.level, Password: s.password, EncryptNames: s.encNames, Progress: p}})
		})
		switch {
		case errors.Is(err, core.ErrCancelled):
			return done
		case err != nil:
			report = append(report, filepath.Base(src)+": "+err.Error())
		default:
			done++
			before, after := fileSize(src), fileSize(dst)
			line := fmt.Sprintf("%s → %s (%s → %s)", filepath.Base(src), filepath.Base(dst), shortSize(before), shortSize(after))
			report = append(report, line)
			if s.deleteAfter && src != a.archive {
				recycle(0, []string{src})
			}
		}
	}
	if len(report) > 0 {
		icon := walk.MsgBoxIconInformation
		if done < len(targets) {
			icon = walk.MsgBoxIconWarning
		}
		walk.MsgBox(owner, "Convert archives", strings.Join(report, "\n"), icon)
	}
	return done
}

func fileSize(p string) int64 {
	if st, err := os.Stat(p); err == nil {
		return st.Size()
	}
	return 0
}

func getSetting(key, def string) string {
	if s := walk.App().Settings(); s != nil {
		if v, ok := s.Get(key); ok {
			return v
		}
	}
	return def
}

func putString(key, v string) {
	if s := walk.App().Settings(); s != nil {
		s.Put(key, v)
	}
}

// ---------------------------------------------------------------- Repair

func (a *App) cmdRepair() {
	targets := a.selectedArchives()
	if a.inArchive() {
		targets = []string{a.archive}
	}
	if len(targets) == 0 {
		walk.MsgBox(a.mw, "ZipNest", "Select a .nest archive to repair.", walk.MsgBoxIconInformation)
		return
	}
	repairArchive(a.form(), targets[0])
	if !a.inArchive() {
		a.showDir(a.dir)
	}
}

func repairArchive(owner walk.Form, arc string) {
	if f, _ := core.Detect(arc); f != core.FormatNest {
		walk.MsgBox(owner, "Repair", "Repair works with ZipNest archives (.nest) that have a recovery record.\n\n"+
			"Tip: create .nest archives with \"Add recovery record\" to protect important files.", walk.MsgBoxIconInformation)
		return
	}
	var res *core.RepairResult
	err := runTask(owner, "Repairing "+filepath.Base(arc), func(p core.ProgressFunc) error {
		var err error
		res, err = core.Repair(arc, "", p)
		return err
	})
	switch {
	case errors.Is(err, core.ErrCancelled):
	case errors.Is(err, core.ErrNoRecovery):
		walk.MsgBox(owner, "Repair", filepath.Base(arc)+" has no recovery record, so damaged parts cannot be rebuilt.", walk.MsgBoxIconWarning)
	case err != nil:
		walk.MsgBox(owner, "Repair", err.Error(), walk.MsgBoxIconError)
	case res.Damaged == 0:
		walk.MsgBox(owner, "Repair", filepath.Base(arc)+" is not damaged. No repair needed.", walk.MsgBoxIconInformation)
	default:
		walk.MsgBox(owner, "Repair", fmt.Sprintf("Repaired %d damaged part(s).\n\nThe repaired archive was saved as:\n%s",
			res.Repaired, res.Output), walk.MsgBoxIconInformation)
	}
}
