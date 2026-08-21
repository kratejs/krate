$ErrorActionPreference = "Stop"

Write-Host "Building krate platform packages + JS packages (Release)..." -ForegroundColor Cyan

# Generate the per-platform binary packages (@krate/core-*) via the canonical
# generator script. The generated packages are gitignored and published by CI.
Push-Location "..\.."
try {
    node scripts/build-platform-packages.mjs
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Platform package generation failed" -ForegroundColor Red
        exit 1
    }
} finally {
    Pop-Location
}

# Build the runtime
Push-Location "..\runtime"
try {
    if (Test-Path "package.json") {
        npm run build 2>$null
        if ($LASTEXITCODE -eq 0) {
            Write-Host "  Runtime built" -ForegroundColor Green
        }
    }
} finally {
    Pop-Location
}

Write-Host "Done!" -ForegroundColor Green
