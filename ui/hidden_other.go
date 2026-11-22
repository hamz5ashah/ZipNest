//go:build !windows

package ui

import (
	"io/fs"
	"strings"
)

func isHidden(_ string, name string, _ fs.FileInfo) bool { return strings.HasPrefix(name, ".") }
