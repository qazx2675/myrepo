# pm_monitor (install_mn) — PM 전용 OS 설치 진행 모니터링 TUI

호스트 목록(한 줄에 hostname 하나)을 주면 1분마다 ping · 22번 포트 · `gossh -pm` 으로 서버마다 상태 하나(`설치중` / `지연` / `디스크미인식` / `설치완료` / `ping X` / `재부팅됨` / `완료후꺼짐`)를 판정해 TUI 로 보여 주는 Go 도구입니다. 설치현황·점검요청 코멘트 만들기, DHCP 조회, 완료 서버의 LDAP·DNS·NTP·autofs·H/T 점검까지 키 하나로 합니다. 수집은 백그라운드로 돌고 화면은 붙었다 떼었다 할 수 있으며, 시작 12시간 뒤 스스로 끝납니다. Go 바이너리(`install_mn_os8`) + bash 래퍼(`install_mn.sh`) 한 쌍이며 표준 라이브러리만 사용하므로 `vendor/` 가 필요 없습니다.

> 흐름도: [workflow.svg](workflow.svg) · 구조: [ARCHITECTURE.md](ARCHITECTURE.md) · 흐름 설명: [WORKFLOW.md](WORKFLOW.md) · 변경 시 점검: [PR_CHECKLIST.md](PR_CHECKLIST.md)

⚠️ **주의사항 (Disclaimer)**
본 도구는 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 상태 판정은 ping·포트·로그 문구 일치에 기댄 추정이므로 중요한 서버는 몇 대를 골라 직접 접속해 실제 상태를 확인하세요.

⚠️ **관리서버 공용 사용**
여러 사람이 함께 쓰는 관리서버에서 돌기 때문에 부하를 낮게 유지하도록 만들었습니다: ICMP 초당 `ping_pps`(기본 500)개 제한, 22번 포트 동시 연결 `tcp_parallel`(기본 100)개 제한, gossh 는 한 번에 하나만 `nice -n 10` 으로 실행, 수집 회차 사이에는 쉬고, 완료 서버는 평소에 ping·ssh 하지 않으며, 상태 파일은 바뀐 것이 있을 때만 씁니다. 백그라운드 수집은 **시작 12시간 뒤 자동으로 종료**되므로 잊어도 서버에 남지 않습니다.

## 1. 빌드 및 설치 방법

### 1.1 폐쇄망(회사) 환경 — 빌드 불필요

회사에는 Go 가 없으므로 **빌드된 `install_mn_os8`(linux/amd64 정적 바이너리)을 저장소에 함께 커밋**해 두었습니다. 폴더를 통째로 옮기면 바로 실행됩니다. (os6 용은 만들지 않습니다.)

```bash
# 인터넷이 되는 환경에서 저장소 다운로드 후 압축
tar czf pm_monitor.tar.gz ".claude/HPC/pm_monitor"
# 폐쇄망 서버에서
tar xzf pm_monitor.tar.gz
cd ".claude/HPC/pm_monitor"
chmod +x install_mn.sh install_mn_os8   # 실행 권한이 빠졌을 때만
```

필요 조건: root 권한(raw ICMP), `gossh` 설치(PATH 또는 설정의 `gossh=`).

### 1.2 소스를 고쳐서 다시 빌드할 때 (Go 가 있는 호스트)

```bash
bash build.sh       # go vet + go test 후 install_mn_os8 빌드
bash build.sh -n    # vet·test 없이 빌드만
```

- `go.mod` 는 `go 1.20` 기준입니다. 더 새로운 문법·표준 라이브러리 API 를 쓰지 마세요.
- **소스를 바꿨다면 `install_mn_os8` 을 재빌드해 함께 커밋**해야 합니다. `.gitignore` 로 바이너리를 제외하지 마세요.

## 2. 사용 방법

```bash
# 설정 파일 준비 (처음 한 번). 실행한 디렉터리의 pm_monitor.conf 를 읽는다
cp pm_monitor.conf.sample pm_monitor.conf     # 필요한 줄만 # 을 지우고 수정

# 실행 (root). 호스트파일 = 한 줄에 hostname 하나, 빈 줄·# 줄은 무시
bash install_mn.sh <호스트파일>
bash install_mn.sh -c /경로/pm_monitor.conf <호스트파일>

# 실행 중인 수집 보기 / 끝내기 (호스트파일을 빼면 --status 는 서버 전체의 수집을 보여 준다)
bash install_mn.sh --status [<호스트파일>]
bash install_mn.sh --stop <호스트파일>
```

- 인자가 없거나 호스트파일이 없으면 사용법을 출력하고 종료 코드 2 로 끝납니다.
- **수집은 백그라운드에서 돕니다.** `q` 로 화면만 나가도 수집은 계속되고, 같은 호스트파일로 다시 실행하면 이어서 붙습니다. 자세한 내용은 3.5 를 보세요.
- 상태와 이력은 실행 디렉터리의 `<호스트파일>.pm_state.json` 에 저장되어 다시 실행해도 이어집니다.
- 설정 파일이 없으면 기본값으로 진행하고 화면에 경고 한 줄을 띄웁니다.
- 수동 시험 명령 모음은 [사용법.txt](사용법.txt) 를 참고하세요.

