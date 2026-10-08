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
├── conf/
│   ├── auto_setup.conf.example ← 빈 변수 설정 파일 예시 (복사해 auto_setup.conf 로 사용)
│   ├── setup_guide.sh    ← 빈 변수 입력 가이드 (역할 선택 → 변수별 설명·예시·검증 → setup.sh 이어서 실행)
│   ├── update_v0.4.0.sh  ← 현장 사본 업데이트 (현장 값·주석 보존, 신규 변수만 질문, --undo)
│   └── vars.manifest     ← 가이드·업데이트가 쓰는 변수 목록 (설명·예시·역할·검증)
├── test/
│   ├── run_setup_scripts.sh ← 가이드·업데이트 스텁 검증 (25건)
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

**가이드로 채우기 (권장)** — 변수마다 설명·예시를 보여 주고 `read` 로 값을 받아 검증한 뒤 기록합니다. 역할(os8 = 데몬 서버 / os6 = 원격 클라이언트)을 고르면 그 역할의 변수만 묻습니다.

```bash
cp -n conf/auto_setup.conf.example conf/auto_setup.conf   # 처음 한 번 (이미 있으면 그대로 둠)
bash conf/setup_guide.sh                                  # 역할 선택 → 변수 입력 → (os8) setup.sh 이어서 실행 y
```

- 현재값이 표시되고 Enter 면 유지, 경로 변수는 이 서버에 실제로 있는지 확인합니다(다른 서버 위 경로는 확인하지 않음). 잘못된 값은 다시 입력하거나 `s` 로 건너뜁니다.
- os6 역할에서 `setup.sh 를 이어서 실행하시겠습니까?` 는 **n** (setup.sh 는 Go 빌드·cron 등록용, os8_mgmt 전용).
- 옵션: `--role os8|os6`, `--no-setup`, `--dir <프로젝트 루트>`.

**직접 채우기**

```bash
cp conf/auto_setup.conf.example conf/auto_setup.conf
vi conf/auto_setup.conf        # 예: os8_mgmt=<os8_mgmt 호스트>
```

- 설정 파일 위치(먼저 발견된 **하나만** 사용): ① `$AUTO_SETUP_CONF/auto_setup.conf` ② `<auto_setup 실행 파일 디렉터리>/conf/auto_setup.conf` ③ `/etc/auto_setup/auto_setup.conf` (`setup.sh` 가 `conf/auto_setup.conf` 를 여기에 설치, 0600)
- **빌드 때 `-ldflags -X` 로 넣은 값이 있으면 그 값이 우선**하고, 설정 파일은 비어 있는 변수만 채웁니다. 알 수 없는 키·형식이 틀린 줄은 무시합니다.
- 예) 저장소의 빌드 완료 `auto_setup_os6` 를 os6_mgmt 에 두고 같은 디렉터리의 `conf/auto_setup.conf` 에 `os8_mgmt=<호스트>` 만 적으면 원격 클라이언트로 동작합니다.
- 실제 `conf/auto_setup.conf` 는 `.gitignore` 로 커밋에서 제외됩니다(예시 파일만 커밋). 01·os_check 쪽 빈 변수(`auto_done_dir` 등)는 쉘 스크립트이므로 기존 방식대로 채웁니다.

#### main.go 9개 + AWX 프로파일 9개 (v0.6.0)

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
| `awx_profile_1` ~ `awx_profile_9` | (v0.6.0, 선택) TUI `w` 로 AWX 를 실행할 때 고르는 프로파일 9개. 형식 `nodeinfo(y/n)\|OS값\|설명` (아래 「체크스크립트 단독 실행(x) · AWX 실행(w)」 참고) | 그 번호는 쓰지 않음. 프로파일이 하나도 없으면 `w` 는 질문 없이 AWX 를 그대로 실행 |

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

### 폐쇄망 현장 사본 업데이트

새 버전 폴더를 **기존 폴더 위에 덮어 풀면** `conf/auto_setup.conf`(커밋 제외 파일)는 그대로 남습니다. 그다음 업데이트 스크립트로 설정 파일의 바뀐 부분만 반영합니다.

