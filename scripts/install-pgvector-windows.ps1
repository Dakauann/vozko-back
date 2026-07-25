#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Installs pgvector extension on PostgreSQL for Windows.

.DESCRIPTION
    This script downloads, compiles, and installs pgvector for PostgreSQL on Windows.
    It requires Visual Studio Build Tools and PostgreSQL to be installed.

.PARAMETER PostgresVersion
    PostgreSQL major version (e.g., 17, 16, 15). Default: 17

.PARAMETER PostgresPath
    Path to PostgreSQL installation. Default: C:\Program Files\PostgreSQL\{version}

.PARAMETER Database
    Database name to enable pgvector on. Default: vozko-homolog

.PARAMETER User
    PostgreSQL user. Default: postgres

.EXAMPLE
    .\install-pgvector-windows.ps1 -PostgresVersion 17 -Database "mydb" -User "postgres"
#>

param(
    [int]$PostgresVersion = 17,
    [string]$PostgresPath = "",
    [string]$Database = "vozko-homolog",
    [string]$User = "postgres"
)

$ErrorActionPreference = "Stop"

# Set default PostgreSQL path if not provided
if (-not $PostgresPath) {
    $PostgresPath = "C:\Program Files\PostgreSQL\$PostgresVersion"
}

Write-Host "========================================" -ForegroundColor Cyan
Write-Host " pgvector Installation for Windows" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Validate PostgreSQL installation
if (-not (Test-Path $PostgresPath)) {
    Write-Error "PostgreSQL not found at: $PostgresPath"
    exit 1
}

$pgConfig = Join-Path $PostgresPath "bin\pg_config.exe"
if (-not (Test-Path $pgConfig)) {
    Write-Error "pg_config not found at: $pgConfig"
    exit 1
}

Write-Host "[1/6] PostgreSQL found at: $PostgresPath" -ForegroundColor Green

# Check for Visual Studio Build Tools
$vsWhere = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
$hasVS = $false

if (Test-Path $vsWhere) {
    $vsPath = & $vsWhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath 2>$null
    if ($vsPath) {
        $hasVS = $true
        Write-Host "[2/6] Visual Studio Build Tools found" -ForegroundColor Green
    }
}

if (-not $hasVS) {
    Write-Host ""
    Write-Host "Visual Studio Build Tools not found!" -ForegroundColor Yellow
    Write-Host "You can install pgvector using pre-built binaries instead." -ForegroundColor Yellow
    Write-Host ""
    Write-Host "Option 1: Download pre-built pgvector from:" -ForegroundColor Cyan
    Write-Host "  https://github.com/pgvector/pgvector/releases" -ForegroundColor White
    Write-Host ""
    Write-Host "Option 2: Install Visual Studio Build Tools:" -ForegroundColor Cyan
    Write-Host "  winget install Microsoft.VisualStudio.2022.BuildTools --override '--add Microsoft.VisualStudio.Component.VC.Tools.x86.x64'" -ForegroundColor White
    Write-Host ""
    
    $usePrebuilt = Read-Host "Would you like to try downloading pre-built binaries? (y/n)"
    if ($usePrebuilt -eq 'y') {
        # Try to download pre-built release
        $releaseUrl = "https://github.com/pgvector/pgvector/releases"
        Write-Host ""
        Write-Host "Please download the Windows release for PostgreSQL $PostgresVersion from:" -ForegroundColor Cyan
        Write-Host "  $releaseUrl" -ForegroundColor White
        Write-Host ""
        Write-Host "After downloading, extract and copy:" -ForegroundColor Yellow
        Write-Host "  - vector.dll to: $PostgresPath\lib\" -ForegroundColor White
        Write-Host "  - vector.control and vector--*.sql to: $PostgresPath\share\extension\" -ForegroundColor White
        Write-Host ""
        exit 0
    }
    exit 1
}

# Create temp directory
$tempDir = Join-Path $env:TEMP "pgvector-install"
if (Test-Path $tempDir) {
    Remove-Item -Recurse -Force $tempDir
}
New-Item -ItemType Directory -Path $tempDir | Out-Null

Write-Host "[3/6] Downloading pgvector source..." -ForegroundColor Yellow

$pgvectorVersion = "0.8.0"
$downloadUrl = "https://github.com/pgvector/pgvector/archive/refs/tags/v$pgvectorVersion.zip"
$zipPath = Join-Path $tempDir "pgvector.zip"

try {
    Invoke-WebRequest -Uri $downloadUrl -OutFile $zipPath -UseBasicParsing
    Write-Host "       Downloaded pgvector v$pgvectorVersion" -ForegroundColor Green
} catch {
    Write-Error "Failed to download pgvector: $_"
    exit 1
}

Write-Host "[4/6] Extracting source..." -ForegroundColor Yellow
Expand-Archive -Path $zipPath -DestinationPath $tempDir -Force
$sourceDir = Join-Path $tempDir "pgvector-$pgvectorVersion"

Write-Host "[5/6] Building pgvector..." -ForegroundColor Yellow

# Find vcvarsall.bat
$vcvarsall = Join-Path $vsPath "VC\Auxiliary\Build\vcvarsall.bat"
if (-not (Test-Path $vcvarsall)) {
    Write-Error "vcvarsall.bat not found at: $vcvarsall"
    exit 1
}

# Build using nmake
$buildScript = @"
@echo off
call "$vcvarsall" x64
cd /d "$sourceDir"
set "PGROOT=$PostgresPath"
nmake /F Makefile.win
nmake /F Makefile.win install
"@

$buildScriptPath = Join-Path $tempDir "build.bat"
Set-Content -Path $buildScriptPath -Value $buildScript

Push-Location $sourceDir
try {
    & cmd /c $buildScriptPath
    if ($LASTEXITCODE -ne 0) {
        throw "Build failed with exit code $LASTEXITCODE"
    }
    Write-Host "       Build completed successfully" -ForegroundColor Green
} catch {
    Write-Error "Build failed: $_"
    Pop-Location
    exit 1
}
Pop-Location

Write-Host "[6/6] Enabling pgvector in database '$Database'..." -ForegroundColor Yellow

$psql = Join-Path $PostgresPath "bin\psql.exe"
$env:PGPASSWORD = Read-Host -Prompt "Enter password for user '$User'" -AsSecureString | ConvertFrom-SecureString -AsPlainText

try {
    & $psql -h localhost -U $User -d $Database -c "CREATE EXTENSION IF NOT EXISTS vector;"
    if ($LASTEXITCODE -eq 0) {
        Write-Host "       pgvector extension enabled!" -ForegroundColor Green
    }
} catch {
    Write-Host "       Failed to enable extension automatically." -ForegroundColor Yellow
    Write-Host "       Run this SQL manually: CREATE EXTENSION IF NOT EXISTS vector;" -ForegroundColor White
}

# Cleanup
Remove-Item -Recurse -Force $tempDir -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host " Installation Complete!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "To verify installation, run:" -ForegroundColor White
Write-Host "  psql -U $User -d $Database -c `"SELECT extversion FROM pg_extension WHERE extname = 'vector';`"" -ForegroundColor Gray
Write-Host ""
