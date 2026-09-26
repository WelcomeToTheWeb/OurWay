#!/bin/bash
#
# OurWay RMM Stack Verification Script
#
# This script verifies the full OurWay stack:
# 1. Checks server health
# 2. Registers a test user
# 3. Logs in and gets a JWT token
# 4. Lists devices
# 5. Registers an agent device
# 6. Sends a heartbeat
# 7. Sends metrics
# 8. Verifies device shows as online
#
# Usage: ./scripts/verify.sh [SERVER_URL]
# Default server URL: http://localhost:8080

set -e

SERVER_URL="${1:-http://localhost:8080}"
PASS=0
FAIL=0
TOTAL=0

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() {
    echo -e "  ${GREEN}✓ PASS${NC}: $1"
    PASS=$((PASS + 1))
    TOTAL=$((TOTAL + 1))
}

fail() {
    echo -e "  ${RED}✗ FAIL${NC}: $1"
    FAIL=$((FAIL + 1))
    TOTAL=$((TOTAL + 1))
}

step() {
    echo -e "\n${YELLOW}[Step $TOTAL] $1${NC}"
}

echo "========================================"
echo "  OurWay RMM Stack Verification"
echo "  Server: $SERVER_URL"
echo "========================================"

# Step 1: Health check
step "Checking server health"
HEALTH=$(curl -s -w "\n%{http_code}" "$SERVER_URL/health")
HEALTH_CODE=$(echo "$HEALTH" | tail -1)
HEALTH_BODY=$(echo "$HEALTH" | head -1)

if [ "$HEALTH_CODE" = "200" ] && echo "$HEALTH_BODY" | grep -q '"status":"ok"'; then
    pass "Health endpoint returns 200 OK"
else
    fail "Health endpoint returned $HEALTH_CODE: $HEALTH_BODY"
fi

# Step 2: Register test user
step "Registering test user"
USER_DATA=$(curl -s -w "\n%{http_code}" -X POST "$SERVER_URL/api/auth/register" \
    -H "Content-Type: application/json" \
    -d '{"username":"verify-test","email":"verify@test.com","password":"testpass123"}')
USER_CODE=$(echo "$USER_DATA" | tail -1)
USER_BODY=$(echo "$USER_DATA" | head -1)

# 201 = created, 500 = already exists (both acceptable for verification)
if [ "$USER_CODE" = "201" ] || [ "$USER_CODE" = "500" ]; then
    pass "User registration (status: $USER_CODE)"
else
    fail "User registration failed with status $USER_CODE: $USER_BODY"
fi

# Step 3: Login and get token
step "Logging in as test user"
LOGIN_DATA=$(curl -s -w "\n%{http_code}" -X POST "$SERVER_URL/api/auth/login" \
    -H "Content-Type: application/json" \
    -d '{"username":"verify-test","password":"testpass123"}')
LOGIN_CODE=$(echo "$LOGIN_DATA" | tail -1)
LOGIN_BODY=$(echo "$LOGIN_DATA" | head -1)

if [ "$LOGIN_CODE" != "200" ]; then
    fail "Login failed with status $LOGIN_CODE: $LOGIN_BODY"
else
    TOKEN=$(echo "$LOGIN_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['access_token'])" 2>/dev/null || \
            echo "$LOGIN_BODY" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
    if [ -n "$TOKEN" ]; then
        pass "Login successful, obtained JWT token"
    else
        fail "Login successful but could not extract token"
        TOKEN=""
    fi
fi

# Step 4: List devices (should be empty or have previous devices)
step "Listing devices"
DEVICES_DATA=$(curl -s -w "\n%{http_code}" "$SERVER_URL/api/devices" \
    -H "Authorization: Bearer $TOKEN")
DEVICES_CODE=$(echo "$DEVICES_DATA" | tail -1)

if [ "$DEVICES_CODE" = "200" ]; then
    pass "Device listing returned 200 OK"
else
    fail "Device listing failed with status $DEVICES_CODE"
fi

# Step 5: Register agent device
step "Registering agent device"
DEVICE_DATA=$(curl -s -w "\n%{http_code}" -X POST "$SERVER_URL/api/agent/register" \
    -H "Content-Type: application/json" \
    -d '{"name":"verify-agent","hostname":"verify-host","os":"linux","arch":"amd64","agent_version":"0.1.0"}')
DEVICE_CODE=$(echo "$DEVICE_DATA" | tail -1)
DEVICE_BODY=$(echo "$DEVICE_DATA" | head -1)

if [ "$DEVICE_CODE" != "201" ]; then
    fail "Device registration failed with status $DEVICE_CODE: $DEVICE_BODY"
else
    DEVICE_KEY=$(echo "$DEVICE_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['device_key'])" 2>/dev/null || \
                 echo "$DEVICE_BODY" | sed -n 's/.*"device_key":"\([^"]*\)".*/\1/p')
    DEVICE_ID=$(echo "$DEVICE_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['device']['id'])" 2>/dev/null || \
                echo "$DEVICE_BODY" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
    pass "Device registered with key: $DEVICE_KEY"
fi

# Step 6: Send heartbeat
step "Sending device heartbeat"
HB_DATA=$(curl -s -w "\n%{http_code}" -X POST "$SERVER_URL/api/agent/heartbeat" \
    -H "X-Device-Key: $DEVICE_KEY")
HB_CODE=$(echo "$HB_DATA" | tail -1)

if [ "$HB_CODE" = "200" ]; then
    pass "Heartbeat sent successfully"
else
    fail "Heartbeat failed with status $HB_CODE"
fi

# Step 7: Send metrics
step "Sending device metrics"
METRICS_DATA=$(curl -s -w "\n%{http_code}" -X POST "$SERVER_URL/api/agent/metrics" \
    -H "Content-Type: application/json" \
    -H "X-Device-Key: $DEVICE_KEY" \
    -d '{"cpu":45.5,"ram":55.2,"ram_used":4800000000,"ram_total":8589934592,"disk_usage":35.0,"disk_used":15000000000,"disk_total":42949672960,"net_in":1000000,"net_out":500000,"uptime":3600,"processes":120}')
METRICS_CODE=$(echo "$METRICS_DATA" | tail -1)

if [ "$METRICS_CODE" = "200" ]; then
    pass "Metrics sent successfully"
else
    fail "Metrics failed with status $METRICS_CODE"
fi

# Step 8: Verify device is online
step "Verifying device is online"
DEVICE_DETAIL=$(curl -s -w "\n%{http_code}" "$SERVER_URL/api/devices/$DEVICE_ID" \
    -H "Authorization: Bearer $TOKEN")
DETAIL_CODE=$(echo "$DEVICE_DETAIL" | tail -1)
DETAIL_BODY=$(echo "$DEVICE_DETAIL" | head -1)

if [ "$DETAIL_CODE" = "200" ]; then
    if echo "$DETAIL_BODY" | grep -q '"status":"online"'; then
        pass "Device shows as online"
    else
        fail "Device not showing as online: $DETAIL_BODY"
    fi
else
    fail "Failed to get device details (status: $DETAIL_CODE)"
fi

# Summary
echo ""
echo "========================================"
echo "  Verification Summary"
echo "========================================"
echo -e "  Total:  $TOTAL"
echo -e "  ${GREEN}Passed: $PASS${NC}"
echo -e "  ${RED}Failed: $FAIL${NC}"
echo "========================================"

if [ $FAIL -eq 0 ]; then
    echo -e "\n${GREEN}✓ All verification steps passed!${NC}"
    exit 0
else
    echo -e "\n${RED}✗ Some verification steps failed.${NC}"
    exit 1
fi
