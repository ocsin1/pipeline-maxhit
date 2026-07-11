#!/bin/bash
# Regression test for pipeline-maxhit
# Usage: ./test_regression.sh [MaaEnd_dir]
# Default MaaEnd dir: ../MaaEnd

set -euo pipefail
PIPELINE_MAXHIT="$(cd "$(dirname "$0")" && pwd)"
BIN="${PIPELINE_MAXHIT}/install/pipeline-maxhit.exe"
MAAEND="${1:-${PIPELINE_MAXHIT}/../MaaEnd}"
PIPELINE="${MAAEND}/assets/resource/pipeline"
TASKS="${MAAEND}/assets/tasks"

if [ ! -d "${PIPELINE}" ] || [ ! -d "${TASKS}" ]; then
    echo "SKIP: MaaEnd test data not found at ${MAAEND}"
    echo "Clone MaaEnd alongside this repo or pass its path: $0 <MaaEnd_dir>"
    exit 0
fi

PASS=0
FAIL=0

red()   { echo -e "\033[31m$*\033[0m"; }
green() { echo -e "\033[32m$*\033[0m"; }

assert() {
    local label="$1" expected="$2" actual="$3"
    if [ "$expected" = "$actual" ]; then
        green "  ✓ ${label}: ${expected}"
        PASS=$((PASS + 1))
    else
        red "  ✗ ${label}: expected ${expected}, got ${actual}"
        FAIL=$((FAIL + 1))
    fi
}

assert_ge() {
    local label="$1" min="$2" actual="$3"
    if [ "$actual" -ge "$min" ] 2>/dev/null; then
        green "  ✓ ${label}: ${actual} (>= ${min})"
        PASS=$((PASS + 1))
    else
        red "  ✗ ${label}: expected >= ${min}, got ${actual}"
        FAIL=$((FAIL + 1))
    fi
}

# Extract number after pattern like "可达: N"
extract_num() {
    local pattern="$1" text="$2"
    echo "${text}" | grep "${pattern}" | sed "s/.*${pattern} *//;s/ .*//" | head -1
}

# ---------- build ----------
echo "=== Building ==="
cd "${PIPELINE_MAXHIT}"
go build -o install/pipeline-maxhit.exe . || { red "BUILD FAILED"; exit 1; }
go test -v ./... 2>&1 | grep -E "PASS|FAIL|---"
echo ""

# ---------- all-tasks smoke test ----------
echo "=== All-tasks smoke test (no panics, no errors) ==="
output=$("${BIN}" -pipeline "${PIPELINE}" -task "${TASKS}" -all-tasks -no-scc 2>&1) || {
    red "SMOKE TEST FAILED (exit code $?)"
    echo "${output}" | grep -E "panic|错误" || true
    exit 1
}
if echo "${output}" | grep -q "错误"; then
    red "SMOKE TEST FAILED (stderr contains '错误')"
    echo "${output}" | grep "错误"
    exit 1
fi
if echo "${output}" | grep -q "^panic"; then
    red "SMOKE TEST FAILED (panic detected)"
    exit 1
fi
green "  ✓ All 41 tasks completed without panics or errors"
echo ""

# ---------- per-task regression ----------
echo "=== Per-task regression ==="

run_task() {
    "${BIN}" -pipeline "${PIPELINE}" -task "${TASKS}/$1" -task-name "$2" -no-scc 2>&1
}

# --- AndroidOpenGame (entry mode) ---
echo "--- AndroidOpenGame (-entry) ---"
out=$("${BIN}" -pipeline "${PIPELINE}" -entry AndroidOpenGame -no-scc 2>&1)
reachable=$(extract_num "可达:" "${out}")
total=$(extract_num "节点总数:" "${out}")
assert_ge "total nodes" 2000 "${total}"
assert_ge "reachable nodes" 15 "${reachable}"
assert "AndroidOpenGame exec"     "1" "$(echo "${out}" | grep "^AndroidOpenGame " | awk '{print $2}')"
assert "OpenGame exec"  "1" "$(echo "${out}" | grep "^OpenGame " | awk '{print $2}')"

# --- DailyRewards ---
echo "--- DailyRewards ---"
out=$(run_task "DailyRewards.json" "DailyRewards")
assert "DailyRewardStart exec=1" "1" "$(echo "${out}" | grep "^DailyRewardStart " | awk '{print $2}')"
assert "DailyRewardEnd exec=1"   "1" "$(echo "${out}" | grep "^DailyRewardEnd " | awk '{print $2}')"
assert "DailyClaimDeliveryJobsRewardSub exec=1" "1" "$(echo "${out}" | grep "^DailyClaimDeliveryJobsRewardSub " | awk '{print $2}')"

# --- ItemTransfer ---
echo "--- ItemTransfer ---"
out=$(run_task "ItemTransfer.json" "ItemTransfer")
assert "ItemTransfer exec=1" "1" "$(echo "${out}" | grep "^ItemTransfer " | awk '{print $2}')"

# --- CreditShopping (fallback to union) ---
echo "--- CreditShopping (union fallback) ---"
out=$("${BIN}" -pipeline "${PIPELINE}" -task "${TASKS}/CreditShopping.json" -task-name CreditShoppingN2 -no-scc 2>&1)
assert_ge "reachable nodes" 5 "$(extract_num "可达:" "${out}")"

# --- AccountSwitch (was broken by // comments) ---
echo "--- AccountSwitch (comment fix) ---"
out=$("${BIN}" -pipeline "${PIPELINE}" -entry AccountSwitchStart -no-scc 2>&1)
assert "AccountSwitchStart exec=1" "1" "$(echo "${out}" | grep "^AccountSwitchStart " | awk '{print $2}')"
assert_ge "reachable nodes" 50 "$(extract_num "可达:" "${out}")"

# --- AutoEssence (has // comments + struct-mod options) ---
echo "--- AutoEssence ---"
out=$(run_task "AutoEssence.json" "AutoEssence")
assert_ge "total nodes" 3000 "$(extract_num "节点总数:" "${out}")"

# ---------- edge cases ----------
echo "=== Edge case tests ==="
go test -v ./... 2>&1 | grep -E "^(=== RUN|--- PASS|--- FAIL|PASS|FAIL)"

# ---------- summary ----------
echo ""
echo "=============================="
TOTAL=$((PASS + FAIL))
if [ "${FAIL}" -eq 0 ]; then
    green "ALL ${TOTAL} CHECKS PASSED"
else
    red "${FAIL}/${TOTAL} CHECKS FAILED"
    exit 1
fi
