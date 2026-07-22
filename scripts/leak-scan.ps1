# Scans the working tree for anything that should never be published.
#
# This package was extracted from a private application. `go test` already fails
# on the obvious markers (see leak_test.go), but that only covers source files
# and only the brand itself. This script is the wider net: documentation, commit
# messages, build artefacts and the shapes that leak an environment rather than a
# name - absolute paths, machine-specific measurements, artefact hashes.
#
#   pwsh scripts/leak-scan.ps1
#
# Exit code 0 means clean.

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root

$patterns = @(
    @{ Name = "upstream product name"; Pattern = "token" + "pilor" }
    @{ Name = "upstream product name"; Pattern = "co" + "dex" }
    # The WebView2 COM layer is written in this repository. A hit here means a
    # third-party browser binding crept back in, bringing its attribution and its
    # limits with it.
    @{ Name = "third-party webview binding"; Pattern = "wa" + "ils" }
    @{ Name = "absolute Windows path"; Pattern = "[A-Za-z]:\\(Users|dev|devTools)\\" }
    @{ Name = "artefact hash"; Pattern = "\b[0-9a-fA-F]{40,64}\b" }
    @{ Name = "agent signature"; Pattern = "Duzenleyen|Son Guncelleme" }
    @{ Name = "executable name"; Pattern = "\w+\.exe\b" }
    # A pseudo-version whose hash part looks like a real commit hash names a real
    # commit somewhere - possibly in the private history this package was
    # extracted from. The one sanctioned fixture is obviously synthetic (the Go
    # reference time plus a counting-pattern hash) and is excluded by the
    # lookahead.
    @{ Name = "pseudo-version with a real-looking commit hash"; Pattern = "v0\.0\.0-\d{14}-(?!abcdef" + "123456\b)[0-9a-f]{12}" }
    # Commit-trailer text belongs in a commit message, never in a tracked file.
    @{ Name = "commit trailer in a file"; Pattern = "Co-Auth" + "ored-By" }
)

# Source must stay ASCII: a stray non-ASCII character in a .go file is almost
# always a half-translated comment. Prose is exempt - an em dash in the README is
# not a leak.
$sourcePatterns = @(
    @{ Name = "non-ASCII character in source"; Pattern = "[^\x00-\x7F]" }
)
$sourceExtensions = @(".go", ".js", ".css", ".html", ".cs", ".ps1", ".yml")

# Files that legitimately contain one of the shapes above.
$skip = @(
    "go.sum",              # module checksums are long hex strings by definition
    "leak-scan.ps1",       # this file lists the patterns
    "leak_test.go"         # so does this one
)

# The log sanitiser's whole job is to strip file system paths out of messages, so
# its tests have to contain paths to strip. The names in them are invented
# (Example User, Alice O'Brien) - that is the fixture, not a leak.
$pathFixtures = @(
    "internal/logsafe/logsafe_test.go",
    "internal/doctor/doctor_test.go",
    "host/diagnostics_windows_test.go"
)

# Action pins in the CI workflow are full commit SHAs by design: pinning a
# third-party action to an immutable ref is the supply-chain hardening issue #13
# asked for, and a 40-hex commit SHA is exactly the "artefact hash" shape. The
# workflow file is exempt from that one rule only - the same targeted carve-out
# the path fixtures above get from the absolute-path rule - so every other rule
# (non-ASCII, absolute paths, upstream names) still scans it.
$hashFixtures = @(
    ".github/workflows/ci.yml"
)

