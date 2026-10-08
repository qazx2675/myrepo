# CHANGELOG

## [0.6.0] - 2026-10-08

### 추가
- **`x` 체크스크립트 단독 실행**: 화면 1·2 어디서나 `x` → user 선택(awx_dir 의 01 `user_route` 메뉴 `info_mn.sh`/`info.sh`, 메뉴를 못 찾으면 user 이름 직접 입력) → 호스트를 붙여넣고 `Ctrl+D`(공백·쉼표·탭·`|` 는 줄바꿈, 중복 제거) → `t` 설정체크만 / `c` 설정체크 + 설정수정. 실행 중 `q`/`Esc` 는 무시하고 `Ctrl+X → y` 로 프로세스 그룹을 취소합니다. 결과는 기존 결과 창과 같은 스크롤 화면(↑↓, Space/b, q/Esc 닫기)으로 보입니다.
  - **기록 안 남김**: auto_setup.log·codes·runs 에 아무것도 남기지 않습니다. 임시 디렉터리에서 실행하고 결과 창을 닫으면 그 디렉터리를 지웁니다 (`oneoff.go`)
- **`w` AWX 실행**: 화면 1·2 어디서나 `w` → TUI 를 내려놓고 awx_dir 에서 `bash 01.AWX_nodeinfo_V2.sh` 를 이 터미널에서 실행합니다. 유효한 프로파일이 있으면 `AWX auto 실행? [y/n]` 를 묻고, y 면 프로파일 번호를 고릅니다(auto 면 `AWX_AUTO*` 환경변수 전달). `Ctrl+X` 는 AWX 를 **즉시** 취소하며, 끝나면 auto_setup 화면으로 돌아와 `AWX 종료 (rc=N)` / `AWX 취소됨 (Ctrl+X)` 을 표시합니다 (`awxrun.go`, `tty_linux.go` 의 termios 조작)
- **AWX 프로파일 conf**: `awx_profile_1` ~ `awx_profile_9`, 형식 `nodeinfo(y/n)|OS값|설명` (예: `y|2025|설명`). 허용 OS 값은 `2024 2025 2026 2026-OPC_MDP 2026-ECAD_TCAD default`. 필드가 부족하거나 nodeinfo·OS 값이 틀리면 그 프로파일은 목록에서 빠지고 사유가 표시됩니다 (`awxprofile.go`)

### 변경
- **Ctrl+C / Ctrl+Z 무시**: TUI 에서 종료·중단 없이 `종료는 q 를 누르세요` 만 표시합니다. 종료는 q. Ctrl+Z 의 SIGTSTP 도 무시
- **원격(os6) 화면 자동 갱신 5초 → 10초** (`refreshAway`)
- **conf**: `auto_setup.conf.example`·`vars.manifest` 에 awx_profile_1~9 추가. `conf/setup_guide.sh` 재생성, `conf/update_v0.6.0.sh` 추가 (이전 = origin/master)
- **도움말**: `x`·`w` 키 설명 추가 (`render.go` `renderHelp`)
- 테스트: `awxprofile_test.go`(프로파일 파싱·무효 사유), `oneoff_test.go`(호스트 정규화·user 검증·키 시퀀스, 가짜 실행기), `awxrun_test.go`(AWX 질문·프로파일 선택·환경변수), `tui_test.go`(Ctrl+C 로 종료되지 않음), 모의 테스트 `test/demo_flow.sh` 에 x/w 시나리오 추가

## [0.5.1] - 2026-10-08

### 변경
- **`g` 재확인 화면을 호스트표 + 진행 과정 한 화면으로**: 위쪽에 호스트표(단계가 갱신되는 것을 바로 확인), 아래쪽에 `1/3 → 2/3 → 3/3` 진행 과정과 호스트별 결과표 (터미널 높이 24줄 이상일 때, 더 작으면 종전처럼 전체 화면)
- **`설정수정` 빨간색 강조**: 환경설정을 실제로 수정하는 모드를 눈에 띄게 — 하단 `[c] 설정체크 + 설정수정`, `c` 확인 문구의 `설정수정 (환경설정 수정함)`, 결과 창 제목·`작업 :` 줄. `t`(설정체크만)는 빨강 없음
- 키 안내 줄(↑↓ 이동 … q 복귀)을 흐린 회색에서 굵은 글씨로 변경 (잘 보이게)
- **종료된(jobs/done) job 에서도 `g` 동작**: 종료된 job 에서 g 를 누르면 요청이 거부되어 진행 창이 "요청 전달됨" 에서 멈추던 문제 수정. 종료 job 은 경로(local/os6)만 갱신하고 단계·완료 상태는 건드리지 않는다. job 을 아예 찾을 수 없으면 진행 창에 사유(`job 을 찾을 수 없습니다`)가 표시된다
- 진행 창 줄 색: `무응답 0대`·`접속불가 0대` 는 빨강으로 표시하지 않고, `최종 결과` 줄은 초록
- 테스트: `mode_test.go` (색, 분할 레이아웃). 골든 갱신

