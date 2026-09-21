#!/bin/bash
# config_check.sh - OS 체크 + 환경설정 적용 + 재체크 + 벤더 코멘트 (gossh 기반)
#
# 사용: bash config_check.sh   (user 번호 선택 → 작업 진행 y/n → 환경설정 수정 y/n/set)
# 대상 목록: 실행한 디렉토리의 ${user}.txt (공백/줄바꿈 구분, 혼용 가능)
# 상세: README.md / 계획서.md

############################ 설정 ############################
home="/user/siy/MGMT"
setting_home="/user/svrauto/SA/check/SETTING"
user_info_mn="$home/INFO/USER/info_mn.sh"
user_info_output="$home/INFO/USER/info.sh"
appl_setting_file="${setting_home}/appl_change.sh"
setting_file="${setting_home}/setting.sh"
insert_file="${setting_home}/setting_insert.sh"
check_script="/user/svrauto/SA/check/os/run.sh"
vm_inventory="/root/server_vm_python/vcenter_mgmt/vcenter_vm_inventory_status.txt"

os6_host=""            # 이 서버의 hostname이 이 값이면 OS6용 gossh를 PATH에 추가
uptime_enable_user=""  # uptime 확인 대상 user (예: user1|user2)
ai_server_list=""      # AI GPU 서버 hostname (예: host1|host2, 완전 일치)
ai_server_script=""    # AI GPU 서버에서 실행할 스크립트
dhcp_server=""         # DHCP 정보 조회 서버
###############################################################

R=$'\033[31m'; G=$'\033[32m'; Y=$'\033[33m'; B=$'\033[5;31m'; N=$'\033[0m'
red()    { printf '%s%s%s\n' "$R" "$*" "$N"; }
green()  { printf '%s%s%s\n' "$G" "$*" "$N"; }
yellow() { printf '%s%s%s\n' "$Y" "$*" "$N"; }

TMP=$(mktemp -d /tmp/config_check.XXXXXX) || exit 1
trap 'rm -rf "$TMP"' EXIT
trap 'echo; exit 130' INT TERM

# gossh 는 Ctrl+C 를 직접 처리한다(1회: 진행 호스트 표시, 2회: 걸린 호스트 취소, 3회: 중단).
# 이 구간에서는 스크립트가 INT 를 무시해야 gossh 의 동작이 그대로 유지된다.
gsh() {
    trap '' INT
    gossh "$@"
    local rc=$?
    trap 'echo; exit 130' INT
    return $rc
}

if [ -n "$os6_host" ] && { [ "$(hostname)" = "$os6_host" ] || [ "$(hostname -s)" = "$os6_host" ]; }; then
    export PATH="/user/siy/gossh/OS6:$PATH"
fi

# 파일의 줄 수
lines() { awk 'END{print NR}' "$1" 2>/dev/null; }

# 벤더 판별 (호스트 시작 일치). awk 함수 원문.
VENDOR_AWK='function vendor(h) {
    if (h ~ /^(c|h|sh|s2h|s3h|s4h)/) return "D"
    if (h ~ /^s/) return "S"
    if (h ~ /^p/) return "L"
    if (h ~ /^l/) return "J"
    if (h ~ /^d/) return "T"
    if (h ~ /^m/) return "W"
    return "X"
}'

# gossh 결과 파일의 압축 표기(host[01-03], host[0001-0002]ev[01-03])를 풀어 "호스트<TAB>태그"로 출력
EXPAND_AWK='function expand(tok, tag,   p, q, pre, body, post, d, lo, hi, w, i) {
    p = index(tok, "[")
    if (!p) { if (tok != "") print tok "\t" tag; return }
    q = index(tok, "]")
    pre = substr(tok, 1, p - 1); body = substr(tok, p + 1, q - p - 1); post = substr(tok, q + 1)
    d = index(body, "-")
    if (!d) { expand(pre body post, tag); return }
    lo = substr(body, 1, d - 1); hi = substr(body, d + 1); w = length(lo)
    for (i = lo + 0; i <= hi + 0; i++) expand(pre sprintf("%0" w "d", i) post, tag)
}
{
    tag = $1; line = $0; sub(/^[^\t]*\t/, "", line); gsub(/\r/, "", line)
    n = split(line, parts, ",")
    for (i = 1; i <= n; i++) expand(parts[i], tag)
}'

