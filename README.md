# auto_setup — OS 설치 → OS 설정체크 자동 연계

OS 설치(01) 완료 후 자동으로 OS 설정을 검사하고 결과를 공유하는 데몬 + CLI 도구.

- **1차(v0.1.0)**: 01 전달 → ping·준비확인 감시 → os_check 자동 실행 → code/wall 공유
- **2차(v0.2.0)**: 양방향 동일 실행(os8_mgmt 단일 원본, os6_mgmt 는 원격 클라이언트), 이중체크, 완료기록, LDAP 백업/복원, 상태 TUI
- **v0.7.0**: TUI `m` 변경 메뉴 (IP 변경·LDAP 변경·IP+LDAP·원복·auto.appl 원복, 엔진 호출) + 좁은 화면 줄바꿈 — 아래 「변경 작업」 참고
- **v0.7.1**: `m` 메뉴를 화면 전환 없이 안내 줄 아래에 인라인으로 표시, 화면 2 `d` 수동 완료(TUI), 준비확인에서 anaconda 가 보이면 ping X 를 못 봤어도 설치중으로 표시
- **v0.7.2**: `PageUp`/`PageDown` 지원 (목록·호스트표·결과 창·변경 화면)
- **v0.7.3**: `auto_setup -user <이름>` (해당 user 의 job 만 보이고 `x`·`w`·`m` 의 user 선택을 건너뜀), 재확인(`g`)·준비확인이 gossh 실패 사유를 표시, os6 준비확인 원격 명령을 `bash -c` 로 감쌈
- **v0.8.1**: 신규 설치 양식 → AWX 실행에서 **프로파일을 고르지 않으면 nodeinfo 자동 N 을 적용하지 않음** (프로파일을 `y` 로 고른 경우에만 N 강제) — 아래 「신규 설치 양식」
- **v0.8.0**: ① 항목별 인라인 메뉴 `h`(호스트 작업)·`w`(AWX) — 아래 「키」 ② **신규 설치 양식**(`w`→2 / `auto_setup newhost`) ③ **DNS 점검**(양식 생성의 Go 구현 + awx_script 01 의 `[4-1]`) ④ **gossh 작업 기록**(`s` / `auto_setup s`) ⑤ 세션 user ⑥ 결함 수정: 준비확인·`g` 의 gossh/ssh 호출에 **60초 제한**, os6_mgmt 로 보내는 원격 명령을 모두 `bash -c` 로 감싸 **csh/tcsh 로그인 셸에서도 동작**(`!` 포함 명령은 base64 로 전송), gossh 출력 해석 통일(FQDN↔짧은 이름, 해석 실패 시 로그·화면에 사유), 셸 인용 함수 `shq`

흐름도: 전체 [`workflow.svg`](workflow.svg) · [`WORKFLOW.md`](WORKFLOW.md), 명령·키별 31개 [`docs/svg/`](docs/svg/README.md).

## 빌드 및 설치 방법

### 의존성

| 항목 | 요구사항 | 설명 |
|---|---|---|
| 운영체제 | RHEL 8+ | bash, go, gossh, ssh, `script`·`less` (gossh 작업 `s` 가 씀) |
| Go | 폐쇄망은 **Go 1.26.5** 로 빌드 (`go.mod` 는 `go 1.20` — os6 용 Go 1.20 정적 빌드와 호환) | 표준 라이브러리만 (외부 의존성 0). **Go 1.20 문법·표준 라이브러리만** 사용 (내장 `min`/`max`, `slices`/`maps`, range-over-int, `log/slog` 금지) |
| 폐쇄망 | vendor 불필요 | 오프라인 빌드 가능 (`-mod=vendor` 를 쓰지 않음 — 외부 모듈이 없어 vendor 디렉터리가 없음) |
| 권한 | root | raw ICMP, cron 등록, /usr/local/bin |

### 파일 배치