```bash
# 1. 새 버전 폴더를 기존 폴더 위에 덮어쓰기 (conf/auto_setup.conf 는 저장소에 없으므로 보존됨)
# 2. 설정 파일 업데이트 — 현장 값·주석은 건드리지 않고 이번 버전 변경만 반영, 신규 변수만 질문
bash conf/update_v0.4.0.sh --dry     # 미리보기 (파일 불변)
bash conf/update_v0.4.0.sh
# 3. (os8_mgmt) 재빌드·설치 후 데몬 재기동 — 실행 중인 데몬은 옛 바이너리이므로 재기동해야 새 동작이 적용됨
bash setup.sh
auto_setup --restart
# 4. (os6_mgmt) 새 auto_setup_os6 를 os6_autosetup 경로에 'auto_setup' 이름으로 다시 복사
# 되돌리기 (설정 파일만, 가장 최근 백업)
bash conf/update_v0.4.0.sh --undo
```

- 업데이트는 앵커(코드 줄) 기준으로 삽입합니다. 앵커 줄이 현장에서 바뀌었거나 지워졌으면 **파일을 건드리지 않고 중단**하고 위치를 알려 줍니다. CRLF 사본도 자동으로 LF 로 처리합니다.
- 백업은 `conf/auto_setup.conf.bak.<시각>`, 되돌리기 기록은 `conf/.update_journal` 에 남습니다(커밋 제외).
- **v0.6.0 업데이트**: `bash conf/update_v0.6.0.sh` (미리보기 `--dry`, 되돌리기 `--undo`). 새 변수는 선택 항목 `awx_profile_1`~`9` 뿐입니다. 업데이트 뒤 설정 변경 확인은 아래 「주의사항 (Disclaimer)」 을 따르십시오.
- v0.4.1 은 설정 파일 변경이 없습니다(바이너리만 변경, `update_v0.4.0.sh` 가 최신 업데이트 스크립트). v0.4.0 의 설정 파일 변경은 `os6_mgmt` 위 안내 주석 1줄뿐이고 신규 변수는 없습니다. 동작 변경은 바이너리에 있으므로 3·4 단계가 필수입니다.

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
codes/<code>.txt 생성 (wall 알림은 v0.4.3 부터 끔 — 결과는 TUI·`auto_setup code NNNN`·로그로 확인) (호스트 현황, 4자리 code) / 완료기록·수동 완료는 run 대상에서 제외
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
| `code NNNN` | code 에 해당하는 결과 코멘트 출력 (로그 `run 완료: … code NNNN` 또는 TUI 의 최근 run 에 나오는 4자리) |
| `cancel <jobid>` | 무기한 감시 중인 작업 종료 (status 에서 확인) |
| `done <host...>` | 호스트를 수동 완료 처리 (출처 manual, 부팅 시각 검증 없음) |
| `done --job <jobid> [--yml <그룹>]` | job(또는 그 그룹)의 미완료 호스트 전부를 수동 완료 처리 (완료된 호스트는 건드리지 않음, 진행 중 job 만) |
| `request manual-run <jobid> <yml> [check]` | 수동 실행 요청 (완료 여부와 상관없이 그룹 전체를 시도, 접속불가 호스트는 로그에 기록). 기본 = 설정체크 + 설정수정(TUI c), `check` = 설정체크만·환경설정 수정 안 함(TUI t) |
| `request cancel <jobid>` | 작업 종료 요청 |
| `request refresh` | 데몬에 즉시 ping·준비확인 요청 (TUI `r` 키와 같음, 5초 안에 수거) |
| `request recheck <jobid> <yml 또는 *>` | 완료 제외 호스트를 os8_mgmt → 안 되면 os6_mgmt 순으로 직접 재확인 (TUI `g` 키와 같음, 5초 안에 수거) |
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
| 1 | a | 선택한 job 의 **모든 그룹(yml) 호스트를 한 표로** (그룹(yml) 열 표시, 수동 실행은 그룹별 화면에서) |
| 1 | q / Esc | 종료 |
| 2 (호스트표) | ↑ ↓ (k j) | 호스트 행 이동 |
| 2 | ← / Esc / q | 화면 1 로 복귀 |
| 2 | f | 정체·실패 호스트만 보기 (토글) |
| 2 | a | 그룹별 화면 ↔ 전체 보기 전환 (전체 보기에서 a 는 화면 1 로) |
| 2 | c | OS 체크 수동 실행(이중체크). **완료 여부와 상관없이** 그룹 전체를 시도, y/n 확인 후 요청 (접속불가·미응답 호스트는 결과 wall 에 따로 표시) |
| 공통 | r | 바로 새로고침 + 데몬에 즉시 ping·준비확인 요청 (자동 갱신: 로컬 2초, 원격 10초. 연타는 5초에 1회만 요청) |
| 공통 | x | 체크스크립트 단독 실행 (user·호스트 입력 → t/c, 기록 안 남김) — 아래 참고 |
| 공통 | w | AWX 실행 (awx_dir 의 01 을 이 터미널에서 실행, 프로파일 선택) — 아래 참고 |
| 공통 | Ctrl+C / Ctrl+Z | 무시 (종료는 q) |
| 공통 | ? | 도움말 (아무 키나 누르면 닫힘) |

