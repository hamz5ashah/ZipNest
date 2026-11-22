//go:build windows

package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"zipnest/ui"
)

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	procSHFileOpW     = shell32.NewProc("SHFileOperationW")
	procSHChangeNotif = shell32.NewProc("SHChangeNotify")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

// host connects the interface to Windows.
type host struct {
	ui.BaseHost
	wv     webview2.WebView
	server *ui.Server
	code   int
	exited bool
}

func (h *host) Platform() string { return "windows" }

func shellExecute(verb, file, dir string) error {
	v, _ := windows.UTF16PtrFromString(verb)
	f, _ := windows.UTF16PtrFromString(file)
	var d *uint16
	if dir != "" {
		d, _ = windows.UTF16PtrFromString(dir)
	}
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(f)), 0, uintptr(unsafe.Pointer(d)), 1)
	if r <= 32 {
		if r == 31 { // SE_ERR_NOASSOC
			return errors.New("no program is set to open this kind of file")
		}
		return syscall.Errno(r)
	}
	return nil
}

func (h *host) Open(p string) error { return shellExecute("open", p, filepath.Dir(p)) }

func (h *host) Reveal(p string) error {
	return exec.Command("explorer.exe", "/select,", p).Start()
}

// Trash moves files to the Recycle Bin.
func (h *host) Trash(paths []string) error {
	var from []uint16
	for _, p := range paths {
		u, err := syscall.UTF16FromString(p)
		if err != nil {
			return err
		}
		from = append(from, u...)
	}
	from = append(from, 0)
	type shFileOpStruct struct {
		hwnd                  uintptr
		wFunc                 uint32
		pFrom                 *uint16
		pTo                   *uint16
		fFlags                uint16
		fAnyOperationsAborted int32
		hNameMappings         uintptr
		lpszProgressTitle     *uint16
	}
	op := shFileOpStruct{
		wFunc:  3, // FO_DELETE
		pFrom:  &from[0],
		fFlags: 0x40 | 0x10 | 0x400 | 0x4, // FOF_ALLOWUNDO | FOF_NOCONFIRMATION | FOF_NOERRORUI | FOF_SILENT
	}
	r, _, _ := procSHFileOpW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return errors.New("some files could not be moved to the Recycle Bin")
	}
	return nil
}

// Places uses the real (possibly OneDrive-redirected) user folders.
func (h *host) Places() []ui.Place {
	var out []ui.Place
	add := func(name string, id *windows.KNOWNFOLDERID, icon string) {
		if p, err := windows.KnownFolderPath(id, 0); err == nil && p != "" {
			out = append(out, ui.Place{Name: name, Path: p, Icon: icon})
		}
	}
	add("Home", windows.FOLDERID_Profile, "home")
	add("Desktop", windows.FOLDERID_Desktop, "desktop")
	add("Documents", windows.FOLDERID_Documents, "documents")
	add("Downloads", windows.FOLDERID_Downloads, "downloads")
	add("Pictures", windows.FOLDERID_Pictures, "pictures")
	add("Music", windows.FOLDERID_Music, "music")
	add("Videos", windows.FOLDERID_Videos, "videos")
	return out
}

func (h *host) Exit(code int) {
	h.code, h.exited = code, true
	if h.wv != nil {
		h.wv.Dispatch(func() { h.wv.Terminate() })
	}
}

// Integration describes the file-type and Explorer-menu settings for the page.
func (h *host) Integration() map[string]any {
	in := loadIntegration()
	var types []map[string]any
	for _, t := range fileTypes {
		types = append(types, map[string]any{"label": t.Label, "exts": t.Exts, "on": in.Assoc[t.Label]})
	}
	return map[string]any{
		"types": types, "shell": in.Shell, "cascaded": in.Cascaded, "icons": in.Icons,
		"win11": in.Win11, "win11Available": win11MenuAvailable(),
	}
}

func (h *host) SetIntegration(m map[string]any) error {
	in := loadIntegration()
	b := func(k string, def bool) bool {
		if v, ok := m[k].(bool); ok {
			return v
		}
		return def
	}
	if types, ok := m["types"].([]any); ok {
		for _, x := range types {
			t, _ := x.(map[string]any)
			label, _ := t["label"].(string)
			on, _ := t["on"].(bool)
			if _, known := in.Assoc[label]; known {
				in.Assoc[label] = on
			}
		}
	}
	in.Shell = b("shell", in.Shell)
	in.Cascaded = b("cascaded", in.Cascaded)
	in.Icons = b("icons", in.Icons)
	in.Win11 = b("win11", in.Win11)
	return in.apply()
}

// ---------------------------------------------------------------- registry helpers

const regRoot = `Software\Classes\`

func setKey(path, def string, values map[string]string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, regRoot+path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if def != "" {
		if err := k.SetStringValue("", def); err != nil {
			return err
		}
	}
	for n, v := range values {
		if err := k.SetStringValue(n, v); err != nil {
			return err
		}
	}
	return nil
}

func deleteTree(path string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, regRoot+path, registry.ENUMERATE_SUB_KEYS)
	if err == nil {
		subs, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, s := range subs {
			deleteTree(path + `\` + s)
		}
	}
	registry.DeleteKey(registry.CURRENT_USER, regRoot+path)
}
