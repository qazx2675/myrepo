#!/usr/bin/env bash
# Automated E2E test suite for change_ilo_ip.sh using tests/mock_ilorest
# Pure Bash (no jq/python3), safe in isolated temporary directories.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET_SCRIPT="$REPO_ROOT/change_ilo_ip.sh"
MOCK_ILOREST="$REPO_ROOT/tests/mock_ilorest"

PASS_COUNT=0
FAIL_COUNT=0
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/ilo_e2e_tests.XXXXXX")"

cleanup_all() {
    rm -rf "$TEST_ROOT" >/dev/null 2>&1 || true
}
trap cleanup_all EXIT INT TERM

pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    printf '[PASS] %s\n' "$1"
}

fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    printf '[FAIL] %s\n' "$1" >&2
}

assert_eq() {
    local expected="$1"
    local actual="$2"
    local msg="$3"
    if [[ "$expected" == "$actual" ]]; then
        pass "$msg"
    else
        fail "$msg (expected='$expected', actual='$actual')"
    fi
}

assert_contains() {
    local haystack="$1"
    local needle="$2"
    local msg="$3"
    if printf '%s' "$haystack" | grep -Fq "$needle"; then
        pass "$msg"
    else
        fail "$msg (missing: '$needle')"
    fi
}

assert_file_contains() {
    local file="$1"
    local needle="$2"
    local msg="$3"
    if [[ -f "$file" ]] && grep -Fq "$needle" "$file"; then
        pass "$msg"
    else
        fail "$msg (file='$file', missing='$needle')"
    fi
}

assert_file_not_contains() {
    local file="$1"
    local needle="$2"
    local msg="$3"
    if [[ -f "$file" ]] && ! grep -Fq "$needle" "$file"; then
        pass "$msg"
    else
        fail "$msg (file='$file', unexpected='$needle')"
    fi
}

new_sandbox() {
    local name="$1"
    local sdir="$TEST_ROOT/$name"
    rm -rf "$sdir"
    mkdir -p "$sdir/work" "$sdir/state"
    MOCK_ILOREST_STATE_DIR="$sdir/state" "$MOCK_ILOREST" --init >/dev/null
    printf '%s' "$sdir"
}

printf '=== HPE iLO IP Changer E2E Test Suite ===\n'

# ------------------------------------------------------------------------------
# 0. Bash syntax check
# ------------------------------------------------------------------------------
if bash -n "$TARGET_SCRIPT" "$MOCK_ILOREST" "$SCRIPT_DIR/run_tests.sh"; then
    pass "Syntax check (bash -n)"
else
    fail "Syntax check (bash -n)"
fi

if [[ ! -x "$MOCK_ILOREST" ]]; then
    chmod +x "$MOCK_ILOREST" 2>/dev/null || true
fi

# ------------------------------------------------------------------------------
# Scenario 1: --check pre-check mode (iLO 4 / 5 / 6, Dedicated / Shared, Oem Hp/Hpe)
# ------------------------------------------------------------------------------
printf '\n--- Scenario 1: --check pre-check mode ---\n'
S1="$(new_sandbox s1)"
cat > "$S1/work/list.txt" <<'EOF'
# Multi-generation test
10.10.10.101	10.20.30.101	30
10.10.10.102 10.20.30.102 30
10.10.10.103 10.20.40.15 0
EOF

S1_OUT="$(
    cd "$S1/work" && \
    printf '\n\n' | \
    env -u ILO_USER -u ILO_PASS -u ILOREST_BIN \
        MOCK_ILOREST_STATE_DIR="$S1/state" \
        ILO_USER="Administrator" \
        ILO_PASS="Pass123!" \
        bash "$TARGET_SCRIPT" --check --ilorest-bin "$MOCK_ILOREST" -f "$S1/work/list.txt" 2>&1
)"
S1_RC=$?

