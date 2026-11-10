package device

import (
	"encoding/base64"
	"reflect"
	"testing"
	"unicode/utf16"
)

const windowsDisks = `[
  {"Number":0,"FriendlyName":"Samsung SSD 970","Size":1000204886016,"BusType":"NVMe","IsBoot":true,"IsSystem":true,"Labels":["Windows"],"Letters":["C"]},
  {"Number":2,"FriendlyName":"SanDisk Ultra","Size":16043212800,"BusType":"USB","IsBoot":false,"IsSystem":false,"Labels":["USB"],"Letters":["E"]},
  {"Number":3,"FriendlyName":"Generic SD Reader","Size":31914983424,"BusType":"SD","IsBoot":false,"IsSystem":false,"Labels":"BOOT","Letters":null},
  {"Number":4,"FriendlyName":"Windows To Go","Size":64023257088,"BusType":"USB","IsBoot":true,"IsSystem":false,"Labels":[],"Letters":[]}
]`

func TestParseWindowsDisks(t *testing.T) {
	devices, err := parseWindowsDisks([]byte(windowsDisks))
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 3 {
		t.Fatalf("got %d devices, want 3 (the NVMe system disk is not removable): %+v", len(devices), devices)
	}

	want := Device{
		ID:      "PhysicalDrive2",
		Path:    `\\.\PhysicalDrive2`,
		RawPath: `\\.\PhysicalDrive2`,
		Number:  2,
		Model:   "SanDisk Ultra",
		Size:    16_043_212_800,
		Bus:     "USB",
		Labels:  []string{"USB"},
		Mounts:  []string{"E:"},
	}
	if !reflect.DeepEqual(devices[0], want) {
		t.Fatalf("disk 2 =\n%+v\nwant\n%+v", devices[0], want)
	}
	if !reflect.DeepEqual(devices[1].Labels, []string{"BOOT"}) {
		t.Errorf("a single label serialized as a string must still be read, got %v", devices[1].Labels)
	}
	if !devices[2].System {
		t.Error("a USB disk Windows booted from must be marked as a system disk")
	}
}

func TestParseWindowsDisksEmpty(t *testing.T) {
	for _, in := range []string{"", "  \r\n", "[]", "null"} {
		devices, err := parseWindowsDisks([]byte(in))
		if err != nil || len(devices) != 0 {
			t.Errorf("parseWindowsDisks(%q) = %v, %v; want no devices and no error", in, devices, err)
		}
	}
}

func TestEncodePowerShell(t *testing.T) {
	script := "Get-Disk | ConvertTo-Json # ñ"
	raw, err := base64.StdEncoding.DecodeString(encodePowerShell(script))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("encoded command must be UTF-16LE, got %d bytes", len(raw))
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	if got := string(utf16.Decode(units)); got != script {
		t.Fatalf("round trip = %q, want %q", got, script)
	}
}
