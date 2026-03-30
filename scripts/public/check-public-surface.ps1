param(
    [string]$ProjectDir
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($ProjectDir)) {
    $projectDir = Split-Path -Parent (Split-Path -Parent $scriptDir)
} else {
    $projectDir = $ProjectDir
}

$patterns = @(
    'github.com/kombifyio/SpeechKit',
    'https://github.com/kombifyio/SpeechKit',
    'kombination-personal',
    'kombination',
    'kombify.io',
    'kombify.space',
    'kombify-Core',
    'kombify-Administration',
    'mako1',
    '82.165.251.178',
    'Private --'
)

$searchRoots = @(
    '.github',
    'android',
    'assets',
    'cmd',
    'docker',
    'examples',
    'internal',
    'pkg',
    'go.mod',
    'go.sum',
    'README.md',
    'CHANGELOG.md',
    'CONTRIBUTING.md',
    'CODE_OF_CONDUCT.md',
    'SECURITY.md',
    'SUPPORT.md',
    'config.example.toml',
    'docs',
    'frontend/app/README.md',
    'frontend/app/src',
    'installer',
    'scripts'
)

$hits = @()
Push-Location $projectDir
try {
    foreach ($root in $searchRoots) {
        if (-not (Test-Path $root)) {
            continue
        }

        $items = Get-Item $root
        if ($items.PSIsContainer) {
            $files = Get-ChildItem $root -Recurse -File
        } else {
            $files = @($items)
        }

        foreach ($file in $files) {
            if ($file.FullName -like '*\docs\plans\*' -or $file.FullName -like '*\scripts\public\check-public-surface.*') {
                continue
            }
            # Skip HeliBoard third-party submodule content (dictionaries, changelogs, build artifacts)
            if ($file.FullName -like '*\android\keyboard\heliboard\app\src\main\assets\dicts\*' -or
                $file.FullName -like '*\android\keyboard\heliboard\fastlane\*' -or
                $file.FullName -like '*\android\keyboard\heliboard\app\build\*' -or
                $file.FullName -like '*\android\*\build\*') {
                continue
            }
            # Skip binary files
            if ($file.Extension -in @('.class', '.jar', '.dex', '.apk', '.aab', '.png', '.jpg', '.ico', '.woff2', '.syso', '.dict', '.bin', '.lock')) {
                continue
            }
            foreach ($pattern in $patterns) {
                $matches = Select-String -Path $file.FullName -Pattern $pattern -SimpleMatch -ErrorAction SilentlyContinue
                foreach ($match in $matches) {
                    $hits += '{0}:{1}: {2}' -f $match.Path, $match.LineNumber, $match.Line.Trim()
                }
            }
        }
    }

    $allExecutables = Get-ChildItem . -Recurse -File -Filter *.exe -ErrorAction SilentlyContinue
    foreach ($exe in $allExecutables) {
        $relativePath = Resolve-Path -Relative $exe.FullName
        $normalized = $relativePath.TrimStart('.','\').Replace('\', '/')
        if ($normalized -notlike 'dist/windows/*' -and $normalized -notlike 'installer/*') {
            $hits += "unexpected exe outside release surface: $normalized"
        }
    }

    foreach ($internalFile in @('AGENTS.md', 'CLAUDE.md')) {
        if (Test-Path $internalFile) {
            $hits += "internal-only file present: $internalFile"
        }
    }
}
finally {
    Pop-Location
}

if ($hits.Count -gt 0) {
    Write-Host 'Public surface check failed:'
    $hits | ForEach-Object { Write-Host "  $_" }
    exit 1
}

Write-Host 'Public surface check passed.'
