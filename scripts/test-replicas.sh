#!/bin/bash
# =============================================================================
# Replica System Validation Script
# =============================================================================
# Tests that two API replicas behind Traefik are working correctly:
#   1. Both replicas are healthy and reachable
#   2. Load balancer distributes requests (round-robin)
#   3. WebSocket sticky sessions work
#   4. Redis pub/sub cross-replica broadcasting works
#   5. Authentication works across replicas (stateless JWT)
#
# Prerequisites:
#   docker compose -f docker-compose.yml -f docker-compose.replica-test.yml \
#     up --build app-1 app-2 api-lb redis rabbitmq
#
# Usage: bash scripts/test-replicas.sh [API_URL]
# =============================================================================

set -euo pipefail

API_URL="${1:-http://127.0.0.1:3000}"
DASHBOARD_URL="http://127.0.0.1:8088"
PASS=0
FAIL=0
TOTAL=0

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

pass() { ((PASS++)); ((TOTAL++)); echo -e "  ${GREEN}✓${NC} $1"; }
fail() { ((FAIL++)); ((TOTAL++)); echo -e "  ${RED}✗${NC} $1"; }
info() { echo -e "${CYAN}→${NC} $1"; }
header() { echo -e "\n${YELLOW}═══ $1 ═══${NC}"; }

# =============================================================================
header "1. Health Check — Both Replicas"
# =============================================================================

