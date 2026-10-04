# auto_setup — OS 설치 → OS 설정체크 자동 연계

OS 설치(01) 완료 후 자동으로 OS 설정을 검사하고 결과를 공유하는 데몬 + CLI 도구.

- **1차(v0.1.0)**: 01 전달 → ping·준비확인 감시 → os_check 자동 실행 → code/wall 공유
- **2차(v0.2.0)**: 양방향 동일 실행(os8_mgmt 단일 원본, os6_mgmt 는 원격 클라이언트), 이중체크, 완료기록, LDAP 백업/복원, 상태 TUI

## 빌드 및 설치 방법

### 의존성

| 항목 | 요구사항 | 설명 |
|---|---|---|
| 운영체제 | RHEL 8+ (Go 1.20+) | bash, go, gossh, ssh |
| Go | 1.20 이상 | 표준 라이브러리만 (외부 의존성 0) |
| 폐쇄망 | vendor 불필요 | 오프라인 빌드 가능 |
| 권한 | root | raw ICMP, cron 등록, /usr/local/bin |

### 파일 배치

```
auto_setup/
├── main.go / daemon.go / state.go / ... (Go 소스, 파일별 역할은 ARCHITECTURE.md)
├── setup.sh              ← 빌드 + 설치 + cron + tmpfiles 등록
├── build_os6.sh          ← os6 용 정적 빌드 (Go 1.20 서버에서)
├── conf/auto_setup.conf.example ← 빈 변수 설정 파일 예시 (복사해 auto_setup.conf 로 사용)
├── test/
│   ├── manual_check.sh   ← 대화형 단계별 점검 (1~17단계)
│   ├── run_e2e.sh        ← 1차 목업 E2E (85건)
│   ├── run_e2e2.sh       ← 2차 목업 E2E (a~f, 191건)
│   └── lib_e2e.sh        ← E2E 공용 함수·스텁 (gossh, ssh, wall)
├── testdata/             ← TUI 골든 파일, os_check 결과 샘플
├── 사용법.txt
└── 문서 (README.md / CHANGELOG.md / ARCHITECTURE.md / WORKFLOW.md / workflow.svg / PR_CHECKLIST.md)
```

### 최상단 빈 변수

`main.go` 최상단의 변수들은 **항상 빈 문자열로 둡니다**(저장소 커밋 시, 값은 테스트·운영 빌드 때만 `-ldflags -X` 로 주입). `go build` 전에 소스를 고치지 마십시오.

#### 설정 파일로 채우기 (빌드 불필요)

빌드가 불가능한 환경(Go 없음)에서는 아래 변수들을 **`conf/auto_setup.conf`** 로 채웁니다. `conf/auto_setup.conf.example` 을 복사해 `키=값` 으로 작성합니다(키 이름 = 아래 변수 이름, `#` 주석, 값의 따옴표는 선택).

```bash
cp conf/auto_setup.conf.example conf/auto_setup.conf
vi conf/auto_setup.conf        # 예: os8_mgmt=<os8_mgmt 호스트>
```

- 설정 파일 위치(먼저 발견된 **하나만** 사용): ① `$AUTO_SETUP_CONF/auto_setup.conf` ② `<auto_setup 실행 파일 디렉터리>/conf/auto_setup.conf` ③ `/etc/auto_setup/auto_setup.conf` (`setup.sh` 가 `conf/auto_setup.conf` 를 여기에 설치, 0600)
- **빌드 때 `-ldflags -X` 로 넣은 값이 있으면 그 값이 우선**하고, 설정 파일은 비어 있는 변수만 채웁니다. 알 수 없는 키·형식이 틀린 줄은 무시합니다.
- 예) 저장소의 빌드 완료 `auto_setup_os6` 를 os6_mgmt 에 두고 같은 디렉터리의 `conf/auto_setup.conf` 에 `os8_mgmt=<호스트>` 만 적으면 원격 클라이언트로 동작합니다.
- 실제 `conf/auto_setup.conf` 는 `.gitignore` 로 커밋에서 제외됩니다(예시 파일만 커밋). 01·os_check 쪽 빈 변수(`auto_done_dir` 등)는 쉘 스크립트이므로 기존 방식대로 채웁니다.

#### main.go 9개

