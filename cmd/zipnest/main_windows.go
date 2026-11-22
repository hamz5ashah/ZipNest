//go:build windows

// ZipNest desktop: a native Windows archive manager.
package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"

	"zipnest/cli"
	"zipnest/core"
)

//go:embed res/*.png
var resFS embed.FS

type App struct {
	mw     *walk.MainWindow
	tv     *walk.TableView
	addr   *walk.LineEdit
	status *walk.StatusBarItem
	model  *listModel

	dir      string // file system folder ("" = list of drives) when no archive is open
	archive  string // currently opened archive
	inner    string // folder inside the archive
	entries  []core.Entry
	format   core.Format
	password string
	search   searchState

	nav           *walk.TableView
	navModel      *navModel
	navBusy       bool
	navHeaderFont *walk.Font
	back, forward []location
	historyBusy   bool
	backBtn       *walk.ToolButton
	fwdBtn        *walk.ToolButton
	filter        *walk.LineEdit
	banner        *walk.Composite
	bannerTitle   *walk.Label
	bannerInfo    *walk.Label
}

func loadBitmap(name string) *walk.Bitmap {
	b, err := resFS.ReadFile("res/" + name + ".png")
	if err != nil {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	bmp, err := walk.NewBitmapFromImageForDPI(img, 96)
	if err != nil {
		return nil
	}
	return bmp
}

func appIcon() *walk.Icon {
	if ic, err := walk.NewIconFromResourceId(1); err == nil {
		return ic
	}
	return nil
}

// initSettings loads %APPDATA%\ZipNest\ZipNest\settings.ini (window size and
// position, column widths, last-used options).
func initSettings() {
	app := walk.App()
	app.SetOrganizationName("ZipNest")
	app.SetProductName("ZipNest")
	s := walk.NewIniFileSettings("settings.ini")
	s.Load()
	app.SetSettings(s)
}

func saveSettings() {
	if s, ok := walk.App().Settings().(*walk.IniFileSettings); ok {
		s.Save()
	}
}

func intSetting(key string, def, max int) int {
	if s := walk.App().Settings(); s != nil {
		if v, ok := s.Get(key); ok {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < max {
				return n
			}
		}
	}
	return def
}

func putSetting(key string, v int) {
	if s := walk.App().Settings(); s != nil {
		s.Put(key, strconv.Itoa(v))
	}
}

func main() {
	runtime.LockOSThread()
	initSettings()
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "--setup":
			// Used by the installer: --setup assoc=RAR,ZIP,... shell=1 cascaded=1 icons=1 win11=1
			os.Exit(runSetup(args[1:]))
		case "--setup-dialog":
			// Shown by the installer after copying files; exit code = shortcut choices.
			os.Exit(runSettingsDialog(nil, true))
		case "--remove-integration":
			// Used by the uninstaller.
			removeIntegration()
			os.Exit(0)
		case "--add", "--add-zip", "--extract-here", "--extract-auto", "--extract-dialog", "--test", "--convert":
			os.Exit(runShellCommand(args[0], args[1:]))
		case "l", "x", "e", "t", "a", "d", "c":
			// Command-line use from a console window.
			attachConsole()
			os.Exit(cli.Run(args, os.Stdin, os.Stdout, os.Stderr))
		}
	}
	a := &App{model: &listModel{}, navModel: &navModel{items: buildNavItems()}}
	a.run(args)
}