## [0.5.0] - 2026-10-08

### 추가 / 변경
- **설정체크만(`t`) 수동 실행 추가 + 모드 구분 명확화**: 그룹 호스트표에서 `c` = **설정체크 + 설정수정**(환경설정을 실제로 수정, 종전 동작), `t` = **설정체크만**(환경설정은 수정하지 않음). 화면 하단·확인 문구·도움말·code 첫 줄(`작업 : 설정체크 + 설정수정` / `작업 : 설정체크만 (설정 수정 안 함)`)에 모드를 밝힌다. **배포 → 완료까지의 자동 흐름은 항상 설정체크 + 설정수정**이며 설정체크만은 수동 `t` 로만 실행된다
  - 요청: `auto_setup request manual-run <jobid> <yml> [check]` (`check` 가 설정체크만, 요청 파일 `mode=check`)
  - 설정체크만 run 은 `os_check` 를 `-auto-check <user> <목록>` 으로 호출하고 점검 결과 `check.res_<user>` 를 읽는다. 호스트를 **완료 처리하지 않고**, 실패 횟수·단계·2차 체크도 건드리지 않으며 job 의 runs 에 `mode: check` 로만 기록한다 (로그: `수동 run 완료: … (설정체크만 (설정 수정 안 함))`)
  - `OS 환경설정 체크/os_check_final_annotated.sh` 에 `-auto-check` 옵션 추가 (변경 항목 32, `-auto` 와 같되 환경설정 수정 질문에 n). **회사에서 쓰는 os_check/config_check 사본에도 같은 3줄 변경이 필요하다** (없으면 `t` 는 "user 값이 비어 있습니다" 로 비정상 종료로 기록됨, `c`·자동 흐름은 영향 없음)
- **수동 실행 결과를 auto_setup 안에서 출력**: `c`/`t` 확인(y) 후 전체 화면 결과 창이 열린다. 요청 전달 → 대기 → 실행 중 상태를 보여 주다가 끝나면 그 창에 code 본문(결과 리포트·FAIL 줄·NO FAIL·LDAP/SPLUNK/커널 요약)을 그대로 표시한다. `↑↓` 스크롤, `Space`/`b` 쪽 이동, `r` 새로고침, `q`/`Esc` 닫기(닫아도 데몬은 계속 진행). `v` 로 그 그룹의 가장 최근 수동 실행 결과를 다시 볼 수 있다 (`view.go`)
- **`g` 재확인 진행 상태 표시**: `g` 를 누르면 진행 창이 열려 `재확인 시작 → 1/3 os8_mgmt 확인 → 2/3 os6_mgmt 경유 확인(필요할 때, 미설정이면 건너뜀 사유) → 3/3 최종 결과` 를 실시간으로 보여 주고, 끝나면 호스트별 결과표(`os8 응답` / `os6 경유` / `접속불가` + 어디서 안 됐는지 `os8 무응답 → os6 무응답`, 응답 호스트는 현재 단계)를 표시한다. 데몬이 단계마다 `recheck/<jobid>.json` 에 기록하고 snapshot 의 `jobs[].recheck` 로 전달된다. 창에서 `g` 로 다시 재확인
- 모의 테스트 환경 `test/demo_flow.sh` (스텁 gossh/ssh + 복사본 os_check, 운영 데이터 미사용), 화면용 가짜 작업 `test/make_demo_jobs.sh`. 가짜 gossh 에 `silent_hosts`(어디서도 무응답)·`os8silent_hosts`(os8 에서만 무응답) 시나리오 추가
- 테스트: `mode_test.go` (설정체크만 run 이 완료 처리하지 않음·mode 검증, Runner 인자·code 첫 줄, 재확인 기록, TUI c/t 문구·결과 창·g 진행 창). 골든 갱신(힌트·하단 줄)

## [0.4.8] - 2026-10-08

### 추가
- **TUI 날짜별 보기**: 화면 1(작업·그룹 목록) 맨 위에 날짜 줄 `<    2026-10-08 (목)    >   N개 그룹` 이 생겼다. 맨 위 그룹에서 `↑` 를 한 번 더 누르면 날짜 줄이 선택되고, 거기서 `←` 전날 / `→` 다음날로 이동한다(그룹이 있는 날짜 범위 안, 그룹이 없는 날도 하루씩 지나감). 해당 날짜의 그룹만 표시하며, 기본은 가장 최근 날짜(최근 날짜에 닿으면 계속 최근을 따라감). 날짜는 그룹 yml 이름의 14자리 시각(`infra_inventory-20261008084159_4ea.yml` → 2026-10-08)에서 읽고, 이름에 시각이 없으면 job 전달일을 쓴다. `↓` 로 목록에 내려가면 종전처럼 `←→` 는 작업 전환
- `--plain`/비 tty 출력은 날짜 필터 없이 전체를 그대로 출력(종전과 같음). 도움말에 한 줄 추가
- 테스트: `route_test.go` (날짜 파싱·이동 범위, 날짜 줄 키 동작, 빈 날짜, 날짜 선택 후 상세 진입)

