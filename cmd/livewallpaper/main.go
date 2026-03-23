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

	"github.com/tipcue/GoWallpaper/internal/app"
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

	// Open a log file for debugging
	logFile, err := os.Create("livewallpaper.log")
	if err == nil {
		defer logFile.Close()
		log.SetOutput(io.MultiWriter(os.Stderr, logFile))
	}

	// Ensure only one instance is running
	if err := app.EnsureSingleInstance(); err != nil {
		log.Fatalf("livewallpaper: only one instance allowed: %v", err)
	}
	defer app.CloseSingleInstance()

	log.Printf("[DEBUG] Config path: %s", *cfgPath)

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("livewallpaper: load config: %v", err)
	}

	log.Printf("[DEBUG] Config loaded: VideoPath=%s, Mode=%s, Loop=%v, FPSLimit=%d", cfg.VideoPath, cfg.Mode, cfg.Loop, cfg.FPSLimit)

	if err := run(cfg); err != nil {
		log.Fatalf("livewallpaper: %v", err)
	}

	log.Printf("[INFO] Wallpaper terminated successfully")
}

// run is the main application logic, separated from main() for testability.
func run(cfg *Config) error {
	// ── 1. Open video decoder ────────────────────────────────────────────────
	log.Printf("[DEBUG] Opening video file: %s", cfg.VideoPath)
	dec, err := video.Open(cfg.VideoPath, video.PixelFormatRGBA)
	if err != nil {
		return fmt.Errorf("open video %q: %w", cfg.VideoPath, err)
	}
	log.Printf("[DEBUG] Video file opened successfully")
	defer dec.Close()

	// ── 2. Initialise GLFW ───────────────────────────────────────────────────
	log.Printf("[DEBUG] Initializing GLFW")
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
	log.Printf("[DEBUG] Monitor resolution: %d x %d", winW, winH)

	glfwWin, err := glfw.CreateWindow(winW, winH, "livewallpaper", nil, nil)
	if err != nil {
		return fmt.Errorf("create glfw window: %w", err)
	}
	log.Printf("[DEBUG] GLFW window created")
	glfwWin.MakeContextCurrent()

	// Enable vsync to improve display stability
	glfw.SwapInterval(1)
	log.Printf("[DEBUG] GLFW swap interval set to 1")

	// ── 3. Reparent into WorkerW ─────────────────────────────────────────────
	log.Printf("[DEBUG] Getting HWND from GLFW window")
	hwnd, err := win.HWNDFromGLFW(glfwWin)
	if err != nil {
		return fmt.Errorf("get hwnd: %w", err)
	}

	log.Printf("[DEBUG] Finding or creating WorkerW")
	workerW, err := win.FindOrCreateWorkerW()
	if err != nil {
		return fmt.Errorf("find workerw: %w", err)
	}

	win.ApplyChildStyle(hwnd)

	// Show the window BEFORE reparenting to ensure it's visible to SetParent
	glfwWin.Show()
	log.Printf("[DEBUG] GLFW window shown BEFORE SetParent")

	log.Printf("[DEBUG] Setting parent to WorkerW")
	if err := win.SetParentToWorkerW(hwnd, workerW); err != nil {
		return fmt.Errorf("set parent: %w", err)
	}

	if err := win.MakeFullscreen(hwnd); err != nil {
		return fmt.Errorf("make fullscreen: %w", err)
	}

	win.PlaceAtBottom(hwnd)

	// Force window to (0, 0) LAST - after all other positioning operations
	// This ensures the wallpaper window is at the correct position
	win.MoveToOrigin(hwnd)

	// Check window position and size before showing
	width, height := glfwWin.GetSize()
	x, y := glfwWin.GetPos()
	log.Printf("[DEBUG] Window position: (%d, %d), size: %d x %d", x, y, width, height)

	// Wait for window system to settle
	time.Sleep(100 * time.Millisecond)
	win.PlaceAtBottom(hwnd)

	log.Printf("[DEBUG] WorkerW reparenting complete")

	// ── 4. Initialise OpenGL ─────────────────────────────────────────────────
	log.Printf("[DEBUG] Initializing OpenGL")
	if err := gl.Init(); err != nil {
		return fmt.Errorf("gl init: %w", err)
	}

	scaleMode := parseScaleMode(cfg.Mode)
	renderer, err := glrender.New(winW, winH, scaleMode)
	if err != nil {
		return fmt.Errorf("create renderer: %w", err)
	}
	log.Printf("[DEBUG] OpenGL renderer created, starting main loop")
	defer renderer.Close()

	// ── 5. Main loop ─────────────────────────────────────────────────────────
	var frameDuration time.Duration
	if cfg.FPSLimit > 0 {
		frameDuration = time.Second / time.Duration(cfg.FPSLimit)
	}

	frameCount := 0
	loopStart := time.Now()
	firstFrameRendered := false

	for !glfwWin.ShouldClose() {
		frameStart := time.Now()
		frameCount++

		if frameCount == 1 {
			log.Printf("[DEBUG] ==== MAIN LOOP: Frame 1 starting ====")
		}

		frame, err := dec.ReadFrame()
		if err != nil {
			// End of stream.
			if err == io.EOF {
				log.Printf("[DEBUG] End of video reached after %d frames", frameCount)
				if cfg.Loop {
					log.Printf("[DEBUG] Looping video")
					if seekErr := dec.Seek(); seekErr != nil {
						log.Printf("livewallpaper: seek for loop error: %v", seekErr)
						break
					}
					frameCount = 0
					firstFrameRendered = false
					continue
				}
				break
			}
			log.Printf("livewallpaper: read frame error: %v", err)
			// Try to recover by seeking or waiting a bit.
			time.Sleep(100 * time.Millisecond)
			continue
		}

		renderer.Upload(frame.Data, frame.Width, frame.Height)
		renderer.Draw()
		glfwWin.SwapBuffers()
		glfw.PollEvents()

		if !firstFrameRendered {
			log.Printf("[DEBUG] First frame rendered to screen successfully")
			firstFrameRendered = true
		}

		if frameCount%60 == 0 {
			elapsed := time.Since(loopStart)
			avgFPS := float64(frameCount) / elapsed.Seconds()
			log.Printf("[INFO] Processed %d frames in %v (avg: %.1f fps)", frameCount, elapsed, avgFPS)
		}

		// Honour FPS limit with a more precise timing approach if needed,
		// but time.Sleep is generally sufficient for wallpaper use.
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

	// Resolve VideoPath relative to the config file directory.
	if cfg.VideoPath != "" && !filepath.IsAbs(cfg.VideoPath) {
		baseDir := filepath.Dir(path)
		cfg.VideoPath = filepath.Clean(filepath.Join(baseDir, cfg.VideoPath))
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
