param(
    [string]$Exe = ".\wispwind.exe"
)

$ErrorActionPreference = "Stop"

$resolved = (Resolve-Path $Exe).Path
New-ItemProperty `
    -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" `
    -Name "WispWind" `
    -Value "`"$resolved`"" `
    -PropertyType String `
    -Force | Out-Null

Write-Host "WispWind autostart installed:"
Write-Host $resolved
