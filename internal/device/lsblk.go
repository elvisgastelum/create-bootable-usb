package device

import (
	"encoding/json"
	"fmt"
	"strings"
)

// lsblkColumns are the columns requested from `lsblk -J -b -o`. They exist in
// every util-linux release that supports JSON output (2.27+).
const lsblkColumns = "NAME,SIZE,MODEL,VENDOR,TRAN,RM,HOTPLUG,TYPE,MOUNTPOINT,LABEL"

type lsblkDev struct {
	Name       string     `json:"name"`
	Size       flexInt    `json:"size"`
	Model      string     `json:"model"`
	Vendor     string     `json:"vendor"`
	Tran       string     `json:"tran"`
	RM         flexBool   `json:"rm"`
	Hotplug    flexBool   `json:"hotplug"`
	Type       string     `json:"type"`
	Mountpoint string     `json:"mountpoint"`
	Label      string     `json:"label"`
	Children   []lsblkDev `json:"children"`
}

// systemMounts are mount points whose disk runs this computer: the root
// file system, boot partitions and the media of common live systems.
var systemMounts = map[string]bool{
	"/": true, "/boot": true, "/boot/efi": true, "/efi": true,
	"/usr": true, "/var": true, "/home": true, "[SWAP]": true,
	"/run/live/medium": true, "/lib/live/mount/medium": true,
	"/run/archiso/bootmnt": true, "/run/initramfs/live": true, "/cdrom": true,
}

// parseLsblk turns `lsblk -J -b -o <lsblkColumns>` output into devices.
// Only whole disks that are removable, hot-pluggable, on USB or SD/MMC
// cards are returned; loop devices and fixed internal disks are dropped.
func parseLsblk(data []byte) ([]Device, error) {
	var out struct {
		Blockdevices []lsblkDev `json:"blockdevices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse lsblk output: %w", err)
	}

	var devices []Device
	for _, b := range out.Blockdevices {
		if b.Type != "disk" {
			continue
		}
		tran := strings.ToLower(strings.TrimSpace(b.Tran))
		sdCard := strings.HasPrefix(b.Name, "mmcblk")
		if !bool(b.RM) && !bool(b.Hotplug) && tran != "usb" && !sdCard {
			continue
		}
		bus := strings.ToUpper(tran)
		if bus == "" && sdCard {
			bus = "SD"
		}
		d := Device{
			ID:        b.Name,
			Path:      "/dev/" + b.Name,
			RawPath:   "/dev/" + b.Name,
			Model:     joinModel(b.Vendor, b.Model),
			Size:      int64(b.Size),
			Bus:       bus,
			Removable: bool(b.RM),
		}
		b.walk(func(n lsblkDev) {
			if n.Label != "" {
				d.Labels = append(d.Labels, n.Label)
			}
			if n.Mountpoint == "" {
				return
			}
			if systemMounts[n.Mountpoint] {
				d.System = true
			}
			if n.Mountpoint != "[SWAP]" {
				d.Mounts = append(d.Mounts, n.Mountpoint)
			}
		})
		devices = append(devices, d)
	}
	return devices, nil
}

// walk visits b and every nested child (partitions, LUKS and LVM volumes).
func (b lsblkDev) walk(visit func(lsblkDev)) {
	visit(b)
	for _, c := range b.Children {
		c.walk(visit)
	}
}

// joinModel combines lsblk's padded VENDOR and MODEL columns.
func joinModel(vendor, model string) string {
	v := strings.Join(strings.Fields(vendor), " ")
	m := strings.Join(strings.Fields(model), " ")
	switch {
	case v == "":
		return m
	case m == "":
		return v
	case strings.HasPrefix(strings.ToLower(m), strings.ToLower(v)):
		return m
	}
	return v + " " + m
}
