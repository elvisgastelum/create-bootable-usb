// Package burn runs one image-to-device write from start to finish:
// re-checking the device, unmounting it, writing, flushing, verifying and
// ejecting.
package burn

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elvisgastelum/create-bootable-usb/internal/device"
	"github.com/elvisgastelum/create-bootable-usb/internal/writer"
)

// System is the operating system boundary; device.Host is the real one.
type System interface {
	List() ([]device.Device, error)
	Prepare(device.Device) error
	Open(device.Device) (device.Handle, error)
	Finish(device.Device) error
}

// Job is what the user chose.
type Job struct {
	Image  string
	Device device.Device
	Verify bool
}

// Stage is a step of a write.
type Stage int

const (
	StagePreparing Stage = iota // re-check and unmount the device
	StageWriting                // copy the image
	StageFlushing               // wait until the data reached the device
	StageVerifying              // read it back and compare
	StageFinishing              // eject
)

func (s Stage) String() string {
	return [...]string{"Preparing", "Writing", "Flushing", "Verifying", "Ejecting"}[s]
}

// Progress is reported while a job runs. Done and Total are in bytes and
// are only set for the writing and verifying stages.
type Progress struct {
	Stage       Stage
	Done, Total int64
}

// Result describes a successful write.
type Result struct {
	SHA256   string        // of the image bytes that were written
	Verified bool          // the device was read back and matched
	Duration time.Duration // whole job
	Warning  string        // non-fatal problem, e.g. the disk could not be ejected
}

// Run writes job.Image to job.Device. Before touching the device it lists
// devices again and refuses to continue unless the same, still eligible
// disk is present and the image is not stored on it.
func Run(ctx context.Context, sys System, job Job, report func(Progress)) (Result, error) {
	start := time.Now()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	report(Progress{Stage: StagePreparing})

	target, err := refresh(sys, job.Device)
	if err != nil {
		return Result{}, err
	}
	image, err := filepath.Abs(job.Image)
	if err != nil {
		return Result{}, err
	}
	resolved, err := filepath.EvalSymlinks(image)
	if err != nil {
		resolved = image
	}
	if ImageOnDevice(image, target) || ImageOnDevice(resolved, target) {
		return Result{}, fmt.Errorf("the image %s is stored on %s, the device you are about to erase; copy it to another disk first", image, target.Path)
	}

	img, err := os.Open(image)
	if err != nil {
		return Result{}, fmt.Errorf("open the image: %w", err)
	}
	defer img.Close()
	st, err := img.Stat()
	if err != nil {
		return Result{}, err
	}
	size := st.Size()
	if size <= 0 {
		return Result{}, fmt.Errorf("the image %s is empty", image)
	}
	if problem := target.Problem(size); problem != "" {
		return Result{}, fmt.Errorf("refusing to write to %s: it %s", target.Path, problem)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	if err := sys.Prepare(target); err != nil {
		return Result{}, err
	}
	h, err := sys.Open(target)
	if err != nil {
		return Result{}, err
	}
	closed := false
	defer func() {
		if !closed {
			h.Close()
		}
	}()

	report(Progress{Stage: StageWriting, Total: size})
	sum, err := writer.Write(ctx, h, img, size, target.Size, func(done int64) {
		report(Progress{Stage: StageWriting, Done: done, Total: size})
	})
	if err != nil {
		return Result{}, fmt.Errorf("write failed: %w", err)
	}

	report(Progress{Stage: StageFlushing})
	if err := h.Flush(); err != nil {
		return Result{}, fmt.Errorf("flush the device: %w", err)
	}

	res := Result{SHA256: sum}
	if job.Verify {
		report(Progress{Stage: StageVerifying, Total: size})
		if _, err := h.Seek(0, io.SeekStart); err != nil {
			return Result{}, fmt.Errorf("verification failed: %w", err)
		}
		if _, err := img.Seek(0, io.SeekStart); err != nil {
			return Result{}, fmt.Errorf("verification failed: %w", err)
		}
		err := writer.Verify(ctx, h, img, size, target.Size, func(done int64) {
			report(Progress{Stage: StageVerifying, Done: done, Total: size})
		})
		if err != nil {
			return Result{}, fmt.Errorf("verification failed: %w", err)
		}
		res.Verified = true
	}

	closed = true
	if err := h.Close(); err != nil {
		return Result{}, fmt.Errorf("close the device: %w", err)
	}
	report(Progress{Stage: StageFinishing})
	if err := sys.Finish(target); err != nil {
		res.Warning = err.Error()
	}
	res.Duration = time.Since(start)
	return res, nil
}

// refresh finds the selected device in a fresh listing so a disk that was
// unplugged, replaced or renumbered since selection is never written.
func refresh(sys System, selected device.Device) (device.Device, error) {
	devices, err := sys.List()
	if err != nil {
		return device.Device{}, fmt.Errorf("check %s again: %w", selected.Path, err)
	}
	for _, d := range devices {
		if d.Path != selected.Path {
			continue
		}
		if d.Size != selected.Size || d.Model != selected.Model {
			return device.Device{}, fmt.Errorf("%s changed since you selected it (now: %s); select the device again", selected.Path, d.Summary())
		}
		return d, nil
	}
	return device.Device{}, fmt.Errorf("%s (%s) is no longer connected", selected.Path, selected.Name())
}

// ImageOnDevice reports whether path lies on one of d's mounted volumes.
// Comparison ignores case and accepts both slash styles, so it errs toward
// refusing.
func ImageOnDevice(path string, d device.Device) bool {
	p := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	for _, m := range d.Mounts {
		m = strings.ToLower(strings.TrimRight(strings.ReplaceAll(m, `\`, "/"), "/"))
		if m == "" || p == m || strings.HasPrefix(p, m+"/") {
			return true
		}
	}
	return false
}
