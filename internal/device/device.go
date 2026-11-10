// Package device discovers disks that a bootable image can be written to and
// prepares them for raw writes on macOS, Linux and Windows.
package device

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/elvisgastelum/create-bootable-usb/internal/format"
)

// Device is a whole disk (never a partition) attached to this computer.
type Device struct {
	ID        string   // short name: "disk6", "sdb", "PhysicalDrive2"
	Path      string   // node that identifies the disk: /dev/disk6, /dev/sdb, \\.\PhysicalDrive2
	RawPath   string   // node opened for writing; /dev/rdiskN on macOS (unbuffered, much faster)
	Number    int      // Windows disk number, used by diskpart; 0 elsewhere
	Model     string   // vendor and model as reported by the OS
	Size      int64    // capacity in bytes
	Bus       string   // "USB", "SD", "Secure Digital", "PCI-Express", ...
	Removable bool     // the OS reports removable media
	System    bool     // holds the running operating system; never writable
	Labels    []string // volume labels of its partitions, e.g. "USB"
	Mounts    []string // mount points or drive letters currently in use
}

// Problem explains why d must not be offered as a target for an image of
// imageSize bytes. It returns "" when d is a valid target. An imageSize of
// zero or less skips the capacity check.
func (d Device) Problem(imageSize int64) string {
	switch {
	case d.System:
		return "holds the running operating system"
	case d.Size <= 0:
		return "no media inserted"
	case !d.Removable && !removableBus(d.Bus):
		return "not a removable or USB device"
	case imageSize > 0 && d.Size < imageSize:
		return fmt.Sprintf("too small (the image needs %s)", format.Bytes(imageSize))
	}
	return ""
}

// Name is the model, or a placeholder when the OS does not report one.
func (d Device) Name() string {
	if strings.TrimSpace(d.Model) == "" {
		return "Unknown device"
	}
	return d.Model
}

// Summary describes the device in one line: model, size, bus and labels.
func (d Device) Summary() string {
	parts := []string{d.Name(), format.Bytes(d.Size)}
	if d.Bus != "" {
		parts = append(parts, d.Bus)
	}
	if len(d.Labels) > 0 {
		quoted := make([]string, len(d.Labels))
		for i, l := range d.Labels {
			quoted[i] = fmt.Sprintf("%q", l)
		}
		parts = append(parts, strings.Join(quoted, ", "))
	}
	return strings.Join(parts, " · ")
}

func removableBus(bus string) bool {
	switch strings.ToLower(strings.TrimSpace(bus)) {
	case "usb", "sd", "secure digital", "mmc":
		return true
	}
	return false
}

// List returns the removable and external disks attached to this computer,
// including ones that are not valid targets; check Device.Problem before
// offering one. Internal fixed disks are never returned.
func List() ([]Device, error) { return list() }

// Handle is a device opened for raw I/O.
type Handle interface {
	io.ReadWriteSeeker
	io.Closer
	// Flush makes sure written data reached the device and drops cached
	// copies, so a read-back verification reads the media itself.
	Flush() error
}

// Host performs device operations against the real operating system.
type Host struct{}

// List returns the attached removable and external disks.
func (Host) List() ([]Device, error) { return list() }

// Prepare releases the device for raw writes: it unmounts its volumes
// (macOS, Linux) or removes its partitions with diskpart (Windows).
func (Host) Prepare(d Device) error { return prepare(d) }

// Open opens the device for raw reads and writes.
func (Host) Open(d Device) (Handle, error) {
	f, err := openDevice(d)
	if err != nil {
		return nil, err
	}
	return fileHandle{f}, nil
}

// Finish runs after the image was written; on macOS it ejects the disk.
func (Host) Finish(d Device) error { return finish(d) }

type fileHandle struct{ *os.File }

func (h fileHandle) Flush() error { return flush(h.File) }
