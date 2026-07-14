# Collects the environment a window bug report needs, and prints it as a block
# to paste into an issue.
#
#   pwsh scripts/diagnostics.ps1
#
# Nothing here is sent anywhere. It prints to the console, and you decide what to
# paste. It reads no file of yours and no personal data: Windows build, GPU,
# monitors, WebView2 runtime, Go toolchain, and the mullion version if this is run
# from a checkout.
#
# The monitor section is the reason this script exists. Windows lies to a process
# that is not DPI-aware: on a 150% monitor it reports a virtualised resolution,
# which is exactly the number a DPI bug report must not contain. So the process
# declares per-monitor awareness before it measures anything.

$ErrorActionPreference = "Stop"

Add-Type @"
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;

public static class Displays {
    [DllImport("user32.dll")]
    public static extern bool SetProcessDpiAwarenessContext(IntPtr value);

    [DllImport("user32.dll")]
    public static extern bool EnumDisplayMonitors(IntPtr hdc, IntPtr rect, MonitorProc proc, IntPtr data);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    public static extern bool GetMonitorInfoW(IntPtr monitor, ref MONITORINFOEX info);

    [DllImport("shcore.dll")]
    public static extern int GetDpiForMonitor(IntPtr monitor, int type, out uint x, out uint y);

    public delegate bool MonitorProc(IntPtr monitor, IntPtr hdc, IntPtr rect, IntPtr data);

    [StructLayout(LayoutKind.Sequential)]
    public struct RECT { public int Left, Top, Right, Bottom; }

    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    public struct MONITORINFOEX {
        public int Size;
        public RECT Monitor;
        public RECT Work;
        public uint Flags;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)] public string Device;
    }

    public class Info {
        public string Device;
        public int Width, Height, Left, Top;
        public int WorkWidth, WorkHeight;
        public uint Dpi;
        public bool Primary;
    }

    // PER_MONITOR_AWARE_V2. Without it every rectangle below is a virtualised
    // approximation, and a report of "1536x864" from a 1920x1080 monitor at 125%
    // sends the reader looking for a bug that is not there.
    public static void MakeDpiAware() {
        SetProcessDpiAwarenessContext(new IntPtr(-4));
    }

    public static List<Info> All() {
        List<Info> found = new List<Info>();
        EnumDisplayMonitors(IntPtr.Zero, IntPtr.Zero, delegate(IntPtr monitor, IntPtr hdc, IntPtr rect, IntPtr data) {
            MONITORINFOEX info = new MONITORINFOEX();
            info.Size = Marshal.SizeOf(typeof(MONITORINFOEX));
            if (!GetMonitorInfoW(monitor, ref info)) return true;

            uint dpiX = 96, dpiY = 96;
            GetDpiForMonitor(monitor, 0, out dpiX, out dpiY);  // 0 = EFFECTIVE

            Info item = new Info();
            item.Device = info.Device;
            item.Left = info.Monitor.Left;
            item.Top = info.Monitor.Top;
            item.Width = info.Monitor.Right - info.Monitor.Left;
            item.Height = info.Monitor.Bottom - info.Monitor.Top;
            item.WorkWidth = info.Work.Right - info.Work.Left;
            item.WorkHeight = info.Work.Bottom - info.Work.Top;
            item.Dpi = dpiX;
            item.Primary = (info.Flags & 1) != 0;   // MONITORINFOF_PRIMARY
            found.Add(item);
            return true;
        }, IntPtr.Zero);
        return found;
    }
}
"@

[Displays]::MakeDpiAware()

function Get-WebView2Version {
    $guid = "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
    $keys = @(
        "HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\$guid",
        "HKLM:\SOFTWARE\Microsoft\EdgeUpdate\Clients\$guid",
        "HKCU:\SOFTWARE\Microsoft\EdgeUpdate\Clients\$guid"
    )
    foreach ($key in $keys) {
        try {
            $value = (Get-ItemProperty -Path $key -ErrorAction Stop).pv
            if ($value) { return $value }
        } catch {}
    }
    return "not found"
}

