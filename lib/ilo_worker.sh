#!/usr/bin/env bash
# HPE iLO Redfish worker script invoked by pdsh -R exec
# Compatible with RHEL 6 Bash 4.1+

set -u

export PYTHONIOENCODING=utf-8

ACTION="${1:-}"
ILO_IP="${2:-}"

if [ -z "$ACTION" ] || [ -z "$ILO_IP" ]; then
    echo "[ERROR] Usage: $0 <discover|apply|reboot> <ilo_ip>" >&2
    exit 1
fi

if [ -z "${SCRIPT_DIR:-}" ]; then
    SCRIPT_DIR=$(cd "$(dirname "$0")/.." && pwd)
fi

PARSER="${SCRIPT_DIR}/lib/raid_parser.py"

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

if [ -z "${PYTHON_BIN:-}" ] || ! "$PYTHON_BIN" -c "import sys, json" >/dev/null 2>&1; then
    PYTHON_BIN=$(detect_python) || {
        echo "[ERROR] [${ILO_IP}] 실행 가능한 Python(json 지원)을 찾을 수 없습니다." >&2
        exit 1
    }
fi

if [ -z "${RUN_TMP_DIR:-}" ]; then
    echo "[ERROR] [${ILO_IP}] RUN_TMP_DIR 환경 변수가 설정되지 않았습니다." >&2
    exit 1
fi

SAFE_IP=$(printf '%s' "$ILO_IP" | tr '.:' '__')
CACHE_DIR="${RUN_TMP_DIR}/cache_${SAFE_IP}"
mkdir -p "$CACHE_DIR"

if [ -n "${LOG_DIR:-}" ]; then
    mkdir -p "$LOG_DIR"
    WORKER_LOG="${LOG_DIR}/${ACTION}_${SAFE_IP}.log"
else
    WORKER_LOG="${RUN_TMP_DIR}/${ACTION}_${SAFE_IP}.log"
fi

LOGIN_ATTEMPTED=0

cleanup() {
    if [ "${LOGIN_ATTEMPTED:-0}" -eq 1 ]; then
        ilorest --cache-dir "$CACHE_DIR" logout >/dev/null 2>&1 || true
        LOGIN_ATTEMPTED=0
    fi
}
trap cleanup EXIT

redact_file_append() {
    local src_file="$1"
    local dst_file="$2"
    if [ ! -f "$src_file" ]; then
        return 0
    fi
    "$PYTHON_BIN" -c '
import os, sys
p = os.environ.get("ILO_PASS", "")
src = sys.argv[1]
dst = sys.argv[2]
f = open(src, "rb")
try:
    data = f.read()
finally:
    f.close()
if p:
    if sys.version_info[0] >= 3:
        pb = p.encode("utf-8", "replace")
    else:
        pb = p
    data = data.replace(pb, b"***")
out = open(dst, "ab")
try:
    out.write(data)
finally:
    out.close()
' "$src_file" "$dst_file" 2>/dev/null || true
}

redact_string() {
    local text="$1"
    printf '%s' "$text" | "$PYTHON_BIN" -c '
import os, sys
p = os.environ.get("ILO_PASS", "")
data = sys.stdin.read()
if p:
    data = data.replace(p, "***")
sys.stdout.write(data)
' 2>/dev/null
}

extract_first_error_line() {
    local src_file="$1"
    if [ ! -s "$src_file" ]; then
        printf '%s' "상세 오류 없음"
        return 0
    fi
    "$PYTHON_BIN" -c '# -*- coding: utf-8 -*-
import os, sys
p = os.environ.get("ILO_PASS", "")
f = open(sys.argv[1], "rb")
try:
    raw = f.read()
finally:
    f.close()
if sys.version_info[0] >= 3:
    text = raw.decode("utf-8", "replace")
else:
    text = raw
lines = text.splitlines()
picked = ""
for ln in lines:
    s = ln.strip()
    if not s:
        continue
    if p:
        s = s.replace(p, "***")
    picked = s
    if "error" in s.lower() or "fail" in s.lower() or "denied" in s.lower() or "40" in s or "50" in s:
        break
if len(picked) > 200:
    picked = picked[:200] + "..."
sys.stdout.write(picked if picked else "상세 오류 없음")
' "$src_file" 2>/dev/null | tr -d '\r\n'
}

