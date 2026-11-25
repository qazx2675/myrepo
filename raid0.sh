#!/usr/bin/env bash
# HPE iLO 관리망 다중 서버 RAID 구성 도구 (Gen10 / Gen10 Plus / Gen11)
# RHEL 6 Bash 4.1+ / pdsh -R exec / ilorest / Python 2.6.6+ 호환

set -u

export PYTHONIOENCODING=utf-8

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
PARSER="${SCRIPT_DIR}/lib/raid_parser.py"
WORKER="${SCRIPT_DIR}/lib/ilo_worker.sh"

WORKLIST="./worklist.txt"
FANOUT=10
DRY_RUN=0
FORCE_DELETE=0
VOL_NAME_PREFIX=""
STRIPE_BYTES=""

print_usage() {
    cat <<'EOF'
사용법: bash raid0.sh [옵션]

HPE ProLiant (Gen10 / Gen10 Plus / Gen11) 서버들의 iLO 관리망(Redfish)에
pdsh + ilorest로 병렬 접속하여 RAID를 일괄 구성합니다.

옵션:
  -w, --worklist FILE   대상 iLO IP 목록 파일 경로 (기본값: ./worklist.txt)
  -f, --fanout N        pdsh 동시 실행 수 (기본값: 10)
  --dry-run             실제 변경 없이 조회 및 적용 예정 페이로드만 미리보기
  --force-delete        기존 RAID 볼륨에 할당된 디스크 충돌 시 기존 볼륨 삭제 후 재생성
  --name PREFIX         생성할 논리 볼륨 이름 접두사
  --stripe BYTES        스트라이프 크기(바이트, 예: 262144)
  -h, --help            이 도움말 출력

예시:
  bash raid0.sh
  bash raid0.sh -w ./worklist.txt -f 20 --dry-run
  bash raid0.sh -w ./servers.txt --force-delete --name DATA_VOL
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        -w|--worklist)
            if [ $# -lt 2 ]; then
                echo "[ERROR] $1 옵션에 파일 경로가 필요합니다." >&2
                exit 1
            fi
            WORKLIST="$2"
            shift 2
            ;;
        --worklist=*)
            WORKLIST="${1#--worklist=}"
            shift
            ;;
        -f|--fanout)
            if [ $# -lt 2 ]; then
                echo "[ERROR] $1 옵션에 숫자가 필요합니다." >&2
                exit 1
            fi
            FANOUT="$2"
            shift 2
            ;;
        --fanout=*)
            FANOUT="${1#--fanout=}"
            shift
            ;;
        --dry-run)
            DRY_RUN=1
            shift
            ;;
        --force-delete)
            FORCE_DELETE=1
            shift
            ;;
        --name)
            if [ $# -lt 2 ]; then
                echo "[ERROR] --name 옵션에 접두사가 필요합니다." >&2
                exit 1
            fi
            VOL_NAME_PREFIX="$2"
            shift 2
            ;;
        --name=*)
            VOL_NAME_PREFIX="${1#--name=}"
            shift
            ;;
        --stripe)
            if [ $# -lt 2 ]; then
                echo "[ERROR] --stripe 옵션에 바이트 숫자가 필요합니다." >&2
                exit 1
            fi
            STRIPE_BYTES="$2"
            shift 2
            ;;
        --stripe=*)
            STRIPE_BYTES="${1#--stripe=}"
            shift
            ;;
        -h|--help)
            print_usage
            exit 0
            ;;
        *)
            echo "[ERROR] 알 수 없는 옵션: $1" >&2
            print_usage >&2
            exit 1
            ;;
    esac
done

case "$FANOUT" in
    ''|*[!0-9]*)
        echo "[ERROR] --fanout 값은 1 이상의 정수여야 합니다: ${FANOUT}" >&2
        exit 1
        ;;
esac
if [ "$FANOUT" -lt 1 ]; then
    echo "[ERROR] --fanout 값은 1 이상이어야 합니다: ${FANOUT}" >&2
    exit 1
fi

