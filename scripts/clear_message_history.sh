#!/bin/bash
#
# clear_message_history.sh
# Deletes all conversation message history from the database.
# 
# SAFETY: Only runs if the database name ends with "_homolog"
#
# Usage:
#   ./scripts/clear_message_history.sh
#   
# Or with custom env file:
#   DB_HOST=localhost DB_PORT=5433 DB_USER=postgres DB_PASSWORD=xxx DB_NAME=vozko_homolog ./scripts/clear_message_history.sh
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Load .env file if it exists
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

if [ -f "$PROJECT_ROOT/.env" ]; then
    echo -e "${YELLOW}Loading .env from $PROJECT_ROOT/.env${NC}"
    set -a
    source "$PROJECT_ROOT/.env"
    set +a
fi

# Database connection settings (from env or defaults)
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-postgres}"
DB_PASSWORD="${DB_PASSWORD:-}"
DB_NAME="${DB_NAME:-}"

# Validate DB_NAME is set
if [ -z "$DB_NAME" ]; then
    echo -e "${RED}ERROR: DB_NAME is not set. Please set it in .env or as an environment variable.${NC}"
    exit 1
fi

# SAFETY CHECK: Only allow databases ending with "-homolog"
if [[ ! "$DB_NAME" =~ -homolog$ ]]; then
    echo -e "${RED}ERROR: Database name '$DB_NAME' does not end with '-homolog'.${NC}"
    echo -e "${RED}This script only runs on homolog databases for safety.${NC}"
    exit 1
fi

echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN}  Clear Message History Script${NC}"
echo -e "${GREEN}============================================${NC}"
echo ""
echo -e "Database: ${YELLOW}$DB_NAME${NC}"
echo -e "Host: ${YELLOW}$DB_HOST:$DB_PORT${NC}"
echo -e "User: ${YELLOW}$DB_USER${NC}"
echo ""

# Confirmation prompt
echo -e "${RED}WARNING: This will permanently delete ALL conversation messages!${NC}"
read -p "Are you sure you want to continue? (type 'yes' to confirm): " CONFIRM

if [ "$CONFIRM" != "yes" ]; then
    echo -e "${YELLOW}Aborted.${NC}"
    exit 0
fi

# Build connection string
export PGPASSWORD="$DB_PASSWORD"

echo ""
echo -e "${YELLOW}Counting existing messages...${NC}"

# Count messages before deletion
COUNT_BEFORE=$(psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -t -A -c "SELECT COUNT(*) FROM conversation_messages WHERE deleted_at IS NULL;" 2>/dev/null || echo "0")

echo -e "Found ${YELLOW}$COUNT_BEFORE${NC} messages to delete."

if [ "$COUNT_BEFORE" = "0" ]; then
    echo -e "${GREEN}No messages to delete. Database is already empty.${NC}"
    exit 0
fi

echo ""
echo -e "${YELLOW}Deleting all conversation messages...${NC}"

# Delete all messages (hard delete for clean slate)
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c "
-- Delete all conversation messages (hard delete)
DELETE FROM conversation_messages;

-- Optionally, also delete soft-deleted records
-- This ensures a completely clean slate
"

echo ""
echo -e "${GREEN}✓ Successfully deleted $COUNT_BEFORE messages from conversation_messages table.${NC}"
echo ""

# Verify deletion
COUNT_AFTER=$(psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -t -A -c "SELECT COUNT(*) FROM conversation_messages;" 2>/dev/null || echo "unknown")

echo -e "Remaining messages: ${GREEN}$COUNT_AFTER${NC}"
echo ""
echo -e "${GREEN}Done!${NC}"
