#!/usr/bin/env bash
# HPE 서버 관리망(iLO 4 / 5 / 6) IP 및 VLAN 일괄 변경 스크립트
# RHEL 6 (Bash 4.1) 및 최신 Linux 호환 (외부 의존성 없음: Pure Bash + POSIX 도구)

set -o pipefail

# ==============================================================================
# 전역 변수 및 기본 설정
# ==============================================================================
OPT_FILE="list.txt"
OPT_NETMASK="255.255.255.0"
OPT_GW=""
OPT_CHECK=0
OPT_YES=0
OPT_ILOREST_BIN=""

CLI_NETMASK_SET=0
CLI_GW_SET=0
CLI_CHECK_SET=0

ILOREST_BIN_PATH=""
CURRENT_TMP_JSON=""
LOGIN_ACTIVE=0
RUN_TS="$(date '+%Y%m%d_%H%M%S')"

# 파싱된 대상 서버 목록 (Bash 4.1 호환 인덱스 배열)
SRV_CUR_IP=()
SRV_NEW_IP=()
SRV_VLAN_ENABLE=()
SRV_VLAN_ID=()
SRV_VLAN_LABEL=()

# 결과 집계용 배열
RES_CUR_IP=()
RES_NEW_IP=()
RES_SUBNET=()
RES_GW=()
RES_VLAN=()
RES_STATUS=()
RES_NOTE=()

TOTAL_COUNT=0
SUCCESS_COUNT=0
FAIL_COUNT=0

# ==============================================================================
# 정리(Cleanup) 트랩
# ==============================================================================
cleanup() {
    if [[ -n "$CURRENT_TMP_JSON" && -e "$CURRENT_TMP_JSON" ]]; then
        rm -f "$CURRENT_TMP_JSON" >/dev/null 2>&1 || true
    fi
    if [[ "$LOGIN_ACTIVE" -eq 1 && -n "$ILOREST_BIN_PATH" && -x "$ILOREST_BIN_PATH" ]]; then
        "$ILOREST_BIN_PATH" --nologo logout >/dev/null 2>&1 || true
        LOGIN_ACTIVE=0
    fi
}
trap cleanup EXIT INT TERM

# ==============================================================================
# 도움말 출력
# ==============================================================================
print_usage() {
    cat <<'EOF'
사용법: change_ilo_ip.sh [옵션]

HPE 서버(iLO 4 / iLO 5 / iLO 6) 관리망 IP, 서브넷 마스크, 게이트웨이 및 VLAN을
일괄 조회(--check)하거나 변경합니다. (RHEL 6 Bash 4.1 이상 호환)

옵션:
  -f, --file <파일>          대상 IP 목록 파일 지정 (기본값: list.txt)
  -m, --netmask <넷마스크>   신규 서브넷 마스크 지정 (기본값: 255.255.255.0)
  -g, --gw <게이트웨이IP>    신규 게이트웨이 고정 IP 지정 (미지정 시 변경 IP 대역 .1 자동)
  -c, --check                사전 점검(Dry-run) 모드: 변경 없이 현재 상태 및 예정값 조회
  -y, --yes                  서브넷/게이트웨이/작업모드 확인 프롬프트 생략 (기본값/옵션 적용)
      --ilorest-bin <경로>   ilorest 실행 파일 경로 직접 지정
  -h, --help                 도움말 출력

환경 변수:
  ILO_USER                   iLO 접속 계정 ID (미설정 시 실행 중 입력 프롬프트 표시)
  ILO_PASS                   iLO 접속 비밀번호 (미설정 시 실행 중 비노출 입력 프롬프트 표시)
  ILOREST_BIN                ilorest 바이너리 경로 기본값 오버라이드

목록 파일 형식 (공백 또는 탭 구분 3컬럼, '#' 주석 허용):
  [현재 관리망 IP]  [변경될 관리망 IP]  [변경될 VLAN (1~4094, 해제 시 0 또는 off)]
  예시: 10.10.10.101 10.20.30.101 30
        10.10.10.103 10.20.40.15 0
EOF
}

