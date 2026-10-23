#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드. vendor/ 안의 의존성만 사용, 인터넷 접속 시도 안 함.
#   bash setup.sh          # 빌드 → ./vm_power_onoff
#   bash setup.sh --os6    # OS6(RHEL/CentOS 6)용: bin_os6/vm_power_onoff.gz 를 풀어 ./vm_power_onoff 로 둔다 (OS6 서버에서는 자동)
# 참고: vcsim 시험(go test -tags vcsim)은 vendor 에 없는 모듈이 필요 없도록 vendor 에 simulator 까지 포함되어 있다.
set -euo pipefail
cd "$(dirname "$0")"

# OS6(커널 2.6.32)는 이 Go 로 만든 실행파일이 뜨지 않아 Go 1.20 으로 미리 빌드한 실행파일을 푼다
OS6=0
[ "${1:-}" = "--os6" ] && OS6=1
case "$(uname -r)" in 2.6.*) OS6=1 ;; esac
grep -qs 'release 6\.' /etc/redhat-release && OS6=1

if [ "$OS6" -eq 1 ]; then
  echo "[INFO] OS6 모드 — bin_os6/vm_power_onoff.gz 를 풀어 설치합니다"
  if [ -f bin_os6/vm_power_onoff.gz ] && gzip -dc bin_os6/vm_power_onoff.gz > vm_power_onoff.tmp; then
    mv -f vm_power_onoff.tmp vm_power_onoff && chmod 755 vm_power_onoff
    echo "설치 완료: $(pwd)/vm_power_onoff"
    echo
    echo '완료. 다음으로 진행하십시오:'
    echo '  1) sudo bash install.sh        # /usr/local/bin 과 /etc/vcetner_pwon_off/pwonoff.conf 설치'
    exit 0
  fi
  rm -f vm_power_onoff.tmp
  echo "오류: bin_os6/vm_power_onoff.gz 가 없거나 풀 수 없습니다 (Go 1.20 빌드 서버에서 bash build_os6.sh)" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인(1.26.5 이상)을 먼저 설치하십시오." >&2
  exit 2
fi

export GOPROXY=off
export GOFLAGS=-mod=vendor

go build -mod=vendor -o vm_power_onoff .

echo
echo "빌드 완료: $(pwd)/vm_power_onoff"
echo '완료. 다음으로 진행하십시오:'
echo '  1) sudo bash install.sh        # /usr/local/bin 과 /etc/vcetner_pwon_off/pwonoff.conf 설치'
echo '  2) /etc/vcetner_pwon_off/pwonoff.conf 의 users / vcenters 를 환경에 맞게 수정'
echo '  3) allowed_dir 에 on_off_<user>.txt 를 만들고 그 디렉터리에서 vm_power_onoff 실행'