########################## 1. user 선택 ##########################
bash "$user_info_mn"
read -r -p "input Number: " user_choice || exit 1
user=$(bash "$user_info_output" "$user_choice" | tr -d '\r' | awk 'NF{print $1; exit}')
[ -z "$user" ] && { echo "user를 확인할 수 없습니다."; exit 1; }
[ -f "${user}.txt" ] || { echo "대상 목록 파일이 없습니다: $(pwd)/${user}.txt"; exit 1; }

########################## 2. 리스트 확인 ##########################
HOSTS="$TMP/hosts"
tr -d '\r' < "${user}.txt" | tr -s '[:space:]' '\n' | awk 'NF && !seen[$0]++' > "$HOSTS"
[ -s "$HOSTS" ] || { echo "대상 호스트가 없습니다."; exit 1; }
TOTAL=$(lines "$HOSTS")

echo "[$user] 작업 대상"
awk -v rows=30 '{h[NR]=$0; if (length($0) > w) w = length($0)} END{
    for (r = 1; r <= rows && r <= NR; r++) {
        line = ""
        for (i = r; i <= NR; i += rows) line = line (i == r ? "" : "  ") (i + rows <= NR ? sprintf("%-" w "s", h[i]) : h[i])
        print line
    }
}' "$HOSTS"
echo
awk '{
    p = $0; sub(/[0-9].*$/, "", p); if (p == "") p = $0
    if (!(p in c)) o[++k] = p
    c[p]++
} END {
    for (i = 1; i <= k; i++) printf "%s * %dea\n", o[i], c[o[i]]
    printf "총 %d EA\n", NR
}' "$HOSTS"
echo

########################## 3. 작업 진행 여부 ##########################
while :; do
    read -r -p "작업을 진행하시겠습니까? (y/n): " ans || exit 1
    case "$ans" in y|Y) break ;; n|N) exit 0 ;; esac
done
if [ -n "$uptime_enable_user" ] && [[ "$user" =~ ^($uptime_enable_user)$ ]]; then
    gsh -script -w "$HOSTS" "uptime"
    echo
fi

########################## 체크 함수 ##########################
# run_check <대상파일> <이름> : gossh 1회 실행 후 $TMP/<이름>.{out,state,fail,usb0,ldap} 생성
#   state : 호스트<TAB>OK|OFF|REF|NOSV|INST|NONE  (NONE = 접속은 됐으나 체크 결과가 한 줄도 없음)
run_check() {
    local t=$1 n=$2
    rm -f "${t}_res_off" "${t}_res_refsed" "${t}_os_install" "${t}_nosvrauto" "${t}_res_cancel"
    # -pm 은 반드시 -w 보다 앞에 둔다. run.sh 의 "pd" 인자는 OK 가 아닌 결과값(INFO 등)까지 출력하게 한다.
    # "; :" 는 run.sh가 0이 아닌 값으로 끝나도 gossh가 출력 전체를 stderr(ERROR:)로 보내지 않고
    # stdout으로 내보내게 하기 위한 것
    gsh -pm -script -w "$t" "bash $check_script pd; :" > "$TMP/$n.out" 2> "$TMP/$n.err"

    { for m in _res_off:OFF _res_cancel:OFF _res_refsed:REF _os_install:INST _nosvrauto:NOSV; do
          [ -s "${t}${m%%:*}" ] && sed "s/^/${m##*:}\t/" "${t}${m%%:*}"
      done; } | awk -F'\t' "$EXPAND_AWK" > "$TMP/$n.set"
    awk -F'\t' 'FILENAME == ARGV[1] { if (!($1 in st)) st[$1] = $2; next }
                { print $1 "\t" (($1 in st) ? st[$1] : "OK") }' "$TMP/$n.set" "$t" > "$TMP/$n.state"

    # gossh 출력은 병렬이라 순서가 섞여 있으므로 호스트명 기준으로 정렬(호스트 내 줄 순서는 유지)
    LC_ALL=C sort -s -t: -k1,1 "$TMP/$n.out" -o "$TMP/$n.out"
    : > "$TMP/$n.fail"; : > "$TMP/$n.usb0"; : > "$TMP/$n.ldap"; : > "$TMP/$n.info"
    # 결과: .fail(FAIL 줄) .usb0 .ldap(INFO/FAIL ldap 값) .info(INFO 줄 중 KERNEL 포함, 값=INFO 뒤 문자열)
    # OK 이지만 결과가 한 줄도 없는 호스트는 NONE 으로 바꿔 .state 를 다시 쓴다
    awk -F'\t' -v P="$TMP/$n" '
        FILENAME == ARGV[1] { st[$1] = $2; order[++no] = $1; next }
        {
            line = $0; sub(/\r$/, "", line)
            i = index(line, ": "); if (!i) next
            h = substr(line, 1, i - 1); b = substr(line, i + 2)
            if (st[h] != "OK") next
            has[h] = 1
            m = split(b, f, "\t")
            s = f[1]; gsub(/^ +| +$/, "", s)
            if (s == "FAIL") {
                print line > (P ".fail")
                if (b ~ /usb0/ && b ~ /interface/ && !(h in usb)) { usb[h] = 1; print h > (P ".usb0") }
            }
            if (s == "INFO" || s == "FAIL") {
                k = f[2]; gsub(/^ +| +$/, "", k)
                if (tolower(k) == "ldap" && !(h in lv)) { v = f[3]; gsub(/^ +| +$/, "", v); lv[h] = v; print h "\t" v > (P ".ldap") }
            }
            if (s == "INFO" && b ~ /KERNEL/ && !(h in iv)) {
                v = f[2]; gsub(/^ +| +$/, "", v); iv[h] = v; print h "\t" v > (P ".info")
            }
        }
        END {
            for (i = 1; i <= no; i++) {
                h = order[i]; s = st[h]
                if (s == "OK" && !(h in has)) s = "NONE"
                print h "\t" s > (P ".state.new")
            }
        }' "$TMP/$n.state" "$TMP/$n.out"
    mv "$TMP/$n.state.new" "$TMP/$n.state"
    cp "$TMP/$n.out" "check.res_${user}"
}

