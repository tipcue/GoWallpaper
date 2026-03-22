// Package video handles video file decoding via FFmpeg (cgo).
package video

import "time"

// Frame holds a single decoded video frame.
type Frame struct {
	// Width and Height of the frame in pixels.
	Width, Height int

	// Stride is the number of bytes per row (may be wider than Width for alignment).
	Stride int

	// Data contains the raw pixel bytes.
	// For RGBA format: 4 bytes per pixel, row-major order.
	// For NV12 format: Y plane (Width×Height bytes) followed by interleaved UV plane.
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