- 집합(행 단위) = 01 이 전달한 **그룹 yml** 입니다. 그룹 줄이 없는(구버전 01 또는 그룹 1개) 작업은 `(all)` 한 행이며, 어느 그룹에도 없는 호스트는 `(기타)` 로 모읍니다. 전체 yml(`all=`)은 작업 제목에 표시됩니다. 화면 상단에 진행 중 작업 합계(총계)가 나옵니다.
- **색**: 완료 초록 / 진행 청록 / 대기 회색 / 경고 노랑 / 정체·실패 빨강(굵게). `NO_COLOR` 설정, tty 가 아님(파이프), `--plain` 이면 색을 쓰지 않습니다.
- **정체 기준**: seen_down(설치 시작) 후 **60분까지는 "설치중"**(RHEL7 은 1시간 걸림), 그 이후에도 READY 가 안 되면 빨간 "정체"(`installStuck` = 60분, 경계값은 아직 설치중). 화면 표시만 하며 wall 알림은 없습니다.
- os6_mgmt 에서도 동일하게 열리며 gossh 로 snapshot 을 10초 간격으로 폴링합니다 (v0.6.0 부터, 이전 5초). 키 입력(수동 실행)은 요청 파일을 os8_mgmt 에 남깁니다(데몬이 5초 주기로 수거).
- 수동 실행은 데몬 run 큐에서 실행됩니다(동시 1개 유지, code·wall 동일). 거부되면 `requests/rejected/<파일>` 의 마지막 `reason=` 줄에 사유가 남습니다.
- **정체 호스트 수동 재시도**: 정체 상태인데 실제로는 접속 가능한 호스트는 `r` 을 누르면 ping 상태와 무관하게 즉시 다시 확인합니다 (로컬 무응답이면 os6_mgmt 경유로도 확인).
- **수동 실행 후 완료 처리**: 수동 실행이 정상 종료되어 결과가 나온 호스트는 현재 단계(정체·설치중 등)와 상관없이 완료로 바뀝니다.
- **수동 실행 전 경로 재확인**: 수동 실행 직전에 호스트를 한 번 확인해, os8 에서 직접 응답하면 local, os8 무응답이고 os6_mgmt 경유로만 응답하면 os6 로 맞춘 뒤 실행합니다 (`os6_mgmt`·`os6_gossh` 설정 시).
- **% (진행률) = 완료 대수 / 전체** 입니다. 배포중·설치중·부팅확인·체크중 호스트는 막대의 `+`(청록)와 `진행 N` 으로만 보이고 % 는 0% 그대로이다가, 호스트가 **완료**될 때 오릅니다. 설치 중 0% 는 정상입니다.

### 체크스크립트 단독 실행(x) · AWX 실행(w) (v0.6.0)

