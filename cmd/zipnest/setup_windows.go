//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
)

// Exit-code bits returned by "ZipNest.exe --setup-dialog" to the installer.
const (
	setupDesktop   = 1
	setupStartMenu = 2
)

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
