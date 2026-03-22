//go:build windows

// Command livewallpaper renders a video file as a Windows live wallpaper by
// embedding an OpenGL window behind the desktop icons via the WorkerW trick.
//
// Usage:
//
//	livewallpaper [-config assets/config.json]
//
// The program reads its configuration from a JSON file (default:
// assets/config.json next to the executable) and then:
//  1. Opens the video file with FFmpeg.
//  2. Creates a GLFW window and initialises OpenGL.
//  3. Re-parents the GLFW window into the WorkerW desktop background.
//  4. Runs the main loop: decode frame → upload to GPU → draw → swap buffers.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"

	glrender "github.com/tipcue/GoWallpaper/internal/render/gl"
	"github.com/tipcue/GoWallpaper/internal/video"
	"github.com/tipcue/GoWallpaper/internal/win"
)

func init() {
	// GLFW and OpenGL must run on the OS main thread.
	runtime.LockOSThread()
}

// Config holds the JSON configuration loaded from assets/config.json.
type Config struct {
	VideoPath string `json:"videoPath"`
	// Mode controls frame scaling: "cover", "contain", or "stretch".
	Mode     string `json:"mode"`
	Loop     bool   `json:"loop"`
	FPSLimit int    `json:"fpsLimit"`
}

func main() {
	cfgPath := flag.String("config", defaultConfigPath(), "path to config.json")
	flag.Parse()

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("livewallpaper: load config: %v", err)
	}

	if err := run(cfg); err != nil {
		log.Fatalf("livewallpaper: %v", err)
	}
}

// run is the main application logic, separated from main() for testability.
func run(cfg *Config) error {
	// ── 1. Open video decoder ────────────────────────────────────────────────
	dec, err := video.Open(cfg.VideoPath, video.PixelFormatRGBA)
	if err != nil {
		return fmt.Errorf("open video %q: %w", cfg.VideoPath, err)
	}
	defer dec.Close()

	// ── 2. Initialise GLFW ───────────────────────────────────────────────────
	if err := glfw.Init(); err != nil {
		return fmt.Errorf("glfw init: %w", err)
	}
	defer glfw.Terminate()

	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Decorated, glfw.False) // no window chrome
	glfw.WindowHint(glfw.Resizable, glfw.False)
	glfw.WindowHint(glfw.Visible, glfw.False) // start hidden; shown after reparent

	monitor := glfw.GetPrimaryMonitor()
	mode := monitor.GetVideoMode()
	winW, winH := mode.Width, mode.Height

	glfwWin, err := glfw.CreateWindow(winW, winH, "livewallpaper", nil, nil)
	if err != nil {
		return fmt.Errorf("create glfw window: %w", err)
	}
	glfwWin.MakeContextCurrent()

	// ── 3. Reparent into WorkerW ─────────────────────────────────────────────
	hwnd, err := win.HWNDFromGLFW(glfwWin)
	if err != nil {
		return fmt.Errorf("get hwnd: %w", err)
	}

	workerW, err := win.FindOrCreateWorkerW()
	if err != nil {
		return fmt.Errorf("find workerw: %w", err)
	}

	win.ApplyChildStyle(hwnd)

	if err := win.SetParentToWorkerW(hwnd, workerW); err != nil {
		return fmt.Errorf("set parent: %w", err)
	}

	if err := win.MakeFullscreen(hwnd); err != nil {
		return fmt.Errorf("make fullscreen: %w", err)
	}

	glfwWin.Show()

	// ── 4. Initialise OpenGL ─────────────────────────────────────────────────
	if err := gl.Init(); err != nil {
		return fmt.Errorf("gl init: %w", err)
	}

	scaleMode := parseScaleMode(cfg.Mode)
	renderer, err := glrender.New(winW, winH, scaleMode)
	if err != nil {
		return fmt.Errorf("create renderer: %w", err)
	}
	defer renderer.Close()

	// ── 5. Main loop ─────────────────────────────────────────────────────────
	var frameDuration time.Duration
	if cfg.FPSLimit > 0 {
		frameDuration = time.Second / time.Duration(cfg.FPSLimit)
	}

	for !glfwWin.ShouldClose() {
		frameStart := time.Now()

		frame, err := dec.ReadFrame()
		if err != nil {
			// End of stream.
			if err == io.EOF {
				if cfg.Loop {
					if seekErr := dec.Seek(); seekErr != nil {
						return fmt.Errorf("seek for loop: %w", seekErr)
					}
					continue
				}
				break
			}
			return fmt.Errorf("read frame: %w", err)
		}

		renderer.Upload(frame.Data, frame.Width, frame.Height)
		renderer.Draw()

		glfwWin.SwapBuffers()
		glfw.PollEvents()

		// Honour FPS limit.
		if frameDuration > 0 {
			elapsed := time.Since(frameStart)
			if sleep := frameDuration - elapsed; sleep > 0 {
				time.Sleep(sleep)
			}
		}
	}

	return nil
}

// loadConfig reads and parses the JSON configuration file at path.
func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.FPSLimit <= 0 {
		cfg.FPSLimit = 30
	}
	return &cfg, nil
}

// defaultConfigPath returns the path to assets/config.json relative to the
// directory containing the executable.
func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join("assets", "config.json")
	}
	return filepath.Join(filepath.Dir(exe), "assets", "config.json")
}

// parseScaleMode converts the "mode" config string to a ScaleMode constant.
func parseScaleMode(s string) glrender.ScaleMode {
	switch s {
	case "contain":
		return glrender.ScaleContain
	case "stretch":
		return glrender.ScaleStretch
	default:
		return glrender.ScaleCover
	}
}