화면 1·2 어디서나 쓸 수 있습니다. `x` 는 TUI 안에서 끝나고, `w` 는 실행하는 동안 TUI 를 잠시 내려놓고 이 터미널에서 01 을 직접 실행합니다.

| 키 | 화면 | 동작 |
|---|---|---|
| `x` | 어디서나 | 체크스크립트 단독 실행 시작 (user 선택부터) |
| `w` | 어디서나 | AWX 실행 시작 (`awx_dir` 의 `01.AWX_nodeinfo_V2.sh`) |
| `t` / `c` | x 의 모드 선택 | `t` 설정체크만 / `c` 설정체크 + 설정수정 (선택 즉시 실행) |
| `Ctrl+D` | x 호스트 입력 | 호스트 입력 완료 (공백·쉼표·탭·`\|` 는 줄바꿈, 중복 제거) |
| `Enter` / `Esc` | x 입력 | 줄바꿈 / 취소 (`q` 는 입력창이 비었을 때만 취소) |
| `Ctrl+X` → `y` | x 실행 중 | 실행 취소 (프로세스 그룹 종료). `n` 이면 계속 |
| `Ctrl+X` | w 실행 중 | **즉시** 취소 (01 이 SIGINT 를 받음) |
| `y` / `n` / `1`~`9` | w 질문 | auto 실행 여부 / 프로파일 번호 선택 (Esc 뒤로) |
| `q` / `Esc` | x 실행 중 | 무시 (결과가 나온 뒤 결과 창에서 q/Esc 로 닫기) |
| `↑` `↓` `Space` `b` | x 결과 창 | 스크롤 / 쪽 이동 (q/Esc 닫기) |

**x (체크스크립트 단독 실행)**

- **user 선택**: awx_dir 의 01 에서 `user_route` 를 읽어 `info_mn.sh` 메뉴를 보여 주고, 번호를 입력하면 `info.sh` 결과가 user 가 됩니다. 메뉴를 찾지 못하면 user 이름을 직접 입력합니다(영문·숫자·`.` `_` `-` 만).
- **호스트**: 여러 줄로 붙여넣고 `Ctrl+D`. 0대면 다시 입력합니다.
- **모드**: `t`(설정체크만) 또는 `c`(설정체크 + 설정수정, 빨강 표시). 고르면 바로 실행합니다.
- **실행**: 임시 디렉터리에 `targets.txt` 를 만들고 `os_check_sh` 를 `-auto-check` 또는 `-auto` 로 호출합니다. 경과 시간과 로그 끝 15줄을 1초마다 갱신합니다.
- **결과**: 기존 결과 창과 같은 화면입니다. 결과 창을 닫으면 임시 디렉터리를 지웁니다. auto_setup.log·codes·runs 에는 아무것도 남기지 않습니다(일회성). `os_check_sh` 가 비어 있으면 안내만 합니다.

**w (AWX 실행)**

- `awx_dir` 이 비었거나 01 이 없으면 안내만 합니다.
- 유효한 프로파일이 있으면 `AWX auto 실행? [y/n]` 를 묻습니다. 화면의 노란 안내대로 **yml 마다 OS 버전이 다르면 `n`** 을 누르세요. `y` 면 프로파일 번호를 고르고, 유효한 프로파일이 없으면 묻지 않고 일반 실행합니다.
- **user · 작업 대상 서버 목록 입력 (v0.6.1)**: auto 여부(프로파일)를 정한 뒤 `x` 와 같은 user 메뉴로 user 를 고르고, 이어서 `작업 대상 서버 목록을 붙여넣은 뒤 Ctrl+D` 화면이 나옵니다. 호스트명(공백·쉼표·| 구분) 또는 12필드 줄을 줄 단위로 섞어 붙여넣을 수 있습니다. 입력한 목록은 `awx_dir/<user>.txt` 를 **덮어쓰고**(백업 없음), 01 의 `등록 후 확인 : 작업 대상 서버 목록을 붙여넣으세요` 에도 호스트명이 자동으로 입력됩니다(01 은 `AWX_USER`·`AWX_VERIFY_FILE` 환경변수를 받음). nodeinfo=n 이면 `<user>.txt` 가 12필드여야 하므로 12필드 줄을 붙여넣으세요. 목록을 비우고 Ctrl+D 면 건너뜁니다(기존 `<user>.txt` 사용, 등록 후 확인은 직접 입력).
- 실행 전에 `[auto_setup] AWX 실행 — 취소: Ctrl+X (즉시), 끝나면 auto_setup 화면으로 돌아갑니다` 한 줄을 출력하고, `awx_dir` 에서 `bash 01.AWX_nodeinfo_V2.sh` 를 실행합니다. auto 를 골랐으면 `AWX_AUTO=1 AWX_AUTO_NODEINFO=<y|n> AWX_AUTO_OS=<값>` 이 추가됩니다(자세한 내용은 awx_script README).
- 끝나면 TUI 로 돌아와 `AWX 종료 (rc=N)` 또는 `AWX 취소됨 (Ctrl+X)` 을 표시합니다.

