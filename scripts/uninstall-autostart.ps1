$ErrorActionPreference = "Stop"

Remove-ItemProperty `
    -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" `
    -Name "WispWind" `
    -ErrorAction SilentlyContinue

Write-Host "WispWind autostart removed."
