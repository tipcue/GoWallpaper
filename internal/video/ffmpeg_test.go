//go:build windows

package video

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// findSampleMP4 locates assets/sample.mp4 relative to the package directory.
// The test runs in internal/video/, so the file lives two levels up.
func findSampleMP4(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "assets", "sample.mp4"),
		filepath.Join("..", "assets", "sample.mp4"),
		filepath.Join("assets", "sample.mp4"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, err := filepath.Abs(c)
			if err != nil {
				return c
			}
			return abs
		}
	}
	t.Skip("assets/sample.mp4 not found; generate it with `ffmpeg -f lavfi -i testsrc=duration=3:size=640x360:rate=30 -c:v libx264 -pix_fmt yuv420p -y assets/sample.mp4`")
	return ""
}

func TestOpen_Close(t *testing.T) {
	path := findSampleMP4(t)
	dec, err := Open(path, PixelFormatRGBA)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer dec.Close()

	if dec.Width() <= 0 || dec.Height() <= 0 {
		t.Fatalf("decoder dimensions non-positive: %dx%d", dec.Width(), dec.Height())
	}
}

func TestReadFrame_RGBA(t *testing.T) {
	path := findSampleMP4(t)
	dec, err := Open(path, PixelFormatRGBA)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer dec.Close()

	frame, err := dec.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame failed: %v", err)
	}
	if frame == nil {
		t.Fatal("ReadFrame returned nil frame with nil error")
	}
	if frame.Width != dec.Width() || frame.Height != dec.Height() {
		t.Fatalf("frame dims %dx%d != decoder dims %dx%d",
			frame.Width, frame.Height, dec.Width(), dec.Height())
	}
	if frame.Format != PixelFormatRGBA {
		t.Fatalf("frame format = %d, want RGBA(%d)", frame.Format, PixelFormatRGBA)
	}
	wantSize := frame.Width * frame.Height * 4
	if len(frame.Data) < wantSize {
		t.Fatalf("frame data len %d < expected %d (W*H*4)", len(frame.Data), wantSize)
	}
	if frame.Stride != frame.Width*4 {
		t.Fatalf("stride = %d, want %d", frame.Stride, frame.Width*4)
	}
	// Sanity: RGBA bytes should not be all-zero (testsrc produces coloured content).
	anyNonZero := false
	for _, b := range frame.Data[:wantSize] {
		if b != 0 {
			anyNonZero = true
			break
		}
	}
	if !anyNonZero {
		t.Fatal("first frame is entirely zero bytes — decoder likely produced a blank buffer")
	}
}

func TestReadFrame_EOF(t *testing.T) {
	path := findSampleMP4(t)
	dec, err := Open(path, PixelFormatRGBA)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer dec.Close()

	count := 0
	for {
		_, err := dec.ReadFrame()
		if err == nil {
			count++
			if count > 1000 {
				t.Fatal("read > 1000 frames without EOF; stream is not finite")
			}
			continue
		}
		if !errors.Is(err, io.EOF) {
			t.Fatalf("expected io.EOF at end of stream, got: %v", err)
		}
		break
	}
	if count == 0 {
		t.Fatal("read 0 frames before EOF; fixture produced no decodable frames")
	}
	t.Logf("read %d frames before EOF", count)
}

func TestSeek_Loops(t *testing.T) {
	path := findSampleMP4(t)
	dec, err := Open(path, PixelFormatRGBA)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer dec.Close()

	// Read first frame and remember its PTS.
	first, err := dec.ReadFrame()
	if err != nil {
		t.Fatalf("initial ReadFrame failed: %v", err)
	}
	if err := dec.Seek(); err != nil {
		t.Fatalf("Seek failed: %v", err)
	}
	again, err := dec.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame after Seek failed: %v", err)
	}
	if again.PTS != first.PTS {
		t.Fatalf("PTS after seek = %v, want %v (loop not restarting at head)", again.PTS, first.PTS)
	}
}

func TestOpen_MissingFile(t *testing.T) {
	_, err := Open("definitely-does-not-exist.mp4", PixelFormatRGBA)
	if err == nil {
		t.Fatal("Open succeeded for a missing file; expected error")
	}
}

func TestOpen_UnsupportedFormat(t *testing.T) {
	path := findSampleMP4(t)
	if _, err := Open(path, PixelFormat(999)); err == nil {
		t.Fatal("Open succeeded with unsupported pixel format; expected error")
	}
}
