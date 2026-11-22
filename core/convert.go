package core

import (
	"errors"
	"os"
	"path/filepath"
)

// ConvertOptions control Convert.
type ConvertOptions struct {
	Password string        // password of the source archive
	Create   CreateOptions // options for the new archive (format, level, password, ...)
}

// Convert repacks an archive into another format (e.g. .rar -> .7z): it is
// extracted into a temporary folder next to the destination and then packed.
func Convert(src, dst string, opt ConvertOptions) error {
	dst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	if s, _ := filepath.Abs(src); s == dst {
		return errors.New("the converted archive must have a different name")
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dst), ".zipnest-convert-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	progress := opt.Create.Progress
	if _, err := Extract(src, tmp, ExtractOptions{Password: opt.Password, Progress: progress}); err != nil {
		return err
	}
	ents, err := os.ReadDir(tmp)
	if err != nil {
		return err
	}
	if len(ents) == 0 {
		return errors.New("the archive is empty")
	}
	var inputs []string
	for _, e := range ents {
		inputs = append(inputs, filepath.Join(tmp, e.Name()))
	}
	if _, err := os.Stat(dst); err == nil {
		os.Remove(dst) // replace, do not "update"
	}
	return Create(dst, inputs, opt.Create)
}