do_login() {
    local err_file="$1"
    LOGIN_ATTEMPTED=1
    if ilorest --cache-dir "$CACHE_DIR" login "https://${ILO_IP}" -u "${ILO_USER:-}" -p "${ILO_PASS:-}" > "$err_file" 2>&1; then
        redact_file_append "$err_file" "$WORKER_LOG"
        rm -f "$err_file"
        return 0
    fi
    redact_file_append "$err_file" "$WORKER_LOG"
    return 1
}

rawget_to() {
    local uri="$1"
    local out_file="$2"
    local tmp_err="${out_file}.err"
    if ilorest --cache-dir "$CACHE_DIR" rawget "$uri" > "$out_file" 2> "$tmp_err"; then
        if [ -s "$tmp_err" ]; then
            redact_file_append "$tmp_err" "$WORKER_LOG"
        fi
        rm -f "$tmp_err"
        if [ -s "$out_file" ]; then
            return 0
        fi
    else
        if [ -s "$tmp_err" ]; then
            redact_file_append "$tmp_err" "$WORKER_LOG"
        fi
    fi
    rm -f "$tmp_err" "$out_file"
    return 1
}

extract_links() {
    local json_file="$1"
    local mode="$2"
    if [ ! -f "$json_file" ]; then
        return 0
    fi
    "$PYTHON_BIN" "$PARSER" extract-links "$json_file" "$mode" 2>/dev/null | tr -d '\r'
}

write_result() {
    local out_json="$1"
    local model="$2"
    local generation="$3"
    local backend="$4"
    local status="$5"
    local reboot_req="$6"
    local msg="$7"
    local safe_msg
    safe_msg=$(redact_string "$msg")
    "$PYTHON_BIN" "$PARSER" write-result "$out_json" "$ILO_IP" "$model" "$generation" "$backend" "$status" "$reboot_req" "$safe_msg"
}

