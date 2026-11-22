//go:build windows

// zipnest-sfx is the self-extractor stub. ZipNest writes it in front of a ZIP
// archive to make "archive.exe"; when run, it unpacks the appended archive.
// It uses the Win32 API directly (no GUI toolkit) to stay small.
//
//	archive.exe                      show the window
//	archive.exe -s [-d DIR] [-pPWD]  extract silently
package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"unsafe"

	"zipnest/core/zipcrypt"
)

func init() { runtime.LockOSThread() }

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")

	pCreateWindowEx      = user32.NewProc("CreateWindowExW")
	pDefWindowProc       = user32.NewProc("DefWindowProcW")
	pRegisterClassEx     = user32.NewProc("RegisterClassExW")
	pGetMessage          = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessage     = user32.NewProc("DispatchMessageW")
	pIsDialogMessage     = user32.NewProc("IsDialogMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pPostMessage         = user32.NewProc("PostMessageW")
	pSendMessage         = user32.NewProc("SendMessageW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pEnableWindow        = user32.NewProc("EnableWindow")
	pSetWindowText       = user32.NewProc("SetWindowTextW")
	pGetWindowText       = user32.NewProc("GetWindowTextW")
	pGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pMessageBox          = user32.NewProc("MessageBoxW")
	pLoadIcon            = user32.NewProc("LoadIconW")
	pLoadCursor          = user32.NewProc("LoadCursorW")
	pGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	pSetFocus            = user32.NewProc("SetFocus")
	pGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	pGetSysColorBrush    = user32.NewProc("GetSysColorBrush")
	pCreateFont          = gdi32.NewProc("CreateFontW")
	pGetModuleHandle     = kernel32.NewProc("GetModuleHandleW")
	pShellExecute        = shell32.NewProc("ShellExecuteW")
	pSHBrowseForFolder   = shell32.NewProc("SHBrowseForFolderW")
	pSHGetPathFromIDList = shell32.NewProc("SHGetPathFromIDListW")
	pCoTaskMemFree       = ole32.NewProc("CoTaskMemFree")
	pCoInitialize        = ole32.NewProc("CoInitialize")
	pInitCommonControls  = comctl32.NewProc("InitCommonControlsEx")
)

const (
	wsOverlapped   = 0x00000000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsTabStop      = 0x00010000
	wsBorder       = 0x00800000
	wsExClientEdge = 0x00000200
	esAutoHScroll  = 0x0080
	esPassword     = 0x0020
	bsDefPush      = 0x0001
	bsAutoCheckBox = 0x0003
	ssEndEllipsis  = 0x4000
	ssPathEllipsis = 0x8000

	wmDestroy  = 0x0002
	wmClose    = 0x0010
	wmSetFont  = 0x0030
	wmCommand  = 0x0111
	wmApp      = 0x8000
	bmGetCheck = 0x00F0
	bmSetCheck = 0x00F1
	pbmSetPos  = 0x0402
	pbmSetRng  = 0x0406

	idBrowse  = 101
	idExtract = 102
	idCancel  = 2 // IDCANCEL: Esc closes
	idOK      = 1 // IDOK: Enter
)

type wndClassEx struct {
	size, style                      uint32
	wndProc                          uintptr
	clsExtra, wndExtra               int32
	instance, icon, cursor, bg, menu uintptr
	className                        *uint16
	iconSm                           uintptr
}

type msg struct {
	hwnd             uintptr
	message          uint32
	wParam, lParam   uintptr
	time             uint32
	ptX, ptY, unused int32
}

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func send(h uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := pSendMessage.Call(h, uintptr(m), w, l)
	return r
}

func setText(h uintptr, s string) { pSetWindowText.Call(h, uintptr(unsafe.Pointer(u16(s)))) }

func getText(h uintptr) string {
	n, _, _ := pGetWindowTextLength.Call(h)
	buf := make([]uint16, n+1)
	pGetWindowText.Call(h, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func msgBox(owner uintptr, text, title string, flags uintptr) {
	pMessageBox.Call(owner, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), flags)
}

// ---------------------------------------------------------------- extraction

var errCancelled = errors.New("cancelled")

// cleanPath makes a member name a safe relative path ("" if it cannot be).
func cleanPath(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if len(name) >= 2 && name[1] == ':' {
		name = name[2:]
	}
	var out []string
	for _, p := range strings.Split(name, "/") {
		switch p {
		case "", ".":
		case "..":
			return ""
		default:
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

// extractAll unpacks every member into dest. progress returns false to cancel.
func extractAll(zr *zip.Reader, dest, password string, progress func(file string, done, total int64) bool) error {
	var total, done int64
	for _, f := range zr.File {
		total += int64(f.UncompressedSize64)
	}
	buf := make([]byte, 256<<10)
	for _, f := range zr.File {
		rel := cleanPath(f.Name)
		if rel == "" || f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if strings.HasSuffix(f.Name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := zipcrypt.OpenEntry(f, password, nil)
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		for {
			n, rerr := rc.Read(buf)
			if n > 0 {
				if _, err := out.Write(buf[:n]); err != nil {
					rerr = err
				}
				done += int64(n)
				if progress != nil && !progress(rel, done, total) {
					rerr = errCancelled
				}
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				rc.Close()
				out.Close()
				os.Remove(target)
				return rerr
			}
		}
		rc.Close()
		out.Close()
		os.Chtimes(target, f.Modified, f.Modified)
	}
	return nil
}

func shortSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
}

// ---------------------------------------------------------------- window

type ui struct {
	zr                                *zip.Reader
	name                              string
	hwnd, dest, chk, pwLabel, pw, bar uintptr
	status, extract, browse, cancelB  uintptr
	running, cancel                   atomic.Bool
	file                              atomic.Pointer[string]
	pos                               atomic.Int64
	result                            error
	target                            string
}

var app ui

func (u *ui) start() {
	u.target = strings.TrimSpace(getText(u.dest))
	if u.target == "" || u.running.Load() {
		return
	}
	u.running.Store(true)
	u.cancel.Store(false)
	pEnableWindow.Call(u.extract, 0)
	pEnableWindow.Call(u.browse, 0)
	pEnableWindow.Call(u.dest, 0)
	pShowWindow.Call(u.bar, 5)
	password := getText(u.pw)
	go func() {
		err := extractAll(u.zr, u.target, password, func(file string, done, total int64) bool {
			f := file
			u.file.Store(&f)
			if total > 0 {
				u.pos.Store(done * 1000 / total)
			}
			pPostMessage.Call(u.hwnd, wmApp+1, 0, 0)
			return !u.cancel.Load()
		})
		u.result = err
		pPostMessage.Call(u.hwnd, wmApp+2, 0, 0)
	}()
}

func (u *ui) finished() {
	u.running.Store(false)
	err := u.result
	switch {
	case errors.Is(err, zipcrypt.ErrPasswordRequired) || errors.Is(err, zipcrypt.ErrBadPassword):
		msgText := "This archive is password protected. Enter the password and click Extract."
		if getText(u.pw) != "" {
			msgText = "The password is incorrect. Please try again."
		}
		setText(u.status, msgText)
		pShowWindow.Call(u.pwLabel, 5)
		pShowWindow.Call(u.pw, 5)
		pShowWindow.Call(u.bar, 0)
		pEnableWindow.Call(u.extract, 1)
		pEnableWindow.Call(u.browse, 1)
		pEnableWindow.Call(u.dest, 1)
		pSetFocus.Call(u.pw)
	case errors.Is(err, errCancelled):
		pDestroyWindow.Call(u.hwnd)
	case err != nil:
		msgBox(u.hwnd, "Extraction failed:\n\n"+err.Error(), u.name, 0x10)
		pDestroyWindow.Call(u.hwnd)
	default:
		if send(u.chk, bmGetCheck, 0, 0) == 1 {
			pShellExecute.Call(0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(u.target))), 0, 0, 1)
		}
		pDestroyWindow.Call(u.hwnd)
	}
}

type browseInfo struct {
	owner       uintptr
	root        uintptr
	displayName *uint16
	title       *uint16
	flags       uint32
	callback    uintptr
	lParam      uintptr
	image       int32
}

func (u *ui) browseFolder() {
	name := make([]uint16, 260)
	bi := browseInfo{owner: u.hwnd, displayName: &name[0], title: u16("Extract to:"), flags: 0x0001 | 0x0040} // RETURNONLYFSDIRS | NEWDIALOGSTYLE
	pidl, _, _ := pSHBrowseForFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return
	}
	path := make([]uint16, 32768)
	if r, _, _ := pSHGetPathFromIDList.Call(pidl, uintptr(unsafe.Pointer(&path[0]))); r != 0 {
		setText(u.dest, filepath.Join(syscall.UTF16ToString(path), u.name))
	}
	pCoTaskMemFree.Call(pidl)
}

func wndProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmCommand:
		switch w & 0xFFFF {
		case idBrowse:
			app.browseFolder()
		case idExtract, idOK:
			app.start()
		case idCancel:
			if app.running.Load() {
				app.cancel.Store(true)
			} else {
				pDestroyWindow.Call(hwnd)
			}
		}
		return 0
	case wmApp + 1:
		if f := app.file.Load(); f != nil {
			setText(app.status, *f)
		}
		send(app.bar, pbmSetPos, uintptr(app.pos.Load()), 0)
		return 0
	case wmApp + 2:
		app.finished()
		return 0
	case wmClose:
		if app.running.Load() {
			app.cancel.Store(true)
			return 0
		}
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(m), w, l)
	return r
}