assert_eq "0" "$S1_RC" "Scenario 1: --check exits 0"
assert_contains "$S1_OUT" "iLO 5 v3.01" "Scenario 1: detects iLO 5"
assert_contains "$S1_OUT" "iLO 4 v2.82" "Scenario 1: detects iLO 4"
assert_contains "$S1_OUT" "iLO 6 v1.55" "Scenario 1: detects iLO 6"
assert_contains "$S1_OUT" "/redfish/v1/Managers/1/EthernetInterfaces/2/ (OEM 키: Hp)" "Scenario 1: iLO 4 selects Shared /2/ with Oem.Hp"
assert_contains "$S1_OUT" "/redfish/v1/Managers/1/EthernetInterfaces/1/ (OEM 키: Hpe)" "Scenario 1: iLO 5/6 select /1/ with Oem.Hpe"
assert_file_not_contains "$S1/state/call_history.log" "RAWPATCH" "Scenario 1: rawpatch not called"
assert_file_not_contains "$S1/state/call_history.log" "ILORESET" "Scenario 1: iloreset not called"

if ! ls -d "$S1/work/logs/backup_"* >/dev/null 2>&1; then
    pass "Scenario 1: no backup directory created in check mode"
else
    fail "Scenario 1: unexpected backup directory created in check mode"
fi

if [[ -d "$S1/state/hosts/10.10.10.101" && -d "$S1/state/hosts/10.10.10.102" && -d "$S1/state/hosts/10.10.10.103" ]]; then
    pass "Scenario 1: state not mutated"
else
    fail "Scenario 1: state mutated unexpectedly"
fi

# ------------------------------------------------------------------------------
# Scenario 2: Interactive pipe flow (default netmask, auto .1 GW, VLAN enable & disable)
# ------------------------------------------------------------------------------
printf '\n--- Scenario 2: Interactive pipe flow ---\n'
S2="$(new_sandbox s2)"
cat > "$S2/work/list.txt" <<'EOF'
# Interactive test list
EOF
printf '10.10.10.101 10.20.30.101 30\r\n10.10.10.102 10.20.30.102 30\n10.10.10.103 10.20.40.15 off\n' >> "$S2/work/list.txt"

S2_OUT="$(
    cd "$S2/work" && \
    printf 'Administrator\nPass123!\n\n\ny\n' | \
    env -u ILO_USER -u ILO_PASS \
        MOCK_ILOREST_STATE_DIR="$S2/state" \
        ILOREST_BIN="$MOCK_ILOREST" \
        bash "$TARGET_SCRIPT" 2>&1
)"
S2_RC=$?

assert_eq "0" "$S2_RC" "Scenario 2: interactive apply exits 0"
assert_file_contains "$S2/state/call_history.log" "RAWPATCH 10.10.10.101 uri=/redfish/v1/Managers/1/EthernetInterfaces/1/ ip=10.20.30.101 mask=255.255.255.0 gw=10.20.30.1 dhcp=false oem_dhcp=false vlan_enable=true vlan_id=30" "Scenario 2: 10.10.10.101 patched with default mask, auto .1 GW, VLAN 30"
assert_file_contains "$S2/state/call_history.log" "RAWPATCH 10.10.10.102 uri=/redfish/v1/Managers/1/EthernetInterfaces/2/ ip=10.20.30.102 mask=255.255.255.0 gw=10.20.30.1 dhcp=false oem_dhcp=false vlan_enable=true vlan_id=30" "Scenario 2: 10.10.10.102 patched /2/ with Oem.Hp disabled"
assert_file_contains "$S2/state/call_history.log" "RAWPATCH 10.10.10.103 uri=/redfish/v1/Managers/1/EthernetInterfaces/1/ ip=10.20.40.15 mask=255.255.255.0 gw=10.20.40.1 dhcp=false oem_dhcp=false vlan_enable=false vlan_id=0" "Scenario 2: 10.10.10.103 patched with auto .1 GW and VLAN disabled"
assert_file_contains "$S2/state/patches/10.10.10.103.json" '"VLANEnable": false' "Scenario 2: patch JSON for 10.10.10.103 sets VLANEnable: false"

