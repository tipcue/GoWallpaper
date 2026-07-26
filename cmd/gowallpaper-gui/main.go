//go:build windows

// Command gowallpaper-gui is a lightweight Windows GUI for GoWallpaper.
// It uses WebView2 (Edge runtime) to render a single-page HTML interface,
// replacing the previous fyne-based implementation with a much smaller
// binary and fewer dependencies.
//
// The wallpaper engine (GLFW + OpenGL) runs on a dedicated OS thread while
// the WebView2 message loop runs on the main goroutine.
package main

import (
	"log"

	"github.com/tipcue/GoWallpaper/internal/engine"
	"github.com/tipcue/GoWallpaper/internal/ui"
)

func main() {
	cfgPath := engine.DefaultAppDataConfigPath()
	if err := ui.Run(cfgPath); err != nil {
		log.Fatalf("gowallpaper-gui: %v", err)
	}
}