func (a *App) run(args []string) {
	start := ""
	if len(args) > 0 {
		start = args[0]
	}
	err := MainWindow{
		AssignTo:   &a.mw,
		Name:       "mainWindow",
		Persistent: true,
		Title:      "ZipNest",
		Icon:       appIcon(),
		Font:       Font{Family: "Segoe UI", PointSize: 9},
		Size:       Size{Width: 1120, Height: 700},
		Layout:     VBox{MarginsZero: true, SpacingZero: true},
		MenuItems: []MenuItem{
			Menu{Text: "&File", Items: []MenuItem{
				Action{Text: "&Open archive...", Shortcut: Shortcut{Modifiers: walk.ModControl, Key: walk.KeyO}, OnTriggered: a.cmdOpenArchive},
				Action{Text: "Go to &parent folder", Shortcut: Shortcut{Key: walk.KeyBack}, OnTriggered: a.goUp},
				Action{Text: "&Back", Shortcut: Shortcut{Modifiers: walk.ModAlt, Key: walk.KeyLeft}, OnTriggered: a.goBack},
				Action{Text: "&Forward", Shortcut: Shortcut{Modifiers: walk.ModAlt, Key: walk.KeyRight}, OnTriggered: a.goForward},
				Action{Text: "&Refresh", Shortcut: Shortcut{Key: walk.KeyF5}, OnTriggered: a.refresh},
				Action{Text: "Select &all", Shortcut: Shortcut{Modifiers: walk.ModControl, Key: walk.KeyA}, OnTriggered: a.selectAll},
				Separator{},
				Action{Text: "E&xit", OnTriggered: func() { a.mw.Close() }},
			}},
			Menu{Text: "&Commands", Items: []MenuItem{
				Action{Text: "&Add files to archive", Shortcut: Shortcut{Modifiers: walk.ModAlt, Key: walk.KeyA}, OnTriggered: a.cmdAdd},
				Action{Text: "&Extract to the specified folder", Shortcut: Shortcut{Modifiers: walk.ModAlt, Key: walk.KeyE}, OnTriggered: a.cmdExtract},
				Action{Text: "Extract &here", Shortcut: Shortcut{Modifiers: walk.ModAlt, Key: walk.KeyW}, OnTriggered: a.cmdExtractHere},
				Action{Text: "&Test archived files", Shortcut: Shortcut{Modifiers: walk.ModAlt, Key: walk.KeyT}, OnTriggered: a.cmdTest},
				Action{Text: "&View file", Shortcut: Shortcut{Modifiers: walk.ModAlt, Key: walk.KeyV}, OnTriggered: a.cmdView},
				Action{Text: "&Delete", Shortcut: Shortcut{Key: walk.KeyDelete}, OnTriggered: a.cmdDelete},
				Action{Text: "&Find files...", Shortcut: Shortcut{Modifiers: walk.ModControl, Key: walk.KeyF}, OnTriggered: a.cmdFind},
				Action{Text: "&Convert archives...", Shortcut: Shortcut{Modifiers: walk.ModAlt, Key: walk.KeyQ}, OnTriggered: a.cmdConvert},
				Action{Text: "&Repair archive", Shortcut: Shortcut{Modifiers: walk.ModAlt, Key: walk.KeyR}, OnTriggered: a.cmdRepair},
				Action{Text: "Show &information", Shortcut: Shortcut{Modifiers: walk.ModAlt, Key: walk.KeyI}, OnTriggered: a.cmdInfo},
			}},
			Menu{Text: "&Options", Items: []MenuItem{
				Action{Text: "&Settings... (file types, right-click menu)", OnTriggered: a.openSettings},
			}},
			Menu{Text: "&Help", Items: []MenuItem{
				Action{Text: "&About ZipNest", OnTriggered: a.about},
			}},
		},
		Children: []Widget{
			// Address bar: Back / Forward / Up, location, live filter.
			Composite{
				Layout: HBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 4},
				Children: []Widget{
					ToolButton{AssignTo: &a.backBtn, Text: "\u2190", Font: Font{Family: "Segoe UI", PointSize: 11}, ToolTipText: "Back (Alt+Left)", OnClicked: a.goBack, MinSize: Size{Width: 30, Height: 26}, MaxSize: Size{Width: 30, Height: 26}},
					ToolButton{AssignTo: &a.fwdBtn, Text: "\u2192", Font: Font{Family: "Segoe UI", PointSize: 11}, ToolTipText: "Forward (Alt+Right)", OnClicked: a.goForward, MinSize: Size{Width: 30, Height: 26}, MaxSize: Size{Width: 30, Height: 26}},
					ToolButton{Text: "\u2191", Font: Font{Family: "Segoe UI", PointSize: 11}, ToolTipText: "Up one level (Backspace)", OnClicked: a.goUp, MinSize: Size{Width: 30, Height: 26}, MaxSize: Size{Width: 30, Height: 26}},
					LineEdit{AssignTo: &a.addr, StretchFactor: 3, OnKeyDown: func(key walk.Key) {
						if key == walk.KeyReturn {
							a.navigate(strings.TrimSpace(a.addr.Text()))
						}
					}},
					LineEdit{AssignTo: &a.filter, CueBanner: "Filter this list...", MaxSize: Size{Width: 220}, MinSize: Size{Width: 160},
						OnTextChanged: func() {
							a.model.setFilter(a.filter.Text())
							a.updateStatus()
						}},
				},
			},
			bannerWidgets(a),
			HSplitter{
				HandleWidth: 4,
				Children: []Widget{
					TableView{
						AssignTo:              &a.nav,
						Model:                 a.navModel,
						HeaderHidden:          true,
						LastColumnStretched:   true,
						CustomRowHeight:       26,
						StretchFactor:         1,
						MinSize:               Size{Width: 170},
						Columns:               []TableViewColumn{{Title: "Places"}},
						StyleCell:             a.styleNav,
						OnCurrentIndexChanged: a.navClicked,
					},
					TableView{
						AssignTo:         &a.tv,
						Name:             "fileList",
						Model:            a.model,
						MultiSelection:   true,
						AlternatingRowBG: true,
						CustomRowHeight:  24,
						StretchFactor:    4,
						Columns: []TableViewColumn{
							{Name: "name", Title: "Name", Width: 360},
							{Name: "size", Title: "Size", Width: 110, Alignment: AlignFar},
							{Name: "packed", Title: "Packed", Width: 110, Alignment: AlignFar},
							{Name: "ratio", Title: "Ratio", Width: 60, Alignment: AlignFar},
							{Name: "type", Title: "Type", Width: 170},
							{Name: "modified", Title: "Modified", Width: 130},
						},
						OnItemActivated:          a.activate,
						OnSelectedIndexesChanged: a.updateStatus,
						OnKeyDown: func(key walk.Key) {
							if key == walk.KeyReturn {
								a.activate()
							}
						},
						ContextMenuItems: []MenuItem{
							Action{Text: "Open", OnTriggered: a.activate},
							Action{Text: "View", OnTriggered: a.cmdView},
							Separator{},
							Action{Text: "Add to archive...", OnTriggered: a.cmdAdd},
							Action{Text: "Extract to...", OnTriggered: a.cmdExtract},
							Action{Text: "Extract here", OnTriggered: a.cmdExtractHere},
							Action{Text: "Test", OnTriggered: a.cmdTest},
							Action{Text: "Convert...", OnTriggered: a.cmdConvert},
							Action{Text: "Repair", OnTriggered: a.cmdRepair},
							Action{Text: "Find...", OnTriggered: a.cmdFind},
							Separator{},
							Action{Text: "Delete", OnTriggered: a.cmdDelete},
							Action{Text: "Information", OnTriggered: a.cmdInfo},
						},
					},
				},
			},
		},
		StatusBarItems: []StatusBarItem{{AssignTo: &a.status, Width: 900}},
		OnDropFiles:    a.dropFiles,
	}.Create()
	if err != nil {
		walk.MsgBox(nil, "ZipNest", "Cannot create the main window: "+err.Error(), walk.MsgBoxIconError)
		return
	}

	a.fixControls()
	if f, err := walk.NewFont("Segoe UI", 8, walk.FontBold); err == nil {
		a.navHeaderFont = f
	}
	a.updateNavButtons()

	switch {
	case start != "" && isFile(start):
		a.dir = filepath.Dir(absPath(start))
		a.showDir(a.dir)
		a.openArchive(absPath(start))
	case start != "" && isDir(start):
		a.showDir(absPath(start))
	default:
		home, _ := os.UserHomeDir()
		if wd, err := os.Getwd(); err == nil && !strings.EqualFold(wd, filepath.Dir(exePath())) &&
			!strings.HasPrefix(strings.ToLower(wd), strings.ToLower(os.Getenv("SystemRoot"))) {
			home = wd
		}
		a.showDir(home)
	}
	a.mw.Run()
	saveSettings()
}