# Verify RAWPATCH -> ILORESET order per server
S2_ORDER_OK=1
for ip in 10.10.10.101 10.10.10.102 10.10.10.103; do
    patch_ln="$(grep -n "^RAWPATCH ${ip} " "$S2/state/call_history.log" | head -n1 | cut -d: -f1)"
    reset_ln="$(grep -n "^ILORESET ${ip}$" "$S2/state/call_history.log" | head -n1 | cut -d: -f1)"
    if [[ -z "$patch_ln" || -z "$reset_ln" || "$patch_ln" -ge "$reset_ln" ]]; then
        S2_ORDER_OK=0
    fi
done
assert_eq "1" "$S2_ORDER_OK" "Scenario 2: rawpatch precedes iloreset for each host"

# Verify backup JSONs and result log
if ls "$S2/work/logs/backup_"*/10.10.10.101.json >/dev/null 2>&1 && \
   ls "$S2/work/logs/backup_"*/10.10.10.102.json >/dev/null 2>&1 && \
   ls "$S2/work/logs/backup_"*/10.10.10.103.json >/dev/null 2>&1; then
    pass "Scenario 2: backup JSONs created for all 3 hosts"
else
    fail "Scenario 2: missing backup JSON files"
fi

if ls "$S2/work/logs/result_"*.log >/dev/null 2>&1; then
    S2_LOG_FILE="$(ls "$S2/work/logs/result_"*.log | head -n1)"
    assert_file_contains "$S2_LOG_FILE" "전체: 3 대 | 성공(완료): 3 대 | 실패: 0 대" "Scenario 2: result log summary counts accurate"
else
    fail "Scenario 2: result log not created"
fi

# ------------------------------------------------------------------------------
# Scenario 3: Interactive 'c' mode selection switches cleanly to check mode
# ------------------------------------------------------------------------------
printf '\n--- Scenario 3: Interactive c mode selection ---\n'
S3="$(new_sandbox s3)"
cat > "$S3/work/list.txt" <<'EOF'
10.10.10.101 10.20.30.101 30
10.10.10.103 10.20.40.15 0
EOF

S3_OUT="$(
    cd "$S3/work" && \
    printf 'Administrator\nPass123!\n\n\nc\n' | \
    env -u ILO_USER -u ILO_PASS \
        MOCK_ILOREST_STATE_DIR="$S3/state" \
        ILOREST_BIN="$MOCK_ILOREST" \
        bash "$TARGET_SCRIPT" 2>&1
)"
S3_RC=$?

assert_eq "0" "$S3_RC" "Scenario 3: interactive 'c' check mode exits 0"
assert_contains "$S3_OUT" "사전 점검(Dry-run)" "Scenario 3: switches to Dry-run mode"
assert_contains "$S3_OUT" "점검완료" "Scenario 3: outputs 점검완료"
assert_file_not_contains "$S3/state/call_history.log" "RAWPATCH" "Scenario 3: rawpatch not called"
assert_file_not_contains "$S3/state/call_history.log" "ILORESET" "Scenario 3: iloreset not called"

# ------------------------------------------------------------------------------
# Scenario 4: Partial failure resilience (auth fail + patch fail + valid host)
# ------------------------------------------------------------------------------
printf '\n--- Scenario 4: Partial failure resilience ---\n'
S4="$(new_sandbox s4)"
cat > "$S4/work/list.txt" <<'EOF'
10.10.10.102 10.20.30.102 30
10.0.0.1 10.30.0.1 10
10.10.10.103 10.20.40.15 0
10.10.10.101 10.20.30.101 30
EOF