if [ -n "$STRIPE_BYTES" ]; then
    case "$STRIPE_BYTES" in
        ''|*[!0-9]*)
            echo "[ERROR] --stripe 값은 양의 정수(바이트)여야 합니다: ${STRIPE_BYTES}" >&2
            exit 1
            ;;
    esac
fi

if [ ! -f "$PARSER" ] || [ ! -f "$WORKER" ]; then
    echo "[ERROR] 필수 내부 파일이 없습니다: ${PARSER} 또는 ${WORKER}" >&2
    exit 1
fi

if [ ! -f "$WORKLIST" ]; then
    echo "[ERROR] 작업 대상 파일(${WORKLIST})을 찾을 수 없습니다." >&2
    echo "        worklist.txt.example 을 복사하여 iLO IP를 입력한 뒤 다시 실행하세요." >&2
    exit 1
fi

detect_python() {
    if [ -n "${PYTHON_BIN:-}" ]; then
        if "$PYTHON_BIN" -c "import sys, json" >/dev/null 2>&1; then
            printf '%s' "$PYTHON_BIN"
            return 0
        fi
    fi
    local cand
    for cand in python3 python /usr/bin/python; do
        if command -v "$cand" >/dev/null 2>&1; then
            if "$cand" -c "import sys, json" >/dev/null 2>&1; then
                printf '%s' "$cand"
                return 0
            fi
        fi
    done
    return 1
}

# Dependency checks
if ! command -v ilorest >/dev/null 2>&1; then
    echo "[ERROR] 'ilorest' 명령을 찾을 수 없습니다. HPE iLOrest 패키지를 설치하세요." >&2
    exit 1
fi

if ! command -v pdsh >/dev/null 2>&1; then
    echo "[ERROR] 'pdsh' 명령을 찾을 수 없습니다. pdsh 및 pdsh-rcmd-exec 패키지를 설치하세요." >&2
    exit 1
fi

if ! pdsh -L 2>&1 | grep -qi "exec"; then
    echo "[ERROR] pdsh에 'exec' 모듈이 없습니다." >&2
    echo "        RHEL 6 환경에서는 'pdsh-rcmd-exec' 패키지를 설치해야 합니다." >&2
    exit 1
fi

PYTHON_BIN=$(detect_python) || {
    echo "[ERROR] 실행 가능한 Python(json 모듈 지원)을 찾을 수 없습니다." >&2
    exit 1
}

RUN_TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ilo_raid.XXXXXX")
RUN_TS=$(date +%Y%m%d_%H%M%S)
LOG_DIR="$(pwd)/logs/raid_${RUN_TS}"
mkdir -p "$LOG_DIR"

cleanup_main() {
    unset ILO_PASS 2>/dev/null || true
    if [ -n "${RUN_TMP_DIR:-}" ] && [ -d "$RUN_TMP_DIR" ]; then
        rm -rf "$RUN_TMP_DIR"
    fi
}
trap cleanup_main EXIT

# Clean worklist (strip CR, comments, whitespace, blank lines, duplicates)
CLEAN_WORKLIST="${RUN_TMP_DIR}/worklist_clean.txt"
tr -d '\r' < "$WORKLIST" \
    | sed -e 's/#.*$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
    | awk 'NF > 0 && !seen[$0]++ { print $0 }' > "$CLEAN_WORKLIST"

TOTAL_IPS=$(wc -l < "$CLEAN_WORKLIST" | tr -d '[:space:]')
if [ -z "$TOTAL_IPS" ] || [ "$TOTAL_IPS" -eq 0 ]; then
    echo "[ERROR] ${WORKLIST} 에 유효한 iLO IP가 없습니다." >&2
    exit 1
fi

echo "========================================================================"
echo " HPE iLO 관리망 다중 서버 RAID 구성 도구"
echo "========================================================================"
echo " - 대상 파일 : ${WORKLIST} (${TOTAL_IPS}대)"
echo " - 동시 실행 : fanout=${FANOUT}"
if [ "$DRY_RUN" -eq 1 ]; then
    echo " - 실행 모드 : DRY-RUN (실제 변경 없음)"
else
    echo " - 실행 모드 : 실제 적용"
