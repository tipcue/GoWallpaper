//go:build windows

// Command gowallpaper-gui is a lightweight Windows GUI for GoWallpaper.
// It lets the user browse a folder of video files and apply one as a live
// desktop wallpaper, with buttons to play, stop, and cycle through videos.
//
// Settings (last folder, last video, scale mode, FPS, loop) are persisted to
// %APPDATA%\GoWallpaper\config.json so they survive restarts.
//
// The wallpaper engine (GLFW + OpenGL) runs on a dedicated OS thread while
// the fyne GUI runs on the main goroutine.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/tipcue/GoWallpaper/internal/engine"
)

// videoExtensions is the set of container formats supported by FFmpeg builds.
var videoExtensions = map[string]bool{
	".mp4":  true,
	".webm": true,
	".mov":  true,
	".mkv":  true,
	".avi":  true,
}

func main() {
	cfgPath := engine.DefaultAppDataConfigPath()

	// Load existing config or start with defaults.
	cfg, err := engine.LoadConfig(cfgPath)
	if err != nil {
		cfg = &engine.Config{
			Mode:     "cover",
			Loop:     true,
			FPSLimit: 30,
		}
	}

	a := fyneapp.NewWithID("io.github.tipcue.gowallpaper")
	w := a.NewWindow("GoWallpaper")
	w.Resize(fyne.NewSize(500, 440))
	w.SetFixedSize(true)

	// ── Mutable state ────────────────────────────────────────────────────────
	var (
		eng        engine.Engine
		videoFiles []string
		selectedID = -1
	)

	// ── Widgets (declared early so closures can reference them) ──────────────
	statusLabel := widget.NewLabel("Status: Stopped")
	statusLabel.Wrapping = fyne.TextTruncate

	folderLabel := widget.NewLabel("No folder selected")
	folderLabel.Wrapping = fyne.TextTruncate

	fileList := widget.NewList(
		func() int { return len(videoFiles) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*widget.Label).SetText(filepath.Base(videoFiles[id]))
		},
	)

	var (
		applyBtn *widget.Button
		stopBtn  *widget.Button
		prevBtn  *widget.Button
		nextBtn  *widget.Button
	)

	// ── Helpers ──────────────────────────────────────────────────────────────

	// updateButtons enables/disables controls based on current state.
	updateButtons := func() {
		running := eng.Running()
		hasSelected := selectedID >= 0 && selectedID < len(videoFiles)

		if running {
			applyBtn.Disable()
			stopBtn.Enable()
		} else {
			if hasSelected {
				applyBtn.Enable()
			} else {
				applyBtn.Disable()
			}
			stopBtn.Disable()
		}

		if len(videoFiles) > 0 {
			prevBtn.Enable()
			nextBtn.Enable()
		} else {
			prevBtn.Disable()
			nextBtn.Disable()
		}
	}

	// scanFolder populates videoFiles from the given directory.
	scanFolder := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		videoFiles = nil
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if videoExtensions[strings.ToLower(filepath.Ext(e.Name()))] {
				videoFiles = append(videoFiles, filepath.Join(dir, e.Name()))
			}
		}
		sort.Strings(videoFiles)
		selectedID = -1
		fileList.Refresh()
		updateButtons()
	}

	// startVideo stops any running engine and starts it with the given path.
	startVideo := func(path string) {
		c := *cfg // copy to avoid aliasing
		c.VideoPath = path
		statusLabel.SetText("Starting…")
		if err := eng.Start(&c); err != nil {
			statusLabel.SetText("Error: " + err.Error())
			dialog.ShowError(err, w)
			updateButtons()
			return
		}
		statusLabel.SetText("Playing: " + filepath.Base(path))
		// Persist the chosen video and folder.
		cfg.VideoPath = path
		cfg.LastDir = filepath.Dir(path)
		if saveErr := engine.SaveConfig(cfg, cfgPath); saveErr != nil {
			log.Printf("gui: save config: %v", saveErr)
		}
		updateButtons()
	}

	// ── Buttons ──────────────────────────────────────────────────────────────

	applyBtn = widget.NewButtonWithIcon("Apply", theme.MediaPlayIcon(), func() {
		if selectedID < 0 || selectedID >= len(videoFiles) {
			return
		}
		startVideo(videoFiles[selectedID])
	})

	stopBtn = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), func() {
		eng.Stop()
		statusLabel.SetText("Status: Stopped")
		updateButtons()
	})

	prevBtn = widget.NewButtonWithIcon("Prev", theme.NavigateBackIcon(), func() {
		if len(videoFiles) == 0 {
			return
		}
		if selectedID <= 0 {
			selectedID = len(videoFiles) - 1
		} else {
			selectedID--
		}
		fileList.Select(widget.ListItemID(selectedID))
		if eng.Running() {
			startVideo(videoFiles[selectedID])
		}
	})

	nextBtn = widget.NewButtonWithIcon("Next", theme.NavigateNextIcon(), func() {
		if len(videoFiles) == 0 {
			return
		}
		selectedID = (selectedID + 1) % len(videoFiles)
		fileList.Select(widget.ListItemID(selectedID))
		if eng.Running() {
			startVideo(videoFiles[selectedID])
		}
	})

	fileList.OnSelected = func(id widget.ListItemID) {
		selectedID = int(id)
		updateButtons()
	}

	// ── Folder picker ────────────────────────────────────────────────────────
	pickFolderBtn := widget.NewButtonWithIcon("Choose Folder", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(lu fyne.ListableURI, err error) {
			if err != nil || lu == nil {
				return
			}
			dir := lu.Path()
			folderLabel.SetText(dir)
			cfg.LastDir = dir
			_ = engine.SaveConfig(cfg, cfgPath)
			scanFolder(dir)
		}, w)
	})

	// ── Settings ─────────────────────────────────────────────────────────────
	modeSelect := widget.NewSelect([]string{"cover", "contain", "stretch"}, func(s string) {
		cfg.Mode = s
		_ = engine.SaveConfig(cfg, cfgPath)
	})
	modeSelect.SetSelected(cfg.Mode)

	fpsEntry := widget.NewEntry()
	fpsEntry.SetText(strconv.Itoa(cfg.FPSLimit))
	fpsEntry.OnSubmitted = func(s string) {
		fps, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || fps <= 0 {
			fpsEntry.SetText(strconv.Itoa(cfg.FPSLimit))
			return
		}
		cfg.FPSLimit = fps
		_ = engine.SaveConfig(cfg, cfgPath)
	}

	loopCheck := widget.NewCheck("Loop video", func(checked bool) {
		cfg.Loop = checked
		_ = engine.SaveConfig(cfg, cfgPath)
	})
	loopCheck.SetChecked(cfg.Loop)

	// ── Layout ───────────────────────────────────────────────────────────────
	settings := container.NewGridWithColumns(4,
		widget.NewLabel("Scale:"), modeSelect,
		widget.NewLabel("FPS:"), fpsEntry,
	)

	controls := container.NewGridWithColumns(4, applyBtn, stopBtn, prevBtn, nextBtn)

	header := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("Folder:"), pickFolderBtn, folderLabel),
		settings,
		loopCheck,
		controls,
		statusLabel,
		widget.NewSeparator(),
	)

	// Restore last folder/selection from config.
	if cfg.LastDir != "" {
		folderLabel.SetText(cfg.LastDir)
		scanFolder(cfg.LastDir)
		if cfg.VideoPath != "" {
			for i, f := range videoFiles {
				if f == cfg.VideoPath {
					selectedID = i
					fileList.Select(widget.ListItemID(i))
					break
				}
			}
		}
	}
	updateButtons()

	// OnStop is called from a background goroutine when the engine exits for any
	// reason (natural video end, Stop button, or init error).
	// Fyne widget methods are goroutine-safe so we update the UI directly.
	// The Apply button is only re-enabled if there is a valid selection;
	// the click handler also guards this, so enabling it is always safe.
	eng.OnStop = func() {
		statusLabel.SetText("Status: Stopped")
		stopBtn.Disable()
		// Re-enable Apply only when a file is selected. selectedID is written
		// exclusively from the fyne event goroutine, so reading it here is safe
		// on all supported platforms (int read is atomic on amd64/arm64).
		if selectedID >= 0 {
			applyBtn.Enable()
		}
	}

	w.SetContent(container.NewBorder(header, nil, nil, nil, fileList))

	w.SetCloseIntercept(func() {
		eng.Stop()
		w.Close()
	})

	fmt.Printf("GoWallpaper GUI starting. Config: %s\n", cfgPath)
	w.ShowAndRun()
}
