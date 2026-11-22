//go:build windows

package main

import (
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"

	"zipnest/core"
)

var formatNames = []string{"NEST (ZipNest)", "ZIP", "7Z", "TAR", "TAR.GZ", "TAR.XZ", "TAR.ZST"}
var formatValues = []core.Format{core.FormatNest, core.FormatZip, core.Format7z, core.FormatTar, core.FormatTarGz, core.FormatTarXz, core.FormatTarZst}

var recoveryNames = []string{"1 %", "3 %", "5 %", "10 %"}
var recoveryValues = []int{1, 3, 5, 10}
var levelNames = []string{"Store (no compression)", "Fastest", "Fast", "Normal", "Good", "Best"}
var levelValues = []int{0, 1, 3, 6, 8, 9}

// Volume size presets for "Split to volumes".
var volumeNames = []string{"Don't split", "10 MB", "100 MB", "700 MB (CD)", "1 GB", "4092 MB (FAT32)", "4.7 GB (DVD)", "25 GB (Blu-ray)"}
var volumeValues = []int64{0, 10 << 20, 100 << 20, 700 << 20, 1 << 30, 4092 << 20, 4700 * 1000 * 1000, 25 * 1000 * 1000 * 1000}

type addSettings struct {
	archive     string
	format      core.Format
	level       int
	password    string
	encNames    bool
	volume      int64
	sfx         bool
	recovery    int
	lock        bool
	deleteAfter bool
}

