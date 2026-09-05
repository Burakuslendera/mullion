# Scans the selected stage-0 index blobs and safe present worktree revisions,
# plus reachable commit messages, for the configured private-data shapes. Exit 0
# means every declared source in that bounded union was read and checked.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$isWindowsHost = [Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT

if ($null -eq ("MullionLeakScanPathIdentity" -as [type]) -and $isWindowsHost) {
    Add-Type @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Text;
using Microsoft.Win32.SafeHandles;

public static class MullionLeakScanPathIdentity {
    const uint FileReadAttributes = 0x80;
    const uint FileShareRead = 0x1;
    const uint FileShareWrite = 0x2;
    const uint FileShareDelete = 0x4;
    const uint OpenExisting = 3;
    const uint FileFlagBackupSemantics = 0x02000000;
    const uint FileFlagOpenReparsePoint = 0x00200000;

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    static extern SafeFileHandle CreateFileW(string name, uint access, uint share,
        IntPtr securityAttributes, uint creation, uint flags, IntPtr templateFile);

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    static extern uint GetFinalPathNameByHandleW(SafeFileHandle handle,
        StringBuilder path, uint capacity, uint flags);

    public static string Get(string path) {
        return Get(path, false);
    }

    public static string Get(string path, bool openReparsePoint) {
        uint flags = FileFlagBackupSemantics;
        if (openReparsePoint) {
            flags |= FileFlagOpenReparsePoint;
        }
        using (SafeFileHandle handle = CreateFileW(path, FileReadAttributes,
            FileShareRead | FileShareWrite | FileShareDelete, IntPtr.Zero,
            OpenExisting, flags, IntPtr.Zero)) {
            if (handle.IsInvalid) {
                throw new Win32Exception(Marshal.GetLastWin32Error());
            }
            StringBuilder result = new StringBuilder(32768);
            uint length = GetFinalPathNameByHandleW(handle, result,
                (uint)result.Capacity, 0);
            if (length == 0 || length >= result.Capacity) {
                throw new Win32Exception(Marshal.GetLastWin32Error());
            }
            return result.ToString();
        }
    }
}
'@
}

# Detector families stay separate because they have different false-positive
# controls. The drive rule protects configured identifying roots; it is not a
# claim that every absolute drive path is private. UNC rules protect machine/share
# identity and distinguish ordinary from extended syntax. Separator runs are
# intentional: the published bytes may be a runtime path or a source-escaped one.
# The ordinary UNC lookbehind rejects both a scheme colon and a preceding
# separator. Without both, regex retry can restart inside https:////host/share
# and misclassify a URL spelling as a machine/share disclosure.
$patterns = @(
    @{ Name = "upstream product name"; Pattern = "token" + "pilor" }
    @{ Name = "upstream product name"; Pattern = "co" + "dex" }
    @{ Name = "third-party webview binding"; Pattern = "wa" + "ils" }
    @{ Name = "sensitive Windows drive path"; Pattern = '(?i)(?<![A-Za-z0-9])[A-Z]:[\\/]+(?:Users|Documents and Settings|dev|devTools)[\\/]+(?<user>(?:[^<>:"/\\|?*\x00-\x1F\r\n]+(?=[\\/])|[A-Za-z0-9 ._~''<>@!#$%&()+,;=\[\]^{}-]+))' }
    @{ Name = "extended UNC host"; Pattern = '(?i)[\\/]{2,}\?[\\/]+UNC[\\/]+(?<host>[A-Za-z0-9._<>-]+)[\\/]+(?<share>(?:[^<>:"/\\|?*\x00-\x1F\r\n]+(?=[\\/])|[A-Za-z0-9$._<>@!#%&()+,;=\[\]^{}~-]+))' }
    @{ Name = "UNC host"; Pattern = '(?i)(?<![:\\/])[\\/]{2,}(?![?.][\\/])(?<host>[A-Za-z0-9._<>-]+)[\\/]+(?<share>(?:[^<>:"/\\|?*\x00-\x1F\r\n]+(?=[\\/])|[A-Za-z0-9$._<>@!#%&()+,;=\[\]^{}~-]+))' }
    @{ Name = "artefact hash"; Pattern = "\b[0-9a-fA-F]{40,64}\b" }
    @{ Name = "agent signature"; Pattern = ("Duze" + "nleyen|Son Gun" + "celleme") }
    @{ Name = "executable name"; Pattern = "\w+\.exe\b" }
    @{ Name = "pseudo-version with a real-looking commit hash"; Pattern = "v0\.0\.0-\d{14}-(?!abcdef" + "123456\b)[0-9a-f]{12}" }
    @{ Name = "commit trailer in a file"; Pattern = "Co-Auth" + "ored-By" }
)
$sourceRule = @{ Name = "non-ASCII character in source"; Pattern = "[^\x00-\x7F]" }
$sourceExtensions = @(
    ".go", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".css", ".html",
    ".htm", ".json", ".svg", ".cs", ".ps1", ".yml", ".yaml", ".mod", ""
)
$binaryExtensions = @(".png")