### 2.1 데모로 먼저 보기 (실제 서버 접속 없음)

```bash
bash demo/demo.sh                 # root 로 실행. 안내 화면에서 Enter (27대)
bash demo/demo.sh -n 8000         # 8000대로 렉·가독성 확인 (갱신 30초 간격)
bash demo/demo.sh -name demo2     # 다른 이름으로 데모를 하나 더 (작업 폴더 /tmp/demo2)
```

가짜 gossh·ssh 와 가짜 호스트 27대(벤더별 이름)로 데모 환경을 `/tmp/pm_demo` 에 만들고 바로 `install_mn` 을 띄웁니다. 시간이 지나면 설치완료·지연·완료후꺼짐·재부팅됨이 차례로 나타나므로 키(`c r d l / 1~7`)를 눌러 출력을 확인할 수 있습니다. `q` 로 화면만 나가면 수집은 계속되고(데모는 최대 1시간), 그 뒤 Enter 로 다시 붙거나 `e` 로 끝낼 수 있습니다. 끝내면 잠깐 넣었던 `/etc/hosts` 줄과 lo 주소, `/tmp/pm_demo` 를 모두 되돌립니다.

## 3. 옵션별 상세 설명

### 3.1 설정 키 (`pm_monitor.conf`, `key=value`, `#` 주석)

| 키 | 기본값 | 설명 |
|---|---|---|
| `gossh` | PATH 에서 찾음 | gossh 경로 |
| `interval_sec` | 60 | 갱신 주기(초) |
| `delay_min` | 60 | 지연 기준(분). 설치중으로 처음 본 뒤 이 시간이 지나도 완료가 아니면 지연 |
| `disk_pattern` | `Disk "" given in clearpart command does not exist` | `/tmp/anaconda.log` 에서 찾을 문구. 여러 줄 가능 |
| `vendor` / `vendor_other` | `^(c\|h\|sh\|s2h\|s3h\|s4h)=D`, `^s=sds`, `^p=L`, `^l=j`, `^d=t`, `^m=W` / `기타` | hostname 정규식=표시 이름. 위에서부터 처음 맞는 줄. `vendor` 를 쓰면 기본 규칙 전체를 대체 |
| `dhcp_server` | 비어 있음 | `d` 키에서 쓰는 DHCP 서버. 비면 "dhcp_server 미설정" 표시 |
| `ldap_check_sh` | 비어 있음 | `l` 키에서 쓰는 ldap_check.sh 경로. 비면 "미설정" 표시 |
| `ping_timeout_ms` | 1500 | raw ICMP 응답 대기 시간 |
| `ping_pps` | 500 | 초당 ICMP 송신 상한(관리서버 부하 제한) |
| `tcp_parallel` | 100 | 22번 포트 동시 연결 상한 |

### 3.2 TUI 키

| 키 | 동작 |
|---|---|
| (기본 화면) | 위: 상태별 개수·마지막 갱신 시각·회차 진행 / 아래: hostname·벤더·상태·경과 시간 표 |
| ↑↓ PgUp PgDn Home End | 표 스크롤 |
| `/` | hostname 검색 |
| `1`~`7` / `0` | 상태 필터(1 설치중 2 지연 3 디스크미인식 4 설치완료 5 ping X 6 재부팅됨 7 완료후꺼짐) / 해제 |
| `c` | 설치현황 코멘트 켜기/끄기. 켤 때 완료 서버에 ping 을 보내 완료후꺼짐·재부팅됨을 판정 |
| `r` | 지연 서버의 벤더별 점검요청 코멘트 |
| `d` | DHCP 조회(ping X 서버를 `dhcp_server` 로 보냄) 결과 |
| `l` | 완료 서버의 LDAP·DNS·NTP·autofs·H/T 점검 결과(아직 안 채워진 서버만 조회) |
| `q` | 패널(c r d l)에서는 뒤로(기본 화면), 기본 화면에서는 **화면만 나가기**(수집은 계속, 터미널 복구) |
| `Q` | 수집까지 완전히 종료. 확인 문구에서 `y` 만 종료, 다른 키는 취소 |
| Ctrl+C | 어디서든 화면만 나가기(수집은 계속, 터미널 복구). ssh 연결이 끊겨도 같다 |

`c r d l` 은 다시 누르면 닫힙니다.

### 3.3 상태

| 상태 | 판정 |
|---|---|
| 설치중 | gossh `-pm` 이 OS 설치중으로 판정, 또는 ping O 인데 22번 포트가 refused |
| 지연 | 설치중으로 처음 본 뒤 `delay_min` 이 지나도 완료가 아님(표시만) |
| 디스크미인식 | 설치중 서버의 `/tmp/anaconda.log` 에 `disk_pattern` 문구가 있음 |
| 설치완료 | ssh 접속이 되고 anaconda 가 없음 |
| ping X | ping 무응답 |
| 재부팅됨 | 완료후꺼짐이던 서버가 다시 ping O (완료 수에 포함) |
| 완료후꺼짐 | 설치완료였던 서버가 `c` 를 누른 시점에 ping X |