action_discover() {
    local raw_dir="${RUN_TMP_DIR}/raw_${SAFE_IP}"
    local inv_out="${RUN_TMP_DIR}/inv_${SAFE_IP}.json"
    local login_err="${raw_dir}/login_err.tmp"
    mkdir -p "$raw_dir"

    if ! do_login "$login_err"; then
        local msg
        msg=$(extract_first_error_line "$login_err")
        printf 'iLO login failed: %s\n' "$msg" > "${raw_dir}/error.txt"
        rm -f "$login_err"
        "$PYTHON_BIN" "$PARSER" normalize "$ILO_IP" "$raw_dir" "$inv_out"
        return 0
    fi

    if ! rawget_to "/redfish/v1/Systems/1" "${raw_dir}/system.json"; then
        printf 'Failed to GET /redfish/v1/Systems/1\n' > "${raw_dir}/error.txt"
        "$PYTHON_BIN" "$PARSER" normalize "$ILO_IP" "$raw_dir" "$inv_out"
        return 0
    fi

    # Best-effort Manager info (iLO version)
    rawget_to "/redfish/v1/Managers/1" "${raw_dir}/manager.json" || true

    # Standard Redfish Storage collection (Gen10 Plus MR/RDE, Gen11 iLO 6, Gen10 mirror)
    if rawget_to "/redfish/v1/Systems/1/Storage" "${raw_dir}/storage_collection.json"; then
        local st_links="${raw_dir}/_st_links.tmp"
        extract_links "${raw_dir}/storage_collection.json" members > "$st_links"
        local i=0
        local st_uri
        while IFS= read -r st_uri; do
            [ -n "$st_uri" ] || continue
            if rawget_to "$st_uri" "${raw_dir}/storage_${i}.json"; then
                # Drives
                local drv_links="${raw_dir}/_st_${i}_drv.tmp"
                extract_links "${raw_dir}/storage_${i}.json" drives > "$drv_links"
                local d=0
                local drv_entry
                while IFS= read -r drv_entry; do
                    [ -n "$drv_entry" ] || continue
                    case "$drv_entry" in
                        COLLECTION:*)
                            local drv_coll_uri="${drv_entry#COLLECTION:}"
                            if rawget_to "$drv_coll_uri" "${raw_dir}/storage_${i}_drives.json"; then
                                local coll_members="${raw_dir}/_st_${i}_drv_m.tmp"
                                extract_links "${raw_dir}/storage_${i}_drives.json" members > "$coll_members"
                                local m_uri
                                while IFS= read -r m_uri; do
                                    [ -n "$m_uri" ] || continue
                                    rawget_to "$m_uri" "${raw_dir}/storage_${i}_drive_${d}.json" || true
                                    d=$((d + 1))
                                done < "$coll_members"
                                rm -f "$coll_members"
                            fi
                            ;;
                        *)
                            rawget_to "$drv_entry" "${raw_dir}/storage_${i}_drive_${d}.json" || true
                            d=$((d + 1))
                            ;;
                    esac
                done < "$drv_links"
                rm -f "$drv_links"

                # Volumes & Capabilities
                local vol_coll_uri
                vol_coll_uri=$(extract_links "${raw_dir}/storage_${i}.json" volumes_uri | head -n 1)
                if [ -n "$vol_coll_uri" ]; then
                    if rawget_to "$vol_coll_uri" "${raw_dir}/storage_${i}_volumes.json"; then
                        local vol_links="${raw_dir}/_st_${i}_vol.tmp"
                        extract_links "${raw_dir}/storage_${i}_volumes.json" members > "$vol_links"
                        local v=0
                        local vol_uri
                        while IFS= read -r vol_uri; do
                            [ -n "$vol_uri" ] || continue
                            rawget_to "$vol_uri" "${raw_dir}/storage_${i}_volume_${v}.json" || true
                            v=$((v + 1))
                        done < "$vol_links"
                        rm -f "$vol_links"
                    fi
                    local cap_uri
                    cap_uri=$(extract_links "${raw_dir}/storage_${i}.json" capabilities_uri | head -n 1)
                    if [ -z "$cap_uri" ]; then
                        cap_uri="${vol_coll_uri%/}/Capabilities"
                    fi
                    rawget_to "$cap_uri" "${raw_dir}/storage_${i}_capabilities.json" || true
                fi

                i=$((i + 1))
            fi
        done < "$st_links"
        rm -f "$st_links"
    fi

    # Legacy HPE SmartStorage ArrayControllers (Gen10 / Gen10 Plus SR; fails harmlessly on Gen11)
    if rawget_to "/redfish/v1/Systems/1/SmartStorage/ArrayControllers" "${raw_dir}/ssc_controllers.json"; then
        local ssc_links="${raw_dir}/_ssc_links.tmp"
        extract_links "${raw_dir}/ssc_controllers.json" members > "$ssc_links"
        local si=0
        local ctrl_uri
        while IFS= read -r ctrl_uri; do
            [ -n "$ctrl_uri" ] || continue
            if rawget_to "$ctrl_uri" "${raw_dir}/ssc_ctrl_${si}.json"; then
                local ssc_drv_coll
                ssc_drv_coll=$(extract_links "${raw_dir}/ssc_ctrl_${si}.json" ssc_drive_links | head -n 1)
                if [ -z "$ssc_drv_coll" ]; then
                    ssc_drv_coll="${ctrl_uri%/}/DiskDrives"
                fi
                if rawget_to "$ssc_drv_coll" "${raw_dir}/ssc_ctrl_${si}_drives.json"; then
                    local sd_links="${raw_dir}/_ssc_${si}_drv.tmp"
                    extract_links "${raw_dir}/ssc_ctrl_${si}_drives.json" members > "$sd_links"
                    local sd=0
                    local sd_uri
                    while IFS= read -r sd_uri; do
                        [ -n "$sd_uri" ] || continue
                        rawget_to "$sd_uri" "${raw_dir}/ssc_ctrl_${si}_drive_${sd}.json" || true
                        sd=$((sd + 1))
                    done < "$sd_links"
                    rm -f "$sd_links"
                fi

                local ssc_vol_coll
                ssc_vol_coll=$(extract_links "${raw_dir}/ssc_ctrl_${si}.json" ssc_volume_links | head -n 1)
                if [ -z "$ssc_vol_coll" ]; then
                    ssc_vol_coll="${ctrl_uri%/}/LogicalDrives"
                fi
                if rawget_to "$ssc_vol_coll" "${raw_dir}/ssc_ctrl_${si}_volumes.json"; then
                    local sv_links="${raw_dir}/_ssc_${si}_vol.tmp"
                    extract_links "${raw_dir}/ssc_ctrl_${si}_volumes.json" members > "$sv_links"
                    local sv=0
                    local sv_uri
                    while IFS= read -r sv_uri; do
                        [ -n "$sv_uri" ] || continue
                        rawget_to "$sv_uri" "${raw_dir}/ssc_ctrl_${si}_volume_${sv}.json" || true
                        sv=$((sv + 1))
                    done < "$sv_links"
                    rm -f "$sv_links"
                fi

                si=$((si + 1))
            fi
        done < "$ssc_links"
        rm -f "$ssc_links"
    fi

    # SmartStorageConfig root and settings (best-effort)
    local ssc_cfg_uri
    ssc_cfg_uri=$(extract_links "${raw_dir}/system.json" ssc_config_uri | head -n 1)
    if [ -z "$ssc_cfg_uri" ]; then
        ssc_cfg_uri="/redfish/v1/Systems/1/smartstorageconfig/"
    fi
    if rawget_to "$ssc_cfg_uri" "${raw_dir}/ssc_config_root.json"; then
        local cfg_members="${raw_dir}/_ssc_cfg_m.tmp"
        extract_links "${raw_dir}/ssc_config_root.json" members > "$cfg_members"
        if [ -s "$cfg_members" ]; then
            local ck=0
            local cfg_uri
            while IFS= read -r cfg_uri; do
                [ -n "$cfg_uri" ] || continue
                if rawget_to "$cfg_uri" "${raw_dir}/ssc_config_${ck}.json"; then
                    local set_uri
                    set_uri=$(extract_links "${raw_dir}/ssc_config_${ck}.json" settings_uri | head -n 1)
                    if [ -z "$set_uri" ]; then
                        set_uri="${cfg_uri%/}/settings/"
                    fi
                    rawget_to "$set_uri" "${raw_dir}/ssc_settings_${ck}.json" || true
                    ck=$((ck + 1))
                fi
            done < "$cfg_members"
        else
            local set_uri
            set_uri=$(extract_links "${raw_dir}/ssc_config_root.json" settings_uri | head -n 1)
            if [ -z "$set_uri" ]; then
                set_uri="/redfish/v1/Systems/1/smartstorageconfig/settings/"
            fi
            rawget_to "$set_uri" "${raw_dir}/ssc_settings_0.json" || true
        fi
        rm -f "$cfg_members"
    elif [ -f "${raw_dir}/ssc_controllers.json" ]; then
        rawget_to "/redfish/v1/Systems/1/smartstorageconfig/settings/" "${raw_dir}/ssc_settings_0.json" || true
    fi

    "$PYTHON_BIN" "$PARSER" normalize "$ILO_IP" "$raw_dir" "$inv_out"
}

