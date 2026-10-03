# CHANGELOG

## [Unreleased]

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