S4_OUT="$(
    cd "$S4/work" && \
    env -u ILO_USER -u ILO_PASS \
        MOCK_ILOREST_STATE_DIR="$S4/state" \
        MOCK_AUTH_FAIL_IPS="10.10.10.102" \
        MOCK_UNREACHABLE_IPS="10.0.0.1" \
        MOCK_PATCH_FAIL_IPS="10.10.10.103" \
        ILO_USER="Administrator" \
        ILO_PASS="Pass123!" \
        bash "$TARGET_SCRIPT" -y --ilorest-bin "$MOCK_ILOREST" </dev/null 2>&1
)"
S4_RC=$?

assert_eq "1" "$S4_RC" "Scenario 4: exits 1 when at least one host fails"
assert_file_contains "$S4/state/call_history.log" "LOGIN_AUTH_FAIL 10.10.10.102" "Scenario 4: auth failure recorded for 10.10.10.102"
assert_file_contains "$S4/state/call_history.log" "LOGIN_UNREACHABLE 10.0.0.1" "Scenario 4: unreachable failure recorded for 10.0.0.1"
assert_file_contains "$S4/state/call_history.log" "RAWPATCH_FAIL 10.10.10.103" "Scenario 4: rawpatch failure recorded for 10.10.10.103"
assert_file_not_contains "$S4/state/call_history.log" "ILORESET 10.10.10.103" "Scenario 4: iloreset NOT called for patch-failed 10.10.10.103"
assert_file_contains "$S4/state/call_history.log" "RAWPATCH 10.10.10.101" "Scenario 4: continues and patches 10.10.10.101"
assert_file_contains "$S4/state/call_history.log" "ILORESET 10.10.10.101" "Scenario 4: resets 10.10.10.101"
assert_contains "$S4_OUT" "전체: 4 대 | 성공(완료): 1 대 | 실패: 3 대" "Scenario 4: summary report counts 4 total / 1 success / 3 fail"

# ------------------------------------------------------------------------------
# Scenario 5: CLI overrides (--netmask 255.255.254.0 --gw 10.20.30.254 -y)
# ------------------------------------------------------------------------------
printf '\n--- Scenario 5: CLI overrides ---\n'
S5="$(new_sandbox s5)"
cat > "$S5/work/list.txt" <<'EOF'
10.10.10.101 10.20.30.101 30
10.10.10.103 10.20.31.15 0
EOF

S5_OUT="$(
    cd "$S5/work" && \
    MOCK_ILOREST_STATE_DIR="$S5/state" \
    ILO_USER="Administrator" \
    ILO_PASS="Pass123!" \
    bash "$TARGET_SCRIPT" --netmask 255.255.254.0 --gw 10.20.30.254 -y --ilorest-bin "$MOCK_ILOREST" </dev/null 2>&1
)"
S5_RC=$?

assert_eq "0" "$S5_RC" "Scenario 5: CLI override run exits 0"
assert_file_contains "$S5/state/call_history.log" "RAWPATCH 10.10.10.101 uri=/redfish/v1/Managers/1/EthernetInterfaces/1/ ip=10.20.30.101 mask=255.255.254.0 gw=10.20.30.254" "Scenario 5: custom netmask & fixed GW applied to .101"
assert_file_contains "$S5/state/call_history.log" "RAWPATCH 10.10.10.103 uri=/redfish/v1/Managers/1/EthernetInterfaces/1/ ip=10.20.31.15 mask=255.255.254.0 gw=10.20.30.254" "Scenario 5: custom netmask & fixed GW applied to .103"

# ------------------------------------------------------------------------------
# Scenario 6: Invalid list.txt validation + MOCK_PRELINK_ERR=1 recovery guide
# ------------------------------------------------------------------------------
printf '\n--- Scenario 6: Invalid list.txt and prelink detection ---\n'
S6="$(new_sandbox s6)"

cat > "$S6/work/bad_ip.txt" <<'EOF'
999.1.1.1 10.20.30.101 30
EOF

