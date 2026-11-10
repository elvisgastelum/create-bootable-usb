package writer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math/rand"
	"testing"
)

func randomImage(t *testing.T, size int) []byte {
	t.Helper()
	img := make([]byte, size)
	rand.New(rand.NewSource(42)).Read(img)
	return img
}

type writeRecord struct {
	offset int64
	data   []byte
}

// fakeDevice is an in-memory raw device that records every write.
type fakeDevice struct {
	data   []byte
	pos    int64
	writes []writeRecord
}

func (d *fakeDevice) Write(p []byte) (int, error) {
	end := d.pos + int64(len(p))
	if end > int64(len(d.data)) {
		d.data = append(d.data, make([]byte, end-int64(len(d.data)))...)
	}
	copy(d.data[d.pos:], p)
	d.writes = append(d.writes, writeRecord{offset: d.pos, data: append([]byte(nil), p...)})
	d.pos = end
	return len(p), nil
}

func (d *fakeDevice) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		d.pos = offset
	case io.SeekCurrent:
		d.pos += offset
	default:
		return 0, errors.New("unsupported whence")
	}
	return d.pos, nil
}

func TestWriteCopiesImageInAlignedChunks(t *testing.T) {
	img := randomImage(t, 2*ChunkSize+12345) // final chunk is not sector aligned
	dev := &fakeDevice{}
	var reported []int64

	sum, err := Write(context.Background(), dev, bytes.NewReader(img), int64(len(img)), 1<<30,
		func(done int64) { reported = append(reported, done) })
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(dev.data[:len(img)], img) {
		t.Fatal("device content differs from the image")
	}
	if len(dev.data)%Alignment != 0 {
		t.Fatalf("wrote %d bytes; raw devices need whole %d-byte sectors", len(dev.data), Alignment)
	}
	if tail := dev.data[len(img):]; !bytes.Equal(tail, make([]byte, len(tail))) {
		t.Fatal("padding after the image must be zeros")
	}
	for _, w := range dev.writes {
		if len(w.data)%Alignment != 0 || w.offset%Alignment != 0 {
			t.Fatalf("write of %d bytes at %d is not sector aligned", len(w.data), w.offset)
		}
	}
	want := sha256.Sum256(img)
	if sum != hex.EncodeToString(want[:]) {
		t.Fatalf("sha256 = %s, want %x", sum, want)
	}
	if last := reported[len(reported)-1]; last != int64(len(img)) {
		t.Fatalf("last progress = %d, want %d", last, len(img))
	}
	for i := 1; i < len(reported); i++ {
		if reported[i] <= reported[i-1] {
			t.Fatalf("progress must grow: %v", reported)
		}
	}
}

// The partition table lives in the first chunk. Blanking it first and
// writing it last keeps the OS from mounting half-written partitions, and
// an interrupted write leaves no stale table behind.
func TestWriteBlanksTheFirstChunkAndWritesItLast(t *testing.T) {
	img := randomImage(t, 3*ChunkSize)
	dev := &fakeDevice{data: bytes.Repeat([]byte{0xEE}, 4*ChunkSize)} // old content
	if _, err := Write(context.Background(), dev, bytes.NewReader(img), int64(len(img)), 1<<30, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	first, last := dev.writes[0], dev.writes[len(dev.writes)-1]
	if first.offset != 0 || !bytes.Equal(first.data, make([]byte, ChunkSize)) {
		t.Fatalf("first write must blank chunk 0, got %d bytes at %d", len(first.data), first.offset)
	}
	if last.offset != 0 || !bytes.Equal(last.data, img[:ChunkSize]) {
		t.Fatalf("last write must be the image's first chunk at offset 0, got %d bytes at %d", len(last.data), last.offset)
	}
}

func TestWriteSingleChunkImage(t *testing.T) {
	img := randomImage(t, 10_000)
	dev := &fakeDevice{}
	var reported []int64
	if _, err := Write(context.Background(), dev, bytes.NewReader(img), int64(len(img)), 1<<30,
		func(done int64) { reported = append(reported, done) }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dev.data[:len(img)], img) {
		t.Fatal("device content differs from the image")
	}
	if len(reported) != 1 || reported[0] != int64(len(img)) {
		t.Fatalf("progress = %v, want one report of %d", reported, len(img))
	}
}

func TestWritePaddingStopsAtDeviceEnd(t *testing.T) {
	img := randomImage(t, 10_000)
	deviceSize := int64(10_100) // smaller than the image rounded up to 4096
	dev := &fakeDevice{}
	if _, err := Write(context.Background(), dev, bytes.NewReader(img), int64(len(img)), deviceSize, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if int64(len(dev.data)) > deviceSize {
		t.Fatalf("wrote %d bytes past a %d-byte device", len(dev.data), deviceSize)
	}
	if !bytes.Equal(dev.data[:len(img)], img) {
		t.Fatal("device content differs from the image")
	}
}

func TestWriteStopsWhenCancelled(t *testing.T) {
	img := randomImage(t, 4*ChunkSize)
	ctx, cancel := context.WithCancel(context.Background())
	dev := &fakeDevice{data: bytes.Repeat([]byte{0xEE}, 4*ChunkSize)}
	_, err := Write(ctx, dev, bytes.NewReader(img), int64(len(img)), 1<<30, func(int64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(dev.writes) >= 5 { // blank chunk 0 + 3 chunks + real chunk 0
		t.Fatalf("writing must stop after cancellation, got %d writes", len(dev.writes))
	}
	if !bytes.Equal(dev.data[:ChunkSize], make([]byte, ChunkSize)) {
		t.Fatal("a cancelled write must leave the first chunk blank, so no stale partition table remains")
	}
}

func TestWriteFailsWhenImageIsShorterThanExpected(t *testing.T) {
	img := randomImage(t, 1000)
	if _, err := Write(context.Background(), &fakeDevice{}, bytes.NewReader(img), 5000, 1<<30, func(int64) {}); err == nil {
		t.Fatal("want an error when the image ends early")
	}
}

func TestVerify(t *testing.T) {
	img := randomImage(t, ChunkSize+5000)
	// What a device returns: the image, then whatever was already on it.
	device := append(append([]byte(nil), img...), bytes.Repeat([]byte{0xAB}, 1<<20)...)

	t.Run("matching device", func(t *testing.T) {
		var last int64
		err := Verify(context.Background(), bytes.NewReader(device), bytes.NewReader(img), int64(len(img)), int64(len(device)),
			func(done int64) { last = done })
		if err != nil {
			t.Fatal(err)
		}
		if last != int64(len(img)) {
			t.Fatalf("last progress = %d, want %d", last, len(img))
		}
	})

	t.Run("corrupted byte is reported with its offset", func(t *testing.T) {
		bad := append([]byte(nil), device...)
		const offset = ChunkSize + 17
		bad[offset] ^= 0xFF
		err := Verify(context.Background(), bytes.NewReader(bad), bytes.NewReader(img), int64(len(img)), int64(len(bad)), func(int64) {})
		var mismatch *MismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("err = %v, want *MismatchError", err)
		}
		if mismatch.Offset != offset {
			t.Fatalf("mismatch offset = %d, want %d", mismatch.Offset, offset)
		}
	})

	t.Run("device shorter than the image", func(t *testing.T) {
		err := Verify(context.Background(), bytes.NewReader(img[:100]), bytes.NewReader(img), int64(len(img)), int64(len(device)), func(int64) {})
		if err == nil {
			t.Fatal("want an error when the device cannot be read back")
		}
	})
}
