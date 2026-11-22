//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows/registry"
)

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	procSHFileOpW     = shell32.NewProc("SHFileOperationW")
	procSHChangeNotif = shell32.NewProc("SHChangeNotify")
)

func shellOpen(owner win.HWND, path string) {
	win.ShellExecute(owner, syscall.StringToUTF16Ptr("open"), syscall.StringToUTF16Ptr(path), nil,
		syscall.StringToUTF16Ptr(filepath.Dir(path)), win.SW_SHOWNORMAL)
}

var (
	typeNameMu    sync.Mutex
	typeNameCache = map[string]string{}
)

// typeName returns Explorer's type description ("Text Document") for a name.
func typeName(name string, dir bool) string {
	key := strings.ToLower(filepath.Ext(name))
	if dir {
		key = "<dir>"
	}
	typeNameMu.Lock()
	defer typeNameMu.Unlock()
	if t, ok := typeNameCache[key]; ok {
		return t
	}
	attrs := uint32(0x80) // FILE_ATTRIBUTE_NORMAL
	if dir {
		attrs = 0x10 // FILE_ATTRIBUTE_DIRECTORY
	}
	var fi win.SHFILEINFO
	q := "x" + key
	if dir {
		q = "folder"
	}
	win.SHGetFileInfo(syscall.StringToUTF16Ptr(q), attrs, &fi, uint32(unsafe.Sizeof(fi)),
		win.SHGFI_TYPENAME|win.SHGFI_USEFILEATTRIBUTES)
	t := syscall.UTF16ToString(fi.SzTypeName[:])
	if t == "" {
		if dir {
			t = "File folder"
		} else if key != "" {
			t = strings.ToUpper(key[1:]) + " File"
		} else {
			t = "File"
		}
	}
	typeNameCache[key] = t
	return t
}

var (
	iconDirOnce sync.Once
	iconDir     string
	iconMu      sync.Mutex
	iconPaths   = map[string]string{}
)

// iconFor returns a path whose shell icon represents a member inside an archive.
// Windows only gives icons for existing paths, so ZipNest keeps tiny empty
// placeholder files (one per extension) in its temp folder.
func iconFor(name string, dir bool) string {
	iconDirOnce.Do(func() {
		iconDir = filepath.Join(os.TempDir(), "ZipNest", "icons")
		os.MkdirAll(filepath.Join(iconDir, "folder"), 0o755)
	})
	if dir {
		return filepath.Join(iconDir, "folder")
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".exe" || ext == ".lnk" || ext == ".ico" || ext == ".url" {
		ext = ".bin" // these take their icon from file content
	}
	iconMu.Lock()
	defer iconMu.Unlock()
	if p, ok := iconPaths[ext]; ok {
		return p
	}
	p := filepath.Join(iconDir, "file"+ext)
	if _, err := os.Stat(p); err != nil {
		os.WriteFile(p, nil, 0o644)
	}
	iconPaths[ext] = p
	return p
}

// recycle moves files to the Recycle Bin.
func recycle(owner win.HWND, paths []string) error {
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
		hwnd                  win.HWND
		wFunc                 uint32
		pFrom                 *uint16
		pTo                   *uint16
		fFlags                uint16
		fAnyOperationsAborted int32
		hNameMappings         uintptr
		lpszProgressTitle     *uint16
	}
	op := shFileOpStruct{
		hwnd:   owner,
		wFunc:  3, // FO_DELETE
		pFrom:  &from[0],
		fFlags: 0x40 | 0x10 | 0x400, // FOF_ALLOWUNDO | FOF_NOCONFIRMATION | FOF_NOERRORUI
	}
	r, _, _ := procSHFileOpW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

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