| 변수 | 의미 | 비우면 |
|---|---|---|
| `os6_mgmt` | 모든 대역에 접근 가능한 서버(RHEL6) | os6 경로·2차 체크 미사용 |
| `os6_gossh` | os6_mgmt 위 gossh 실행 파일 전체 경로 | — |
| `os6_autosetup` | os6_mgmt 위 auto_setup(os6 빌드)이 있는 디렉터리 | — |
| `os_check_sh` | 이 서버의 `os_check_final_annotated.sh` 전체 경로 (autofs 공유 경로면 os8_mgmt·os6_mgmt 동일) | — |
| `os8_mgmt` | os8_mgmt 호스트. **비어 있지 않으면 원격 클라이언트 모드** (os6 빌드 때 주입) | 로컬 모드(데몬 서버) |
| `os8_autosetup` | os8_mgmt 위 auto_setup 경로 | `/usr/local/bin/auto_setup` |
| `os6_os_check_sh` | os6_mgmt 위 os_check 경로(2차 체크용). `"-"` 면 2차 체크 끄기 | `os6_mgmt` 가 있으면 `os_check_sh` 로 폴백(autofs 동일 경로), `os6_mgmt` 도 비면 2차 체크 생략 |
| `awx_dir` | `awxkit/dhcp.sh` 가 있는 awx_script 디렉터리 (autofs 동일 경로) | `os_check_sh` 와 같은 디렉터리의 `dhcp.sh` 만 후보 |
| `ldap_share_dir` | LDAP 복원용 autofs 공유 경로. os8_mgmt·대상 서버가 모두 보는 곳이며 **대상 root 가 읽을 수 있어야 함(no_root_squash)** | 자동 복원 안 함(binddn 이 다르면 `수동 복원 필요` 로 표시) |

#### os_check 2개, 01 1개

| 파일 | 변수 | 의미 |
|---|---|---|
| `OS 환경설정 체크/os_check_final_annotated.sh` | `auto_done_dir` | 완료기록을 쓸 **로컬** 디렉터리(os8_mgmt 에서만 채움) |
| 같은 파일 | `auto_done_host` | os8_mgmt 호스트명(다른 서버에서 채움). gossh 원샷으로 그쪽 `done/` 에 기록 |
| `awx_script/01.AWX_nodeinfo_V2.sh` | `auto_setup_host` | os8_mgmt 호스트명(os6_mgmt 등에서 채움). 비면 로컬 queue |

> **autofs 로 os_check 를 공유하는 경우**: 한 파일을 os8_mgmt 와 os6_mgmt 가 같이 쓰므로 `auto_done_dir`(os8 로컬용)과 `auto_done_host`(원격용)를 서버별로 다르게 채울 수 없습니다. **`auto_done_host` 만 채우십시오**(os8_mgmt 에서 실행될 때는 자기 자신에게 gossh 합니다).
> **`auto_done_dir` 을 채운 채 os6_mgmt 에서 실행하면 os6 로컬에 기록되어 os8_mgmt 의 데몬이 보지 못합니다.**

```go
var (
	os6_mgmt        = ""
	os6_gossh       = ""
	os6_autosetup   = ""
	os_check_sh     = ""
	awx_dir         = ""
	os8_mgmt        = ""
	os8_autosetup   = ""
	os6_os_check_sh = ""
	ldap_share_dir  = ""
)
```

**주입 방법** (소스 수정 없이):
```bash
# os8_mgmt 데몬 빌드 (2차 체크 포함)
go build -ldflags "\
  -X main.os_check_sh=/절대경로/os_check_final_annotated.sh \
  -X main.os6_mgmt=os6_mgmt_호스트 \
  -X main.os6_gossh=/remote/gossh \
  -X main.os6_autosetup=/remote/auto_setup_dir" \
  -o auto_setup .

# os6_mgmt 용 원격 클라이언트 빌드 (Go 1.20 서버에서, 정적)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -ldflags "-X main.os8_mgmt=<os8_mgmt 호스트> -X main.os8_autosetup=<os8_mgmt 위 auto_setup 경로>" \
  -o auto_setup_client .
```

### 빌드 및 설치 (폐쇄망)

폐쇄망 환경에서는 외부 의존성이 없으므로 소스만 복사하면 됩니다.

```bash
# 1. 소스 복사 및 이동
cp -r auto_setup /root/로컬경로/
cd /root/로컬경로/auto_setup

# 2. 빌드만 (테스트용)
go build -o auto_setup .

# 3. 빌드 + /usr/local/bin 설치 + cron(매분 ensure) + tmpfiles 등록 (root, 멱등)
bash setup.sh

# 4. os6 용 빌드 (Go 1.20 서버 .60 에서, 정적 바이너리)
bash build_os6.sh
# → auto_setup_os6 생성 (os6_mgmt 의 os6_autosetup 경로에 'auto_setup' 이름으로 배치, probe 용)
# (Go 가 없는 회사 환경은 빌드 불필요: 저장소에 빌드 완료된 auto_setup_os6 (go1.20 정적, 변수 빈 값) 가 있으니 probe 용으로 그대로 배치)
```