// addDialog asks for the archive name and options ("Archive name and parameters").
func addDialog(owner walk.Form, suggested string) (*addSettings, bool) {
	var dlg *walk.Dialog
	var nameEdit, pw1, pw2 *walk.LineEdit
	var fmtBox, levelBox, volBox *walk.ComboBox
	var delChk, showChk, sfxChk, encChk, recChk, lockChk *walk.CheckBox
	var recBox *walk.ComboBox
	var okBtn, cancelBtn *walk.PushButton
	var res *addSettings

	format := func() core.Format { return formatValues[max(0, fmtBox.CurrentIndex())] }
	syncExt := func() {
		name := strings.TrimSpace(nameEdit.Text())
		stem := name
		if core.FormatFromName(name) != "" || strings.HasSuffix(strings.ToLower(name), ".exe") {
			stem = name[:len(name)-len(extOf(name))]
		}
		ext := "." + string(format())
		if sfxChk.Checked() {
			ext = ".exe"
		}
		nameEdit.SetText(stem + ext)
	}
	updateOptions := func() {
		f := format()
		sfxChk.SetEnabled(f == core.FormatZip && len(sfxStub) > 0 && volBox.CurrentIndex() <= 0)
		if !sfxChk.Enabled() {
			sfxChk.SetChecked(false)
		}
		encChk.SetEnabled(f == core.Format7z || f == core.FormatNest)
		if !encChk.Enabled() {
			encChk.SetChecked(false)
		}
		nest := f == core.FormatNest
		recChk.SetEnabled(nest)
		lockChk.SetEnabled(nest)
		recBox.SetEnabled(nest && recChk.Checked())
		if !nest {
			recChk.SetChecked(false)
			lockChk.SetChecked(false)
		}
		pwOK := f == core.FormatZip || f == core.Format7z || nest
		pw1.SetEnabled(pwOK)
		pw2.SetEnabled(pwOK)
	}

	err := Dialog{
		AssignTo:      &dlg,
		Font:          Font{Family: "Segoe UI", PointSize: 9},
		Title:         "Archive name and parameters",
		Icon:          appIcon(),
		DefaultButton: &okBtn,
		CancelButton:  &cancelBtn,
		MinSize:       Size{Width: 600},
		Layout:        VBox{},
		Children: []Widget{
			Label{Text: "Archive name:"},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					LineEdit{AssignTo: &nameEdit, Text: suggested},
					PushButton{Text: "Browse...", OnClicked: func() {
						fd := walk.FileDialog{
							Title:          "Save archive as",
							Filter:         "Archives (*.zip;*.7z;*.tar*;*.exe)|*.zip;*.7z;*.tar;*.tar.gz;*.tar.xz;*.tar.zst;*.exe|All files|*.*",
							FilePath:       nameEdit.Text(),
							InitialDirPath: filepath.Dir(nameEdit.Text()),
						}
						if ok, _ := fd.ShowSave(dlg); ok {
							nameEdit.SetText(fd.FilePath)
						}
					}},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					// Left column: format and compression.
					Composite{
						Layout: VBox{MarginsZero: true},
						Children: []Widget{
							GroupBox{
								Title:  "Archive format",
								Layout: Grid{Columns: 2},
								Children: []Widget{
									Label{Text: "Format:"},
									ComboBox{AssignTo: &fmtBox, Model: formatNames, CurrentIndex: intSetting("add.format3", 1, len(formatNames)),
										OnCurrentIndexChanged: func() { updateOptions(); syncExt() }},
									Label{Text: "Compression:"},
									ComboBox{AssignTo: &levelBox, Model: levelNames, CurrentIndex: intSetting("add.level", 3, len(levelNames))},
									Label{Text: "Split to volumes:"},
									ComboBox{AssignTo: &volBox, Model: volumeNames, CurrentIndex: 0,
										OnCurrentIndexChanged: func() { updateOptions() }},
								},
							},
							GroupBox{
								Title:  "Archiving options",
								Layout: VBox{Alignment: AlignHNearVNear},
								Children: []Widget{
									CheckBox{AssignTo: &sfxChk, Text: "Create self-extracting archive (.exe)", OnCheckedChanged: func() { syncExt() }},
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											CheckBox{AssignTo: &recChk, Text: "Add recovery record (NEST)", Checked: getSetting("add.rr", "") == "1",
												OnCheckedChanged: func() { recBox.SetEnabled(recChk.Enabled() && recChk.Checked()) }},
											ComboBox{AssignTo: &recBox, Model: recoveryNames, CurrentIndex: intSetting("add.rrpct", 1, len(recoveryNames)), MaxSize: Size{Width: 70}},
										},
									},
									CheckBox{AssignTo: &lockChk, Text: "Lock archive (NEST)"},
									CheckBox{AssignTo: &delChk, Text: "Delete files after archiving"},
								},
							},
						},
					},
					// Right column: password.
					GroupBox{
						Title:  "Password (AES-256)",
						Layout: Grid{Columns: 1, Alignment: AlignHNearVNear},
						Children: []Widget{
							Label{Text: "Enter password:"},
							LineEdit{AssignTo: &pw1, PasswordMode: true},
							Label{Text: "Reenter password:"},
							LineEdit{AssignTo: &pw2, PasswordMode: true},
							CheckBox{AssignTo: &showChk, Text: "Show password", OnCheckedChanged: func() {
								pw1.SetPasswordMode(!showChk.Checked())
								pw2.SetPasswordMode(!showChk.Checked())
							}},
							CheckBox{AssignTo: &encChk, Text: "Encrypt file names (7Z, NEST)"},
							VSpacer{},
						},
					},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{AssignTo: &okBtn, Text: "OK", OnClicked: func() {
						name := strings.TrimSpace(nameEdit.Text())
						if name == "" {
							walk.MsgBox(dlg, "ZipNest", "Please enter an archive name.", walk.MsgBoxIconWarning)
							return
						}
						f := format()
						if pw1.Text() != pw2.Text() {
							walk.MsgBox(dlg, "ZipNest", "The passwords do not match.", walk.MsgBoxIconWarning)
							return
						}
						if encChk.Checked() && pw1.Text() == "" {
							walk.MsgBox(dlg, "ZipNest", "Enter a password to encrypt the file names.", walk.MsgBoxIconWarning)
							return
						}
						if sfxChk.Checked() {
							if !strings.HasSuffix(strings.ToLower(name), ".exe") {
								name += ".exe"
							}
						} else if core.FormatFromName(name) == "" {
							name += "." + string(f)
						}
						putSetting("add.format3", max(0, fmtBox.CurrentIndex()))
						if recChk.Checked() {
							putString("add.rr", "1")
						} else {
							putString("add.rr", "")
						}
						putSetting("add.rrpct", max(0, recBox.CurrentIndex()))
						putSetting("add.level", max(0, levelBox.CurrentIndex()))
						saveSettings()
						res = &addSettings{
							archive:  name,
							format:   f,
							level:    levelValues[max(0, levelBox.CurrentIndex())],
							password: pw1.Text(),
							encNames: encChk.Checked(),
							volume:   volumeValues[max(0, volBox.CurrentIndex())],
							sfx:      sfxChk.Checked(),
							lock:     lockChk.Checked(),
							recovery: func() int {
								if recChk.Checked() {
									return recoveryValues[max(0, recBox.CurrentIndex())]
								}
								return 0
							}(),
							deleteAfter: delChk.Checked(),
						}
						dlg.Accept()
					}},
					PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(owner)
	if err != nil {
		walk.MsgBox(owner, "ZipNest", err.Error(), walk.MsgBoxIconError)
		return nil, false
	}
	updateOptions()
	if fmtBox.CurrentIndex() > 0 {
		syncExt() // remembered format: give the suggested name the matching extension
	}
	if dlg.Run() != walk.DlgCmdOK || res == nil {
		return nil, false
	}
	return res, true
}