## [0.4.7] - 2026-10-08

### 추가
- **TUI `g` / `auto_setup request recheck <jobid> <yml|*>` (서버 상태 수동 재확인)**: 완료를 제외한 모든 호스트(설치중·정체·실패 포함)를 단계와 상관없이 즉시 확인한다. ① os8_mgmt 에서 준비확인 → 응답하면 route=local(os6 로 남아 있던 호스트도 복귀), ② 무응답이면 os6_mgmt 경유 → 응답하면 route=os6, ③ 둘 다 무응답이면 접속불가(상태 불변, 로그 `[!] 재확인(g) 접속불가·미응답 N대 (job …): 호스트…`). 응답한 호스트는 준비확인 결과를 바로 반영(이미 READY 인 호스트는 경로만). 전체 보기(`a`)에서는 job 전체. `os6_mgmt`·`os6_gossh` 미설정이면 ① 만 수행. 요청은 `ReqRecheck`(`requests.go` `recheckHosts`), 화면 힌트·도움말에 `g 재확인` 추가(골든 갱신)
- **LDAP 백업·확인도 os6_mgmt 경유**: 전달 시점 LDAP 수집이 os8 에서 안 되는 local 호스트(ssh 불가 등)는 같은 호출에서 os6_mgmt 경유로 한 번 더 수집하고 route=os6 로 전환(`경로 전환: … (LDAP 백업 …)`). 설치 후 binddn 확인(Apply)도 os8 무응답이면 os6 로 재시도하고 같은 경로로 복원. 수집 불가 호스트는 사유를 로그로 남긴다 (`[!] LDAP 백업 불가: <호스트> (job …) - <사유>`). `os6_mgmt`·`os6_gossh` 가 없으면 종전과 같음
- 테스트: `route_test.go`(g 키·요청 인자, 경로 판별 순서, 거부·os6 미설정), `ldapbk_test.go`(os6 재수집·Apply 폴백, 가짜 gossh 에 `STUB_NOLOCAL`). 골든 갱신(힌트 줄)

## [0.4.6] - 2026-10-08

### 수정
- **수동 run 전 경로 재판별을 양방향으로**: 설치 중 `route=os6` 로 붙었다가 이제 os8 에서도 직접 응답하는 호스트는 수동 run 직전에 `route=local` 로 되돌린다. 종전(0.4.5)에는 local → os6 만 바꿔 `os6` 로 남은 호스트가 os6 gossh 래퍼를 거치며 체크 출력이 비는 `no_output`(total=1ea OK=0ea) 이 났다. 로그: `경로 전환: <호스트> os6 → local (수동 run 전 확인: os8 직접 응답 …)`. os8 무응답·os6 응답이면 os6 유지, `os6_mgmt`·`os6_gossh` 미설정이면 기존 동작
- 테스트: `route_test.go` (os6 로 남은 호스트가 os8 응답 시 local 로 복귀). Go 전체·e2e c,f 통과

## [0.4.5] - 2026-10-08

### 변경
- **수동 run 전 경로 재판별**: 수동 실행(`c`/`request manual-run`) 직전에 그룹 호스트를 한 번 확인해, os8 에서 응답하지 않는데 os6_mgmt 경유로는 응답하는 호스트는 route=os6 로 바꾸고 그 run 이 os6 gossh 래퍼를 쓰게 한다. 경로가 local 로 남은 정체 호스트가 "정상 접속되는데도 체크가 안 되던" 문제 대응 (`os6_mgmt`·`os6_gossh` 가 있을 때만, 없으면 기존 동작). 로그: `경로 전환: <호스트> → os6 (수동 run 전 확인 …)`
- 테스트: `route_test.go` (os6 재판별, 미설정 시 추가 확인 없음). 0.4.4 변경과 함께 Go 전체·e2e b,c 통과

## [0.4.4] - 2026-10-08

### 변경
- **정체 호스트 수동 재시도(`r`)**: `r`(또는 `auto_setup request refresh`)이 데몬에 즉시 ping·준비확인을 요청할 때, 정체(installStuck 초과) 호스트는 ping 상태·경로 판별 결과와 상관없이 이번 준비확인에 강제로 포함해 다시 확인한다 (os8 에서 ping 정보가 없거나 경로가 틀려 갱신이 멈춘 경우). 로컬 무응답이면 기존 규칙대로 os6_mgmt 경유로 한 번 더 확인하고, 응답하면 부팅확인·READY 등 일반 흐름으로 진행. 로그: `수동 재시도(r): 정체 호스트 N대를 즉시 재확인`
- **수동 run 정상 실행 시 완료 처리**: 수동 run(`c`/`request manual-run`)이 정상 종료되고 결과(postapply)가 나온 호스트는 현재 단계(정체·설치중 등)와 상관없이 완료(processed) 처리. 이미 완료된 호스트는 건드리지 않음. 로그: `수동 run 으로 완료 처리: …`
- 문법 검사(`go vet`·`go build`)만 수행, 테스트 미실행. 기존 화면·골든 문구는 변경 없음(도움말에 한 줄 추가)