### setup.sh 수행 내용

1. `go build -o auto_setup .` (main.go 포함 전체 빌드)
2. `install -m 0755 auto_setup /usr/local/bin/auto_setup`
3. crontab 에 `* * * * * /usr/local/bin/auto_setup ensure >/dev/null 2>&1` 등록 (중복 방지)
4. `/etc/tmpfiles.d/auto_setup.conf` 에 `x /tmp/auto_setup` 등록 (무기한 감시 작업이 tmp 정리로 지워지지 않도록)

## 사용 방법

### 구성: 양방향 동일 실행

```
os8_mgmt (RHEL8) : auto_setup 데몬 + 상태(/tmp/auto_setup) + 01/os_check 실행 가능   ← 단일 원본
os6_mgmt (RHEL6) : 데몬 없음. 01/os_check 실행 가능, auto_setup 은 "원격 클라이언트 모드"
```

- 데몬·상태는 **os8_mgmt 에만** 둡니다(os6_mgmt 는 여러 사람이 쓰는 서버라 상주 프로세스 금지).
- os6_mgmt 에서 `auto_setup`(os8_mgmt 변수를 채워 빌드한 클라이언트)을 실행하면 `status`·`snapshot`·`code`·`cancel`·`done`·`request`·리포트/TUI 가 모두 **os8_mgmt 에 요청**되고, 출력은 어디서 실행해도 같습니다.
- 전달·조회·요청은 ssh 세션이 아니라 **gossh 원샷**(`gossh -pm -script -w <목록> "<os8_autosetup> <하위명령>"`)입니다. 이유: RHEL6 의 ssh 클라이언트는 RHEL8 sshd 와 호환 문제가 있고, 세션을 유지하면 서버 부담이 되기 때문입니다.
- `--start/--stop/--restart`, `daemon`, `ensure` 는 **os8_mgmt 에서만** 동작합니다(원격 클라이언트에서는 `[X] 데몬은 os8_mgmt 에서만 실행합니다` 로 거부).
- os8 → os6 방향(ping 감시 probe 장기 ssh 세션, os6 gossh 래퍼, 준비확인)은 1차 그대로입니다.

### 01 과의 연계 흐름

```
01.AWX_nodeinfo_V2.sh 실행 (os8_mgmt 또는 os6_mgmt)
    ↓ (02 성공)
queue/<epoch>_<user>_<pid>.job 생성 (프롬프트 없음)
  - os8_mgmt: 로컬 queue 에 직접 기록
  - os6_mgmt(auto_setup_host 채움): gossh 로 os8_mgmt 의 queue 에 원자 전송
    ↓
데몬(cron 매분 ensure)이 queue 수거 → jobs/<jobid>.json
    ↓
전달 시점 LDAP 백업 (ping O 인 호스트, 옛 OS 가 살아 있을 때)
    ↓
자동 감시: ping 상태 판별 → 경로(local/os6) 결정 → 무응답 감지(설치 시작) → 준비확인(7분/3분)
    ↓
READY → LDAP 비교(binddn 의 uid)·복원 (같으면 생략, 다르면 공유경로 경유로 백업 세트 복원)
    ↓
os_check -auto <user> <목록> 자동 실행 (동시 1개)
    ↓
2차(이중) 체크: route=local 의 접속불가·FAIL 호스트만 os6_mgmt 에서 한 번 더 (설정 시)
    ↓
wall + codes/<code>.txt 생성 (호스트 현황, 4자리 code) / 완료기록·수동 완료는 run 대상에서 제외
```

### 명령어

| 명령 | 동작 |
|---|---|
| (인자 없음) | 현재 상태 리포트. tty 면 TUI, 아니면 텍스트 표 |
| `--plain` | 텍스트 표로 출력 (색 없음) |
| `-h`, `--help` | 사용법 출력 |
| `--start` \| `start` | 데몬 기동 (이미 실행 중이면 안내) — os8_mgmt 에서만 |
| `--stop` \| `stop` | 데몬 종료 (pid 확인 후 SIGTERM, 대기) — os8_mgmt 에서만 |
| `--restart` \| `restart` | 데몬 재기동 — os8_mgmt 에서만 |
| `status` | 작업별 진행 상태 (jobid, user, 호스트 상태별 대수, 남은 호스트 가로) |
| `snapshot` | 상태 스냅샷(JSON) 출력 (TUI·원격 클라이언트가 쓰는 내용) |
| `code NNNN` | code 에 해당하는 결과 코멘트 출력 (wall 에 나온 4자리) |
| `cancel <jobid>` | 무기한 감시 중인 작업 종료 (status 에서 확인) |
| `done <host...>` | 호스트를 수동 완료 처리 (출처 manual, 부팅 시각 검증 없음) |
| `request manual-run <jobid> <yml>` | 수동 OS 체크 실행 요청 (그룹 전체가 완료일 때만 수락) |
| `request cancel <jobid>` | 작업 종료 요청 |
| `daemon` | 포그라운드 데몬 루프 (보통 ensure 가 백그라운드로 기동) — os8_mgmt 에서만 |
| `ensure` | 데몬이 없으면 백그라운드 기동 (cron 매분) — os8_mgmt 에서만 |
| `probe [-i 10s]` | (os6_mgmt 쪽) stdin 호스트 목록을 받고 ping 상태 출력 (`host up\|down`) |
| 그 외 | 사용법을 stderr 로 출력 후 exit 1 |

