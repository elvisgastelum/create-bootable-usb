//go:build darwin && integration

package burn

import (
	"bytes"
	"context"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elvisgastelum/create-bootable-usb/internal/device"
)

// ramDiskSystem runs the real macOS device steps (diskutil unmountDisk,
// raw /dev/rdisk I/O, diskutil eject) against a RAM disk, which the
// eligibility rules would otherwise hide because it is virtual.
type ramDiskSystem struct {
	device.Host
	disk device.Device
}

func (s ramDiskSystem) List() ([]device.Device, error) { return []device.Device{s.disk}, nil }

// TestRunOnRAMDisk writes an unaligned image to a 32 MiB RAM disk through
// /dev/rdiskN with verification, exercising sector alignment on a real raw
// device. Run it with: go test -tags integration ./internal/burn/
func TestRunOnRAMDisk(t *testing.T) {
	out, err := exec.Command("hdiutil", "attach", "-nomount", "ram://65536").Output()
	if err != nil {
		t.Skipf("cannot create a RAM disk: %v", err)
	}
	node := strings.Fields(string(out))[0] // /dev/diskN
	t.Cleanup(func() { exec.Command("hdiutil", "detach", "-force", node).Run() })
	raw := strings.Replace(node, "/dev/disk", "/dev/rdisk", 1)
	// hdiutil gives the attaching user read-only nodes; the owner may add write.
	if err := os.Chmod(raw, 0o640); err != nil {
		t.Fatal(err)
	}

	img := make([]byte, 5<<20+7) // the last chunk is not sector aligned
	rand.New(rand.NewSource(1)).Read(img)
	image := filepath.Join(t.TempDir(), "test.iso")
	if err := os.WriteFile(image, img, 0o644); err != nil {
		t.Fatal(err)
	}

	disk := device.Device{ID: strings.TrimPrefix(node, "/dev/"), Path: node, RawPath: raw,
		Model: "RAM disk", Size: 65536 * 512, Bus: "USB", Removable: true}
	sys := ramDiskSystem{disk: disk}

	// Read the device back independently before it is ejected.
	var readBack []byte
	res, err := Run(context.Background(), sys, Job{Image: image, Device: disk, Verify: true}, func(p Progress) {
		if p.Stage == StageFinishing && readBack == nil {
			f, err := os.Open(raw)
			if err != nil {
				t.Errorf("open for read-back: %v", err)
				return
			}
			defer f.Close()
			readBack = make([]byte, 5<<20+4096)
			if _, err := f.Read(readBack); err != nil {
				t.Errorf("read back: %v", err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Verified {
		t.Fatal("the write was not verified")
	}
	if !bytes.Equal(readBack[:len(img)], img) {
		t.Fatal("the RAM disk does not contain the image")
	}
	if res.Warning != "" {
		t.Logf("finish warning: %s", res.Warning)
	}
}