## [0.4.3] - 2026-10-08

### 변경
- **wall 알림 끔**: `newNotifier` 기본값을 `nopNotifier`(아무것도 보내지 않음)로 변경. 자동 run·수동 run 완료 시 서버 전체 터미널에 메시지가 뜨지 않는다. 결과는 TUI·`auto_setup code NNNN`·로그(`run 완료: … code NNNN`)로 확인. wall 코드(`notify.go` `wallNotifier`)는 남겨 두었으며 `newNotifier` 한 줄로 되살릴 수 있다
- 수동 run 에서 체크 결과가 없는 호스트(접속불가·미응답)는 **로그**에 호스트 목록을 남긴다: `[!] 수동 run 접속불가·미응답 N대 (job … 그룹 … code …): 호스트…` (wall 에 표시하던 정보의 대체)
- 테스트: `route_test.go`·`requests_test.go` 갱신, `run_e2e.sh` d · `run_e2e2.sh` b8·c1·c3·f4 의 wall 확인을 로그 확인으로 변경

## [0.4.2] - 2026-10-07

### 신규
- `auto_setup done --job <jobid> [--yml <그룹>]` — job(또는 그 그룹)의 **미완료 호스트 전부**를 수동 완료 처리(출처 manual, 이미 완료된 호스트는 건드리지 않음, 대상 호스트 목록을 출력). 진행 중 job(`jobs/<id>.json`)만 대상이며 job 파일은 바꾸지 않는다. 원격 클라이언트(os6_mgmt)에서도 동작
- TUI `a` 키 — **전체 보기**: 한 job 의 모든 그룹(yml) 호스트를 한 표로 보고(그룹(yml) 열 추가, 단계별 합계·`f` 정체·실패 필터 동작), 화면 1·2 어디서나 `a`. yml 이 여러 개로 갈라진 job 을 그룹마다 들어가 보지 않아도 대상 확인 가능. 수동 실행(`c`)은 그룹별 화면에서만
- 테스트: `job_test.go`, TUI 골든 갱신(안내 줄)

## [0.4.1] - 2026-10-07

### 변경
- `notify.go` — wall 을 UTF-8 로케일(`LC_ALL`·`LANG`, 이미 UTF-8 이면 유지 아니면 `ko_KR.UTF-8`)로 실행: cron 데몬은 C 로케일이라 wall 이 한글을 `안` 처럼 8진수로 출력하던 문제
- `notify.go` — `wall -n`(배너 "Broadcast message from …" 와 빈 줄 제거, root 만 가능, 실패하면 일반 wall 로 재시도): 메시지가 두 줄씩 늘어나 보이던 문제
- 수동 실행(TUI `c` · `request manual-run`)이 **그룹 전체 완료를 요구하지 않음**: 미완료 그룹도 전체를 시도하고, 체크 결과가 없는 호스트는 결과 wall 마지막 줄에 `[!] 접속불가·미응답 N대: …` 로 표시(로그에도 건수). 확인 질문에 `미완료 N대 포함` 표시
- 테스트: `route_test.go`(wall 로케일·접속불가 줄), `requests_test.go`·`tui_test.go`·골든·`run_e2e2.sh c` 를 새 규칙으로 갱신

## [0.4.0] - 2026-10-07

os8_mgmt 에서 접속이 안 되는 호스트가 `배포중` 에서 멈추던 문제 — 일반 호스트와 같은 흐름(부팅확인·설치중 → READY → 체크 → 완료)으로 진행하도록 os6 경유 자동 전환. 폐쇄망 설정 가이드·업데이트 스크립트 추가.

### 변경
- `daemon.go` `ping()` — route=local 인데 os8 로컬 ping 무응답이면 다음 ping 부터 `both`(로컬 ICMP + os6 probe) 로 확인, os6 가 응답하면 route=os6 로 전환(로그 `경로 전환: <호스트> local → os6`). 종전에는 첫 ping 에서만 경로를 정해 설치 중 os8 에서 안 보이게 된 호스트·`os6_mgmt` 를 나중에 채운 경우 계속 `배포중` 이었음. 전환은 local → os6 한 방향
- `daemon.go` `check()` — os8 준비확인(gossh) 무응답인 local 호스트는 같은 주기에 os6_mgmt 경유로 다시 확인, 응답하면 route=os6 (`os6_mgmt`·`os6_gossh` 둘 다 있을 때). os_check run 도 os6 경로로 실행
- `icmp.go` — Pinger 경로 `both`, 결과에 `Via`(local/os6). 로컬 응답 우선, 없으면 os6 정보, os6 정보도 없으면 로컬 무응답
- TUI `r` 키 — 화면 다시 읽기 + 데몬에 **즉시 ping·준비확인 요청**(새 요청 종류 `refresh`, 5초에 1회). 원격 클라이언트(os6_mgmt)에서도 동일. CLI `auto_setup request refresh`
- 비고 `ping 정보없음` / `ping 정보없음(os6 probe 확인)` — ping 정보가 1분 이상 없을 때(os6 probe 세션 접속 실패 등) 단계가 멈춘 이유 표시. 호스트 JSON 에 `no_ping`(omitempty) 추가
- 도움말 — `%` 는 완료 대수/전체(설치 중 0% 는 정상) 설명 추가

