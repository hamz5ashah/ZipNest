// Command winres generates rsrc_windows_<arch>.syso (icon, manifest, version info) for the
// Windows GUI build. Run it from cmd/zipnest:  go run ../../tools/winres
package main

import (
	"log"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

// usage (from the target's directory): go run ../../tools/winres [description] [original-filename]
func main() {
	desc, orig := "ZipNest archive manager", "ZipNest.exe"
	if len(os.Args) > 2 {
		desc, orig = os.Args[1], os.Args[2]
	}
	f, err := os.Open(iconPath())
	if err != nil {
		log.Fatal(err)
	}
	icon, err := winres.LoadICO(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	rs := winres.ResourceSet{}
	if err := rs.SetIcon(winres.ID(1), icon); err != nil {
		log.Fatal(err)
	}
	rs.SetManifest(winres.AppManifest{
		Description:         desc,
		Compatibility:       winres.Win7AndAbove,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
		LongPathAware:       true,
	})
	vi := version.Info{}
	vi.SetFileVersion("1.5.0.0")
	vi.SetProductVersion("1.5.0.0")
	vi.Set(version.LangNeutral, version.ProductName, "ZipNest")
	vi.Set(version.LangNeutral, version.FileDescription, desc)
	vi.Set(version.LangNeutral, version.OriginalFilename, orig)
	vi.Set(version.LangNeutral, version.LegalCopyright, "ZipNest contributors")
	rs.SetVersionInfo(vi)

	for _, arch := range []winres.Arch{winres.ArchAMD64, winres.ArchARM64, winres.ArchI386} {
		out, err := os.Create("rsrc_windows_" + string(arch) + ".syso")
		if err != nil {
			log.Fatal(err)
		}
		if err := rs.WriteObject(out, arch); err != nil {
			log.Fatal(err)
		}
		out.Close()
	}
	log.Println("resources written")
}

func iconPath() string {
	for _, p := range []string{"res/zipnest.ico", "../zipnest/res/zipnest.ico"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "res/zipnest.ico"
}