# ==============================================================================
# 유틸리티 함수
# ==============================================================================
trim_ws() {
    local s="$1"
    s="${s#"${s%%[![:space:]]*}"}"
    s="${s%"${s##*[![:space:]]}"}"
    printf '%s' "$s"
}

is_valid_ipv4() {
    local ip="$1"
    local IFS='.'
    local octets=()
    local oct=""

    if [[ ! "$ip" =~ ^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$ ]]; then
        return 1
    fi

    read -r -a octets <<< "$ip"
    if [[ "${#octets[@]}" -ne 4 ]]; then
        return 1
    fi

    for oct in "${octets[@]}"; do
        if [[ ! "$oct" =~ ^([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$ ]]; then
            return 1
        fi
    done

    return 0
}

sanitize_note() {
    local text="$1"
    text="$(printf '%s' "$text" | tr '\r\n\t' '   ' | sed 's/  */ /g')"
    text="$(trim_ws "$text")"
    if [[ "${#text}" -gt 80 ]]; then
        text="${text:0:77}..."
    fi
    printf '%s' "$text"
}

# ==============================================================================
# CLI 인자 파싱
# ==============================================================================
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -f|--file)
                if [[ $# -lt 2 || -z "$2" ]]; then
                    printf '오류: %s 옵션에는 파일 경로가 필요합니다.\n' "$1" >&2
                    exit 1
                fi
                OPT_FILE="$2"
                shift 2
                ;;
            -m|--netmask)
                if [[ $# -lt 2 || -z "$2" ]]; then
                    printf '오류: %s 옵션에는 서브넷 마스크가 필요합니다.\n' "$1" >&2
                    exit 1
                fi
                OPT_NETMASK="$2"
                CLI_NETMASK_SET=1
                shift 2
                ;;
            -g|--gw)
                if [[ $# -lt 2 || -z "$2" ]]; then
                    printf '오류: %s 옵션에는 게이트웨이 IP가 필요합니다.\n' "$1" >&2
                    exit 1
                fi
                OPT_GW="$2"
                CLI_GW_SET=1
                shift 2
                ;;
            -c|--check)
                OPT_CHECK=1
                CLI_CHECK_SET=1
                shift
                ;;
            -y|--yes)
                OPT_YES=1
                shift
                ;;
            --ilorest-bin)
                if [[ $# -lt 2 || -z "$2" ]]; then
                    printf '오류: %s 옵션에는 ilorest 실행 경로가 필요합니다.\n' "$1" >&2
                    exit 1
                fi
                OPT_ILOREST_BIN="$2"
                shift 2
                ;;
            -h|--help)
                print_usage
                exit 0
                ;;
            *)
                printf '오류: 알 수 없는 옵션입니다: %s\n' "$1" >&2
                printf '도움말을 보려면 %s --help 를 실행하세요.\n' "$0" >&2
                exit 1
                ;;
        esac
    done
}

# ==============================================================================
# 사전 검증 (Pre-flight Validation)
# ==============================================================================
print_prelink_recovery() {
    local bin_path="$1"
    cat >&2 <<EOF

[심각 오류] ilorest 바이너리 실행 중 RHEL 6 prelink 손상 오류가 감지되었습니다 (Cannot open self).
RHEL 6 환경의 prelink 데몬이 바이너리 ELF 구조를 변경할 때 발생하는 알려진 문제입니다.

[조치 방법]
  1. /etc/prelink.conf 파일에 아래 예외 설정을 추가하세요:
     -b $bin_path
  2. 기존 prelink 변경 사항을 원복하세요:
     prelink -ua
  3. 필요 시 ilorest 패키지를 재설치한 뒤 스크립트를 다시 실행하세요:
     rpm -Uvh --replacepkgs ilorest-*.rpm   (또는 yum reinstall ilorest)
EOF
}

resolve_and_check_ilorest() {
    local candidate=""
    local resolved=""
    local ver_out=""
    local ver_rc=0

    if [[ -n "$OPT_ILOREST_BIN" ]]; then
        candidate="$OPT_ILOREST_BIN"
    elif [[ -n "${ILOREST_BIN:-}" ]]; then
        candidate="$ILOREST_BIN"
    elif command -v ilorest >/dev/null 2>&1; then
        candidate="$(command -v ilorest)"
    else
        candidate="/usr/sbin/ilorest"
    fi

    if [[ "$candidate" == */* ]]; then
        resolved="$candidate"
    elif command -v "$candidate" >/dev/null 2>&1; then
        resolved="$(command -v "$candidate")"
    else
        resolved="$candidate"
    fi

    if [[ ! -e "$resolved" || -d "$resolved" || ! -x "$resolved" ]]; then
        printf '오류: 실행 가능한 ilorest 바이너리를 찾을 수 없습니다: %s\n' "$resolved" >&2
        printf 'HPE ilorest 설치 여부를 확인하거나 --ilorest-bin 옵션으로 경로를 지정하세요.\n' >&2
        exit 1
    fi

    ILOREST_BIN_PATH="$resolved"

    ver_out="$("$ILOREST_BIN_PATH" --nologo --version 2>&1)"
    ver_rc=$?
    if printf '%s' "$ver_out" | grep -qi 'Cannot open self'; then
        print_prelink_recovery "$ILOREST_BIN_PATH"
        exit 1
    fi

    if [[ "$ver_rc" -ne 0 ]]; then
        ver_out="$("$ILOREST_BIN_PATH" --nologo --help 2>&1)"
        ver_rc=$?
        if printf '%s' "$ver_out" | grep -qi 'Cannot open self'; then
            print_prelink_recovery "$ILOREST_BIN_PATH"
            exit 1
        fi
        if [[ "$ver_rc" -ne 0 ]]; then
            printf '오류: ilorest 실행 점검에 실패했습니다 (%s).\n' "$ILOREST_BIN_PATH" >&2
            printf '출력: %s\n' "$(sanitize_note "$ver_out")" >&2
            exit 1
        fi
    fi
}

validate_cli_network_options() {
    if [[ "$CLI_NETMASK_SET" -eq 1 ]] && ! is_valid_ipv4 "$OPT_NETMASK"; then
        printf '오류: --netmask 값이 올바른 IPv4 형식이 아닙니다: %s\n' "$OPT_NETMASK" >&2
        exit 1
    fi

    if [[ "$CLI_GW_SET" -eq 1 ]] && ! is_valid_ipv4 "$OPT_GW"; then
        printf '오류: --gw 값이 올바른 IPv4 형식이 아닙니다: %s\n' "$OPT_GW" >&2
        exit 1
    fi
}

parse_and_validate_list() {
    local line_no=0
    local raw_line=""
    local clean_line=""
    local col1="" col2="" col3="" extra=""
    local vlan_lower=""
    local vlan_num=0
    local parse_errors=0

    if [[ ! -f "$OPT_FILE" || ! -r "$OPT_FILE" ]]; then
        printf '오류: 대상 목록 파일을 찾을 수 없거나 읽을 수 없습니다: %s\n' "$OPT_FILE" >&2
        exit 1
    fi

    while IFS= read -r raw_line || [[ -n "$raw_line" ]]; do
        line_no=$((line_no + 1))
        # CRLF(\r) 제거 및 앞뒤 공백 정리
        clean_line="${raw_line//$'\r'/}"
        clean_line="$(trim_ws "$clean_line")"

        # 빈 줄 또는 주석(#) 줄 건너뛰기
        if [[ -z "$clean_line" || "${clean_line:0:1}" == "#" ]]; then
            continue
        fi

        col1=""
        col2=""
        col3=""
        extra=""
        IFS=$' \t' read -r col1 col2 col3 extra <<< "$clean_line"

        if [[ -z "$col1" || -z "$col2" || -z "$col3" || -n "$extra" ]]; then
            printf '오류: [%s:%d] 형식 오류 - 정확히 3개 컬럼([현재IP] [변경IP] [VLAN])이어야 합니다: "%s"\n' \
                "$OPT_FILE" "$line_no" "$clean_line" >&2
            parse_errors=$((parse_errors + 1))
            continue
        fi

        if ! is_valid_ipv4 "$col1"; then
            printf '오류: [%s:%d] 현재 IP 형식이 올바르지 않습니다: "%s"\n' \
                "$OPT_FILE" "$line_no" "$col1" >&2
            parse_errors=$((parse_errors + 1))
            continue
        fi

        if ! is_valid_ipv4 "$col2"; then
            printf '오류: [%s:%d] 변경될 IP 형식이 올바르지 않습니다: "%s"\n' \
                "$OPT_FILE" "$line_no" "$col2" >&2
            parse_errors=$((parse_errors + 1))
            continue
        fi

        vlan_lower="$(printf '%s' "$col3" | tr '[:upper:]' '[:lower:]')"
        case "$vlan_lower" in
            0|off|disable|none)
                SRV_CUR_IP+=("$col1")
                SRV_NEW_IP+=("$col2")
                SRV_VLAN_ENABLE+=("false")
                SRV_VLAN_ID+=("0")
                SRV_VLAN_LABEL+=("해제(off)")
                ;;
            *)
                if [[ ! "$col3" =~ ^[0-9]+$ ]]; then
                    printf '오류: [%s:%d] VLAN 값이 올바르지 않습니다 (1~4094 또는 0/off/disable/none): "%s"\n' \
                        "$OPT_FILE" "$line_no" "$col3" >&2
                    parse_errors=$((parse_errors + 1))
                    continue
                fi
                vlan_num=$((10#$col3))
                if [[ "$vlan_num" -lt 1 || "$vlan_num" -gt 4094 ]]; then
                    printf '오류: [%s:%d] VLAN 범위 초과 (1~4094 허용): "%s"\n' \
                        "$OPT_FILE" "$line_no" "$col3" >&2
                    parse_errors=$((parse_errors + 1))
                    continue
                fi
                SRV_CUR_IP+=("$col1")
                SRV_NEW_IP+=("$col2")
                SRV_VLAN_ENABLE+=("true")
                SRV_VLAN_ID+=("$vlan_num")
                SRV_VLAN_LABEL+=("$vlan_num")
                ;;
        esac
    done < "$OPT_FILE"

    if [[ "$parse_errors" -gt 0 ]]; then
        printf '목록 파일 검증 실패(%d건 오류). 작업을 시작하지 않고 종료합니다.\n' "$parse_errors" >&2
        exit 1
    fi

    TOTAL_COUNT="${#SRV_CUR_IP[@]}"
    if [[ "$TOTAL_COUNT" -eq 0 ]]; then
        printf '오류: 목록 파일(%s)에 처리할 대상 서버가 없습니다.\n' "$OPT_FILE" >&2
        exit 1
    fi
}

# ==============================================================================
# 대화형 입력 (Interactive Prompts)
# ==============================================================================
prompt_credentials_and_options() {
    local input_val=""
    local mode_val=""
    local gw_rule=""

    if [[ -z "${ILO_USER:-}" ]]; then
        printf '관리망(iLO) 계정 ID [기본값: Administrator]: '
        IFS= read -r input_val || true
        input_val="$(trim_ws "$input_val")"
        if [[ -z "$input_val" ]]; then
            ILO_USER="Administrator"
        else
            ILO_USER="$input_val"
        fi
    fi

    while [[ -z "${ILO_PASS:-}" ]]; do
        printf '관리망(iLO) 비밀번호: '
        if ! IFS= read -r -s input_val; then
            printf '\n오류: 비밀번호 입력이 중단되었습니다(EOF).\n' >&2
            exit 1
        fi
        printf '\n'
        if [[ -z "$input_val" ]]; then
            printf '비밀번호는 비워둘 수 없습니다. 다시 입력하세요.\n' >&2
        else
            ILO_PASS="$input_val"
        fi
    done

    if [[ "$CLI_NETMASK_SET" -eq 0 && "$OPT_YES" -eq 0 ]]; then
        while true; do
            printf '신규 서브넷 마스크 [Enter=255.255.255.0]: '
            if ! IFS= read -r input_val; then
                OPT_NETMASK="255.255.255.0"
                break
            fi
            input_val="$(trim_ws "$input_val")"
            if [[ -z "$input_val" ]]; then
                OPT_NETMASK="255.255.255.0"
                break
            elif is_valid_ipv4 "$input_val"; then
                OPT_NETMASK="$input_val"
                break
            else
                printf '오류: 올바른 IPv4 서브넷 마스크 형식이 아닙니다: %s\n' "$input_val" >&2
            fi
        done
    fi

    if [[ "$CLI_GW_SET" -eq 0 && "$OPT_YES" -eq 0 ]]; then
        while true; do
            printf '신규 게이트웨이 [Enter=변경될 IP 대역 .1 자동 / 직접 입력 시 고정 IP]: '
            if ! IFS= read -r input_val; then
                OPT_GW=""
                break
            fi
            input_val="$(trim_ws "$input_val")"
            if [[ -z "$input_val" ]]; then
                OPT_GW=""
                break
            elif is_valid_ipv4 "$input_val"; then
                OPT_GW="$input_val"
                break
            else
                printf '오류: 올바른 IPv4 게이트웨이 형식이 아닙니다: %s\n' "$input_val" >&2
            fi
        done
    fi

    if [[ "$CLI_CHECK_SET" -eq 0 && "$OPT_YES" -eq 0 ]]; then
        if [[ -n "$OPT_GW" ]]; then
            gw_rule="$OPT_GW (고정 지정)"
        else
            gw_rule="변경될 IP 대역 .1 자동 계산"
        fi
        printf '\n================ 작업 실행 전 요약 ================\n'
        printf '  대상 대수     : %d 대\n' "$TOTAL_COUNT"
        printf '  목록 파일     : %s\n' "$OPT_FILE"
        printf '  신규 서브넷   : %s\n' "$OPT_NETMASK"
        printf '  게이트웨이    : %s\n' "$gw_rule"
        printf '=================================================\n'
        printf '작업 모드를 선택하세요 [y: 실제 변경 진행 / c: 사전 점검(조회만) / n: 취소]: '
        IFS= read -r mode_val || true
        mode_val="$(trim_ws "$mode_val")"
        case "$mode_val" in
            y|Y)
                OPT_CHECK=0
                ;;
            c|C)
                OPT_CHECK=1
                ;;
            *)
                printf '작업이 사용자에 의해 취소되었습니다.\n'
                exit 0
                ;;
        esac
    fi
}

# ==============================================================================
# Pure Bash + POSIX AWK Redfish JSON 파싱 함수
# ==============================================================================
collapse_json() {
    printf '%s' "$1" | tr '\r\n' '  '
}

json_extract_string() {
    local key="$1"
    local flat_json="$2"
    printf '%s' "$flat_json" | awk -v k="$key" '
    {
        pat = "\"" k "\"[[:space:]]*:[[:space:]]*\"[^\"]*\""
        if (match($0, pat)) {
            s = substr($0, RSTART, RLENGTH)
            sub(/^.*:[[:space:]]*"/, "", s)
            sub(/"$/, "", s)
            print s
        }
    }'
}

json_extract_eth_uris() {
    local flat_json="$1"
    printf '%s' "$flat_json" | awk '
    {
        line = $0
        while (match(line, /"@odata\.id"[[:space:]]*:[[:space:]]*"[^"]*\/EthernetInterfaces\/[^"\/]+\/?"/)) {
            s = substr(line, RSTART, RLENGTH)
            sub(/^.*:[[:space:]]*"/, "", s)
            sub(/"$/, "", s)
            if (!(s in seen)) {
                seen[s] = 1
                print s
            }
            line = substr(line, RSTART + RLENGTH)
        }
    }'
}

# 경계 안전(Boundary-safe) IPv4 정확 일치 검사: "10.0.0.1"이 "10.0.0.10"에 매칭되지 않도록 함
json_has_exact_address() {
    local flat_json="$1"
    local target_ip="$2"
    printf '%s' "$flat_json" | awk -v ip="$target_ip" '
    BEGIN { found = 0 }
    {
        line = $0
        while (match(line, /"Address"[[:space:]]*:[[:space:]]*"[^"]*"/)) {
            s = substr(line, RSTART, RLENGTH)
            sub(/^.*:[[:space:]]*"/, "", s)
            sub(/"$/, "", s)
            if (s == ip) {
                found = 1
                break
            }
            line = substr(line, RSTART + RLENGTH)
        }
    }
    END { exit (found ? 0 : 1) }'
}

# 균형 잡힌 JSON 객체({ ... }) 블록 추출 (문자열 내부 괄호 무시)
json_extract_object_block() {
    local key="$1"
    local flat_json="$2"
    printf '%s' "$flat_json" | awk -v k="$key" '
    {
        pat = "\"" k "\"[[:space:]]*:[[:space:]]*\\{"
        if (!match($0, pat)) {
            exit 0
        }
        start_pos = RSTART + RLENGTH - 1
        len = length($0)
        depth = 0
        in_str = 0
        esc = 0
        for (i = start_pos; i <= len; i++) {
            c = substr($0, i, 1)
            if (in_str) {
                if (esc) {
                    esc = 0
                } else if (c == "\\") {
                    esc = 1
                } else if (c == "\"") {
                    in_str = 0
                }
            } else {
                if (c == "\"") {
                    in_str = 1
                } else if (c == "{") {
                    depth++
                } else if (c == "}") {
                    depth--
                    if (depth == 0) {
                        print substr($0, start_pos, i - start_pos + 1)
                        exit 0
                    }
                }
            }
        }
    }'
}

# 현재 IP가 속한 IP 객체 내부에서 SubnetMask / Gateway 추출 (0.0.0.0 등 정적 빈 값 오탐 방지)
json_extract_ip_field() {
    local flat_json="$1"
    local target_ip="$2"
    local field="$3"
    printf '%s' "$flat_json" | awk -v ip="$target_ip" -v fld="$field" '
    function get_fld(blk, f,    pat, val) {
        pat = "\"" f "\"[[:space:]]*:[[:space:]]*\"[^\"]*\""
        if (match(blk, pat)) {
            val = substr(blk, RSTART, RLENGTH)
            sub(/^.*:[[:space:]]*"/, "", val)
            sub(/"$/, "", val)
            return val
        }
        return ""
    }
    {
        len = length($0)
        in_str = 0
        esc = 0
        depth = 0
        obj_start = 0
        fallback = ""

        for (i = 1; i <= len; i++) {
            c = substr($0, i, 1)
            if (in_str) {
                if (esc) {
                    esc = 0
                } else if (c == "\\") {
                    esc = 1
                } else if (c == "\"") {
                    in_str = 0
                }
            } else {
                if (c == "\"") {
                    in_str = 1
                } else if (c == "{") {
                    obj_start_stack[++depth] = i
                } else if (c == "}") {
                    if (depth > 0) {
                        s_pos = obj_start_stack[depth]
                        depth--
                        blk = substr($0, s_pos, i - s_pos + 1)
                        # 가장 안쪽 IP 주소 객체 판단 ("Address" 포함 && 중첩 객체 미포함)
                        if (index(blk, "\"Address\"") > 0 && index(substr(blk, 2), "{") == 0) {
                            addr = get_fld(blk, "Address")
                            v = get_fld(blk, fld)
                            if (addr == ip && v != "") {
                                print v
                                exit 0
                            }
                            if (fallback == "" && v != "" && v != "0.0.0.0") {
                                fallback = v
                            }
                        }
                    }
                }
            }
        }
        if (fallback != "") {
            print fallback
        } else {
            v = get_fld($0, fld)
            if (v != "") {
                print v
            }
        }
    }'
}

json_extract_vlan_info() {
    local flat_json="$1"
    local vlan_blk=""
    local v_en=""
    local v_id=""

    vlan_blk="$(json_extract_object_block "VLAN" "$flat_json")"
    if [[ -z "$vlan_blk" ]]; then
        vlan_blk="$flat_json"
    fi

    v_en="$(printf '%s' "$vlan_blk" | awk '
    {
        if (match($0, /"VLANEnable"[[:space:]]*:[[:space:]]*(true|false)/)) {
            s = substr($0, RSTART, RLENGTH)
            sub(/^.*:[[:space:]]*/, "", s)
            print s
        }
    }')"

    v_id="$(printf '%s' "$vlan_blk" | awk '
    {
        if (match($0, /"VLANId"[[:space:]]*:[[:space:]]*[0-9]+/)) {
            s = substr($0, RSTART, RLENGTH)
            sub(/^.*:[[:space:]]*/, "", s)
            print s
        }
    }')"

    printf '%s|%s' "${v_en:-unknown}" "${v_id:-0}"
}

json_detect_oem_dhcp_key() {
    local flat_json="$1"
    local oem_blk=""
    local vendor_blk=""

    oem_blk="$(json_extract_object_block "Oem" "$flat_json")"
    if [[ -z "$oem_blk" ]]; then
        printf ''
        return 0
    fi

    vendor_blk="$(json_extract_object_block "Hpe" "$oem_blk")"
    if [[ -n "$vendor_blk" ]] && printf '%s' "$vendor_blk" | grep -q '"DHCPv4"'; then
        printf 'Hpe'
        return 0
    fi

    vendor_blk="$(json_extract_object_block "Hp" "$oem_blk")"
    if [[ -n "$vendor_blk" ]] && printf '%s' "$vendor_blk" | grep -q '"DHCPv4"'; then
        printf 'Hp'
        return 0
    fi

    printf ''
}

# ==============================================================================
# 서버 1대 처리 (순차 실행 및 장애 격리)
# ==============================================================================
record_result() {
    local cur_ip="$1"
    local new_ip="$2"
    local subnet="$3"
    local gw="$4"
    local vlan="$5"
    local status="$6"
    local note="$7"

    RES_CUR_IP+=("$cur_ip")
    RES_NEW_IP+=("$new_ip")
    RES_SUBNET+=("$subnet")
    RES_GW+=("$gw")
    RES_VLAN+=("$vlan")
    RES_STATUS+=("$status")
    RES_NOTE+=("$note")
}

write_patch_json() {
    local out_file="$1"
    local iface_uri="$2"
    local new_ip="$3"
    local subnet="$4"
    local gw="$5"
    local vlan_enable="$6"
    local vlan_id="$7"
    local oem_key="$8"

    {
        printf '{\n'
        printf '  "%s": {\n' "$iface_uri"
        printf '    "DHCPv4": {\n'
        printf '      "DHCPEnabled": false\n'
        printf '    },\n'
        if [[ -n "$oem_key" ]]; then
            printf '    "Oem": {\n'
            printf '      "%s": {\n' "$oem_key"
            printf '        "DHCPv4": {\n'
            printf '          "Enabled": false\n'
            printf '        }\n'
            printf '      }\n'
            printf '    },\n'
        fi
        printf '    "IPv4StaticAddresses": [\n'
        printf '      {\n'
        printf '        "Address": "%s",\n' "$new_ip"
        printf '        "SubnetMask": "%s",\n' "$subnet"
        printf '        "Gateway": "%s"\n' "$gw"
        printf '      }\n'
        printf '    ],\n'
        if [[ "$vlan_enable" == "true" ]]; then
            printf '    "VLAN": {\n'
            printf '      "VLANEnable": true,\n'
            printf '      "VLANId": %s\n' "$vlan_id"
            printf '    }\n'
        else
            printf '    "VLAN": {\n'
            printf '      "VLANEnable": false\n'
            printf '    }\n'
        fi
        printf '  }\n'
        printf '}\n'
    } > "$out_file"
}

process_server() {
    local idx="$1"
    local cur_ip="${SRV_CUR_IP[$idx]}"
    local new_ip="${SRV_NEW_IP[$idx]}"
    local vlan_en="${SRV_VLAN_ENABLE[$idx]}"
    local vlan_id="${SRV_VLAN_ID[$idx]}"
    local vlan_label="${SRV_VLAN_LABEL[$idx]}"
    local target_gw=""

    local cmd_out=""
    local rc=0
    local mgr_flat=""
    local fw_ver=""
    local eth_list_raw=""
    local eth_list_flat=""
    local eth_uris=()
    local uri=""
    local matched_uri=""
    local matched_raw=""
    local matched_flat=""
    local cur_subnet=""
    local cur_gw=""
    local cur_vlan_pair=""
    local cur_vlan_en=""
    local cur_vlan_id=""
    local cur_vlan_disp=""
    local oem_key=""
    local backup_dir=""
    local backup_file=""

    if [[ -n "$OPT_GW" ]]; then
        target_gw="$OPT_GW"
    else
        target_gw="${new_ip%.*}.1"
    fi

    printf '\n--------------------------------------------------\n'
    printf '[%d/%d] 대상 서버 처리 중: %s -> %s (VLAN: %s)\n' \
        "$((idx + 1))" "$TOTAL_COUNT" "$cur_ip" "$new_ip" "$vlan_label"

    # 1. 기존 세션 초기화
    "$ILOREST_BIN_PATH" --nologo logout >/dev/null 2>&1 || true

    # 2. 로그인
    LOGIN_ACTIVE=1
    cmd_out="$("$ILOREST_BIN_PATH" --nologo login "$cur_ip" -u "$ILO_USER" -p "$ILO_PASS" 2>&1)"
    rc=$?
    if [[ "$rc" -ne 0 ]]; then
        printf '  [실패] iLO 로그인 실패 (%s)\n' "$(sanitize_note "$cmd_out")" >&2
        "$ILOREST_BIN_PATH" --nologo logout >/dev/null 2>&1 || true
        LOGIN_ACTIVE=0
        FAIL_COUNT=$((FAIL_COUNT + 1))
        record_result "$cur_ip" "$new_ip" "$OPT_NETMASK" "$target_gw" "$vlan_label" \
            "실패" "로그인 실패: $(sanitize_note "$cmd_out")"
        return 0
    fi

    # 3. iLO 버전/펌웨어 조회
    cmd_out="$("$ILOREST_BIN_PATH" --nologo rawget "/redfish/v1/Managers/1" 2>&1)"
    rc=$?
    if [[ "$rc" -ne 0 ]]; then
        printf '  [실패] /redfish/v1/Managers/1 조회 실패 (%s)\n' "$(sanitize_note "$cmd_out")" >&2
        "$ILOREST_BIN_PATH" --nologo logout >/dev/null 2>&1 || true
        LOGIN_ACTIVE=0
        FAIL_COUNT=$((FAIL_COUNT + 1))
        record_result "$cur_ip" "$new_ip" "$OPT_NETMASK" "$target_gw" "$vlan_label" \
            "실패" "Managers 조회 실패: $(sanitize_note "$cmd_out")"
        return 0
    fi
    mgr_flat="$(collapse_json "$cmd_out")"
    fw_ver="$(json_extract_string "FirmwareVersion" "$mgr_flat")"
    if [[ -z "$fw_ver" ]]; then
        fw_ver="iLO (버전 미확인)"
    fi

    # 4. EthernetInterfaces 멤버 URI 목록 조회
    eth_list_raw="$("$ILOREST_BIN_PATH" --nologo rawget "/redfish/v1/Managers/1/EthernetInterfaces" 2>&1)"
    rc=$?
    if [[ "$rc" -ne 0 ]]; then
        printf '  [실패] EthernetInterfaces 목록 조회 실패 (%s)\n' "$(sanitize_note "$eth_list_raw")" >&2
        "$ILOREST_BIN_PATH" --nologo logout >/dev/null 2>&1 || true
        LOGIN_ACTIVE=0
        FAIL_COUNT=$((FAIL_COUNT + 1))
        record_result "$cur_ip" "$new_ip" "$OPT_NETMASK" "$target_gw" "$vlan_label" \
            "실패" "인터페이스 목록 조회 실패"
        return 0
    fi
    eth_list_flat="$(collapse_json "$eth_list_raw")"

    while IFS= read -r uri; do
        if [[ -n "$uri" ]]; then
            eth_uris+=("$uri")
        fi
    done < <(json_extract_eth_uris "$eth_list_flat")

    if [[ "${#eth_uris[@]}" -eq 0 ]]; then
        printf '  [실패] 조회된 EthernetInterfaces URI가 없습니다.\n' >&2
        "$ILOREST_BIN_PATH" --nologo logout >/dev/null 2>&1 || true
        LOGIN_ACTIVE=0
        FAIL_COUNT=$((FAIL_COUNT + 1))
        record_result "$cur_ip" "$new_ip" "$OPT_NETMASK" "$target_gw" "$vlan_label" \
            "실패" "네트워크 인터페이스 없음"
        return 0
    fi

    # 5. 현재 IP와 정확히 일치하는 활성 포트 자동 탐색
    for uri in "${eth_uris[@]}"; do
        cmd_out="$("$ILOREST_BIN_PATH" --nologo rawget "$uri" 2>&1)"
        if [[ $? -ne 0 ]]; then
            continue
        fi
        local flat_cand=""
        flat_cand="$(collapse_json "$cmd_out")"
        if json_has_exact_address "$flat_cand" "$cur_ip"; then
            matched_uri="$uri"
            matched_raw="$cmd_out"
            matched_flat="$flat_cand"
            break
        fi
    done

    if [[ -z "$matched_uri" ]]; then
        printf '  [실패] 현재 IP(%s)와 일치하는 관리망 포트를 찾지 못했습니다.\n' "$cur_ip" >&2
        "$ILOREST_BIN_PATH" --nologo logout >/dev/null 2>&1 || true
        LOGIN_ACTIVE=0
        FAIL_COUNT=$((FAIL_COUNT + 1))
        record_result "$cur_ip" "$new_ip" "$OPT_NETMASK" "$target_gw" "$vlan_label" \
            "실패" "일치 포트 미발견"
        return 0
    fi

    # 현재 네트워크 설정 및 OEM 키 추출
    cur_subnet="$(json_extract_ip_field "$matched_flat" "$cur_ip" "SubnetMask")"
    cur_gw="$(json_extract_ip_field "$matched_flat" "$cur_ip" "Gateway")"
    cur_vlan_pair="$(json_extract_vlan_info "$matched_flat")"
    cur_vlan_en="${cur_vlan_pair%%|*}"
    cur_vlan_id="${cur_vlan_pair##*|}"
    if [[ "$cur_vlan_en" == "true" ]]; then
        cur_vlan_disp="활성(ID:$cur_vlan_id)"
    elif [[ "$cur_vlan_en" == "false" ]]; then
        cur_vlan_disp="비활성"
    else
        cur_vlan_disp="미확인"
    fi
    oem_key="$(json_detect_oem_dhcp_key "$matched_flat")"

    printf '  - iLO 버전    : %s\n' "$fw_ver"
    printf '  - 활성 포트   : %s (OEM 키: %s)\n' "$matched_uri" "${oem_key:-없음}"
    printf '  - 현재 설정   : IP=%s, 서브넷=%s, GW=%s, VLAN=%s\n' \
        "$cur_ip" "${cur_subnet:-미확인}" "${cur_gw:-미확인}" "$cur_vlan_disp"
    printf '  - 변경 예정   : IP=%s, 서브넷=%s, GW=%s, VLAN=%s\n' \
        "$new_ip" "$OPT_NETMASK" "$target_gw" "$vlan_label"

    # 6. 사전 점검 모드 vs 실제 변경 모드 분기
    if [[ "$OPT_CHECK" -eq 1 ]]; then
        "$ILOREST_BIN_PATH" --nologo logout >/dev/null 2>&1 || true
        LOGIN_ACTIVE=0
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        printf '  [점검완료] 변경 사항을 적용하지 않고 다음 서버로 이동합니다.\n'
        record_result "$cur_ip" "$new_ip" "$OPT_NETMASK" "$target_gw" "$vlan_label" \
            "점검완료" "$fw_ver | $matched_uri"
        return 0
    fi

    # 7. 실제 변경 모드: 백업 저장
    backup_dir="logs/backup_${RUN_TS}"
    mkdir -p "$backup_dir"
    backup_file="${backup_dir}/${cur_ip}.json"
    printf '%s\n' "$matched_raw" > "$backup_file"
    chmod 600 "$backup_file"
    printf '  - 원본 백업   : %s\n' "$backup_file"

    # 8. 임시 JSON 생성 및 rawpatch 실행
    CURRENT_TMP_JSON="$(mktemp "${TMPDIR:-/tmp}/ilo_patch.XXXXXX")"
    chmod 600 "$CURRENT_TMP_JSON"
    write_patch_json "$CURRENT_TMP_JSON" "$matched_uri" "$new_ip" "$OPT_NETMASK" "$target_gw" \
        "$vlan_en" "$vlan_id" "$oem_key"

    cmd_out="$("$ILOREST_BIN_PATH" --nologo rawpatch "$CURRENT_TMP_JSON" 2>&1)"
    rc=$?
    rm -f "$CURRENT_TMP_JSON" >/dev/null 2>&1 || true
    CURRENT_TMP_JSON=""

    if [[ "$rc" -ne 0 ]]; then
        printf '  [실패] rawpatch 적용 실패 — iloreset을 실행하지 않고 건너뜁니다. (%s)\n' \
            "$(sanitize_note "$cmd_out")" >&2
        "$ILOREST_BIN_PATH" --nologo logout >/dev/null 2>&1 || true
        LOGIN_ACTIVE=0
        FAIL_COUNT=$((FAIL_COUNT + 1))
        record_result "$cur_ip" "$new_ip" "$OPT_NETMASK" "$target_gw" "$vlan_label" \
            "실패" "rawpatch 실패: $(sanitize_note "$cmd_out")"
        return 0
    fi

    # 9. iLO 재시작 (iloreset)
    cmd_out="$("$ILOREST_BIN_PATH" --nologo iloreset 2>&1)"
    rc=$?
    if [[ "$rc" -ne 0 ]]; then
        printf '  [실패] iloreset 실행 실패 (%s)\n' "$(sanitize_note "$cmd_out")" >&2
        "$ILOREST_BIN_PATH" --nologo logout >/dev/null 2>&1 || true
        LOGIN_ACTIVE=0
        FAIL_COUNT=$((FAIL_COUNT + 1))
        record_result "$cur_ip" "$new_ip" "$OPT_NETMASK" "$target_gw" "$vlan_label" \
            "실패" "iloreset 실패: $(sanitize_note "$cmd_out")"
        return 0
    fi

    "$ILOREST_BIN_PATH" --nologo logout >/dev/null 2>&1 || true
    LOGIN_ACTIVE=0
    SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
    printf '  [성공] 설정 패치 및 iLO 재시작 명령 완료 (약 30~60초 후 신규 IP로 전환)\n'
    record_result "$cur_ip" "$new_ip" "$OPT_NETMASK" "$target_gw" "$vlan_label" \
        "성공" "적용완료 ($fw_ver)"
}

# ==============================================================================
# 최종 요약 출력 및 로그 저장
# ==============================================================================
print_and_save_summary() {
    local result_log="logs/result_${RUN_TS}.log"
    local mode_desc="실제 변경"
    local i=0
    local report_text=""

    if [[ "$OPT_CHECK" -eq 1 ]]; then
        mode_desc="사전 점검(Dry-run)"
    fi

    mkdir -p "logs"

    report_text="$({
        printf '\n========================================================================================================\n'
        printf 'HPE iLO 관리망 IP/VLAN 일괄 작업 결과 요약 (%s | 실행일시: %s)\n' "$mode_desc" "$RUN_TS"
        printf '========================================================================================================\n'
        printf '%-4s | %-15s | %-15s | %-15s | %-15s | %-10s | %-8s | %s\n' \
            "번호" "현재IP" "변경IP" "서브넷" "게이트웨이" "VLAN" "상태" "비고"
        printf '%s\n' "--------------------------------------------------------------------------------------------------------"
        for ((i = 0; i < TOTAL_COUNT; i++)); do
            printf '%-4d | %-15s | %-15s | %-15s | %-15s | %-10s | %-8s | %s\n' \
                "$((i + 1))" \
                "${RES_CUR_IP[$i]}" \
                "${RES_NEW_IP[$i]}" \
                "${RES_SUBNET[$i]}" \
                "${RES_GW[$i]}" \
                "${RES_VLAN[$i]}" \
                "${RES_STATUS[$i]}" \
                "${RES_NOTE[$i]}"
        done
        printf '========================================================================================================\n'
        printf '전체: %d 대 | 성공(완료): %d 대 | 실패: %d 대\n' \
            "$TOTAL_COUNT" "$SUCCESS_COUNT" "$FAIL_COUNT"
        printf '결과 로그 저장 위치: %s\n' "$result_log"
        printf '========================================================================================================\n'
    })"

    printf '%s\n' "$report_text"
    printf '%s\n' "$report_text" > "$result_log"
}

# ==============================================================================
# 메인 실행 흐름
# ==============================================================================
main() {
    local idx=0

    parse_args "$@"
    validate_cli_network_options
    resolve_and_check_ilorest
    parse_and_validate_list
    prompt_credentials_and_options

    for ((idx = 0; idx < TOTAL_COUNT; idx++)); do
        process_server "$idx"
    done

    print_and_save_summary

    if [[ "$FAIL_COUNT" -gt 0 ]]; then
        exit 1
    fi
    exit 0
}

main "$@"
