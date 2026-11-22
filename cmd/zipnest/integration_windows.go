//go:build windows

package main

// Windows integration: file associations, the classic Explorer context menu
// (optionally cascaded, with icons) and the Windows 11 context menu (a COM
// extension registered through a small sparse MSIX package).
// Everything is per-user (HKCU) except the signing certificate the installer adds.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const configKey = `Software\ZipNest`

// fileType is one checkbox in "Associate ZipNest with".
type fileType struct {
	Label string
	Exts  []string
	On    bool // default
}

var fileTypes = []fileType{
	{"NEST", []string{".nest"}, true},
	{"RAR", []string{".rar"}, true},
	{"ZIP", []string{".zip"}, true},
	{"7Z", []string{".7z"}, true},
	{"TAR", []string{".tar"}, true},
	{"GZ", []string{".gz", ".tgz"}, true},
	{"BZ2", []string{".bz2", ".tbz2", ".tbz"}, true},
	{"XZ", []string{".xz", ".txz"}, true},
	{"ZST", []string{".zst", ".tzst"}, true},
	{"ZIPX", []string{".zipx"}, true},
	{"001", []string{".001"}, true},
	{"JAR", []string{".jar"}, false},
	{"CBR/CBZ", []string{".cbr", ".cbz", ".cb7"}, false},
}

// allArchiveExts lists every extension ZipNest can open (for menus).
func allArchiveExts() []string {
	var out []string
	for _, t := range fileTypes {
		out = append(out, t.Exts...)
	}
	return out
}

// integration holds the user's choices.
type integration struct {
	Assoc    map[string]bool // fileType label -> associated
	Shell    bool            // context menu entries
	Cascaded bool            // put them in a "ZipNest" submenu
	Icons    bool            // icons next to menu items
	Win11    bool            // Windows 11 modern context menu
}

func defaultIntegration() integration {
	in := integration{Assoc: map[string]bool{}, Shell: true, Cascaded: true, Icons: true, Win11: true}
	for _, t := range fileTypes {
		in.Assoc[t.Label] = t.On
	}
	return in
}

func loadIntegration() integration {
	in := defaultIntegration()
	k, err := registry.OpenKey(registry.CURRENT_USER, configKey, registry.QUERY_VALUE)
	if err != nil {
		return in
	}
	defer k.Close()
	if s, _, err := k.GetStringValue("Assoc"); err == nil {
		for l := range in.Assoc {
			in.Assoc[l] = false
		}
		for _, l := range strings.Split(s, ",") {
			if l != "" {
				in.Assoc[l] = true
			}
		}
	}
	get := func(name string, def bool) bool {
		v, _, err := k.GetIntegerValue(name)
		if err != nil {
			return def
		}
		return v != 0
	}
	in.Shell = get("Shell", in.Shell)
	in.Cascaded = get("Cascaded", in.Cascaded)
	in.Icons = get("MenuIcons", in.Icons)
	in.Win11 = get("Win11Menu", in.Win11)
	return in
}

func (in integration) save() {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, configKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	var on []string
	for l, v := range in.Assoc {
		if v {
			on = append(on, l)
		}
	}
	sort.Strings(on)
	k.SetStringValue("Assoc", strings.Join(on, ","))
	b := func(v bool) uint32 {
		if v {
			return 1
		}
		return 0
	}
	k.SetDWordValue("Shell", b(in.Shell))
	k.SetDWordValue("Cascaded", b(in.Cascaded))
	k.SetDWordValue("MenuIcons", b(in.Icons)) // also read by ZipNestShell.dll
	k.SetDWordValue("Win11Menu", b(in.Win11))
}

// apply makes Windows match the settings and remembers them.
func (in integration) apply() error {
	in.save()
	var errs []error
	errs = append(errs, applyAssociations(in.Assoc))
	unregisterShell()
	if in.Shell {
		errs = append(errs, registerShell(in.Cascaded, in.Icons))
	}
	if win11MenuAvailable() {
		errs = append(errs, setWin11Menu(in.Shell && in.Win11))
	}
	procSHChangeNotif.Call(0x08000000, 0, 0, 0) // SHCNE_ASSOCCHANGED
	return errors.Join(errs...)
}

// ---------------------------------------------------------------- associations

const progID = "ZipNest.Archive"

func applyAssociations(assoc map[string]bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	anyOn := false
	for _, v := range assoc {
		anyOn = anyOn || v
	}
	if anyOn {
		if err := setKey(progID, "ZipNest archive", nil); err != nil {
			return err
		}
		setKey(progID+`\DefaultIcon`, exe+",0", nil)
		setKey(progID+`\shell\open\command`, `"`+exe+`" "%1"`, nil)
		setKey(`Applications\ZipNest.exe\shell\open\command`, `"`+exe+`" "%1"`, nil)
	}
	for _, t := range fileTypes {
		for _, ext := range t.Exts {
			if assoc[t.Label] {
				setKey(`Applications\ZipNest.exe\SupportedTypes`, "", map[string]string{ext: ""})
				if err := setKey(ext+`\OpenWithProgids`, "", map[string]string{progID: ""}); err != nil {
					return err
				}
				// Become the default handler unless another program is set for this
				// extension (Windows protects the user's explicit choice).
				if k, err := registry.OpenKey(registry.CURRENT_USER, regRoot+ext, registry.QUERY_VALUE|registry.SET_VALUE); err == nil {
					if cur, _, err := k.GetStringValue(""); err != nil || cur == "" || cur == progID {
						k.SetStringValue("", progID)
					}
					k.Close()
				}
			} else {
				removeAssociation(ext)
			}
		}
	}
	if !anyOn {
		deleteTree(progID)
		deleteTree(`Applications\ZipNest.exe`)
	}
	return nil
}