**프로파일 conf 예시** (`conf/auto_setup.conf`, 형식 `nodeinfo(y/n)|OS값|설명`)

```
awx_profile_1=y|2025|nodeinfo 사용, 모든 yml 을 OS 2025 로
awx_profile_2=n|2026-OPC_MDP|nodeinfo 미사용, OPC_MDP
```

- OS 값은 `2024 2025 2026 2026-OPC_MDP 2026-ECAD_TCAD default` 중 하나입니다. 필드가 부족하거나 nodeinfo 가 y/n 이 아니거나 OS 값이 허용되지 않으면 그 프로파일은 목록에서 빠지고, 목록 아래 회색 줄에 사유가 표시됩니다.

> **부하**: 대부분 os8_mgmt 에서 쓰면 부하가 거의 없습니다. 원격(os6) 화면은 자동 갱신이 **10초마다 gossh 1회**입니다.

**모의 테스트로 확인**: `bash test/demo_flow.sh` 로 모의 환경을 만들고(데모 conf 는 awx_profile_1·2 가 정상, 3 은 일부러 틀린 예), `source /tmp/as_demo_flow/env.sh` 후 `auto_setup` 을 실행해 `x` 로 127.0.0.11/12/14 를 붙여넣고 `t` 또는 `c`, `w` 로 auto `y` → 프로파일 1 을 골라 보고 `Ctrl+X` 취소도 눌러 보십시오. 끝나면 `bash test/demo_flow.sh --stop`.

### 실행 모드: 설정체크 + 설정수정 / 설정체크만 (v0.5.0)

| 구분 | 키 / 명령 | 환경설정 수정 | 호스트 완료 처리 | os_check 호출 |
|---|---|---|---|---|
| **배포 → 완료 자동 흐름** | (자동) | **수정함** (체크 → 수정 → 재점검) | 함 | `-auto` |
| 수동 **설정체크 + 설정수정** | `c` / `request manual-run <jobid> <yml>` | **수정함** | 함 (정상 실행된 호스트는 단계 무관) | `-auto` |
| 수동 **설정체크만** | `t` / `request manual-run <jobid> <yml> check` | **수정 안 함** (점검 결과만) | 안 함 (단계·실패 횟수·2차 체크도 그대로) | `-auto-check` |

