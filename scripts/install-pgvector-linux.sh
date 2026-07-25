#!/bin/bash
#
# install-pgvector-linux.sh
# Installs pgvector extension on PostgreSQL for Linux (Debian/Ubuntu/RHEL/CentOS)
#
# Usage:
#   sudo ./install-pgvector-linux.sh [OPTIONS]
#
# Options:
#   -v, --pg-version    PostgreSQL major version (default: auto-detect)
#   -d, --database      Database name to enable pgvector (default: vozko-homolog)
#   -u, --user          PostgreSQL user (default: postgres)
#   -h, --help          Show this help message
#
# Examples:
#   sudo ./install-pgvector-linux.sh
#   sudo ./install-pgvector-linux.sh -v 17 -d mydb -u myuser
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Default values
PG_VERSION=""
DATABASE="vozko-homolog"
PG_USER="postgres"
PGVECTOR_VERSION="0.8.0"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -v|--pg-version)
            PG_VERSION="$2"
            shift 2
            ;;
        -d|--database)
            DATABASE="$2"
            shift 2
            ;;
        -u|--user)
            PG_USER="$2"
            shift 2
            ;;
        -h|--help)
            head -30 "$0" | tail -25
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

echo -e "${CYAN}========================================"
echo " pgvector Installation for Linux"
echo -e "========================================${NC}"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Error: This script must be run as root (sudo)${NC}"
    exit 1
fi

# Detect OS
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS=$ID
        OS_VERSION=$VERSION_ID
    elif [ -f /etc/redhat-release ]; then
        OS="rhel"
    else
        echo -e "${RED}Error: Unable to detect OS${NC}"
        exit 1
    fi
}

# Detect PostgreSQL version if not specified
detect_pg_version() {
    if [ -n "$PG_VERSION" ]; then
        return
    fi
    
    # Try pg_config first
    if command -v pg_config &> /dev/null; then
        PG_VERSION=$(pg_config --version | grep -oP '\d+' | head -1)
    # Try psql
    elif command -v psql &> /dev/null; then
        PG_VERSION=$(psql --version | grep -oP '\d+' | head -1)
    # Check common paths
    elif [ -d /usr/lib/postgresql ]; then
        PG_VERSION=$(ls /usr/lib/postgresql/ | sort -V | tail -1)
    else
        echo -e "${RED}Error: Cannot detect PostgreSQL version. Please specify with -v${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}[✓] Detected PostgreSQL version: $PG_VERSION${NC}"
}

# Install dependencies
install_dependencies() {
    echo -e "${YELLOW}[1/5] Installing build dependencies...${NC}"
    
    case $OS in
        ubuntu|debian)
            apt-get update -qq
            apt-get install -y -qq build-essential git postgresql-server-dev-$PG_VERSION > /dev/null 2>&1
            ;;
        rhel|centos|rocky|almalinux|fedora)
            if command -v dnf &> /dev/null; then
                dnf install -y -q gcc make git postgresql$PG_VERSION-devel > /dev/null 2>&1
            else
                yum install -y -q gcc make git postgresql$PG_VERSION-devel > /dev/null 2>&1
            fi
            ;;
        *)
            echo -e "${RED}Error: Unsupported OS: $OS${NC}"
            exit 1
            ;;
    esac
    
    echo -e "${GREEN}       Dependencies installed${NC}"
}

# Check if pgvector is available via package manager
check_package_manager() {
    echo -e "${YELLOW}[2/5] Checking for pgvector package...${NC}"
    
    case $OS in
        ubuntu|debian)
            # Check if postgresql-XX-pgvector is available
            if apt-cache show postgresql-$PG_VERSION-pgvector &> /dev/null; then
                echo -e "${GREEN}       Found package: postgresql-$PG_VERSION-pgvector${NC}"
                apt-get install -y -qq postgresql-$PG_VERSION-pgvector > /dev/null 2>&1
                return 0
            fi
            ;;
        rhel|centos|rocky|almalinux|fedora)
            # Check PGDG repo
            if command -v dnf &> /dev/null; then
                if dnf list pgvector_$PG_VERSION &> /dev/null; then
                    echo -e "${GREEN}       Found package: pgvector_$PG_VERSION${NC}"
                    dnf install -y -q pgvector_$PG_VERSION > /dev/null 2>&1
                    return 0
                fi
            fi
            ;;
    esac
    
    echo -e "${YELLOW}       Package not available, will build from source${NC}"
    return 1
}

