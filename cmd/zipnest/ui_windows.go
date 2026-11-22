//go:build windows

package main

// Look-and-feel pieces of the main window: the navigation pane (quick access,
// drives, recent archives), Back/Forward history, the archive info banner and
// the About window.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"zipnest/cli"
	"zipnest/core"
)

var (
	brandColor  = walk.RGB(107, 43, 224)
	bannerColor = walk.RGB(245, 240, 254)
	mutedColor  = walk.RGB(110, 108, 128)
)

// ---------------------------------------------------------------- navigation pane

type navItem struct {
	label  string
	path   string // empty for section headings
	header bool
}

type navModel struct {
	walk.TableModelBase
	items []navItem
}

func (m *navModel) RowCount() int { return len(m.items) }

func (m *navModel) Value(r, c int) interface{} {
	it := m.items[r]
	if it.header {
		return strings.ToUpper(it.label)
	}
	return it.label
}

func (m *navModel) Image(r int) interface{} {
	it := m.items[r]
	// A path that does not exist gives "no icon" (nil would show icon #0).
	if it.header || it.path == "" {
		return "\\?\no-icon"
	}
	if _, err := os.Stat(it.path); err != nil {
		return "\\?\no-icon"
	}
	return it.path
}

func knownFolder(name string) string {
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, name)
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		return p
	}
	return ""
}

func recentArchives() []string {
	var out []string
	for _, p := range strings.Split(getSetting("recent", ""), "|") {
		if p != "" {
			if _, err := os.Stat(p); err == nil {
				out = append(out, p)
			}
		}
	}
	return out
}

func addRecent(p string) {
	list := []string{p}
	for _, r := range recentArchives() {
		if !strings.EqualFold(r, p) && len(list) < 8 {
			list = append(list, r)
		}
	}
	putString("recent", strings.Join(list, "|"))
	saveSettings()
}

