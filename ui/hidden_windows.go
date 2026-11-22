package ui

import (
	"io/fs"
	"strings"
	"syscall"
)

func isHidden(_ string, name string, info fs.FileInfo) bool {
	if d, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return d.FileAttributes&(syscall.FILE_ATTRIBUTE_HIDDEN|syscall.FILE_ATTRIBUTE_SYSTEM) != 0
	}
	return strings.HasPrefix(name, ".")
}