기존 하위명령(`daemon/ensure/code/status/cancel/probe`)은 그대로이며 cron·setup.sh 와 호환됩니다.

### 상태 TUI (무인자 실행)

```
auto_setup            # tty 면 TUI
auto_setup --plain    # 텍스트 표
NO_COLOR=1 auto_setup # 색 없는 TUI
```

**키**

| 화면 | 키 | 동작 |
|---|---|---|
| 1 (작업·그룹) | ↑ ↓ (k j) | 그룹 행 이동 |
| 1 | ← → | 이전/다음 작업(job) 으로 이동 |
| 1 | Enter | 선택한 그룹의 호스트표(화면 2) |
| 1 | q / Esc | 종료 |
| 2 (호스트표) | ↑ ↓ (k j) | 호스트 행 이동 |
| 2 | ← / Esc / q | 화면 1 로 복귀 |
| 2 | f | 정체·실패 호스트만 보기 (토글) |
| 2 | c | OS 체크 수동 실행(이중체크). **그룹 전체가 완료일 때만**, y/n 확인 후 요청 |
| 공통 | r | 바로 새로고침 (자동: 로컬 2초, 원격 5초) |
| 공통 | ? | 도움말 (아무 키나 누르면 닫힘) |

- 집합(행 단위) = 01 이 전달한 **그룹 yml** 입니다. 그룹 줄이 없는(구버전 01 또는 그룹 1개) 작업은 `(all)` 한 행이며, 어느 그룹에도 없는 호스트는 `(기타)` 로 모읍니다. 전체 yml(`all=`)은 작업 제목에 표시됩니다. 화면 상단에 진행 중 작업 합계(총계)가 나옵니다.
- **색**: 완료 초록 / 진행 청록 / 대기 회색 / 경고 노랑 / 정체·실패 빨강(굵게). `NO_COLOR` 설정, tty 가 아님(파이프), `--plain` 이면 색을 쓰지 않습니다.
- **정체 기준**: seen_down(설치 시작) 후 **60분까지는 "설치중"**(RHEL7 은 1시간 걸림), 그 이후에도 READY 가 안 되면 빨간 "정체"(`installStuck` = 60분, 경계값은 아직 설치중). 화면 표시만 하며 wall 알림은 없습니다.
- os6_mgmt 에서도 동일하게 열리며 gossh 로 snapshot 을 5초 간격으로 폴링합니다. 키 입력(수동 실행)은 요청 파일을 os8_mgmt 에 남깁니다(데몬이 5초 주기로 수거).
- 수동 실행은 데몬 run 큐에서 실행됩니다(동시 1개 유지, code·wall 동일). 거부되면 `requests/rejected/<파일>` 의 마지막 `reason=` 줄에 사유가 남습니다.

### 완료기록 (타 경로에서 먼저 체크 끝난 호스트)

os_check 는 정상 종료 시 이번 실행의 `check.res_<user>_postapply` 에 에러 없이 나온 호스트마다 기록을 남깁니다(빈 변수 `auto_done_dir`/`auto_done_host` 가 채워진 경우만).

- 위치: `<AUTO_SETUP_DIR>/done/<host>` — 한 줄 `epoch user sha256 source` (임시파일 → mv 원자 기록, sha256 = 그 호스트의 postapply 줄 해시)
- **인정 규칙**: 전달(`job.submitted`) 이후이고 **마지막 부팅 이후**의 기록만 인정(재설치 이전 기록 배제). 기록 시각이 지금보다 **5분 넘게 미래**면 위조로 보고 거부. 재설치 증거(seen_down 또는 전달 이후 부팅)가 아직 없으면 조용히 보류.
- 인정되면 해당 호스트는 완료(`done_src=external`)로 처리되어 run 대상에서 제외되고, 기록은 `done/applied/` 로 이동합니다. 거부 사유는 로그에 1회만 남습니다.
- 수동: `auto_setup done <host...>` — 출처 `manual`, **부팅 시각 검증 없음**(경고 표시, 전달 이전 기록만 거부). 정말 끝난 호스트에만 사용.
- 기록 실패는 os_check 가 경고 1줄만 출력하며 결과·종료코드는 불변입니다. 두 변수가 모두 비면 기존과 100% 동일합니다.