fi
if [ "$FORCE_DELETE" -eq 1 ]; then
    echo " - 충돌 정책 : --force-delete 활성화 (기존 충돌 볼륨 삭제 후 재생성)"
else
    echo " - 충돌 정책 : 안전 모드 (기존 볼륨 충돌 시 보호)"
fi
echo " - 로그 경로 : ${LOG_DIR}"
echo "------------------------------------------------------------------------"

# Prompt iLO credentials (supports both interactive TTY and piped stdin)
printf 'iLO ID: '
if ! IFS= read -r ILO_USER; then
    echo ""
    echo "[ERROR] iLO ID 입력이 중단되었습니다." >&2
    exit 1
fi
ILO_USER=$(printf '%s' "$ILO_USER" | tr -d '\r')

printf 'iLO Password: '
if [ -t 0 ]; then
    if ! IFS= read -r -s ILO_PASS; then
        echo ""
        echo "[ERROR] iLO Password 입력이 중단되었습니다." >&2
        exit 1
    fi
else
    if ! IFS= read -r ILO_PASS; then
        echo ""
        echo "[ERROR] iLO Password 입력이 중단되었습니다." >&2
        exit 1
    fi
fi
printf '\n'
ILO_PASS=$(printf '%s' "$ILO_PASS" | tr -d '\r')

if [ -z "$ILO_USER" ] || [ -z "$ILO_PASS" ]; then
    echo "[ERROR] iLO ID와 Password는 비어 있을 수 없습니다." >&2
    exit 1
fi

export ILO_USER ILO_PASS RUN_TMP_DIR LOG_DIR PYTHON_BIN SCRIPT_DIR DRY_RUN

# ------------------------------------------------------------------------------
# Step 1: Parallel Discovery
# ------------------------------------------------------------------------------
echo ""
echo "[1/4] 대상 서버(${TOTAL_IPS}대) Redfish 하드웨어/디스크 정보 병렬 조회 중..."
pdsh -R exec -f "$FANOUT" -w "^${CLEAN_WORKLIST}" bash "$WORKER" discover %h > "${LOG_DIR}/pdsh_discover.log" 2>&1 || true

SUM_JSON="${RUN_TMP_DIR}/summary.json"
"$PYTHON_BIN" "$PARSER" summarize "$RUN_TMP_DIR" --format json > "$SUM_JSON" 2>/dev/null || true

"$PYTHON_BIN" "$PARSER" summarize "$RUN_TMP_DIR" --format text | tr -d '\r' | tee "${LOG_DIR}/discovery_summary.txt"