판정 우선순위는 디스크미인식 > 지연 > 설치중입니다. 22번 포트가 refused 인 서버는 로그를 읽을 수 없어 설치중으로 남습니다.

### 3.4 LDAP·H/T (`l`) 출력 형식

`l` 패널 칸 (v0.2.0):

| 칸 | 내용 |
|---|---|
| LDAP | ldap_check.sh 출력 `INFO	LDAP	<infra>	<site>` 의 **infra** 만. FAIL 이면 `FAIL`(비고 `ldap_check UNDEFINED`) |
| DNS · NTP | `/etc/resolv.conf` nameserver 와 `/etc/chrony.conf`(없으면 `/etc/ntp.conf`) server·pool 을 순서 무시로 비교. 같으면 `ip1/ip2`(숫자 오름차순), 다르면 `다름`(비고에 양쪽 값) |
| autofs | `/etc/auto.appl` 의 `/appl`·`/wappl` 마운트 위치(예 `nas01:/export/appl`). 두 줄이 같으면 `/appl` 값, 다르면 `다름`(비고에 양쪽 값) |
| H/T | Thread(s) per core 2 이상 ON, 1 OFF |
| 비고 | 위 원인들을 `; ` 로 연결 |

값이 없으면 `-` 입니다. chrony·ntp 에 IP 대신 호스트 이름이 적혀 있으면 그 서버에서 `getent hosts` 로 IP 로 바꿔 비교합니다(이름 하나당 5초 제한). 못 바꾼 이름은 비고에 `NTP 이름 풀이 실패 <이름>` 으로 남습니다.

### 3.5 백그라운드 수집 (v0.3.0)

| 상황 | 동작 |
|---|---|
| `install_mn <호스트파일>` (수집 없음) | 수집 데몬을 시작한 뒤 화면을 붙임 |
| `install_mn <호스트파일>` (수집 중) | 같은 수집에 이어서 붙음. 첫 화면부터 지금까지의 상태가 보임 |
| `q`(기본 화면) · Ctrl+C · 연결 끊김 | 화면만 닫음. 수집은 계속. 나갈 때 자동 종료 시각과 다시 붙는 방법을 안내 |
| `Q` → `y` · `install_mn --stop <호스트파일>` | 수집 종료(상태 기록은 남음) |
| 시작 12시간 경과 | 수집이 스스로 종료하고 붙은 화면도 닫힘. 다시 실행하면 새 12시간으로 시작 |
| 호스트파일·설정 파일 내용이 바뀐 뒤 다시 실행 | 수집을 다시 시작(상태 기록 유지, 12시간 새로 셈). 화면 위쪽에 안내 |

- 12시간은 **수집을 시작한 시각**부터 셉니다. 다시 붙어도 늘어나지 않습니다. 화면 머리줄의 `자동 종료 HH:MM` 이나 `install_mn --status` 의 남은 시간으로 확인합니다.
- 수집은 호스트파일(실행 폴더 + 파일 이름)마다 하나입니다. 다른 호스트파일은 따로 동시에 돌릴 수 있고, 그만큼 부하도 더해집니다.
- 소켓·잠금·로그는 `/run/install_mn/`(root 전용)에 있고 서버를 재부팅하면 사라집니다. 시작이 안 될 때는 거기의 `<id>.log` 를 보세요.
- `-f` 를 주면 예전처럼 백그라운드 없이 화면과 함께 수집하고, 화면을 닫으면 끝납니다(디버깅용).

## 4. 문서별 설명

| 파일 | 설명 |
|---|---|
| `README.md` | 이 문서 |
| `ARCHITECTURE.md` | 파일별 역할 표 |
| `WORKFLOW.md` | 수집 회차와 주요 키 흐름 |
| `workflow.svg` | 전체 흐름도 |
| `PR_CHECKLIST.md` | 변경 시 점검 목록 |
| `CHANGELOG.md` | 버전별 변경 이력 |
| `사용법.txt` | 직접 칠 명령어 모음(수동 시험용) |
| `계획서.md` | 확정된 규칙·단계·테스트 통과 기준 |
| `pm_monitor.conf.sample` | 설정 파일 샘플 |
| `build.sh` / `install_mn.sh` | 빌드 스크립트 / 실행 래퍼 |
| `install_mn_os8` | 빌드된 os8 바이너리(커밋 대상) |
| `test/` | 스텁 gossh·ssh, 부하 측정 스크립트, 백그라운드 수집 시험(`daemon_check.py`) |
| `demo/demo.sh` | 데모 환경 한 번에 구성·실행 (2.1 참고) |
| `testdata/` | 렌더 골든 파일 |

## 5. 전역 명령어로 사용하기 (선택 사항)

`install_mn.sh` 는 자기 위치(심볼릭 링크를 따라감)를 기준으로 바이너리를 찾으므로 복사하지 말고 링크로 연결하세요.

```bash
ln -s "$(pwd)/install_mn.sh" /usr/local/bin/install_mn
# 이후 어디서든: install_mn <호스트파일>   (설정은 실행한 디렉터리의 pm_monitor.conf)
```
