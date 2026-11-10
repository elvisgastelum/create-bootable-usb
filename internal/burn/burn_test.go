package burn

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elvisgastelum/create-bootable-usb/internal/device"
)

// fileTarget is a fake raw device backed by a regular file.
type fileTarget struct {
	*os.File
	corruptAt int64 // flip the byte at this device offset whenever it is written; -1 disables
}

func (f *fileTarget) Write(p []byte) (int, error) {
	offset, err := f.File.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if f.corruptAt >= offset && f.corruptAt < offset+int64(len(p)) {
		p = append([]byte(nil), p...)
		p[f.corruptAt-offset] ^= 0xFF
	}
	return f.File.Write(p)
}

func (f *fileTarget) Flush() error { return f.Sync() }

type fakeSystem struct {
	devices   []device.Device
	devPath   string
	corruptAt int64
	finishErr error
	prepared  []device.Device
	finished  []device.Device
}

func (s *fakeSystem) List() ([]device.Device, error) { return s.devices, nil }
func (s *fakeSystem) Prepare(d device.Device) error {
	s.prepared = append(s.prepared, d)
	return nil
}
func (s *fakeSystem) Open(device.Device) (device.Handle, error) {
	f, err := os.OpenFile(s.devPath, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &fileTarget{File: f, corruptAt: s.corruptAt}, nil
}
func (s *fakeSystem) Finish(d device.Device) error {
	s.finished = append(s.finished, d)
	return s.finishErr
}

const deviceSize = 16 << 20

func setup(t *testing.T) (image string, img []byte, sys *fakeSystem, stick device.Device) {
	t.Helper()
	dir := t.TempDir()
	img = make([]byte, 5<<20+7)
	rand.New(rand.NewSource(7)).Read(img)
	image = filepath.Join(dir, "proxmox.iso")
	if err := os.WriteFile(image, img, 0o644); err != nil {
		t.Fatal(err)
	}
	devPath := filepath.Join(dir, "disk6")
	if err := os.WriteFile(devPath, bytes.Repeat([]byte{0xEE}, deviceSize), 0o644); err != nil {
		t.Fatal(err)
	}
	stick = device.Device{ID: "disk6", Path: "/dev/disk6", RawPath: "/dev/rdisk6", Model: "ProductCode",
		Size: deviceSize, Bus: "USB", Removable: true, Labels: []string{"USB"}, Mounts: []string{filepath.Join(dir, "Volumes", "USB")}}
	sys = &fakeSystem{devices: []device.Device{stick}, devPath: devPath, corruptAt: -1}
	return image, img, sys, stick
}

func deviceContent(t *testing.T, sys *fakeSystem) []byte {
	t.Helper()
	data, err := os.ReadFile(sys.devPath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRunWritesVerifiesAndEjects(t *testing.T) {
	image, img, sys, stick := setup(t)
	var stages []Stage
	res, err := Run(context.Background(), sys, Job{Image: image, Device: stick, Verify: true}, func(p Progress) {
		if len(stages) == 0 || stages[len(stages)-1] != p.Stage {
			stages = append(stages, p.Stage)
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := deviceContent(t, sys); !bytes.Equal(got[:len(img)], img) {
		t.Fatal("device does not start with the image")
	}
	sum := sha256.Sum256(img)
	if res.SHA256 != hex.EncodeToString(sum[:]) || !res.Verified || res.Warning != "" {
		t.Fatalf("result = %+v", res)
	}
	want := []Stage{StagePreparing, StageWriting, StageFlushing, StageVerifying, StageFinishing}
	if len(stages) != len(want) {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
	for i := range want {
		if stages[i] != want[i] {
			t.Fatalf("stages = %v, want %v", stages, want)
		}
	}
	if len(sys.prepared) != 1 || len(sys.finished) != 1 {
		t.Fatalf("prepare called %d times, finish %d times; want 1 each", len(sys.prepared), len(sys.finished))
	}
}

func TestRunWithoutVerification(t *testing.T) {
	image, _, sys, stick := setup(t)
	res, err := Run(context.Background(), sys, Job{Image: image, Device: stick, Verify: false}, func(p Progress) {
		if p.Stage == StageVerifying {
			t.Fatal("verification must be skipped")
		}
	})
	if err != nil || res.Verified {
		t.Fatalf("Run() = %+v, %v; want success without verification", res, err)
	}
}

func TestRunRefusesUnsafeTargets(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(sys *fakeSystem, job *Job)
		wantErr string
	}{
		{
			name:    "device unplugged since it was selected",
			mutate:  func(sys *fakeSystem, _ *Job) { sys.devices = nil },
			wantErr: "no longer connected",
		},
		{
			name: "a different disk now answers at the same path",
			mutate: func(sys *fakeSystem, _ *Job) {
				sys.devices = []device.Device{{ID: "disk6", Path: "/dev/disk6", Size: 2 * deviceSize, Bus: "USB", Removable: true}}
			},
			wantErr: "changed",
		},
		{
			name: "device became the boot disk",
			mutate: func(sys *fakeSystem, _ *Job) {
				d := sys.devices[0]
				d.System = true
				sys.devices = []device.Device{d}
			},
			wantErr: "operating system",
		},
		{
			name: "image is stored on the device being erased",
			mutate: func(sys *fakeSystem, job *Job) {
				job.Image = filepath.Join(sys.devices[0].Mounts[0], "proxmox.iso")
			},
			wantErr: "stored on",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			image, _, sys, stick := setup(t)
			before := deviceContent(t, sys)
			job := Job{Image: image, Device: stick, Verify: true}
			tt.mutate(sys, &job)

			_, err := Run(context.Background(), sys, job, func(Progress) {})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
			}
			if len(sys.prepared) != 0 {
				t.Fatal("the device must not be touched")
			}
			if !bytes.Equal(deviceContent(t, sys), before) {
				t.Fatal("device content changed")
			}
		})
	}
}

func TestRunReportsVerificationFailure(t *testing.T) {
	image, _, sys, stick := setup(t)
	sys.corruptAt = 3 << 20
	_, err := Run(context.Background(), sys, Job{Image: image, Device: stick, Verify: true}, func(Progress) {})
	if err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("err = %v, want a verification failure", err)
	}
	if len(sys.finished) != 0 {
		t.Fatal("a failed write must not be reported as finished (ejected)")
	}
}

func TestRunTreatsEjectFailureAsWarning(t *testing.T) {
	image, _, sys, stick := setup(t)
	sys.finishErr = errors.New("eject failed: disk busy")
	res, err := Run(context.Background(), sys, Job{Image: image, Device: stick, Verify: true}, func(Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Warning, "disk busy") {
		t.Fatalf("warning = %q, want the eject error", res.Warning)
	}
}

func TestRunHonoursCancellationBeforeTouchingTheDevice(t *testing.T) {
	image, _, sys, stick := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, sys, Job{Image: image, Device: stick, Verify: true}, func(Progress) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(sys.prepared) != 0 {
		t.Fatal("a cancelled job must not prepare the device")
	}
}

func TestImageOnDevice(t *testing.T) {
	d := device.Device{Mounts: []string{"/Volumes/USB", "E:"}}
	tests := map[string]bool{
		"/Volumes/USB/proxmox.iso":     true,
		"/Volumes/USB":                 true,
		"/Volumes/USB2/proxmox.iso":    false,
		"/Users/dev/Downloads/pve.iso": false,
		`E:\isos\proxmox.iso`:          true,
		`e:\proxmox.iso`:               true,
		`C:\Users\dev\Downloads\a.iso`: false,
	}
	for path, want := range tests {
		if got := ImageOnDevice(path, d); got != want {
			t.Errorf("ImageOnDevice(%q) = %v, want %v", path, got, want)
		}
	}
}