func buildNavItems() []navItem {
	home, _ := os.UserHomeDir()
	items := []navItem{{label: "Quick access", header: true}, {label: "Home", path: home}}
	for _, f := range []string{"Desktop", "Documents", "Downloads", "Pictures", "Music", "Videos"} {
		if p := knownFolder(f); p != "" {
			items = append(items, navItem{label: f, path: p})
		}
	}
	items = append(items, navItem{label: "This PC", header: true})
	for c := 'A'; c <= 'Z'; c++ {
		d := string(c) + `:\`
		if _, err := os.Stat(d); err == nil {
			items = append(items, navItem{label: "Drive " + string(c) + ":", path: d})
		}
	}
	if rec := recentArchives(); len(rec) > 0 {
		items = append(items, navItem{label: "Recent archives", header: true})
		for _, p := range rec {
			items = append(items, navItem{label: filepath.Base(p), path: p})
		}
	}
	return items
}

func (a *App) refreshNav() {
	if a.nav == nil {
		return
	}
	a.navModel.items = buildNavItems()
	a.navModel.PublishRowsReset()
}

func (a *App) navClicked() {
	i := a.nav.CurrentIndex()
	if i < 0 || i >= len(a.navModel.items) || a.navBusy {
		return
	}
	it := a.navModel.items[i]
	if it.header || it.path == "" {
		return
	}
	a.navBusy = true
	defer func() { a.navBusy = false }()
	if isDir(it.path) {
		a.showDir(it.path)
	} else {
		a.dir = filepath.Dir(it.path)
		a.openArchive(it.path)
	}
}

func (a *App) styleNav(style *walk.CellStyle) {
	r := style.Row()
	if r < 0 || r >= len(a.navModel.items) {
		return
	}
	if a.navModel.items[r].header {
		style.TextColor = mutedColor
		if a.navHeaderFont != nil {
			style.Font = a.navHeaderFont
		}
	}
}

// ---------------------------------------------------------------- history

type location struct {
	dir, archive, inner string
}

func (a *App) currentLocation() location {
	return location{dir: a.dir, archive: a.archive, inner: a.inner}
}

// remember records the location we are leaving (called before every navigation
// that is not Back/Forward itself).
func (a *App) remember(next location) {
	cur := a.currentLocation()
	if a.historyBusy || cur == next || (cur.dir == "" && cur.archive == "") {
		return
	}
	a.back = append(a.back, cur)
	if len(a.back) > 100 {
		a.back = a.back[1:]
	}
	a.forward = nil
	a.updateNavButtons()
}

func (a *App) goTo(l location) {
	a.historyBusy = true
	defer func() { a.historyBusy = false }()
	switch {
	case l.archive != "" && strings.EqualFold(l.archive, a.archive):
		a.showArchiveDir(l.inner)
	case l.archive != "":
		a.dir = filepath.Dir(l.archive)
		a.openArchive(l.archive)
		if a.inArchive() {
			a.showArchiveDir(l.inner)
		}
	case l.dir != "":
		a.showDir(l.dir)
	default:
		a.showDrives()
	}
}

func (a *App) goBack() {
	if len(a.back) == 0 {
		return
	}
	l := a.back[len(a.back)-1]
	a.back = a.back[:len(a.back)-1]
	a.forward = append(a.forward, a.currentLocation())
	a.goTo(l)
	a.updateNavButtons()
}

func (a *App) goForward() {
	if len(a.forward) == 0 {
		return
	}
	l := a.forward[len(a.forward)-1]
	a.forward = a.forward[:len(a.forward)-1]
	a.back = append(a.back, a.currentLocation())
	a.goTo(l)
	a.updateNavButtons()
}

func (a *App) updateNavButtons() {
	if a.backBtn != nil {
		a.backBtn.SetEnabled(len(a.back) > 0)
		a.fwdBtn.SetEnabled(len(a.forward) > 0)
	}
}

// ---------------------------------------------------------------- archive banner

func (a *App) updateBanner() {
	if a.banner == nil {
		return
	}
	if !a.inArchive() || a.search.active {
		a.banner.SetVisible(false)
		return
	}
	var files, dirs, enc int
	var size, packed int64
	for _, e := range a.entries {
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
	arcSize := fileSize(a.archive)
	parts := []string{strings.ToUpper(string(a.format)) + " archive",
		fmt.Sprintf("%d file(s) in %d folder(s)", files, dirs),
		fmt.Sprintf("%s → %s", shortSize(size), shortSize(arcSize))}
	if size > 0 && arcSize > 0 {
		parts = append(parts, fmt.Sprintf("ratio %d%%", arcSize*100/size))
	}
	if enc > 0 {
		parts = append(parts, "password protected")
	}
	if a.format == core.FormatNest {
		if d, err := core.NestInfo(a.archive, a.password); err == nil {
			if d.RecoveryPercent > 0 {
				parts = append(parts, fmt.Sprintf("recovery record %d%%", d.RecoveryPercent))
			}
			if d.Locked {
				parts = append(parts, "locked")
			}
			if d.Comment != "" {
				parts = append(parts, "“"+d.Comment+"”")
			}
		}
	}
	a.bannerTitle.SetText(filepath.Base(a.archive))
	a.bannerInfo.SetText(strings.Join(parts, "   •   "))
	a.banner.SetVisible(true)
}

func (a *App) closeArchive() {
	if !a.inArchive() {
		return
	}
	dir, name := filepath.Dir(a.archive), filepath.Base(a.archive)
	a.showDir(dir)
	a.selectName(name)
}

// extractAll extracts the whole open archive (Extract To dialog).
func (a *App) extractAll() {
	a.tv.SetSelectedIndexes(nil)
	inner := a.inner
	a.inner = ""
	a.extract(false)
	if a.inArchive() {
		a.showArchiveDir(inner)
	}
}

func bannerWidgets(a *App) Widget {
	return Composite{
		AssignTo:   &a.banner,
		Visible:    false,
		Background: SolidColorBrush{Color: bannerColor},
		Layout:     HBox{Margins: Margins{Left: 12, Top: 8, Right: 12, Bottom: 8}, Spacing: 12},
		Children: []Widget{
			ImageView{Image: loadBitmap("logo32"), MinSize: Size{Width: 32, Height: 32}, MaxSize: Size{Width: 32, Height: 32}},
			Composite{
				Layout: VBox{MarginsZero: true, SpacingZero: true},
				Children: []Widget{
					Label{AssignTo: &a.bannerTitle, Font: Font{Family: "Segoe UI", PointSize: 11, Bold: true}, TextColor: walk.RGB(40, 30, 70)},
					Label{AssignTo: &a.bannerInfo, TextColor: mutedColor},
				},
			},
			HSpacer{},
			PushButton{Text: "Extract all...", MinSize: Size{Width: 110, Height: 30}, OnClicked: a.extractAll},
			PushButton{Text: "Test", MinSize: Size{Width: 70, Height: 30}, OnClicked: a.cmdTest},
			PushButton{Text: "Close", MinSize: Size{Width: 70, Height: 30}, OnClicked: a.closeArchive},
		},
	}
}

// ---------------------------------------------------------------- About

func (a *App) about() {
	var dlg *walk.Dialog
	var okBtn *walk.PushButton
	Dialog{
		AssignTo:      &dlg,
		Font:          Font{Family: "Segoe UI", PointSize: 9},
		Title:         "About ZipNest",
		Icon:          appIcon(),
		DefaultButton: &okBtn,
		CancelButton:  &okBtn,
		FixedSize:     true,
		Layout:        VBox{Margins: Margins{Left: 20, Top: 16, Right: 20, Bottom: 12}, Spacing: 10},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 14},
				Children: []Widget{
					ImageView{Image: loadBitmap("logo48"), MinSize: Size{Width: 48, Height: 48}, MaxSize: Size{Width: 48, Height: 48}},
					Composite{
						Layout: VBox{MarginsZero: true, SpacingZero: true},
						Children: []Widget{
							Label{Text: "ZipNest", Font: Font{Family: "Segoe UI", PointSize: 16, Bold: true}, TextColor: brandColor},
							Label{Text: "Version " + cli.Version + "  •  free archive manager", TextColor: mutedColor},
						},
					},
					HSpacer{},
				},
			},
			Label{Text: "Open and extract:  ZIP, RAR (RAR4/RAR5), 7z, TAR, GZ, BZ2, XZ, ZST, split archives"},
			Label{Text: "Create:  ZIP and 7z (AES-256, hidden file names), TAR.GZ / XZ / ZST,"},
			Label{Text: "             split volumes, self-extracting .exe"},
			Label{Text: "Tools:  test, convert, find, Windows 11 context menu"},
			Label{Text: "RAR is a format of RARLAB; ZipNest only reads it.", TextColor: mutedColor},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{AssignTo: &okBtn, Text: "OK", MinSize: Size{Width: 90}, OnClicked: func() { dlg.Accept() }},
				},
			},
		},
	}.Run(a.mw)
}

// archiveEntriesSummary is used by the status bar.
func archiveEntriesSummary(entries []core.Entry) (files int, size int64) {
	for _, e := range entries {
		if !e.Dir {
			files++
			if e.Size > 0 {
				size += e.Size
			}
		}
	}
	return
}
