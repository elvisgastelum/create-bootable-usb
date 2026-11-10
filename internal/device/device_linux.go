//go:build linux

package device

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func list() ([]Device, error) {
	out, err := run("lsblk", "-J", "-b", "-o", lsblkColumns)
	if err != nil {
		return nil, err
	}
	return parseLsblk(out, sysfsMMCType)
}

// sysfsMMCType reads the card type ("SD", "MMC", "SDIO") of an mmcblk device.
func sysfsMMCType(name string) string {
	data, err := os.ReadFile("/sys/block/" + name + "/device/type")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func prepare(d Device) error {
	mounts := append([]string(nil), d.Mounts...)
	// Unmount nested mount points before their parents.
	sort.Slice(mounts, func(i, j int) bool { return len(mounts[i]) > len(mounts[j]) })
	for _, m := range mounts {
		if _, err := run("umount", m); err != nil {
			return fmt.Errorf("unmount %s: %w", m, err)
		}
	}
	return nil
}

// openDevice uses O_EXCL, which on Linux makes open fail with EBUSY while
// any partition of the disk is still mounted or held by another program.
func openDevice(d Device) (*os.File, error) {
	f, err := os.OpenFile(d.RawPath, os.O_RDWR|syscall.O_EXCL, 0)
	if errors.Is(err, syscall.EBUSY) {
		return nil, fmt.Errorf("open %s: the device is busy (still mounted or used by another program)", d.RawPath)
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", d.RawPath, err)
	}
	return f, nil
}

// flush writes the page cache to the device, then drops the cached pages so
// verification reads back from the USB device instead of from memory.
func flush(f *os.File) error {
	if err := f.Sync(); err != nil {
		return err
	}
	_ = unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED)
	return nil
}

func finish(Device) error { return nil }