report_fail() {   # $1=이름
    if [ -s "$TMP/$1.fail" ]; then
        while IFS= read -r l; do red "$l"; done < "$TMP/$1.fail"
    else
        green "NO FAIL"
    fi
}

# report_multi <값파일> <state 파일> <모드> <요약파일>
#   값파일: 호스트<TAB>값. 값이 하나면 그대로, 2종류 이상이면 값별 대수 + 소수 값 호스트를 노란색으로 출력.
#   모드 ldap : "LDAP : 값" 한 줄로 출력 (+ LDAP 값이 없는 OK 호스트를 미확인으로 표시)
#   모드 info : 값 요약을 <요약파일>에 써서 상태줄(report_status)이 붙이게 함
report_multi() {
    awk -F'\t' -v Y="$Y" -v N="$N" -v mode="$3" -v sumf="$4" '
        FILENAME == ARGV[1] { if ($2 == "OK") { ord_h[++nh] = $1 } ; next }
        { cnt[$2]++; hs[$2] = hs[$2] " " $1; got[$1] = 1; if (!($2 in seen)) { seen[$2] = 1; ord[++k] = $2 } }
        END {
            for (i = 2; i <= k; i++) { x = ord[i]; j = i - 1; while (j >= 1 && cnt[ord[j]] < cnt[x]) { ord[j + 1] = ord[j]; j-- } ord[j + 1] = x }
            if (mode == "ldap") {
                miss = ""; nm = 0
                for (i = 1; i <= nh; i++) if (!(ord_h[i] in got)) { nm++; miss = miss " " ord_h[i] }
                if (k == 0) { print Y "LDAP : 정보 없음" N; exit }
                if (k == 1) print "LDAP : " ord[1]
                else {
                    line = ""
                    for (i = 1; i <= k; i++) line = line (i > 1 ? " / " : "") ord[i] "(" cnt[ord[i]] "ea)"
                    print Y "[경고] LDAP infra가 2개 이상입니다" N
                    print Y "LDAP : " line N
                    for (i = 2; i <= k; i++) print Y "  " ord[i] " :" hs[ord[i]] N
                }
                if (nm > 0) print Y "  LDAP 미확인 " nm "대 :" miss N
            } else {
                if (k == 0) exit
                if (k == 1) print ord[1] > sumf
                else {
                    line = ""
                    for (i = 1; i <= k; i++) line = line (i > 1 ? " / " : "") ord[i] "(" cnt[ord[i]] "ea)"
                    print line > sumf
                    print Y "[경고] INFO 값이 2개 이상입니다" N
                    for (i = 2; i <= k; i++) print Y "  " ord[i] " :" hs[ord[i]] N
                }
            }
        }' "$2" "$1"
}