OK_COUNT=$("$PYTHON_BIN" -c '
import json, sys
try:
    f = open(sys.argv[1], "rb")
    try:
        raw = f.read()
    finally:
        f.close()
    if sys.version_info[0] >= 3:
        d = json.loads(raw.decode("utf-8", "replace"))
    else:
        d = json.loads(raw)
    sys.stdout.write(str(int(d.get("ok_count", 0))))
except Exception:
    sys.stdout.write("0")
' "$SUM_JSON" 2>/dev/null | tr -d '\r')

if [ -z "$OK_COUNT" ] || [ "$OK_COUNT" -eq 0 ]; then
    echo ""
    echo "[ERROR] 정상 조회된 서버가 없습니다. iLO IP, 계정/비밀번호, 네트워크 연결을 확인하세요." >&2
    echo "        상세 로그: ${LOG_DIR}" >&2
    exit 1
fi

echo ""
printf '대상 컨트롤러 선택 [기본값: auto]: '
IFS= read -r CTRL_SEL || CTRL_SEL=""
CTRL_SEL=$(printf '%s' "$CTRL_SEL" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
if [ -z "$CTRL_SEL" ]; then
    CTRL_SEL="auto"
fi

echo ""
echo "[디스크 현황] 선택 컨트롤러: ${CTRL_SEL}"
"$PYTHON_BIN" "$PARSER" summarize "$RUN_TMP_DIR" --format text --controller-sel "$CTRL_SEL" | tr -d '\r' | tee "${LOG_DIR}/drive_inventory.txt"

# ------------------------------------------------------------------------------
# Step 2: Interactive Disk Range & RAID Selection
# ------------------------------------------------------------------------------
echo ""
echo "[2/4] RAID 구성 옵션 입력"
printf '대상 디스크 범위 입력 (예: sda-sdh, nvme0n1-nvme7n1, 1-8, 1I:1:1-1I:1:8): '
if ! IFS= read -r DISK_RANGE; then
    echo ""
    echo "[ERROR] 디스크 범위 입력이 중단되었습니다." >&2
    exit 1
fi
DISK_RANGE=$(printf '%s' "$DISK_RANGE" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
if [ -z "$DISK_RANGE" ]; then
    echo "[ERROR] 디스크 범위가 비어 있습니다." >&2
    exit 1
fi

printf 'RAID 레벨 입력 (0, 1, 5, 6, 10, 50, 60): '
if ! IFS= read -r RAID_LEVEL; then
    echo ""
    echo "[ERROR] RAID 레벨 입력이 중단되었습니다." >&2
    exit 1
fi
RAID_LEVEL=$(printf '%s' "$RAID_LEVEL" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
case "$RAID_LEVEL" in
    0|1|5|6|10|50|60)
        ;;
    *)
        echo "[ERROR] 지원하지 않는 RAID 레벨입니다: ${RAID_LEVEL} (지원: 0, 1, 5, 6, 10, 50, 60)" >&2
        exit 1
        ;;
esac

RAID0_MODE="combined"
if [ "$RAID_LEVEL" = "0" ]; then
    echo "  1) 전체 통합 RAID 0 (선택한 디스크 전체를 1개 볼륨으로 구성)"
    echo "  2) 디스크별 개별 RAID 0 (1디스크 = 1볼륨)"
    printf 'RAID 0 모드 선택 [1/2, 기본값: 1]: '
    IFS= read -r R0_CHOICE || R0_CHOICE=""
    R0_CHOICE=$(printf '%s' "$R0_CHOICE" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    case "$R0_CHOICE" in
        ""|1|combined)
            RAID0_MODE="combined"
            ;;
        2|per-disk)
            RAID0_MODE="per-disk"
            ;;
        *)
            echo "[ERROR] 잘못된 RAID 0 모드 선택입니다: ${R0_CHOICE} (1 또는 2 입력)" >&2
            exit 1
            ;;
    esac
fi

PLAN_DIR="${RUN_TMP_DIR}/plans"
mkdir -p "$PLAN_DIR"

PLAN_ARGS=(
    "$PARSER" plan
    --inv-dir "$RUN_TMP_DIR"
    --out-dir "$PLAN_DIR"
    --controller "$CTRL_SEL"
    --range "$DISK_RANGE"
    --raid "$RAID_LEVEL"
    --raid0-mode "$RAID0_MODE"
    --force-delete "$FORCE_DELETE"
)
if [ -n "$VOL_NAME_PREFIX" ]; then
    PLAN_ARGS+=(--name "$VOL_NAME_PREFIX")
fi
if [ -n "$STRIPE_BYTES" ]; then
    PLAN_ARGS+=(--stripe "$STRIPE_BYTES")
fi

echo ""
echo "[계획 검토] 서버별 RAID 적용 계획 생성 중..."
"$PYTHON_BIN" "${PLAN_ARGS[@]}" | tr -d '\r' | tee "${LOG_DIR}/plan_preview.txt"

PLAN_SUMMARY="${PLAN_DIR}/plan_summary.json"
if [ ! -f "$PLAN_SUMMARY" ]; then
    echo "[ERROR] 실행 계획 요약 파일(plan_summary.json)이 생성되지 않았습니다." >&2
    exit 1
fi

READY_IPS_FILE="${RUN_TMP_DIR}/ready_ips.txt"
CONFLICT_IPS_FILE="${RUN_TMP_DIR}/conflict_ips.txt"
PLAN_META_FILE="${RUN_TMP_DIR}/plan_meta.txt"

"$PYTHON_BIN" -c '
import json, sys

def to_bytes(s):
    if sys.version_info[0] >= 3:
        return str(s).encode("utf-8")
    if isinstance(s, unicode):
        return s.encode("utf-8")
    return str(s)

