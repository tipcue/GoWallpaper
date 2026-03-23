// Package gl provides an OpenGL-based renderer for video frames.
//
// It manages a full-screen quad, a GLSL shader program, and a 2-D texture
// that is updated each frame with pixel data supplied by the video package.
package gl

import (
	"fmt"
	"strings"

	"github.com/go-gl/gl/v4.1-core/gl"
)

// ScaleMode controls how frames are scaled to fill the window.
type ScaleMode int

const (
	// ScaleCover scales the frame so that it covers the entire window,
	// cropping the video if its aspect ratio differs from the window.
	ScaleCover ScaleMode = iota
	// ScaleContain fits the entire frame inside the window, adding
	// letterbox / pillarbox bars if necessary.
	ScaleContain
	// ScaleStretch stretches the frame to exactly fill the window,
	// ignoring aspect ratio.
	ScaleStretch
)

// Renderer draws video frames onto a full-screen quad using OpenGL.
type Renderer struct {
	program uint32
	vao     uint32
	vbo     uint32
	texture uint32

	winW, winH     int
	frameW, frameH int
	mode           ScaleMode

	uMin, uMax, vMin, vMax float32
	xMin, xMax, yMin, yMax float32
}

// New creates and initialises a Renderer.
// gl.Init() must have been called successfully before calling New.
func New(winW, winH int, mode ScaleMode) (*Renderer, error) {
	prog, err := newProgram(vertexShaderSrc, fragmentShaderSrc)
	if err != nil {
		return nil, fmt.Errorf("gl: shader program: %w", err)
	}

	r := &Renderer{
		program: prog,
		winW:    winW,
		winH:    winH,
		mode:    mode,
	}

	r.setupQuad()
	r.setupTexture()
	return r, nil
}

// setupQuad creates a VAO/VBO for a full-screen quad.
// The quad covers NDC [-1,1] in X and Y with UV [0,1].
func (r *Renderer) setupQuad() {
	// Each vertex: X, Y, U, V
	vertices := []float32{
		-1, 1, 0, 0, // top-left
		-1, -1, 0, 1, // bottom-left
		1, -1, 1, 1, // bottom-right

		-1, 1, 0, 0, // top-left
		1, -1, 1, 1, // bottom-right
		1, 1, 1, 0, // top-right
	}

	gl.GenVertexArrays(1, &r.vao)
	gl.BindVertexArray(r.vao)

	gl.GenBuffers(1, &r.vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(vertices)*4, gl.Ptr(vertices), gl.STATIC_DRAW)

	// position attribute (location 0): X, Y
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 4*4, 0)

	// texcoord attribute (location 1): U, V
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointerWithOffset(1, 2, gl.FLOAT, false, 4*4, 2*4)

	gl.BindVertexArray(0)
}

// setupTexture allocates the OpenGL texture object.
func (r *Renderer) setupTexture() {
	gl.GenTextures(1, &r.texture)
	gl.BindTexture(gl.TEXTURE_2D, r.texture)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.BindTexture(gl.TEXTURE_2D, 0)
}

// Upload copies an RGBA frame into the GPU texture.
// frameW and frameH are the frame dimensions in pixels; data must be
// frameW×frameH×4 bytes in row-major order.
// Upload copies an RGBA frame into the GPU texture.
// frameW and frameH are the frame dimensions in pixels; data must be
// frameW×frameH×4 bytes in row-major order.
func (r *Renderer) Upload(data []byte, frameW, frameH int) {
	gl.BindTexture(gl.TEXTURE_2D, r.texture)

	// Reallocate texture storage only when the frame size changes.
	if frameW != r.frameW || frameH != r.frameH {
		r.frameW = frameW
		r.frameH = frameH

		// Allocate (or re-allocate) storage without uploading data.
		gl.TexImage2D(
			gl.TEXTURE_2D, 0, gl.RGBA,
			int32(frameW), int32(frameH), 0,
			gl.RGBA, gl.UNSIGNED_BYTE, nil,
		)
	}

	// Upload pixels into existing storage.
	gl.TexSubImage2D(
		gl.TEXTURE_2D, 0,
		0, 0,
		int32(frameW), int32(frameH),
		gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(data),
	)

	gl.BindTexture(gl.TEXTURE_2D, 0)
}

