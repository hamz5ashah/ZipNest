package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{
		"docs/a.txt":           []byte("hello world\n"),
		"docs/sub/b.bin":       bytes.Repeat([]byte{1, 2, 3, 250}, 50000),
		"docs/sub/ünïcødé.txt": []byte("名前"),
		"docs/empty.txt":       {},
	}
	for name, data := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func checkTree(t *testing.T, root string, want map[string][]byte) {
	t.Helper()
	for name, data := range want {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("%s: content mismatch", name)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		format   Format
		password string
	}{
		{FormatZip, ""}, {FormatZip, "s3cret"}, {FormatTar, ""}, {FormatTarGz, ""},
		{FormatTarXz, ""}, {FormatTarZst, ""},
	} {
		t.Run(string(tc.format)+"/"+tc.password, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			want := writeTree(t, src)
			arc := filepath.Join(dir, "out."+string(tc.format))
			if err := Create(arc, []string{filepath.Join(src, "docs")}, CreateOptions{Format: tc.format, Level: 6, Password: tc.password}); err != nil {
				t.Fatal(err)
			}
			if f, err := Detect(arc); err != nil || f != tc.format {
				t.Fatalf("Detect = %v, %v", f, err)
			}
			if tc.password != "" {
				if _, err := Extract(arc, filepath.Join(dir, "nopw"), ExtractOptions{}); !errors.Is(err, ErrPasswordRequired) {
					t.Fatalf("want ErrPasswordRequired, got %v", err)
				}
				if _, err := Test(arc, "wrong", nil); !errors.Is(err, ErrBadPassword) {
					t.Fatalf("want ErrBadPassword, got %v", err)
				}
			}
			res, err := Test(arc, tc.password, nil)
			if err != nil || len(res.Errors) > 0 || res.Files != len(want) {
				t.Fatalf("Test: %+v %v", res, err)
			}
			out := filepath.Join(dir, "x")
			if _, err := Extract(arc, out, ExtractOptions{Password: tc.password}); err != nil {
				t.Fatal(err)
			}
			checkTree(t, out, want)
		})
	}
}

func TestSelectBaseAndDelete(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeTree(t, src)
	arc := filepath.Join(dir, "a.zip")
	if err := Create(arc, []string{filepath.Join(src, "docs")}, CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	// Extract one folder relative to its parent, like the archive browser does.
	out := filepath.Join(dir, "x")
	res, err := Extract(arc, out, ExtractOptions{Select: []string{"docs/sub"}, BaseDir: "docs"})
	if err != nil || res.Files != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(out, "sub", "b.bin")); err != nil {
		t.Fatal(err)
	}
	// Add a file into a folder of the existing archive (update mode).
	extra := filepath.Join(dir, "new.txt")
	os.WriteFile(extra, []byte("new"), 0o644)
	if err := Create(arc, []string{extra}, CreateOptions{BaseDir: "docs/sub"}); err != nil {
		t.Fatal(err)
	}
	if n, err := Delete(arc, []string{"docs/a.txt"}); err != nil || n != 1 {
		t.Fatalf("Delete = %d, %v", n, err)
	}
	entries, _, err := List(arc, "")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.Dir {
			names = append(names, e.Path)
		}
	}
	sort.Strings(names)
	want := []string{"docs/empty.txt", "docs/sub/b.bin", "docs/sub/new.txt", "docs/sub/ünïcødé.txt"}
	if len(names) != len(want) {
		t.Fatalf("entries = %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("entries = %v", names)
		}
	}
	kids := Children(entries, "docs")
	if len(kids) != 2 || !kids[0].Dir || kids[0].Path != "docs/sub" {
		t.Fatalf("Children = %+v", kids)
	}
}