func removeAssociation(ext string) {
	if k, err := registry.OpenKey(registry.CURRENT_USER, regRoot+ext+`\OpenWithProgids`, registry.SET_VALUE); err == nil {
		k.DeleteValue(progID)
		k.Close()
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, regRoot+ext, registry.QUERY_VALUE|registry.SET_VALUE); err == nil {
		if cur, _, err := k.GetStringValue(""); err == nil && cur == progID {
			k.DeleteValue("")
		}
		k.Close()
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, regRoot+`Applications\ZipNest.exe\SupportedTypes`, registry.SET_VALUE); err == nil {
		k.DeleteValue(ext)
		k.Close()
	}
}

func unassociateAll() {
	for _, ext := range allArchiveExts() {
		removeAssociation(ext)
	}
	deleteTree(progID)
	deleteTree(`Applications\ZipNest.exe`)
}

// ---------------------------------------------------------------- classic context menu

type menuItem struct {
	key, text, args string
	archivesOnly    bool
}

var menuItems = []menuItem{
	{"1open", "Open with ZipNest", `"%1"`, true},
	{"2extract", "Extract files...", `--extract-dialog "%1"`, true},
	{"3extractto", "Extract to subfolder", `--extract-auto "%1"`, true},
	{"4extracthere", "Extract Here", `--extract-here "%1"`, true},
	{"5test", "Test archive", `--test "%1"`, true},
	{"6convert", "Convert archive...", `--convert "%1"`, true},
	{"7add", "Add to archive...", `--add "%1"`, false},
	{"8addzip", "Add to ZIP (quick)", `--add-zip "%1"`, false},
}

// archiveQuery is an AQS condition matching archive files (registry "AppliesTo").
func archiveQuery() string {
	var parts []string
	for _, e := range allArchiveExts() {
		parts = append(parts, "System.FileExtension:="+e)
	}
	return strings.Join(parts, " OR ")
}

// registerShell adds ZipNest to the classic Explorer context menu (on Windows 11:
// under "Show more options").
func registerShell(cascaded, icons bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	q := `"` + exe + `"`
	query := archiveQuery()
	for _, base := range []string{`*`, `Directory`} {
		root := base + `\shell\ZipNest`
		if cascaded {
			v := map[string]string{"MUIVerb": "ZipNest", "SubCommands": ""}
			if icons {
				v["Icon"] = exe + ",0"
			}
			if err := setKey(root, "", v); err != nil {
				return err
			}
		}
		for _, it := range menuItems {
			if it.archivesOnly && base == `Directory` {
				continue
			}
			key := base + `\shell\ZipNest.` + it.key[1:]
			if cascaded {
				key = root + `\shell\` + it.key
			}
			v := map[string]string{"MUIVerb": it.text}
			if icons {
				v["Icon"] = exe + ",0"
			}
			if it.archivesOnly {
				v["AppliesTo"] = query
			}
			if err := setKey(key, "", v); err != nil {
				return err
			}
			if err := setKey(key+`\command`, q+" "+it.args, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func unregisterShell() {
	for _, base := range []string{`*`, `Directory`} {
		deleteTree(base + `\shell\ZipNest`)
		for _, it := range menuItems {
			deleteTree(base + `\shell\ZipNest.` + it.key[1:])
		}
		// entries written by ZipNest 1.0/1.1
		deleteTree(base + `\shell\ZipNest.add`)
		deleteTree(base + `\shell\ZipNest.addzip`)
	}
	for _, ext := range allArchiveExts() {
		for _, old := range []string{"ZipNest.open", "ZipNest.xhere", "ZipNest.xto"} {
			deleteTree(`SystemFileAssociations\` + ext + `\shell\` + old)
		}
	}
}

// ---------------------------------------------------------------- Windows 11 menu

const shellPackageName = "ZipNest.ShellMenu"

func windowsBuild() uint32 {
	type osVersionInfo struct {
		size, major, minor, build, platform uint32
		csd                                 [128]uint16
	}
	v := osVersionInfo{size: uint32(unsafe.Sizeof(osVersionInfo{}))}
	syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion").Call(uintptr(unsafe.Pointer(&v)))
	return v.build
}

func installDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

// win11MenuAvailable: Windows 11 and the package was installed next to ZipNest.exe.
func win11MenuAvailable() bool {
	if windowsBuild() < 22000 {
		return false
	}
	_, err := os.Stat(filepath.Join(installDir(), "ZipNestShell.msix"))
	return err == nil
}

func powershell(script string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

// setWin11Menu registers or removes the sparse package that adds "ZipNest" to the
// Windows 11 context menu (requires the installer's certificate to be trusted).
func setWin11Menu(on bool) error {
	dir := installDir()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	remove := "Get-AppxPackage -Name " + shellPackageName + " | Remove-AppxPackage"
	if !on {
		return powershell(remove)
	}
	return powershell(remove + "; Add-AppxPackage -Path " + quote(filepath.Join(dir, "ZipNestShell.msix")) +
		" -ExternalLocation " + quote(dir))
}
