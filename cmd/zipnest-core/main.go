// zipnest-core is the archive engine as a standalone command-line program.
// It is bundled inside the Android APK (as lib/<abi>/libzipnest.so so that
// Android installs it as an executable) and also works as a CLI on any OS.
package main

import (
	"os"

	"zipnest/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