### 신규
- `conf/setup_guide.sh` — 빈 변수 9개 입력 가이드(역할 os8/os6 선택 → 설명·예시·현재값 → 검증 → `conf/auto_setup.conf` 기록 → os8 이면 setup.sh 이어서 실행). git-upload-sk 틀로 생성
- `conf/update_v0.4.0.sh` — 현장 사본 `conf/auto_setup.conf` 업데이트(현장 값·주석 보존, 앵커 불일치 시 중단·파일 불변, CRLF 자동 처리, `--dry`/`--undo`). 이번 버전 변경은 `os6_mgmt` 위 안내 주석 1줄, 신규 변수 없음
- `conf/vars.manifest` — 가이드·업데이트 공용 변수 목록(설명·예시·역할·검증)
- `test/run_setup_scripts.sh` — 가이드·업데이트 스텁 검증 25건 (Rocky 8 / CentOS 6 bash 4.1)
- 테스트: `route_test.go` (local→os6 ping 전환, os6 없으면 종전 동작, 준비확인 대체, refresh 즉시 수행, mergeBoth, no_ping, TUI r 요청)

### 수정
- `setup.sh`/`build_os6.sh` — PATH 에 go 가 없어도 `/usr/local/go/bin`, `/opt/go*/bin` 을 찾아 사용(비로그인·csh 셸에서 "go 를 찾을 수 없습니다" 방지). `build_os6.sh` 는 `/opt/go1.20` 이 있으면 우선 사용하고 사용한 go 버전을 출력.
- `auto_setup_os6` 재빌드(go1.20.14 정적, 변수 빈 값), git 실행 권한(+x) 기록
- `.gitignore` — 업데이트 백업 `conf/auto_setup.conf.bak.*`, `conf/.update_journal` 제외

### 주의사항
- 설치된 데몬은 옛 바이너리이므로 업데이트 후 `bash setup.sh` + `auto_setup --restart` 필요. os6_mgmt 의 `auto_setup` 도 새 `auto_setup_os6` 로 교체
- os6 경로는 os8_mgmt → os6_mgmt `ssh -o BatchMode=yes`(키 인증) 가 되어야 동작

## [0.3.0] - 2026-10-04

### 신규
- `conf.go`, `conf/auto_setup.conf.example` — main.go 의 빈 변수 9개를 **설정 파일**(`키=값`)로 채움. 위치(먼저 발견된 하나): `$AUTO_SETUP_CONF/auto_setup.conf` → `<실행 파일 디렉터리>/conf/auto_setup.conf` → `/etc/auto_setup/auto_setup.conf`. 빌드 때 `-X` 로 넣은 값이 우선하고 설정 파일은 빈 변수만 채움(알 수 없는 키·잘못된 줄 무시). 빌드가 불가능한 환경에서 빌드 완료 바이너리(`auto_setup_os6`)를 설정 파일만으로 원격 클라이언트로 쓸 수 있음
- `setup.sh` — `conf/auto_setup.conf` 가 있으면 `/etc/auto_setup/auto_setup.conf` 로 설치(0600)
- 저장소에 빌드 완료 `auto_setup_os6`(go1.20 정적, 변수 빈 값) 포함, 실제 `conf/auto_setup.conf` 는 `.gitignore` 제외
- 테스트: `conf_test.go`

## [0.2.1] - 2026-10-03

LDAP 비교 기준을 bindpw 해시에서 binddn 의 uid 값으로 바꾸고, 복원 때 bindpw 가 어떤 명령줄(ps)에도 실리지 않게 autofs 공유경로 경유로 변경.

