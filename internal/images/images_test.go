package images

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path string, data []byte, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func hybridImage() []byte {
	img := make([]byte, 4096)
	img[510], img[511] = 0x55, 0xAA
	return img
}

func TestDiscoverFindsImagesNewestFirst(t *testing.T) {
	downloads, desktop := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(downloads, "old.iso"), hybridImage(), 48*time.Hour)
	writeFile(t, filepath.Join(downloads, "proxmox-ve_9.2-1.ISO"), hybridImage(), time.Hour)
	writeFile(t, filepath.Join(desktop, "raspios.img"), hybridImage(), 2*time.Hour)
	writeFile(t, filepath.Join(downloads, "notes.txt"), []byte("hi"), 0)
	writeFile(t, filepath.Join(downloads, "empty.iso"), nil, 0)
	if err := os.Mkdir(filepath.Join(downloads, "folder.iso"), 0o755); err != nil {
		t.Fatal(err)
	}

	// The same folder twice and a missing folder must not break discovery.
	found := Discover([]string{downloads, desktop, downloads, filepath.Join(downloads, "missing")})

	var names []string
	for _, c := range found {
		names = append(names, filepath.Base(c.Path))
	}
	want := []string{"proxmox-ve_9.2-1.ISO", "raspios.img", "old.iso"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("Discover() = %v, want %v", names, want)
	}
	if found[0].Size != 4096 {
		t.Fatalf("size = %d, want 4096", found[0].Size)
	}
}

func TestInspect(t *testing.T) {
	dir := t.TempDir()
	hybrid := filepath.Join(dir, "hybrid.iso")
	plain := filepath.Join(dir, "windows.iso")
	empty := filepath.Join(dir, "empty.iso")
	writeFile(t, hybrid, hybridImage(), 0)
	writeFile(t, plain, make([]byte, 4096), 0)
	writeFile(t, empty, nil, 0)

	info, err := Inspect(hybrid)
	if err != nil || !info.Hybrid || info.Size != 4096 {
		t.Fatalf("Inspect(hybrid) = %+v, %v; want a 4096-byte hybrid image", info, err)
	}
	info, err = Inspect(plain)
	if err != nil || info.Hybrid {
		t.Fatalf("Inspect(plain) = %+v, %v; want a non-hybrid image", info, err)
	}
	for _, bad := range []string{empty, dir, filepath.Join(dir, "missing.iso")} {
		if _, err := Inspect(bad); err == nil {
			t.Errorf("Inspect(%s) must fail", filepath.Base(bad))
		}
	}
}

func TestExpandAndShorten(t *testing.T) {
	home := t.TempDir()
	if got := Expand("~/Downloads/a.iso", home); got != filepath.Join(home, "Downloads", "a.iso") {
		t.Errorf("Expand(~/...) = %q", got)
	}
	if got := Expand(`  "/tmp/my image.iso"  `, home); got != filepath.Clean("/tmp/my image.iso") {
		t.Errorf("Expand must trim spaces and quotes pasted from a file manager, got %q", got)
	}
	if got := Shorten(filepath.Join(home, "Downloads", "a.iso"), home); got != filepath.Join("~", "Downloads", "a.iso") {
		t.Errorf("Shorten() = %q", got)
	}
}

func TestXDGDirs(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cfg, "user-dirs.dirs"), []byte(`# comment
XDG_DESKTOP_DIR="$HOME/Escritorio"
XDG_DOWNLOAD_DIR="$HOME/Descargas"
`), 0)
	got := xdgDirs(home)
	want := []string{filepath.Join(home, "Descargas"), filepath.Join(home, "Escritorio")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("xdgDirs() = %v, want %v", got, want)
	}
}
