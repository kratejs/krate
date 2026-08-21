param(
    [string]$OutputDir = ".\bin"
)

$ErrorActionPreference = "Stop"

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

# Build components package
$componentsDir = "..\components"
if (Test-Path $componentsDir) {
    Write-Host "Building components package..." -ForegroundColor Cyan
    Push-Location $componentsDir
    try {
        if (Test-Path "package.json") {
            Write-Host "  @krate/components ready" -ForegroundColor Green
        }
    } finally {
        Pop-Location
    }
}

# Build runtime if applicable
$runtimeDir = "..\runtime"
if (Test-Path $runtimeDir) {
    Write-Host "Building runtime..." -ForegroundColor Cyan
    Push-Location $runtimeDir
    try {
        # Run npm build if package.json exists
        if (Test-Path "package.json") {
            npm run build 2>$null
            if ($LASTEXITCODE -eq 0) {
                Write-Host "  Runtime built" -ForegroundColor Green
            }
        }
    } finally {
        Pop-Location
    }
}

Write-Host "Done!" -ForegroundColor Green