### 변경
- `ldapbk.go` — 비교: 새 OS `ldap.conf` 의 `binddn uid=<값>,…` 에서 uid 값만 추출(키워드 대소문자 무관·공백/탭·따옴표 제거·여러 binddn 줄은 uid 가 있는 첫 줄)해 백업본의 같은 값과 비교. 원격 확인 명령은 uid 한 줄만 출력하며 bindpw 를 읽지도 출력하지도 않음(sha256 해시 비교 코드 제거). 같으면 `same`(생략), 다르면 `diff`(복원), 한쪽이라도 읽지 못하면 `na`(`binddn 확인불가`, 복원 안 함)
- `ldapbk.go` — 복원: 2중 base64 명령줄 전송 제거. `ldap_share_dir` 가 있으면 `<ldap_share_dir>/.as_ldap_<랜덤 hex>/<호스트>/` 를 0700·파일 0600 으로 만들어 백업 세트를 두고, gossh 명령에는 경로·mode·owner·group·서비스명만 실음(대상이 `cat` 으로 읽어 원 mode/owner 로 `mv`, `restorecon`, 서비스 재시작). 먼저 `[ -r ]` 로 접근 확인(실패 시 `공유경로 접근 실패`), 복원 후 binddn uid 재확인(`복원 후 binddn 불일치`), 성공·실패 모두 공유 임시 디렉터리 즉시 삭제. 복원 크기 상한(`ldapMaxRestoreBytes`) 제거
- `ldap_share_dir` 가 비어 있으면 자동 복원 안 함: `Applied=false`, `Reason="수동 복원 필요(ldap_share_dir 미설정, 백업: <경로>)"`, 비고 `LDAP 수동 복원 필요`
- 표시 문구: 비고 `LDAP binddn 동일` / `LDAP 복원` / `LDAP 수동 복원 필요` / `LDAP 적용실패` / `LDAP binddn 확인불가`, 데몬 로그 `LDAP 확인: <host> (job <id>) binddn=same|diff|na applied=…` (uid 값은 로그에 남기지 않음). JSON 키 `bindpw` 는 호환을 위해 유지(값 의미만 binddn 비교 결과)
- 새 빈 변수 `ldap_share_dir`(main.go 는 9개). 테스트: `ldapbk_test.go` 갱신(uid 파서 표·awk/Go 동일성·공유경로 0700/0600·삭제·수동 복원 필요·접근 실패·비밀 미노출), `test/run_e2e2.sh e` 갱신(same/diff/변수 비움/비밀 grep 0건), `test/lib_e2e.sh` 변형 빌드 `auto_setup_share`

### 주의사항
- `ldap_share_dir` 는 os8_mgmt·대상 서버(·os6_mgmt)가 같은 경로로 보는 autofs 이고 대상 root 가 읽을 수 있어야 함(no_root_squash). 사용 후 자동 삭제되며 명령줄(ps)에는 bindpw 가 실리지 않음. 백업(`ldapbak/`)은 여전히 자동 삭제되지 않으므로 수동 `rm -rf`

## [0.2.0] - 2026-10-03

양방향 동일 실행(os8_mgmt 단일 원본) · 완료기록 · LDAP 백업/복원 · 이중체크 · 상태 TUI. 1차(0.1.0) 동작·하위명령은 그대로 유지.

### 신규
- `client.go` — 원격 클라이언트 모드: `os8_mgmt` 가 채워진 빌드는 status/snapshot/code/cancel/done/request·리포트를 gossh 원샷(`gossh -pm -script -w`)으로 os8_mgmt 에서 실행하고 stdout/stderr/종료코드를 로컬 실행과 같게 복원(원격에서 base64 로 실어 gossh 의 줄 다듬기 영향 제거). `SnapshotSource`(로컬/원격) 인터페이스
- `ctl.go` — `--start|--stop|--restart`(`start|stop|restart` 도 허용), `snapshot`·`done`·`request` 명령, 색 메시지(`[O]/[!]/[X]`, `NO_COLOR` 존중). 원격 클라이언트에서 데몬 명령은 `[X] 데몬은 os8_mgmt 에서만 실행합니다` 로 거부
- `paths.go` — `os6_os_check_sh` 해석(명시값 > `os6_mgmt` 가 있으면 `os_check_sh` 폴백 > 없음, `"-"` 면 2차 체크 끔), `awx_dir`/os_check 옆 `dhcp.sh` 링크 후보
- `model.go` — 호스트 단계(대기·배포중·설치중·부팅확인·READY·LDAP확인·체크중·2차체크·완료·정체·실패), 정체 판정(`installStuck` 60분), 그룹(yml) 집합, `Snapshot`/`BuildSnapshot`
- `tui.go` / `render.go` / `tty_linux.go` / `tty_other.go` / `tui_hook.go` — 무인자 `auto_setup` TUI(↑↓ 이동, ←→ job 전환/복귀, Enter 세부, `f` 정체·실패 필터, `c` 수동 OS 체크 y/n, `r`, `?`, `q`/Esc), 순수 렌더 함수 + 골든 테스트(색 on/off, 폭 80/120), `--plain`, 색 규칙(`NO_COLOR`·비 tty 자동 off), 로컬 2초/원격 5초 갱신
- `done.go` — 완료기록(`done/<host>`, 한 줄 `epoch user sha256 source`) 인정 규칙(전달 이후·마지막 부팅 이후, 미래 +5분 초과 거부), `done/applied/` 이동, 수동 `auto_setup done`
- `requests.go` — 요청 채널 `requests/<epoch>_<kind>.req`(manual-run / cancel), 5초 주기 수거, `active/`·`rejected/`(reason= 줄)
- `ldapbk.go` — LDAP 백업(전달 시점, `ldapbak/<jobid>/<host>/` 0700/0600)·READY 후 bindpw 해시 비교·백업 세트 복원. OS7 이하 nslcd.conf+ntp.conf / OS8+ sssd.conf+chrony.conf / 공통 ldap.conf·resolv.conf, s4 규칙 환경변수 `AUTO_SETUP_LDAP_S4_PREFIX`·`AUTO_SETUP_LDAP_S4_SERVICES`
- `second.go` — 2차(이중) 체크: 1차 후 route=local 의 접속불가·FAIL 호스트만 os6_mgmt 에서 `os_check -auto` 1회, 결과 tar 회수 → `runs/<code>/second/`, code 에 `### 2차 체크 (os6_mgmt)` 섹션(최종 판정 = 1차 OK 또는 2차 OK)
- 테스트: `*_test.go`(client·ctl·done·ldapbk·model·render·requests·second·tui·paths·daemon2), `testdata/tui_*.golden`, `test/run_e2e2.sh`(a 양방향 동일성 / b 완료기록 / c 요청·TUI / d 데몬 제어 / e LDAP / f 2차 체크, 191건), `test/lib_e2e.sh`
- 최상단 빈 변수(커밋엔 빈 값): `main.go` `os8_mgmt`·`os8_autosetup`·`os6_os_check_sh`·`awx_dir`(main.go 는 1차 4개 + 2차 4개 = 8개), os_check `auto_done_dir`·`auto_done_host`, 01 `auto_setup_host`

