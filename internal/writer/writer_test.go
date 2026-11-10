package writer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/rand"
	"testing"
)

func randomImage(t *testing.T, size int) []byte {
	t.Helper()
	img := make([]byte, size)
	rand.New(rand.NewSource(42)).Read(img)
	return img
}

// recorder is a fake raw device that records the size of every write.
type recorder struct {
	bytes.Buffer
	writes []int
}

func (r *recorder) Write(p []byte) (int, error) {
	r.writes = append(r.writes, len(p))
	return r.Buffer.Write(p)
}

func TestWriteCopiesImageInAlignedChunks(t *testing.T) {
	img := randomImage(t, 2*ChunkSize+12345) // final chunk is not sector aligned
	dst := &recorder{}
	var reported []int64

	sum, err := Write(context.Background(), dst, bytes.NewReader(img), int64(len(img)), 1<<30,
		func(done int64) { reported = append(reported, done) })
	if err != nil {
		t.Fatal(err)
	}

	got := dst.Bytes()
	if !bytes.Equal(got[:len(img)], img) {
		t.Fatal("device content differs from the image")
	}
	if len(got)%Alignment != 0 {
		t.Fatalf("wrote %d bytes; raw devices need whole %d-byte sectors", len(got), Alignment)
	}
	if tail := got[len(img):]; !bytes.Equal(tail, make([]byte, len(tail))) {
		t.Fatal("padding after the image must be zeros")
	}
	for _, n := range dst.writes {
		if n%Alignment != 0 {
			t.Fatalf("write of %d bytes is not sector aligned (writes: %v)", n, dst.writes)
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

func TestWritePaddingStopsAtDeviceEnd(t *testing.T) {
	img := randomImage(t, 10_000)
	deviceSize := int64(10_100) // smaller than the image rounded up to 4096
	dst := &recorder{}
	if _, err := Write(context.Background(), dst, bytes.NewReader(img), int64(len(img)), deviceSize, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if int64(dst.Len()) > deviceSize {
		t.Fatalf("wrote %d bytes past a %d-byte device", dst.Len(), deviceSize)
	}
	if !bytes.Equal(dst.Bytes()[:len(img)], img) {
		t.Fatal("device content differs from the image")
	}
}

func TestWriteStopsWhenCancelled(t *testing.T) {
	img := randomImage(t, 4*ChunkSize)
	ctx, cancel := context.WithCancel(context.Background())
	dst := &recorder{}
	_, err := Write(ctx, dst, bytes.NewReader(img), int64(len(img)), 1<<30, func(int64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if dst.Len() >= len(img) {
		t.Fatal("writing must stop after cancellation")
	}
}

func TestWriteFailsWhenImageIsShorterThanExpected(t *testing.T) {
	img := randomImage(t, 1000)
	if _, err := Write(context.Background(), &recorder{}, bytes.NewReader(img), 5000, 1<<30, func(int64) {}); err == nil {
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
