// Package writer copies an image to a raw device and verifies it, the same
// job as `dd bs=4m` followed by a byte-for-byte comparison.
package writer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

const (
	// ChunkSize is how much is read and written per system call.
	ChunkSize = 4 << 20
	// Alignment is the transfer granularity. Raw devices (macOS /dev/rdisk,
	// Windows \\.\PhysicalDrive) only accept whole sectors; 4096 is a
	// multiple of both 512-byte and 4 KiB sectors.
	Alignment = 4096
)

// MismatchError reports the first byte where the device differs from the image.
type MismatchError struct{ Offset int64 }

func (e *MismatchError) Error() string {
	return fmt.Sprintf("the device differs from the image at byte %d; the USB device may be faulty", e.Offset)
}

// Write copies the first size bytes of src to dst and returns their SHA-256.
// The final chunk is zero-padded to Alignment, but never past deviceSize.
// progress receives the number of image bytes written after every chunk.
func Write(ctx context.Context, dst io.Writer, src io.Reader, size, deviceSize int64, progress func(done int64)) (string, error) {
	buf := make([]byte, ChunkSize)
	hash := sha256.New()
	var done int64
	for done < size {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n := int(min(int64(ChunkSize), size-done))
		if _, err := io.ReadFull(src, buf[:n]); err != nil {
			return "", fmt.Errorf("read the image at byte %d: %w", done, err)
		}
		hash.Write(buf[:n])

		out := buf[:paddedLen(n, done, deviceSize)]
		clear(out[n:])
		if _, err := dst.Write(out); err != nil {
			return "", fmt.Errorf("write to the device at byte %d: %w", done, err)
		}
		done += int64(n)
		progress(done)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Verify reads the first size bytes back from dev and compares them with
// src, returning a *MismatchError at the first difference.
func Verify(ctx context.Context, dev io.Reader, src io.Reader, size, deviceSize int64, progress func(done int64)) error {
	want := make([]byte, ChunkSize)
	got := make([]byte, ChunkSize)
	var done int64
	for done < size {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := int(min(int64(ChunkSize), size-done))
		if _, err := io.ReadFull(src, want[:n]); err != nil {
			return fmt.Errorf("read the image at byte %d: %w", done, err)
		}
		if _, err := io.ReadFull(dev, got[:paddedLen(n, done, deviceSize)]); err != nil {
			return fmt.Errorf("read back from the device at byte %d: %w", done, err)
		}
		if !bytes.Equal(got[:n], want[:n]) {
			i := 0
			for got[i] == want[i] {
				i++
			}
			return &MismatchError{Offset: done + int64(i)}
		}
		done += int64(n)
		progress(done)
	}
	return nil
}

// paddedLen rounds a chunk of n bytes at offset up to Alignment without
// running past the end of the device.
func paddedLen(n int, offset, deviceSize int64) int {
	p := (n + Alignment - 1) / Alignment * Alignment
	if deviceSize > 0 && offset+int64(p) > deviceSize {
		p = max(n, int(deviceSize-offset))
	}
	return p
}
