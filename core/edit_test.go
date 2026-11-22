package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func names(t *testing.T, p, pw string) string {
	t.Helper()
	ents, _, err := List(p, pw)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range ents {
		if !e.Dir {
			n = append(n, e.Path)
		}
	}
	sort.Strings(n)
	return strings.Join(n, ",")
}

func TestEdit(t *testing.T) {
	for _, tc := range []struct {
		f  Format
		pw string
	}{{FormatZip, ""}, {FormatZip, "pw"}, {Format7z, ""}, {Format7z, "pw"}, {FormatNest, ""}, {FormatNest, "pw"}, {FormatTarGz, ""}} {
		t.Run(string(tc.f)+tc.pw, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			os.MkdirAll(filepath.Join(src, "sub"), 0o755)
			os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello a"), 0o644)
			os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("hello b"), 0o644)
			os.WriteFile(filepath.Join(src, "skip.log"), []byte("x"), 0o644)
			arc := filepath.Join(dir, "t."+string(tc.f))
			if err := Create(arc, []string{src}, CreateOptions{Format: tc.f, Password: tc.pw, Exclude: []string{"*.log"}}); err != nil {
				t.Fatal(err)
			}
			if got := names(t, arc, tc.pw); got != "src/a.txt,src/sub/b.txt" {
				t.Fatalf("create/exclude: %s", got)
			}
			extra := filepath.Join(dir, "new.txt")
			os.WriteFile(extra, []byte("new file"), 0o644)
			opt := EditOptions{Password: tc.pw}
			if err := Update(arc, []string{extra}, "src/sub", nil, opt); err != nil {
				t.Fatal(err)
			}
			if got := names(t, arc, tc.pw); got != "src/a.txt,src/sub/b.txt,src/sub/new.txt" {
				t.Fatalf("update: %s", got)
			}
			if tc.f != FormatTarGz {
				if err := Rename(arc, "src/sub", "folder", opt); err != nil {
					t.Fatal(err)
				}
				if got := names(t, arc, tc.pw); got != "src/a.txt,src/folder/b.txt,src/folder/new.txt" {
					t.Fatalf("rename: %s", got)
				}
			}
			n, err := DeleteFrom(arc, []string{"src/a.txt"}, opt)
			if err != nil || n != 1 {
				t.Fatal(n, err)
			}
			data, size, err := ReadEntry(arc, tc.pw, strings.Split(names(t, arc, tc.pw), ",")[1], 100)
			if err != nil || string(data) != "new file" || size != 8 {
				t.Fatalf("read: %q %d %v", data, size, err)
			}
			if tc.pw != "" {
				if _, _, err := ReadEntry(arc, "", strings.Split(names(t, arc, tc.pw), ",")[1], 100); err == nil {
					t.Fatal("password kept after edit: expected error")
				}
			}
			in, err := Info(arc, tc.pw, nil)
			if err != nil || in.Files != 2 || !in.Writable || in.Encrypted != (tc.pw != "") {
				t.Fatalf("info %+v %v", in, err)
			}
			if tc.f == FormatZip || tc.f == FormatNest {
				if err := SetComment(arc, "my comment", opt); err != nil {
					t.Fatal(err)
				}
				if in, _ := Info(arc, tc.pw, nil); in.Comment != "my comment" {
					t.Fatalf("comment %q", in.Comment)
				}
			}
		})
	}
}