```
auto_setup/
├── main.go / daemon.go / state.go / ... (Go 소스, 파일별 역할은 ARCHITECTURE.md)
│   └── v0.8.0 새 파일 묶음: userfilter.go · sessionuser.go (user) / menu_tui.go · flow_tui.go (메뉴·흐름 화면 공통)
│       / newhost.go · newhost_tpl.go · newhost_tui.go (신규 설치 양식) / dnscheck.go (DNS 점검) / work.go · work_tui.go (gossh 작업)
│       / tcsh_live_test.go (랩 전용 tcsh 시험)
├── auto_setup_os6        ← 빌드 완료 os6 용 정적 바이너리 (Go 1.20, 변수 빈 값 — 회사 환경에는 Go 가 없으므로 저장소에 포함)
├── setup.sh              ← 빌드 + 설치 + cron + tmpfiles 등록
├── build_os6.sh          ← os6 용 정적 빌드 (Go 1.20 서버에서)
├── conf/
│   ├── auto_setup.conf.example ← 빈 변수 설정 파일 예시 (복사해 auto_setup.conf 로 사용)
│   ├── setup_guide.sh    ← 빈 변수 입력 가이드 (역할 선택 → 변수별 설명·예시·검증 → setup.sh 이어서 실행)
│   ├── update_v0.4.0.sh · update_v0.6.0.sh · update_v0.7.0.sh · update_v0.8.0.sh
│   │                     ← 현장 사본 업데이트 (현장 값·주석 보존, 신규 변수만 질문, --dry·--undo). 버전 순서대로 적용
│   └── vars.manifest     ← 가이드·업데이트가 쓰는 변수 목록 (설명·예시·역할·검증, 현재 32개)
├── test/
│   ├── run_setup_scripts.sh ← 가이드·업데이트 스텁 검증 (54건)
│   ├── manual_check.sh   ← 대화형 단계별 점검 (1~17단계)
│   ├── run_e2e.sh        ← 1차 목업 E2E (86건)
│   ├── run_e2e2.sh       ← 2차 목업 E2E (a~f, 194건)
│   ├── lib_e2e.sh        ← E2E 공용 함수·스텁 (gossh, ssh, wall)
│   ├── demo_change.sh / run_change_os.sh (173건) / run_change_lab.sh (18건) ← 변경 메뉴(m) 모의 환경 · RHEL 7/8/9 검증 · .58 실동작 (v0.7.0)
│   └── lib_change.sh / fake_gossh.sh ← 변경 시험용 fixture 노드 · 가짜 gossh (v0.7.0)
├── testdata/             ← TUI 골든 파일, os_check 결과 샘플
├── docs/svg/             ← 명령·키별 흐름도 SVG 31개 + README.md (v0.8.0)
├── .github/workflows/ci.yml ← 폴더 내 CI (저장소 루트의 .github/workflows/auto-setup.yml 이 실제 경로 필터 CI)
├── 사용법.txt
└── 문서 (README.md / CHANGELOG.md / ARCHITECTURE.md / WORKFLOW.md / workflow.svg / PR_CHECKLIST.md / 수정계획서.md)
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

#### 빈 변수 목록 (main.go 9개 + AWX 프로파일 9개 + 변경 4개 + 신규 설치·작업 10개)

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
| `enable_ip_change` · `enable_ldap_change` | (v0.7.0) TUI 변경 메뉴(`m`)의 IP·LDAP 항목을 켬. `y`·`yes`·`1`·`true` 만 켜짐 (「변경 작업」 참고) | 꺼짐 (`--admin` 이면 conf 와 무관하게 켜짐) |
| `ip_engine` · `ldap_engine` | (v0.7.0) 변경 엔진 실행 파일 전체 경로 | auto_setup 과 같은 디렉터리 → PATH 순으로 찾음 |
| `newhost_template_1` ~ `newhost_template_9` | (v0.8.0, 선택) 신규 설치 양식(`w`→2 / `newhost`)의 공용 템플릿 9개. 형식 `별칭\|infra nic disk 용량 os boot` (예 `A\|sec bond sda 500 8.10 uefi`) — 별칭은 비워도 되고 `\|` 를 생략해도 됨. 「신규 설치 양식」 참고 | 그 번호는 쓰지 않음 (화면에서 `+` 로 만든 user 별 템플릿은 그대로 쓸 수 있음) |
| `work_os6_over` | (v0.8.0, 선택) gossh 작업(`s`)을 os8 gossh 로 실행하는 **최대 대수**. 초과하면 전량 os6_mgmt 경유 (「gossh 작업 기록」 참고) | `1000` (숫자가 아니면 1000 으로 처리하고 화면에 경고) |

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

(위 9개가 처음 정의된 변수이고 `awx_profile_*`·`enable_*`·`*_engine`·`newhost_template_*`·`work_os6_over` 도 같은 방식으로 `main.go` 에 빈 문자열로 있습니다 — 전체 32개 목록은 `conf/vars.manifest`.)

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

# 2. 빌드만 (테스트용) — 회사 폐쇄망 빌드 서버는 Go 1.26.5
go build -o auto_setup .

# 3. 빌드 + /usr/local/bin 설치 + cron(매분 ensure) + tmpfiles 등록 + 변경 엔진 2개·conf 샘플 (root, 멱등)
bash setup.sh

# 4. os6 용 빌드 (Go 1.20 서버 .60 에서, 정적 바이너리 — Go 1.20 문법만 쓰므로 1.26.5 와 1.20 양쪽에서 빌드됨)
bash build_os6.sh
# → auto_setup_os6 생성 (os6_mgmt 의 os6_autosetup 경로에 'auto_setup' 이름으로 배치, probe 용)
# (v0.7.0) 같은 실행에서 ../ip_change/bin/ip-change-engine_os6, ../ldap_setting/bin/ldap-config-engine_os6 도 빌드 (소스가 있을 때)
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
- **v0.7.0 업데이트**: `bash conf/update_v0.7.0.sh` (미리보기 `--dry`, 되돌리기 `--undo`). 새 변수는 선택 항목 `enable_ip_change`, `enable_ldap_change`, `ip_engine`, `ldap_engine` 4개이며 모두 빈 값(꺼짐)으로 추가됩니다. 앵커는 `ldap_share_dir=` 줄입니다.
- **v0.8.0 업데이트**: `bash conf/update_v0.8.0.sh` (미리보기 `--dry`, 되돌리기 `--undo`). 새 변수는 선택 항목 `newhost_template_1`~`9`, `work_os6_over` **10개**이며 모두 빈 값으로 추가됩니다 (기존 값은 바꾸지 않음). 앵커는 v0.7.0 의 마지막 변수 `ldap_engine=` 줄이므로 **v0.7.0 업데이트가 먼저 적용된 conf** 여야 합니다 (더 오래된 사본은 v0.6.0 → v0.7.0 → v0.8.0 순서로 적용). 앵커가 없으면 파일을 건드리지 않고 중단합니다.

**v0.8.0 적용 순서 (폐쇄망)**

```bash
# 1. 새 버전 폴더를 기존 폴더 위에 덮어쓰기 (conf/auto_setup.conf 는 저장소에 없으므로 보존됨)
# 2. (os8_mgmt) conf 에 새 빈 변수 10개 추가
bash conf/update_v0.8.0.sh --dry     # 미리보기 (파일 불변)
bash conf/update_v0.8.0.sh           # 실행 — 변수 10개 질문에는 Enter (빈 값 유지)
# 3. (os8_mgmt) 재빌드·설치 후 데몬 재기동 (실행 중인 데몬은 옛 바이너리)
bash setup.sh
auto_setup --restart
# 4. (os6_mgmt) 저장소의 새 auto_setup_os6 를 os6_autosetup 경로에 'auto_setup' 이름으로 교체
#    (원격 클라이언트·probe 가 이 바이너리를 씀)
# 5. (awx_script 가 있는 서버) 01 의 DNS 점검 — 현장 01 은 0.6.1 이상이어야 하며 현장 값·주석은 보존됨
bash <awx_script>/setup/update_unreleased_dns.sh --dry
bash <awx_script>/setup/update_unreleased_dns.sh
# 되돌리기: bash conf/update_v0.8.0.sh --undo  /  bash <awx_script>/setup/update_unreleased_dns.sh --undo
```

- v0.8.0 의 새 동작(`h`·`w` 메뉴, `s`, 신규 설치 양식, 60초 제한, `bash -c`)은 **바이너리**에 있으므로 3·4 단계가 필수입니다. conf 업데이트는 새 빈 변수만 추가합니다.
- awx_script 의 업데이트는 `[Unreleased]` 항목이라 버전 이름 대신 `update_unreleased_dns.sh` 입니다 (`01.AWX_nodeinfo_V2.sh` 만 변경, 02 는 변경 없음).
- 업데이트 뒤 설정 변경 확인은 아래 「주의사항 (Disclaimer)」 을 따르십시오.
- v0.4.1 은 설정 파일 변경이 없습니다(바이너리만 변경, `update_v0.4.0.sh` 가 최신 업데이트 스크립트). v0.4.0 의 설정 파일 변경은 `os6_mgmt` 위 안내 주석 1줄뿐이고 신규 변수는 없습니다. 동작 변경은 바이너리에 있으므로 3·4 단계가 필수입니다.

### setup.sh 수행 내용

1. `go build -o auto_setup .` (main.go 포함 전체 빌드). `conf/auto_setup.conf` 가 있으면 `/etc/auto_setup/auto_setup.conf` 로 설치 (기존 파일을 덮어씀)
2. `install -m 0755 auto_setup /usr/local/bin/auto_setup`
3. crontab 에 `* * * * * /usr/local/bin/auto_setup ensure >/dev/null 2>&1` 등록 (중복 방지)
4. `/etc/tmpfiles.d/auto_setup.conf` 에 `x /tmp/auto_setup` 등록 (무기한 감시 작업이 tmp 정리로 지워지지 않도록)
5. (v0.7.0) `ip-change-engine`·`ldap-config-engine` 을 `GOPROXY=off CGO_ENABLED=0` 으로 빌드해 `/usr/local/bin` 에 설치 (소스가 없으면 건너뜀)
6. (v0.7.0) `ip_change.conf`·`ldap_config.conf`(600)·`assets.txt` 샘플을 `/etc/auto_setup/` 에 복사 — 이미 있는 파일은 덮어쓰지 않음

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

각 행의 마지막 칸은 흐름도입니다 (`docs/svg/`, 목록은 [`docs/svg/README.md`](docs/svg/README.md)).

| 명령 | 동작 | 흐름도 |
|---|---|---|
| (인자 없음) | 현재 상태 리포트. tty 면 TUI, 아니면 텍스트 표 | [cli_report](docs/svg/cli_report.svg) |
| `--plain` | 텍스트 표로 출력 (색 없음) | [cli_report](docs/svg/cli_report.svg) |
| `--admin` | TUI 의 변경 메뉴(`m`)를 conf 와 상관없이 켬 (TUI 전용, 데몬·`--plain`·`request` 에는 영향 없음) | [cli_report](docs/svg/cli_report.svg) |
| `-user <이름>` | (v0.7.3) 지정한 user 의 job 만 보이고 `x`·`w`·`m` 의 user 선택을 건너뜀. v0.8.0 부터 `s`·신규 설치 양식도 이 user 를 씀 (`--admin`·`--plain`·`newhost`·`s` 와 함께 쓸 수 있음, `-user=<이름>` 형식도 허용). 이름은 영문·숫자·`.` `_` `-` 만, `.` 와 `..` 는 거부 | [cli_report](docs/svg/cli_report.svg) |
| `-h`, `--help` | 사용법 출력 | |
| `--start` \| `start` | 데몬 기동 (이미 실행 중이면 안내) — os8_mgmt 에서만 | [cli_daemon_ctl](docs/svg/cli_daemon_ctl.svg) |
| `--stop` \| `stop` | 데몬 종료 (pid 확인 후 SIGTERM, 대기) — os8_mgmt 에서만 | [cli_daemon_ctl](docs/svg/cli_daemon_ctl.svg) |
| `--restart` \| `restart` | 데몬 재기동 — os8_mgmt 에서만 | [cli_daemon_ctl](docs/svg/cli_daemon_ctl.svg) |
| `status` | 작업별 진행 상태 (jobid, user, 호스트 상태별 대수, 남은 호스트 가로) | [cli_status](docs/svg/cli_status.svg) |
| `snapshot` | 상태 스냅샷(JSON) 출력 (TUI·원격 클라이언트가 쓰는 내용) | [cli_snapshot](docs/svg/cli_snapshot.svg) |
| `code NNNN` | code 에 해당하는 결과 코멘트 출력 (로그 `run 완료: … code NNNN` 또는 TUI 의 최근 run 에 나오는 4자리) | [cli_code](docs/svg/cli_code.svg) |
| `cancel <jobid>` | 무기한 감시 중인 작업 종료 (status 에서 확인) | [cli_cancel](docs/svg/cli_cancel.svg) |
| `done <host...>` | 호스트를 수동 완료 처리 (출처 manual, 부팅 시각 검증 없음) | [cli_done](docs/svg/cli_done.svg) |
| `done --job <jobid> [--yml <그룹>]` | job(또는 그 그룹)의 미완료 호스트 전부를 수동 완료 처리 (완료된 호스트는 건드리지 않음, 진행 중 job 만) | [cli_done](docs/svg/cli_done.svg) |
| `request manual-run <jobid> <yml> [check]` | 수동 실행 요청 (완료 여부와 상관없이 그룹 전체를 시도, 접속불가 호스트는 로그에 기록). 기본 = 설정체크 + 설정수정(TUI `h`→`c`), `check` = 설정체크만·환경설정 수정 안 함(TUI `h`→`t`) | [cli_request_run](docs/svg/cli_request_run.svg) |
| `request cancel <jobid>` | 작업 종료 요청 | [cli_request_other](docs/svg/cli_request_other.svg) |
| `request refresh` | 데몬에 즉시 ping·준비확인 요청 (TUI `r` 키와 같음, 5초 안에 수거) | [cli_request_other](docs/svg/cli_request_other.svg) |
| `request recheck <jobid> <yml 또는 *>` | 완료 제외 호스트를 os8_mgmt → 안 되면 os6_mgmt 순으로 직접 재확인 (TUI `h`→`g` 와 같음, 5초 안에 수거) | [cli_request_recheck](docs/svg/cli_request_recheck.svg) |
| `daemon` | 포그라운드 데몬 루프 (보통 ensure 가 백그라운드로 기동) — os8_mgmt 에서만 | [cli_ensure_daemon](docs/svg/cli_ensure_daemon.svg) |
| `ensure` | 데몬이 없으면 백그라운드 기동 (cron 매분) — os8_mgmt 에서만 | [cli_ensure_daemon](docs/svg/cli_ensure_daemon.svg) |
| `probe [-i 10s]` | (os6_mgmt 쪽) stdin 호스트 목록을 받고 ping 상태 출력 (`host up\|down`) | [cli_probe](docs/svg/cli_probe.svg) |
| `newhost [-user <이름>]` | (v0.8.0) 신규 설치 양식 — hostname ip mac → `awx_dir/<user>.txt`. TUI `w`→2 와 같은 화면 (tty·os8_mgmt 에서만, 「신규 설치 양식」 참고) | [cli_newhost](docs/svg/cli_newhost.svg) |
| `s [-user <이름>]` | (v0.8.0) gossh 작업 기록 — 붙여넣은 호스트를 `/tmp/<3자리>` 로 만들어 gossh 실행·user 별 기록. TUI `s` 와 같은 화면 (tty 에서만, 「gossh 작업 기록」 참고) | [cli_s](docs/svg/cli_s.svg) |
| 그 외 | 사용법을 stderr 로 출력 후 exit 1 | |

(내부용 `work-relay` 는 os6_mgmt 의 gossh 작업 기록을 os8_mgmt 에 저장·조회하는 하위명령이며 직접 쓰지 않습니다.)

기존 하위명령(`daemon/ensure/code/status/cancel/probe`)은 그대로이며 cron·setup.sh 와 호환됩니다.

### 상태 TUI (무인자 실행)

```
auto_setup            # tty 면 TUI
auto_setup --plain    # 텍스트 표
NO_COLOR=1 auto_setup # 색 없는 TUI
```

**키** — 메뉴 구조 (v0.8.0). 흐름도는 마지막 칸

| 화면 | 키 | 동작 | 흐름도 |
|---|---|---|---|
| 1 (작업·그룹) | ↑ ↓ (k j) | 그룹 행 이동 (`PgUp`/`PgDn` 은 한 화면씩) | [PgUp/PgDn](docs/svg/tui_pgupdn.svg) |
| 1 | ← → | 이전/다음 작업(job) 으로 이동 (맨 위 날짜 줄에서는 전날/다음날) | |
| 1 | Enter | 선택한 그룹의 호스트표(화면 2) | |
| 1 | a | 선택한 job 의 **모든 그룹(yml) 호스트를 한 표로** (그룹(yml) 열 표시, 수동 실행은 그룹별 화면에서) | [a](docs/svg/tui_a.svg) |
| 1 | q / Esc | 종료 | |
| 2 (호스트표) | ↑ ↓ (k j) · `PgUp`/`PgDn` | 호스트 행 이동 (`PgUp`/`PgDn` 은 한 화면씩) | [PgUp/PgDn](docs/svg/tui_pgupdn.svg) |
| 2 | ← / Esc / q | 화면 1 로 복귀 | |
| 2 | f | 정체·실패 호스트만 보기 (토글) | [f](docs/svg/tui_f.svg) |
| 2 | a | 그룹별 화면 ↔ 전체 보기 전환 (전체 보기에서 a 는 화면 1 로) | [a](docs/svg/tui_a.svg) |
| 2 | **h** | **호스트 작업 메뉴** — 안내 줄 아래에 인라인으로 열림 (` 호스트 작업 ▶ c 체크+수정  t 체크만  g 재확인  d 수동 완료  v 결과`). 글자 또는 번호(1~5)로 선택, `Esc`/`q`/`h` 로 메뉴만 닫음, 그 밖의 키는 무시. 전체 보기(`a`)에서는 `g`·`d` 만 표시 | [h](docs/svg/tui_h_menu.svg) |
| 2 | h → **c** (1) | OS 체크 수동 실행(이중체크): **설정체크 + 설정수정**. **완료 여부와 상관없이** 그룹 전체를 시도, y/n 확인 후 요청 (접속불가·미응답 호스트는 결과 wall 에 따로 표시) | [h→c](docs/svg/tui_h_c.svg) |
| 2 | h → **t** (2) | **설정체크만** (환경설정 수정 안 함), y/n 확인 후 요청 | [h→t](docs/svg/tui_h_t.svg) |
| 2 | h → **g** (3) | 서버 상태 재확인 — 완료 제외 호스트를 os8 → os6_mgmt → 접속불가 순으로 직접 확인, 진행 창 표시 (「os8_mgmt 에서 접속이 안 되는 호스트」 참고) | [h→g](docs/svg/tui_h_g.svg) |
| 2 | h → **d** (4) | (v0.7.1) 수동 완료. 안내 줄 아래에서 `1` 이 호스트 / `2` 그룹 미완료 전체 선택 → `y` 확인 (`Esc` 취소). 부팅 시각 검증 없이 완료로 인정하고 체크스크립트는 실행하지 않음 (CLI `auto_setup done` 과 같음). 전체 보기에서는 `1` 만 | [h→d](docs/svg/tui_h_d.svg) |
| 2 | h → **v** (5) | 이 그룹의 가장 최근 수동 실행 결과 다시 보기 | [h→v](docs/svg/tui_h_v.svg) |
| 공통 | r | 바로 새로고침 + 데몬에 즉시 ping·준비확인 요청 (자동 갱신: 로컬 2초, 원격 10초. 연타는 5초에 1회만 요청) | [r](docs/svg/tui_r.svg) |
| 공통 | **w** | **AWX 메뉴** — ` AWX ▶ 1 AWX 실행  2 신규 설치 양식`. `w` 를 한 번 더(`ww`)·`Enter`·`1` 이 예전의 AWX 실행, `2` 가 신규 설치 양식. `Esc`/`q` 로 메뉴만 닫음 | [w](docs/svg/tui_w_menu.svg) |
| 공통 | w → **1** | AWX 실행 (awx_dir 의 01 을 이 터미널에서 실행, 프로파일 선택) — 「체크스크립트 단독 실행(x) · AWX 실행(w)」 참고 | [w→1](docs/svg/tui_w_1.svg) |
| 공통 | w → **2** | (v0.8.0) 신규 설치 양식 — os8_mgmt 에서만, 「신규 설치 양식」 참고 | [w→2](docs/svg/tui_w_2.svg) |
| 공통 | m | 변경 작업 메뉴 (IP 변경·LDAP 변경·IP+LDAP·원복·auto.appl 원복). **현재 화면 아래 안내 줄 밑에 인라인으로 열림**(v0.7.1) — 번호 선택, `Esc`/`m`/`q` 는 메뉴만 닫음. conf `enable_ip_change`/`enable_ldap_change` 또는 `--admin` 이 있을 때만, os8_mgmt 에서만 실행 — 「변경 작업」 참고 | [m](docs/svg/tui_m.svg) |
| 공통 | s | (v0.8.0) gossh 작업 기록 — 메뉴 없이 작업 화면이 바로 열림. 「gossh 작업 기록」 참고 | [s](docs/svg/tui_s.svg) |
| 공통 | x | 체크스크립트 단독 실행 (user·호스트 입력 → t/c, 기록 안 남김) — 아래 참고 | [x](docs/svg/tui_x.svg) |
| 공통 | Ctrl+C / Ctrl+Z | 무시 (종료는 q) | |
| 공통 | ? | 도움말 (아무 키나 누르면 닫힘) | [?](docs/svg/tui_help.svg) |

- **안내 줄 (v0.8.0)**: 화면 2 는 첫 줄 ` ↑↓ 이동  ← 목록  f 정체·실패만  a 전체 그룹  r 새로고침  ? 도움말  q 복귀`, 둘째 줄 ` h 호스트 작업  w AWX·신규설치  m 변경  s gossh 작업  x 단독 체크` (`m 변경` 은 켜졌을 때만, 화면 1 은 `h` 가 없음). 화면 1 은 ` ↑↓ 이동  ←→ 작업 전환  Enter 상세  a 전체 보기  r 새로고침  ? 도움말  q 종료` 에 같은 둘째 줄. 60칸 이상에서는 `…` 없이 항목 단위로 줄바꿈합니다.
- **숨은 단축키**: 예전의 `c` `t` `g` `v` `d` (화면 2) 와 `x` 는 **그대로 동작**합니다 — `h` 메뉴는 같은 핸들러를 호출하는 것뿐입니다. 안내 줄에서만 빠졌고 `?` 도움말에 표시됩니다. 손에 익은 키를 그대로 쓰면 됩니다.
- **`w` 가 메뉴로 바뀐 것** (v0.8.0): 예전에 `w` 한 번으로 AWX 를 실행하던 동작은 `ww` (또는 `w` `Enter`) 입니다.

- 집합(행 단위) = 01 이 전달한 **그룹 yml** 입니다. 그룹 줄이 없는(구버전 01 또는 그룹 1개) 작업은 `(all)` 한 행이며, 어느 그룹에도 없는 호스트는 `(기타)` 로 모읍니다. 전체 yml(`all=`)은 작업 제목에 표시됩니다. 화면 상단에 진행 중 작업 합계(총계)가 나옵니다.
- **색**: 완료 초록 / 진행 청록 / 대기 회색 / 경고 노랑 / 정체·실패 빨강(굵게). `NO_COLOR` 설정, tty 가 아님(파이프), `--plain` 이면 색을 쓰지 않습니다.
- **정체 기준**: seen_down(설치 시작) 후 **60분까지는 "설치중"**(RHEL7 은 1시간 걸림), 그 이후에도 READY 가 안 되면 빨간 "정체"(`installStuck` = 60분, 경계값은 아직 설치중). 화면 표시만 하며 wall 알림은 없습니다.
- os6_mgmt 에서도 동일하게 열리며 gossh 로 snapshot 을 10초 간격으로 폴링합니다 (v0.6.0 부터, 이전 5초). 키 입력(수동 실행)은 요청 파일을 os8_mgmt 에 남깁니다(데몬이 5초 주기로 수거).
- 수동 실행은 데몬 run 큐에서 실행됩니다(동시 1개 유지, code·wall 동일). 거부되면 `requests/rejected/<파일>` 의 마지막 `reason=` 줄에 사유가 남습니다.
- **정체 호스트 수동 재시도**: 정체 상태인데 실제로는 접속 가능한 호스트는 `r` 을 누르면 ping 상태와 무관하게 즉시 다시 확인합니다 (로컬 무응답이면 os6_mgmt 경유로도 확인).
- **anaconda 감지 (v0.7.1)**: 준비확인에서 anaconda 가 보이면 ping X 를 못 봤어도(기동 전에 이미 내려갔다 올라온 호스트) 설치중으로 올립니다 (이전에는 대기). `g` 재확인에서는 `단계 대기 → 설치중` 으로 표시됩니다. 종료된 job 은 단계가 바뀌지 않습니다.
- **수동 완료 `d` (v0.7.1)**: 화면 2 에서 `h`→`d` (또는 `d`) → 안내 줄 아래 `수동 완료 ▶ 1 이 호스트(<호스트명>)  2 그룹 미완료 N대   (Esc 취소)` → 번호 → 확인 줄에서 `y`. 로컬 데몬이면 `markDone`, os6_mgmt 클라이언트면 `done` 을 os8_mgmt 로 중계합니다. 이미 완료된 호스트는 호스트 선택지가 없고, 종료된 작업은 안내만 합니다. 정말 끝난 호스트에만 사용하세요.
- **수동 실행 후 완료 처리**: 수동 실행이 정상 종료되어 결과가 나온 호스트는 현재 단계(정체·설치중 등)와 상관없이 완료로 바뀝니다.
- **수동 실행 전 경로 재확인**: 수동 실행 직전에 호스트를 한 번 확인해, os8 에서 직접 응답하면 local, os8 무응답이고 os6_mgmt 경유로만 응답하면 os6 로 맞춘 뒤 실행합니다 (`os6_mgmt`·`os6_gossh` 설정 시).
- **% (진행률) = 완료 대수 / 전체** 입니다. 배포중·설치중·부팅확인·체크중 호스트는 막대의 `+`(청록)와 `진행 N` 으로만 보이고 % 는 0% 그대로이다가, 호스트가 **완료**될 때 오릅니다. 설치 중 0% 는 정상입니다.

### 체크스크립트 단독 실행(x) · AWX 실행(w → 1) (v0.6.0)

화면 1·2 어디서나 쓸 수 있습니다. `x` 는 TUI 안에서 끝나고, AWX 실행은 실행하는 동안 TUI 를 잠시 내려놓고 이 터미널에서 01 을 직접 실행합니다. v0.8.0 부터 `w` 는 메뉴(`1` AWX 실행 · `2` 신규 설치 양식)이며 **`ww` / `w` `Enter` / `w` `1` 이 예전의 `w` 와 같습니다**. 흐름도: [x](docs/svg/tui_x.svg) · [w→1](docs/svg/tui_w_1.svg)

| 키 | 화면 | 동작 |
|---|---|---|
| `x` | 어디서나 | 체크스크립트 단독 실행 시작 (user 선택부터) |
| `w` → `1` (`ww`·`w` Enter) | 어디서나 | AWX 실행 시작 (`awx_dir` 의 `01.AWX_nodeinfo_V2.sh`) |
| `t` / `c` | x 의 모드 선택 | `t` 설정체크만 / `c` 설정체크 + 설정수정 (선택 즉시 실행) |
| `Ctrl+D` | x 호스트 입력 | 호스트 입력 완료 (공백·쉼표·탭·`\|` 는 줄바꿈, 중복 제거) |
| `Enter` / `Esc` | x 입력 | 줄바꿈 / 취소 (`q` 는 입력창이 비었을 때만 취소) |
| `Ctrl+X` → `y` | x 실행 중 | 실행 취소 (프로세스 그룹 종료). `n` 이면 계속 |
| `Ctrl+X` | AWX 실행 중 | **즉시** 취소 (01 이 SIGINT 를 받음) |
| `y` / `n` / `1`~`9` | AWX 실행 질문 | auto 실행 여부 / 프로파일 번호 선택 (Esc 뒤로) |
| `q` / `Esc` | x 실행 중 | 무시 (결과가 나온 뒤 결과 창에서 q/Esc 로 닫기) |
| `↑` `↓` `Space` `b` | x 결과 창 | 스크롤 / 쪽 이동 (q/Esc 닫기) |

**x (체크스크립트 단독 실행)**

- **user 선택**: awx_dir 의 01 에서 `user_route` 를 읽어 `info_mn.sh` 메뉴를 보여 주고, 번호를 입력하면 `info.sh` 결과가 user 가 됩니다. 메뉴를 찾지 못하면 user 이름을 직접 입력합니다(영문·숫자·`.` `_` `-` 만).
- **호스트**: 여러 줄로 붙여넣고 `Ctrl+D`. 0대면 다시 입력합니다.
- **모드**: `t`(설정체크만) 또는 `c`(설정체크 + 설정수정, 빨강 표시). 고르면 바로 실행합니다.
- **실행**: 임시 디렉터리에 `targets.txt` 를 만들고 `os_check_sh` 를 `-auto-check` 또는 `-auto` 로 호출합니다. 경과 시간과 로그 끝 15줄을 1초마다 갱신합니다.
- **결과**: 기존 결과 창과 같은 화면입니다. 결과 창을 닫으면 임시 디렉터리를 지웁니다. auto_setup.log·codes·runs 에는 아무것도 남기지 않습니다(일회성). `os_check_sh` 가 비어 있으면 안내만 합니다.

**w → 1 (AWX 실행)**

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

**모의 테스트로 확인**: `bash test/demo_flow.sh` 로 모의 환경을 만들고(데모 conf 는 awx_profile_1·2 가 정상, 3 은 일부러 틀린 예), `source /tmp/as_demo_flow/env.sh` 후 `auto_setup` 을 실행해 `x` 로 127.0.0.11/12/14 를 붙여넣고 `t` 또는 `c`, `ww` 로 auto `y` → 프로파일 1 을 골라 보고 `Ctrl+X` 취소도 눌러 보십시오. 끝나면 `bash test/demo_flow.sh --stop`.

### 세션 user (v0.8.0)

`auto_setup -user <이름>` 으로 실행하면 그 user 를 씁니다 (v0.7.3 그대로, 헤더에 `[user: <이름>]`). 옵션 없이 실행했으면 **gossh 작업(`s`)·신규 설치 양식(`w`→2)을 처음 열 때** `이 세션에서 사용할 user` 를 한 번 고르게 하고(`x` 와 같은 user 메뉴: 01 의 `user_route` → `info_mn.sh` 번호 또는 이름 직접 입력) **TUI 를 닫을 때까지 기억**합니다.

- CLI `auto_setup s` / `auto_setup newhost` 는 시작할 때 묻습니다 (번호 또는 이름, 3번까지). `-user` 가 있으면 묻지 않습니다.
- 체크스크립트 단독 실행(`x`)·AWX 실행(`w`→1)·변경 작업(`m`)은 **기존처럼 매번 묻습니다** (`-user` 가 있으면 건너뜀). 단 신규 설치 양식에서 AWX 실행으로 이어질 때는 같은 user 로 자동 진행합니다.
- 세션 user 는 job 을 걸러 내지 않습니다 (job 필터링은 `-user` 만).
- `-user .`·`-user ..` 는 거부됩니다 (user 이름이 `newhost/<user>`·`work/<user>` 디렉터리 이름으로 쓰임).

### 신규 설치 양식 (w→2 / `auto_setup newhost`, v0.8.0)

nodeinfo 를 쓸 수 없는 **신규 설치 서버**용입니다. 엔지니어에게 받은 `hostname ip mac` 만으로 01 의 12필드 양식(`awx_dir/<user>.txt`)을 만들고, 이어서 AWX(01)를 실행할 수 있습니다 (**nodeinfo 는 항상 N**). 흐름도: [w→2](docs/svg/tui_w_2.svg) · [newhost](docs/svg/cli_newhost.svg)

- **실행 조건**: **os8_mgmt 에서만** (os6_mgmt 원격 클라이언트는 `신규 설치 양식은 os8_mgmt 에서 실행하세요` 안내만), `awx_dir` 가 있어야 함, CLI 는 tty 필요. TUI 에서는 `w`→`2`, 밖에서는 `auto_setup newhost [-user <이름>]` (같은 화면. 저장했으면 종료코드 0, 저장 없이 끝나면 1).
- **user**: `-user` 가 있으면 그 user, 없으면 「세션 user」.
- 모든 단계에서 `Esc` = 이전 단계(첫 단계는 닫기), `Ctrl+X` → `y` = 취소(저장 안 함). `Ctrl+C`·`Ctrl+Z` 는 무시.

**5단계**

| 단계 | 내용 |
|---|---|
| 1/5 붙여넣기 | `hostname ip mac` 을 한 줄에 한 대. 구분자는 공백·탭·쉼표, 순서가 달라도 IP·MAC 패턴으로 구분합니다. MAC 은 `aa:bb:..`·`aa-bb-..`·`aabb.ccdd.eeff`·구분자 없는 12자리를 받아 **소문자 `aa:bb:cc:dd:ee:ff`** 로 바꿉니다. 빈 줄·`.`·`Ctrl+D` 로 끝. 형식 오류와 목록 안의 hostname·IP·MAC 중복은 빨갛게 표시되고 **고치기 전에는 다음으로 넘어가지 않습니다** — `d 3`·`d 3-5,8` 로 지우거나 `e 3` 으로 다시 입력, 빈 줄에서 `Backspace` 는 마지막 줄을 꺼내 고칩니다 |
| 2/5 템플릿 지정 | 아래 「템플릿」·「지정」 참고. 값이 없는 행(빨강)이 있으면 다음으로 넘어가지 않습니다 |
| 3/5 DNS 점검 | 「DNS 점검」. 백그라운드로 돌고 `Esc` 로 멈출 수 있습니다. 불일치가 있으면 표 + `a` 모두 DNS 로 / `n` 그대로 / `s` 하나씩 / `q` 중단. 불일치가 없거나 DNS 를 쓸 수 없으면 바로 4단계. `a`·`s` 로 바꾼 IP 는 `auto_setup.log` 에 `newhost user=<이름> …` 로 남습니다 |
| 4/5 미리보기 | 12필드 줄을 템플릿별로 묶어 보여 줍니다 (`추천` 행·DNS 로 바꾼 행은 색 표시). `y` 로 저장, `Esc` 는 템플릿 지정으로 |
| 5/5 저장 확인 | 저장된 내용을 다시 읽어 보여 주고 `AWX 실행할까요? [y/n]`. `n` 이면 저장만 완료 (나중에 `w`→1) |

**템플릿** — 한 줄 형식 `별칭|infra nic disk 용량 os boot` (예 `A|sec bond sda 500 8.10 uefi`)

```
1 [A]   tmp tmp sec <hostname> <ip> <mac> bond sda sda5       500 8.10 uefi   (sec-bond-sda-500-8.10-uefi)
2 [mec] tmp tmp mec <hostname> <ip> <mac> eth0 sda sda5       500 9.6  uefi   (mec-eth0-sda-500-9.6-uefi)
3       tmp tmp mec <hostname> <ip> <mac> eth0 nvme nvme0n1p8 400 9.6  uefi   (mec-eth0-nvme-400-9.6-uefi)
```

- 저장되는 줄은 `tmp tmp infra hostname ip mac nic disk part 용량 os boot` (01 의 12필드). `1.1T→1200` 변환·MAC 짝수 보정·infra 별칭은 01 이 하므로 여기서는 하지 않습니다.
- 이름은 값으로 자동 생성됩니다 (`sec-bond-sda-500-8.10-uefi`). 짧은 별칭(`A`, `mec` 등)은 선택입니다. **root 파티션은 disk 로 고정**: `sda` → `sda5`, `nvme` → `nvme0n1p8`.
- 출처 두 곳: **conf 공용** `newhost_template_1`~`9` + **user 별** `<AUTO_SETUP_DIR>/newhost/<user>/templates` (화면에서 `+ infra nic disk 용량 os boot [별칭]` 으로 추가하면 자동 저장되어 다음에도 보임. 값이 겹치면 거부).
- 값 검사: disk 는 `sda`/`nvme` 만, 용량은 숫자만, boot 는 `uefi`/`legacy`/`레거시` 만 (01 이 `레거시` 를 legacy 로 바꾸므로 둘 다 허용), infra·nic·os 는 공백만 불가. 틀린 conf 템플릿은 목록에서 빠지고 사유가 표시됩니다.

**지정 (미니 언어)** — 표 아래 한 줄에 적고 Enter

```
svr001-020=1  svr021-023=mec  svr024-043=3
```

- 왼쪽(대상): 범위 `svr001-020`, 패턴 `spice*`, 행 번호 `1-20`, 전체 `*`. 오른쪽(값): 번호·별칭·자동 이름 무엇이든. **나중 지정이 앞 지정을 덮어씁니다.**
- **개별 수정**: `svr005 os=9.6`, `svr001-005 nic=eth0` 처럼 열 하나만 바꿉니다.
- 한 줄 전체를 먼저 검사하고 하나라도 틀리면 **아무것도 바꾸지 않고** 입력을 되돌려 줍니다.
- **지정이 없는 행**은 이력 추천값을 노란색 `추천` 으로 채웁니다. 이력 = 이 기능으로 만든 과거 양식(`newhost/<user>/history`) + `awx_dir` 의 `${user}.txt` 백업(모든 user). hostname 에서 숫자를 뺀 **접두어가 가장 길게 일치하는 가장 최근 기록**을 고릅니다 (읽기 상한: 최근 200개 파일, 파일당 256KB).

**저장과 AWX 연결**

- 저장 위치 `awx_dir/<user>.txt`. 기존 파일은 `<AUTO_SETUP_DIR>/newhost/<user>/backup/<시각>.txt` 로 백업하고(실패하면 아무것도 바꾸지 않음), 같은 내용을 `history/<시각>.txt` 에도 남깁니다. 임시 파일에 쓰고 rename 하며 권한은 기존 파일을 따릅니다 (새 파일은 0644 — 01 이 읽어야 함). 이력 사본만 못 남기면 저장은 유지하고 경고합니다. `newhost/<user>/` 는 디렉터리 0700·파일 0600 이며 자동 삭제하지 않습니다.
- 5/5 에서 `y` → 기존 AWX 실행 흐름: 프로파일을 쓸지 먼저 묻고(`AWX auto 실행? [y/n]`), **`y` 로 프로파일을 고르면 nodeinfo 는 프로파일 값과 상관없이 N 으로 강제**(`AWX_AUTO_NODEINFO=n`), **`n`(프로파일 없이 일반 실행)이면 자동 N 을 적용하지 않고** 01 의 nodeinfo 질문을 직접 응답합니다. user 는 양식의 user(`AWX_USER`), **대상 서버 목록은 방금 만든 hostname 들로 자동 입력**(`AWX_VERIFY_FILE`). DNS 를 실제로 점검했으면 `AWX_DNS_CHECKED=1` 을 넘겨 01 이 같은 점검을 되풀이하지 않고 표만 출력합니다.
- **프로파일을 고르지 않거나 하나도 없으면** `AWX_AUTO`·`AWX_AUTO_NODEINFO` 를 넘기지 않는 일반 실행입니다 — nodeinfo 를 쓸 수 있도록 01 의 모든 질문(nodeinfo·작업진행·인벤토리 소스·OS 버전 등)에 직접 응답합니다. user·대상 목록·`AWX_DNS_CHECKED` 만 자동입니다.

### DNS 점검 (v0.8.0)

양식의 hostname·IP 가 DNS 와 맞는지 확인합니다. **같은 규칙을 두 곳**에서 씁니다 — auto_setup(Go, `dnscheck.go`, 신규 설치 양식 3/5 단계)과 awx_script 01(bash, `[4-1] dns_check`, `${user}.txt` 의 hostname(4번째)·IP(5번째) — `[4] fix_mac` 다음, `[5]` 대상 출력 전).

- **기준값**: `/etc/resolv.conf` 의 각 DNS 서버(최대 3)에 **직접** A 레코드를 묻고 그 답을 기준으로 합니다. `/etc/hosts`·`nsswitch`·`getent` 는 쓰지 않습니다. 짧은 이름은 `search`/`domain`/`ndots` 를 glibc·nslookup 과 같은 순서로 붙여 조회합니다. Go 쪽은 외부 명령 없이 표준 라이브러리만으로 UDP(잘림 시 TCP) 질의를 직접 구현했습니다.
- **판정** (호스트마다 5종)
  - **일치**: 응답한 서버들의 답이 같고 IP 가 1개이며 등록 IP 와 같음
  - **불일치**: 합의된 기준 IP ≠ 등록 IP — 표에 나오고 교체를 고를 수 있음
  - **판단 불가**: 서버끼리 답이 다르거나 A 레코드가 여러 개 — 경고만, 자동 교체 대상에서 뺌
  - **미등록**: 응답한 서버가 모두 NXDOMAIN — `미등록` 표시만 하고 점검에서 뺌
  - **조회 실패**: 모든 서버가 응답하지 않음 — 그 호스트는 판단 불가
- **DNS 사용 가능 여부**: 목록 중 1대라도 어느 서버에서든 답을 받으면 점검합니다. 전부 실패하면 `DNS 조회 불가 — 점검 생략` 한 줄만 출력하고 기존대로 진행합니다 (01 은 `dig`·`nslookup` 둘 다 없으면 `DNS 도구 없음 — 점검 생략`).
- **중복 경고**: 불일치 호스트의 **등록 IP** 가 다른 hostname 의 것으로 보이면 ⚠ 를 덧붙입니다 — `등록IP 1.1.1.1 은 svr0005 의 IP (목록|DNS|PTR)`. ① 목록 안의 다른 hostname 이 같은 IP 를 씀(목록) ② 목록 안 다른 호스트의 DNS 기준 IP 가 그 IP(DNS) ③ 그 IP 의 역조회(PTR)가 다른 hostname 을 가리킴(PTR, 불일치 IP 만 질의).
- **출력과 선택**
  ```
  hostname  등록IP   DNS IP   비고
  svr0001   1.1.1.1  1.1.1.2  ⚠ 등록IP 1.1.1.1 은 svr0005 의 IP (DNS)
  a 모두 DNS 로 / n 그대로 / s 하나씩 / q 중단 :
  ```
  `a` 불일치 전부 DNS IP 로 교체 · `n` 등록값 그대로 · `s` 호스트마다 `y/n` · `q` 중단. 교체는 IP(5번째 필드)만 바꾸고 01 은 LOG 에 `[DNS] svr0001 1.1.1.1 -> 1.1.1.2` 로 남깁니다.
- **속도**: 동시 32개, 질의당 2초·재시도 1회, 응답을 한 번도 못 받고 3번 실패한 서버는 이후 질의를 건너뜁니다. 01(bash)은 1000대·2코어에서 약 11초 (랩 측정).
- **01 의 auto 모드 (`AWX_AUTO=1`)**: stdin 이 `/dev/null` 이라 질문은 **`/dev/tty`** 로 받습니다. 불일치가 나오면 표를 출력하고 `등록값 그대로 계속 진행할까요? [y/N]` 을 묻습니다 — `y` 면 진행, 그 외이거나 터미널이 없어 물을 수 없으면 **중단**합니다. auto_setup 양식 생성에서 이미 점검·결정했으면(`AWX_DNS_CHECKED=1`) 표만 출력하고 묻지 않고 진행합니다.
- 01 은 `AWX_AUTO_NODEINFO`(y|n)를 **`AWX_AUTO` 없이도** 존중합니다 (auto_setup 은 이 단독 사용을 쓰지 않습니다 — 직접 실행할 때만). 테스트 훅 환경변수 `AWX_RESOLV_CONF`·`AWX_TTY` 는 시험용입니다.

### gossh 작업 기록 (s / `auto_setup s`, v0.8.0)

"zz, bb, fc 로 만든 파일에 뭘 했는지" 를 잊지 않도록 호스트 목록 파일(`/tmp/<소문자 3자리>`)별 gossh 실행 기록을 **user 별**로 남깁니다. TUI `s` 는 메뉴 없이 작업 화면이 바로 열리고, 밖에서는 `auto_setup s [-user <이름>]` (tty 에서만, os6_mgmt 에서도 가능 — 기록은 os8_mgmt 에 중계). 터미널을 옮겨도 같은 user 로 열면 같은 목록이 보입니다. 흐름도: [s](docs/svg/tui_s.svg) · [auto_setup s](docs/svg/cli_s.svg)

```
[user: alice] gossh 작업
 실행: 1,000대 이하 os8 gossh (접속불가는 os6_mgmt 경유 재실행) · 초과는 전량 os6_mgmt(os6mgmt01) 경유
 qkz : svr001 등 20대                                    (메모: LDAP 재적용 대상)
   ├ 10/12 14:02 os8      cat /proc/uptime               성공 18 실패 0 접속불가 2(os6 재실행 → 성공 2)
   └ 10/12 14:10 os8      systemctl is-active sssd       성공 20
 fbw : spice101 등 1,200대
   └ 10/12 15:30 os6 전량 rpm -q kernel                  성공 1,198 접속불가 2
 ↑↓ 이동  n 새 작업  / 호스트 검색  Enter 선택  q 닫기          (화면 모양은 예시)