### 이중체크 (2차 체크)

1차 run 이 끝난 뒤 **route=local 호스트 중 접속불가(결과에 없음) 또는 FAIL 인 호스트만** os6_mgmt 에서 `os_check -auto` 를 **한 번 더** 실행합니다(역방향 재시도 없음, route=os6 호스트는 이미 os6 경유이므로 제외).

- 같은 run 큐에서 이어서 수행(동시 1개 유지). 결과는 `runs/<code>/second/` 에 회수(ssh + tar)하고, code 파일 끝에 `### 2차 체크 (os6_mgmt)` 섹션을 이어 붙입니다(대상·결과·병합 후 최종 FAIL/NO FAIL).
- **최종 판정 = 1차 OK 또는 2차 OK**.
- **부작용**: os_check 의 설정(set) 단계가 목록에 p/d 호스트가 있으면 같은 호스트에 **설정을 한 번 더 적용**합니다. 원치 않으면 `os6_os_check_sh="-"` 로 2차 체크를 끄십시오.
- `os6_mgmt` 가 비어 있거나 `os6_os_check_sh` 해석 결과가 비면 2차 체크는 생략되어 1차와 동일합니다.

### LDAP 백업/복원

옛 OS 가 살아 있는 **전달 시점**에 호스트별 LDAP 관련 설정을 백업하고, 설치가 끝난(READY) 뒤 **os_check 실행 전**에 비교·복원합니다. os_check/설정 스크립트는 변경 없음.

1. 전달 시점: job 수거 직후 ping O 이고 아직 seen_down 이 아닌 호스트를 gossh 로 백업(`/tmp/auto_setup/ldapbak/<jobid>/<host>/`, 디렉터리 0700 · 파일 0600). 이미 재설치 중이거나 접속 불가면 `backup:none` → 이후 단계 생략.
2. READY 후: 새 OS 의 `ldap.conf` 에서 **`binddn` 줄의 `uid=<값>` 의 값만** 뽑아 백업본의 같은 값과 비교합니다. 예: `binddn uid=asdf,ou=user,dc=x` → `asdf`. 원격에서는 **bindpw 를 읽지도 출력하지도 않고** uid 값 한 줄만 회수합니다(키워드 `binddn`/`BINDDN` 대소문자 무관, 따옴표 제거).
   - 같으면(`same`) 생략, 다르면(`diff`) **백업 파일 세트를 복원**(원 권한·소유, `restorecon`, 서비스 재시작) 후 새 OS 의 binddn uid 가 백업과 같아졌는지 재확인.
   - 어느 한쪽이라도 binddn uid 를 읽지 못하면(`na`, `binddn 확인불가`) 복원하지 않습니다.
   - `ldap_share_dir` 이 비어 있으면 diff 여도 자동 복원하지 않고 `수동 복원 필요(ldap_share_dir 미설정, 백업: <경로>)` 로 표시합니다(비고에 `LDAP 수동 복원 필요`).
   - 실패 시 로그·상태에만 남기고 run 은 계속 진행(os_check 가 FAIL 로 보고).
3. 복원 전송 방식(`ldap_share_dir` 설정 시): os8_mgmt 가 `<ldap_share_dir>/.as_ldap_<랜덤 32자>/<호스트>/` 를 0700, 파일을 0600 으로 만들어 백업 파일을 잠시 두고, gossh 명령에는 **경로·mode·owner·group·서비스명만** 싣습니다. 대상 서버는 그 파일을 `cat` 으로 읽어 제자리에 쓰고(원 mode/owner 로 `mv`), 끝나면 성공·실패와 관계없이 os8_mgmt 가 공유 임시 디렉터리를 **즉시 삭제**합니다. 대상이 공유경로를 못 읽으면 `공유경로 접근 실패` 로 표시하고 아무것도 바꾸지 않습니다.

**대상 파일**

| 구분 | OS 7 이하 | OS 8 이상 |
|---|---|---|
| 공통 | `/etc/openldap/ldap.conf`, `/etc/resolv.conf` | 동일 |
| 인증 | `/etc/nslcd.conf` | `/etc/sssd/sssd.conf` |
| 시각 | `/etc/ntp.conf` | `/etc/chrony.conf` |

