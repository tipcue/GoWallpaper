//go:build windows

package engine

import (
	"context"
	"fmt"
	"io"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"

	glrender "github.com/tipcue/GoWallpaper/internal/render/gl"
	"github.com/tipcue/GoWallpaper/internal/video"
	"github.com/tipcue/GoWallpaper/internal/win"
)

// Engine manages the live wallpaper rendering lifecycle.
// Call Start to begin playback and Stop to terminate it.
// Engine is safe for concurrent use from a single UI goroutine.
type Engine struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}

	// OnStop, if non-nil, is called from a background goroutine whenever the
	// render loop exits for any reason (natural end-of-video, Stop() call, or
	// init error). Fyne widget methods (SetText, Enable, Disable) are
	// goroutine-safe and may be called directly inside this function.
	OnStop func()
}

// Start launches (or restarts) the wallpaper engine with the given config.
// It blocks until the engine has finished initialising (or returns an error).
// Any previously running engine is stopped first.
func (e *Engine) Start(cfg *Config) error {
	e.mu.Lock()

	e.stopLocked()

	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel

	done := make(chan struct{})
	e.done = done

	// initResult carries the init-phase error (or nil on success) back to Start.
	initResult := make(chan error, 1)

	// Release the mutex before blocking so Stop() can still be called if needed.
	e.mu.Unlock()

	go func() {
		// Lock this goroutine to a single OS thread; GLFW requires all window
		// operations and OpenGL context calls to come from the same OS thread.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)

		runEngine(ctx, cfg, initResult)
	}()

	// Cleanup goroutine: once the render goroutine exits (for any reason),
	// clear the engine state so Running() is accurate and fire OnStop.
	go func() {
		<-done // wait for render goroutine to finish

		e.mu.Lock()
		// Guard against a concurrent Start() that has already replaced e.done.
		// Capture onStop inside the guard so it belongs to this engine run.
		var onStop func()
		if e.done == done {
			onStop = e.OnStop
			if e.cancel != nil {
				e.cancel()
				e.cancel = nil
			}
			e.done = nil
		}
		e.mu.Unlock()

		if onStop != nil {
			onStop()
		}
	}()

	// Block until the engine has finished starting up.
	return <-initResult
}

// Stop terminates the running engine and waits for the render goroutine to exit.
// Stop is a no-op if the engine is not running.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopLocked()
}

// Running reports whether the engine is currently rendering.
// It is accurate even if the render goroutine has exited naturally between
// the last Stop() call and the next cleanup goroutine execution.
func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.done == nil {
		return false
	}
	// Non-blocking check: if the channel is already closed the goroutine has
	// exited. Update state immediately so callers always see the truth.
	select {
	case <-e.done:
		if e.cancel != nil {
			e.cancel()
			e.cancel = nil
		}
		e.done = nil
		return false
	default:
		return true
	}
}

// stopLocked cancels the context and waits for the goroutine to exit.
// Must be called with e.mu held.
func (e *Engine) stopLocked() {
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.done != nil {
		<-e.done
		e.done = nil
	}
}

// Run executes the wallpaper engine synchronously in the calling goroutine until
// ctx is cancelled or the video ends (when Loop is false).
// The caller must have called runtime.LockOSThread() before invoking Run.
// This is the entry point used by the CLI so it can run on the main OS thread.
func Run(ctx context.Context, cfg *Config) error {
	done := make(chan error, 1)
	runEngine(ctx, cfg, done)
	return <-done
}

// ParseScaleMode converts the "mode" config string to a ScaleMode constant.
func ParseScaleMode(s string) glrender.ScaleMode {
	switch s {
	case "contain":
		return glrender.ScaleContain
	case "stretch":
		return glrender.ScaleStretch
	default:
		return glrender.ScaleCover
	}
}

