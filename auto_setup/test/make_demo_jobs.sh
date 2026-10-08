#!/usr/bin/env bash
# make_demo_jobs.sh - 화면(TUI)·날짜별 보기 수동 테스트용 가짜 작업 목록 생성.
#   사용: bash test/make_demo_jobs.sh [데이터디렉터리]   (기본 /tmp/as_demo)
#   보기: AUTO_SETUP_DIR=/tmp/as_demo auto_setup          (데몬 없이 파일만 읽음, 실제 /tmp/auto_setup 은 건드리지 않음)
# 오늘·어제·그제 3일치, yml 이름은 infra_inventory-YYYYMMDDhhmmss_Nea.yml, 단계(완료/설치중/정체/실패/배포중 등) 섞음.
set -euo pipefail

D="${1:-/tmp/as_demo}"
rm -rf "$D"
mkdir -p "$D/jobs/done" "$D/queue" "$D/codes" "$D/runs" "$D/bin" "$D/done" "$D/requests"

now=$(date +%s)
ts() { date -d "@$1" +%Y%m%d%H%M%S; }
dh() { date -d "@$1" +%Y%m%d-%H%M%S; }

# host JSON: 이름 단계 [전달시각 기준 down 경과(초)]
host() { # $1=name $2=stage $3=submitted $4=extra-json(optional)
  local n="$1" st="$2" sub="$3" ex="${4:-}" ip="10.9.${RANDOM:0:1}.$((RANDOM % 250 + 1))"
  case "$st" in
    done)       echo "\"$n\":{\"ip\":\"$ip\",\"route\":\"local\",\"seen_down\":true,\"ready_at\":$((sub+1500)),\"processed\":\"0001\",\"miss\":0,\"fails\":0,\"stage\":\"done\",\"stage_at\":$((sub+2400)),\"done_src\":\"run\"$ex}" ;;
    installing) echo "\"$n\":{\"ip\":\"$ip\",\"route\":\"local\",\"seen_down\":true,\"ready_at\":0,\"processed\":\"\",\"miss\":2,\"fails\":0,\"stage\":\"installing\",\"stage_at\":$((now-600)),\"down_at\":$((now-1200))$ex}" ;;
    os6inst)    echo "\"$n\":{\"ip\":\"$ip\",\"route\":\"os6\",\"seen_down\":true,\"ready_at\":0,\"processed\":\"\",\"miss\":2,\"fails\":0,\"stage\":\"installing\",\"stage_at\":$((now-300)),\"down_at\":$((now-900))$ex}" ;;
    deploying)  echo "\"$n\":{\"ip\":\"$ip\",\"route\":\"local\",\"seen_down\":true,\"ready_at\":0,\"processed\":\"\",\"miss\":2,\"fails\":0,\"stage\":\"deploying\",\"stage_at\":$((now-120)),\"down_at\":$((now-120))$ex}" ;;
    stuck)      echo "\"$n\":{\"ip\":\"$ip\",\"route\":\"os6\",\"seen_down\":true,\"ready_at\":0,\"processed\":\"\",\"miss\":2,\"fails\":0,\"stage\":\"installing\",\"stage_at\":$((now-9000)),\"down_at\":$((now-8000))$ex}" ;;
    failed)     echo "\"$n\":{\"ip\":\"$ip\",\"route\":\"local\",\"seen_down\":true,\"ready_at\":$((sub+1500)),\"processed\":\"\",\"miss\":0,\"fails\":3,\"stage\":\"failed\",\"stage_at\":$((now-3000))$ex}" ;;
    ready)      echo "\"$n\":{\"ip\":\"$ip\",\"route\":\"local\",\"seen_down\":true,\"ready_at\":$((now-200)),\"processed\":\"\",\"miss\":0,\"fails\":0,\"stage\":\"ready\",\"stage_at\":$((now-200))$ex}" ;;
    queued)     echo "\"$n\":{\"ip\":\"$ip\",\"route\":\"local\",\"seen_down\":false,\"ready_at\":0,\"processed\":\"\",\"miss\":0,\"fails\":0,\"stage\":\"queued\",\"stage_at\":$sub$ex}" ;;
  esac
}

# job 작성: $1=user $2=submitted $3=dest(jobs|jobs/done) $4..=그룹 "ymlts|N|infra|os|boot|stage1,stage2,..."
mkjob() {
  local user="$1" sub="$2" dest="$3"; shift 3
  local id hosts="" groups="" g
  id="$(dh "$sub")-$user"
  for g in "$@"; do
    IFS='|' read -r yts cnt infra os boot stages <<<"$g"
    local yml="infra_inventory-${yts}_${cnt}ea.yml" names="" i=0 st
    IFS=',' read -ra sa <<<"$stages"
    for st in "${sa[@]}"; do
      i=$((i+1)); local hn="${user}-${yts:8:4}-$(printf %02d "$i")"
      hosts+="${hosts:+,}$(host "$hn" "$st" "$sub")"
      names+="${names:+,}\"$hn\""
    done
    groups+="${groups:+,}\"$yml\":{\"infra\":\"$infra\",\"os\":\"$os\",\"boot\":\"$boot\",\"splunk\":\"n\",\"hosts\":[$names]}"
  done
  cat > "$D/$dest/$id.json" <<EOF
{"id":"$id","user":"$user","submitted":$sub,"first_ready":0,"late_first_ready":0,"first_run_done":false,
 "hosts":{$hosts},"runs":[],"groups":{$groups},"all_yml":"infra_inventory-$(ts "$sub")_all.yml"}
EOF
  echo "  $dest/$id.json"
}

echo "생성 위치: $D"
t0=$((now - 600))                 # 오늘
t1=$((now - 86400 - 3600))        # 어제
t2=$((now - 2*86400 - 7200))      # 그제
t3=$((now - 4*86400))             # 4일 전 (종료된 job)

# 오늘: 진행 중 2개 (그룹 3개)
mkjob kim  "$t0" jobs "$(ts $t0)|4|ib|rhel8|pxe|installing,installing,os6inst,deploying" \
                      "$(ts $((t0+30)))|3|eth|rhel7|pxe|ready,queued,deploying"
mkjob lee  "$((t0+90))" jobs "$(ts $((t0+90)))|2|ib|rhel9|iso|deploying,queued"
# 어제: 일부 완료·정체·실패
mkjob kim  "$t1" jobs "$(ts $t1)|5|ib|rhel8|pxe|done,done,stuck,failed,installing"
# 그제: 전부 완료(진행 중 job 에 남은 상태) + 정체
mkjob park "$t2" jobs "$(ts $t2)|3|eth|rhel8|pxe|done,done,done" \
                      "$(ts $((t2+60)))|2|eth|rhel7|pxe|done,stuck"
# 4일 전: 종료된 job
mkjob choi "$t3" jobs/done "$(ts $t3)|3|ib|rhel8|pxe|done,done,done"

echo
echo "보기:  AUTO_SETUP_DIR=$D auto_setup           (TUI: 맨 위에서 ↑ → 날짜 줄, ← → 날짜 이동)"
echo "       AUTO_SETUP_DIR=$D auto_setup --plain   (날짜 필터 없이 전체)"
echo "삭제:  rm -rf $D"