s4 호스트(hostname 접두사 규칙)는 OS 8+ 여도 `services` 에 `nslcd`/`ntp` 가 있으면 해당 파일을 씁니다. `auto_setup` 은 `ldap_config.conf` 를 읽지 않으므로 규칙을 환경변수로 받습니다.

| 환경변수 | 기본 | 설명 |
|---|---|---|
| `AUTO_SETUP_LDAP_S4_PREFIX` | (비면 s4 규칙 꺼짐) | s4 호스트명 접두사 |
| `AUTO_SETUP_LDAP_S4_SERVICES` | `nslcd,ntp` | s4 호스트가 구형 서비스를 쓰는 항목 |

> **bindpw 취급**
> - 복원은 공유경로 경유이며 **gossh 명령줄(대상 서버의 `ps` 포함)에 bindpw 가 실리지 않습니다.** 공유경로는 **대상 root 가 읽을 수 있어야 하며(no_root_squash)**, 임시 디렉터리는 사용 후 자동 삭제됩니다. os6_mgmt 경유 호스트도 os6_mgmt 가 같은 공유 경로를 봐야 합니다.
> - 백업 수집은 gossh 응답(stdout)으로 받으며 명령은 `cat` 뿐입니다. 저장은 0700/0600 입니다.
> - 백업(`/tmp/auto_setup/ldapbak/<jobid>/`)은 **자동 삭제되지 않습니다.** 작업이 끝나면 수동으로 `rm -rf` 하십시오.
> - 로그·snapshot·code·wall·TUI 에는 bindpw 가 남지 않습니다(비교 기준은 binddn uid, 기본 로그에는 호스트별 same/diff 만 남기고 uid 값도 남기지 않음).

### 단계별 점검 (테스트)

#### 기본 점검 (대화형, 데이터 격리)

```bash
# 기본: 각 단계마다 Enter 를 받으며 1~17단계 진행 (운영 /tmp/auto_setup 은 건드리지 않음)
bash test/manual_check.sh

# 단계 명령
bash test/manual_check.sh -y          # 전 단계 연속 실행 (wall·설치 단계 제외)
bash test/manual_check.sh -s 11       # 11단계(2차 기능 시작)부터 재시작
bash test/manual_check.sh --install   # 9단계 설치까지 포함

# 실제 ICMP 점검 (root 필요, 응답/무응답 IP 지정)
PING_UP=192.168.0.59 PING_DOWN=192.168.0.250 bash test/manual_check.sh -s 4
```

단계: 1 빌드·단위테스트, 2 CLI, 3 ensure·재기동, 4 실제 ICMP, 5 01→queue→jobs, 6 os_check -auto, 7 목업 전체 흐름, 8 wall, 9 설치, 10 정리, **(2차) 11 상태 TUI·--plain·요청, 12 데몬 --start/--restart/--stop, 13 양방향 동일성, 14 완료기록, 15 LDAP 백업/복원(스텁), 16 2차 체크, 17 정리**

#### 자동 테스트

```bash
# Go 단위 테스트 (가짜 시계·pinger·checker, TUI 골든 포함)
go test ./...

# 정적 검사
go vet ./... && gofmt -l .

# 1차 목업 E2E (스크래치, 스텁 gossh/ssh/wall, root 필요, 약 1분) — 85건
bash test/run_e2e.sh

# 2차 목업 E2E (a 양방향 동일성 / b 완료기록 / c 요청·TUI / d 데몬 제어 / e LDAP / f 2차 체크) — 191건
bash test/run_e2e2.sh          # 전체
bash test/run_e2e2.sh b e      # 시나리오 지정

# E2E 스크래치 보존 (로그 확인)
KEEP_SCRATCH=1 bash test/run_e2e2.sh f

# awx_script 테스트 (54건)
bash ../awx_script/test/run_tests.sh
```

## 옵션별 상세

### 조정 가능한 상수 (main.go)

| 상수 | 기본값 | 설명 |
|---|---|---|
| `pingInterval` | 10s | ICMP 송신 주기 |
| `downMisses` | 2 | 연속 무응답 횟수 → ping X 판정 |
| `checkInterval` | 30s | 준비확인 체크 주기 |
| `bootWait` | 7분 | 첫 READY 이후 모든 호스트 대기 |
| `lateWait` | 3분 | 늦게 올라온 호스트 묶음 대기 |
| `queuePoll` | 5s | queue·요청·완료기록 수거 주기 |
| `installStuck` | 60분 | seen_down 후 이 시간을 넘겨도 READY 가 안 되면 정체 |