func runWindow(zr *zip.Reader, name, dest string, files int, size int64) {
	pCoInitialize.Call(0)
	icc := [2]uint32{8, 0x00000020 | 0x0004} // ICC_PROGRESS_CLASS | ICC_STANDARD_CLASSES
	pInitCommonControls.Call(uintptr(unsafe.Pointer(&icc)))
	inst, _, _ := pGetModuleHandle.Call(0)
	icon, _, _ := pLoadIcon.Call(inst, 1)
	cursor, _, _ := pLoadCursor.Call(0, 32512)
	bg, _, _ := pGetSysColorBrush.Call(15) // COLOR_BTNFACE
	cls := u16("ZipNestSFX")
	wc := wndClassEx{wndProc: syscall.NewCallback(wndProc), instance: inst, icon: icon, cursor: cursor, bg: bg, className: cls, iconSm: icon}
	wc.size = uint32(unsafe.Sizeof(wc))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))

	dpi := uintptr(96)
	if pGetDpiForSystem.Find() == nil {
		dpi, _, _ = pGetDpiForSystem.Call()
	}
	s := func(v int) uintptr { return uintptr(v) * dpi / 96 }
	font, _, _ := pCreateFont.Call(^uintptr(s(15)-1), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Segoe UI"))))

	w, h := s(540), s(270)
	sw, _, _ := pGetSystemMetrics.Call(0)
	sh, _, _ := pGetSystemMetrics.Call(1)
	title := name + " - ZipNest self-extracting archive"
	hwnd, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(u16(title))),
		wsOverlapped|wsCaption|wsSysMenu|wsMinimizeBox, (sw-w)/2, (sh-h)/2, w, h, 0, 0, inst, 0)
	app.hwnd = hwnd
	ctl := func(class, text string, style, ex uintptr, x, y, cw, ch int, id uintptr) uintptr {
		c, _, _ := pCreateWindowEx.Call(ex, uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))),
			wsChild|wsVisible|style, s(x), s(y), s(cw), s(ch), hwnd, id, inst, 0)
		send(c, wmSetFont, font, 1)
		return c
	}
	info := fmt.Sprintf("This program extracts \"%s\" (%d file(s), %s).", name, files, shortSize(size))
	ctl("STATIC", info, ssEndEllipsis, 0, 16, 14, 500, 20, 0)
	ctl("STATIC", "Destination folder:", 0, 0, 16, 44, 300, 18, 0)
	app.dest = ctl("EDIT", dest, wsTabStop|esAutoHScroll, wsExClientEdge, 16, 64, 400, 24, 0)
	app.browse = ctl("BUTTON", "Browse...", wsTabStop, 0, 424, 63, 92, 26, idBrowse)
	app.chk = ctl("BUTTON", "Open the folder when done", wsTabStop|bsAutoCheckBox, 0, 16, 96, 300, 22, 0)
	send(app.chk, bmSetCheck, 1, 0)
	app.pwLabel = ctl("STATIC", "Password:", 0, 0, 16, 128, 80, 20, 0)
	app.pw = ctl("EDIT", "", wsTabStop|esAutoHScroll|esPassword, wsExClientEdge, 100, 125, 316, 24, 0)
	pShowWindow.Call(app.pwLabel, 0)
	pShowWindow.Call(app.pw, 0)
	app.bar = ctl("msctls_progress32", "", 0, 0, 16, 160, 500, 16, 0)
	send(app.bar, pbmSetRng, 0, 1000<<16)
	pShowWindow.Call(app.bar, 0)
	app.status = ctl("STATIC", "", ssPathEllipsis, 0, 16, 182, 500, 18, 0)
	ctl("STATIC", "Made with ZipNest", 0, 0, 16, 214, 200, 18, 0)
	app.extract = ctl("BUTTON", "Extract", wsTabStop|bsDefPush, 0, 320, 208, 96, 28, idExtract)
	app.cancelB = ctl("BUTTON", "Cancel", wsTabStop, 0, 424, 208, 92, 28, idCancel)
	app.zr, app.name = zr, name

	pShowWindow.Call(hwnd, 1)
	pSetFocus.Call(app.extract)
	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		if d, _, _ := pIsDialogMessage.Call(hwnd, uintptr(unsafe.Pointer(&m))); d != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	name := strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
	dest := filepath.Join(filepath.Dir(exe), name)
	silent, password := false, ""
	for i := 1; i < len(os.Args); i++ {
		switch a := os.Args[i]; {
		case a == "-s" || a == "/s":
			silent = true
		case (a == "-d" || a == "/d") && i+1 < len(os.Args):
			i++
			dest = os.Args[i]
		case strings.HasPrefix(a, "-p"):
			password = a[2:]
		}
	}
	zr, err := zip.OpenReader(exe)
	if err != nil {
		if !silent {
			msgBox(0, "This file does not contain a valid archive.\n\n"+err.Error(), "ZipNest", 0x10)
		}
		os.Exit(1)
	}
	defer zr.Close()
	if silent {
		if err := extractAll(&zr.Reader, dest, password, nil); err != nil {
			os.Exit(1)
		}
		return
	}
	var files int
	var size int64
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "/") {
			files++
			size += int64(f.UncompressedSize64)
		}
	}
	runWindow(&zr.Reader, name, dest, files, size)
}
