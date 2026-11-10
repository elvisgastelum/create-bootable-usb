package device

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func darwinFixtures(t *testing.T) (list, all []byte, infos map[string][]byte) {
	t.Helper()
	return readFixture(t, "diskutil_list_physical.plist"), readFixture(t, "diskutil_list_all.plist"), map[string][]byte{
		"disk0": readFixture(t, "diskutil_info_disk0.plist"),
		"disk4": readFixture(t, "diskutil_info_disk4.plist"),
		"disk6": readFixture(t, "diskutil_info_disk6.plist"),
	}
}

func TestParseDarwinDevices(t *testing.T) {
	list, all, infos := darwinFixtures(t)
	devices, err := parseDarwinDevices(list, all, infos, readFixture(t, "diskutil_info_root.plist"))
	if err != nil {
		t.Fatal(err)
	}

	byID := map[string]Device{}
	for _, d := range devices {
		byID[d.ID] = d
	}
	if _, ok := byID["disk0"]; ok {
		t.Fatalf("internal disk0 must not be listed, got %+v", devices)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2 (disk4, disk6): %+v", len(devices), devices)
	}

	stick := byID["disk6"]
	want := Device{
		ID:        "disk6",
		Path:      "/dev/disk6",
		RawPath:   "/dev/rdisk6",
		Model:     "ProductCode",
		Size:      16_043_212_800,
		Bus:       "USB",
		Removable: true,
		Labels:    []string{"USB"},
		Mounts:    []string{"/Volumes/USB"},
	}
	if !reflect.DeepEqual(stick, want) {
		t.Fatalf("disk6 =\n%+v\nwant\n%+v", stick, want)
	}
	if p := stick.Problem(1_706_178_560); p != "" {
		t.Fatalf("the USB stick must be eligible for the Proxmox ISO, got %q", p)
	}

	nvme := byID["disk4"]
	if !reflect.DeepEqual(nvme.Labels, []string{"ExternalSSD"}) {
		t.Errorf("disk4 labels = %v, want [ExternalSSD] (EFI partition hidden)", nvme.Labels)
	}
	if p := nvme.Problem(1_706_178_560); p == "" {
		t.Fatal("the external Thunderbolt NVMe disk must not be eligible by default")
	}
}

func TestParseDarwinDevicesMarksBootDisk(t *testing.T) {
	list, all, infos := darwinFixtures(t)
	// A Mac booted from the external stick: the APFS container holding "/"
	// lives on disk6s2.
	root := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>ParentWholeDisk</key><string>disk3</string>
<key>APFSPhysicalStores</key><array><dict><key>APFSPhysicalStore</key><string>disk6s2</string></dict></array>
</dict></plist>`)
	devices, err := parseDarwinDevices(list, all, infos, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devices {
		if d.ID == "disk6" && !d.System {
			t.Fatal("disk6 holds the boot volume and must be marked as a system disk")
		}
		if d.ID == "disk4" && d.System {
			t.Fatal("disk4 does not hold the boot volume")
		}
	}
}

// An APFS-formatted USB SSD: its volumes live on the synthesized container
// disk9, which `diskutil list -plist physical` leaves out.
const (
	apfsPhysicalList = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>WholeDisks</key><array><string>disk8</string></array>
<key>AllDisksAndPartitions</key><array><dict>
  <key>DeviceIdentifier</key><string>disk8</string>
  <key>Size</key><integer>500107862016</integer>
  <key>Partitions</key><array>
    <dict><key>DeviceIdentifier</key><string>disk8s1</string><key>Content</key><string>EFI</string><key>VolumeName</key><string>EFI</string></dict>
    <dict><key>DeviceIdentifier</key><string>disk8s2</string><key>Content</key><string>Apple_APFS</string></dict>
  </array>
</dict></array></dict></plist>`
	apfsAllList = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>WholeDisks</key><array><string>disk8</string><string>disk9</string></array>
<key>AllDisksAndPartitions</key><array>
  <dict><key>DeviceIdentifier</key><string>disk8</string><key>Size</key><integer>500107862016</integer></dict>
  <dict>
    <key>DeviceIdentifier</key><string>disk9</string>
    <key>APFSPhysicalStores</key><array><dict><key>DeviceIdentifier</key><string>disk8s2</string></dict></array>
    <key>APFSVolumes</key><array>
      <dict><key>DeviceIdentifier</key><string>disk9s1</string><key>VolumeName</key><string>Backup</string><key>MountPoint</key><string>MOUNT</string></dict>
      <dict><key>DeviceIdentifier</key><string>disk9s2</string><key>VolumeName</key><string>Preboot</string></dict>
    </array>
  </dict>
</array></dict></plist>`
	apfsInfo = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>DeviceIdentifier</key><string>disk8</string>
<key>MediaName</key><string>Samsung PSSD T7</string>
<key>TotalSize</key><integer>500107862016</integer>
<key>BusProtocol</key><string>USB</string>
<key>RemovableMedia</key><false/>
<key>Internal</key><false/>
</dict></plist>`
)

func TestParseDarwinDevicesAttachesAPFSVolumes(t *testing.T) {
	parse := func(mount string) Device {
		t.Helper()
		all := []byte(strings.Replace(apfsAllList, "MOUNT", mount, 1))
		devices, err := parseDarwinDevices([]byte(apfsPhysicalList), all,
			map[string][]byte{"disk8": []byte(apfsInfo)}, readFixture(t, "diskutil_info_root.plist"))
		if err != nil {
			t.Fatal(err)
		}
		if len(devices) != 1 {
			t.Fatalf("got %+v, want only disk8", devices)
		}
		return devices[0]
	}

	d := parse("/Volumes/Backup")
	if !reflect.DeepEqual(d.Labels, []string{"Backup"}) {
		t.Errorf("labels = %v, want [Backup] from the APFS container (EFI and Preboot hidden)", d.Labels)
	}
	if !reflect.DeepEqual(d.Mounts, []string{"/Volumes/Backup"}) {
		t.Errorf("mounts = %v, want the APFS volume mount point", d.Mounts)
	}
	if !reflect.DeepEqual(d.Containers, []string{"disk9"}) {
		t.Errorf("containers = %v, want [disk9] so it is unmounted before writing", d.Containers)
	}
	if d.System || d.Problem(1_706_178_560) != "" {
		t.Errorf("an APFS USB SSD must be a valid target, got system=%v problem=%q", d.System, d.Problem(1_706_178_560))
	}

	if !parse("/").System {
		t.Error("a disk whose APFS volume is mounted at / must be marked as a system disk")
	}
}

func TestWholeDisk(t *testing.T) {
	for in, want := range map[string]string{"disk0s2": "disk0", "disk12": "disk12", "disk3s1s1": "disk3", "": ""} {
		if got := wholeDisk(in); got != want {
			t.Errorf("wholeDisk(%q) = %q, want %q", in, got, want)
		}
	}
}