# git ls-files octal-escapes and quotes any path with non-ASCII bytes when
# core.quotePath is on (its default): a file so named comes back as the literal
# "\303\251name.go", which -LiteralPath below cannot open - the read fails and,
# without the guard in the scan loop, the file is skipped while the run still
# reports clean. Ask git for the raw byte names (core.quotePath=false), split on
# the NUL that -z writes between them, and set the console to UTF-8 so PowerShell
# decodes git's UTF-8 path bytes rather than the ambient code page. This is the
# git-quoting sibling of the glob-quoting skip #16 closed with -LiteralPath, which
# does not cover it. A raw newline in a name - forbidden on Windows - is normalised
# by PowerShell's line-based capture, so such a name is not reliably scanned on a
# case-sensitive file system.
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }
$tracked = (git -c core.quotePath=false ls-files -z) -join "`n" -split "`0"
# A failed enumeration must not read as an empty, clean tree.
if ($LASTEXITCODE -ne 0) { throw "git ls-files failed (exit $LASTEXITCODE)" }
$files = $tracked | Where-Object {
    ($_ -ne "") -and
    ($skip -notcontains (Split-Path $_ -Leaf)) -and
    (-not $_.EndsWith(".png"))
}

$found = @()
foreach ($file in $files) {
    $rules = $patterns
    if ($sourceExtensions -contains [System.IO.Path]::GetExtension($file)) {
        $rules = $patterns + $sourcePatterns
    }
    foreach ($rule in $rules) {
        if ($rule.Name -eq "absolute Windows path" -and $pathFixtures -contains $file) {
            continue
        }
        if ($rule.Name -eq "artefact hash" -and $hashFixtures -contains $file) {
            continue
        }
        # -LiteralPath, not -Path: -Path treats its argument as a wildcard, so a
        # tracked file whose name contains glob metacharacters ([ ] on Windows)
        # would fail to resolve. A file the scan cannot open must fail loudly, not
        # vanish - that is the point of #16 - so report a read failure as a
        # finding instead of swallowing it with -ErrorAction SilentlyContinue, and
        # stop scanning a file we already know we cannot read.
        try {
            $hits = Select-String -LiteralPath $file -Pattern $rule.Pattern -AllMatches -ErrorAction Stop
        } catch {
            $found += [pscustomobject]@{
                File  = $file
                Line  = 0
                Rule  = "unscannable file"
                Match = $_.Exception.Message
            }
            break
        }
        foreach ($hit in $hits) {
            $found += [pscustomobject]@{
                File  = $file
                Line  = $hit.LineNumber
                Rule  = $rule.Name
                Match = $hit.Line.Trim()
            }
        }
    }
}

# Commit messages ship with a push exactly like tracked files do. Three rules
# are file-only, because the same shape is legitimate in a message: AI
# attribution trailers are sanctioned here, a git-generated revert or
# cherry-pick body cites a full 40-hex commit hash, and naming an executable in
# a message discloses nothing.
git rev-parse --verify --quiet HEAD > $null 2>&1
if ($LASTEXITCODE -eq 0) {
    $commitFileOnly = "commit trailer in a file", "artefact hash", "executable name"
    $commitRules = $patterns | Where-Object { $commitFileOnly -notcontains $_.Name }
    $commitShas = @(git log --format=%H)
    # A failed log enumeration must not read as an empty, clean history - the same
    # fail-closed rule the tracked-file enumeration above uses (issue #71).
    if ($LASTEXITCODE -ne 0) { throw "git log failed (exit $LASTEXITCODE)" }
    foreach ($sha in $commitShas) {
        $body = (git log -1 --format=%B $sha) -join "`n"
        if ($LASTEXITCODE -ne 0) { throw "git log for $sha failed (exit $LASTEXITCODE)" }
        foreach ($rule in $commitRules) {
            foreach ($m in [regex]::Matches($body, $rule.Pattern, "IgnoreCase")) {
                $found += [pscustomobject]@{
                    File  = "commit " + $sha.Substring(0, 7)
                    Line  = 0
                    Rule  = $rule.Name
                    Match = $m.Value
                }
            }
        }
    }
}

if ($found.Count -eq 0) {
    Write-Output "leak-scan: clean ($($files.Count) tracked files)"
    Pop-Location
    exit 0
}

Write-Output "leak-scan: $($found.Count) finding(s)"
$found | Format-Table -AutoSize -Wrap
Pop-Location
exit 1