// buildToolBar creates the big-icon toolbar (Add, Extract To, ...).
func (a *App) buildToolBar() {
	tb, err := walk.NewToolBarWithOrientationAndButtonStyle(a.mw, walk.Horizontal, walk.ToolBarButtonImageAboveText)
	if err != nil {
		return
	}
	if il, err := walk.NewImageListForDPI(walk.Size{Width: 32, Height: 32}, 0, 96); err == nil {
		tb.SetImageList(il)
		tb.ApplyDPI(tb.DPI())
	}
	tb.SetMaxTextRows(1)
	items := []struct {
		text, icon, tip string
		fn              func()
	}{
		{"Add", "add", "Add files to archive (Alt+A)", a.cmdAdd},
		{"Extract To", "extract", "Extract to the specified folder (Alt+E)", a.cmdExtract},
		{"Extract Here", "extracthere", "Extract into the current folder (Alt+W)", a.cmdExtractHere},
		{"Test", "test", "Test archived files (Alt+T)", a.cmdTest},
		{"View", "view", "Open the selected file (Alt+V)", a.cmdView},
		{"Delete", "delete", "Delete selected items (Del)", a.cmdDelete},
		{"Find", "find", "Find files in the archive or folder (Ctrl+F)", a.cmdFind},
		{"Convert", "convert", "Convert archives to another format (Alt+Q)", a.cmdConvert},
		{"Repair", "repair", "Repair a damaged .nest archive (Alt+R)", a.cmdRepair},
		{"Info", "info", "Show archive information (Alt+I)", a.cmdInfo},
	}
	for _, it := range items {
		act := walk.NewAction()
		act.SetText(it.text)
		act.SetToolTip(it.tip)
		if bmp := loadBitmap(it.icon); bmp != nil {
			act.SetImage(bmp)
		}
		fn := it.fn
		act.Triggered().Attach(func() { fn() })
		tb.Actions().Add(act)
	}
	old := a.mw.ToolBar()
	a.mw.SetToolBar(tb)
	if old != nil && old != tb {
		old.Dispose()
	}
}