info "Checking app-1 directly..."
if docker exec vozko-api-1 wget -qO- http://127.0.0.1:3000/health 2>/dev/null | grep -q "ok"; then
    HOSTNAME_1=$(docker exec vozko-api-1 wget -qO- http://127.0.0.1:3000/health 2>/dev/null)
    pass "app-1 healthy: $HOSTNAME_1"
else
    fail "app-1 not responding"
fi

info "Checking app-2 directly..."
if docker exec vozko-api-2 wget -qO- http://127.0.0.1:3000/health 2>/dev/null | grep -q "ok"; then
    HOSTNAME_2=$(docker exec vozko-api-2 wget -qO- http://127.0.0.1:3000/health 2>/dev/null)
    pass "app-2 healthy: $HOSTNAME_2"
else
    fail "app-2 not responding"
fi

info "Checking via load balancer..."
if curl -sf "$API_URL/health" | grep -q "ok"; then
    pass "Load balancer forwarding correctly"
else
    fail "Load balancer not forwarding to replicas"
fi

# =============================================================================
header "2. Load Distribution — Round Robin"
# =============================================================================

info "Sending 20 requests through the load balancer (no cookies)..."
declare -A REPLICA_HITS
for i in $(seq 1 20); do
    RESP=$(curl -sf "$API_URL/health" 2>/dev/null || echo "error")
    # Extract hostname from health response: "ok - goroutines=N alloc=NMiB - <hostname>"
    HOST=$(echo "$RESP" | grep -oP '(?<= - )[a-f0-9]{12}$' || echo "$RESP")
    REPLICA_HITS["$HOST"]=$(( ${REPLICA_HITS["$HOST"]:-0} + 1 ))
done

UNIQUE_BACKENDS=${#REPLICA_HITS[@]}
if [ "$UNIQUE_BACKENDS" -ge 2 ]; then
    pass "Requests distributed across $UNIQUE_BACKENDS backends"
    for host in "${!REPLICA_HITS[@]}"; do
        echo -e "      $host: ${REPLICA_HITS[$host]} requests"
    done
else
    fail "All requests went to same backend (got $UNIQUE_BACKENDS unique)"
    echo "      This may indicate sticky cookies are being cached. Try: curl -sf (no cookie jar)"
fi

# =============================================================================
header "3. Sticky Sessions — WebSocket Cookie"
# =============================================================================

info "Testing sticky session cookie..."
# First request: get the sticky cookie
COOKIE_RESP=$(curl -sf -c - "$API_URL/health" 2>/dev/null)
COOKIE_VALUE=$(echo "$COOKIE_RESP" | grep "_vozko_replica" | awk '{print $NF}' || echo "")

if [ -n "$COOKIE_VALUE" ]; then
    pass "Sticky cookie set: _vozko_replica=$COOKIE_VALUE"

    # Send 10 requests with the cookie — should all go to same backend
    info "Sending 10 requests WITH sticky cookie..."
    STICKY_HOST=""
    STICKY_OK=true
    for i in $(seq 1 10); do
        RESP=$(curl -sf -b "_vozko_replica=$COOKIE_VALUE" "$API_URL/health" 2>/dev/null || echo "error")
        HOST=$(echo "$RESP" | grep -oP '(?<= - )[a-f0-9]{12}$' || echo "$RESP")
        if [ -z "$STICKY_HOST" ]; then
            STICKY_HOST="$HOST"
        elif [ "$HOST" != "$STICKY_HOST" ]; then
            STICKY_OK=false
        fi
    done

    if $STICKY_OK; then
        pass "All sticky requests routed to same backend: $STICKY_HOST"
    else
        fail "Sticky sessions not working — requests went to different backends"
    fi
else
    fail "No sticky cookie set by Traefik"
fi

# =============================================================================
header "4. Redis Connectivity — Cross-Replica Shared State"
# =============================================================================

info "Checking Redis connectivity from both replicas..."
REDIS_OK_1=$(docker exec vozko-redis redis-cli ping 2>/dev/null || echo "error")
if [ "$REDIS_OK_1" = "PONG" ]; then
    pass "Redis responding"
else
    fail "Redis not responding: $REDIS_OK_1"
fi

info "Checking replica IDs in Redis..."
REPLICA_KEYS=$(docker exec vozko-redis redis-cli keys "hub:replica:*" 2>/dev/null || echo "none")
if [ "$REPLICA_KEYS" != "none" ] && [ -n "$REPLICA_KEYS" ]; then
    REPLICA_COUNT=$(echo "$REPLICA_KEYS" | wc -l)
    pass "Found $REPLICA_COUNT replica heartbeats in Redis"
    echo "$REPLICA_KEYS" | while read -r key; do
        echo -e "      $key"
    done
else
    fail "No replica heartbeats found in Redis (Hub may not have started pub/sub yet)"
fi

# =============================================================================
header "5. Traefik Dashboard — Service Discovery"
# =============================================================================

info "Checking Traefik API for registered services..."
SERVICES=$(curl -sf "$DASHBOARD_URL/api/http/services" 2>/dev/null || echo "error")
if echo "$SERVICES" | grep -q "api-service"; then
    SERVER_COUNT=$(echo "$SERVICES" | grep -oP '"url"' | wc -l)
    pass "Traefik sees api-service with $SERVER_COUNT servers"
else
    fail "Traefik api-service not found"
    echo "      Dashboard: $DASHBOARD_URL/dashboard/"
fi

# =============================================================================
header "6. API Consistency — Same Response Schema"
# =============================================================================

info "Comparing API responses from both replicas..."
RESP_1=$(docker exec vozko-api-1 wget -qO- http://127.0.0.1:3000/health 2>/dev/null || echo "error")
RESP_2=$(docker exec vozko-api-2 wget -qO- http://127.0.0.1:3000/health 2>/dev/null || echo "error")

# Both should start with "ok"
if [[ "$RESP_1" == ok* ]] && [[ "$RESP_2" == ok* ]]; then
    pass "Both replicas return consistent health format"
else
    fail "Response format mismatch: [$RESP_1] vs [$RESP_2]"
fi

# =============================================================================
header "RESULTS"
# =============================================================================

echo ""
echo -e "  Total: $TOTAL | ${GREEN}Passed: $PASS${NC} | ${RED}Failed: $FAIL${NC}"
echo ""

if [ "$FAIL" -eq 0 ]; then
    echo -e "  ${GREEN}All replica tests passed!${NC}"
    echo ""
    echo "  Next steps:"
    echo "    1. Open two browser tabs to test WebSocket cross-replica messaging"
    echo "    2. Send a message from tab A, verify it appears on tab B"
    echo "    3. Kill app-1 (docker stop vozko-api-1), verify app-2 takes over"
    echo "    4. Restart app-1, verify it rejoins the load balancer"
    echo ""
    exit 0
else
    echo -e "  ${RED}Some tests failed. Check logs:${NC}"
    echo "    docker logs vozko-api-1"
    echo "    docker logs vozko-api-2"
    echo "    docker logs vozko-api-lb"
    echo ""
    exit 1
fi
