// Package gl provides an OpenGL-based renderer for video frames.
package gl

import (
	"fmt"
	"strings"

	"github.com/go-gl/gl/v4.1-core/gl"
)

// compileShader compiles GLSL source src of the given shaderType and returns
// the shader object handle, or an error with the info log.
func compileShader(src string, shaderType uint32) (uint32, error) {
	shader := gl.CreateShader(shaderType)

	cSrc, free := gl.Strs(src)
	gl.ShaderSource(shader, 1, cSrc, nil)
	free()

	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLen int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLen)
		log := strings.Repeat("\x00", int(logLen+1))
		gl.GetShaderInfoLog(shader, logLen, nil, gl.Str(log))
		gl.DeleteShader(shader)
		return 0, fmt.Errorf("compile error: %s", log)
	}
	return shader, nil
}
