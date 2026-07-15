# Captures a mullion window to a PNG.
#
# Finds the window by class name rather than by process id: a launcher (go run,
# a wrapper script) may not be the process that owns the window, so the pid you
# started is not necessarily the pid you want.
#
#   pwsh scripts/screenshot.ps1 -Name restored
#   pwsh scripts/screenshot.ps1 -Name maximised -ClassName MullionWindow
#
# It needs an interactive desktop, so it is not part of CI or the test suite.

param(
    [string]$Name = "window",
    [string]$ClassName = "MullionWindow",
    [string]$OutDir = "docs/images",
    [int]$Margin = 24
)

$ErrorActionPreference = "Stop"

Add-Type -AssemblyName System.Drawing
# The Win32 interop lives in screenshot.cs, next to this script, so it reads and
# edits as C#: source in another language is never written inline
# (CONTRIBUTING.md, Code style).
Add-Type -TypeDefinition (Get-Content -Raw (Join-Path $PSScriptRoot "screenshot.cs"))

# Without this the window rectangle comes back in virtualised coordinates on a
# scaled monitor and the crop lands somewhere else on the screen.
[void][Win32]::SetProcessDPIAware()

$hwnd = [Win32]::FindByClass($ClassName)
if ($hwnd -eq [IntPtr]::Zero) {
    throw "No visible window with class '$ClassName'. Start the demo first: cd examples/basic; go run ."
}

[void][Win32]::SetForegroundWindow($hwnd)
Start-Sleep -Milliseconds 500

$rect = New-Object Win32+RECT
if (-not [Win32]::GetWindowRect($hwnd, [ref]$rect)) {
    throw "GetWindowRect failed for class '$ClassName'."
}

# A margin around the window keeps the drop shadow and the rounded corners in
# frame; a tight crop looks clipped and hides exactly the DWM details a frame
# change is most likely to break.
$x = $rect.Left - $Margin
$y = $rect.Top - $Margin
$width = ($rect.Right - $rect.Left) + (2 * $Margin)
$height = ($rect.Bottom - $rect.Top) + (2 * $Margin)

$bitmap = New-Object System.Drawing.Bitmap $width, $height
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.CopyFromScreen($x, $y, 0, 0, $bitmap.Size)

if (-not (Test-Path $OutDir)) {
    New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
}
# .NET resolves a relative path against the process working directory, which is
# not PowerShell's current location. Save() would silently write somewhere else,
# or fail as it does here, so hand it an absolute path.
$path = Join-Path (Resolve-Path $OutDir) "$Name.png"
$bitmap.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)

$graphics.Dispose()
$bitmap.Dispose()

Write-Output "saved $path ($width x $height)"