f = open(sys.argv[1], "rb")
try:
    raw = f.read()
finally:
    f.close()
if sys.version_info[0] >= 3:
    d = json.loads(raw.decode("utf-8", "replace"))
else:
    d = json.loads(raw)

ready = d.get("ready_ips") or []
conflicts = d.get("conflict_ips") or []
errors = d.get("error_ips") or []
has_del = 1 if d.get("has_deletes") else 0
del_cnt = int(d.get("total_delete_count") or 0)
hint = d.get("remediation_hint") or ""

rf = open(sys.argv[2], "wb")
try:
    for ip in ready:
        rf.write(to_bytes(ip) + b"\n")
finally:
    rf.close()

cf = open(sys.argv[3], "wb")
try:
    for ip in conflicts:
        cf.write(to_bytes(ip) + b"\n")
finally:
    cf.close()

mf = open(sys.argv[4], "wb")
try:
    mf.write(to_bytes("READY_COUNT=%d\n" % len(ready)))
    mf.write(to_bytes("CONFLICT_COUNT=%d\n" % len(conflicts)))
    mf.write(to_bytes("ERROR_COUNT=%d\n" % len(errors)))
    mf.write(to_bytes("HAS_DELETES=%d\n" % has_del))
    mf.write(to_bytes("TOTAL_DELETE_COUNT=%d\n" % del_cnt))
    hint_clean = to_bytes(hint).replace(b"\n", b" ").replace(b"\r", b"")
    mf.write(b"REMEDIATION_HINT=" + hint_clean + b"\n")
finally:
    mf.close()
' "$PLAN_SUMMARY" "$READY_IPS_FILE" "$CONFLICT_IPS_FILE" "$PLAN_META_FILE"

READY_COUNT=0
CONFLICT_COUNT=0
ERROR_COUNT=0
HAS_DELETES=0
TOTAL_DELETE_COUNT=0
REMEDIATION_HINT=""

while IFS= read -r meta_line; do
    meta_line=$(printf '%s' "$meta_line" | tr -d '\r')
    case "$meta_line" in
        READY_COUNT=*) READY_COUNT="${meta_line#READY_COUNT=}" ;;
        CONFLICT_COUNT=*) CONFLICT_COUNT="${meta_line#CONFLICT_COUNT=}" ;;
        ERROR_COUNT=*) ERROR_COUNT="${meta_line#ERROR_COUNT=}" ;;
        HAS_DELETES=*) HAS_DELETES="${meta_line#HAS_DELETES=}" ;;
        TOTAL_DELETE_COUNT=*) TOTAL_DELETE_COUNT="${meta_line#TOTAL_DELETE_COUNT=}" ;;
        REMEDIATION_HINT=*) REMEDIATION_HINT="${meta_line#REMEDIATION_HINT=}" ;;
    esac
done < "$PLAN_META_FILE"

if [ -z "$REMEDIATION_HINT" ]; then
    REMEDIATION_HINT="기존 RAID 볼륨에 할당된 디스크가 감지되었습니다. 기존 볼륨을 삭제하고 재생성하려면 --force-delete 옵션을 사용하세요: bash raid0.sh --force-delete"
fi