function Get-Gpus {
    # Get-CimInstance is the obvious call and it is not reliable everywhere: on
    # some PowerShell installations (notably the Store build) the CIM layer is
    # broken and throws. The registry holds the same information, so failing over
    # to it means the script still produces a useful report rather than a stack
    # trace on the machine of the person trying to help you.
    #
    # Results are collected into a list and returned once, at the end. A pipeline
    # that emits and then throws leaves its output in the function's result even
    # though the catch ran, so the naive version reports the adapters AND the
    # failure message - which reads as a bug in the reporter's machine.
    $found = @()

    try {
        foreach ($adapter in (Get-CimInstance -ClassName Win32_VideoController -ErrorAction Stop)) {
            $driver = if ($adapter.DriverVersion) { $adapter.DriverVersion } else { "unknown" }
            $found += "$($adapter.Name) (driver $driver)"
        }
    } catch {
        $found = @()
    }
    if ($found.Count -gt 0) { return $found }

    # SilentlyContinue, not Stop. This key has subkeys the current user cannot
    # read, and under a Stop preference one of them aborts the whole enumeration -
    # so the adapters that were readable are lost along with the one that was not.
    # Skip what cannot be read; report what can.
    $class = "HKLM:\SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}"
    foreach ($key in @(Get-ChildItem $class -ErrorAction SilentlyContinue)) {
        $item = Get-ItemProperty $key.PSPath -ErrorAction SilentlyContinue
        if (-not $item -or -not $item.DriverDesc) { continue }
        $driver = if ($item.DriverVersion) { $item.DriverVersion } else { "unknown" }
        $found += "$($item.DriverDesc) (driver $driver)"
    }
    if ($found.Count -gt 0) { return $found }

    return @("could not be read")
}

function Get-MullionVersion {
    # Only works from a checkout. A consumer running a released build does not
    # have the source, and does not need this: the library logs its own version at
    # startup, read out of the binary's build info, which is authoritative in a way
    # that a script guessing from a directory never is.
    $root = Split-Path -Parent $PSScriptRoot
    if (-not (Test-Path (Join-Path $root "go.mod"))) { return $null }
    try {
        $sha = (git -C $root rev-parse --short HEAD 2>$null)
        $dirty = (git -C $root status --porcelain 2>$null)
        if ($sha) {
            $tag = (git -C $root describe --tags --exact-match HEAD 2>$null)
            $label = if ($tag) { "$tag ($sha)" } else { $sha }
            # A modified tree means the revision names a commit the running code
            # is not. Saying so is the difference between a reproducible report
            # and a false one.
            if ($dirty) { $label = "$label, modified" }
            return $label
        }
    } catch {}
    return "source checkout, no commit"
}

$os = [System.Environment]::OSVersion.Version

# ProductName still reads "Windows 10" on Windows 11 - Microsoft never changed the
# registry value. Reporting it verbatim sends the reader to the wrong Windows, and
# on a frame or snap bug that is a wasted day, because the two shells behave
# differently. The build number is the honest signal: 22000 and above is 11.
$displayVersion = ""
$edition = "Windows"
try {
    $current = Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion"
    $displayVersion = $current.DisplayVersion
    $edition = $current.ProductName
    if ($os.Build -ge 22000) {
        $edition = $edition -replace "^Windows 10", "Windows 11"
    }
} catch {}

$goVersion = try { (go version) } catch { "not installed" }
$mullion = Get-MullionVersion

Write-Output '```'
Write-Output "Windows:   $edition $displayVersion (build $($os.Build).$($os.Revision))"
Write-Output "Arch:      $env:PROCESSOR_ARCHITECTURE"
Write-Output "WebView2:  $(Get-WebView2Version)"
if ($env:WEBVIEW2_BROWSER_EXECUTABLE_FOLDER) {
    Write-Output "           pinned by WEBVIEW2_BROWSER_EXECUTABLE_FOLDER"
}
Write-Output "Go:        $goVersion"
if ($mullion) {
    Write-Output "mullion:   $mullion (from a checkout)"
} else {
    Write-Output "mullion:   see the 'mullion: version=' line in the log"
}

Write-Output ""
foreach ($gpu in (Get-Gpus)) {
    Write-Output "GPU:       $gpu"
}

Write-Output ""
$monitors = [Displays]::All()
Write-Output "Monitors:  $($monitors.Count)"
$index = 1
foreach ($m in $monitors) {
    $scale = [math]::Round(($m.Dpi / 96.0) * 100)
    $primary = if ($m.Primary) { ", primary" } else { "" }
    Write-Output ("  [$index] $($m.Width)x$($m.Height) at $scale% (dpi $($m.Dpi)), " +
                  "origin $($m.Left),$($m.Top), work area $($m.WorkWidth)x$($m.WorkHeight)$primary")
    $index++
}
Write-Output '```'
Write-Output ""
Write-Output "Measured with per-monitor DPI awareness, so the resolutions above are physical."
