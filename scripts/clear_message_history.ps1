#
# clear_message_history.ps1
# Deletes all conversation message history from the database.
# 
# SAFETY: Only runs if the database name ends with "_homolog"
#
# Usage:
#   .\scripts\clear_message_history.ps1
#   
# Or with custom env vars:
#   $env:DB_HOST="localhost"; $env:DB_PORT="5433"; .\scripts\clear_message_history.ps1
#

$ErrorActionPreference = "Stop"

# Load .env file if it exists
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir
$EnvFile = Join-Path $ProjectRoot ".env"

if (Test-Path $EnvFile) {
    Write-Host "Loading .env from $EnvFile" -ForegroundColor Yellow
    Get-Content $EnvFile | ForEach-Object {
        if ($_ -match '^\s*([^#][^=]+)=(.*)$') {
            $name = $matches[1].Trim()
            $value = $matches[2].Trim()
            # Remove surrounding quotes if present
            $value = $value -replace '^["'']|["'']$', ''
            [Environment]::SetEnvironmentVariable($name, $value, "Process")
        }
    }
}

# Database connection settings (from env or defaults)
$DB_HOST = if ($env:DB_HOST) { $env:DB_HOST } else { "localhost" }
$DB_PORT = if ($env:DB_PORT) { $env:DB_PORT } else { "5432" }
$DB_USER = if ($env:DB_USER) { $env:DB_USER } else { "postgres" }
$DB_PASSWORD = if ($env:DB_PASSWORD) { $env:DB_PASSWORD } else { "" }
$DB_NAME = if ($env:DB_NAME) { $env:DB_NAME } else { "" }

# Validate DB_NAME is set
if ([string]::IsNullOrEmpty($DB_NAME)) {
    Write-Host "ERROR: DB_NAME is not set. Please set it in .env or as an environment variable." -ForegroundColor Red
    exit 1
}

# SAFETY CHECK: Only allow databases ending with "_homolog"
if (-not $DB_NAME.EndsWith("_homolog")) {
    Write-Host "ERROR: Database name '$DB_NAME' does not end with '_homolog'." -ForegroundColor Red
    Write-Host "This script only runs on homolog databases for safety." -ForegroundColor Red
    exit 1
}

Write-Host "============================================" -ForegroundColor Green
Write-Host "  Clear Message History Script" -ForegroundColor Green
Write-Host "============================================" -ForegroundColor Green
Write-Host ""
Write-Host "Database: " -NoNewline; Write-Host $DB_NAME -ForegroundColor Yellow
Write-Host "Host: " -NoNewline; Write-Host "${DB_HOST}:${DB_PORT}" -ForegroundColor Yellow
Write-Host "User: " -NoNewline; Write-Host $DB_USER -ForegroundColor Yellow
Write-Host ""

# Confirmation prompt
Write-Host "WARNING: This will permanently delete ALL conversation messages!" -ForegroundColor Red
$CONFIRM = Read-Host "Are you sure you want to continue? (type 'yes' to confirm)"

if ($CONFIRM -ne "yes") {
    Write-Host "Aborted." -ForegroundColor Yellow
    exit 0
}

# Set PGPASSWORD for psql
$env:PGPASSWORD = $DB_PASSWORD

Write-Host ""
Write-Host "Counting existing messages..." -ForegroundColor Yellow

# Count messages before deletion
try {
    $COUNT_BEFORE = & psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -A -c "SELECT COUNT(*) FROM conversation_messages WHERE deleted_at IS NULL;" 2>$null
    $COUNT_BEFORE = $COUNT_BEFORE.Trim()
} catch {
    $COUNT_BEFORE = "0"
}

Write-Host "Found " -NoNewline; Write-Host $COUNT_BEFORE -ForegroundColor Yellow -NoNewline; Write-Host " messages to delete."

if ($COUNT_BEFORE -eq "0") {
    Write-Host "No messages to delete. Database is already empty." -ForegroundColor Green
    exit 0
}

Write-Host ""
Write-Host "Deleting all conversation messages..." -ForegroundColor Yellow

# Delete all messages (hard delete for clean slate)
& psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -c @"
-- Delete all conversation messages (hard delete)
DELETE FROM conversation_messages;

-- Optionally, also delete soft-deleted records
-- This ensures a completely clean slate
"@

Write-Host ""
Write-Host "Successfully deleted $COUNT_BEFORE messages from conversation_messages table." -ForegroundColor Green
Write-Host ""

# Verify deletion
try {
    $COUNT_AFTER = & psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -A -c "SELECT COUNT(*) FROM conversation_messages;" 2>$null
    $COUNT_AFTER = $COUNT_AFTER.Trim()
} catch {
    $COUNT_AFTER = "unknown"
}

Write-Host "Remaining messages: " -NoNewline; Write-Host $COUNT_AFTER -ForegroundColor Green
Write-Host ""
Write-Host "Done!" -ForegroundColor Green