read_plan_field() {
    local plan_file="$1"
    local field_name="$2"
    "$PYTHON_BIN" -c '
import json, sys
f = open(sys.argv[1], "rb")
try:
    raw = f.read()
finally:
    f.close()
if sys.version_info[0] >= 3:
    data = json.loads(raw.decode("utf-8", "replace"))
else:
    data = json.loads(raw)
key = sys.argv[2]
val = data.get(key)
if val is None and isinstance(data.get("controller"), dict):
    val = data["controller"].get(key)
if val is None:
    val = ""
if isinstance(val, bool):
    sys.stdout.write("1" if val else "0")
else:
    if sys.version_info[0] < 3 and isinstance(val, unicode):
        sys.stdout.write(val.encode("utf-8"))
    else:
        sys.stdout.write(str(val))
' "$plan_file" "$field_name" 2>/dev/null | tr -d '\r'
}

action_apply() {
    local results_dir="${RUN_TMP_DIR}/results"
    local res_file="${results_dir}/res_${SAFE_IP}.json"
    local plan_file="${RUN_TMP_DIR}/plans/plan_${SAFE_IP}.json"
    local work_dir="${RUN_TMP_DIR}/apply_${SAFE_IP}"
    mkdir -p "$results_dir" "$work_dir"

    if [ ! -f "$plan_file" ]; then
        write_result "$res_file" "Unknown" "Unknown" "none" "SKIPPED" "0" "실행 계획 파일이 없습니다"
        return 0
    fi

    local p_status p_model p_gen p_backend p_settings_uri p_msg
    p_status=$(read_plan_field "$plan_file" "status")
    p_model=$(read_plan_field "$plan_file" "model")
    p_gen=$(read_plan_field "$plan_file" "generation")
    p_backend=$(read_plan_field "$plan_file" "backend")
    p_settings_uri=$(read_plan_field "$plan_file" "settings_uri")
    p_msg=$(read_plan_field "$plan_file" "message")
    if [ -z "$p_msg" ]; then
        p_msg=$(read_plan_field "$plan_file" "error")
    fi

    [ -n "$p_model" ] || p_model="Unknown"
    [ -n "$p_gen" ] || p_gen="Unknown"
    [ -n "$p_backend" ] || p_backend="unknown"

    if [ "$p_status" != "READY" ] && [ "$p_status" != "READY_WITH_DELETE" ]; then
        write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "SKIPPED" "0" "${p_msg:-적용 대상 제외 ($p_status)}"
        return 0
    fi

    if [ "${DRY_RUN:-0}" = "1" ]; then
        write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "SKIPPED" "0" "DRY-RUN 모드 (변경 미적용)"
        return 0
    fi

    local login_err="${work_dir}/login_err.tmp"
    if ! do_login "$login_err"; then
        local lmsg
        lmsg=$(extract_first_error_line "$login_err")
        rm -f "$login_err"
        write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "FAILED" "0" "iLO 로그인 실패: ${lmsg}"
        return 0
    fi

    if [ "$p_backend" = "smartstorageconfig" ]; then
        if [ -z "$p_settings_uri" ]; then
            p_settings_uri="/redfish/v1/Systems/1/smartstorageconfig/settings/"
        fi

        local live_settings="${work_dir}/live_settings.json"
        local payload_file="${work_dir}/ssc_payload.json"
        local put_resp="${work_dir}/ssc_put_resp.txt"

        if ! rawget_to "$p_settings_uri" "$live_settings"; then
            write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "FAILED" "0" "SmartStorageConfig 현재 설정 조회 실패 (${p_settings_uri})"
            return 0
        fi

        if ! "$PYTHON_BIN" "$PARSER" build-ssc-payload "$live_settings" "$plan_file" "$payload_file" >> "$WORKER_LOG" 2>&1; then
            write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "FAILED" "0" "SmartStorageConfig 페이로드 생성 실패"
            return 0
        fi

        if ilorest --cache-dir "$CACHE_DIR" rawput "$payload_file" > "$put_resp" 2>&1; then
            redact_file_append "$put_resp" "$WORKER_LOG"
            write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "REBOOT_REQUIRED" "1" "SmartStorageConfig 적용 완료 (재부팅 시 RAID 반영)"
        else
            redact_file_append "$put_resp" "$WORKER_LOG"
            local emsg
            emsg=$(extract_first_error_line "$put_resp")
            write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "FAILED" "0" "SmartStorageConfig rawput 실패: ${emsg}"
        fi
        return 0
    fi

    if [ "$p_backend" = "redfish_storage" ]; then
        if ! "$PYTHON_BIN" "$PARSER" build-redfish-payloads "$plan_file" "$work_dir" >> "$WORKER_LOG" 2>&1; then
            write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "FAILED" "0" "Redfish 페이로드 생성 실패"
            return 0
        fi

        local need_reboot=0
        local deleted_cnt=0
        local created_cnt=0

        if [ -s "${work_dir}/deletes.txt" ]; then
            local vol_uri
            while IFS= read -r vol_uri; do
                vol_uri=$(printf '%s' "$vol_uri" | tr -d '\r')
                [ -n "$vol_uri" ] || continue
                local del_resp="${work_dir}/del_${deleted_cnt}_resp.txt"
                if ilorest --cache-dir "$CACHE_DIR" rawdelete "$vol_uri" > "$del_resp" 2>&1; then
                    redact_file_append "$del_resp" "$WORKER_LOG"
                    deleted_cnt=$((deleted_cnt + 1))
                    local del_rb
                    del_rb=$(extract_links "$del_resp" check_reboot_msg | head -n 1)
                    if [ "$del_rb" = "REBOOT" ]; then
                        need_reboot=1
                    fi
                else
                    redact_file_append "$del_resp" "$WORKER_LOG"
                    local dmsg
                    dmsg=$(extract_first_error_line "$del_resp")
                    write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "FAILED" "0" "기존 볼륨 삭제 실패 (${vol_uri}): ${dmsg}"
                    return 0
                fi
            done < "${work_dir}/deletes.txt"
        fi

        local post_file
        for post_file in "${work_dir}"/post_*.json; do
            [ -f "$post_file" ] || continue
            local post_resp="${post_file%.json}_resp.txt"
            if ilorest --cache-dir "$CACHE_DIR" rawpost "$post_file" > "$post_resp" 2>&1; then
                redact_file_append "$post_resp" "$WORKER_LOG"
                created_cnt=$((created_cnt + 1))
                local post_rb
                post_rb=$(extract_links "$post_resp" check_reboot_msg | head -n 1)
                if [ "$post_rb" = "REBOOT" ]; then
                    need_reboot=1
                fi
            else
                redact_file_append "$post_resp" "$WORKER_LOG"
                local pemsg
                pemsg=$(extract_first_error_line "$post_resp")
                write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "FAILED" "0" "볼륨 생성 실패 ($(basename "$post_file")): ${pemsg}"
                return 0
            fi
        done

        if [ "$need_reboot" -eq 1 ]; then
            write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "REBOOT_REQUIRED" "1" "Redfish 볼륨 ${created_cnt}개 생성 요청 완료 (삭제 ${deleted_cnt}개, 재부팅 필요)"
        else
            write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "APPLIED" "0" "Redfish 볼륨 ${created_cnt}개 즉시 적용 완료 (삭제 ${deleted_cnt}개)"
        fi
        return 0
    fi

    write_result "$res_file" "$p_model" "$p_gen" "$p_backend" "FAILED" "0" "지원되지 않는 백엔드: ${p_backend}"
}

