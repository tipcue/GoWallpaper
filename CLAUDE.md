# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

GoWallpaper is a Windows-only live wallpaper tool that plays video files behind the desktop icons using the WorkerW trick. It is written in Go, uses CGo for FFmpeg decoding, OpenGL 4.1 for rendering, and GLFW for windowing. Builds produce both a CLI (`livewallpaper.exe`) and a fyne-based GUI (`gowallpaper-gui.exe`).

All source files use `//go:build windows` — there is no cross-platform path; do not try to make code build on Linux/macOS.

## Build & Test

CGO is mandatory and must be pointed at an MSYS2 UCRT64 toolchain. The `build.ps1` script derives `CC` and `PKG_CONFIG_PATH` from `$env:MSYS2_PREFIX` (default `D:/MSYS2`) and **forces** the ucrt64 `bin/` directory to the front of `PATH` (not merely "if missing"). That matters because Git for Windows also ships a `mingw64/bin` with colliding runtime DLLs (`libgcc_s_seh-1.dll`, `libwinpthread-1.dll`, …); if Git wins the loader search, `go test` binaries die at load time with `exit status 0xc0000139`. Keep `CC` and `PKG_CONFIG_PATH` on the same environment (ucrt64) — do not mix ucrt64 gcc with mingw64 pkg-config. Override the MSYS2 root by setting `$env:MSYS2_PREFIX` (e.g. `C:\msys64`) before invoking the script. Run from PowerShell:

```powershell
.\build.ps1              # build CLI + GUI + ffmpeg-diag (default target)
.\build.ps1 cli          # build livewallpaper.exe only
.\build.ps1 gui          # build gowallpaper-gui.exe only
.\build.ps1 test         # run unit tests for video, win, render/gl
.\build.ps1 run          # run livewallpaper.exe
.\build.ps1 run-diag     # run ffmpeg-diag with first mp4 in assets/
.\build.ps1 clean        # remove *.exe and go clean
.\build.ps1 all          # clean + build + test
```

For manual builds, set env first (values must match your MSYS2 install):
```powershell
$env:CGO_ENABLED = 1
$env:CC = "D:/MSYS2/ucrt64/bin/x86_64-w64-mingw32-gcc.exe"
$env:PKG_CONFIG_PATH = "D:/MSYS2/ucrt64/lib/pkgconfig"
$env:PATH = "D:/MSYS2/ucrt64/bin;$env:PATH"   # must be first — before Git's mingw64/bin
go build -o livewallpaper.exe ./cmd/livewallpaper
go build -o gowallpaper-gui.exe ./cmd/gowallpaper-gui
```

FFmpeg dev libraries are required: `pacman -S mingw-w64-ucrt-x86_64-ffmpeg`.

Run a single package test: `go test -v ./internal/video/...` (tests need `assets/sample.mp4`).

## Architecture

### Threading model (critical)
GLFW/OpenGL **must** run on a dedicated OS thread (`runtime.LockOSThread`).
- **CLI** (`cmd/livewallpaper/main.go`): locks the main thread in `init()` and calls `engine.Run` synchronously.
- **GUI** (`cmd/gowallpaper-gui/main.go`): fyne runs on the main goroutine; `engine.Engine.Start` spawns a goroutine that locks its own OS thread for the render loop. Never call GLFW or GL from the fyne event goroutine.

### Package layout (`internal/`)
- `engine/` — `Engine` type with `Start/Stop/Running` (uses mutex + cancel ctx + done channel). `Run` is the synchronous entry used by the CLI. `ParseScaleMode` maps config strings to GL scale constants. `config.go` defines `Config` (JSON: `videoPath`, `mode`, `loop`, `fpsLimit`, `lastDir`) with `LoadConfig`/`SaveConfig`; `DefaultAppDataConfigPath()` returns `%APPDATA%\GoWallpaper\config.json` (the GUI's persisted state).
- `video/` — CGo FFmpeg decoder (`ffmpeg.go`, `frame.go`). Reuses a single frame buffer per decoder; no per-frame allocation.
- `render/gl/` — OpenGL renderer (`renderer.go`) + shaders (`shader.go`). Updates texture in-place via `TexSubImage2D`. Scale modes: `ScaleCover` / `ScaleContain` / `ScaleStretch`. Geometry recomputation is lazy (only when frame or window dims change).
- `win/` — WorkerW desktop integration (`workerw.go`) + `hwnd_glfw.go` (extracts HWND from a glfw.Window). Uses raw `user32.dll` syscalls (no cgo wrapper). Calls: `FindOrCreateWorkerW`, `SetParentToWorkerW`, `MakeFullscreen`, `PlaceAtBottom`, `MoveToOrigin`, `ApplyChildStyle`.
- `app/singleton.go` — single-instance mutex (named `GoWallpaper`).

### Render pipeline (`runEngine` in `internal/engine/engine.go`)
video.Open → glfw.Init → glfw.CreateWindow (hidden, fullscreen-sized, decorations off) → get HWND → find/create WorkerW → reparent window → style as WS_CHILD + WS_EX_NOACTIVATE + WS_EX_TRANSPARENT → Fullscreen + PlaceAtBottom → gl.Init → renderer.New → frame loop: ReadFrame → Upload → Draw → SwapBuffers → PollEvents. FPS throttled via `time.Sleep` when `FPSLimit > 0`; `io.EOF` triggers `dec.Seek()` when `Loop=true`.

### Engine concurrency
`Engine.Start` blocks until init completes (returns init error via `initResult` channel). A separate cleanup goroutine watches `done` so `OnStop` fires whether the engine exits naturally, via `Stop()`, or from init failure. `Running()` is accurate even if the render goroutine exited naturally between calls — it non-blocking-checks `done` and clears state if closed. Do not change this three-goroutine structure casually; bugs here caused stale Running() state and missed OnStop in commit `de06b1e`.

### Config resolution difference
- **CLI** resolves `videoPath` relative to the **config file's directory**.
- **GUI** stores absolute paths only (folder picker → `filepath.Join(dir, name)`) — no resolution needed.
This asymmetry is intentional; do not unify without reading both entrypoints.

### Diagnostic commands (`cmd/diagnose-*`, `cmd/ffmpeg-diag`)
Standalone debugging tools — `diagnose-progman`, `diagnose-workerw`, `diagnose-all` inspect the WorkerW/Progman hierarchy; `ffmpeg-diag` decodes N frames from a video to verify CGo/FFmpeg setup. Built by `build.ps1` (ffmpeg-diag) or manually via `go build ./cmd/<name>`.

## Conventions

- RTK prefix rule does **not** apply to Go tooling (`go build`, `go test`) — use them directly. Only apply `rtk` to high-output commands (`git log`, `git diff`, `docker logs`) per the global RTK guide.
- Windows API constants and procs in `internal/win/` are kept as package-level `var` (typed `int32`/`int64` to avoid uintptr overflow at compile time). Follow the same pattern when adding new user32 calls.
- Logs: CLI writes to `livewallpaper.log` + stderr; GUI logs via stdlib `log`. Both `*.log` files are gitignored.
- `assets/config.json` is the CLI default config (committed); `assets/config.local.json` is gitignored for local overrides.
