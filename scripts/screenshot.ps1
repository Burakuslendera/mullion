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
    [int]$Margin = 24,
    # Crop this much larger than the window overall, half on each side, so the
    # aspect ratio is preserved (10 = a 10% wider and taller crop). 0 keeps the
    # fixed pixel -Margin above.
    [double]$MarginPercent = 0,
    # Cover the desktop with a solid neutral grey while capturing, so the margin
    # around the window carries no desktop content into a published image. The
    # backdrop lives only for the duration of the capture and closes itself.
    [switch]$Backdrop
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

# SetWindowPos insert-after handles and flags, for the backdrop z-order below.
$HWND_TOPMOST = [IntPtr]::new(-1)
$HWND_NOTOPMOST = [IntPtr]::new(-2)
$SWP_NOMOVE_NOSIZE_NOACTIVATE = 0x13  # SWP_NOSIZE | SWP_NOMOVE | SWP_NOACTIVATE

$backdropForm = $null
if ($Backdrop) {
    Add-Type -AssemblyName System.Windows.Forms
    $backdropForm = New-Object System.Windows.Forms.Form
    $backdropForm.Text = "mullion-backdrop"
    $backdropForm.FormBorderStyle = [System.Windows.Forms.FormBorderStyle]::None
    # Cover the target's monitor completely - its full bounds, not the work
    # area, which a maximised window would stop short of wherever an appbar or
    # the taskbar sits.
    $screen = [System.Windows.Forms.Screen]::FromHandle($hwnd)
    $backdropForm.StartPosition = [System.Windows.Forms.FormStartPosition]::Manual
    $backdropForm.Bounds = $screen.Bounds
    $backdropForm.BackColor = [System.Drawing.Color]::FromArgb(43, 45, 52)
    $backdropForm.ShowInTaskbar = $false
    $backdropForm.TopMost = $true
    $backdropForm.Show()
}

try {
    if ($backdropForm) {
        # Let the form finish painting, then raise the target INTO the topmost
        # band above it. SetForegroundWindow alone is not reliable here - the
        # foreground-steal restriction can refuse it (see
        # docs/lessons-and-dead-ends.md section 4); an explicit z-order via
        # SetWindowPos is deterministic and needs no focus at all.
        for ($i = 0; $i -lt 6; $i++) {
            [System.Windows.Forms.Application]::DoEvents()
            Start-Sleep -Milliseconds 50
        }
        if (-not [Win32]::SetWindowPos($hwnd, $HWND_TOPMOST, 0, 0, 0, 0, $SWP_NOMOVE_NOSIZE_NOACTIVATE)) {
            throw "SetWindowPos failed to raise the window above the backdrop."
        }
    }

    [void][Win32]::SetForegroundWindow($hwnd)
    if ($backdropForm) {
        # Keep pumping through the settle delay: the form has no message loop
        # of its own, and an unpumped window can be ghosted by the shell
        # mid-capture.
        for ($i = 0; $i -lt 10; $i++) {
            [System.Windows.Forms.Application]::DoEvents()
            Start-Sleep -Milliseconds 50
        }
    } else {
        Start-Sleep -Milliseconds 500
    }

    $rect = New-Object Win32+RECT
    if (-not [Win32]::GetWindowRect($hwnd, [ref]$rect)) {
        throw "GetWindowRect failed for class '$ClassName'."
    }

    # A margin around the window keeps the drop shadow and the rounded corners in
    # frame; a tight crop looks clipped and hides exactly the DWM details a frame
    # change is most likely to break.
    $marginX = $Margin
    $marginY = $Margin
    if ($MarginPercent -gt 0) {
        $marginX = [int](($rect.Right - $rect.Left) * $MarginPercent / 200)
        $marginY = [int](($rect.Bottom - $rect.Top) * $MarginPercent / 200)
    }
    $x = $rect.Left - $marginX
    $y = $rect.Top - $marginY
    $width = ($rect.Right - $rect.Left) + (2 * $marginX)
    $height = ($rect.Bottom - $rect.Top) + (2 * $marginY)

    $bitmap = New-Object System.Drawing.Bitmap $width, $height
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    $graphics.CopyFromScreen($x, $y, 0, 0, $bitmap.Size)

    if (-not (Test-Path $OutDir)) {
        New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
    }
    # .NET resolves a relative path against the process working directory, which
    # is not PowerShell's current location. Save() would silently write somewhere
    # else, or fail as it does here, so hand it an absolute path.
    $path = Join-Path (Resolve-Path $OutDir) "$Name.png"
    $bitmap.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)

    $graphics.Dispose()
    $bitmap.Dispose()

    Write-Output "saved $path ($width x $height)"
} finally {
    # The backdrop never outlives the capture, whether it succeeded or threw -
    # and the window is put back where it was found, out of the topmost band.
    if ($backdropForm) {
        [void][Win32]::SetWindowPos($hwnd, $HWND_NOTOPMOST, 0, 0, 0, 0, $SWP_NOMOVE_NOSIZE_NOACTIVATE)
        $backdropForm.Close()
        $backdropForm.Dispose()
    }
}