func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{
		"a/b.txt": "a/b.txt", "/etc/passwd": "etc/passwd", `C:\x\y`: "x/y",
		"../evil": "", "a/../../b": "", "./a//b/": "a/b",
	} {
		if got := CleanPath(in); got != want {
			t.Errorf("CleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatFromName(t *testing.T) {
	for in, want := range map[string]Format{
		"x.ZIP": FormatZip, "x.tar.gz": FormatTarGz, "x.tgz": FormatTarGz, "a.part1.rar": FormatRar,
		"a.r01": FormatRar, "b.7z": Format7z, "c.txt": "",
	} {
		if got := FormatFromName(in); got != want {
			t.Errorf("FormatFromName(%q) = %q, want %q", in, got, want)
		}
	}
	if ArchiveStem("/x/photos.tar.gz") != "photos" {
		t.Error("ArchiveStem")
	}
}

func TestSevenZipRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name      string
		level     int
		password  string
		hideNames bool
	}{{"lzma2", 6, "", false}, {"store", 0, "", false}, {"aes", 5, "pw", false}, {"aes-names", 5, "pw", true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			want := writeTree(t, src)
			os.MkdirAll(filepath.Join(src, "docs", "emptydir"), 0o755)
			arc := filepath.Join(dir, "a.7z")
			err := Create(arc, []string{filepath.Join(src, "docs")}, CreateOptions{Level: tc.level, Password: tc.password, EncryptNames: tc.hideNames})
			if err != nil {
				t.Fatal(err)
			}
			if tc.hideNames {
				if _, _, err := List(arc, ""); !errors.Is(err, ErrPasswordRequired) {
					t.Fatalf("hidden names: want ErrPasswordRequired, got %v", err)
				}
			}
			res, err := Test(arc, tc.password, nil)
			if err != nil || len(res.Errors) > 0 || res.Files != len(want) {
				t.Fatalf("Test: %+v %v", res, err)
			}
			out := filepath.Join(dir, "x")
			if _, err := Extract(arc, out, ExtractOptions{Password: tc.password}); err != nil {
				t.Fatal(err)
			}
			checkTree(t, out, want)
			if st, err := os.Stat(filepath.Join(out, "docs", "emptydir")); err != nil || !st.IsDir() {
				t.Fatal("empty directory missing")
			}
		})
	}
}

func TestVolumes(t *testing.T) {
	for _, f := range []Format{FormatZip, Format7z, FormatTarGz} {
		t.Run(string(f), func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			want := writeTree(t, src)
			arc := filepath.Join(dir, "big."+string(f))
			if err := Create(arc, []string{filepath.Join(src, "docs")}, CreateOptions{Format: f, Level: 0, VolumeSize: 50000}); err != nil {
				t.Fatal(err)
			}
			parts := volumeParts(arc + ".001")
			if len(parts) < 3 {
				t.Fatalf("expected several parts, got %v", parts)
			}
			if _, err := os.Stat(arc); err == nil {
				t.Fatal("unsplit archive should be removed")
			}
			out := filepath.Join(dir, "x")
			if _, err := Extract(arc+".001", out, ExtractOptions{}); err != nil {
				t.Fatal(err)
			}
			checkTree(t, out, want)
		})
	}
}