### 변경
- `main.go` — CLI 확장(무인자 리포트, `--plain`, `-h`, `--start|--stop|--restart`, `snapshot`, `done`, `request`), 상수 `installStuck`
- `state.go` / `daemon.go` — queue 그룹 줄(`yml= infra= os= boot= splunk= hosts=`, `all=`) 파싱(그룹 줄 없는 구버전 01 은 `(all)` 단일 그룹), Job JSON 확장(`groups`, `all_yml`, 호스트 `stage`/`ldap`/`second`/`done_src` 등, 기존 키 유지), pid 파일(`auto_setup.pid`), LDAP·2차·완료기록·요청 단계 연동
- `runner.go` — dhcp.sh 링크 후보를 `paths.go` 의 `dhcpCandidates` 로 해석
- `test/manual_check.sh` — 11~17단계 추가(상태 TUI·--plain, 데몬 제어, 양방향 동일성, 완료기록, LDAP, 2차 체크, 정리), `test/run_e2e.sh` 85건
- `사용법.txt` — 원격 클라이언트·완료기록·TUI 명령 추가

### 주의사항
- 데몬·상태는 os8_mgmt 에만 둔다. os6_mgmt 에서 데몬 명령(`--start/--stop/--restart/daemon/ensure`)은 거부된다
- autofs 로 os_check 를 공유하면 `auto_done_dir` 은 비우고 `auto_done_host` 만 채울 것(os6_mgmt 에서 `auto_done_dir` 이 채워져 있으면 os6 로컬에 기록되어 데몬이 못 봄)
- (0.2.0 당시. 0.2.1 에서 공유경로 경유로 변경되어 명령줄 노출 없음) LDAP 복원 때 파일 내용(bindpw 포함)이 base64 로 gossh 명령줄에 실렸음. 백업(`ldapbak/`)은 자동 삭제되지 않으므로 수동 `rm -rf`. 로그·snapshot·code·wall·TUI 에는 bindpw 가 남지 않음
- 2차 체크는 설정(set)을 한 번 더 적용하는 부작용이 있음 → 끄려면 `os6_os_check_sh="-"`
- 알려진 제한: `/tmp/auto_setup` 소유자 검증 없음, LDAP Apply 직렬 처리, 정리 정책 없는 디렉터리(ldapbak, requests/rejected, jobs/done, runs/*/second), `AUTO_SETUP_NOW` 는 테스트 전용, 원격 모드 gossh 시간 제한 없음
- 설정 변경 스크립트이므로 실행 후 랜덤한 서버 몇 대를 확인해 실제로 변경되었는지 검증 필수
- 기존 01·os_check 사용법 불변(빈 변수면 100% 동일)

## [0.1.0] - 2026-10-02

### 신규
- `main.go` — 최상단 빈 변수 4개(os6_mgmt, os6_gossh, os6_autosetup, os_check_sh) + CLI 분기 (daemon/ensure/code/status/cancel/probe) + 조정 가능 상수(pingInterval/downMisses/checkInterval/bootWait/lateWait/queuePoll)
- `daemon.go` — 데몬 루프: queue 수거(5초 주기) → 경로 판별(로컬/os6) → ping 감시(10초, raw 소켓) → 준비확인(30초, gossh -pm) → 7분/3분 타이머 → run 큐
- `state.go` — Job/Host 구조체, JSON 원자 저장(/tmp/auto_setup/jobs/*.json) 및 복원(재기동 후 이어하기)
- `icmp.go` — raw 소켓 ping: echo id+seq 로 호스트 매핑, 주기마다 전체 호스트 송신, 수신 고루틴이 응답 기록
- `probe.go` — probe 하위명령(os6_mgmt 쪽, stdin 호스트 목록 → `host up|down` 출력) + os6 장기 ssh 세션 관리(자동 재시작, 끊김 복구)
- `check.go` — 준비확인: 후보(미처리 && ping up && 설치 감지 또는 기동 후 1회) 에 gossh -pm -script "cat /proc/uptime" 실행, uptime 초 < (지금 − 전달시각) && anaconda 아님 → READY
- `runner.go` — os_check 실행(-auto): 1차=전체 목록, 재실행=미처리 호스트만, os6 포함 시 PATH 앞에 gossh 래퍼 삽입. code 파일(codes/<code>.txt) 생성: 결과 리포트 + 설정체크 FAIL/NO FAIL + LDAP/SPLUNK/커널 블록 + 원본 경로. wall 으로 완료 호스트 + code 공유
- `ensure.go` — ensure 하위명령: /tmp/auto_setup/auto_setup.lock flock 시도 → 잡혀있으면 종료(살아있음), 아니면 setsid daemon 백그라운드 기동
- `log.go` — /tmp/auto_setup/auto_setup.log 관리(상태 변화·실행·오류만 기록, 주기 반복 로그 없음, 10MB 롤오버 1개 유지)
- `notify.go` — wall 로 알림(root 권한)
- `iface.go` — Pinger/Checker/Runner/Notifier/Clock 인터페이스 (테스트 가짜 주입용)
- `*_test.go` — 단위 테스트(state_test, daemon_test, icmp_test, probe_test, check_test, runner_test): 정상 흐름·7분 규칙·재기동 복원·중복 전달 등 가짜 시계·pinger·checker 사용
- `test/manual_check.sh` — 대화형 점검: 단계별 설명·실행·확인(Enter 대기), `-y` 자동, `-s N` 단계 지정, `--install` 설치까지 포함
- `test/run_e2e.sh` — 목록 E2E: 스크래치 사용, 스텁 gossh/ssh/wall, 01 복사본 실행 → queue 생성 → 데몬 수거 → os_check 실행 → code 확인 → wall 수신
- `test/` 스텁 — `gossh`(목업 uptime 반환), `ssh`(목록 stdin 받고 probe 시뮬레이션), `wall`(메시지 파일 저장)
- `setup.sh` — 빌드 + /usr/local/bin 설치 + cron 매분 ensure 등록 + /etc/tmpfiles.d/auto_setup.conf 등록 (멱등)
- `build_os6.sh` — CGO_ENABLED=0 정적 빌드(Go 1.20, GOOS=linux GOARCH=amd64), auto_setup_os6 생성
- 문서: `README.md` (설치·사용·옵션), `CHANGELOG.md` (변경 이력), `ARCHITECTURE.md` (폴더·함수 역할), `WORKFLOW.md` (흐름도 mermaid), `workflow.svg` (흐름도 SVG), `PR_CHECKLIST.md` (검증 항목), `사용법.txt` (명령어 빠른 참고)
- CI: `.github/workflows/ci.yml` (setup-go + go vet/test/build + bash -n + shellcheck, working-directory 지정)

### 보정사항 (계획서 §8 결정사항 및 리뷰 반영)
- `daemon.go`: cron 실행 시 PATH 부족으로 gossh 미발견 → ensure 에서 `/usr/local/go/bin` 을 PATH 앞에 추가
- `probe.go`: os6 장기 세션이 반복적으로 로그를 남기면서 10MB 로그 롤오버 발생 → probe 루프에서 이미 up/down 인 호스트는 로그하지 않기 (변화만 기록)
- `runner.go`: os_check 실행 중 오류(예: os_check_sh 미발견) → 대기 후 재시도, 그 사이에 같은 오류 메시지가 반복 출력 → lastRunErr 추적해 동일 오류는 로그하지 않기
- `daemon.go`: 진행 중 run 이 중단된 후 재시도하면 같은 code 를 재사용할 수 있음 → run 큐 진입 시 newCode() 로 신규 code 생성(기존 사용 code 는 덮어쓰지 않음)

### 주의사항
- 테스트 값은 스크립트 본체에 들어 있지 않으며, `test/` 하네스가 스크래치 복사본에만 값을 주입 (빌드 시 `-ldflags -X` 또는 환경변수 `AUTO_SETUP_DIR`)
- 설정 변경 스크립트이므로 실행 후 랜덤한 서버 몇 대를 확인해 실제로 변경되었는지 검증 필수
- 기존 01·os_check 사용법은 절대 변경되지 않음 (01 프롬프트·출력 동일, os_check 인자 없이 실행 시 100% 동일)
- 무기한 감시 작업이 많으면 `auto_setup cancel` 로 정리하거나 `/tmp/auto_setup/jobs/done/` 을 확인하세요
