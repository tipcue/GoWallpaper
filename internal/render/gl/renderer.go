package gl

type Renderer struct {
	pictureWidth  int
	pictureHeight int
	frameSize    int
	// other fields...
}

func (r *Renderer) Upload(newFrame []byte) {
	// Only allocate texture storage when the frame size changes
	if len(newFrame) != r.frameSize {
		// Allocate texture storage
		gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, int32(r.pictureWidth), int32(r.pictureHeight), 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
		r.frameSize = len(newFrame)
	}
	// Upload the frame using TexSubImage2D
	gl.TexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, int32(r.pictureWidth), int32(r.pictureHeight), gl.RGBA, gl.UNSIGNED_BYTE, newFrame)
}