func TestSFXAndConvert(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	want := writeTree(t, src)
	stub := append([]byte("MZ"), bytes.Repeat([]byte{0x90}, 5000)...) // stand-in for the stub program
	exe := filepath.Join(dir, "setup.exe")
	if err := Create(exe, []string{filepath.Join(src, "docs")}, CreateOptions{SFXStub: stub, Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	if f, err := Detect(exe); err != nil || f != FormatZip {
		t.Fatalf("Detect(sfx) = %v %v", f, err)
	}
	if _, err := Extract(exe, filepath.Join(dir, "x"), ExtractOptions{Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	checkTree(t, filepath.Join(dir, "x"), want)

	conv := filepath.Join(dir, "converted.7z")
	if err := Convert(exe, conv, ConvertOptions{Password: "pw", Create: CreateOptions{Level: 3}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(conv, filepath.Join(dir, "y"), ExtractOptions{}); err != nil {
		t.Fatal(err)
	}
	checkTree(t, filepath.Join(dir, "y"), want)
}

func TestNestRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name     string
		level    int
		password string
		hide     bool
	}{{"zstd", 6, "", false}, {"store", 0, "", false}, {"aes", 3, "pw", false}, {"aes-names", 9, "pw", true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			want := writeTree(t, src)
			os.MkdirAll(filepath.Join(src, "docs", "emptydir"), 0o755)
			arc := filepath.Join(dir, "a.nest")
			err := Create(arc, []string{filepath.Join(src, "docs")}, CreateOptions{Level: tc.level, Password: tc.password,
				EncryptNames: tc.hide, Comment: "hello", Lock: true})
			if err != nil {
				t.Fatal(err)
			}
			if f, _ := Detect(arc); f != FormatNest {
				t.Fatalf("Detect = %v", f)
			}
			if tc.hide {
				if _, _, err := List(arc, ""); !errors.Is(err, ErrPasswordRequired) {
					t.Fatalf("want ErrPasswordRequired, got %v", err)
				}
			}
			if tc.password != "" {
				if _, err := Test(arc, "wrong", nil); !errors.Is(err, ErrBadPassword) {
					t.Fatalf("want ErrBadPassword, got %v", err)
				}
				if !tc.hide {
					if _, err := Extract(arc, filepath.Join(dir, "nopw"), ExtractOptions{}); !errors.Is(err, ErrPasswordRequired) {
						t.Fatalf("want ErrPasswordRequired, got %v", err)
					}
				}
			}
			res, err := Test(arc, tc.password, nil)
			if err != nil || len(res.Errors) > 0 || res.Files != len(want) {
				t.Fatalf("Test: %+v %v", res, err)
			}
			out := filepath.Join(dir, "x")
			if _, err := Extract(arc, out, ExtractOptions{Password: tc.password}); err != nil {
				t.Fatal(err)
			}
			checkTree(t, out, want)
			d, err := NestInfo(arc, tc.password)
			if err != nil || !d.Locked || d.Comment != "hello" {
				t.Fatalf("NestInfo = %+v %v", d, err)
			}
		})
	}
}

func TestNestRecovery(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0o755)
	want := map[string][]byte{}
	// ~3 MB of mixed data so the archive has many shards.
	for i := 0; i < 6; i++ {
		data := make([]byte, 500_000)
		for j := range data {
			data[j] = byte((j*7 + i*13) ^ (j >> 5))
		}
		name := filepath.Join(src, fmt.Sprintf("f%d.bin", i))
		os.WriteFile(name, data, 0o644)
		want[fmt.Sprintf("src/f%d.bin", i)] = data
	}
	arc := filepath.Join(dir, "r.nest")
	if err := Create(arc, []string{src}, CreateOptions{Level: 0, RecoveryPercent: 5, Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(arc)
	raw, _ := os.ReadFile(arc)
	// Damage: a contiguous run of 2% of the file plus scattered bytes.
	burst := len(raw) / 50
	for i := 0; i < burst; i++ {
		raw[len(raw)/3+i] ^= 0xFF
	}
	for i := 0; i < 20; i++ {
		raw[1000+i*9000] ^= 0x55
	}
	os.WriteFile(arc, raw, 0o644)
	if _, err := Test(arc, "pw", nil); err == nil {
		res, _ := Test(arc, "pw", nil)
		if len(res.Errors) == 0 {
			t.Fatal("damaged archive passed the test")
		}
	}
	res, err := Repair(arc, "", nil)
	if err != nil {
		t.Fatalf("Repair: %+v %v", res, err)
	}
	if res.Damaged == 0 || res.Repaired != res.Damaged {
		t.Fatalf("Repair result %+v", res)
	}
	fixed := filepath.Join(dir, "fixed.r.nest")
	if st, _ := os.Stat(fixed); st == nil || st.Size() != info.Size() {
		t.Fatal("repaired archive missing or wrong size")
	}
	out := filepath.Join(dir, "x")
	if _, err := Extract(fixed, out, ExtractOptions{Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	checkTree(t, out, want)
	// A second repair finds nothing to do.
	if r2, err := Repair(fixed, "", nil); err != nil || r2.Damaged != 0 {
		t.Fatalf("second repair: %+v %v", r2, err)
	}
}
