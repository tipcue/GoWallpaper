# GoWallpaper Build Script
# Usage: ./build.ps1 [target]
# Targets: build (default), cli, gui, test, run, clean, all

param(
    [string]$Target = "build"
)

# Set environment variables for CGO and FFmpeg
$env:CGO_ENABLED = 1
$env:CC = "D:/MSYS2/ucrt64/bin/x86_64-w64-mingw32-gcc.exe"
$env:PKG_CONFIG_PATH = "D:/MSYS2/mingw64/lib/pkgconfig"

Write-Host "=== GoWallpaper Build Script ===" -ForegroundColor Cyan
Write-Host "Target: $Target" -ForegroundColor Yellow
Write-Host "Working directory: $(Get-Location)" -ForegroundColor Gray

switch ($Target.ToLower()) {
    "build" {
        Write-Host "`n[1/3] Building CLI (livewallpaper)..." -ForegroundColor Green
        go build -o livewallpaper.exe ./cmd/livewallpaper
        if ($LASTEXITCODE -eq 0) {
            Write-Host "[OK] livewallpaper.exe built successfully" -ForegroundColor Green
        } else {
            Write-Host "[FAILED] Build failed" -ForegroundColor Red
            exit 1
        }

        Write-Host "`n[2/3] Building GUI (gowallpaper-gui)..." -ForegroundColor Green
        go build -o gowallpaper-gui.exe ./cmd/gowallpaper-gui
        if ($LASTEXITCODE -eq 0) {
            Write-Host "[OK] gowallpaper-gui.exe built successfully" -ForegroundColor Green
        } else {
            Write-Host "[FAILED] Build failed" -ForegroundColor Red
            exit 1
        }

        Write-Host "`n[3/3] Building diagnostic tool..." -ForegroundColor Green
        go build -o ffmpeg-diag.exe ./cmd/ffmpeg-diag
        if ($LASTEXITCODE -eq 0) {
            Write-Host "[OK] ffmpeg-diag.exe built successfully" -ForegroundColor Green
        } else {
            Write-Host "[FAILED] Build failed" -ForegroundColor Red
            exit 1
        }
        
        Write-Host "`n[OK] All builds completed!`n" -ForegroundColor Green
    }

    "cli" {
        Write-Host "`nBuilding CLI (livewallpaper)..." -ForegroundColor Green
        go build -o livewallpaper.exe ./cmd/livewallpaper
        if ($LASTEXITCODE -eq 0) {
            Write-Host "[OK] livewallpaper.exe built successfully" -ForegroundColor Green
        } else {
            Write-Host "[FAILED] Build failed" -ForegroundColor Red
            exit 1
        }
    }

    "gui" {
        Write-Host "`nBuilding GUI (gowallpaper-gui)..." -ForegroundColor Green
        go build -o gowallpaper-gui.exe ./cmd/gowallpaper-gui
        if ($LASTEXITCODE -eq 0) {
            Write-Host "[OK] gowallpaper-gui.exe built successfully" -ForegroundColor Green
        } else {
            Write-Host "[FAILED] Build failed" -ForegroundColor Red
            exit 1
        }
    }

    "test" {
        Write-Host "`nRunning unit tests..." -ForegroundColor Green
        Write-Host "Note: Tests require video file assets/sample.mp4" -ForegroundColor Yellow
        
        Write-Host "`nTesting video module..." -ForegroundColor Cyan
        go test -v ./internal/video/...
        
        Write-Host "`nTesting win module..." -ForegroundColor Cyan
        go test -v ./internal/win/...
        
        Write-Host "`nTesting render module..." -ForegroundColor Cyan
        go test -v ./internal/render/gl/...
        
        Write-Host "`n[OK] Unit tests completed!`n" -ForegroundColor Green
    }

    "run" {
        Write-Host "`nRunning livewallpaper.exe..." -ForegroundColor Green
        if (!(Test-Path "livewallpaper.exe")) {
            Write-Host "[ERROR] livewallpaper.exe not found, run build first" -ForegroundColor Red
            exit 1
        }
        .\livewallpaper.exe
    }

    "run-diag" {
        Write-Host "`nRunning FFmpeg diagnostic tool..." -ForegroundColor Green
        if (!(Test-Path "ffmpeg-diag.exe")) {
            Write-Host "[ERROR] ffmpeg-diag.exe not found, run build first" -ForegroundColor Red
            exit 1
        }
        $videoPath = Get-ChildItem "assets/*.mp4" -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty FullName
        if ($videoPath) {
            .\ffmpeg-diag.exe -video "$videoPath" -frames 5
        } else {
            Write-Host "[ERROR] No MP4 video found in assets/" -ForegroundColor Red
            exit 1
        }
    }

    "clean" {
        Write-Host "`nCleaning build artifacts..." -ForegroundColor Green
        Remove-Item -Force -ErrorAction SilentlyContinue livewallpaper.exe
        Remove-Item -Force -ErrorAction SilentlyContinue gowallpaper-gui.exe
        Remove-Item -Force -ErrorAction SilentlyContinue ffmpeg-diag.exe
        go clean ./cmd/...
        Write-Host "[OK] Cleanup completed!`n" -ForegroundColor Green
    }

    "all" {
        Write-Host "`nRunning full build sequence..." -ForegroundColor Green
        & $PSCommandPath -Target "clean"
        & $PSCommandPath -Target "build"
        & $PSCommandPath -Target "test"
    }

    default {
        Write-Host "Usage: ./build.ps1 [target]" -ForegroundColor Yellow
        Write-Host "Available targets:"
        Write-Host "  build      - Build CLI, GUI, and diagnostic tool (default)"
        Write-Host "  cli        - Build CLI only (livewallpaper.exe)"
        Write-Host "  gui        - Build GUI only (gowallpaper-gui.exe)"
        Write-Host "  test       - Run unit tests"
        Write-Host "  run        - Run livewallpaper"
        Write-Host "  run-diag   - Run FFmpeg diagnostic tool"
        Write-Host "  clean      - Clean build artifacts"
        Write-Host "  all        - Run full build sequence"
        Write-Host ""
        Write-Host "Examples:"
        Write-Host "  ./build.ps1          # Build all"
        Write-Host "  ./build.ps1 gui      # Build GUI only"
        Write-Host "  ./build.ps1 cli      # Build CLI only"
        Write-Host "  ./build.ps1 test     # Run tests"
        Write-Host "  ./build.ps1 run      # Run program"
    }
}
