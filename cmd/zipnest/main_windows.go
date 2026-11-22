//go:build windows

// ZipNest desktop for Windows: the ZipNest interface (package ui) shown in a
// WebView2 window. Without the WebView2 runtime it opens in Microsoft Edge's
// app mode instead.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"zipnest/cli"
	"zipnest/ui"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procGetDpiSystem = user32.NewProc("GetDpiForSystem")
	procMessageBoxW  = user32.NewProc("MessageBoxW")
)

func main() {
	runtime.LockOSThread()
	args := os.Args[1:]
	action := ""
	var actionArgs []string
	if len(args) > 0 {
		switch args[0] {
		case "--setup":
			// Used by the installer: --setup assoc=RAR,ZIP,... shell=1 cascaded=1 icons=1 win11=1
			os.Exit(runSetup(args[1:]))
		case "--remove-integration":
			// Used by the uninstaller.
			removeIntegration()
			os.Exit(0)
		case "--setup-dialog":
			// Shown by the installer after copying files; exit code = shortcut choices.
			action = "setup"
		case "--add", "--add-zip", "--extract-here", "--extract-auto", "--extract-dialog", "--test", "--convert":
			action, actionArgs = args[0][2:], absAll(args[1:])
			if len(actionArgs) == 0 {
				os.Exit(2)
			}
		case "l", "x", "e", "t", "a", "d", "c", "r":
			// Command-line use from a console window.
			attachConsole()
			os.Exit(cli.Run(args, os.Stdin, os.Stdout, os.Stderr))
		}
	}
	start := ""
	if action == "" && len(args) > 0 {
		start = args[0]
	}
	os.Exit(run(start, action, actionArgs))
}

func absAll(ps []string) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		if a, err := filepath.Abs(p); err == nil {
			p = a
		}
		out = append(out, p)
	}
	return out
}

func localDir() string {
	d, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "ZipNest")
}

func dataDir() string {
	d, err := os.UserConfigDir() // %APPDATA%
	if err != nil {
		return localDir()
	}
	return filepath.Join(d, "ZipNest")
}

// windowSize picks the window size (in physical pixels) for an action.
func windowSize(action string) (uint, uint) {
	w, h := 1200, 780
	switch action {
	case "setup":
		w, h = 860, 640
	case "add", "extract-dialog", "convert":
		w, h = 860, 760
	case "add-zip", "extract-here", "extract-auto", "test":
		w, h = 640, 420
	}
	dpi, _, _ := procGetDpiSystem.Call()
	if dpi == 0 {
		dpi = 96
	}
	return uint(w * int(dpi) / 96), uint(h * int(dpi) / 96)
}

func run(start, action string, args []string) int {
	h := &host{}
	s, err := ui.Start(ui.Options{
		Host: h, Version: cli.Version, DataDir: dataDir(), TempDir: filepath.Join(os.TempDir(), "ZipNest"),
		SFXStub: cli.SFXStub, Start: start, Action: action, Args: args,
	})
	if err != nil {
		messageBox("ZipNest could not start:\n\n" + err.Error())
		return 1
	}
	defer s.CleanTemp()
	h.server = s

	code := 0
	if webView2Installed() {
		code = runWebView(h, s, action)
	} else {
		code = runEdgeApp(h, s)
	}
	if action == "setup" && !h.exited {
		// The setup window was closed without OK: keep the shown choices, like OK.
		loadIntegration().apply()
		return setupDesktop | setupStartMenu
	}
	return code
}

func runWebView(h *host, s *ui.Server, action string) int {
	w, ht := windowSize(action)
	title := "ZipNest"
	if action == "setup" {
		title = "ZipNest Setup"
	}
	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  filepath.Join(localDir(), "WebView2"),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title: title, Width: w, Height: ht, IconId: 1, Center: true,
		},
	})
	if wv == nil {
		return runEdgeApp(h, s)
	}
	h.wv = wv
	wv.Bind("znSetTitle", func(t string) { wv.SetTitle(t) })
	wv.Navigate(s.URL)
	wv.Run()
	wv.Destroy()
	return h.code
}

// runEdgeApp shows the interface in Edge's app mode (a plain window without
// browser controls) and waits until the page stops checking in.
func runEdgeApp(h *host, s *ui.Server) int {
	edge := ""
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		p := filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`)
		if _, err := os.Stat(p); err == nil {
			edge = p
			break
		}
	}
	if edge != "" {
		cmd := exec.Command(edge, "--app="+s.URL, "--user-data-dir="+filepath.Join(localDir(), "EdgeApp"), "--no-first-run")
		if cmd.Start() != nil {
			edge = ""
		}
	}
	if edge == "" {
		shellExecute("open", s.URL, "")
	}
	time.Sleep(20 * time.Second)
	for !h.exited && s.SinceHeartbeat() < 12*time.Second {
		time.Sleep(time.Second)
	}
	return h.code
}

// webView2Installed checks for the WebView2 runtime (built into Windows 11 and
// installed with Edge on Windows 10).
func webView2Installed() bool {
	const id = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	keys := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\` + id},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + id},
		{registry.CURRENT_USER, `Software\Microsoft\EdgeUpdate\Clients\` + id},
	}
	for _, k := range keys {
		if key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE); err == nil {
			v, _, err := key.GetStringValue("pv")
			key.Close()
			if err == nil && v != "" && v != "0.0.0.0" {
				return true
			}
		}
	}
	return false
}

func messageBox(text string) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString("ZipNest")
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x10)
}

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
