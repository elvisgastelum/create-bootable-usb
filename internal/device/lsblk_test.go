package device

import (
	"reflect"
	"testing"
)

// Newer util-linux prints booleans and numbers as JSON types.
const lsblkNew = `{
   "blockdevices": [
      {"name":"loop0", "size":4096, "model":null, "vendor":null, "tran":null, "rm":false, "hotplug":false, "type":"loop", "mountpoint":"/snap/core", "label":null},
      {"name":"nvme0n1", "size":512110190592, "model":"Samsung SSD 980", "vendor":null, "tran":"nvme", "rm":false, "hotplug":false, "type":"disk", "mountpoint":null, "label":null,
         "children": [
            {"name":"nvme0n1p1", "size":536870912, "model":null, "vendor":null, "tran":"nvme", "rm":false, "hotplug":false, "type":"part", "mountpoint":"/boot/efi", "label":null},
            {"name":"nvme0n1p2", "size":511571165184, "model":null, "vendor":null, "tran":"nvme", "rm":false, "hotplug":false, "type":"part", "mountpoint":"/", "label":null}
         ]
      },
      {"name":"sdb", "size":16043212800, "model":"Ultra Fit       ", "vendor":"SanDisk ", "tran":"usb", "rm":true, "hotplug":true, "type":"disk", "mountpoint":null, "label":null,
         "children": [
            {"name":"sdb1", "size":16042164224, "model":null, "vendor":null, "tran":"usb", "rm":true, "hotplug":true, "type":"part", "mountpoint":"/media/me/USB", "label":"USB"}
         ]
      },
      {"name":"sdc", "size":64023257088, "model":"Live Stick", "vendor":"Kingston", "tran":"usb", "rm":true, "hotplug":true, "type":"disk", "mountpoint":null, "label":null,
         "children": [
            {"name":"sdc1", "size":64022208512, "model":null, "vendor":null, "tran":"usb", "rm":true, "hotplug":true, "type":"part", "mountpoint":"/run/live/medium", "label":"LIVE"}
         ]
      },
      {"name":"mmcblk0", "size":31914983424, "model":null, "vendor":null, "tran":null, "rm":false, "hotplug":false, "type":"disk", "mountpoint":null, "label":null},
      {"name":"mmcblk1", "size":62537072640, "model":null, "vendor":null, "tran":null, "rm":false, "hotplug":false, "type":"disk", "mountpoint":null, "label":null},
      {"name":"mmcblk1boot0", "size":4194304, "model":null, "vendor":null, "tran":null, "rm":false, "hotplug":false, "type":"disk", "mountpoint":null, "label":null}
   ]
}`

// util-linux before 2.33 prints every column as a string.
const lsblkOld = `{
   "blockdevices": [
      {"name": "sdb", "size": "16043212800", "model": "Ultra Fit", "vendor": "SanDisk", "tran": "usb", "rm": "1", "hotplug": "1", "type": "disk", "mountpoint": null, "label": null,
         "children": [
            {"name": "sdb1", "size": "16042164224", "model": null, "vendor": null, "tran": null, "rm": "1", "hotplug": "1", "type": "part", "mountpoint": null, "label": "USB"}
         ]
      },
      {"name": "sda", "size": "256060514304", "model": "WDC WD2500", "vendor": "ATA", "tran": "sata", "rm": "0", "hotplug": "0", "type": "disk", "mountpoint": null, "label": null}
   ]
}`

// fakeMMCType answers like /sys/block/<name>/device/type: mmcblk0 is an SD
// card, mmcblk1 is soldered-on eMMC storage.
func fakeMMCType(name string) string {
	return map[string]string{"mmcblk0": "SD", "mmcblk1": "MMC"}[name]
}

func TestParseLsblkNewFormat(t *testing.T) {
	devices, err := parseLsblk([]byte(lsblkNew), fakeMMCType)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Device{}
	for _, d := range devices {
		byID[d.ID] = d
	}
	for _, hidden := range []string{"loop0", "nvme0n1", "mmcblk1", "mmcblk1boot0"} {
		if _, ok := byID[hidden]; ok {
			t.Errorf("%s is internal storage and must not be listed", hidden)
		}
	}

	want := Device{
		ID:        "sdb",
		Path:      "/dev/sdb",
		RawPath:   "/dev/sdb",
		Model:     "SanDisk Ultra Fit",
		Size:      16_043_212_800,
		Bus:       "USB",
		Removable: true,
		Labels:    []string{"USB"},
		Mounts:    []string{"/media/me/USB"},
	}
	if got := byID["sdb"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("sdb =\n%+v\nwant\n%+v", got, want)
	}
	if !byID["sdc"].System {
		t.Error("sdc runs the live system (/run/live/medium) and must be marked as a system disk")
	}
	sd, ok := byID["mmcblk0"]
	if !ok || sd.Bus != "SD" {
		t.Errorf("mmcblk0 must be listed as an SD card, got %+v (listed=%v)", sd, ok)
	}
}

func TestParseLsblkOldFormat(t *testing.T) {
	devices, err := parseLsblk([]byte(lsblkOld), fakeMMCType)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("got %d devices, want only the USB stick: %+v", len(devices), devices)
	}
	d := devices[0]
	if d.ID != "sdb" || d.Size != 16_043_212_800 || !d.Removable || d.Bus != "USB" {
		t.Fatalf("unexpected device %+v", d)
	}
	if !reflect.DeepEqual(d.Labels, []string{"USB"}) || len(d.Mounts) != 0 {
		t.Fatalf("labels=%v mounts=%v, want [USB] and none", d.Labels, d.Mounts)
	}
}

func TestParseLsblkRejectsGarbage(t *testing.T) {
	if _, err := parseLsblk([]byte("lsblk: unknown column"), fakeMMCType); err == nil {
		t.Fatal("want an error for non-JSON output")
	}
}
