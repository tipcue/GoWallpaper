package video

import "time"

// Frame holds a single decoded video frame.
type Frame struct {
	// Width and Height of the frame in pixels.
	Width, Height int

	// Stride is the number of bytes per row.
	// In the Decoder's output, this is guaranteed to be a tightly-packed stride
	// (e.g., Width * 4 for RGBA).
	Stride int

	// Data contains the raw pixel bytes in a tightly-packed layout.
	// For RGBA format: Width * Height * 4 bytes.
	// For NV12 format: Y plane (Width * Height bytes) followed immediately
	// by the interleaved UV plane (Width * Height / 2 bytes).
	Data []byte

	// Format identifies the pixel format of Data.
	Format PixelFormat

	// PTS is the presentation timestamp for this frame.
	PTS time.Duration
}

// PixelFormat identifies the pixel layout stored in Frame.Data.
type PixelFormat int

const (
	// PixelFormatRGBA is 32-bit RGBA, 4 bytes per pixel.
	PixelFormatRGBA PixelFormat = iota
	// PixelFormatNV12 is a bi-planar YUV 4:2:0 format (Y plane then interleaved UV).
	PixelFormatNV12
)
