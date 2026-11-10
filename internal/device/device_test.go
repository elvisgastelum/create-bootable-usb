package device

import (
	"strings"
	"testing"
)

func TestProblem(t *testing.T) {
	const gb = int64(1_000_000_000)
	stick := Device{ID: "disk6", Size: 16 * gb, Bus: "USB", Removable: true}

	tests := []struct {
		name      string
		device    Device
		imageSize int64
		want      string // substring of the problem; "" means eligible
	}{
		{name: "usb stick that fits is eligible", device: stick, imageSize: 2 * gb, want: ""},
		{name: "unknown image size skips the size check", device: stick, imageSize: 0, want: ""},
		{name: "fixed usb ssd is eligible", device: Device{Size: 500 * gb, Bus: "USB"}, imageSize: gb, want: ""},
		{name: "removable sd card is eligible", device: Device{Size: 32 * gb, Bus: "Secure Digital", Removable: true}, imageSize: gb, want: ""},
		{name: "system disk is never eligible", device: Device{Size: 64 * gb, Bus: "USB", Removable: true, System: true}, imageSize: gb, want: "operating system"},
		{name: "empty card reader", device: Device{Size: 0, Bus: "USB", Removable: true}, imageSize: gb, want: "no media"},
		{name: "fixed thunderbolt nvme is not offered", device: Device{Size: 2000 * gb, Bus: "PCI-Express"}, imageSize: gb, want: "not a removable or USB device"},
		{name: "too small for the image", device: Device{Size: gb, Bus: "USB", Removable: true}, imageSize: 2 * gb, want: "too small"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.device.Problem(tt.imageSize)
			if tt.want == "" && got != "" {
				t.Fatalf("Problem() = %q, want eligible", got)
			}
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("Problem() = %q, want it to mention %q", got, tt.want)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	d := Device{Model: "ProductCode", Size: 16_043_212_800, Bus: "USB", Labels: []string{"USB"}}
	got := d.Summary()
	for _, want := range []string{"ProductCode", "16.0 GB", "USB", `"USB"`} {
		if !strings.Contains(got, want) {
			t.Errorf("Summary() = %q, missing %q", got, want)
		}
	}
	if got := (Device{}).Name(); got != "Unknown device" {
		t.Errorf("Name() of a device without a model = %q, want %q", got, "Unknown device")
	}
}
