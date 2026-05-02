param(
    [string]$Output = "wispwind.exe",
    [switch]$Windowed
)

$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

$mingwBin = "C:\msys64\mingw64\bin"
if (Test-Path $mingwBin) {
    $env:PATH = "$mingwBin;$env:PATH"
    $env:PKG_CONFIG_PATH = "C:\msys64\mingw64\lib\pkgconfig"
}

$env:CGO_ENABLED = "1"

$extldflags = "-static -static-libgcc -static-libstdc++ -lwinmm -lole32 -lsetupapi -luuid -lpropsys"
$ldflags = "-s -w -extldflags `"$extldflags`""
if ($Windowed) {
    $ldflags = "-H=windowsgui $ldflags"
}

$args = @("build", "-ldflags", $ldflags, "-o", $Output, "cmd/app/main.go")
& go @args
