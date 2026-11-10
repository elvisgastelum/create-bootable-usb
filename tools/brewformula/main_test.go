package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/formula.golden")

func checksums(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRenderMatchesGolden(t *testing.T) {
	got, err := render("v1.2.3", checksums(t)) // a leading v is accepted
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "formula.golden")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("formula differs from %s (run go test ./tools/brewformula -update):\n%s", golden, got)
	}
}

func TestRenderPointsEachPlatformAtItsArchive(t *testing.T) {
	got, err := render("1.2.3", checksums(t))
	if err != nil {
		t.Fatal(err)
	}
	base := "https://github.com/elvisgastelum/create-bootable-usb/releases/download/v1.2.3/"
	for archive, sum := range map[string]string{
		"create-bootable-usb_macos_arm64.tar.gz":  "3333",
		"create-bootable-usb_macos_x86_64.tar.gz": "4444",
		"create-bootable-usb_linux_arm64.tar.gz":  "1111",
		"create-bootable-usb_linux_x86_64.tar.gz": "2222",
	} {
		url := `url "` + base + archive + `"`
		i := strings.Index(got, url)
		if i < 0 {
			t.Fatalf("formula has no %s", url)
		}
		// The sha256 line right after the url must belong to that archive.
		next := got[i+len(url):]
		if !strings.HasPrefix(strings.TrimSpace(next), `sha256 "`+strings.Repeat(sum, 16)+`"`) {
			t.Errorf("%s is not followed by its own sha256", archive)
		}
	}
	if strings.Contains(got, "windows") {
		t.Error("Homebrew has no Windows build; the formula must not mention it")
	}
	// brew audit --strict rejects a version Homebrew can already read from the URL.
	if strings.Contains(got, "\n  version ") {
		t.Error("formula must not declare a version: brew reads it from the url (v1.2.3)")
	}
}

func TestRenderRejectsBadInput(t *testing.T) {
	missing := strings.ReplaceAll(string(checksums(t)), "create-bootable-usb_linux_arm64.tar.gz", "other.tar.gz")
	badHash := strings.Replace(string(checksums(t)), strings.Repeat("3", 64), "not-a-hash", 1)
	tests := []struct {
		name, version, checksums, want string
	}{
		{"archive missing from checksums", "1.2.3", missing, "create-bootable-usb_linux_arm64.tar.gz"},
		{"malformed hash", "1.2.3", badHash, "sha256"},
		{"pre-release version", "1.2.3-rc.1", string(checksums(t)), "MAJOR.MINOR.PATCH"},
		{"empty version", "", string(checksums(t)), "MAJOR.MINOR.PATCH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := render(tt.version, []byte(tt.checksums))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}