// runEngine is the GLFW + OpenGL wallpaper render loop.
// It signals initResult with nil on successful startup, or with an error if
// initialisation fails. After signalling, it runs until ctx is cancelled
// or the video ends. Must be called from a goroutine with LockOSThread active.
func runEngine(ctx context.Context, cfg *Config, initResult chan<- error) {
	signal := func(err error) { initResult <- err }

	// ── 1. Open video decoder ────────────────────────────────────────────────
	dec, err := video.Open(cfg.VideoPath, video.PixelFormatRGBA)
	if err != nil {
		signal(fmt.Errorf("open video %q: %w", cfg.VideoPath, err))
		return
	}
	defer dec.Close()

	// ── 2. Initialise GLFW ───────────────────────────────────────────────────
	if err := glfw.Init(); err != nil {
		signal(fmt.Errorf("glfw init: %w", err))
		return
	}
	defer glfw.Terminate()

	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Decorated, glfw.False)
	glfw.WindowHint(glfw.Resizable, glfw.False)
	glfw.WindowHint(glfw.Visible, glfw.False) // shown after reparent

	monitor := glfw.GetPrimaryMonitor()
	mode := monitor.GetVideoMode()
	winW, winH := mode.Width, mode.Height

	glfwWin, err := glfw.CreateWindow(winW, winH, "livewallpaper", nil, nil)
	if err != nil {
		signal(fmt.Errorf("create glfw window: %w", err))
		return
	}
	glfwWin.MakeContextCurrent()
	// Avoid double-throttling: when the user sets FPSLimit we pace with
	// time.Sleep below and leave the swap chain unthrottled. Otherwise use
	// vsync so idle wallpapers do not burn cycles racing the GPU.
	if cfg.FPSLimit > 0 {
		glfw.SwapInterval(0)
	} else {
		glfw.SwapInterval(1)
	}

	// ── 3. Reparent into WorkerW ─────────────────────────────────────────────
	hwnd, err := win.HWNDFromGLFW(glfwWin)
	if err != nil {
		signal(fmt.Errorf("get hwnd: %w", err))
		return
	}

	workerW, err := win.FindOrCreateWorkerW()
	if err != nil {
		signal(fmt.Errorf("find workerw: %w", err))
		return
	}

	win.ApplyChildStyle(hwnd)
	glfwWin.Show()

	if err := win.SetParentToWorkerW(hwnd, workerW); err != nil {
		signal(fmt.Errorf("set parent: %w", err))
		return
	}
	if err := win.MakeFullscreen(hwnd); err != nil {
		signal(fmt.Errorf("make fullscreen: %w", err))
		return
	}
	win.PlaceAtBottom(hwnd)
	win.MoveToOrigin(hwnd)

	time.Sleep(100 * time.Millisecond)
	win.PlaceAtBottom(hwnd)

	// ── 4. Initialise OpenGL ─────────────────────────────────────────────────
	if err := gl.Init(); err != nil {
		signal(fmt.Errorf("gl init: %w", err))
		return
	}

	renderer, err := glrender.New(winW, winH, ParseScaleMode(cfg.Mode))
	if err != nil {
		signal(fmt.Errorf("create renderer: %w", err))
		return
	}
	defer renderer.Close()

	// Initialisation succeeded – unblock the caller.
	signal(nil)

	// ── 5. Start environment monitors ───────────────────────────────────────
	watchdog := win.StartWatchdog(2 * time.Second)
	defer watchdog.Stop()

	detector := StartFullscreenDetector(3 * time.Second)
	defer detector.Stop()

	// ── 6. Main render loop ──────────────────────────────────────────────────
	// Pace by media PTS when available; FPSLimit is only an upper bound.
	var pacer Pacer
	if cfg.FPSLimit > 0 {
		pacer.MinFrame = time.Second / time.Duration(cfg.FPSLimit)
	}

	frameCount := 0
	loopStart := time.Now()
	suspended := false

	for !glfwWin.ShouldClose() {
		// Honour stop requests between frames.
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Handle desktop environment changes (Explorer restart, display change).
		select {
		case ev := <-watchdog.Events:
			switch ev.Kind {
			case win.ExplorerRestarted:
				log.Printf("[INFO] engine: explorer restarted, reattaching wallpaper")
				if err := win.Reattach(hwnd); err != nil {
					log.Printf("[ERROR] engine: reattach failed: %v", err)
				}
				pacer.Reset()
			case win.DisplayChanged:
				log.Printf("[INFO] engine: display changed, resizing wallpaper")
				if err := win.MakeFullscreen(hwnd); err != nil {
					log.Printf("[ERROR] engine: resize failed: %v", err)
				}
				win.PlaceAtBottom(hwnd)
				pacer.Reset()
			}
		default:
		}

		// Handle fullscreen app suspension.
		select {
		case suspended = <-detector.Suspended:
			if suspended {
				// Reset frame timing so we don't burst-decode on resume.
				pacer.Reset()
			}
		default:
		}

		// While suspended, idle without decoding or presenting frames.
		if suspended {
			glfw.PollEvents()
			time.Sleep(200 * time.Millisecond)
			continue
		}

		frameCount++

		frame, err := dec.ReadFrame()
		if err != nil {
			if err == io.EOF {
				if cfg.Loop {
					if seekErr := dec.Seek(); seekErr != nil {
						log.Printf("engine: seek error: %v", seekErr)
						return
					}
					pacer.Reset()
					frameCount = 0
					loopStart = time.Now()
					continue
				}
				return
			}
			log.Printf("engine: read frame error: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Wait until this frame's PTS (and FPS cap) before presenting.
		pacer.Wait(ctx, frame.PTS)
		if ctx.Err() != nil {
			return
		}

		renderer.Upload(frame.Data, frame.Width, frame.Height)
		renderer.Draw()
		glfwWin.SwapBuffers()
		glfw.PollEvents()

		if frameCount%60 == 0 {
			elapsed := time.Since(loopStart)
			log.Printf("[INFO] engine: %d frames, avg %.1f fps",
				frameCount, float64(frameCount)/elapsed.Seconds())
		}
	}
}
