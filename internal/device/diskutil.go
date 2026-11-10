package device

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"howett.net/plist"
)

// diskutilList is the subset of `diskutil list -plist [physical]` we use.
type diskutilList struct {
	WholeDisks            []string `plist:"WholeDisks"`
	AllDisksAndPartitions []struct {
		DeviceIdentifier string `plist:"DeviceIdentifier"`
		Size             int64  `plist:"Size"`
		VolumeName       string `plist:"VolumeName"`
		MountPoint       string `plist:"MountPoint"`
		Partitions       []struct {
			DeviceIdentifier string `plist:"DeviceIdentifier"`
			Content          string `plist:"Content"`
			VolumeName       string `plist:"VolumeName"`
			MountPoint       string `plist:"MountPoint"`
		} `plist:"Partitions"`
		// Set on synthesized APFS container disks (unfiltered listing only).
		APFSPhysicalStores []struct {
			DeviceIdentifier string `plist:"DeviceIdentifier"`
		} `plist:"APFSPhysicalStores"`
		APFSVolumes []struct {
			VolumeName string `plist:"VolumeName"`
			MountPoint string `plist:"MountPoint"`
		} `plist:"APFSVolumes"`
	} `plist:"AllDisksAndPartitions"`
}

// apfsRoleVolumes are system-managed APFS volumes not worth showing as labels.
var apfsRoleVolumes = map[string]bool{"Preboot": true, "Recovery": true, "VM": true, "Update": true}

// apfsContainer is a synthesized APFS disk and the volumes it holds.
type apfsContainer struct {
	id     string
	labels []string
	mounts []string
}

// diskutilInfo is the subset of `diskutil info -plist <disk>` we use.
type diskutilInfo struct {
	DeviceIdentifier   string `plist:"DeviceIdentifier"`
	DeviceNode         string `plist:"DeviceNode"`
	MediaName          string `plist:"MediaName"`
	Size               int64  `plist:"Size"`
	TotalSize          int64  `plist:"TotalSize"`
	BusProtocol        string `plist:"BusProtocol"`
	RemovableMedia     bool   `plist:"RemovableMedia"`
	Internal           bool   `plist:"Internal"`
	ParentWholeDisk    string `plist:"ParentWholeDisk"`
	APFSPhysicalStores []struct {
		APFSPhysicalStore string `plist:"APFSPhysicalStore"`
	} `plist:"APFSPhysicalStores"`
}

func decodePlist(data []byte, v any) error {
	return plist.NewDecoder(bytes.NewReader(data)).Decode(v)
}

// parseDarwinWholeDisks returns the whole-disk identifiers in a
// `diskutil list -plist` document.
func parseDarwinWholeDisks(list []byte) ([]string, error) {
	var l diskutilList
	if err := decodePlist(list, &l); err != nil {
		return nil, fmt.Errorf("parse diskutil list: %w", err)
	}
	return l.WholeDisks, nil
}

// parseDarwinDevices builds devices from `diskutil list -plist physical`,
// the unfiltered `diskutil list -plist` (which also holds the APFS
// containers whose volumes live on a physical disk), `diskutil info -plist
// <disk>` for each whole disk (keyed by identifier) and `diskutil info
// -plist /`, which locates the boot disk. Internal fixed disks are dropped;
// internal removable media (built-in SD slots) are kept.
func parseDarwinDevices(list, all []byte, infos map[string][]byte, root []byte) ([]Device, error) {
	var l diskutilList
	if err := decodePlist(list, &l); err != nil {
		return nil, fmt.Errorf("parse diskutil list: %w", err)
	}
	var everything diskutilList
	if err := decodePlist(all, &everything); err != nil {
		return nil, fmt.Errorf("parse diskutil list: %w", err)
	}
	containers := map[string][]apfsContainer{} // physical whole disk -> containers it backs
	for _, e := range everything.AllDisksAndPartitions {
		if len(e.APFSPhysicalStores) == 0 {
			continue
		}
		c := apfsContainer{id: e.DeviceIdentifier}
		for _, v := range e.APFSVolumes {
			if v.VolumeName != "" && !apfsRoleVolumes[v.VolumeName] {
				c.labels = append(c.labels, v.VolumeName)
			}
			if v.MountPoint != "" {
				c.mounts = append(c.mounts, v.MountPoint)
			}
		}
		for _, store := range e.APFSPhysicalStores {
			if disk := wholeDisk(store.DeviceIdentifier); disk != "" {
				containers[disk] = append(containers[disk], c)
			}
		}
	}
	var rootInfo diskutilInfo
	if err := decodePlist(root, &rootInfo); err != nil {
		return nil, fmt.Errorf("parse diskutil info /: %w", err)
	}
	system := map[string]bool{}
	if d := wholeDisk(rootInfo.ParentWholeDisk); d != "" {
		system[d] = true
	}
	if d := wholeDisk(rootInfo.DeviceIdentifier); d != "" {
		system[d] = true
	}
	for _, s := range rootInfo.APFSPhysicalStores {
		if d := wholeDisk(s.APFSPhysicalStore); d != "" {
			system[d] = true
		}
	}

	var devices []Device
	for _, disk := range l.AllDisksAndPartitions {
		raw, ok := infos[disk.DeviceIdentifier]
		if !ok {
			continue
		}
		var info diskutilInfo
		if err := decodePlist(raw, &info); err != nil {
			return nil, fmt.Errorf("parse diskutil info %s: %w", disk.DeviceIdentifier, err)
		}
		if info.Internal && !info.RemovableMedia {
			continue
		}

		id := disk.DeviceIdentifier
		d := Device{
			ID:        id,
			Path:      "/dev/" + id,
			RawPath:   "/dev/r" + id,
			Model:     strings.TrimSpace(info.MediaName),
			Size:      firstPositive(info.TotalSize, info.Size, disk.Size),
			Bus:       info.BusProtocol,
			Removable: info.RemovableMedia,
			System:    system[id],
		}
		if disk.VolumeName != "" {
			d.Labels = append(d.Labels, disk.VolumeName)
		}
		if disk.MountPoint != "" {
			d.Mounts = append(d.Mounts, disk.MountPoint)
		}
		for _, p := range disk.Partitions {
			if p.VolumeName != "" && p.Content != "EFI" {
				d.Labels = append(d.Labels, p.VolumeName)
			}
			if p.MountPoint != "" {
				d.Mounts = append(d.Mounts, p.MountPoint)
			}
		}
		for _, c := range containers[id] {
			d.Containers = append(d.Containers, c.id)
			d.Labels = append(d.Labels, c.labels...)
			d.Mounts = append(d.Mounts, c.mounts...)
			for _, m := range c.mounts {
				if m == "/" {
					d.System = true
				}
			}
		}
		devices = append(devices, d)
	}
	return devices, nil
}

var wholeDiskRE = regexp.MustCompile(`^disk\d+`)

// wholeDisk maps a partition or volume identifier ("disk0s2") to its whole
// disk ("disk0").
func wholeDisk(id string) string { return wholeDiskRE.FindString(id) }

func firstPositive(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}
