# Builds the frontend and the bblv binaries for Windows and Linux into dist/.
# Usage: .\build.ps1 [-Version 1.0.0]
param(
    [string]$Version = ""
)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not $Version) {
    try { $Version = (git describe --tags --always --dirty 2>$null) } catch { }
    if (-not $Version) { $Version = "dev" }
}

Push-Location web
try {
    npm ci --prefer-offline
    if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "frontend build failed" }
}
finally {
    Pop-Location
}

New-Item -ItemType Directory -Force dist | Out-Null
$env:CGO_ENABLED = "0"
try {
    foreach ($target in @("windows/amd64", "linux/amd64", "linux/arm64")) {
        $os, $arch = $target.Split("/")
        $env:GOOS = $os
        $env:GOARCH = $arch
        $out = "dist/bblv-$os-$arch"
        if ($os -eq "windows") { $out += ".exe" }
        go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $out ./cmd/bblv
        if ($LASTEXITCODE -ne 0) { throw "go build for $target failed" }
        Write-Host "built $out ($Version)"
    }
}
finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
}