# report_status <state 파일> [INFO 요약파일]
#   total : 각 항목의 합과 같으면 초록, 다르면 빨간색 깜빡임. OK 와 total 을 제외하고 0 인 항목은 숨김. 탭 구분.
report_status() {
    local info=""
    [ -n "$2" ] && [ -s "$2" ] && info=$(cat "$2")
    awk -F'\t' -v G="$G" -v R="$R" -v Y="$Y" -v B="$B" -v N="$N" -v info="$info" '{ c[$2]++; t++ } END {
        sum = c["OK"] + c["OFF"] + c["REF"] + c["NOSV"] + c["INST"]
        out = (sum == t ? G : B) "total=" t "ea" N "\t" G "OK=" c["OK"] + 0 "ea" N
        if (c["OFF"])  out = out "\t" R "pingX=" c["OFF"] "ea" N
        if (c["REF"])  out = out "\t" Y "pingO_sshx=" c["REF"] "ea" N
        if (c["NOSV"]) out = out "\t" Y "nosvrauto=" c["NOSV"] "ea" N
        if (c["INST"]) out = out "\t" Y "os_install=" c["INST"] "ea" N
        if (info != "") out = out "\t" Y "INFO=" info N
        print out
    }' "$1"
}

do_check() {   # $1=대상파일 $2=이름
    local t0=$SECONDS
    echo "[체크 실행 중] gossh -pm ($(lines "$1")대)..."
    run_check "$1" "$2"
    echo "(소요 $((SECONDS - t0))초)"
    report_fail "$2"
    rm -f "$TMP/$2.infosum"
    report_multi "$TMP/$2.ldap" "$TMP/$2.state" ldap
    report_multi "$TMP/$2.info" "$TMP/$2.state" info "$TMP/$2.infosum"
    report_status "$TMP/$2.state" "$TMP/$2.infosum"
}

########################## 4. 체크 ##########################
do_check "$HOSTS" first
CUR=first
FINAL="$TMP/first.state"

########################## 5. 환경설정 수정 여부 ##########################
while :; do
    read -r -p "환경설정을 수정하시겠습니까? (y/n/set): " ans || exit 1
    case "$ans" in y|Y|n|N|set|SET) break ;; esac
done
case "$ans" in
    y|Y|set|SET)
        awk -F'\t' '$2 == "OK" { print $1 }' "$FINAL" > "$TMP/ok.txt"
        if [ -s "$TMP/ok.txt" ]; then
            cmd="bash $insert_file; bash $appl_setting_file"
            case "$ans" in set|SET) cmd="$cmd; bash $setting_file" ;; esac
            echo "[설정 적용] $(lines "$TMP/ok.txt")대 : $cmd"
            gsh -script -w "$TMP/ok.txt" "$cmd" > "$TMP/setting.out" 2> "$TMP/setting.err"
            cat "$TMP/setting.out" "$TMP/setting.err" | grep -i -E 'fail|error|fatal|denied|not found' | head -50
            echo
            echo "[재체크]"
            do_check "$TMP/ok.txt" recheck
            awk -F'\t' 'FILENAME == ARGV[1] { r[$1] = $2; next }
                        { print $1 "\t" (($1 in r) ? r[$1] : $2) }' "$TMP/recheck.state" "$TMP/first.state" > "$TMP/final.state"
            FINAL="$TMP/final.state"
            CUR=recheck
            echo "[최종 상태]"
            report_status "$FINAL" "$TMP/recheck.infosum"
        else
            yellow "설정을 적용할 OK 대상이 없습니다."
        fi
        ;;
esac

########################## 6. 마무리 ##########################
echo

# AI GPU 서버
if [ -n "$ai_server_list" ]; then
    awk -v list="$ai_server_list" 'BEGIN { n = split(list, a, "|"); for (i = 1; i <= n; i++) if (a[i] != "") s[a[i]] = 1 }
                                   $1 in s' "$HOSTS" > "$TMP/ai.all"
    if [ -s "$TMP/ai.all" ]; then
        awk -F'\t' 'FILENAME == ARGV[1] { if ($2 == "OK") ok[$1] = 1; next } $1 in ok' "$FINAL" "$TMP/ai.all" > "$TMP/check_list_${user}"
        [ -s "$TMP/check_list_${user}" ] && [ -n "$ai_server_script" ] && \
            gsh -script -w "$TMP/check_list_${user}" "bash $ai_server_script"
        yellow "AI GPU 서버있음, 따로 설정확인필요/GPU power limit"
        echo
    fi
fi

# 벤더별 목록 (V_<벤더>.ok / .off)
awk -F'\t' -v P="$TMP/V_" "$VENDOR_AWK"'
    { v = vendor($1)
      if ($2 == "OK") print $1 > (P v ".ok")
      else if ($2 == "OFF") print $1 > (P v ".off") }' "$FINAL"
horiz() { [ -s "$1" ] && paste -sd' ' "$1"; }