- 배포 → 완료까지의 자동 흐름은 **항상** 설정체크 + 설정수정입니다. 설정체크만은 수동 `t` 로만 실행됩니다.
- `c`/`t` 는 확인(y/n) 문구에 어느 모드인지 적혀 있고, 결과 code 본문 첫 줄에도 `작업 : 설정체크 + 설정수정` / `작업 : 설정체크만 (설정 수정 안 함)` 으로 남습니다.
- **결과 창**: `y` 를 누르면 전체 화면 결과 창이 열려 요청 전달 → 대기 → 실행 중을 보여 주다가, 끝나면 그 창에 결과(결과 리포트·FAIL 줄·NO FAIL·LDAP/SPLUNK/커널 요약)를 그대로 출력합니다. `↑↓` 스크롤, `Space`/`b` 쪽 이동, `r` 새로고침, `q`/`Esc` 닫기(닫아도 데몬은 계속 진행). 호스트표에서 `v` 로 그 그룹의 가장 최근 수동 실행 결과를 다시 봅니다.
- **`g` 진행 창**: `재확인 시작 → 1/3 os8_mgmt 확인 → 2/3 os6_mgmt 경유 확인 → 3/3 최종 결과` 가 실시간으로 나오고, 끝나면 호스트별 결과표(`os8 응답` / `os6 경유` / `접속불가` 와 어디서 안 됐는지, 응답 호스트는 현재 단계)가 표시됩니다. 창에서 `g` 로 다시 재확인.
- **`-auto-check` 지원 필요**: `OS 환경설정 체크/os_check_final_annotated.sh`(변경 항목 32)에 `-auto-check <user> <목록>` 옵션을 추가했습니다 (`-auto` 와 같되 환경설정 수정 질문에 n). **현장에서 쓰는 os_check/config_check 사본에도 같은 변경이 필요**하며, 없으면 `t` 는 비정상 종료로 기록됩니다 (`c`·자동 흐름은 영향 없음).

#### 모의 테스트 (운영 데이터·실서버 미사용)

root 로 실행합니다 (raw ICMP). 스텁 gossh/ssh + 복사본 os_check 로 데몬이 실제와 같은 흐름을 돕니다.

```bash
bash test/demo_flow.sh              # 빌드 + 가짜 작업(오늘/어제) + 스텁 + 데몬 기동, 사용법 출력
source /tmp/as_demo_flow/env.sh     # 이 셸에 AUTO_SETUP_DIR·PATH·auto_setup 별칭 설정
auto_setup                          # 화면: 오늘 그룹 A/B 에서 Enter → t / c / g / v
bash test/demo_flow.sh --stop       # 데몬 종료 + 정리
```

시나리오: 그룹 A(127.0.0.11 정상 / .12 설정체크 FAIL / .13 이미 완료 / .14 접속불가) 에서 `t` → FAIL·접속불가가 결과 창에 나오고 호스트는 완료 처리되지 않음, `c` → 정상 호스트가 완료 처리됨. 그룹 B(.21 os8 무응답·os6 경유 응답 / .22 어디서도 무응답 / .23 os8 응답) 에서 `g` → 진행 창. 화면만 보려면 `bash test/make_demo_jobs.sh` 후 `AUTO_SETUP_DIR=/tmp/as_demo auto_setup` (데몬 없이 파일만 읽음).

### os8_mgmt 에서 접속이 안 되는 호스트 (os6 경유 자동 전환, v0.4.0)

정상 흐름: `대기 → (ping X) 배포중 → (ping O) 부팅확인 / (anaconda 확인) 설치중 → (재부팅 후 ping O, uptime 짧음) READY → 체크중 → 완료`.
anaconda 단계에서 sshd 가 없어 22 포트가 거부되면 준비확인이 응답하지 않으므로 ping 이 되는 동안은 **부팅확인**으로 보입니다(정상).

os8_mgmt 에서 ping·ssh 가 안 되는 호스트도 위와 같은 흐름이 되도록, `os6_mgmt` 가 설정돼 있으면 데몬이 경로를 자동으로 바꿉니다.

| 상황 | 데몬 동작 |
|---|---|
| 첫 ping 에서 os8 무응답 | 처음부터 route=os6 (종전과 같음) |
| route=local 인데 os8 ping 무응답 (설치 중 망 변경, os6_mgmt 를 나중에 채움 등) | 다음 ping 부터 os6 probe 에도 물어 os6 가 응답하면 **route=os6 로 전환** (로그 `경로 전환: <호스트> local → os6`) |
| ping 은 되는데 os8 에서 준비확인(gossh) 무응답 | 같은 주기에 os6_mgmt 경유로 다시 확인, 응답하면 route=os6 로 전환 (`os6_gossh` 필요) |
| os6 probe 세션이 응답이 없음 (ssh 키 미설정 등) | 상태는 그대로 두고 1분 뒤 비고에 `ping 정보없음(os6 probe 확인)` 표시 |

