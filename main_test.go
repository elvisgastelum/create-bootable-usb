package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func runMain(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, strings.NewReader("\n"), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := runMain("--version")
	if code != 0 || !strings.HasPrefix(out, "create-bootable-usb dev") {
		t.Fatalf("--version = %d %q", code, out)
	}
}

// The relaunched Windows process receives "--elevated <image>"; the image
// must be checked, not rejected as a usage error.
func TestElevatedFlagBeforeAnImageIsAccepted(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.iso")
	code, _, errOut := runMain("--elevated", missing)
	if code != 1 || !strings.Contains(errOut, "does not exist") {
		t.Fatalf("exit %d, stderr %q; want the missing-image error", code, errOut)
	}
	if !strings.Contains(errOut, "Press Enter") {
		t.Fatal("an elevated window must wait before closing on an error")
	}
}

func TestTwoImagesAreAUsageError(t *testing.T) {
	code, _, errOut := runMain("a.iso", "b.iso")
	if code != 2 || !strings.Contains(errOut, "Usage:") {
		t.Fatalf("exit %d, stderr %q; want usage and exit 2", code, errOut)
	}
}