S6A_OUT="$(
    cd "$S6/work" && \
    MOCK_ILOREST_STATE_DIR="$S6/state" \
    ILO_USER="Administrator" \
    ILO_PASS="Pass123!" \
    bash "$TARGET_SCRIPT" -y -f "$S6/work/bad_ip.txt" --ilorest-bin "$MOCK_ILOREST" </dev/null 2>&1
)"
S6A_RC=$?
assert_eq "1" "$S6A_RC" "Scenario 6a: invalid IP 999.1.1.1 rejected with exit 1"
assert_file_not_contains "$S6/state/call_history.log" "LOGIN" "Scenario 6a: no login attempted on invalid IP"

cat > "$S6/work/bad_vlan.txt" <<'EOF'
10.10.10.101 10.20.30.101 5000
EOF

S6B_OUT="$(
    cd "$S6/work" && \
    MOCK_ILOREST_STATE_DIR="$S6/state" \
    ILO_USER="Administrator" \
    ILO_PASS="Pass123!" \
    bash "$TARGET_SCRIPT" -y -f "$S6/work/bad_vlan.txt" --ilorest-bin "$MOCK_ILOREST" </dev/null 2>&1
)"
S6B_RC=$?
assert_eq "1" "$S6B_RC" "Scenario 6b: invalid VLAN 5000 rejected with exit 1"
assert_file_not_contains "$S6/state/call_history.log" "LOGIN" "Scenario 6b: no login attempted on invalid VLAN"

cat > "$S6/work/valid.txt" <<'EOF'
10.10.10.101 10.20.30.101 30
EOF

S6C_OUT="$(
    cd "$S6/work" && \
    MOCK_ILOREST_STATE_DIR="$S6/state" \
    MOCK_PRELINK_ERR=1 \
    ILO_USER="Administrator" \
    ILO_PASS="Pass123!" \
    bash "$TARGET_SCRIPT" -y -f "$S6/work/valid.txt" --ilorest-bin "$MOCK_ILOREST" </dev/null 2>&1
)"
S6C_RC=$?
assert_eq "1" "$S6C_RC" "Scenario 6c: MOCK_PRELINK_ERR=1 exits 1"
assert_contains "$S6C_OUT" "/etc/prelink.conf" "Scenario 6c: prints /etc/prelink.conf recovery guide"
assert_contains "$S6C_OUT" "prelink -ua" "Scenario 6c: prints prelink -ua recovery guide"

# ------------------------------------------------------------------------------
# Scenario 7: IP boundary matching (10.0.0.1 vs 10.0.0.10)
# ------------------------------------------------------------------------------
printf '\n--- Scenario 7: IP boundary matching (10.0.0.1 vs 10.0.0.10) ---\n'
S7="$(new_sandbox s7)"
cat > "$S7/work/list.txt" <<'EOF'
10.0.0.1 10.30.0.1 10
10.0.0.10 10.30.0.10 20
EOF

S7_OUT="$(
    cd "$S7/work" && \
    MOCK_ILOREST_STATE_DIR="$S7/state" \
    ILO_USER="Administrator" \
    ILO_PASS="Pass123!" \
    bash "$TARGET_SCRIPT" -y --ilorest-bin "$MOCK_ILOREST" </dev/null 2>&1
)"
S7_RC=$?

assert_eq "0" "$S7_RC" "Scenario 7: IP boundary matching run exits 0"
assert_file_contains "$S7/state/call_history.log" "RAWPATCH 10.0.0.1 uri=/redfish/v1/Managers/1/EthernetInterfaces/2/ ip=10.30.0.1" "Scenario 7: 10.0.0.1 selects /2/ (ignores /1/ with 10.0.0.10)"
assert_file_contains "$S7/state/call_history.log" "RAWPATCH 10.0.0.10 uri=/redfish/v1/Managers/1/EthernetInterfaces/2/ ip=10.30.0.10" "Scenario 7: 10.0.0.10 selects /2/ (ignores /1/ with 10.0.0.1)"

# ------------------------------------------------------------------------------
# Final test report
# ------------------------------------------------------------------------------
printf '\n=== Test Summary: %d passed, %d failed ===\n' "$PASS_COUNT" "$FAIL_COUNT"
if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
fi
exit 0
