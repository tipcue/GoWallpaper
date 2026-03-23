param(
    [string]$ExePath = "livewallpaper.exe",
    [string]$ObjdumpPath = "D:/MSYS2/ucrt64/bin/objdump.exe",
    [string[]]$SearchDirs = @("D:/MSYS2/mingw64/bin", "D:/MSYS2/ucrt64/bin")
)

$ErrorActionPreference = "Stop"

if (!(Test-Path $ExePath)) {
    throw "Executable not found: $ExePath"
}
if (!(Test-Path $ObjdumpPath)) {
    throw "objdump not found: $ObjdumpPath"
}

$exeFull = (Resolve-Path $ExePath).Path
$targetDir = Split-Path -Parent $exeFull
$stampPath = Join-Path $targetDir ".bundle-stamp"

$systemDlls = @{
    "advapi32.dll" = $true
    "bcrypt.dll" = $true
    "comdlg32.dll" = $true
    "combase.dll" = $true
    "gdi32.dll" = $true
    "imm32.dll" = $true
    "kernel32.dll" = $true
    "msvcrt.dll" = $true
    "ntdll.dll" = $true
    "ole32.dll" = $true
    "opengl32.dll" = $true
    "rpcrt4.dll" = $true
    "secur32.dll" = $true
    "shell32.dll" = $true
    "user32.dll" = $true
    "winmm.dll" = $true
    "ws2_32.dll" = $true
    "wsock32.dll" = $true
    "iphlpapi.dll" = $true
    "gdiplus.dll" = $true
    "dnsapi.dll" = $true
    "shlwapi.dll" = $true
    "crypt32.dll" = $true
    "dwrite.dll" = $true
    "usp10.dll" = $true
    "bcryptprimitives.dll" = $true
    "ncrypt.dll" = $true
    "userenv.dll" = $true
    "msimg32.dll" = $true
    "api-ms-win-core-synch-l1-2-0.dll" = $true
}

$exeStamp = (Get-Item $exeFull).LastWriteTimeUtc.Ticks
if (Test-Path $stampPath) {
    $previousStamp = Get-Content $stampPath -ErrorAction SilentlyContinue
    if ($previousStamp -eq "$exeStamp") {
        Write-Host "Runtime DLL bundle is up to date; skipping scan."
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
$warnedMissing = New-Object System.Collections.Generic.HashSet[string]

$queue.Enqueue($exeFull)
$seen.Add($exeFull) | Out-Null

while ($queue.Count -gt 0) {
    $current = $queue.Dequeue()
    $imports = Get-Imports $current

    foreach ($dll in $imports) {
        if ($systemDlls.ContainsKey($dll)) {
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
        if (Test-Path $dest) {
            $srcTime = (Get-Item $resolved).LastWriteTimeUtc
            $dstTime = (Get-Item $dest).LastWriteTimeUtc
            if ($dstTime -ge $srcTime) {
                $needCopy = $false
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

Set-Content -Path $stampPath -Value "$exeStamp"
Write-Host "Bundled DLL count:" $copied.Count
Write-Host "Output directory:" $targetDir
