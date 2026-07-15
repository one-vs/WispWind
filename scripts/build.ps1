param(
    [string]$Output = "wispwind.exe",
    [switch]$Windowed
)

$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

$mingwRoots = @("C:\msys64\mingw64", "$env:USERPROFILE\scoop\apps\msys2\current\mingw64")
foreach ($root in $mingwRoots) {
    if (Test-Path "$root\bin\gcc.exe") {
        $env:PATH = "$root\bin;$env:PATH"
        $env:PKG_CONFIG_PATH = "$root\lib\pkgconfig"
        break
    }
}

$env:CGO_ENABLED = "1"

$extldflags = "-static -static-libgcc -static-libstdc++ -lstdc++ -lwinmm -lole32 -lsetupapi -luuid -lpropsys"
$ldflags = "-s -w -extldflags `"$extldflags`""
if ($Windowed) {
    $ldflags = "-H=windowsgui $ldflags"
}

$args = @("build", "-ldflags", $ldflags, "-o", $Output, "cmd/app/main.go")
& go @args
