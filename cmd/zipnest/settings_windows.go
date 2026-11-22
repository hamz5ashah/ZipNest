//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"zipnest/core"
)

// Exit-code bits returned by "ZipNest.exe --setup-dialog" to the installer.
const (
	setupDesktop   = 1
	setupStartMenu = 2
)

// settingsDialog shows "ZipNest Setup": file associations, interface and shell
// integration options (modelled on the classic archiver setup screen).
// installer=true adds the Interface group and returns the shortcut choices.
func settingsDialog(owner walk.Form) { runSettingsDialog(owner, false) }

func runSettingsDialog(owner walk.Form, installer bool) int {
	in := loadIntegration()
	var dlg *walk.Dialog
	var okBtn, cancelBtn *walk.PushButton
	var shellChk, cascChk, iconChk, win11Chk, deskChk, startChk *walk.CheckBox
	checks := make([]*walk.CheckBox, len(fileTypes))
	win11 := win11MenuAvailable()

	// Two columns, filled top-to-bottom like the classic layout.
	rows := (len(fileTypes) + 1) / 2
	var assocWidgets []Widget
	for r := 0; r < rows; r++ {
		for c := 0; c < 2; c++ {
			i := c*rows + r
			if i >= len(fileTypes) {
				assocWidgets = append(assocWidgets, HSpacer{})
				continue
			}
			assocWidgets = append(assocWidgets, CheckBox{AssignTo: &checks[i], Text: fileTypes[i].Label, Checked: in.Assoc[fileTypes[i].Label]})
		}
	}
	assocWidgets = append(assocWidgets, PushButton{Text: "Toggle all", ColumnSpan: 2, OnClicked: func() {
		all := true
		for _, c := range checks {
			all = all && c.Checked()
		}
		for _, c := range checks {
			c.SetChecked(!all)
		}
	}})

	updateShell := func() {
		on := shellChk.Checked()
		cascChk.SetEnabled(on)
		iconChk.SetEnabled(on)
		win11Chk.SetEnabled(on && win11)
	}

	var right []Widget
	if installer {
		right = append(right, GroupBox{
			Title:         "Interface",
			StretchFactor: 1,
			Layout:        VBox{Alignment: AlignHNearVNear},
			Children: []Widget{
				CheckBox{AssignTo: &deskChk, Text: "Add ZipNest to Desktop", Checked: true},
				CheckBox{AssignTo: &startChk, Text: "Add ZipNest to Start Menu", Checked: true},
			},
		})
	}
	win11Text := "Windows 11 menu (top level)"
	if !win11 {
		win11Text = "Windows 11 menu (Windows 11 only)"
	}
	right = append(right, GroupBox{
		Title:         "Shell integration",
		StretchFactor: 1,
		Layout:        VBox{Alignment: AlignHNearVNear},
		Children: []Widget{
			CheckBox{AssignTo: &shellChk, Text: "Integrate ZipNest into Explorer", Checked: in.Shell, OnCheckedChanged: func() { updateShell() }},
			CheckBox{AssignTo: &cascChk, Text: "Cascaded context menus", Checked: in.Cascaded},
			CheckBox{AssignTo: &iconChk, Text: "Icons in context menus", Checked: in.Icons},
			CheckBox{AssignTo: &win11Chk, Text: win11Text, Checked: in.Win11 && win11},
		},
	}, VSpacer{})

	title := "ZipNest Settings"
	if installer {
		title = "ZipNest Setup"
	}
	result := 0
	accepted := false
	collect := func() {
		for i, t := range fileTypes {
			in.Assoc[t.Label] = checks[i].Checked()
		}
		in.Shell = shellChk.Checked()
		in.Cascaded = cascChk.Checked()
		in.Icons = iconChk.Checked()
		if win11 {
			in.Win11 = win11Chk.Checked()
		}
		if installer {
			if deskChk.Checked() {
				result |= setupDesktop
			}
			if startChk.Checked() {
				result |= setupStartMenu
			}
		}
	}

	buttons := []Widget{HSpacer{}, PushButton{AssignTo: &okBtn, Text: "OK", MinSize: Size{Width: 90}, OnClicked: func() {
		collect()
		accepted = true
		dlg.Accept()
	}}}
	if !installer {
		buttons = append(buttons, PushButton{AssignTo: &cancelBtn, Text: "Cancel", MinSize: Size{Width: 90}, OnClicked: func() { dlg.Cancel() }})
	}

	err := Dialog{
		AssignTo:      &dlg,
		Font:          Font{Family: "Segoe UI", PointSize: 9},
		Title:         title,
		Icon:          appIcon(),
		DefaultButton: &okBtn,
		CancelButton:  &cancelBtn,
		MinSize:       Size{Width: 600},
		Layout:        VBox{},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					GroupBox{Title: "Associate ZipNest with", Layout: Grid{Columns: 2, Alignment: AlignHNearVNear}, Children: assocWidgets},
					Composite{Layout: Grid{Columns: 1, MarginsZero: true}, Children: right, MinSize: Size{Width: 290}},
				},
			},
			GroupBox{
				Layout: VBox{},
				Children: []Widget{
					TextLabel{MinSize: Size{Width: 560}, Text: "The first group chooses which archive types open in ZipNest when you double-click them. " +
						"Shell integration adds ZipNest to the right-click menu in File Explorer: \"Extract Here\", " +
						"\"Extract to <folder>\", \"Add to archive\" and more. \"Cascaded\" groups them in a ZipNest submenu; " +
						"on Windows 11 the ZipNest menu can also appear at the top level, without \"Show more options\"."},
				},
			},
			Composite{Layout: HBox{MarginsZero: true}, Children: buttons},
		},
	}.Create(owner)
	if err != nil {
		walk.MsgBox(owner, "ZipNest", err.Error(), walk.MsgBoxIconError)
		return 0
	}
	updateShell()
	if installer {
		// Closing the window keeps the shown choices, like pressing OK.
		dlg.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
			if !accepted {
				collect()
				accepted = true
			}
		})
	}
	dlg.Run()
	if !accepted {
		return 0
	}
	err = runTask(owner, "Applying settings", func(p core.ProgressFunc) error {
		p("Updating file associations and the Explorer menu...", 0, 0)
		return in.apply()
	})
	if err != nil {
		walk.MsgBox(owner, "ZipNest", "Some settings could not be applied:\n\n"+err.Error(), walk.MsgBoxIconWarning)
	}
	return result
}

// runSetup applies settings given on the command line (silent install):
// assoc=RAR,ZIP,...  shell=0|1  cascaded=0|1  icons=0|1  win11=0|1
// With no arguments the defaults (or the user's saved choices) are applied.
func runSetup(args []string) int {
	in := loadIntegration()
	for _, a := range args {
		k, v, _ := strings.Cut(a, "=")
		on := v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true")
		switch strings.ToLower(k) {
		case "assoc":
			for l := range in.Assoc {
				in.Assoc[l] = false
			}
			for _, l := range strings.Split(v, ",") {
				for _, t := range fileTypes {
					if strings.EqualFold(t.Label, l) {
						in.Assoc[t.Label] = true
					}
				}
			}
		case "shell":
			in.Shell = on
		case "cascaded":
			in.Cascaded = on
		case "icons":
			in.Icons = on
		case "win11":
			in.Win11 = on
		}
	}
	if err := in.apply(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// removeIntegration undoes everything (used by the uninstaller).
func removeIntegration() {
	if windowsBuild() >= 22000 {
		setWin11Menu(false)
	}
	unregisterShell()
	unassociateAll()
	deleteTree(`Applications\ZipNest.exe`)
	procSHChangeNotif.Call(0x08000000, 0, 0, 0)
}
