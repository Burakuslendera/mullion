# Scans Git-tracked publication text and reachable commit messages for the
# configured private-data shapes. Exit 0 means every declared input was read and
# checked with zero findings; it is not a general secret scan.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

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
# name it did not earn. Each allowance therefore binds one normalized Git path,
# detector family, exact synthetic capture or named component, and expected count.
# Workflow action hashes additionally bind the complete `uses:` line: a pin copied
# to unrelated text cannot satisfy the allowance. History allowances are inactive
# unless their source anchor is tracked. Counts make changed fixtures loud; deleting
# an ordinary path retires its unreachable allowance instead of a stale carve-out.
$checkoutPin = "11d5960a326750d58380" + "78e36cf38b85af677262"
$setupGoPin = "40f1582b2485089dde7a" + "bd97c1529aa768e1baff"
$allowances = @(
    [pscustomobject]@{ Path = ".github/workflows/ci.yml"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($checkoutPin) + "$"); Action = "actions/checkout"; Expected = 2; Consumed = 0 }
    [pscustomobject]@{ Path = ".github/workflows/ci.yml"; Rule = "artefact hash"; Value = ("^" + [regex]::Escape($setupGoPin) + "$"); Action = "actions/setup-go"; Expected = 2; Consumed = 0 }
    [pscustomobject]@{ Path = "docs/decisions/0025-urls-are-logged-as-urls.md"; Rule = "sensitive Windows drive path"; Value = ("^C:/Users/" + "alice$"); Expected = 1; Consumed = 0 }
    [pscustomobject]@{ Path = "docs/decisions/0028-message-keeps-the-urls-inside-it.md"; Rule = "sensitive Windows drive path"; Value = ("^C:/Users/" + "alice$"); Expected = 3; Consumed = 0 }
    [pscustomobject]@{ Path = "host/diagnostics_windows_test.go"; Rule = "sensitive Windows drive path"; Value = '(?i)^C:[\\/]+Users[\\/]+Example User$'; Expected = 1; Consumed = 0 }
    [pscustomobject]@{ Path = "host/systembrowser_windows_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)(?:etc|attacker)'; Expected = 1; Consumed = 0 }
    [pscustomobject]@{ Path = "host/webview_windows_test.go"; Rule = "sensitive Windows drive path"; Value = '(?i)^C:[\\/]+Users[\\/]+jane$'; Expected = 1; Consumed = 0 }
    [pscustomobject]@{ Path = "host/webview_windows_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)jane'; Expected = 1; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/doctor/architecture_gate_unsupported_windows_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)server'; Expected = 1; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/doctor/probe_windows_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)server'; Expected = 1; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/doctor/doctor_test.go"; Rule = "sensitive Windows drive path"; Value = '(?i)^C:[\\/]+Users[\\/]+(?:Example User|EXAMPL~1)$'; Expected = 13; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/doctor/doctor_test.go"; Rule = "extended UNC host"; Group = "host"; Value = '(?i)(?:HOME-NAS|BUILD-NAS)'; Expected = 3; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/doctor/doctor_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)(?:HOME-NAS|BUILD-NAS|rt)'; Expected = 17; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/logsafe/logsafe_test.go"; Rule = "sensitive Windows drive path"; Value = "(?i)^C:[\\/]+Users[\\/]+(?:Example User|Alice O'Brien|D'Angelo|O'Brien|Ana O'Neil)$"; Expected = 7; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/logsafe/logsafe_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)server'; Expected = 1; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/logsafe/message_url_test.go"; Rule = "sensitive Windows drive path"; Value = '(?i)^C:[\\/]+Users[\\/]+alice$'; Expected = 4; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/logsafe/message_url_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)FILESERVER'; Expected = 1; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/logsafe/url.go"; Rule = "sensitive Windows drive path"; Value = ("^C:/Users/" + "alice$"); Expected = 2; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/logsafe/url_test.go"; Rule = "sensitive Windows drive path"; Value = "(?i)^C:[\\/]+Users[\\/]+(?:\.\.\.|Alice O'Brien|alice)$"; Expected = 5; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/logsafe/url_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)server'; Expected = 1; Consumed = 0 }
    [pscustomobject]@{ Path = "internal/webview2/loader_discovery_windows_test.go"; Rule = "UNC host"; Group = "host"; Value = '(?i)BUILD-NAS'; Expected = 1; Consumed = 0 }
    [pscustomobject]@{ PathPattern = '^commit '; Anchor = "docs/decisions/0025-urls-are-logged-as-urls.md"; Rule = "sensitive Windows drive path"; Value = ("^C:/Users/" + "alice$"); Expected = 1; Consumed = 0 }
)

