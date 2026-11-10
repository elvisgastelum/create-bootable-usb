package format

import (
	"testing"
	"time"
)

func TestBytes(t *testing.T) {
	tests := map[int64]string{
		0:                 "0 B",
		999:               "999 B",
		1_000:             "1.0 KB",
		650_000_000:       "650.0 MB",
		1_706_178_560:     "1.7 GB",
		16_043_212_800:    "16.0 GB",
		2_000_398_934_016: "2.0 TB",
	}
	for in, want := range tests {
		if got := Bytes(in); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestRateAndDuration(t *testing.T) {
	if got := Rate(24_300_000); got != "24.3 MB/s" {
		t.Errorf("Rate = %q", got)
	}
	if got := Rate(0); got != "—" {
		t.Errorf("Rate(0) = %q", got)
	}
	if got := Duration(72*time.Second + 400*time.Millisecond); got != "1m12s" {
		t.Errorf("Duration = %q", got)
	}
	if got := Duration(200 * time.Millisecond); got != "0s" {
		t.Errorf("Duration(<1s) = %q", got)
	}
}