func extOf(name string) string {
	n := strings.ToLower(name)
	if strings.HasSuffix(n, ".exe") {
		return name[len(name)-4:]
	}
	for _, f := range formatValues {
		if strings.HasSuffix(n, "."+string(f)) {
			return "." + string(f)
		}
	}
	return filepath.Ext(name)
}

// extractDialog asks for the destination folder and overwrite mode.
func extractDialog(owner walk.Form, dest string) (string, string, bool) {
	var dlg *walk.Dialog
	var destEdit *walk.LineEdit
	var modeBox *walk.ComboBox
	var okBtn, cancelBtn *walk.PushButton
	modes := []string{"overwrite", "skip", "rename"}
	accepted := false
	var outDest, outMode string
	err := Dialog{
		AssignTo:      &dlg,
		Font:          Font{Family: "Segoe UI", PointSize: 9},
		Title:         "Extraction path and options",
		DefaultButton: &okBtn,
		CancelButton:  &cancelBtn,
		MinSize:       Size{Width: 520},
		Layout:        VBox{},
		Children: []Widget{
			Label{Text: "Destination path (will be created if it does not exist):"},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					LineEdit{AssignTo: &destEdit, Text: dest},
					PushButton{Text: "Browse...", OnClicked: func() {
						fd := walk.FileDialog{Title: "Extract to", FilePath: destEdit.Text()}
						if ok, _ := fd.ShowBrowseFolder(dlg); ok {
							destEdit.SetText(fd.FilePath)
						}
					}},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{Text: "When a file already exists:"},
					ComboBox{AssignTo: &modeBox, CurrentIndex: 0,
						Model: []string{"Overwrite it", "Skip it", "Keep both (rename new file)"}},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{AssignTo: &okBtn, Text: "OK", OnClicked: func() {
						outDest = strings.TrimSpace(destEdit.Text())
						if outDest == "" {
							return
						}
						outMode = modes[max(0, modeBox.CurrentIndex())]
						accepted = true
						dlg.Accept()
					}},
					PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(owner)
	if err != nil {
		walk.MsgBox(owner, "ZipNest", err.Error(), walk.MsgBoxIconError)
		return "", "", false
	}
	dlg.Run()
	if !accepted {
		return "", "", false
	}
	return outDest, outMode, true
}

// passwordDialog asks for a password.
func passwordDialog(owner walk.Form, archive, message string) (string, bool) {
	var dlg *walk.Dialog
	var pw *walk.LineEdit
	var show *walk.CheckBox
	var okBtn, cancelBtn *walk.PushButton
	accepted := false
	value := ""
	err := Dialog{
		AssignTo:      &dlg,
		Font:          Font{Family: "Segoe UI", PointSize: 9},
		Title:         "Enter password",
		DefaultButton: &okBtn,
		CancelButton:  &cancelBtn,
		MinSize:       Size{Width: 420},
		Layout:        VBox{},
		Children: []Widget{
			Label{Text: message},
			Label{Text: "for the archive " + filepath.Base(archive)},
			LineEdit{AssignTo: &pw, PasswordMode: true},
			CheckBox{AssignTo: &show, Text: "Show password", OnCheckedChanged: func() {
				pw.SetPasswordMode(!show.Checked())
			}},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{AssignTo: &okBtn, Text: "OK", OnClicked: func() { value = pw.Text(); accepted = true; dlg.Accept() }},
					PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(owner)
	if err != nil {
		return "", false
	}
	pw.SetFocus()
	dlg.Run()
	return value, accepted
}

// runTask runs fn in the background. If it takes longer than a moment, a modal
// progress window (with Cancel) is shown until it finishes.
func runTask(owner walk.Form, title string, fn func(progress core.ProgressFunc) error) error {
	type state struct {
		file        string
		done, total int64
	}
	var cancelled, finished atomic.Bool
	var latest atomic.Pointer[state]
	progress := func(file string, done, total int64) bool {
		if cancelled.Load() {
			return false
		}
		latest.Store(&state{file, done, total})
		return true
	}
	resCh := make(chan error, 1)
	start := time.Now()
	go func() { resCh <- fn(progress) }()

	// Quick operations finish without any window (no flicker).
	select {
	case err := <-resCh:
		return err
	case <-time.After(300 * time.Millisecond):
	}

	var dlg *walk.Dialog
	var lbl, pct *walk.Label
	var pb *walk.ProgressBar
	var cancelBtn *walk.PushButton
	err := Dialog{
		AssignTo:     &dlg,
		Font:         Font{Family: "Segoe UI", PointSize: 9},
		Title:        title,
		CancelButton: &cancelBtn,
		FixedSize:    true,
		MinSize:      Size{Width: 460},
		Layout:       VBox{},
		Children: []Widget{
			Label{AssignTo: &lbl, Text: "Please wait...", EllipsisMode: EllipsisPath},
			ProgressBar{AssignTo: &pb, MinValue: 0, MaxValue: 1000},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{AssignTo: &pct, Text: ""},
					HSpacer{},
					PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() {
						cancelled.Store(true)
						cancelBtn.SetEnabled(false)
						lbl.SetText("Cancelling...")
					}},
				},
			},
		},
	}.Create(owner)
	if err != nil {
		// No window: just wait for the work.
		return <-resCh
	}
	dlg.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if !finished.Load() {
			*canceled = true
			cancelled.Store(true)
		}
	})
	pb.SetMarqueeMode(true)

	update := func() {
		p := latest.Load()
		if p == nil {
			return
		}
		lbl.SetText(p.file)
		if p.total > 0 {
			pb.SetMarqueeMode(false)
			v := int(p.done * 1000 / p.total)
			pb.SetValue(v)
			speed := ""
			if el := time.Since(start).Seconds(); el > 1 && p.done > 0 {
				rate := float64(p.done) / el
				speed = "   •   " + shortSize(int64(rate)) + "/s"
				if left := float64(p.total-p.done) / rate; left >= 1 {
					speed += "   •   " + timeLeft(left)
				}
			}
			pct.SetText(itoa(v/10) + "%   •   " + shortSize(p.done) + " of " + shortSize(p.total) + speed)
		} else if p.done > 0 {
			pct.SetText(shortSize(p.done))
		}
	}

	var result error
	// The watcher starts once the modal loop runs (closing a dialog before Run()
	// would leave the owner disabled) and keeps it open briefly so walk's
	// asynchronous layout has finished before the window is destroyed.
	dlg.Starting().Attach(func() {
		shown := time.Now()
		go func() {
			tick := time.NewTicker(120 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case err := <-resCh:
					result = err
					if d := time.Since(shown); d < 500*time.Millisecond {
						time.Sleep(500*time.Millisecond - d)
					}
					finished.Store(true)
					dlg.Synchronize(func() { dlg.Close(walk.DlgCmdOK) })
					return
				case <-tick.C:
					dlg.Synchronize(update)
				}
			}
		}()
	})
	dlg.Run()
	if owner != nil {
		win.EnableWindow(owner.Handle(), true)
	}
	if cancelled.Load() && result != nil && errors.Is(result, core.ErrCancelled) {
		return core.ErrCancelled
	}
	return result
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}

// timeLeft formats a remaining time estimate.
func timeLeft(sec float64) string {
	switch {
	case sec < 60:
		return itoa(int(sec)+1) + " s left"
	case sec < 3600:
		return itoa(int(sec/60)+1) + " min left"
	}
	return itoa(int(sec/3600)) + " h " + itoa(int(sec/60)%60) + " min left"
}