- 자동 전환은 local → os6 한 방향뿐입니다(되돌아가며 흔들리지 않음). route=os6 호스트의 os_check run 은 os6 경로로 실행됩니다. 한 번 os6 가 된 호스트는 계속 os6 경유로 감시합니다: **ping 10초, 준비확인 30초 주기**(os6 probe 세션으로 ping, os6_gossh 로 확인). 되돌리려면 `g`(재확인) 또는 수동 실행 `c` — 이때만 os8 에서 다시 응답하는지 확인해 route 를 local 로 복귀시킵니다.
- **날짜별 보기 (v0.4.8)**: 화면 1 맨 위의 날짜 줄(`<    2026-10-08 (목)    >`)에서 `←` 전날 / `→` 다음날. 맨 위 그룹에서 `↑` 를 누르면 날짜 줄이 선택됩니다. 날짜는 그룹 yml 이름의 시각(`infra_inventory-20261008084159_4ea.yml` → 2026-10-08)에서 읽고, 기본은 가장 최근 날짜입니다.
- **`g` 키 (서버 상태 수동 재확인, v0.4.7)**: 호스트표(그룹/전체 보기)에서 `g` 를 누르면 완료를 제외한 모든 호스트(설치중·정체·실패 포함, 단계 무관)를 데몬이 지금 바로 확인합니다. ① os8_mgmt 에서 확인 → 응답하면 route=local, ② 무응답이면 os6_mgmt 경유 → 응답하면 route=os6, ③ 둘 다 무응답이면 접속불가로 간주(상태는 그대로, 로그에 `[!] 재확인(g) 접속불가·미응답 N대`). 응답한 호스트는 준비확인 결과(설치중·부팅확인·READY)를 즉시 반영합니다. 명령: `auto_setup request recheck <jobid> <그룹.yml 또는 *>`. `os6_mgmt`·`os6_gossh` 가 비면 ① 만 수행.
- **LDAP 백업도 os6 경유 (v0.4.7)**: 전달 시점 LDAP 설정 수집이 os8 에서 안 되는 호스트(ssh 불가 등)는 같은 호출에서 os6_mgmt 경유로 다시 수집하고 route=os6 로 바꿉니다(로그 `경로 전환: … (LDAP 백업 …)`). 설치 후 binddn 비교·복원(Apply)도 os8 무응답이면 os6_mgmt 경유로 한 번 더 시도합니다. 수집이 안 된 호스트는 사유가 로그에 남습니다(`[!] LDAP 백업 불가: <호스트> … - <사유>`).
- `r` 키(또는 `auto_setup request refresh`)는 화면만 다시 읽는 것이 아니라 데몬에 **즉시 ping·준비확인**을 요청합니다. 데몬이 os8 에서 안 보이는 호스트는 위 규칙대로 os6_mgmt 에서 정보를 가져옵니다.
- 배포중에서 멈춰 있으면 데몬 로그에서 `경로 판별`·`경로 전환`·`os6 probe 세션`·`준비확인 실패` 줄을 확인하십시오. os6 경로는 데몬이 `ssh -o BatchMode=yes <os6_mgmt>` 로 접속하므로 os8_mgmt → os6_mgmt **ssh 키 인증**이 되어 있어야 합니다.
- 설정 파일(`os6_mgmt` 등)을 바꾼 뒤에는 `auto_setup --restart` 가 필요합니다(설정은 데몬 기동 때 한 번 읽음).

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
| `log.go` / `notify.go` / `iface.go` | 로그 / 알림(wall 은 기본 꺼짐, 코드는 유지) / 인터페이스(Pinger·Checker·Runner·Notifier·Clock·LdapBackup·Second) |
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
auto_setup request manual-run 20261002-153000-user1 gpu.yml          # 설정체크 + 설정수정 (c)
auto_setup request manual-run 20261002-153000-user1 gpu.yml check    # 설정체크만, 환경설정 수정 안 함 (t)
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