# There is no basename or whole-file skip. Issue #108 showed that Split-Path
# -Leaf turns an exemption into cross-tree authority: a nested file can inherit a
# name it did not earn. Each allowance therefore binds one exact Git path,
# detector family, exact synthetic capture or named component, and expected count.
# Workflow action hashes additionally bind the complete `uses:` line: a pin copied
# to unrelated text cannot satisfy the allowance. Normal allowances are evaluated
# per distinct byte revision; history allowances activate only from their exact
# stage-0 index anchor. Counts make changed fixtures loud.
$checkoutPin = "3d3c42e5aac5ba805825" + "da76410c181273ba90b1"
$setupGoPin = "b7ad1dad31e06c5925ef" + "5d2fc7ad053ef454303e"
$windows10EvidenceCommit = "2a20cffb0dfdd4dc6b3af" + "028eed5f63e4955b1af"
$verificationCutoverHead = "3a281ab20f660f401e2c" + "ca437b67e6cc5e613e48"
$win11ToWin10ArtifactHash = "5A9B807B7B809F666B2B3AD11D851" + "8B896B079EC3B5515317046B0796A424F00"
$win10ToWin11ArtifactHash = "A6B15AD5DAE3D2BFDD0B5FC0D295" + "2A02234636AC71FA552CBAE379BD39B51860"
$windowsArtifactSuffix = "amd64v1" + ".exe"
$consumerArtifactName = "app" + ".exe"
$issue135EvidenceManifestHash = "B5E6DA1688FAEEB5EBCE4A2B2B7FF0FF" + "8B6BC8C3050C9B0990D8B6DAFEC13C66"
$issue135Head = "f7860ae8804b27954bf3" + "3708d16a92797b4d66f0"
$issue135TrackedDiffHash = "409586B47D3D530C7A7FA816288E1851A" + "828858E4F52E857BF4A223FEFE26332"
$issue135AggregateIdentity = "4914E61763B02D073E7DB79771E0151B9" + "3BB6F2102C9F87C8A0CD91C130F76F9"
$issue135UntrackedManifestHash = "00FD27577869D8A26D94DA65A2C2FC2A" + "FE6810EDA04A720CC1150B68F859BCFF"
$issue135UntaggedArtifactHash = "D7D79A86A64124349F28D833E6AB4AD6" + "53E612F407C8E837836B7C5871197754"
$issue135TaggedArtifactHash = "A0F5D1314A041DFAA5FAD2D01382F032" + "3CBEC39EA8E71FEE27D037A3C80B4769"
$issue135ManualLogHash = "0EBF09387CD75986FEDFF1985ED35E0BE" + "4EEE65B24B85DD8A7846D1F4E47AF72"
$issue135RightEdgeLogHash = "1E7C90B35D797AC4751ED3599B7AB2DF" + "CDC23CBA88A9020BB1E564A08BF20E2A"
$issue135TaggedLogHash = "E8EA9CA6732A4FA226EE176FF6A0CEFF" + "D58B368C0BD5E80109966C516AD5E8D4"
$issue135UntaggedExecutable = "untagged" + ".exe"
$issue135TaggedExecutable = "tagged" + ".exe"
$allowances = @(
    [pscustomobject]@{ Path = ".github/workflows/ci.yml"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($checkoutPin) + "$"); Action = "actions/checkout"; Expected = 3 }
    [pscustomobject]@{ Path = ".github/workflows/ci.yml"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($setupGoPin) + "$"); Action = "actions/setup-go"; Expected = 3 }
    # These recorded compatibility identifiers are public evidence, not an
    # exclusion: each allowance is confined to one documented capture and count.
    # d6b1081 moved the 2026-08 evidence under
    # docs/verification/records/2026-08.md; the allowances follow the archived evidence.
    [pscustomobject]@{ Path = "docs/verification/records/2026-08.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($windows10EvidenceCommit) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/2026-09.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($verificationCutoverHead) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/2026-08.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($win11ToWin10ArtifactHash) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/2026-08.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($win10ToWin11ArtifactHash) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/2026-08.md"; Rule = "executable name"; Value = ("^" + [regex]::Escape($windowsArtifactSuffix) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/windows-10-compatibility.md"; Rule = "executable name"; Value = ("^" + [regex]::Escape($consumerArtifactName) + "$"); Expected = 4 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($issue135EvidenceManifestHash) + "$"); Expected = 2 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($issue135Head) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($issue135TrackedDiffHash) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($issue135AggregateIdentity) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($issue135UntrackedManifestHash) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($issue135UntaggedArtifactHash) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($issue135TaggedArtifactHash) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($issue135ManualLogHash) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($issue135RightEdgeLogHash) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($issue135TaggedLogHash) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "executable name"; Value = ("^" + [regex]::Escape($issue135UntaggedExecutable) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/verification/records/issues/issue-135-paired-live.md"; Rule = "executable name"; Value = ("^" + [regex]::Escape($issue135TaggedExecutable) + "$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/decisions/0025-urls-are-logged-as-urls.md"; Rule = "sensitive Windows drive path"; Value = ("^C:/Users/" + "alice$"); Expected = 1 }
    [pscustomobject]@{ Path = "docs/decisions/0028-message-keeps-the-urls-inside-it.md"; Rule = "sensitive Windows drive path"; Value = ("^C:/Users/" + "alice$"); Expected = 3 }
    [pscustomobject]@{ Path = "docs/guard-authority-details.md"; Rule = "UNC host"; Group = "host"; Value = '(?i)BUILD-NAS'; Expected = 1 }
    [pscustomobject]@{ Path = "host/diagnostics_windows_test.go"; Rule = "sensitive Windows drive path"; Value = '(?i)^C:[\\/]+Users[\\/]+Example User$'; Expected = 1 }
    [pscustomobject]@{ Path = "host/leak_scan_test.go"; Rule = "sensitive Windows drive path"; Value = '(?i)^C:[\\/]+Users[\\/]+private-user$'; Expected = 1 }
    [pscustomobject]@{ Path = "host/leak_scan_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)private-user'; Expected = 1 }
    [pscustomobject]@{ Path = "host/systembrowser_windows_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)(?:etc|attacker)'; Expected = 1 }
    [pscustomobject]@{ Path = "host/webview_windows_test.go"; Rule = "sensitive Windows drive path"; Value = '(?i)^C:[\\/]+Users[\\/]+jane$'; Expected = 1 }
    [pscustomobject]@{ Path = "host/webview_windows_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)jane'; Expected = 1 }
    [pscustomobject]@{ Path = "internal/doctor/architecture_gate_unsupported_windows_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)server'; Expected = 1 }
    [pscustomobject]@{ Path = "internal/doctor/probe_windows_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)server'; Expected = 1 }
    [pscustomobject]@{ Path = "internal/doctor/doctor_test.go"; Rule = "sensitive Windows drive path"; Value = '(?i)^C:[\\/]+Users[\\/]+(?:Example User|EXAMPL~1)$'; Expected = 13 }
    [pscustomobject]@{ Path = "internal/doctor/doctor_test.go"; Rule = "extended UNC host"; Group = "host"; Value = '(?i)(?:HOME-NAS|BUILD-NAS)'; Expected = 3 }
    [pscustomobject]@{ Path = "internal/doctor/doctor_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)(?:HOME-NAS|BUILD-NAS|rt)'; Expected = 17 }
    [pscustomobject]@{ Path = "internal/doctor/public_output.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)BUILD-NAS'; Expected = 1 }
    [pscustomobject]@{ Path = "internal/logsafe/logsafe_test.go"; Rule = "sensitive Windows drive path"; Value = "(?i)^C:[\\/]+Users[\\/]+(?:Example User|Alice O'Brien|D'Angelo|O'Brien|Ana O'Neil)$"; Expected = 7 }
    [pscustomobject]@{ Path = "internal/logsafe/logsafe_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)server'; Expected = 1 }
    [pscustomobject]@{ Path = "internal/logsafe/message_url_test.go"; Rule = "sensitive Windows drive path"; Value = '(?i)^C:[\\/]+Users[\\/]+alice$'; Expected = 4 }
    [pscustomobject]@{ Path = "internal/logsafe/message_url_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)FILESERVER'; Expected = 1 }
    [pscustomobject]@{ Path = "internal/logsafe/url.go"; Rule = "sensitive Windows drive path"; Value = ("^C:/Users/" + "alice$"); Expected = 2 }
    [pscustomobject]@{ Path = "internal/logsafe/url_test.go"; Rule = "sensitive Windows drive path"; Value = "(?i)^C:[\\/]+Users[\\/]+(?:\.\.\.|Alice O'Brien|alice)$"; Expected = 5 }
    [pscustomobject]@{ Path = "internal/logsafe/url_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)server'; Expected = 1 }
    [pscustomobject]@{ Path = "internal/webview2/loader_discovery_windows_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)BUILD-NAS'; Expected = 1 }
    [pscustomobject]@{ PathPattern = '^commit '; Anchor = "docs/decisions/0025-urls-are-logged-as-urls.md"; Rule = "sensitive Windows drive path"; Value = ("^C:/Users/" + "alice$"); Expected = 1 }
)

# All repository bytes enter the scanner through this byte-oriented process
# boundary. PowerShell's native-command pipeline is intentionally not used for
# Git manifests or blobs: it can decode, quote, or truncate NUL/binary data.
function Invoke-GitRaw([string[]]$Arguments, [byte[]]$InputBytes) {
    $psi = [System.Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = "git"
    $psi.WorkingDirectory = (Get-Location).Path
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    foreach ($argument in $Arguments) {
        [void]$psi.ArgumentList.Add($argument)
    }

    $process = [System.Diagnostics.Process]::new()
    $process.StartInfo = $psi
    try {
        if (-not $process.Start()) {
            throw "could not start Git"
        }
    } catch {
        throw "git $($Arguments -join ' ') could not start: $($_.Exception.Message)"
    }

    $stdout = [System.IO.MemoryStream]::new()
    $stderr = [System.IO.MemoryStream]::new()
    try {
        # Begin both reads before writing requests. This avoids a full pipe
        # deadlock when a repository has many or large objects.
        $stdoutTask = $process.StandardOutput.BaseStream.CopyToAsync($stdout)
        $stderrTask = $process.StandardError.BaseStream.CopyToAsync($stderr)
        if ($null -ne $InputBytes -and $InputBytes.Length -ne 0) {
            $process.StandardInput.BaseStream.Write($InputBytes, 0, $InputBytes.Length)
            $process.StandardInput.BaseStream.Flush()
        }
        $process.StandardInput.Close()
        $process.WaitForExit()
        $stdoutTask.GetAwaiter().GetResult()
        $stderrTask.GetAwaiter().GetResult()
        $stdoutBytes = $stdout.ToArray()
        $stderrBytes = $stderr.ToArray()
        if ($process.ExitCode -ne 0) {
            $detail = [System.Text.Encoding]::UTF8.GetString($stderrBytes).Trim()
            if ($detail -eq "") { $detail = "no Git diagnostic" }
            throw "git $($Arguments -join ' ') failed (exit $($process.ExitCode)): $detail"
        }
        return [pscustomobject]@{ Bytes = $stdoutBytes; ErrorBytes = $stderrBytes }
    } finally {
        $stdout.Dispose()
        $stderr.Dispose()
        $process.Dispose()
    }
}

function Read-StrictText($InputObject) {
    if ($null -eq $InputObject) {
        [byte[]]$bytes = [byte[]]::new(0)
    } elseif ($InputObject -is [string]) {
        $bytes = [System.IO.File]::ReadAllBytes($InputObject)
    } else {
        [byte[]]$bytes = $InputObject
    }
    if ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF) {
        return [System.Text.UTF8Encoding]::new($false, $true).GetString($bytes, 3, $bytes.Length - 3)
    }
    if ($bytes.Length -ge 4 -and
        (($bytes[0] -eq 0xFF -and $bytes[1] -eq 0xFE -and $bytes[2] -eq 0x00 -and $bytes[3] -eq 0x00) -or
         ($bytes[0] -eq 0x00 -and $bytes[1] -eq 0x00 -and $bytes[2] -eq 0xFE -and $bytes[3] -eq 0xFF))) {
        throw "unsupported UTF-32 byte order mark"
    }
    if ($bytes.Length -ge 2 -and $bytes[0] -eq 0xFF -and $bytes[1] -eq 0xFE) {
        return [System.Text.UnicodeEncoding]::new($false, $true, $true).GetString($bytes, 2, $bytes.Length - 2)
    }
    if ($bytes.Length -ge 2 -and $bytes[0] -eq 0xFE -and $bytes[1] -eq 0xFF) {
        return [System.Text.UnicodeEncoding]::new($true, $true, $true).GetString($bytes, 2, $bytes.Length - 2)
    }
    return [System.Text.UTF8Encoding]::new($false, $true).GetString($bytes)
}

function Read-StrictGitPath([byte[]]$Bytes) {
    if ($null -eq $Bytes) {
        throw "empty Git path"
    }
    try {
        # Unlike content decoding, a path's leading EF BB BF is data. Never
        # strip it as a text-file BOM.
        return [System.Text.UTF8Encoding]::new($false, $true).GetString($Bytes)
    } catch {
        throw "invalid UTF-8 Git path: $($_.Exception.Message)"
    }
}

function Copy-ByteRange([byte[]]$Bytes, [int]$Start, [int]$Length) {
    if ($Start -lt 0 -or $Length -lt 0 -or $Start -gt $Bytes.Length - $Length) {
        throw "invalid byte range"
    }
    $copy = [byte[]]::new($Length)
    if ($Length -ne 0) {
        [System.Buffer]::BlockCopy($Bytes, $Start, $copy, 0, $Length)
    }
    return ,$copy
}

function Find-Byte([byte[]]$Bytes, [int]$Start, [byte]$Wanted) {
    for ($index = $Start; $index -lt $Bytes.Length; $index++) {
        if ($Bytes[$index] -eq $Wanted) { return $index }
    }
    return -1
}

function Convert-BytesToHex([byte[]]$Bytes) {
    if ($null -eq $Bytes -or $Bytes.Length -eq 0) { return "" }
    return [Convert]::ToHexString($Bytes).ToLowerInvariant()
}

function Get-AsciiText([byte[]]$Bytes, [string]$Context) {
    foreach ($byte in $Bytes) {
        if ($byte -gt 0x7F) {
            throw "$Context contains non-ASCII protocol bytes"
        }
    }
    return [System.Text.Encoding]::ASCII.GetString($Bytes)
}

function Get-NulRecords([byte[]]$Bytes, [string]$Context) {
    $records = [System.Collections.Generic.List[byte[]]]::new()
    $start = 0
    for ($index = 0; $index -lt $Bytes.Length; $index++) {
        if ($Bytes[$index] -ne 0) { continue }
        if ($index -eq $start) {
            throw "$Context contains an empty NUL record"
        }
        [byte[]]$record = Copy-ByteRange $Bytes $start ($index - $start)
        [void]$records.Add($record)
        $start = $index + 1
    }
    if ($start -ne $Bytes.Length) {
        throw "$Context does not end with a NUL delimiter"
    }
    return [pscustomobject]@{ Records = $records.ToArray() }
}

function Get-GitText([string[]]$Arguments) {
    $result = Invoke-GitRaw $Arguments $null
    try {
        return Read-StrictText $result.Bytes
    } catch {
        throw "git $($Arguments -join ' ') produced invalid text: $($_.Exception.Message)"
    }
}

function Get-SingleGitText([string[]]$Arguments, [string]$Context) {
    $text = Get-GitText $Arguments
    if ($text -notmatch '^(?<value>[^\r\n]+)\r?\n?$') {
        throw "$Context returned malformed text"
    }
    return $Matches["value"]
}

function Test-Oid([string]$Oid) {
    return $Oid -match '^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$'
}

function Get-ValidatedGitPath([byte[]]$PathBytes, [string]$Context) {
    if ($PathBytes.Length -eq 0) { throw "$Context has an empty Git path" }
    $path = Read-StrictGitPath $PathBytes
    if ($path.IndexOf([char]0) -ge 0) { throw "$Context contains a NUL Git path" }
    if ($path.StartsWith("/") -or [System.IO.Path]::IsPathRooted($path)) {
        throw "$Context contains an absolute Git path"
    }
    $parts = $path.Split([char]"/")
    foreach ($part in $parts) {
        if ($part -eq "" -or $part -eq "." -or $part -eq "..") {
            throw "$Context contains an unsafe Git path '$path'"
        }
        if ($isWindowsHost) {
            if ($part.Contains("\") -or $part.Contains(":") -or
                $part.EndsWith(" ") -or $part.EndsWith(".")) {
                throw "$Context contains a Windows-ambiguous Git path '$path'"
            }
            foreach ($invalid in [System.IO.Path]::GetInvalidFileNameChars()) {
                if ($part.IndexOf($invalid) -ge 0) {
                    throw "$Context contains a Windows-ambiguous Git path '$path'"
                }
            }
        }
    }
    return [pscustomobject]@{
        Path = $path
        PathBytes = $PathBytes
        PathKey = Convert-BytesToHex $PathBytes
    }
}

function Get-LineNumber([string]$Text, [int]$Index) {
    if ($Index -eq 0) { return 1 }
    return 1 + [regex]::Matches($Text.Substring(0, $Index), "`n").Count
}

# Hash text is authorized only when it is the value of an executable `uses`
# mapping in a real top-level `steps` sequence. Physical-line matching is not
# authority: YAML block scalars can contain step-shaped text, and quoted mapping
# keys are executable even though a raw `uses:` prefix does not recognize them.
function Test-WorkflowActionPin([string]$Text, [System.Text.RegularExpressions.Match]$Match, [string]$Action) {
    $offset = 0
    $jobsIndent = -1
    $jobIndent = -1
    $jobsChildIndent = -1
    $jobFieldIndent = -1
    $stepsIndent = -1
    $stepIndent = -1
    $stepMappingIndent = -1
    $scalarParentIndent = -1
    $scalarContentIndent = -1

    foreach ($rawLine in [regex]::Split($Text, "(?<=`n)")) {
        $line = $rawLine.TrimEnd([char[]]"`r`n")
        $trimmed = $line.TrimStart()
        $indent = $line.Length - $trimmed.Length
        $matchOnLine = $Match.Index -ge $offset -and $Match.Index -lt $offset + $line.Length

        if ($scalarParentIndent -ge 0) {
            if ($trimmed -eq "") {
                $offset += $rawLine.Length
                continue
            }
            if ($scalarContentIndent -lt 0 -and $indent -gt $scalarParentIndent) {
                $scalarContentIndent = $indent
            }
            if ($scalarContentIndent -ge 0 -and $indent -ge $scalarContentIndent) {
                $offset += $rawLine.Length
                continue
            }
            $scalarParentIndent = -1
            $scalarContentIndent = -1
        }

        if ($trimmed -eq "" -or $trimmed.StartsWith("#")) {
            $offset += $rawLine.Length
            continue
        }

        $mapping = $trimmed
        $isSequence = $false
        $mappingIndent = $indent
        if ($mapping -match '^-\s+') {
            $isSequence = $true
            $mapping = $mapping.Substring($Matches[0].Length)
            $mappingIndent += $Matches[0].Length
        }
        $colon = $mapping.IndexOf(":")
        $key = if ($colon -ge 0) { $mapping.Substring(0, $colon).Trim().Trim('"').Trim("'") } else { "" }
        $value = if ($colon -ge 0) { $mapping.Substring($colon + 1).Trim() } else { "" }
        $value = [regex]::Replace($value, "\s+#.*$", "")

        if ($jobsIndent -ge 0 -and $indent -le $jobsIndent) {
            $jobsIndent = -1
            $jobsChildIndent = -1
            $jobIndent = -1
            $jobFieldIndent = -1
            $stepsIndent = -1
            $stepIndent = -1
            $stepMappingIndent = -1
        } elseif ($jobIndent -ge 0 -and $indent -le $jobIndent) {
            $jobIndent = -1
            $jobFieldIndent = -1
            $stepsIndent = -1
            $stepIndent = -1
            $stepMappingIndent = -1
        } elseif ($stepsIndent -ge 0 -and $indent -le $stepsIndent) {
            $stepsIndent = -1
            $stepIndent = -1
            $stepMappingIndent = -1
        }

        if ($indent -eq 0 -and -not $isSequence -and $key -ceq "jobs" -and $value -ceq "") {
            $jobsIndent = 0
            $jobsChildIndent = -1
        } elseif ($jobsIndent -ge 0 -and $indent -gt $jobsIndent) {
            if ($jobsChildIndent -lt 0) {
                $jobsChildIndent = $indent
            }
            if ($indent -eq $jobsChildIndent -and -not $isSequence -and $colon -ge 0 -and $value -ceq "") {
                $jobIndent = $indent
                $jobFieldIndent = -1
                $stepsIndent = -1
                $stepIndent = -1
                $stepMappingIndent = -1
            } elseif ($jobIndent -ge 0 -and $indent -gt $jobIndent) {
                if ($jobFieldIndent -lt 0) {
                    $jobFieldIndent = $indent
                }
                if ($indent -eq $jobFieldIndent -and -not $isSequence -and $key -ceq "steps" -and $value -ceq "") {
                    $stepsIndent = $indent
                    $stepIndent = -1
                    $stepMappingIndent = -1
                }
            }
        }

        $isStepMapping = $false
        if ($stepsIndent -ge 0 -and $indent -gt $stepsIndent) {
            if ($stepIndent -lt 0) {
                $stepIndent = $indent
            }
            if ($indent -eq $stepIndent -and $isSequence) {
                $stepMappingIndent = $mappingIndent
                $isStepMapping = $true
            } elseif ($stepIndent -ge 0 -and -not $isSequence -and $mappingIndent -eq $stepMappingIndent) {
                $isStepMapping = $true
            }
        }

        if ($matchOnLine) {
            if (-not $isStepMapping -or $key -cne "uses") {
                return $false
            }
            $token = $Action + "@" + $Match.Value
            $tokenIndex = $line.IndexOf($token, [System.StringComparison]::Ordinal)
            return $value -ceq $token -and
                $tokenIndex -ge 0 -and
                $Match.Index -eq $offset + $tokenIndex + $Action.Length + 1
        }

        if ($value -match '^[>|](?:[+-]?[1-9]?|[1-9][+-])$') {
            $scalarParentIndent = $indent
            $indicator = [regex]::Match($value, '[1-9]')
            $scalarContentIndent = if ($indicator.Success) { $indent + [int]$indicator.Value } else { -1 }
        }
        $offset += $rawLine.Length
    }
    return $false
}

function Get-Allowance([string]$File, [string]$Rule, [System.Text.RegularExpressions.Match]$Match, [string]$Text) {
    $sanitizedUser = $Rule -eq "sensitive Windows drive path" -and $Match.Groups["user"].Value -ceq "<user>"
    $sanitizedHost = ($Rule -eq "UNC host" -or $Rule -eq "extended UNC host") -and $Match.Groups["host"].Value -ceq "<host>"
    if ($sanitizedUser -or $sanitizedHost) {
        return [pscustomobject]@{ Sanitized = $true }
    }
    foreach ($allowance in $allowances) {
        # A commit allowance is activated only by its exact stage-0 index
        # anchor. HEAD and worktree copies must never make a history allowance
        # active after that anchor has been moved or deleted.
        if ($null -ne $allowance.Anchor -and -not $indexPathSet.Contains($allowance.Anchor)) {
            continue
        }
        $pathMatches = ($null -ne $allowance.Path -and [string]::Equals($allowance.Path, $File, [System.StringComparison]::Ordinal)) -or
            ($null -ne $allowance.PathPattern -and $File -cmatch $allowance.PathPattern)
        if (-not $pathMatches -or $allowance.Rule -cne $Rule) {
            continue
        }
        if ($null -ne $allowance.Action -and -not (Test-WorkflowActionPin $Text $Match $allowance.Action)) {
            continue
        }
        $candidate = $Match.Value
        if ($null -ne $allowance.Group) {
            $candidate = $Match.Groups[$allowance.Group].Value
        }
        $allowed = [regex]::Match($candidate, $allowance.Value)
        if ($allowed.Success -and $allowed.Index -eq 0 -and $allowed.Length -eq $candidate.Length) {
            return $allowance
        }
    }
    return $null
}

function Add-Matches(
    [System.Collections.ArrayList]$Findings,
    [string]$File,
    [string]$Text,
    $Rules,
    [string]$Source,
    [string]$RevisionId
) {
    foreach ($rule in $Rules) {
        foreach ($match in [regex]::Matches($Text, $rule.Pattern, [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)) {
            $allowance = Get-Allowance $File $rule.Name $match $Text
            if ($null -ne $allowance) {
                if ($allowance.Sanitized) {
                    continue
                }
                if (-not $allowance.RevisionCounts.ContainsKey($RevisionId)) {
                    $allowance.RevisionCounts[$RevisionId] = 0
                }
                $allowance.RevisionCounts[$RevisionId] = [int]$allowance.RevisionCounts[$RevisionId] + 1
                continue
            }
            [void]$Findings.Add([pscustomobject]@{
                File = $File
                Source = $Source
                Revision = $RevisionId
                Line = Get-LineNumber $Text $match.Index
                Rule = $rule.Name
                Match = $match.Value
            })
        }
    }
}

# Git path/object manifests are parsed as raw bytes. A decoded path is only a
# display/lookup spelling; PathKey remains the exact byte identity used by all
# source and allowance decisions.
function Get-GitManifest([byte[]]$Bytes, [string]$Kind) {
    $recordResult = Get-NulRecords $Bytes "$Kind manifest"
    $entries = [System.Collections.Generic.Dictionary[string,object]]::new([System.StringComparer]::Ordinal)
    $paths = [System.Collections.Generic.List[string]]::new()
    foreach ($record in $recordResult.Records) {
        $tab = Find-Byte $record 0 0x09
        if ($tab -le 0 -or $tab -ge $record.Length - 1) {
            throw "$Kind manifest contains a malformed record"
        }
        [byte[]]$metaBytes = Copy-ByteRange $record 0 $tab
        [byte[]]$pathBytes = Copy-ByteRange $record ($tab + 1) ($record.Length - $tab - 1)
        $meta = Get-AsciiText $metaBytes "$Kind manifest metadata"
        if ($Kind -eq "HEAD") {
            if ($meta -notmatch '^([0-7]{6}) ([a-z]+) ([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$') {
                throw "HEAD manifest contains a malformed entry"
            }
            $mode = $Matches[1]
            $type = $Matches[2]
            $oid = $Matches[3].ToLowerInvariant()
            if (($mode -eq "100644" -or $mode -eq "100755" -or $mode -eq "120000") -and $type -ne "blob") {
                throw "HEAD manifest has mode $mode with type $type"
            }
            if ($mode -eq "160000" -or $type -eq "commit") {
                throw "HEAD manifest contains a Gitlink"
            }
            if ($mode -eq "040000" -or $type -eq "tree") {
                throw "HEAD manifest contains a sparse directory/tree entry"
            }
            if ($mode -ne "100644" -and $mode -ne "100755" -and $mode -ne "120000") {
                throw "HEAD manifest contains unsupported mode $mode"
            }
        } else {
            if ($meta -notmatch '^([0-7]{6}) ([0-9a-fA-F]{40}|[0-9a-fA-F]{64}) ([0-9]+)$') {
                throw "index manifest contains a malformed entry"
            }
            $mode = $Matches[1]
            $oid = $Matches[2].ToLowerInvariant()
            $stage = $Matches[3]
            if ($stage -ne "0") {
                throw "index contains an unmerged/non-stage-0 entry"
            }
            if ($mode -eq "160000") {
                throw "index contains a Gitlink"
            }
            if ($mode -eq "040000") {
                throw "index contains a sparse directory entry"
            }
            if ($mode -ne "100644" -and $mode -ne "100755" -and $mode -ne "120000") {
                throw "index contains unsupported mode $mode"
            }
            $type = "blob"
        }
        if (-not (Test-Oid $oid)) {
            throw "$Kind manifest contains a malformed object ID"
        }
        $pathInfo = Get-ValidatedGitPath $pathBytes "$Kind manifest"
        if ($entries.ContainsKey($pathInfo.PathKey)) {
            throw "$Kind manifest contains a duplicate exact Git path"
        }
        $entry = [pscustomobject]@{
            Path = $pathInfo.Path
            PathBytes = $pathInfo.PathBytes
            PathKey = $pathInfo.PathKey
            Mode = $mode
            Type = $type
            Oid = $oid
        }
        $entries[$pathInfo.PathKey] = $entry
        [void]$paths.Add($pathInfo.Path)
    }
    return [pscustomobject]@{ Entries = $entries; Paths = $paths.ToArray() }
}

function Get-GitPathKeySet([byte[]]$Bytes, [string]$Context) {
    $keys = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::Ordinal)
    $paths = [System.Collections.Generic.Dictionary[string,string]]::new([System.StringComparer]::Ordinal)
    $records = (Get-NulRecords $Bytes $Context).Records
    foreach ($record in $records) {
        $pathInfo = Get-ValidatedGitPath $record $Context
        if ($keys.Contains($pathInfo.PathKey)) {
            throw "$Context contains a duplicate exact Git path"
        }
        [void]$keys.Add($pathInfo.PathKey)
        $paths[$pathInfo.PathKey] = $pathInfo.Path
    }
    return [pscustomobject]@{ Keys = $keys; Paths = $paths }
}

function Get-GitIndexFlags([byte[]]$Bytes) {
    $flags = [System.Collections.Generic.Dictionary[string,string]]::new([System.StringComparer]::Ordinal)
    $records = (Get-NulRecords $Bytes "index flags").Records
    foreach ($record in $records) {
        if ($record.Length -lt 3 -or $record[1] -ne 0x20) {
            throw "index flags contain a malformed record"
        }
        $flagByte = $record[0]
        if ($flagByte -ne [byte][char]"H" -and $flagByte -ne [byte][char]"S" -and
            $flagByte -ne [byte][char]"h" -and $flagByte -ne [byte][char]"s") {
            throw "index flags contain an unsupported or ambiguous entry state"
        }
        [byte[]]$pathBytes = Copy-ByteRange $record 2 ($record.Length - 2)
        $pathInfo = Get-ValidatedGitPath $pathBytes "index flags"
        if ($flags.ContainsKey($pathInfo.PathKey)) {
            throw "index flags contain a duplicate exact Git path"
        }
        $flags[$pathInfo.PathKey] = [char]$flagByte
    }
    return $flags
}

function Get-GitBlobMap([string[]]$Oids) {
    $blobs = [System.Collections.Generic.Dictionary[string,object]]::new([System.StringComparer]::Ordinal)
    if ($Oids.Count -eq 0) {
        return [pscustomobject]@{ Blobs = $blobs }
    }
    $requests = [System.Text.StringBuilder]::new()
    foreach ($oid in $Oids) {
        if (-not (Test-Oid $oid)) { throw "cannot request malformed Git object ID" }
        [void]$requests.Append($oid).Append("`n")
    }
    [byte[]]$requestBytes = [System.Text.Encoding]::ASCII.GetBytes($requests.ToString())
    $result = Invoke-GitRaw @("--no-replace-objects", "cat-file", "--batch") $requestBytes
    [byte[]]$output = $result.Bytes
    $offset = 0
    foreach ($oid in $Oids) {
        $lineEnd = Find-Byte $output $offset 0x0A
        if ($lineEnd -lt 0) { throw "git cat-file --batch returned a truncated header" }
        [byte[]]$headerBytes = Copy-ByteRange $output $offset ($lineEnd - $offset)
        $header = Get-AsciiText $headerBytes "git cat-file --batch header"
        if ($header -notmatch '^([0-9a-fA-F]{40}|[0-9a-fA-F]{64}) (blob|missing)(?: ([0-9]+))?$') {
            throw "git cat-file --batch returned a malformed header"
        }
        $returnedOid = $Matches[1].ToLowerInvariant()
        $type = $Matches[2]
        if ($returnedOid -cne $oid.ToLowerInvariant()) {
            throw "git cat-file --batch returned an unexpected object ID"
        }
        if ($type -ne "blob") {
            throw "Git object $oid is missing or is not a blob"
        }
        $size = 0L
        try {
            $size = [Int64]::Parse($Matches[3], [Globalization.CultureInfo]::InvariantCulture)
        } catch {
            throw "git cat-file --batch returned an invalid blob size"
        }
        if ($size -lt 0 -or $size -gt [Int32]::MaxValue) {
            throw "git cat-file --batch returned an unsupported blob size"
        }
        $bodyStart = $lineEnd + 1
        $bodyEnd = $bodyStart + [int]$size
        if ($bodyEnd -ge $output.Length -or $output[$bodyEnd] -ne 0x0A) {
            throw "git cat-file --batch returned a truncated blob"
        }
        [byte[]]$blobBytes = Copy-ByteRange $output $bodyStart ([int]$size)
        $blobs[$oid.ToLowerInvariant()] = [pscustomobject]@{ Bytes = $blobBytes }
        $offset = $bodyEnd + 1
    }
    if ($offset -ne $output.Length) {
        throw "git cat-file --batch returned trailing protocol bytes"
    }
    return [pscustomobject]@{ Blobs = $blobs }
}

function Get-GitExtension([string]$Path) {
    $slash = $Path.LastIndexOf("/")
    $name = if ($slash -ge 0) { $Path.Substring($slash + 1) } else { $Path }
    $dot = $name.LastIndexOf(".")
    if ($dot -le 0) { return "" }
    return $name.Substring($dot).ToLowerInvariant()
}

function Test-TextEntry([string]$Path, [string]$Mode) {
    if ($Mode -eq "120000") { return $true }
    return $binaryExtensions -notcontains (Get-GitExtension $Path)
}

function Test-ByteEqual([byte[]]$Left, [byte[]]$Right) {
    if ($null -eq $Left -or $null -eq $Right -or $Left.Length -ne $Right.Length) {
        return $false
    }
    for ($index = 0; $index -lt $Left.Length; $index++) {
        if ($Left[$index] -ne $Right[$index]) { return $false }
    }
    return $true
}

function Get-RevisionDigest([byte[]]$Bytes) {
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        return Convert-BytesToHex ($sha.ComputeHash($Bytes))
    } finally {
        $sha.Dispose()
    }
}

function Add-SourceRevision([System.Collections.ArrayList]$Revisions, $Record) {
    foreach ($revision in $Revisions) {
        if ($revision.PathKey -ceq $Record.PathKey -and (Test-ByteEqual $revision.Bytes $Record.Bytes)) {
            if ($revision.Sources -cnotcontains $Record.Source) {
                $revision.Sources = @($revision.Sources + $Record.Source)
            }
            if ($null -ne $Record.Oid -and $revision.Oids -cnotcontains $Record.Oid) {
                $revision.Oids = @($revision.Oids + $Record.Oid)
            }
            if ($revision.Modes -cnotcontains $Record.Mode) {
                $revision.Modes = @($revision.Modes + $Record.Mode)
            }
            $revision.TextEligible = $revision.TextEligible -or $Record.TextEligible
            return
        }
    }
    $digest = Get-RevisionDigest $Record.Bytes
    [void]$Revisions.Add([pscustomobject]@{
        Path = $Record.Path
        PathKey = $Record.PathKey
        Bytes = $Record.Bytes
        Digest = $digest
        RevisionId = $Record.PathKey + ":" + $digest
        Sources = @($Record.Source)
        Oids = if ($null -eq $Record.Oid) { @() } else { @($Record.Oid) }
        Modes = @($Record.Mode)
        TextEligible = [bool]$Record.TextEligible
    })
}

function Get-CanonicalFilesystemPath([string]$Path, [bool]$OpenReparsePoint = $false) {
    $fullPath = [System.IO.Path]::GetFullPath($Path)
    if ($fullPath.Length -gt 1) {
        $fullPath = if ($isWindowsHost) {
            $fullPath.TrimEnd([char[]]"\\/")
        } else {
            $fullPath.TrimEnd([char[]]"/")
        }
    }
    if (-not $isWindowsHost) {
        return $fullPath
    }
    try {
        return [MullionLeakScanPathIdentity]::Get($fullPath, $OpenReparsePoint).TrimEnd([char[]]"\\/")
    } catch {
        throw "cannot resolve filesystem identity '$Path': $($_.Exception.Message)"
    }
}

function Get-SafeWorktreeBytes($Entry, [System.Collections.Generic.Dictionary[string,object]]$IdentityMap) {
    $parts = $Entry.Path.Split([char]"/")
    $current = $root
    $item = $null
    $attributes = [System.IO.FileAttributes]0
    $isContainer = $false
    for ($partIndex = 0; $partIndex -lt $parts.Length; $partIndex++) {
        try {
            if ($isWindowsHost) {
                $current = Join-Path -Path $current -ChildPath $parts[$partIndex] -ErrorAction Stop
                $item = Get-Item -LiteralPath $current -Force -ErrorAction Stop
                $attributes = $item.Attributes
                $isContainer = [bool]$item.PSIsContainer
            } else {
                $current = [System.IO.Path]::Combine($current, $parts[$partIndex])
                $attributes = [System.IO.File]::GetAttributes($current)
                $isContainer = (($attributes -band [System.IO.FileAttributes]::Directory) -ne 0)
            }
        } catch [System.Management.Automation.ItemNotFoundException] {
            return [pscustomobject]@{ Present = $false }
        } catch [System.IO.FileNotFoundException] {
            return [pscustomobject]@{ Present = $false }
        } catch [System.IO.DirectoryNotFoundException] {
            return [pscustomobject]@{ Present = $false }
        } catch {
            throw "cannot inspect worktree path '$($Entry.Path)': $($_.Exception.Message)"
        }
        $reparse = (($attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0)
        if ($partIndex -lt $parts.Length - 1) {
            if ($reparse) {
                throw "worktree path '$($Entry.Path)' has a reparse-point parent"
            }
            if (-not $isContainer) {
                throw "worktree path '$($Entry.Path)' has a non-directory parent"
            }
            continue
        }
    }

    $openReparse = $false
    $target = $null
    if ($Entry.Mode -eq "120000" -and $reparse) {
        if ($isWindowsHost) {
            $linkType = [string]$item.LinkType
            if ($linkType -ne "" -and $linkType -cne "SymbolicLink") {
                throw "worktree symlink '$($Entry.Path)' is an unsupported reparse point"
            }
            $target = $item.LinkTarget
            if ($null -eq $target) { $target = $item.Target }
            if ($null -eq $target) {
                try { $target = ([System.IO.FileInfo]::new($current)).LinkTarget } catch { }
            }
        } else {
            try { $target = ([System.IO.FileInfo]::new($current)).LinkTarget } catch { }
        }
        $targetValues = @($target)
        if ($targetValues.Count -ne 1 -or $null -eq $targetValues[0]) {
            throw "cannot read worktree symlink '$($Entry.Path)' without dereferencing it"
        }
        try {
            [byte[]]$bytes = [System.Text.UTF8Encoding]::new($false, $true).GetBytes([string]$targetValues[0])
        } catch {
            throw "cannot decode worktree symlink '$($Entry.Path)': $($_.Exception.Message)"
        }
        $openReparse = $true
    } else {
        if ($reparse) {
            throw "regular worktree path '$($Entry.Path)' is a reparse point"
        }
        if ($isContainer) {
            throw "worktree path '$($Entry.Path)' is not a regular file"
        }
        try {
            [byte[]]$bytes = [System.IO.File]::ReadAllBytes($current)
        } catch {
            throw "cannot read worktree path '$($Entry.Path)': $($_.Exception.Message)"
        }
    }

    $identity = Get-CanonicalFilesystemPath $current $openReparse
    if ($IdentityMap.ContainsKey($identity) -and $IdentityMap[$identity].PathKey -cne $Entry.PathKey) {
        throw "worktree paths '$($IdentityMap[$identity].Path)' and '$($Entry.Path)' resolve to one filesystem identity"
    }
    $IdentityMap[$identity] = [pscustomobject]@{ Path = $Entry.Path; PathKey = $Entry.PathKey }
    return [pscustomobject]@{ Present = $true; Bytes = $bytes }
}

# The only successful path is the final branch below. Every scope decision before
# it throws on ambiguity: repository discovery, exact manifests, blob lookup,
# strict decoding, worktree identity, HEAD validity, shallow history, commit
# enumeration and stale allowances.
try {
    Push-Location $root
    try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }
    $OutputEncoding = [System.Text.Encoding]::UTF8

    foreach ($name in @(
        "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
        "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_CEILING_DIRECTORIES",
        "GIT_NAMESPACE", "GIT_REPLACE_REF_BASE", "GIT_SHALLOW_FILE"
    )) {
        if ($null -ne [Environment]::GetEnvironmentVariable($name)) {
            throw "$name is set; refusing an ambiguous Git source"
        }
    }

    $null = Get-GitText @("rev-parse", "--git-dir")
    $top = Get-SingleGitText @("rev-parse", "--show-toplevel") "Git top level"
    $rootFull = Get-CanonicalFilesystemPath $root
    $topFull = Get-CanonicalFilesystemPath $top
    $pathComparison = if ($isWindowsHost) {
        [System.StringComparison]::OrdinalIgnoreCase
    } else {
        [System.StringComparison]::Ordinal
    }
    if (-not [string]::Equals($rootFull, $topFull, $pathComparison)) {
        throw "Git top level '$topFull' does not match scanner root '$rootFull'"
    }
    $index = Get-SingleGitText @("rev-parse", "--git-path", "index") "Git index"
    if (-not [System.IO.Path]::IsPathRooted($index)) { $index = Join-Path $root $index }
    $indexFull = Get-CanonicalFilesystemPath $index
    $rootPrefix = if ($rootFull -eq [System.IO.Path]::DirectorySeparatorChar) {
        $rootFull
    } else {
        $rootFull + [System.IO.Path]::DirectorySeparatorChar
    }
    if (-not $indexFull.StartsWith($rootPrefix, $pathComparison)) {
        throw "Git index '$indexFull' is outside scanner root '$rootFull'"
    }

    # Replacement refs and legacy grafts rewrite reachable history without
    # changing HEAD. Reject both before binding the one immutable HEAD OID.
    $grafts = Get-SingleGitText @("rev-parse", "--git-path", "info/grafts") "Git graft state"
    if (-not [System.IO.Path]::IsPathRooted($grafts)) { $grafts = Join-Path $root $grafts }
    if (Test-Path -LiteralPath $grafts) {
        throw "legacy Git graft state is present; reachable history is ambiguous"
    }
    $replacementText = Get-GitText @("for-each-ref", "--format=%(refname)", "refs/replace")
    $replacementRefs = @($replacementText -split "`r?`n" | Where-Object { $_ -ne "" })
    if ($replacementRefs.Count -ne 0) {
        throw "Git replacement refs are present; reachable history is ambiguous"
    }

    try {
        $headOid = Get-SingleGitText @("--no-replace-objects", "rev-parse", "--verify", "--quiet", "HEAD^{commit}") "HEAD OID"
    } catch {
        throw "cannot resolve exact HEAD OID: $($_.Exception.Message)"
    }
    if (-not (Test-Oid $headOid)) { throw "HEAD OID is malformed" }
    $headOid = $headOid.ToLowerInvariant()
    $shallow = Get-SingleGitText @("--no-replace-objects", "rev-parse", "--is-shallow-repository") "Git shallow state"
    if ($shallow -ceq "true") {
        throw "history is shallow; commit-message scope is incomplete"
    }

    # HEAD is metadata for one decision only: an absent ordinary worktree path
    # is skippable only when its stage-0 index entry is byte-identical to HEAD.
    # HEAD tree blobs are deliberately not part of the publication source set.
    $headBytes = (Invoke-GitRaw @("--no-replace-objects", "ls-tree", "-r", "-z", "--full-tree", $headOid) $null).Bytes
    $headManifest = Get-GitManifest $headBytes "HEAD"
    $indexBytes = (Invoke-GitRaw @("ls-files", "--stage", "--full-name", "-z") $null).Bytes
    $indexManifest = Get-GitManifest $indexBytes "index"
    $flagBytes = (Invoke-GitRaw @("ls-files", "-v", "-z") $null).Bytes
    $indexFlags = Get-GitIndexFlags $flagBytes
    $deletedBytes = (Invoke-GitRaw @("ls-files", "--deleted", "-z") $null).Bytes
    $deletedSet = (Get-GitPathKeySet $deletedBytes "deleted-path manifest").Keys

    $indexPathSet = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::Ordinal)
    foreach ($entry in $indexManifest.Entries.Values) {
        [void]$indexPathSet.Add($entry.Path)
        if (-not $indexFlags.ContainsKey($entry.PathKey)) {
            throw "index flags omitted exact stage-0 path '$($entry.Path)'"
        }
    }
    foreach ($pathKey in $indexFlags.Keys) {
        if (-not $indexManifest.Entries.ContainsKey($pathKey)) {
            throw "index flags reported a path absent from the stage-0 index"
        }
    }
    if ($isWindowsHost) {
        $caseAliases = [System.Collections.Generic.Dictionary[string,string]]::new([System.StringComparer]::OrdinalIgnoreCase)
        foreach ($entry in $indexManifest.Entries.Values) {
            if ($caseAliases.ContainsKey($entry.Path) -and $caseAliases[$entry.Path] -cne $entry.PathKey) {
                throw "Git paths '$($caseAliases[$entry.Path])' and '$($entry.Path)' collide on Windows"
            }
            $caseAliases[$entry.Path] = $entry.PathKey
        }
    }

    $identityComparer = if ($isWindowsHost) {
        [System.StringComparer]::OrdinalIgnoreCase
    } else {
        [System.StringComparer]::Ordinal
    }
    $worktreeIdentity = [System.Collections.Generic.Dictionary[string,object]]::new($identityComparer)
    $selectedEntries = [System.Collections.ArrayList]::new()
    $worktreeBytesByPath = [System.Collections.Generic.Dictionary[string,object]]::new([System.StringComparer]::Ordinal)
    $worktreeMissingCount = 0
    $worktreePresentCount = 0
    foreach ($entry in $indexManifest.Entries.Values) {
        $worktree = Get-SafeWorktreeBytes $entry $worktreeIdentity
        if (-not $worktree.Present) {
            $flag = $indexFlags[$entry.PathKey]
            if ($flag -cne "H") {
                throw "cannot skip missing worktree path '$($entry.Path)': index has skip-worktree or assume-unchanged state"
            }
            if (-not $deletedSet.Contains($entry.PathKey)) {
                throw "cannot skip missing worktree path '$($entry.Path)': Git did not report an ordinary deletion"
            }
            if (-not $headManifest.Entries.ContainsKey($entry.PathKey)) {
                throw "cannot skip missing worktree path '$($entry.Path)': path is not present in bound HEAD"
            }
            $headEntry = $headManifest.Entries[$entry.PathKey]
            if ($headEntry.Mode -cne $entry.Mode -or $headEntry.Oid -cne $entry.Oid) {
                throw "cannot skip missing worktree path '$($entry.Path)': index diverges from bound HEAD"
            }
            # A safely deleted path has no publishable worktree bytes and its
            # index is the unchanged HEAD copy. Do not fetch or scan it.
            $worktreeMissingCount++
            continue
        }
        $worktreePresentCount++
        [void]$selectedEntries.Add($entry)
        $worktreeBytesByPath[$entry.PathKey] = $worktree.Bytes
    }

    $oidSet = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::Ordinal)
    foreach ($entry in $selectedEntries) { [void]$oidSet.Add($entry.Oid) }
    [string[]]$oidList = @($oidSet)
    $blobMap = (Get-GitBlobMap $oidList).Blobs

    foreach ($allowance in $allowances) {
        $allowance | Add-Member -NotePropertyName RevisionCounts -NotePropertyValue @{} -Force
    }
    $revisions = [System.Collections.ArrayList]::new()
    $binaryPathSet = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::Ordinal)
    foreach ($entry in $selectedEntries) {
        if (-not $blobMap.ContainsKey($entry.Oid)) {
            throw "index blob $($entry.Oid) for '$($entry.Path)' was not returned"
        }
        $textEntry = Test-TextEntry $entry.Path $entry.Mode
        if (-not $textEntry) { [void]$binaryPathSet.Add($entry.PathKey) }
        Add-SourceRevision $revisions ([pscustomobject]@{
            Path = $entry.Path
            PathKey = $entry.PathKey
            Oid = $entry.Oid
            Mode = $entry.Mode
            Bytes = [byte[]]$blobMap[$entry.Oid].Bytes
            Source = "index"
            TextEligible = $textEntry
        })
        $textEntry = Test-TextEntry $entry.Path $entry.Mode
        if (-not $textEntry) { [void]$binaryPathSet.Add($entry.PathKey) }
        Add-SourceRevision $revisions ([pscustomobject]@{
            Path = $entry.Path
            PathKey = $entry.PathKey
            Oid = $null
            Mode = $entry.Mode
            Bytes = [byte[]]$worktreeBytesByPath[$entry.PathKey]
            Source = "worktree"
            TextEligible = $textEntry
        })
    }

    $found = [System.Collections.ArrayList]::new()
    $scannedRevisionCount = 0
    $scannedPathSet = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::Ordinal)
    foreach ($revision in $revisions) {
        if (-not $revision.TextEligible) { continue }
        try {
            $text = Read-StrictText $revision.Bytes
        } catch {
            throw "cannot decode '$($revision.Path)' revision $($revision.Digest) from $($revision.Sources -join ','): $($_.Exception.Message)"
        }
        $scannedRevisionCount++
        [void]$scannedPathSet.Add($revision.PathKey)
        $rules = $patterns
        if ($sourceExtensions -contains (Get-GitExtension $revision.Path)) {
            $rules = $patterns + $sourceRule
        }
        Add-Matches $found $revision.Path $text $rules ($revision.Sources -join ",") $revision.RevisionId
    }
    if ($scannedRevisionCount -eq 0) {
        throw "no tracked text revisions selected; refusing a clean verdict"
    }

    $commitRaw = (Invoke-GitRaw @("-c", "i18n.logOutputEncoding=UTF-8", "--no-replace-objects", "log", "--format=%H", "--no-decorate", "--no-color", $headOid) $null).Bytes
    $commitText = Read-StrictText $commitRaw
    $commitShas = [System.Collections.Generic.List[string]]::new()
    $commitLines = $commitText -split "`r?`n"
    foreach ($line in $commitLines) {
        if ($line -eq "") { continue }
        if (-not (Test-Oid $line)) { throw "git log returned a malformed commit ID" }
        [void]$commitShas.Add($line.ToLowerInvariant())
    }
    if ($commitShas.Count -eq 0) {
        throw "HEAD exists but no reachable commits were enumerated"
    }
    $commitFileOnly = "commit trailer in a file", "artefact hash", "executable name"
    $commitRules = $patterns | Where-Object { $commitFileOnly -notcontains $_.Name }
    foreach ($sha in $commitShas) {
        $bodyRaw = (Invoke-GitRaw @("-c", "i18n.logOutputEncoding=UTF-8", "--no-replace-objects", "log", "-1", "--format=%B", $sha) $null).Bytes
        try {
            $body = Read-StrictText $bodyRaw
        } catch {
            throw "cannot decode commit ${sha}: $($_.Exception.Message)"
        }
        Add-Matches $found ("commit " + $sha.Substring(0, 7)) $body $commitRules "history" "history"
    }

    foreach ($allowance in $allowances) {
        $active = $false
        if ($null -ne $allowance.Anchor) {
            $active = $indexPathSet.Contains($allowance.Anchor)
        } elseif ($null -ne $allowance.Path) {
            foreach ($revision in $revisions) {
                if ($revision.TextEligible -and [string]::Equals($revision.Path, $allowance.Path, [System.StringComparison]::Ordinal)) {
                    $active = $true
                    break
                }
            }
        }
        if (-not $active) { continue }
        $counts = @($allowance.RevisionCounts.Values | ForEach-Object { [int]$_ })
        $maximum = 0
        foreach ($count in $counts) {
            if ($count -gt $maximum) { $maximum = $count }
            if ($count -gt [int]$allowance.Expected) {
                $displayPath = if ($null -ne $allowance.Anchor) { $allowance.Anchor } else { $allowance.Path }
                [void]$found.Add([pscustomobject]@{
                    File = $displayPath
                    Source = "allowance"
                    Revision = "count"
                    Line = 0
                    Rule = "allowance exceeds expected"
                    Match = "$($allowance.Rule): revision count $count, expected $($allowance.Expected)"
                })
            }
        }
        if ($counts -notcontains [int]$allowance.Expected) {
            $displayPath = if ($null -ne $allowance.Anchor) { $allowance.Anchor } else { $allowance.Path }
            [void]$found.Add([pscustomobject]@{
                File = $displayPath
                Source = "allowance"
                Revision = "stale"
                Line = 0
                Rule = "stale synthetic allowance"
                Match = "$($allowance.Rule): maximum $maximum, expected $($allowance.Expected)"
            })
        }
    }

    $authorizedCount = 0
    foreach ($allowance in $allowances) {
        foreach ($count in $allowance.RevisionCounts.Values) {
            $authorizedCount += [int]$count
        }
    }
    if ($found.Count -ne 0) {
        Write-Output "leak-scan: $($found.Count) finding(s); no clean verdict"
        foreach ($finding in $found) {
            Write-Output (($finding | Select-Object File,Source,Line,Rule,Match,Revision) | ConvertTo-Json -Compress)
        }
        exit 1
    }

    Write-Output "leak-scan: clean within configured scope"
    Write-Output "  tracked logical paths scanned: $($scannedPathSet.Count)"
    Write-Output "  tracked text files scanned: $($scannedPathSet.Count)"
    Write-Output "  content revisions scanned: $scannedRevisionCount"
    Write-Output "  HEAD paths used for missing classification: $($headManifest.Entries.Count)"
    Write-Output "  index blob entries: $($indexManifest.Entries.Count)"
    Write-Output "  worktree revisions present: $worktreePresentCount"
    Write-Output "  worktree copies skipped (ordinary deletion): $worktreeMissingCount"
    Write-Output "  commits scanned: $($commitShas.Count)"
    Write-Output "  binary paths excluded: $($binaryPathSet.Count)"
    Write-Output "  binary files excluded: $($binaryPathSet.Count)"
    Write-Output "  explicit synthetic allowances consumed: $authorizedCount"
    Write-Output "  inspection errors: 0"
    exit 0
} finally {
    Pop-Location
}