# Decode once per selected file. Select-String previously mixed selection,
# decoding and rule execution, which made an unreadable input easy to treat as no
# matches. Strict decoding turns malformed text into an inspection error before
# any clean verdict is possible; the BOM branches retain intentional UTF-16 data.
function Read-StrictText([string]$Path) {
    $bytes = [System.IO.File]::ReadAllBytes($Path)
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

function Use-Allowance([string]$File, [string]$Rule, [System.Text.RegularExpressions.Match]$Match, [string]$Text) {
    $sanitizedUser = $Rule -eq "sensitive Windows drive path" -and $Match.Groups["user"].Value -ceq "<user>"
    $sanitizedHost = ($Rule -eq "UNC host" -or $Rule -eq "extended UNC host") -and $Match.Groups["host"].Value -ceq "<host>"
    if ($sanitizedUser -or $sanitizedHost) {
        return $true
    }
    foreach ($allowance in $allowances) {
        if ($null -ne $allowance.Anchor -and $tracked -cnotcontains $allowance.Anchor) {
            continue
        }
        $pathMatches = ($null -ne $allowance.Path -and $allowance.Path -ceq $File) -or
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
            $allowance.Consumed++
            return $true
        }
    }
    return $false
}

function Add-Matches([System.Collections.ArrayList]$Findings, [string]$File, [string]$Text, $Rules) {
    foreach ($rule in $Rules) {
        foreach ($match in [regex]::Matches($Text, $rule.Pattern, [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)) {
            if (Use-Allowance $File $rule.Name $match $Text) {
                continue
            }
            [void]$Findings.Add([pscustomobject]@{
                File = $File
                Line = Get-LineNumber $Text $match.Index
                Rule = $rule.Name
                Match = $match.Value
            })
        }
    }
}

# The only successful path is the final branch below. Every scope decision before
# it throws on ambiguity: repository discovery, tracked-file enumeration, strict
# decoding, HEAD validity, shallow history, commit enumeration and stale
# allowances. Adding an early "clean" return would recreate the false-success
# class even if every detector remained correct.
try {
    Push-Location $root
    try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }
    $OutputEncoding = [System.Text.Encoding]::UTF8

    # Git environment overrides can redirect the index, objects or worktree away
    # from the repository whose files this script opens below.
    foreach ($name in @(
        "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
        "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_CEILING_DIRECTORIES",
        "GIT_NAMESPACE", "GIT_REPLACE_REF_BASE", "GIT_SHALLOW_FILE"
    )) {
        if ($null -ne [Environment]::GetEnvironmentVariable($name)) {
            throw "$name is set; refusing an ambiguous Git source"
        }
    }

    git rev-parse --git-dir > $null 2>&1
    if ($LASTEXITCODE -ne 0) { throw "not a Git repository" }
    $top = (git rev-parse --show-toplevel) -join ""
    if ($LASTEXITCODE -ne 0) { throw "cannot identify Git top level (exit $LASTEXITCODE)" }
    $rootFull = [System.IO.Path]::GetFullPath($root).TrimEnd([char[]]"\/")
    $topFull = [System.IO.Path]::GetFullPath($top).TrimEnd([char[]]"\/")
    if (-not [string]::Equals($rootFull, $topFull, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Git top level '$topFull' does not match scanner root '$rootFull'"
    }
    $index = (git rev-parse --git-path index) -join ""
    if ($LASTEXITCODE -ne 0) { throw "cannot identify Git index (exit $LASTEXITCODE)" }
    if (-not [System.IO.Path]::IsPathRooted($index)) { $index = Join-Path $root $index }
    $indexFull = [System.IO.Path]::GetFullPath($index)
    $rootPrefix = $rootFull + [System.IO.Path]::DirectorySeparatorChar
    if (-not $indexFull.StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Git index '$indexFull' is outside scanner root '$rootFull'"
    }

    # Replacement refs and legacy grafts rewrite reachable history without
    # changing HEAD. Reject both and still disable replacement-object lookup on
    # every message enumeration so a clean verdict always reads real objects.
    $grafts = (git rev-parse --git-path info/grafts) -join ""
    if ($LASTEXITCODE -ne 0) { throw "cannot identify Git graft state (exit $LASTEXITCODE)" }
    if (-not [System.IO.Path]::IsPathRooted($grafts)) { $grafts = Join-Path $root $grafts }
    if (Test-Path -LiteralPath $grafts) { throw "legacy Git graft state is present; reachable history is ambiguous" }
    $replacementRefs = @(git for-each-ref --format="%(refname)" refs/replace)
    if ($LASTEXITCODE -ne 0) { throw "cannot inspect Git replacement refs (exit $LASTEXITCODE)" }
    if ($replacementRefs.Count -ne 0) { throw "Git replacement refs are present; reachable history is ambiguous" }

    # Ask Git for raw NUL-delimited names so non-ASCII and glob metacharacters
    # remain filenames rather than quoted syntax. Binary exclusions are counted,
    # not silently forgotten. Zero selected text is never evidence of cleanliness.
    $tracked = (git -c core.quotePath=false ls-files -z) -join "`n" -split "`0"
    if ($LASTEXITCODE -ne 0) { throw "git ls-files failed (exit $LASTEXITCODE)" }
    $tracked = @($tracked | Where-Object { $_ -ne "" })
    $binary = @($tracked | Where-Object { $binaryExtensions -contains [System.IO.Path]::GetExtension($_).ToLowerInvariant() })
    $files = @($tracked | Where-Object { $binaryExtensions -notcontains [System.IO.Path]::GetExtension($_).ToLowerInvariant() })
    if ($files.Count -eq 0) { throw "no tracked text files selected; refusing a clean verdict" }

    $found = [System.Collections.ArrayList]::new()
    foreach ($file in $files) {
        $fullPath = Join-Path $root $file
        try {
            $text = Read-StrictText $fullPath
        } catch {
            throw "cannot decode tracked file '$file': $($_.Exception.Message)"
        }
        $rules = $patterns
        if ($sourceExtensions -contains [System.IO.Path]::GetExtension($file).ToLowerInvariant()) {
            $rules = $patterns + $sourceRule
        }
        Add-Matches $found $file $text $rules
    }

    # A failed HEAD lookup is not equivalent to "no messages to scan". Distinguish
    # an unborn repository from a broken reference, reject both for this
    # publication verdict, and reject shallow history because reachable-history
    # coverage would otherwise mean only the truncated clone.
    git rev-parse --verify --quiet HEAD > $null
    if ($LASTEXITCODE -ne 0) {
        $commitCount = git --no-replace-objects rev-list --all --count
        if ($LASTEXITCODE -ne 0) { throw "cannot distinguish an unborn repository from an invalid HEAD" }
        if ([int]$commitCount -eq 0) { throw "repository has no commit; commit-message scope cannot be verified" }
        throw "HEAD is invalid although repository history exists"
    }

    $shallow = (git rev-parse --is-shallow-repository) -join ""
    if ($LASTEXITCODE -ne 0) { throw "git shallow-state inspection failed (exit $LASTEXITCODE)" }
    if ($shallow.Trim() -eq "true") { throw "history is shallow; commit-message scope is incomplete" }

    # Issue #108 includes commit text in the publication boundary. Repository or
    # global output settings can make Git emit UTF-16LE-looking bytes and NUL-hide
    # an ASCII path from Select-String. Git reads each commit's declared encoding;
    # force the converted log output to UTF-8 on enumeration and per-object reads.
    # Neither command may inherit replacement-object or shallow-history authority.
    $commitShas = @(git -c i18n.logOutputEncoding=UTF-8 --no-replace-objects log --format=%H)
    if ($LASTEXITCODE -ne 0) { throw "git log failed (exit $LASTEXITCODE)" }
    if ($commitShas.Count -eq 0) { throw "HEAD exists but no reachable commits were enumerated" }
    $commitFileOnly = "commit trailer in a file", "artefact hash", "executable name"
    $commitRules = $patterns | Where-Object { $commitFileOnly -notcontains $_.Name }
    foreach ($sha in $commitShas) {
        $body = (git -c i18n.logOutputEncoding=UTF-8 --no-replace-objects log -1 --format=%B $sha) -join "`n"
        if ($LASTEXITCODE -ne 0) { throw "git log for $sha failed (exit $LASTEXITCODE)" }
        Add-Matches $found ("commit " + $sha.Substring(0, 7)) $body $commitRules
    }

    # Expected allowance counts are checked only when their anchor is tracked.
    # This lets the real script run in minimal fixture repositories while making
    # a deleted or moved synthetic fixture loud in the production repository.
    foreach ($allowance in $allowances) {
        $anchor = $allowance.Path
        if ($null -ne $allowance.Anchor) {
            $anchor = $allowance.Anchor
        }
        if ($tracked -ccontains $anchor -and $allowance.Consumed -ne $allowance.Expected) {
            [void]$found.Add([pscustomobject]@{
                File = $anchor
                Line = 0
                Rule = "stale synthetic allowance"
                Match = "$($allowance.Rule): consumed $($allowance.Consumed), expected $($allowance.Expected)"
            })
        }
    }

    # Findings and inspection failures have different mechanics but the same
    # authority rule: neither may print the clean wording. The success block also
    # states the inspected and excluded counts so readers cannot mistake this
    # configured known-shape scan for a general secret scanner.
    if ($found.Count -ne 0) {
        Write-Output "leak-scan: $($found.Count) finding(s); no clean verdict"
        $found | Format-Table -AutoSize -Wrap
        exit 1
    }

    Write-Output "leak-scan: clean within configured scope"
    Write-Output "  tracked text files scanned: $($files.Count)"
    Write-Output "  commits scanned: $($commitShas.Count)"
    Write-Output "  binary files excluded: $($binary.Count)"
    Write-Output "  explicit synthetic allowances consumed: $((($allowances | Measure-Object -Property Consumed -Sum).Sum))"
    Write-Output "  inspection errors: 0"
    exit 0
} finally {
    Pop-Location
}
