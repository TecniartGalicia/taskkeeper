# Captures the "[Extension Development Host]" VS Code window to PNG frames at 5 fps
# until src/test/demo drops stop.flag. Window-only capture (System.Drawing), so
# nothing of the real desktop leaks in. Mirrors the recipe used for the other
# Argalla extensions.
param(
  [string]$Out = "$env:TEMP\tk-demo-out",
  [int]$Width = 1440,
  [int]$Height = 860,
  [int]$Fps = 5
)
Add-Type -AssemblyName System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class Win {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool MoveWindow(IntPtr h,int x,int y,int w,int t,bool r);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h,int c);
}
"@

$frames = Join-Path $Out 'frames'
New-Item -ItemType Directory -Force -Path $frames | Out-Null
Get-ChildItem $frames -Filter *.png -ErrorAction SilentlyContinue | Remove-Item -Force
$stop = Join-Path $Out 'stop.flag'
if (Test-Path $stop) { Remove-Item $stop -Force }

# Wait for the dev-host window.
$proc = $null
$deadline = (Get-Date).AddSeconds(60)
while ((Get-Date) -lt $deadline) {
  $proc = Get-Process | Where-Object { $_.MainWindowTitle -like '*Extension Development Host*' } | Select-Object -First 1
  if ($proc) { break }
  Start-Sleep -Milliseconds 300
}
if (-not $proc) { Write-Error 'dev-host window not found'; exit 1 }

$h = $proc.MainWindowHandle
[Win]::ShowWindow($h, 9) | Out-Null   # SW_RESTORE
[Win]::MoveWindow($h, 60, 40, $Width, $Height, $true) | Out-Null
[Win]::SetForegroundWindow($h) | Out-Null
Start-Sleep -Milliseconds 500

$i = 0
$interval = [int](1000 / $Fps)
$bmp = New-Object System.Drawing.Bitmap $Width, $Height
$gfx = [System.Drawing.Graphics]::FromImage($bmp)
$endBy = (Get-Date).AddSeconds(60)
while (-not (Test-Path $stop) -and (Get-Date) -lt $endBy) {
  $gfx.CopyFromScreen(60, 40, 0, 0, (New-Object System.Drawing.Size $Width, $Height))
  $bmp.Save((Join-Path $frames ("f{0:D5}.png" -f $i)))
  $i++
  Start-Sleep -Milliseconds $interval
}
$gfx.Dispose(); $bmp.Dispose()
Write-Host "captured $i frames to $frames"
