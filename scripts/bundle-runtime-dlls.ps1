# Copy non-system DLL dependencies next to a GoWallpaper binary so it can run
# on machines without MSYS2 on PATH.
#
# Usage:
#   .\scripts\bundle-runtime-dlls.ps1
#   .\scripts\bundle-runtime-dlls.ps1 -ExePath gowallpaper-gui.exe
#   .\scripts\bundle-runtime-dlls.ps1 -Force
#   $env:MSYS2_PREFIX = "C:\msys64"; .\scripts\bundle-runtime-dlls.ps1
#
# Paths default from $env:MSYS2_PREFIX (same convention as build.ps1). Search
# order is ucrt64 first — do not put mingw64 ahead of it, or you risk mixing
# CRT/runtime DLLs with a ucrt64-linked binary.

param(
    [string]$ExePath = "livewallpaper.exe",
    [string]$ObjdumpPath = "",
    [string[]]$SearchDirs = @(),
    [switch]$Force
)

$ErrorActionPreference = "Stop"

if (-not $env:MSYS2_PREFIX) { $env:MSYS2_PREFIX = "D:/MSYS2" }
$prefix = $env:MSYS2_PREFIX.TrimEnd('\', '/')

if (-not $ObjdumpPath) {
    $ObjdumpPath = "$prefix/ucrt64/bin/objdump.exe"
}
if ($SearchDirs.Count -eq 0) {
    # ucrt64 only by default — matches build.ps1's CC/PKG_CONFIG_PATH.
    $SearchDirs = @("$prefix/ucrt64/bin")
}

if (!(Test-Path $ExePath)) {
    throw "Executable not found: $ExePath"
}
if (!(Test-Path $ObjdumpPath)) {
    throw "objdump not found: $ObjdumpPath (set `$env:MSYS2_PREFIX or -ObjdumpPath)"
}

$exeFull = (Resolve-Path $ExePath).Path
$targetDir = Split-Path -Parent $exeFull
# Per-exe stamp so bundling multiple binaries in one dir does not thrash.
$stampPath = Join-Path $targetDir (".bundle-stamp." + [IO.Path]::GetFileName($exeFull))

# OS / UCRT forwarders that must come from the target machine, not MSYS2.
# Also skip the entire api-ms-win-* family (UCRT API-set stubs).
$systemDlls = @{
    "advapi32.dll" = $true
    "bcrypt.dll" = $true
    "bcryptprimitives.dll" = $true
    "combase.dll" = $true
    "comdlg32.dll" = $true
    "crypt32.dll" = $true
    "dnsapi.dll" = $true
    "dwrite.dll" = $true
    "gdi32.dll" = $true
    "gdi32full.dll" = $true
    "gdiplus.dll" = $true
    "imm32.dll" = $true
    "iphlpapi.dll" = $true
    "kernel32.dll" = $true
    "kernelbase.dll" = $true
    "msimg32.dll" = $true
    "msvcrt.dll" = $true
    "ncrypt.dll" = $true
    "ntdll.dll" = $true
    "ole32.dll" = $true
    "oleaut32.dll" = $true
    "opengl32.dll" = $true
    "rpcrt4.dll" = $true
    "sechost.dll" = $true
    "secur32.dll" = $true
    "setupapi.dll" = $true
    "shell32.dll" = $true
    "shlwapi.dll" = $true
    "ucrtbase.dll" = $true
    "user32.dll" = $true
    "userenv.dll" = $true
    "usp10.dll" = $true
    "uxtheme.dll" = $true
    "version.dll" = $true
    "win32u.dll" = $true
    "winmm.dll" = $true
    "ws2_32.dll" = $true
    "wsock32.dll" = $true
}

function Test-SystemDll([string]$name) {
    if ($systemDlls.ContainsKey($name)) { return $true }
    if ($name.StartsWith("api-ms-win-")) { return $true }
    if ($name.StartsWith("ext-ms-")) { return $true }
    return $false
}

$exeStamp = (Get-Item $exeFull).LastWriteTimeUtc.Ticks
$searchKey = ($SearchDirs | ForEach-Object { $_.TrimEnd('\', '/').ToLowerInvariant() }) -join "|"
$stampValue = "$exeStamp|$searchKey"
if (-not $Force -and (Test-Path $stampPath)) {
    $previousStamp = Get-Content $stampPath -ErrorAction SilentlyContinue
    if ($previousStamp -eq $stampValue) {
        Write-Host "Runtime DLL bundle is up to date; skipping scan. (use -Force to rescan)"
        Write-Host "Output directory:" $targetDir
        exit 0
    }
}

function Get-Imports([string]$binPath) {
    $lines = & $ObjdumpPath -p $binPath | Select-String "DLL Name:"
    $dlls = @()
    foreach ($line in $lines) {
        $name = ($line.Line -replace ".*DLL Name:\s*", "").Trim()
        if ($name) { $dlls += $name.ToLowerInvariant() }
    }
    return $dlls
}

function Resolve-DllPath([string]$dllName) {
    foreach ($dir in $SearchDirs) {
        $candidate = Join-Path $dir $dllName
        if (Test-Path $candidate) {
            return (Resolve-Path $candidate).Path
        }
    }
    return $null
}

$queue = New-Object System.Collections.Generic.Queue[string]
$seen = New-Object System.Collections.Generic.HashSet[string]
$copied = New-Object System.Collections.Generic.HashSet[string]
$skippedFresh = 0
$warnedMissing = New-Object System.Collections.Generic.HashSet[string]

$queue.Enqueue($exeFull)
$seen.Add($exeFull) | Out-Null

Write-Host "Bundling runtime DLLs for:" $exeFull
Write-Host "Search dirs:" ($SearchDirs -join "; ")

while ($queue.Count -gt 0) {
    $current = $queue.Dequeue()
    $imports = Get-Imports $current

    foreach ($dll in $imports) {
        if (Test-SystemDll $dll) {
            continue
        }

        $resolved = Resolve-DllPath $dll
        if (-not $resolved) {
            if (-not $warnedMissing.Contains($dll)) {
                Write-Warning "Missing dependency in search dirs: $dll"
                $warnedMissing.Add($dll) | Out-Null
            }
            continue
        }

        $dest = Join-Path $targetDir $dll
        $needCopy = $true
        if (-not $Force -and (Test-Path $dest)) {
            $srcTime = (Get-Item $resolved).LastWriteTimeUtc
            $dstTime = (Get-Item $dest).LastWriteTimeUtc
            if ($dstTime -ge $srcTime) {
                $needCopy = $false
                $skippedFresh++
            }
        }

        if ($needCopy) {
            Copy-Item $resolved $dest -Force
            $copied.Add($dll) | Out-Null
        }

        if (-not $seen.Contains($resolved)) {
            $seen.Add($resolved) | Out-Null
            $queue.Enqueue($resolved)
        }
    }
}

Set-Content -Path $stampPath -Value $stampValue
Write-Host "Copied:" $copied.Count " Already fresh:" $skippedFresh " Missing:" $warnedMissing.Count
Write-Host "Output directory:" $targetDir
if ($warnedMissing.Count -gt 0) {
    exit 1
}