### 호스트 단계(stage)

`대기(queued)` → `배포중(deploying)` → `설치중(installing)` → `부팅확인(booting)` → `READY` → `LDAP확인(ldap)` → `체크중(checking)` → `2차체크(second)` → `완료(done)`. 부가: `정체(stuck)`, `실패(failed)` = os_check 비정상 종료 연속 3회(`maxFails`).

### 환경변수

| 이름 | 기본값 | 설명 |
|---|---|---|
| `AUTO_SETUP_DIR` | `/tmp/auto_setup` | 작업 데이터 위치 (테스트만 오버라이드) |
| `AUTO_SETUP_CONF` | (없음) | 설정 파일 디렉터리 (`<디렉터리>/auto_setup.conf`, 최우선 탐색) |
| `AUTO_SETUP_LDAP_S4_PREFIX` | (없음) | LDAP s4 규칙: 호스트명 접두사 |
| `AUTO_SETUP_LDAP_S4_SERVICES` | `nslcd,ntp` | LDAP s4 규칙: 대상 서비스 |
| `AUTO_SETUP_NOW` | (없음) | **테스트 전용** snapshot 기준 시각(epoch 초). **운영에서 설정 금지** |
| `NO_COLOR` | (없음) | 설정하면 TUI·메시지 색 끔 |
| `COLUMNS` | 120 | `--plain` 출력 폭 |

## 문서별 설명

| 파일 | 설명 |
|---|---|
| `main.go` | 최상단 빈 변수 8개 + CLI 분기 + 상수 정의 |
| `paths.go` | os6 os_check 경로 해석, dhcp.sh 후보 (autofs 동일 경로 포함) |
| `conf.go` | 설정 파일 로더 (빈 변수 채우기) |
| `daemon.go` | queue 수거·경로 판별·ping 감시·7분/3분 스케줄·run 큐·LDAP/2차 단계 연동 (상태기계) |
| `state.go` | Job/Host 구조체, queue 그룹 줄 파싱, JSON 원자 저장·복원 |
| `model.go` | 호스트 단계(Stage)·정체 판정, Snapshot 구조체와 BuildSnapshot (CLI·TUI·원격 공용) |
| `client.go` | SnapshotSource(로컬/원격), gossh 원샷 전송·출력 복원 (원격 클라이언트 모드) |
| `ctl.go` | `--start/--stop/--restart`, snapshot/done/request 명령, 색 메시지 |
| `tui.go` / `render.go` | TUI 루프·키 처리 / 순수 렌더 함수(상태→문자열) |
| `tty_linux.go` / `tty_other.go` / `tui_hook.go` | raw tty·창 크기 / 비 Linux 대체 / 리포트 진입 훅 |
| `done.go` | 완료기록 인정 규칙·수거, 수동 완료 |
| `requests.go` | 요청 채널(`requests/*.req`) 기록·수거(manual-run / cancel) |
| `ldapbk.go` | LDAP 백업(전달 시점)·비교·복원 |
| `second.go` | 2차(이중) 체크: os6_mgmt 실행·결과 회수·code 병합 |
| `icmp.go` | raw 소켓 ping (표준 net, 체크섬·파싱 직접) |
| `probe.go` | probe 하위명령 + os6 장기 ssh 세션 관리 |
| `check.go` | gossh -pm uptime 준비확인 (local/os6) |
| `runner.go` | os_check 실행(-auto), os6 gossh 래퍼, code 생성, wall |
| `ensure.go` | ensure 하위명령: flock + setsid + 데몬 기동 |
| `log.go` / `notify.go` / `iface.go` | 로그 / wall 알림 / 인터페이스(Pinger·Checker·Runner·Notifier·Clock·LdapBackup·Second) |
| `*_test.go` / `testdata/` | 단위 테스트·TUI 골든 |
| `test/manual_check.sh` | 대화형 점검 1~17단계 |
| `test/run_e2e.sh` / `test/run_e2e2.sh` / `test/lib_e2e.sh` | 1차 / 2차 목업 E2E / 공용 함수 |
| `setup.sh` | 빌드·설치·cron·tmpfiles 등록 (멱등) |
| `build_os6.sh` | CGO_ENABLED=0 정적 빌드 (Go 1.20, os6_mgmt 용) |
| `README.md` | 이 문서 — 설치·사용·옵션 가이드 |
| `CHANGELOG.md` | 변경 이력 |
| `ARCHITECTURE.md` | 폴더 구조·함수 역할·런타임 디렉터리 |
| `WORKFLOW.md` | 실행 흐름도 (텍스트 mermaid) |
| `workflow.svg` | 실행 흐름도 (SVG) |
| `PR_CHECKLIST.md` | 풀리퀘스트 검증 항목 |
| `사용법.txt` | 명령어 중심 빠른 참고 (운영 담당자 용도) |
| `.github/workflows/ci.yml` | 폴더 내 CI (go vet / test / build, gofmt, bash -n / shellcheck) |