// fixControls enlarges the toolbar images to 32x32 and stops the list view from
// drawing an icon in every column (walk enables sub-item images).
func (a *App) fixControls() {
	a.buildToolBar()
	findEx := syscall.NewLazyDLL("user32.dll").NewProc("FindWindowExW")
	cls := syscall.StringToUTF16Ptr("SysListView32")
	var child uintptr
	for {
		child, _, _ = findEx.Call(uintptr(a.tv.Handle()), child, uintptr(unsafe.Pointer(cls)), 0)
		if child == 0 {
			break
		}
		win.SendMessage(win.HWND(child), win.LVM_SETEXTENDEDLISTVIEWSTYLE, win.LVS_EX_SUBITEMIMAGES, 0)
	}
	a.mw.RequestLayout()
}

func (a *App) setRows(rows []row) {
	a.model.setRows(rows)
	a.tv.SetSelectedIndexes(nil)
	if len(rows) > 0 {
		a.tv.SetCurrentIndex(0)
	}
	a.tv.EnsureItemVisible(0)
	a.tv.Invalidate()
}

func exePath() string {
	p, _ := os.Executable()
	return p
}

func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func (a *App) errorBox(err error) {
	if err == nil || errors.Is(err, core.ErrCancelled) {
		return
	}
	walk.MsgBox(a.mw, "ZipNest", err.Error(), walk.MsgBoxIconError)
}

func (a *App) inArchive() bool { return a.archive != "" }

// ---- navigation ----

func (a *App) setTitle() {
	if a.inArchive() {
		a.mw.SetTitle(filepath.Base(a.archive) + " - ZipNest")
	} else {
		a.mw.SetTitle("ZipNest")
	}
}

