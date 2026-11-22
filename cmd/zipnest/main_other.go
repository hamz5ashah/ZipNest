//go:build !windows

// On non-Windows systems the desktop build is the command-line interface.
package main

import (
	"os"

	"zipnest/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
