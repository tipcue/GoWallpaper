//go:build windows

// Package ui provides the WebView2-based GUI for GoWallpaper, replacing the
// previous fyne implementation. It embeds a single-page HTML frontend and
// exposes Go engine operations to JavaScript via webview bindings.
package ui

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	webview2 "github.com/jchv/go-webview2"

	"github.com/tipcue/GoWallpaper/internal/engine"
)

//go:embed index.html
var indexHTML string

// videoExtensions is the set of container formats supported by FFmpeg builds.
var videoExtensions = map[string]bool{
	".mp4":  true,
	".webm": true,
	".mov":  true,
	".mkv":  true,
	".avi":  true,
}

// Run launches the WebView2 GUI window and blocks until it is closed.
// cfgPath is the path to the persistent JSON config file.
func Run(cfgPath string) error {
	cfg, err := engine.LoadConfig(cfgPath)
	if err != nil {
		cfg = &engine.Config{
			Mode:     "cover",
			Loop:     true,
			FPSLimit: 30,
		}
	}

	var eng engine.Engine

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "GoWallpaper",
			Width:  520,
			Height: 480,
			Center: true,
		},
	})
	if w == nil {
		return fmt.Errorf("failed to create webview2 window (is Edge WebView2 Runtime installed?)")
	}
	defer w.Destroy()

	w.SetSize(520, 480, webview2.HintFixed)

	// hwnd is used as the parent for native dialogs.
	hwnd := uintptr(w.Window())

	// ── Bindings ─────────────────────────────────────────────────────────────

	// getConfig returns the current config as JSON.
	if err := w.Bind("getConfig", func() (string, error) {
		data, err := json.Marshal(cfg)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}); err != nil {
		return fmt.Errorf("bind getConfig: %w", err)
	}

	// saveSettings merges partial settings from the frontend and persists.
	if err := w.Bind("saveSettings", func(jsonStr string) error {
		var patch struct {
			Mode     string `json:"mode"`
			FPSLimit int    `json:"fpsLimit"`
			Loop     *bool  `json:"loop"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &patch); err != nil {
			return fmt.Errorf("parse settings: %w", err)
		}
		if patch.Mode != "" {
			cfg.Mode = patch.Mode
		}
		if patch.FPSLimit > 0 {
			cfg.FPSLimit = patch.FPSLimit
		}
		if patch.Loop != nil {
			cfg.Loop = *patch.Loop
		}
		return engine.SaveConfig(cfg, cfgPath)
	}); err != nil {
		return fmt.Errorf("bind saveSettings: %w", err)
	}

	// pickFolder opens the native folder browser and returns the chosen path.
	if err := w.Bind("pickFolder", func() (string, error) {
		dir, err := PickFolder(hwnd, "Select Wallpaper Folder")
		if err != nil {
			return "", err
		}
		if dir == "" {
			return "", nil // cancelled
		}
		cfg.LastDir = dir
		_ = engine.SaveConfig(cfg, cfgPath)
		return dir, nil
	}); err != nil {
		return fmt.Errorf("bind pickFolder: %w", err)
	}

	// scanFolder returns a JSON array of video file paths in the given directory.
	if err := w.Bind("scanFolder", func(dir string) (string, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", fmt.Errorf("read directory: %w", err)
		}
		var files []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if videoExtensions[strings.ToLower(filepath.Ext(e.Name()))] {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
		sort.Strings(files)
		data, err := json.Marshal(files)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}); err != nil {
		return fmt.Errorf("bind scanFolder: %w", err)
	}

	// applyVideo starts the wallpaper engine with the given video path.
	if err := w.Bind("applyVideo", func(path string) error {
		c := *cfg // copy
		c.VideoPath = path
		if err := eng.Start(&c); err != nil {
			return err
		}
		// Persist last video + folder.
		cfg.VideoPath = path
		cfg.LastDir = filepath.Dir(path)
		if saveErr := engine.SaveConfig(cfg, cfgPath); saveErr != nil {
			log.Printf("ui: save config: %v", saveErr)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("bind applyVideo: %w", err)
	}

	// stopVideo stops the running engine.
	if err := w.Bind("stopVideo", func() error {
		eng.Stop()
		return nil
	}); err != nil {
		return fmt.Errorf("bind stopVideo: %w", err)
	}

	// ── Engine stop callback ─────────────────────────────────────────────────
	// Called from a background goroutine when the engine exits for any reason.
	// Dispatch ensures the JS eval runs on the UI thread.
	eng.OnStop = func() {
		w.Dispatch(func() {
			w.Eval(`if(window.onEngineStop) window.onEngineStop();`)
		})
	}

	// ── Load UI ──────────────────────────────────────────────────────────────
	w.SetHtml(indexHTML)

	fmt.Printf("GoWallpaper GUI starting. Config: %s\n", cfgPath)
	w.Run()

	// Window closed — stop engine cleanly.
	eng.Stop()
	return nil
}

