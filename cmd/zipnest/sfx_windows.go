//go:build windows

package main

import (
	_ "embed"

	"zipnest/cli"
)

// sfxStub is the self-extractor program (cmd/zipnest-sfx), built by build.sh
// into sfx/zipnest-sfx.exe before ZipNest.exe is compiled.
//
//go:embed sfx/zipnest-sfx.exe
var sfxStub []byte

func init() { cli.SFXStub = sfxStub }