## 알려진 제한

- `/tmp/auto_setup` 의 **소유자 검증이 없습니다.** root 만 쓰는 서버에서 운영하고 `ls -ld /tmp/auto_setup` 으로 소유자·권한을 확인하십시오.
- LDAP Apply 는 **직렬 처리**입니다(호스트가 많으면 READY 후 run 시작이 늦어질 수 있음).
- **정리 정책이 없는 디렉터리**: `ldapbak/`, `requests/rejected/`, `jobs/done/`, `runs/*/second/` — 필요시 수동 정리.
- `AUTO_SETUP_NOW` 는 테스트 전용입니다(운영에서 설정하면 snapshot 시각이 어긋남).
- 원격 클라이언트 모드의 gossh 호출에는 **시간 제한이 없습니다**(os8_mgmt 가 응답하지 않으면 대기).
- 2차 체크는 설정(set)을 한 번 더 적용할 수 있습니다(`os6_os_check_sh="-"` 로 끔).

## 주의사항 (Disclaimer)

본 스크립트 및 도구는 **100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.**

**설정 변경 후 반드시 다음을 확인하세요:**

1. 설정 변경 스크립트이므로, **설정 변경 후 랜덤한 서버 몇 대를 선택하여 실제로 변경되었는지 확인하세요.** LDAP 복원·2차 체크도 서버 설정을 바꿉니다.
2. 데몬이 상시 실행되므로 감시 대상이 많거나 os_check 가 오래 걸리면 리소스를 모니터링하세요.
3. 무기한 감시 작업은 `auto_setup cancel` 로 종료할 때까지 계속되므로, 필요시 `jobs/done/` 을 확인해 정리하세요.
4. LDAP 백업(`ldapbak/`)에는 bindpw 가 포함된 원본 설정이 있으므로 작업 종료 후 수동 삭제를 권장합니다.

### 기존 01·os_check 사용법은 변경되지 않음

- **01.AWX_nodeinfo_V2.sh**: 프롬프트·출력·종료코드 동일, auto_setup 전달은 로그 1줄뿐(`auto_setup_host` 가 비면 1차와 100% 동일)
- **os_check_final_annotated.sh**: 인자 없이 실행하면 지금과 100% 동일 (`-auto` 일 때만 분기, `auto_done_*` 가 비면 아무 동작 없음)

## 전역 명령어로 사용하기 (선택)

setup.sh 가 이미 `/usr/local/bin/auto_setup` 에 설치했으므로 PATH 에 있습니다(os6_mgmt 는 원격 클라이언트 빌드를 `/usr/local/bin/auto_setup` 으로 복사하면 같은 명령이 됩니다).

```bash
# 상태 리포트 (TUI)
auto_setup

# 데몬 제어 (os8_mgmt)
auto_setup --restart

# 작업별 상태 / 4자리 code 결과 / 취소
auto_setup status
auto_setup code 4821
auto_setup cancel 20261002-153000-user1

# 수동 완료 처리 / 수동 OS 체크 요청
auto_setup done web01 web02
auto_setup request manual-run 20261002-153000-user1 gpu.yml
```

## 버전 관리 및 태그

git 태그 규칙:

```bash
# 1차 릴리스
git tag -a auto_setup-v0.1.0 -m "OS 설치 → 설정체크 자동 연계 데몬 및 CLI"

# 2차 릴리스
git tag -a auto_setup-v0.2.0 -m "양방향 동일 실행·완료기록·LDAP 백업/복원·TUI (CHANGELOG 2026-10-03 항목)"
git push origin auto_setup-v0.2.0
```

태그 규칙: `auto_setup-v<major>.<minor>.<patch>`

## 직접 테스트 방법 요약

```bash
# 1단계: 기본 검사
bash -n setup.sh build_os6.sh test/*.sh
go vet ./... && gofmt -l .

# 2단계: 대화형 점검
bash test/manual_check.sh -y

# 3단계: 자동 테스트
go test ./...
bash test/run_e2e.sh          # 85건
bash test/run_e2e2.sh         # 191건
bash ../awx_script/test/run_tests.sh   # 54건

# 4단계: 랩 스모크 테스트 (.58)
bash setup.sh
auto_setup status
# 실제 ping·ssh 동작 확인
```

자세한 명령은 `사용법.txt` 참조.
