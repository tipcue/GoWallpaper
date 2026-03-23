//go:build windows

// Command livewallpaper renders a video file as a Windows live wallpaper by
// embedding an OpenGL window behind the desktop icons via the WorkerW trick.
//
// Usage:
//
// livewallpaper [-config assets/config.json]
//
// The program reads its configuration from a JSON file (default:
// assets/config.json next to the executable) and then starts the shared
// wallpaper engine from internal/engine.
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/tipcue/GoWallpaper/internal/app"
	"github.com/tipcue/GoWallpaper/internal/engine"
)

func init() {
	// GLFW and OpenGL must run on the OS main thread.
	runtime.LockOSThread()
}

func main() {
	cfgPath := flag.String("config", defaultConfigPath(), "path to config.json")
	flag.Parse()

	// Open a log file for debugging.
	if lf, err := os.Create("livewallpaper.log"); err == nil {
		defer lf.Close()
		log.SetOutput(io.MultiWriter(os.Stderr, lf))
	}

	// Ensure only one instance is running.
	if err := app.EnsureSingleInstance(); err != nil {
		log.Fatalf("livewallpaper: only one instance allowed: %v", err)
	}
	defer app.CloseSingleInstance()

	log.Printf("[DEBUG] Config path: %s", *cfgPath)

	cfg, err := engine.LoadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("livewallpaper: load config: %v", err)
	}

	// Resolve VideoPath relative to the config file directory for the CLI.
	// The GUI always stores absolute paths (via the folder picker), so it does
	// not need this resolution step.
	if cfg.VideoPath != "" && !filepath.IsAbs(cfg.VideoPath) {
		cfg.VideoPath = filepath.Clean(filepath.Join(filepath.Dir(*cfgPath), cfg.VideoPath))
	}

	log.Printf("[DEBUG] Config: VideoPath=%s, Mode=%s, Loop=%v, FPSLimit=%d",
		cfg.VideoPath, cfg.Mode, cfg.Loop, cfg.FPSLimit)

	// engine.Run executes synchronously on the current (main OS) thread.
	if err := engine.Run(context.Background(), cfg); err != nil {
		log.Fatalf("livewallpaper: %v", err)
	}

	log.Printf("[INFO] Wallpaper terminated successfully")
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