action_reboot() {
    local reb_dir="${RUN_TMP_DIR}/reboot_results"
    local reb_file="${reb_dir}/reb_${SAFE_IP}.json"
    local work_dir="${RUN_TMP_DIR}/reboot_${SAFE_IP}"
    mkdir -p "$reb_dir" "$work_dir"

    local r_model="Unknown"
    local r_gen="Unknown"
    local r_backend="unknown"
    local res_file="${RUN_TMP_DIR}/results/res_${SAFE_IP}.json"
    if [ -f "$res_file" ]; then
        r_model=$(read_plan_field "$res_file" "model")
        r_gen=$(read_plan_field "$res_file" "generation")
        r_backend=$(read_plan_field "$res_file" "backend")
        [ -n "$r_model" ] || r_model="Unknown"
        [ -n "$r_gen" ] || r_gen="Unknown"
        [ -n "$r_backend" ] || r_backend="unknown"
    fi

    local login_err="${work_dir}/login_err.tmp"
    if ! do_login "$login_err"; then
        local lmsg
        lmsg=$(extract_first_error_line "$login_err")
        rm -f "$login_err"
        write_result "$reb_file" "$r_model" "$r_gen" "$r_backend" "FAILED" "0" "재부팅용 iLO 로그인 실패: ${lmsg}"
        return 0
    fi

    local reb_resp="${work_dir}/reboot_resp.txt"
    if ilorest --cache-dir "$CACHE_DIR" reboot > "$reb_resp" 2>&1; then
        redact_file_append "$reb_resp" "$WORKER_LOG"
        write_result "$reb_file" "$r_model" "$r_gen" "$r_backend" "REBOOTED" "0" "재부팅 명령 전송 완료 (ilorest reboot)"
        return 0
    fi
    redact_file_append "$reb_resp" "$WORKER_LOG"

    # Fallback: Redfish ComputerSystem.Reset GracefulRestart
    local reset_payload="${work_dir}/reset_payload.json"
    printf '%s\n' '{"path": "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset", "body": {"ResetType": "GracefulRestart"}}' > "$reset_payload"

    local fb_resp="${work_dir}/reboot_fb_resp.txt"
    if ilorest --cache-dir "$CACHE_DIR" rawpost "$reset_payload" > "$fb_resp" 2>&1; then
        redact_file_append "$fb_resp" "$WORKER_LOG"
        write_result "$reb_file" "$r_model" "$r_gen" "$r_backend" "REBOOTED" "0" "재부팅 명령 전송 완료 (Redfish GracefulRestart)"
    else
        redact_file_append "$fb_resp" "$WORKER_LOG"
        local emsg
        emsg=$(extract_first_error_line "$fb_resp")
        write_result "$reb_file" "$r_model" "$r_gen" "$r_backend" "FAILED" "0" "재부팅 명령 실패: ${emsg}"
    fi
}

case "$ACTION" in
    discover)
        action_discover
        ;;
    apply)
        action_apply
        ;;
    reboot)
        action_reboot
        ;;
    *)
        echo "[ERROR] 알 수 없는 action: ${ACTION} (discover|apply|reboot)" >&2
        exit 1
        ;;
esac