# Build from source
build_from_source() {
    echo -e "${YELLOW}[3/5] Downloading pgvector v$PGVECTOR_VERSION...${NC}"
    
    TEMP_DIR=$(mktemp -d)
    cd "$TEMP_DIR"
    
    # Download source
    curl -sL "https://github.com/pgvector/pgvector/archive/refs/tags/v$PGVECTOR_VERSION.tar.gz" | tar xz
    cd "pgvector-$PGVECTOR_VERSION"
    
    echo -e "${GREEN}       Downloaded${NC}"
    
    echo -e "${YELLOW}[4/5] Building and installing pgvector...${NC}"
    
    # Find pg_config
    PG_CONFIG=""
    if command -v pg_config &> /dev/null; then
        PG_CONFIG=$(which pg_config)
    elif [ -f "/usr/lib/postgresql/$PG_VERSION/bin/pg_config" ]; then
        PG_CONFIG="/usr/lib/postgresql/$PG_VERSION/bin/pg_config"
    elif [ -f "/usr/pgsql-$PG_VERSION/bin/pg_config" ]; then
        PG_CONFIG="/usr/pgsql-$PG_VERSION/bin/pg_config"
    else
        echo -e "${RED}Error: pg_config not found${NC}"
        exit 1
    fi
    
    export PG_CONFIG
    
    # Build and install
    make -j$(nproc) > /dev/null 2>&1
    make install > /dev/null 2>&1
    
    echo -e "${GREEN}       Build completed${NC}"
    
    # Cleanup
    cd /
    rm -rf "$TEMP_DIR"
}

# Enable extension in database
enable_extension() {
    echo -e "${YELLOW}[5/5] Enabling pgvector in database '$DATABASE'...${NC}"
    
    # Check if database exists
    if ! sudo -u postgres psql -lqt | cut -d \| -f 1 | grep -qw "$DATABASE"; then
        echo -e "${YELLOW}       Database '$DATABASE' does not exist, creating...${NC}"
        sudo -u postgres createdb "$DATABASE" -O "$PG_USER" 2>/dev/null || true
    fi
    
    # Enable extension
    if sudo -u postgres psql -d "$DATABASE" -c "CREATE EXTENSION IF NOT EXISTS vector;" 2>/dev/null; then
        echo -e "${GREEN}       pgvector extension enabled!${NC}"
    else
        echo -e "${YELLOW}       Could not enable extension automatically${NC}"
        echo -e "${YELLOW}       Run manually: CREATE EXTENSION IF NOT EXISTS vector;${NC}"
    fi
}

# Verify installation
verify_installation() {
    echo ""
    echo -e "${CYAN}Verifying installation...${NC}"
    
    VERSION=$(sudo -u postgres psql -d "$DATABASE" -t -c "SELECT extversion FROM pg_extension WHERE extname = 'vector';" 2>/dev/null | tr -d ' ')
    
    if [ -n "$VERSION" ]; then
        echo -e "${GREEN}[✓] pgvector v$VERSION is installed and enabled${NC}"
    else
        echo -e "${YELLOW}[!] Extension not found in database. Enable it with:${NC}"
        echo "    psql -U $PG_USER -d $DATABASE -c \"CREATE EXTENSION IF NOT EXISTS vector;\""
    fi
}

# Main execution
main() {
    detect_os
    detect_pg_version
    install_dependencies
    
    if ! check_package_manager; then
        build_from_source
    fi
    
    enable_extension
    verify_installation
    
    echo ""
    echo -e "${CYAN}========================================"
    echo -e "${GREEN} Installation Complete!"
    echo -e "${CYAN}========================================${NC}"
    echo ""
    echo "To test pgvector, run:"
    echo "  psql -U $PG_USER -d $DATABASE -c \"SELECT '[1,2,3]'::vector;\""
    echo ""
}

main
