# GoWallpaper

一个清晰可实施的 Windows 视频壁纸（Live Wallpaper）项目。  
A clear, implementable Windows live wallpaper project that plays video files behind the desktop icons.

---

## Features

- Plays `.mp4`, `.webm`, `.mov`, `.mkv`, `.avi` files as live wallpapers via the WorkerW trick.
- Scale modes: `cover` (default), `contain`, `stretch`.
- Configurable FPS limit and loop support.
- **CLI** for scripted / headless use.
- **GUI** (Windows) with folder picker, video list, and playback controls.

---

## Requirements

- Windows 10 / 11 (64-bit)
- [MSYS2](https://www.msys2.org/) with the UCRT64 toolchain
- FFmpeg development libraries (via MSYS2 `pacman`)

```powershell
pacman -S mingw-w64-ucrt-x86_64-ffmpeg
```

---

## Build

Set the required environment variables (adjust paths to match your MSYS2 installation):

```powershell
$env:CGO_ENABLED = 1
$env:CC          = "C:\msys64\ucrt64\bin\gcc.exe"
$env:PKG_CONFIG_PATH = "C:\msys64\ucrt64\lib\pkgconfig"
```

### CLI

```powershell
go build -o livewallpaper.exe ./cmd/livewallpaper
```

### GUI

```powershell
go build -o gowallpaper-gui.exe ./cmd/gowallpaper-gui
```

Or use the provided build script:

```powershell
.\build.ps1          # builds both CLI and GUI
.\build.ps1 gui      # builds GUI only
.\build.ps1 cli      # builds CLI only
```

---

## CLI Usage

```
livewallpaper [-config <path>]
```

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `<exe dir>/assets/config.json` | Path to the JSON config file |

**`assets/config.json` example:**

```json
{
  "videoPath": "sample.mp4",
  "mode": "cover",
  "loop": true,
  "fpsLimit": 30
}
```

`videoPath` is resolved relative to the config file's directory when it is not absolute.

---

## GUI Usage

Run `gowallpaper-gui.exe`.

1. Click **Choose Folder** to open a folder containing video files.
2. Select a video from the list.
3. Click **Apply** to start the live wallpaper.
4. Use **Prev** / **Next** to cycle through videos while playing.
5. Click **Stop** to remove the live wallpaper.

Settings (last folder, last video, scale mode, FPS limit, loop) are automatically
saved to `%APPDATA%\GoWallpaper\config.json`.

---

## Architecture

```
cmd/
  livewallpaper/        CLI entrypoint
  gowallpaper-gui/      GUI entrypoint (fyne v2)
internal/
  engine/
    config.go           Shared Config struct; load/save helpers
    engine.go           Engine type (Start/Stop) + Run (for CLI)
  video/                FFmpeg video decoder (CGo)
  render/gl/            OpenGL renderer
  win/                  WorkerW desktop integration
  app/                  Single-instance mutex
assets/
  config.json           Default CLI config (edit to taste)
```

The wallpaper engine (GLFW + OpenGL) runs on a dedicated OS thread
(`runtime.LockOSThread`) so it can coexist with the fyne GUI on the main thread.

---

## Memory Notes

- Frame buffers are **reused** across calls (single `[]byte` allocation per decoder).
- The GPU texture is **updated in-place** (`TexSubImage2D`) rather than re-allocated.
- Geometry recalculation is **lazy** — only re-done when frame or window dimensions change.
