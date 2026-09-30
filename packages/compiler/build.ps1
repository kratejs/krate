param(
    [string]$OutputDir = ".\bin",
    # Skip the JS workspace install/build. Use this when you have already run
    # `pnpm install && pnpm build` at the repo root and only want to rebuild the
    # Go binary.
    [switch]$SkipJs
)

$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path

# The compiler locates the client runtime through the workspace packages, and
# `examples/krate.config.ts` imports `@krate/core`, `@krate/runtime`, and
# `@krate/plugin`. Those are workspace packages whose `dist/` is gitignored, so a
# fresh checkout has no compiled JS and `krate dev` fails to load the config.
# Ensure the workspace is installed and those packages are built before building
# the Go binary.
if (-not $SkipJs) {
    if (-not (Get-Command pnpm -ErrorAction SilentlyContinue)) {
        Write-Host "pnpm not found on PATH. Install it from https://pnpm.io/ and re-run," -ForegroundColor Red
        Write-Host "or run 'pnpm install && pnpm build' at the repo root, then re-run with -SkipJs." -ForegroundColor Red
        exit 1
    }

    Push-Location $repoRoot
    try {
        Write-Host "Installing JS workspace dependencies..." -ForegroundColor Cyan
        pnpm install
        if ($LASTEXITCODE -ne 0) {
            Write-Host "  pnpm install failed" -ForegroundColor Red
            exit 1
        }

        Write-Host "Building JS workspace packages..." -ForegroundColor Cyan
        pnpm --filter @krate/core --filter @krate/runtime --filter @krate/plugin run build
        if ($LASTEXITCODE -ne 0) {
            Write-Host "  JS package build failed" -ForegroundColor Red
            exit 1
        }
    } finally {
        Pop-Location
    }
}

Write-Host "Building krate compiler..." -ForegroundColor Cyan

# Build the krate binary
$binaryName = "krate.exe"
$output = Join-Path $OutputDir $binaryName

# Ensure output directory exists
New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null

# Build
go build -ldflags="-s -w" -o $output .\cmd\krate\

if ($LASTEXITCODE -eq 0) {
    Write-Host "  Built $output" -ForegroundColor Green
} else {
    Write-Host "  Build failed" -ForegroundColor Red
    exit 1
}

# Stage the binary where @krate/core's install.js expects a locally built
# binary, so the workspace `krate` CLI works too (not just .\bin\krate.exe).
$coreBinDir = Join-Path $PSScriptRoot "..\core\bin"
New-Item -ItemType Directory -Path $coreBinDir -Force | Out-Null
Copy-Item -Path $output -Destination (Join-Path $coreBinDir $binaryName) -Force

Write-Host "Done!" -ForegroundColor Green
Write-Host "Run the dev server with: .\bin\krate.exe dev ..\..\examples" -ForegroundColor Green
