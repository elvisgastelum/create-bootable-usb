//go:build darwin

package device

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func list() ([]Device, error) {
	listOut, err := run("diskutil", "list", "-plist", "physical")
	if err != nil {
		return nil, err
	}
	whole, err := parseDarwinWholeDisks(listOut)
	if err != nil {
		return nil, err
	}
	// APFS volumes live on synthesized container disks that the physical
	// listing leaves out; the full listing maps them to their physical disk.
	allOut, err := run("diskutil", "list", "-plist")
	if err != nil {
		return nil, err
	}
	infos := make(map[string][]byte, len(whole))
	for _, id := range whole {
		out, err := run("diskutil", "info", "-plist", id)
		if err != nil {
			continue // the disk disappeared between the two calls
		}
		infos[id] = out
	}
	// Without knowing which disk holds "/" we cannot protect it, so this
	// failure is fatal rather than skipped.
	root, err := run("diskutil", "info", "-plist", "/")
	if err != nil {
		return nil, fmt.Errorf("locate the boot disk: %w", err)
	}
	return parseDarwinDevices(listOut, allOut, infos, root)
}

func prepare(d Device) error {
	for _, c := range d.Containers {
		if _, err := run("diskutil", "unmountDisk", "force", "/dev/"+c); err != nil {
			return fmt.Errorf("unmount the APFS volumes of %s: %w", d.Path, err)
		}
	}
	if _, err := run("diskutil", "unmountDisk", "force", d.Path); err != nil {
		return fmt.Errorf("unmount %s: %w", d.Path, err)
	}
	return nil
}

func openDevice(d Device) (*os.File, error) {
	f, err := os.OpenFile(d.RawPath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", d.RawPath, err)
	}
	return f, nil
}

// flush asks the driver to commit outstanding writes. Raw (/dev/rdisk)
// writes bypass the buffer cache, and some drivers reject fsync on character
// devices, so "not supported" answers are not errors.
func flush(f *os.File) error {
	err := syscall.Fsync(int(f.Fd()))
	if errors.Is(err, syscall.ENOTTY) || errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.ENODEV) {
		return nil
	}
	return err
}

func finish(d Device) error {
	if _, err := run("diskutil", "eject", d.Path); err != nil {
		return fmt.Errorf("eject %s: %w", d.Path, err)
	}
	return nil
}