func (a *App) showDrives() {
	a.remember(location{})
	a.archive, a.inner, a.entries, a.dir = "", "", nil, ""
	var rows []row
	for c := 'A'; c <= 'Z'; c++ {
		d := string(c) + `:\`
		if _, err := os.Stat(d); err == nil {
			rows = append(rows, row{name: d, dir: true, full: d, icon: d, typ: "Drive"})
		}
	}
	a.addr.SetText("Computer")
	a.setRows(rows)
	a.setTitle()
	a.updateStatus()
	a.updateBanner()
}

func (a *App) showDir(dir string) {
	a.remember(location{dir: dir})
	a.search = searchState{}
	if a.filter != nil && a.filter.Text() != "" {
		a.filter.SetText("")
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		a.errorBox(err)
		return
	}
	a.archive, a.inner, a.entries, a.dir = "", "", nil, dir
	rows := []row{{name: "..", up: true, icon: iconFor("", true), typ: "Up one level"}}
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 && !info.IsDir() && !info.Mode().IsRegular() {
			continue
		}
		full := filepath.Join(dir, e.Name())
		r := row{
			name:  e.Name(),
			dir:   info.IsDir(),
			size:  info.Size(),
			mtime: info.ModTime(),
			full:  full,
			icon:  full,
			typ:   typeName(e.Name(), info.IsDir()),
		}
		r.packed = -1
		r.isArchive = !r.dir && core.IsArchiveName(e.Name())
		rows = append(rows, r)
	}
	a.addr.SetText(dir)
	a.setRows(rows)
	a.setTitle()
	a.updateStatus()
	a.updateBanner()
}

func (a *App) openArchive(p string) {
	var wasBusy bool
	var entries []core.Entry
	var format core.Format
	pw := ""
	if strings.EqualFold(p, a.archive) {
		pw = a.password
	}
	err := a.withPassword(p, &pw, "Reading "+filepath.Base(p), func(pw string, _ core.ProgressFunc) error {
		var err error
		entries, format, err = core.List(p, pw)
		return err
	})
	if err != nil {
		a.errorBox(fmt.Errorf("%s: %w", filepath.Base(p), err))
		return
	}
	a.remember(location{dir: filepath.Dir(p), archive: p})
	a.historyBusy, wasBusy = true, a.historyBusy
	a.archive, a.entries, a.format, a.password = p, entries, format, pw
	a.dir = filepath.Dir(p)
	a.showArchiveDir("")
	a.historyBusy = wasBusy
	addRecent(p)
	a.refreshNav()
}

func (a *App) showArchiveDir(inner string) {
	a.remember(location{dir: a.dir, archive: a.archive, inner: strings.Trim(inner, "/")})
	a.search = searchState{}
	if a.filter != nil && a.filter.Text() != "" {
		a.filter.SetText("")
	}
	a.inner = strings.Trim(inner, "/")
	rows := []row{{name: "..", up: true, icon: iconFor("", true), typ: "Up one level"}}
	for _, e := range core.Children(a.entries, a.inner) {
		name := core.BaseName(e.Path)
		r := row{
			name:      name,
			dir:       e.Dir,
			size:      e.Size,
			packed:    e.Packed,
			full:      e.Path,
			icon:      iconFor(name, e.Dir),
			typ:       typeName(name, e.Dir),
			encrypted: e.Encrypted,
		}
		if e.Modified != 0 {
			r.mtime = time.UnixMilli(e.Modified)
		}
		rows = append(rows, r)
	}
	p := a.archive
	if a.inner != "" {
		p += `\` + strings.ReplaceAll(a.inner, "/", `\`)
	}
	a.addr.SetText(p)
	a.setRows(rows)
	a.setTitle()
	a.updateStatus()
	a.updateBanner()
}

func (a *App) navigate(p string) {
	if p == "" || strings.EqualFold(p, "Computer") {
		a.showDrives()
		return
	}
	if isDir(p) {
		a.showDir(absPath(p))
		return
	}
	if isFile(p) {
		a.openArchive(absPath(p))
		return
	}
	// A path inside an archive: "C:\x\file.zip\folder".
	q := p
	for q != filepath.Dir(q) {
		if isFile(q) && core.IsArchiveName(q) {
			if !strings.EqualFold(q, a.archive) {
				a.openArchive(q)
			}
			if a.inArchive() {
				a.showArchiveDir(filepath.ToSlash(strings.TrimPrefix(p[len(q):], `\`)))
			}
			return
		}
		q = filepath.Dir(q)
	}
	walk.MsgBox(a.mw, "ZipNest", "Cannot find "+p, walk.MsgBoxIconWarning)
}

func (a *App) goUp() {
	if a.search.active {
		a.endSearch()
		return
	}
	switch {
	case a.inArchive() && a.inner != "":
		i := strings.LastIndex(a.inner, "/")
		if i < 0 {
			a.showArchiveDir("")
		} else {
			a.showArchiveDir(a.inner[:i])
		}
	case a.inArchive():
		dir := filepath.Dir(a.archive)
		name := filepath.Base(a.archive)
		a.showDir(dir)
		a.selectName(name)
	case a.dir == "":
	case filepath.Dir(a.dir) == a.dir:
		a.showDrives()
	default:
		name := filepath.Base(a.dir)
		a.showDir(filepath.Dir(a.dir))
		a.selectName(name)
	}
}

func (a *App) selectName(name string) {
	for i, r := range a.model.rows {
		if strings.EqualFold(r.name, name) {
			a.tv.SetCurrentIndex(i)
			return
		}
	}
}

func (a *App) refresh() {
	switch {
	case a.inArchive():
		inner := a.inner
		a.openArchive(a.archive)
		a.showArchiveDir(inner)
	case a.dir == "":
		a.showDrives()
	default:
		a.showDir(a.dir)
	}
}

func (a *App) selectAll() {
	var idx []int
	for i, r := range a.model.rows {
		if !r.up {
			idx = append(idx, i)
		}
	}
	a.tv.SetSelectedIndexes(idx)
}

func (a *App) activate() {
	i := a.tv.CurrentIndex()
	if i < 0 || i >= len(a.model.rows) {
		return
	}
	r := a.model.rows[i]
	if a.search.active {
		if r.up {
			a.endSearch()
		} else {
			a.activateSearchResult(r)
		}
		return
	}
	switch {
	case r.up:
		a.goUp()
	case a.inArchive() && r.dir:
		a.showArchiveDir(r.full)
	case a.inArchive():
		a.cmdView()
	case r.dir:
		a.showDir(r.full)
	case r.isArchive:
		a.openArchive(r.full)
	default:
		shellOpen(a.mw.Handle(), r.full)
	}
}

func (a *App) selectedRows() []row {
	var out []row
	for _, i := range a.tv.SelectedIndexes() {
		if i >= 0 && i < len(a.model.rows) && !a.model.rows[i].up {
			out = append(out, a.model.rows[i])
		}
	}
	return out
}

func (a *App) updateStatus() {
	if a.status == nil {
		return
	}
	var files, dirs int
	var total int64
	for _, r := range a.model.rows {
		if r.up {
			continue
		}
		if r.dir {
			dirs++
		} else {
			files++
			if r.size > 0 {
				total += r.size
			}
		}
	}
	text := fmt.Sprintf("Total %d folder(s) and %s in %d file(s)", dirs, shortSize(total), files)
	if sel := a.selectedRows(); len(sel) > 0 {
		var s int64
		for _, r := range sel {
			if r.size > 0 {
				s += r.size
			}
		}
		text = fmt.Sprintf("Selected %d item(s), %s      |      %s", len(sel), shortSize(s), text)
	}
	if a.inArchive() {
		text = strings.ToUpper(string(a.format)) + " archive      |      " + text
	}
	a.status.SetText(text)
}

// withPassword runs op with a progress window, asking for a password when needed.
func (a *App) withPassword(archive string, pw *string, title string, op func(pw string, progress core.ProgressFunc) error) error {
	for {
		cur := *pw
		err := runTask(a.form(), title, func(p core.ProgressFunc) error { return op(cur, p) })
		if errors.Is(err, core.ErrPasswordRequired) || errors.Is(err, core.ErrBadPassword) {
			msg := "Enter password"
			if cur != "" {
				msg = "The password is incorrect. Please try again"
			}
			np, ok := passwordDialog(a.form(), archive, msg)
			if !ok {
				return core.ErrCancelled
			}
			*pw = np
			continue
		}
		return err
	}
}

func (a *App) form() walk.Form {
	if a.mw == nil {
		return nil
	}
	return a.mw
}

func (a *App) dropFiles(files []string) {
	if len(files) == 0 {
		return
	}
	if a.inArchive() {
		a.addToOpenArchive(files)
		return
	}
	if len(files) == 1 && isFile(files[0]) && core.IsArchiveName(files[0]) {
		a.openArchive(files[0])
		return
	}
	a.addFiles(files)
}

func (a *App) openSettings() {
	settingsDialog(a.mw)
}