if [ "$CONFLICT_COUNT" -gt 0 ] && [ "$FORCE_DELETE" -eq 0 ]; then
    echo ""
    echo "========================================================================"
    echo " [경고] 기존 RAID 볼륨 충돌 감지 (${CONFLICT_COUNT}대)"
    echo "========================================================================"
    echo " ${REMEDIATION_HINT}"
    echo " 충돌 서버 목록:"
    while IFS= read -r cip; do
        cip=$(printf '%s' "$cip" | tr -d '\r')
        [ -n "$cip" ] && echo "   - ${cip}"
    done < "$CONFLICT_IPS_FILE"
    echo "========================================================================"

    if [ "$READY_COUNT" -eq 0 ]; then
        echo "[중단] 적용 가능한 정상 대상 서버가 없어 안전하게 종료합니다." >&2
        exit 1
    fi

    if [ "$DRY_RUN" -eq 0 ]; then
        printf '충돌이 없는 정상 대상 서버(%s대)만 계속 진행하시겠습니까? (y/N): ' "$READY_COUNT"
        IFS= read -r PARTIAL_CONFIRM || PARTIAL_CONFIRM=""
        PARTIAL_CONFIRM=$(printf '%s' "$PARTIAL_CONFIRM" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
        if [ "$PARTIAL_CONFIRM" != "y" ] && [ "$PARTIAL_CONFIRM" != "Y" ]; then
            echo "사용자 요청으로 작업을 중단합니다."
            exit 1
        fi
    fi
fi

if [ "$READY_COUNT" -eq 0 ]; then
    echo "[ERROR] 적용 가능한 대상 서버가 없습니다 (오류: ${ERROR_COUNT}대, 충돌: ${CONFLICT_COUNT}대)." >&2
    exit 1
fi

# ------------------------------------------------------------------------------
# Step 3: Dry-Run or Final Confirmation + Parallel Apply
# ------------------------------------------------------------------------------
if [ "$DRY_RUN" -eq 1 ]; then
    echo ""
    echo "========================================================================"
    echo " [DRY-RUN] 실제 서버 변경 없이 예정된 Redfish 페이로드만 출력합니다."
    echo "========================================================================"
    "$PYTHON_BIN" -c '
import glob, json, os, sys
plan_dir = sys.argv[1]
files = sorted(glob.glob(os.path.join(plan_dir, "plan_*.json")))
for path in files:
    if os.path.basename(path) == "plan_summary.json":
        continue
    f = open(path, "rb")
    try:
        raw = f.read()
    finally:
        f.close()
    if sys.version_info[0] >= 3:
        p = json.loads(raw.decode("utf-8", "replace"))
    else:
        p = json.loads(raw)
    ip = p.get("ip", "unknown")
    st = p.get("status", "UNKNOWN")
    bk = p.get("backend", "unknown")
    sys.stdout.write("\n--- [%s] status=%s backend=%s ---\n" % (ip, st, bk))
    previews = p.get("preview_payloads")
    if previews is not None:
        out = json.dumps(previews, indent=2, ensure_ascii=False)
    else:
        out = json.dumps(p, indent=2, ensure_ascii=False)
    if sys.version_info[0] < 3 and isinstance(out, unicode):
        out = out.encode("utf-8")
    sys.stdout.write(out + "\n")
' "$PLAN_DIR" | tr -d '\r' | tee "${LOG_DIR}/dry_run_preview.txt"
    echo ""
    echo "[DRY-RUN 완료] 실제 서버는 변경되지 않았습니다. 로그 경로: ${LOG_DIR}"
    exit 0
fi

echo ""
echo "[3/4] 최종 실행 확인"
if [ "$HAS_DELETES" -eq 1 ]; then
    echo "========================================================================"
    echo " [위험 경고] --force-delete 옵션으로 기존 볼륨 총 ${TOTAL_DELETE_COUNT}개가 삭제됩니다!"
    echo " 삭제된 볼륨의 데이터는 복구할 수 없습니다."
    echo "========================================================================"
    printf "기존 볼륨 삭제 및 RAID 생성을 진행하려면 정확히 DELETE 를 입력하세요: "
    IFS= read -r FINAL_CONFIRM || FINAL_CONFIRM=""
    FINAL_CONFIRM=$(printf '%s' "$FINAL_CONFIRM" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    if [ "$FINAL_CONFIRM" != "DELETE" ]; then
        echo "[취소] 'DELETE'와 일치하지 않아 작업을 안전하게 취소합니다." >&2
        exit 1
    fi
else
    printf '위 계획대로 %s대 서버에 RAID 구성을 적용하시겠습니까? (y/N): ' "$READY_COUNT"
    IFS= read -r FINAL_CONFIRM || FINAL_CONFIRM=""
    FINAL_CONFIRM=$(printf '%s' "$FINAL_CONFIRM" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    if [ "$FINAL_CONFIRM" != "y" ] && [ "$FINAL_CONFIRM" != "Y" ]; then
        echo "사용자 요청으로 작업을 취소합니다."
        exit 0
    fi
fi

APPLY_WORKLIST="${RUN_TMP_DIR}/worklist_apply.txt"
tr -d '\r' < "$READY_IPS_FILE" > "$APPLY_WORKLIST"

echo ""
echo "[적용 중] ${READY_COUNT}대 서버에 병렬 RAID 구성 적용 중..."
pdsh -R exec -f "$FANOUT" -w "^${APPLY_WORKLIST}" bash "$WORKER" apply %h > "${LOG_DIR}/pdsh_apply.log" 2>&1 || true

# ------------------------------------------------------------------------------
# Step 4: Summary, Logs & Selective Reboot
# ------------------------------------------------------------------------------
echo ""
echo "[4/4] RAID 적용 결과 요약"
mkdir -p "${RUN_TMP_DIR}/results"
REBOOT_LIST="${RUN_TMP_DIR}/reboot_list.txt"
rm -f "$REBOOT_LIST"

"$PYTHON_BIN" "$PARSER" summarize-results "${RUN_TMP_DIR}/results" \
    --format text \
    --reboot-list-out "$REBOOT_LIST" \
    --print-reboot-list | tr -d '\r' | tee "${LOG_DIR}/apply_summary.txt"

if [ -f "$REBOOT_LIST" ]; then
    tr -d '\r' < "$REBOOT_LIST" | awk 'NF > 0' > "${REBOOT_LIST}.clean"
    mv "${REBOOT_LIST}.clean" "$REBOOT_LIST"
fi

if [ -s "$REBOOT_LIST" ]; then
    echo ""
    printf '위 재부팅 필요 장비들을 지금 일괄 재부팅하시겠습니까? (y/N): '
    IFS= read -r REBOOT_CONFIRM || REBOOT_CONFIRM=""
    REBOOT_CONFIRM=$(printf '%s' "$REBOOT_CONFIRM" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    if [ "$REBOOT_CONFIRM" = "y" ] || [ "$REBOOT_CONFIRM" = "Y" ]; then
        REBOOT_CNT=$(wc -l < "$REBOOT_LIST" | tr -d '[:space:]')
        echo ""
        echo "[재부팅 실행] ${REBOOT_CNT}대 서버에 일괄 재부팅 명령 전송 중..."
        pdsh -R exec -f "$FANOUT" -w "^${REBOOT_LIST}" bash "$WORKER" reboot %h > "${LOG_DIR}/pdsh_reboot.log" 2>&1 || true

        mkdir -p "${RUN_TMP_DIR}/reboot_results"
        "$PYTHON_BIN" -c '# -*- coding: utf-8 -*-
import glob, json, os, sys
reb_dir = sys.argv[1]
files = sorted(glob.glob(os.path.join(reb_dir, "reb_*.json")))
sys.stdout.write("\n=== 재부팅 실행 결과 ===\n")
sys.stdout.write("%-18s | %-24s | %-10s | %s\n" % ("IP", "Model", "Status", "Message"))
sys.stdout.write("%s\n" % ("-" * 80))
for path in files:
    f = open(path, "rb")
    try:
        raw = f.read()
    finally:
        f.close()
    if sys.version_info[0] >= 3:
        r = json.loads(raw.decode("utf-8", "replace"))
    else:
        r = json.loads(raw)
    line = "%-18s | %-24s | %-10s | %s\n" % (
        r.get("ip", ""),
        r.get("model", ""),
        r.get("status", ""),
        r.get("message", "")
    )
    if sys.version_info[0] < 3 and isinstance(line, unicode):
        line = line.encode("utf-8")
    sys.stdout.write(line)
' "${RUN_TMP_DIR}/reboot_results" | tr -d '\r' | tee "${LOG_DIR}/reboot_summary.txt"
    else
        echo "재부팅을 건너뛰었습니다. 재부팅 필요 장비는 다음 재부팅 시 변경된 RAID가 반영됩니다."
    fi
fi

echo ""
echo "========================================================================"
echo " 작업 완료 - 로그 저장 위치: ${LOG_DIR}"
echo "========================================================================"