// Draw renders the current texture to the framebuffer.
// It recomputes the vertex quad according to the ScaleMode only when necessary.
func (r *Renderer) Draw() {
	gl.UseProgram(r.program)
	gl.Viewport(0, 0, int32(r.winW), int32(r.winH))
	gl.Clear(gl.COLOR_BUFFER_BIT)

	// Compute geometry (UV offsets and vertex positions) based on ScaleMode.
	uMin, uMax, vMin, vMax, xMin, xMax, yMin, yMax := r.computeGeometry()

	// Update the quad vertices if any dimension changed.
	if uMin != r.uMin || uMax != r.uMax || vMin != r.vMin || vMax != r.vMax ||
		xMin != r.xMin || xMax != r.xMax || yMin != r.yMin || yMax != r.yMax {

		r.uMin, r.uMax, r.vMin, r.vMax = uMin, uMax, vMin, vMax
		r.xMin, r.xMax, r.yMin, r.yMax = xMin, xMax, yMin, yMax

		vertices := []float32{
			xMin, yMax, uMin, vMin,
			xMin, yMin, uMin, vMax,
			xMax, yMin, uMax, vMax,

			xMin, yMax, uMin, vMin,
			xMax, yMin, uMax, vMax,
			xMax, yMax, uMax, vMin,
		}

		gl.BindVertexArray(r.vao)
		gl.BindBuffer(gl.ARRAY_BUFFER, r.vbo)
		gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(vertices)*4, gl.Ptr(vertices))
	}

	gl.BindVertexArray(r.vao)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, r.texture)
	loc := gl.GetUniformLocation(r.program, gl.Str("tex\x00"))
	gl.Uniform1i(loc, 0)

	gl.DrawArrays(gl.TRIANGLES, 0, 6)

	gl.BindVertexArray(0)
}

// computeGeometry returns UV coordinates and vertex positions [xMin,xMax] x [yMin,yMax]
// that implement the requested ScaleMode.
func (r *Renderer) computeGeometry() (uMin, uMax, vMin, vMax, xMin, xMax, yMin, yMax float32) {
	// Default: full screen, full texture
	uMin, uMax, vMin, vMax = 0, 1, 0, 1
	xMin, xMax, yMin, yMax = -1, 1, -1, 1

	if r.frameW == 0 || r.frameH == 0 || r.winW == 0 || r.winH == 0 {
		return
	}

	winAR := float32(r.winW) / float32(r.winH)
	frameAR := float32(r.frameW) / float32(r.frameH)

	switch r.mode {
	case ScaleStretch:
		// Keep defaults: stretch full texture to full screen.

	case ScaleCover:
		// Scale so the frame completely covers the window (may crop).
		if frameAR > winAR {
			// Frame is wider than window: crop left/right.
			visible := winAR / frameAR
			margin := (1 - visible) / 2
			uMin, uMax = margin, 1-margin
		} else {
			// Frame is taller than window: crop top/bottom.
			visible := frameAR / winAR
			margin := (1 - visible) / 2
			vMin, vMax = margin, 1-margin
		}

	case ScaleContain:
		// Fit the entire frame inside the window (letterbox / pillarbox).
		if frameAR > winAR {
			// Frame is wider than window: pillarbox (vertical bars).
			visible := winAR / frameAR
			yMin, yMax = -visible, visible
		} else {
			// Frame is taller than window: letterbox (horizontal bars).
			visible := frameAR / winAR
			xMin, xMax = -visible, visible
		}
	}

	return
}

// Resize updates the stored window dimensions for subsequent Draw calls.
func (r *Renderer) Resize(w, h int) {
	r.winW = w
	r.winH = h
}

// Close releases OpenGL resources owned by the Renderer.
func (r *Renderer) Close() {
	gl.DeleteTextures(1, &r.texture)
	gl.DeleteBuffers(1, &r.vbo)
	gl.DeleteVertexArrays(1, &r.vao)
	gl.DeleteProgram(r.program)
}

// newProgram compiles vertSrc and fragSrc, links them into a program, and
// returns the program handle.
func newProgram(vertSrc, fragSrc string) (uint32, error) {
	vert, err := compileShader(vertSrc, gl.VERTEX_SHADER)
	if err != nil {
		return 0, fmt.Errorf("vertex shader: %w", err)
	}
	frag, err := compileShader(fragSrc, gl.FRAGMENT_SHADER)
	if err != nil {
		gl.DeleteShader(vert)
		return 0, fmt.Errorf("fragment shader: %w", err)
	}

	prog := gl.CreateProgram()
	gl.AttachShader(prog, vert)
	gl.AttachShader(prog, frag)
	gl.LinkProgram(prog)

	gl.DeleteShader(vert)
	gl.DeleteShader(frag)

	var status int32
	gl.GetProgramiv(prog, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLen int32
		gl.GetProgramiv(prog, gl.INFO_LOG_LENGTH, &logLen)
		log := strings.Repeat("\x00", int(logLen+1))
		gl.GetProgramInfoLog(prog, logLen, nil, gl.Str(log))
		gl.DeleteProgram(prog)
		return 0, fmt.Errorf("link error: %s", log)
	}
	return prog, nil
}

// GLSL source for vertex and fragment shaders.
var (
	vertexShaderSrc = `#version 410 core
layout(location = 0) in vec2 aPos;
layout(location = 1) in vec2 aUV;
out vec2 vUV;
void main() {
    gl_Position = vec4(aPos, 0.0, 1.0);
    vUV = aUV;
}
` + "\x00"

	fragmentShaderSrc = `#version 410 core
in vec2 vUV;
out vec4 fragColor;
uniform sampler2D tex;
void main() {
    fragColor = texture(tex, vUV);
}
` + "\x00"
)
