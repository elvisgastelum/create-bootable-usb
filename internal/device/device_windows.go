//go:build windows

package device

import (
	"fmt"
	"os"
)

func list() ([]Device, error) {
	out, err := run("powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(windowsDiskScript))
	if err != nil {
		return nil, fmt.Errorf("list disks: %w", err)
	}
	return parseWindowsDisks(out)
}

// prepare removes every partition with `diskpart clean`. Windows refuses raw
// writes to sectors that belong to a mounted volume; a clean disk has none.
func prepare(d Device) error {
	script, err := os.CreateTemp("", "create-bootable-usb-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(script.Name())
	_, err = fmt.Fprintf(script, "select disk %d\r\nclean\r\n", d.Number)
	if cerr := script.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if _, err := run("diskpart.exe", "/s", script.Name()); err != nil {
		return fmt.Errorf("clean disk %d: %w", d.Number, err)
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

func flush(f *os.File) error { return f.Sync() }

func finish(Device) error { return nil }
