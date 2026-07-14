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
$sourceExtensions = @(".go", ".js", ".css", ".html", ".ps1", ".yml")

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
    "diagnostics_windows_test.go"
)

$files = git ls-files | Where-Object {
    $name = Split-Path $_ -Leaf
    ($skip -notcontains $name) -and (-not $_.EndsWith(".png"))
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
        $hits = Select-String -Path $file -Pattern $rule.Pattern -AllMatches -ErrorAction SilentlyContinue
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
    foreach ($sha in @(git log --format=%H)) {
        $body = (git log -1 --format=%B $sha) -join "`n"
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
