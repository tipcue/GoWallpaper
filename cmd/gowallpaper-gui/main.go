//go:build windows

// Command gowallpaper-gui is a lightweight Windows GUI for GoWallpaper.
// It uses WebView2 (Edge runtime) to render a single-page HTML interface,
// replacing the previous fyne-based implementation with a much smaller
// binary and fewer dependencies.
//
// The wallpaper engine (GLFW + OpenGL) runs on a dedicated OS thread while
// the WebView2 message loop runs on the main goroutine.
//
// Flags:
//
//	--autostart  Launched at logon (by registry or Task Scheduler).
//	             Currently behaves the same as a normal launch; reserved
//	             for future "start minimized to tray" behavior.
package main

import (
	"flag"
	"log"

	"github.com/tipcue/GoWallpaper/internal/app"
	"github.com/tipcue/GoWallpaper/internal/engine"
	"github.com/tipcue/GoWallpaper/internal/ui"
)

func main() {
	autostart := flag.Bool("autostart", false, "launched at logon (start minimized)")
	flag.Parse()
	_ = autostart // reserved for future tray-minimize behavior

	// Ensure only one instance runs at a time.
	if err := app.EnsureSingleInstance(); err != nil {
		log.Fatalf("gowallpaper-gui: %v", err)
	}
	defer app.CloseSingleInstance()

	// Configure graceful shutdown behavior (early notification, no ghost UI).
	app.PrepareShutdown()

	cfgPath := engine.DefaultAppDataConfigPath()
	if err := ui.Run(cfgPath); err != nil {
		log.Fatalf("gowallpaper-gui: %v", err)
	}
}