for v in D S L J T W; do
    ok="$TMP/V_$v.ok"; off="$TMP/V_$v.off"
    n_ok=$(lines "$ok"); n_off=$(lines "$off")
    if [ "$v" = D ]; then
        if [ "${n_off:-0}" -gt 0 ]; then
            echo "안녕하세요 D 담당자님"
            echo "하기서버 ${n_off}대는 OS 배포이후 올라오지 않는 것 같습니다."
            echo "점검부탁드립니다."
            [ "${n_ok:-0}" -gt 0 ] && echo "나머지서버들은 OS 설치 완료하였습니다."
            echo "감사합니다."
            horiz "$off"
            if [ "${n_ok:-0}" -gt 0 ]; then
                echo "하기서버 ${n_ok}대는 OS 설치 완료된 서버입니다."
                echo "점검필요하신지 확인 부탁드립니다."
                horiz "$ok"
            fi
            echo
        elif [ "${n_ok:-0}" -gt 0 ]; then
            echo "안녕하세요 D 담당자님"
            echo "하기서버 ${n_ok}대는 OS 설치 완료된 서버입니다."
            echo "점검필요하신지 확인 부탁드립니다."
            horiz "$ok"
            echo
        fi
    elif [ "${n_off:-0}" -gt 0 ]; then
        echo "안녕하세요 $v 담당자님"
        echo "하기서버 ${n_off}대는 OS 배포이후 올라오지 않는 것 같습니다."
        echo "점검부탁드립니다."
        echo "감사합니다."
        horiz "$off"
        echo
    fi
done
if [ -s "$TMP/V_X.off" ]; then
    yellow "[벤더 미분류 접속불가 $(lines "$TMP/V_X.off")대]"
    horiz "$TMP/V_X.off"
    echo
fi

# 전체 접속 가능
awk -F'\t' '$2 == "OK" { print $1 }' "$FINAL" > "$TMP/all.ok"
if [ "$(lines "$TMP/all.ok")" -eq "$TOTAL" ]; then
    echo "안녕하세요. 담당자 님"
    echo "${TOTAL}대 OS 설치 완료하였습니다."
    echo "감사합니다."
    horiz "$TMP/all.ok"
    echo
fi

# DHCP 정보 (접속불가 코멘트 아래)
awk -F'\t' '$2 == "OFF" { print $1 }' "$FINAL" > "$TMP/all.off"
if [ -s "$TMP/all.off" ]; then
    if [ -n "$dhcp_server" ]; then
        echo "[DHCP 정보]"
        ssh -o ConnectTimeout=10 "$dhcp_server" \
            "cat > /root/off_server_list_${user}; bash /root/a.sh; rm -f /root/off_server_list_${user}" < "$TMP/all.off"
        echo
    else
        yellow "dhcp_server 미설정 - DHCP 정보 생략"
    fi
fi

# 기타 상태 서버 요약
awk -F'\t' '$2 != "OK" && $2 != "OFF" { l[$2] = l[$2] " " $1; c[$2]++ } END {
    if (c["REF"] + c["NOSV"] + c["INST"] + c["NONE"] > 0) {
        print "[기타 상태 서버]"
        if (c["REF"])  printf "pingO_sshx(%d대) :%s\n", c["REF"],  l["REF"]
        if (c["NOSV"]) printf "nosvrauto(%d대) :%s\n",  c["NOSV"], l["NOSV"]
        if (c["INST"]) printf "os_install(%d대) :%s\n", c["INST"], l["INST"]
        if (c["NONE"]) printf "no_output(%d대) :%s\n", c["NONE"], l["NONE"]
        print "END"
    }
}' "$FINAL" | while IFS= read -r l; do
    [ "$l" = END ] && { echo; continue; }
    yellow "$l"
done

# usb0 disabled 필요 서버
if [ -s "$TMP/$CUR.usb0" ]; then
    echo "하기서버들은 usb0 disabled가 필요합니다."
    horiz "$TMP/$CUR.usb0"
    echo
fi

# VWP 사용 VM(ev) 서버
grep 'ev' "$HOSTS" > "$TMP/ev.list"
if [ -s "$TMP/ev.list" ]; then
    if [ -r "$vm_inventory" ]; then
        grep -wFf "$TMP/ev.list" "$vm_inventory" | grep vwp > "$TMP/vwp.list"
        if [ -s "$TMP/vwp.list" ]; then
            printf '%s%s%s\n' "$B" "VWP를 사용하는 서버가 존재함" "$N"
            cat "$TMP/vwp.list"
            echo
        fi
    else
        yellow "VM inventory 파일을 읽을 수 없어 VWP 확인 생략: $vm_inventory"
    fi
fi
exit 0