```

| 키 | 동작 |
|---|---|
| `n` | **새 작업**: 호스트 붙여넣기(공백·쉼표·줄바꿈 구분, `svr[001-020]` 범위 표기, 중복은 한 번만, 빈 줄·`.`·`Ctrl+D` 로 끝) → 같은 목록이 이미 있으면 `qkz 와 같은 목록(20대) — 재사용? [y/n]` → 새 목록이면 `/tmp/<3자리>` 이름을 정함(이미 있는 이름은 피함) → 명령 한 줄 입력 → 실행 |
| `Enter` | 선택한 실행 줄의 **항목 메뉴**: `1` 같은 파일로 새 명령 · `2` 재실행 · `3` 실패·접속불가 호스트만 재실행(새 파일, 원본에서 파생 표시) · `4` 결과 보기(압축 출력을 풀어 `less -R`) · `5` 메모(파일 단위) · `6` 호스트 목록 보기 |
| `/` | **호스트 검색**(정확한 이름 또는 짧은 이름) — 그 호스트가 들어 있던 작업과 명령을 시간순으로 |
| `↑` `↓` `PgUp` `PgDn` · `q`/`Esc` | 이동·스크롤 · 닫기 |

**실행 위치** (기준 대수는 conf `work_os6_over`, 비면 1000)

| 열린 곳 | 대수 | 실행 |
|---|---|---|
| os8_mgmt | ≤ `work_os6_over` | os8 gossh (`gossh -w /tmp/<3자리> '<명령>'`). **접속불가** 호스트만 os6_mgmt 경유로 자동 재실행 (`os6_mgmt`·`os6_gossh` 설정 시) |
| os8_mgmt | 초과 | **전량** os6_mgmt 경유 — 호스트 목록을 base64 로 넘기고 `ssh -t` 로 실행 (`bash -c` 로 감쌈, 출력·위험어 확인 그대로) |
| os6_mgmt (원격 클라이언트) | 상관없음 | 이 서버의 gossh(`os6_gossh`)로 바로 실행, 기록은 os8_mgmt 로 중계 |

- `os6_mgmt`/`os6_gossh` 가 비어 있으면 재실행·전량 os6 가 불가능해 os8 로만 실행합니다 (전량 os6 대상인데 미설정이면 실행 전에 `계속? [y/n]`).
- 명령에 `reboot`·`poweroff`·`shutdown`·`halt`·`init 0`·`init 6`·`ddc` 가 있으면 화면에서 한 번 더 확인한 뒤 gossh 의 위험 작업 확인(대상 목록 + y/N)을 거쳐 실행합니다.
- 실행하는 동안 TUI 를 잠시 떠나 **터미널에서 gossh 를 그대로** 돌립니다 (`script -q -f -c` 로 tty 를 유지 — 색·진행률·위험어 확인 그대로, 출력은 로그 파일에도 저장). 끝나면 Enter 로 목록에 돌아옵니다. 성공/실패/접속불가는 gossh 의 `ERROR: 종료코드 N`·`SSH 접속 실패` 줄과 결과 파일로 판정합니다.

**기록 보관** `<AUTO_SETUP_DIR>/work/<user>/` (디렉터리 0700, 파일 0600)

| 경로 | 내용 | 보관 |
|---|---|---|
| `index.jsonl` | 요약: 시각·터미널(pts)·서버·경로(os8 / os6 / os6 전량)·명령·성공 수·실패/접속불가 호스트·메모. 추가만 하고 고쳐 쓰지 않음 | **영구** |
| `lists/<xxx>.txt` | 호스트 목록 사본 — `/tmp` 파일이 지워져도 재실행 | 영구 |
| `out/<id>.log.gz` | 전체 출력(압축) | **7일**, user 당 **200MB** 를 넘으면 오래된 것부터 삭제 |
| `relay/` | os6_mgmt 에서 중계 중인 조각 (os8_mgmt) | 임시 |

- 정리: **데몬이 하루 한 번**(`workCleanAll`) + 작업 화면을 열 때. `/tmp/<3자리>` 목록 파일도 7일 뒤 함께 정리합니다. 보관 기간이 지난 출력은 `4 결과 보기` 에서 `요약만 남아 있습니다` 로 안내합니다.
- os6_mgmt → os8_mgmt 중계는 한 번에 약 56KB(인코딩 후 약 90KB, 원격 셸 인자 128KB 제한 안)씩 나눠 보내며, 출력이 5MB 를 넘으면 앞·뒤만 보냅니다.

### 변경 작업 (IP·LDAP 변경·원복, v0.7.0)

화면 1·2 어디서나 `m` 키로 변경 메뉴를 엽니다 (v0.7.1: 화면 전환 없이 안내 줄 아래에 인라인). 흐름도: [m](docs/svg/tui_m.svg). IP 변경·LDAP 변경·원복은 노드 설정을 바꾸는 작업이므로 **os8_mgmt 에서만** 실행하며, 실행 전에 미리보기와 `yes` 확인을 거칩니다. 엔진(`ip-change-engine`, `ldap-config-engine`)은 바이너리로 호출하며 auto_setup 은 엔진 코드를 흡수하지 않습니다. 두 도구는 계속 독립적으로 쓸 수 있습니다.

**메뉴** (켜진 항목만 표시, 번호는 고정)

| 번호 | 항목 | 표시 조건 |
|---|---|---|
| `1` | IP 변경 | `enable_ip_change` |
| `2` | LDAP 변경 | `enable_ldap_change` |
| `3` | IP+LDAP 변경 (IP 먼저, IP 실패 호스트는 LDAP 건너뜀) | 둘 다 |
| `4` | 원복 (IP · LDAP · 둘 다, 가장 최근 백업) | 둘 중 하나 |
| `5` | auto.appl 원복 (LDAP 적용 후 대기 중인 auto.appl 반영) | `enable_ldap_change` |

**사용 방법 (TUI)**

1. `m` → 현재 화면은 그대로 두고 키 안내 줄 **바로 아래**에 `변경 ▶ 1 IP 변경  2 LDAP 변경 …   (번호 선택, Esc/m 닫기)` 가 열림 (v0.7.1 부터 전체 화면 아님. 높이 32줄 이상이면 항목별 설명이 덧붙음). 번호를 누르면 이후 흐름이 전체 화면으로 열림. 열려 있는 동안 다른 키는 무시, `Esc`/`m`/`q` 는 메뉴만 닫음 (`q` 가 TUI 를 종료하지 않음)
2. `4` 원복이면 종류 선택 (IP / LDAP / 둘 다)
3. user 선택 (`x` 와 같은 메뉴, 번호 또는 이름)
4. `2`·`3` 이면 infra 선택 (`ldap_config.conf` 의 `infra.<이름>.*` 목록에서 번호)
5. 대상을 입력하고 `Ctrl+D`
   - **IP (`1`·`3`)**: 한 줄에 `hostname 새IP`. 예) `web01 10.1.2.11`. 공백·탭·쉼표·`=`·`->` 구분을 모두 쓸 수 있습니다. GW 는 새 IP 의 마지막 옥텟을 `1` 로 바꾼 값 (`10.1.2.1`). 같은 호스트가 두 번 나오면 마지막 값을 쓰고 경고합니다.
   - **LDAP (`2`·`3`)**: hostname 목록 (공백·쉼표·탭·`|`). site 는 `assets.txt` 로 판정하고, 없으면 `ldap_config.conf` 의 `default_site`, 그것도 없으면 그 호스트는 건너뜁니다.
   - **원복·auto.appl 원복 (`4`·`5`)**: user 의 이전 기록으로 대상이 자동으로 채워집니다. 그대로 `Ctrl+D` 하거나, 새로 입력·붙여넣으면 목록을 대체합니다 (Backspace 로 편집).
6. (`2`·`3`) LDAP dry-run 이 자동으로 실행되어 바뀔 내용을 보여 줍니다. 노드는 바뀌지 않습니다.
7. 미리보기에서 **`yes` 를 정확히 입력**하고 Enter 를 누릅니다. 다른 입력이면 아무것도 바꾸지 않고 취소합니다. 진행 중 job 과 겹치는 호스트는 빨간 경고로 표시됩니다.
8. 실행 중에는 `q`/`Esc` 가 무시됩니다. 취소는 `Ctrl+X` → `y` 입니다 (일부 호스트만 바뀌었을 수 있음).
9. 결과 화면이 열립니다 (`↑↓` 스크롤, `q`/`Esc` 닫기). 맨 아래에 기록 파일 경로가 나옵니다.

**실행 경로 (os8 → os6)**

- 모든 호스트는 os8_mgmt 에서 먼저 실행합니다.
- 응답이 없는 호스트만 `os6_mgmt` 경유로 한 번 더 시도합니다 (`os6_mgmt`·`os6_gossh` 가 설정된 경우). 이미 route=os6 로 알려진 호스트는 처음부터 os6 경유입니다.
- 경유는 엔진 `-gossh` 에 넘기는 임시 래퍼 (작업 디렉터리 0700, 끝나면 삭제)로 합니다. 비밀번호 인자 `-p` 는 쓰지 않습니다.
- 원복은 엔진 기본값(가장 최근 백업)을 씁니다. 특정 백업 시점으로 되돌리려면 엔진을 단독으로 실행하십시오 (`사용법.txt` 13절).

**기록과 로그**

- 기록: `<상태 디렉터리>/changes/<user>/<YYYYmmddHHMMSS>_<kind>.json`. 상태 디렉터리는 `/tmp/auto_setup` (또는 `AUTO_SETUP_DIR`). 디렉터리 0700, 파일 0600. `kind` 는 `ip` · `ldap` · `both` · `rollback-ip` · `rollback-ldap` · `rollback-both` · `appl` 중 하나입니다.
- 기록에는 호스트별 결과가 들어갑니다 (IP·LDAP·auto.appl 칸, 기존/새 IP, GW, 경로 os8|os6, 상세). bindpw 와 gossh 명령줄은 들어가지 않습니다.
- `auto_setup.log` 에는 `변경 메뉴 <kind>: user <user> 대상 N대 OK a FAIL b` 한 줄이 남습니다.
- 원복이 성공하면 이전 적용 기록의 해당 호스트에 `ip_rolled_back`·`ldap_rolled_back` 을 표시하고 auto.appl 대기(`appl_pending`)를 해제합니다. 그래서 다음 원복 때 같은 호스트가 다시 대상으로 잡히지 않습니다.

**conf 키** (`/etc/auto_setup/auto_setup.conf` 등, 전부 비우면 꺼짐)

| 키 | 기본값 | 의미 |
|---|---|---|
| `enable_ip_change` | 비움 (꺼짐) | `y`·`yes`·`1`·`true` 면 메뉴 `1` (둘 다 켜지면 `3` 도), 원복의 IP 항목 표시 |
| `enable_ldap_change` | 비움 (꺼짐) | 메뉴 `2` (둘 다 켜지면 `3`), `5`, 원복의 LDAP 항목 표시 |
| `ip_engine` | 비움 | `ip-change-engine` 전체 경로. 비우면 auto_setup 과 같은 디렉터리 → PATH |
| `ldap_engine` | 비움 | `ldap-config-engine` 전체 경로. 같은 규칙 |

변경에 쓰는 파일은 auto_setup.conf 를 읽은 디렉터리에서 찾습니다 (없으면 `/etc/auto_setup/`). `setup.sh` 가 샘플을 설치하며 이미 있는 파일은 덮어쓰지 않습니다.

| 파일 | 내용 |
|---|---|
| `ip_change.conf` | ifcfg 탐색 경로 (`network_scripts_dir`, 기본 `/etc/sysconfig/network-scripts`) 등 |
| `ldap_config.conf` | `infra.<이름>.*` (uri·binddn·bindpw 등), `default_site`. 퍼미션 600 |
| `assets.txt` | `hostname<TAB>site`. LDAP 의 site 판정에 씀 |

**`--admin`**: `auto_setup --admin` 으로 TUI 를 띄우면 conf 설정과 상관없이 메뉴 `1`~`5` 가 모두 나옵니다. 데몬·`--plain`·`request` 명령에는 영향이 없습니다. os6_mgmt 원격 TUI 에서 `m` 을 누르면 "os8_mgmt 에서 실행하세요" 안내만 나오고 실행은 되지 않습니다.

**설치·적용 절차 (os8_mgmt)**

```bash
bash setup.sh                           # auto_setup + 엔진 2개 빌드·설치 + conf 샘플 (기존 /etc/auto_setup 파일은 유지)
auto_setup --restart                    # 옛 바이너리로 도는 데몬 재기동
vi /etc/auto_setup/auto_setup.conf      # enable_ip_change=y, enable_ldap_change=y 등 (기존 설정 파일은 자동으로 바뀌지 않음)
vi /etc/auto_setup/ip_change.conf       # 현장 값으로 수정
vi /etc/auto_setup/ldap_config.conf     # infra 값·bindpw 입력
vi /etc/auto_setup/assets.txt           # hostname<TAB>site
chmod 600 /etc/auto_setup/ldap_config.conf
```

- 저장소의 `conf/auto_setup.conf` 가 있는 상태로 `setup.sh` 를 실행하면 `/etc/auto_setup/auto_setup.conf` 가 그 내용으로 덮어써지므로, 새 키는 저장소 쪽 `conf/auto_setup.conf` 에 넣으십시오.
- os6_mgmt 에는 변경 메뉴 설치가 필요 없습니다. `ip_change/bin/ip-change-engine_os6`·`ldap_setting/bin/ldap-config-engine_os6` 는 RHEL 6 에서 엔진을 단독으로 실행할 때 쓰는 정적 바이너리이며 `build_os6.sh` 가 빌드합니다.

**주의사항 (변경 작업)**

- **설정 변경 도구입니다.** 적용·원복 뒤 무작위 몇 대에 직접 접속해 실제로 반영됐는지 확인하십시오.
- **IP**: ifcfg 파일만 바꿉니다. 반영은 네트워크 재시작 또는 재부팅 뒤이며, 이 도구는 네트워크를 재시작하지 않습니다. 결과 화면에도 이 안내가 나옵니다.
- **노드 서비스 재시작 없음**: LDAP 엔진 호출 (dry-run·적용·원복) 에는 항상 `-no-restart` 가 붙어 nslcd·sssd·autofs·ntpd·chronyd 를 재시작하지 않습니다. 새 설정이 서비스에 반영되는 시점은 사용자가 정합니다.
- **auto.appl**: LDAP 적용 시 새 `auto.appl` 은 `/etc/auto.appl.bak` 에 대기합니다. 메뉴 `5` 를 실행해야 반영됩니다. 그 전까지 `ldap_check` 는 auto.appl 을 읽지 못해 FAIL 로 나올 수 있습니다.
- **bindpw**: `ldap_config.conf` 에만 있고 퍼미션은 600 이어야 합니다. 이 파일은 커밋하지 않습니다 (`.gitignore` 제외). 화면·기록·로그·임시 파일에 남기지 않으며 엔진의 `-print-script` 는 호출하지 않습니다. 다만 LDAP 엔진은 적용·원복 스크립트를 base64 로 인코딩해 gossh 명령줄에 싣기 때문에, os8_mgmt·os6_mgmt·대상 서버의 `ps` 에 그 base64 문자열이 잠깐 보일 수 있습니다. 이는 ldap_setting 의 기존 설계이며 이번 버전에서 바꾸지 않았습니다.
- **진행 중 job**: 변경 대상이 추적 중인 job 의 호스트이면 미리보기에 경고가 나옵니다. IP 가 바뀌면 그 job 의 추적이 끊길 수 있습니다. job·snapshot·데몬은 건드리지 않습니다.
- **취소**: `Ctrl+X` → `y` 로 중단하면 이미 끝난 호스트는 바뀐 상태로 남습니다. 결과 화면과 기록에 호스트별 결과가 남습니다.
- **검증 범위**: RHEL 7·9 실장비 동작은 fixture 검증까지입니다. 실장비 확인 체크리스트는 `사용법.txt` 13절에 있습니다.

### 실행 모드: 설정체크 + 설정수정 / 설정체크만 (v0.5.0)

흐름도: [h→c](docs/svg/tui_h_c.svg) · [h→t](docs/svg/tui_h_t.svg)

| 구분 | 키 / 명령 | 환경설정 수정 | 호스트 완료 처리 | os_check 호출 |
|---|---|---|---|---|
| **배포 → 완료 자동 흐름** | (자동) | **수정함** (체크 → 수정 → 재점검) | 함 | `-auto` |
| 수동 **설정체크 + 설정수정** | `h`→`c` (또는 `c`) / `request manual-run <jobid> <yml>` | **수정함** | 함 (정상 실행된 호스트는 단계 무관) | `-auto` |
| 수동 **설정체크만** | `h`→`t` (또는 `t`) / `request manual-run <jobid> <yml> check` | **수정 안 함** (점검 결과만) | 안 함 (단계·실패 횟수·2차 체크도 그대로) | `-auto-check` |

- 배포 → 완료까지의 자동 흐름은 **항상** 설정체크 + 설정수정입니다. 설정체크만은 수동 `t` 로만 실행됩니다.
- `c`/`t` (`h` 메뉴 또는 숨은 단축키)는 확인(y/n) 문구에 어느 모드인지 적혀 있고, 결과 code 본문 첫 줄에도 `작업 : 설정체크 + 설정수정` / `작업 : 설정체크만 (설정 수정 안 함)` 으로 남습니다.
- **결과 창**: `y` 를 누르면 전체 화면 결과 창이 열려 요청 전달 → 대기 → 실행 중을 보여 주다가, 끝나면 그 창에 결과(결과 리포트·FAIL 줄·NO FAIL·LDAP/SPLUNK/커널 요약)를 그대로 출력합니다. `↑↓` 스크롤, `Space`/`b` 쪽 이동, `r` 새로고침, `q`/`Esc` 닫기(닫아도 데몬은 계속 진행). 호스트표에서 `h`→`v` (또는 `v`) 로 그 그룹의 가장 최근 수동 실행 결과를 다시 봅니다.
- **`g` 진행 창** (`h`→`g` 또는 `g`): `재확인 시작 → 1/3 os8_mgmt 확인 → 2/3 os6_mgmt 경유 확인 → 3/3 최종 결과` 가 실시간으로 나오고, 끝나면 호스트별 결과표(`os8 응답` / `os6 경유` / `접속불가` 와 어디서 안 됐는지, 응답 호스트는 현재 단계)가 표시됩니다. 창에서 `g` 로 다시 재확인.
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

흐름도: [h→g](docs/svg/tui_h_g.svg) · [request recheck](docs/svg/cli_request_recheck.svg)

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
- **`g` 키 (서버 상태 수동 재확인, v0.4.7)**: 호스트표(그룹/전체 보기)에서 `h`→`g` (또는 `g`) 를 누르면 완료를 제외한 모든 호스트(설치중·정체·실패 포함, 단계 무관)를 데몬이 지금 바로 확인합니다. ① os8_mgmt 에서 확인 → 응답하면 route=local, ② 무응답이면 os6_mgmt 경유 → 응답하면 route=os6, ③ 둘 다 무응답이면 접속불가로 간주(상태는 그대로, 로그에 `[!] 재확인(g) 접속불가·미응답 N대`). 응답한 호스트는 준비확인 결과(설치중·부팅확인·READY)를 즉시 반영합니다. 명령: `auto_setup request recheck <jobid> <그룹.yml 또는 *>`. `os6_mgmt`·`os6_gossh` 가 비면 ① 만 수행.
- **LDAP 백업도 os6 경유 (v0.4.7)**: 전달 시점 LDAP 설정 수집이 os8 에서 안 되는 호스트(ssh 불가 등)는 같은 호출에서 os6_mgmt 경유로 다시 수집하고 route=os6 로 바꿉니다(로그 `경로 전환: … (LDAP 백업 …)`). 설치 후 binddn 비교·복원(Apply)도 os8 무응답이면 os6_mgmt 경유로 한 번 더 시도합니다. 수집이 안 된 호스트는 사유가 로그에 남습니다(`[!] LDAP 백업 불가: <호스트> … - <사유>`).
- `r` 키(또는 `auto_setup request refresh`)는 화면만 다시 읽는 것이 아니라 데몬에 **즉시 ping·준비확인**을 요청합니다. 데몬이 os8 에서 안 보이는 호스트는 위 규칙대로 os6_mgmt 에서 정보를 가져옵니다.
- 배포중에서 멈춰 있으면 데몬 로그에서 `경로 판별`·`경로 전환`·`os6 probe 세션`·`준비확인 실패` 줄을 확인하십시오. os6 경로는 데몬이 `ssh -o BatchMode=yes <os6_mgmt>` 로 접속하므로 os8_mgmt → os6_mgmt **ssh 키 인증**이 되어 있어야 합니다.
- 설정 파일(`os6_mgmt` 등)을 바꾼 뒤에는 `auto_setup --restart` 가 필요합니다(설정은 데몬 기동 때 한 번 읽음).
- **시간 제한 (v0.8.0)**: 준비확인과 `g` 의 gossh 호출(os8)·`ssh <os6_mgmt>` 호출(os6)은 호출 1회마다 **전체 60초** 제한이 있습니다. 넘으면 프로세스 그룹(gossh/ssh 와 자식)을 종료하고 그 호스트는 무응답으로 처리하며, 화면의 사유에 `시간 초과(60초)` 가 나옵니다 (상수 `checkTimeout`, conf 변수 없음). os6 ssh 는 다른 경로와 같은 `-o BatchMode=yes -o ConnectTimeout=10` 입니다.
- **csh/tcsh 로그인 셸 (v0.8.0)**: os6_mgmt 로 보내는 원격 명령(준비확인·os_check run·2차 체크·LDAP 백업·복원·원격 클라이언트 중계·변경 엔진·gossh 작업)은 모두 `bash -c '<명령>'` 한 줄(함수 `remoteBash`, 명령 안의 `'` 는 `shq` 로 이스케이프)로 감싸 보냅니다. 그래서 로그인 셸이 csh/tcsh 여도 `$(...)`·`${f}` 를 bash 가 해석합니다. tcsh 는 작은따옴표 안의 `!` 도 history 치환하므로 명령에 `!` 가 있으면 base64(두 글자마다 `.`)로 보냅니다. os6 gossh 래퍼 2종도 같은 형태입니다. 경로·원격 인자는 모두 `shq` 를 거치므로 `os6_gossh` 경로에 공백이나 `'` 가 있어도 깨지지 않습니다.
- **gossh 출력 해석 통일 (v0.8.0)**: 준비확인·LDAP 이 따로 해석하던 gossh 출력을 `parseGossh` 하나로 합쳤습니다 — ANSI 색 제거, `호스트: 내용` 분리, `ERROR: 종료코드 N`·`SSH 접속 실패: …` 줄을 사유로 인식. 호스트 이름은 정확히 같은 이름이 우선이고 없으면 **짧은 이름**(첫 `.` 앞)으로 맞춥니다 (FQDN 과 짧은 이름이 섞여도 응답을 놓치지 않음. 짧은 이름이 둘 이상과 겹치거나 IP 주소면 맞추지 않음). 출력은 있는데 호스트를 하나도 인식하지 못하면 원본 앞 5줄을 `auto_setup.log` 에 `[!] gossh 출력 해석 실패` 로 남기고(LDAP 수집 응답은 bindpw 보호를 위해 원본을 기록하지 않음) `g` 결과 상세에 `출력 형식 불일치` 로 표시합니다.

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
# Go 단위 테스트 (가짜 시계·pinger·checker, TUI 골든 포함) — 398 PASS, 7 SKIP (랩 전용·root 전용)
go test ./...

# 랩 전용: 진짜 ssh 로 로그인 셸이 tcsh/csh 인 계정에 접속해 os6 경로가 `!` 가 있어도 동작하는지 확인 (v0.8.0)
# 환경변수가 없으면 SKIP. 대상은 root 키 인증이 되는 계정 (로그인 셸 tcsh, 예: ascsh@localhost)
AS_TEST_LOGIN_MGMT=ascsh@localhost go test -run ViaLoginShell .

# 정적 검사
go vet ./... && gofmt -l .

# 1차 목업 E2E (스크래치, 스텁 gossh/ssh/wall, root 필요, 약 1분) — 86건
bash test/run_e2e.sh

# 2차 목업 E2E (a 양방향 동일성 / b 완료기록 / c 요청·TUI / d 데몬 제어 / e LDAP / f 2차 체크) — 194건
bash test/run_e2e2.sh          # 전체
bash test/run_e2e2.sh b e      # 시나리오 지정

# E2E 스크래치 보존 (로그 확인)
KEEP_SCRATCH=1 bash test/run_e2e2.sh f

# 변경 메뉴(m) 검증 (v0.7.0): RHEL 7/8/9 fixture 판정·-no-restart 확인 / .58 자기 자신 실동작 (접속 불가면 종료코드 3) / 모의 환경
bash test/run_change_os.sh      # 끝 줄 PASS=<n> FAIL=<m> — 173건
bash test/run_change_lab.sh     # 실제 gossh 로 자기 자신 접속 — fixture 경로만 변경, 실제 /etc 는 건드리지 않음 — 18건

# 가이드·업데이트 스크립트 (v0.4.0·0.6.0·0.7.0·0.8.0, 순차 적용 포함) — 54건
bash test/run_setup_scripts.sh
bash test/demo_change.sh        # 모의 환경 준비 (사용법은 스크립트 상단 참고), --stop 으로 정리

# awx_script 테스트 (108건 — DNS 점검 [4-1] 케이스 포함, 스텁 dig/nslookup + 실제 dig 용 가짜 DNS 서버 test/fake_dns.py)
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
| `checkTimeout` (check.go, v0.8.0) | 60초 | 준비확인·`g` 의 gossh/ssh 호출 1회 전체 상한 (넘으면 프로세스 그룹 종료, 사유 `시간 초과(60초)`). conf 변수 없음 |
| `workKeep` · `workUserCap` (work.go, v0.8.0) | 7일 · 200MB | gossh 작업 전체 출력·`/tmp/<3자리>` 보관 기간 / user 당 출력 상한 |
| `dnsQueryTimeout` · `dnsAttempts` · `dnsConcurrency` · `dnsMaxServers` (dnscheck.go, v0.8.0) | 2초 · 2회(최초 1 + 재시도 1) · 32 · 3 | DNS 점검 질의 시간·횟수·동시 수·사용하는 nameserver 수 |

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
| `main.go` | 최상단 빈 변수(32개) + CLI 분기 + 상수 정의 (`-user`·`newhost`·`s`·`work-relay` 분기 포함) |
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
| `change.go` · `changerun.go` · `changerec.go` | m 변경 메뉴: 상태기계·입력 정규화·bindpw 마스킹 / 엔진·gossh 실행, os6 경유, 엔진 인자(`-no-restart`) / user 별 기록·원복 자동 채움 (v0.7.0) |
| `change_render.go` · `change_tui.go` · `change_s6_test.go` | 변경 화면 문자열(줄바꿈) · `m` 키 연결 · RHEL 7/8/9 fixture 교차 점검 (v0.7.0) |
| `done_tui.go` · `done_tui_test.go` · `anaconda_test.go` | `d` 수동 완료 프롬프트(인라인)·중계 · anaconda 감지 테스트 (v0.7.1) |
| `userfilter.go` · `sessionuser.go` | `-user` 해석·job 필터(v0.7.3) / 세션 user: `s`·신규 설치 양식이 처음 한 번 묻고 TUI 종료까지 기억, CLI 진입용 user 질문 (v0.8.0) |
| `menu_tui.go` · `flow_tui.go` | `h`·`w` 항목별 인라인 메뉴(글자·번호 선택, 숨은 단축키 핸들러 호출) / 전체 화면 흐름 공통 부품(틀·스크롤·붙여넣기·백그라운드 작업) (v0.8.0) |
| `newhost.go` · `newhost_tpl.go` · `newhost_tui.go` | 신규 설치 양식: 붙여넣기 해석·지정 미니 언어·이력 추천·저장 / 템플릿 파싱·검증·`work_os6_over` / 5단계 화면·`newhost` CLI·AWX 연결 (v0.8.0) |
| `dnscheck.go` | DNS 점검 엔진: resolv.conf 서버에 직접 A 질의(UDP/TCP, 표준 라이브러리만)·판정 5종·중복 경고·교체 (v0.8.0) |
| `work.go` · `work_tui.go` | gossh 작업 기록: 붙여넣기·`/tmp/<3자리>`·실행 경로·`script` 래퍼·판정·저장·정리·os6→os8 중계(`work-relay`) / 작업 화면·`s` CLI (v0.8.0) |
| `tcsh_live_test.go` | 랩 전용: 로그인 셸이 tcsh 인 계정으로 os6 경로·원격 클라이언트·gossh 작업 확인 (`AS_TEST_LOGIN_MGMT`, 없으면 SKIP) |
| `auto_setup_os6` | 빌드 완료 os6 용 정적 바이너리 (Go 1.20, 변수 빈 값). 소스 변경 때 `build_os6.sh` 로 재빌드해 커밋 |
| `conf/update_v0.4.0.sh` · `update_v0.6.0.sh` · `update_v0.7.0.sh` · `update_v0.8.0.sh` | 현장 conf 업데이트 (생성 스크립트, 직접 고치지 말고 `vars.manifest` 수정 후 재생성) |
| `docs/svg/*.svg` · `docs/svg/README.md` | 명령·키별 흐름도 31개와 목록 표 (v0.8.0) |
| `계획서_v0.6.md` · `수정계획서.md` | v0.6.x 명세 / v0.8.0 수정계획서(인터뷰 결과, 확정 사항 §9) — 참고용 |
| `계획서_v0.7.md` | v0.7.0 명세 (확정 사항 §1, 단계 §3) |
| `*_test.go` / `testdata/` | 단위 테스트·TUI 골든 |
| `test/manual_check.sh` | 대화형 점검 1~17단계 |
| `test/run_e2e.sh` / `test/run_e2e2.sh` / `test/lib_e2e.sh` | 1차 / 2차 목업 E2E / 공용 함수 |
| `setup.sh` | 빌드·설치·cron·tmpfiles 등록 (멱등) |
| `build_os6.sh` | CGO_ENABLED=0 정적 빌드 (Go 1.20, os6_mgmt 용) |
| `README.md` | 이 문서 — 설치·사용·옵션 가이드 |
| `CHANGELOG.md` | 변경 이력 |
| `ARCHITECTURE.md` | 폴더 구조·함수 역할·런타임 디렉터리 |
| `WORKFLOW.md` | 실행 흐름도 (텍스트 mermaid: 데몬 흐름·상태 리포트·메뉴 구조·신규 설치 양식·gossh 작업·DNS 점검) |
| `workflow.svg` | 실행 흐름도 (SVG, v0.8.0 신규 기능 열 포함) |
| `PR_CHECKLIST.md` | 풀리퀘스트 검증 항목 |
| `사용법.txt` | 명령어 중심 빠른 참고 (운영 담당자 용도) |
| `.github/workflows/ci.yml` | 폴더 내 CI (go vet / test / build, gofmt, bash -n / shellcheck) |
| `<저장소 루트>/.github/workflows/auto-setup.yml` | 이 폴더 변경 시 도는 경로 필터 CI: Go 1.26.5 build·vet·test + `bash -n`, Go 1.20 문법 호환 확인 job (v0.8.0) |

## 알려진 제한

- `/tmp/auto_setup` 의 **소유자 검증이 없습니다.** root 만 쓰는 서버에서 운영하고 `ls -ld /tmp/auto_setup` 으로 소유자·권한을 확인하십시오.
- LDAP Apply 는 **직렬 처리**입니다(호스트가 많으면 READY 후 run 시작이 늦어질 수 있음).
- **정리 정책이 없는 디렉터리**: `ldapbak/`, `requests/rejected/`, `jobs/done/`, `runs/*/second/` — 필요시 수동 정리.
- `AUTO_SETUP_NOW` 는 테스트 전용입니다(운영에서 설정하면 snapshot 시각이 어긋남).
- 원격 클라이언트 모드의 gossh 호출에는 **시간 제한이 없습니다**(os8_mgmt 가 응답하지 않으면 대기).
- 2차 체크는 설정(set)을 한 번 더 적용할 수 있습니다(`os6_os_check_sh="-"` 로 끔).
- (v0.7.0) 변경 메뉴(`m`)의 LDAP 엔진은 적용·원복 스크립트를 base64 로 gossh 명령줄에 실으므로 `ps` 에 base64 문자열이 잠깐 보일 수 있습니다 (ldap_setting 의 기존 설계). 변경 메뉴 RHEL 7·9 실장비 동작은 fixture 검증까지입니다.
- (v0.8.1) **프로파일 없이 이어가는 AWX 경로**: 신규 설치 양식 → AWX 실행에서 프로파일을 고르지 않으면(또는 `awx_profile_N` 이 하나도 없으면) 자동 응답이 전혀 없는 일반 실행이므로 nodeinfo 질문을 포함한 01 의 모든 질문에 직접 응답해야 합니다. 신규 설치라 nodeinfo 를 쓸 수 없으면 프로파일을 골라(`y`) nodeinfo N 으로 실행하거나 질문에 `N` 을 입력하십시오.
- (v0.8.0) **원격 인자 128KB**: os6_mgmt 로 보내는 원격 명령은 셸 인자 한 개(약 128KB)를 넘지 못합니다. gossh 작업의 os6 전량 실행은 호스트 목록을 gzip+base64 로, os6_mgmt → os8_mgmt 기록 중계는 약 56KB 조각으로 나눠 이 안에 맞추지만, 이름이 매우 긴 호스트가 수만 대이면 실패할 수 있습니다. 중계되는 전체 출력은 5MB 를 넘으면 앞·뒤만 보냅니다.
- (v0.8.0) **os6 로컬 보관분 미병합**: os6_mgmt 에서 연 `s` 가 os8_mgmt 로 기록을 보내지 못하면 이 서버(os6_mgmt)의 기록을 보여 주고 거기에 저장합니다. 나중에 os8_mgmt 가 돌아와도 **그 기록은 os8_mgmt 의 기록과 합쳐지지 않습니다**.
- (v0.8.0) **os6 probe 세션의 원격 명령**: 데몬이 os6_mgmt 에 걸어 두는 장기 ssh 세션(`auto_setup probe`)의 명령은 `bash -c` 로 감싸지 않고 경로를 `shQuote` 로만 인용합니다. 로그인 셸이 tcsh 이고 `os6_autosetup` 경로에 `!` 가 있으면 history 치환으로 실패합니다 (경로에 `!` 를 쓰지 마십시오).
- (v0.8.0) **gossh 작업 판정**: 성공/실패/접속불가는 gossh 의 `ERROR: 종료코드`·`SSH 접속 실패` 줄과 결과 파일로 판정하므로 gossh 출력 형식이 바뀌면 `index.jsonl` 의 집계가 어긋날 수 있습니다 (전체 출력은 그대로 보관됨).
- (v0.8.0) **DNS 점검 범위**: IPv4 A 레코드와 `resolv.conf` 의 DNS 서버만 봅니다 (`/etc/hosts`·mDNS·AAAA 는 보지 않음). 서버가 하나만 응답하면 그 답이 기준이 됩니다.

## 주의사항 (Disclaimer)

본 스크립트 및 도구는 **100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.**

**설정 변경 후 반드시 다음을 확인하세요:**

1. 설정 변경 스크립트이므로, **설정 변경 후 랜덤한 서버 몇 대를 선택하여 실제로 변경되었는지 확인하세요.** LDAP 복원·2차 체크·변경 메뉴(`m`)도 서버 설정을 바꿉니다.
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

# 변경 메뉴(m)를 conf 없이 TUI 에서 켜기 (os8_mgmt, 데몬에는 영향 없음)
auto_setup --admin

# 작업별 상태 / 4자리 code 결과 / 취소
auto_setup status
auto_setup code 4821
auto_setup cancel 20261002-153000-user1

# (v0.8.0) 신규 설치 양식 / gossh 작업 기록 — -user 를 주면 user 를 묻지 않음
auto_setup newhost -user alice
auto_setup s -user alice

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
bash -n setup.sh build_os6.sh conf/*.sh test/*.sh
go vet ./... && gofmt -l .

# 2단계: 대화형 점검
bash test/manual_check.sh -y

# 3단계: 자동 테스트
go test ./...
bash test/run_e2e.sh          # 86건
bash test/run_e2e2.sh         # 194건
bash test/run_setup_scripts.sh # 54건
bash ../awx_script/test/run_tests.sh   # 108건

# (선택, 랩) tcsh 로그인 셸 확인
AS_TEST_LOGIN_MGMT=ascsh@localhost go test -run ViaLoginShell .

# 4단계: 랩 스모크 테스트 (.58)
bash setup.sh
auto_setup status
# 실제 ping·ssh 동작 확인
```

자세한 명령은 `사용법.txt` 참조.
