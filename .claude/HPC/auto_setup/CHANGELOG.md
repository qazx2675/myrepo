# CHANGELOG

## [0.8.5] - 2026-10-16

### 수정
- **g(재확인)·수동 run 이 "job 을 찾을 수 없습니다" 로 끝나던 경우 복구**: 화면 목록에는 보이는데(jobs/<id>.json 은 있음) 데몬 메모리에 없는 job 이면 jobs/ 파일에서 다시 올려 확인한다 (`adoptJobFile`, 로그 `job 복구: …`). 더 새로운 job 이 이미 가져간 호스트는 되돌리지 않는다. 이전에는 이 경우 g 결과 창이 `job 을 찾을 수 없습니다` + `상태: 완료` 로만 나와 실제 사유(os6 경유 확인 결과)를 볼 수 없었다

### 변경
- **gossh 작업(`s`)의 호스트 붙여넣기는 Ctrl+D 로만 끝낸다**: 빈 줄·`.` 의 엔터는 입력을 끝내지 않고 무시한다 (엑셀에서 복사해 붙여넣을 때 빈 행이 있어도 계속 입력). 안내 문구 `끝내기: Ctrl+D 만` / `Ctrl+D 입력 끝`. 신규 설치 양식(`n` of newhost)의 붙여넣기는 종전대로 빈 줄·`.`·Ctrl+D
- **gossh 작업 목록에서 Enter 하면 그 실행의 출력을 보여 주면서 아래에 1~6 메뉴를 둔다**: 이전에는 메뉴 한 줄만 보였고 출력은 `4 결과 보기`(less)로만 볼 수 있었다. 출력은 끝(요약)부터 보이고 ↑↓ PgUp/PgDn 으로 스크롤, 5000줄 넘으면 끝쪽만(전체는 `4`). 보관 기간(7일)이 지났거나 출력이 없으면 사유를 보여 준다. 메뉴 동작 1~6 은 그대로

## [0.8.4] - 2026-10-13

### 변경
- **`auto_setup -user <이름>` 에서 화면 1 의 `a` 는 전체 user 보기 토글**: 처음에는 해당 user 의 job 만 보이고, `a` 를 누르면 다른 user 의 job 도 모두 보인다(머리 `[user: alice · 전체 user 보기]`, 안내 줄 `a 내 job 만`). 다시 `a` 로 내 job 만. `-user` 가 없으면 `a` 는 종전처럼 선택한 job 의 모든 그룹 전체 보기. `-user` 일 때 그룹 전체 보기는 Enter 로 들어간 화면 2 의 `a`. x·w·m 의 user 자동 선택은 그대로 지정한 user

## [0.8.3] - 2026-10-13

### 변경
- **gossh 작업(`s`)의 호스트 목록 파일 위치**: `/tmp/<3자리>` → `/tmp/auto_setup/lists/<3자리>` (데이터 디렉터리 `AUTO_SETUP_DIR` 아래 `lists/`). `/tmp` 에 3글자 파일이 흩어지지 않고 auto_setup 상태와 한곳에 모인다. 화면·확인 문구·`gossh -w <경로>` 안내는 전체 경로로 표시한다. 이미 만든 `/tmp/<3자리>` 는 그대로 두며(기록의 호스트 목록 사본 `work/<user>/lists/` 로 재실행 가능) 7일 정리 대상은 새 위치 파일이다
- 참고: auto_setup 의 모든 상태(jobs·queue·done·codes·runs·work·newhost·changes·log)는 `AUTO_SETUP_DIR`(기본 `/tmp/auto_setup`) 아래에 있다. `/tmp` 를 비우면 진행 중 job 추적·완료기록·작업 기록이 사라진다 (설정은 `/etc/auto_setup/auto_setup.conf` 또는 실행파일 옆 `conf/` 라 영향 없음). 영구 보관하려면 환경변수 `AUTO_SETUP_DIR` 로 `/tmp` 밖 경로를 지정

## [0.8.2] - 2026-10-13

### 수정
- **os6_mgmt 경유 준비확인·재확인(g)이 실패하면 실제 사유를 보여 준다**: `os6 준비확인 실패: exit status 255` 만 나오던 것에 ssh 가 낸 출력(예 `Permission denied (publickey)`, `Could not resolve hostname`, 원격 bash 오류)의 첫 3줄을 덧붙인다 (`diagHead`). g 진행 창의 `os6_mgmt 확인 호출 실패: …` 와 `auto_setup.log` 에 그대로 나온다
- **설정체크만(t) — 체크스크립트가 `-auto-check` 를 모르면 실행하지 않고 사유를 알린다**: `-auto` 만 아는 스크립트(config_check.sh 등)에 `-auto-check` 를 주면 user 선택 메뉴에서 멈추고 결과 창에는 아무것도 나오지 않았다. 요청을 거부(`requests/rejected` 의 reason)하고 Runner 도 실행 전에 막는다: `os_check_sh 가 -auto-check 를 지원하지 않음 (설정체크만 t 불가 — c 로 실행하거나 …)`. 스크립트를 읽을 수 없으면 막지 않는다
- **결과 리포트가 없을 때 결과 창(code 본문)에 원인을 보여 준다**: 대상이 모두 접속불가이거나 체크스크립트가 중간에 멈췄을 때 `[!] 결과 리포트가 없습니다 …` 안내와 체크스크립트 출력의 마지막 12줄을 싣는다 (예전에는 로그 경로를 열어 봐야 user 선택 화면을 볼 수 있었다)

## [0.8.1] - 2026-10-13

### 수정
- **신규 설치 양식 → AWX 실행에서 프로파일을 고르지 않으면 자동 N 을 적용하지 않는다**: v0.8.0 은 프로파일 없이 이어가도 `AWX_AUTO_NODEINFO=n` 을 넘겨 nodeinfo 가 자동 N 이었다. 이제 `AWX auto 실행? [y/n]` 에서 `y` 로 프로파일을 고른 경우에만 nodeinfo N 으로 실행하고, `n`(또는 프로파일 없음)은 자동 응답이 없는 일반 실행이라 01 의 nodeinfo 질문에 직접 응답한다 (nodeinfo 를 쓸 수 있도록). user·대상 목록·`AWX_DNS_CHECKED` 자동 입력은 그대로. 질문 화면 머리·저장 화면 안내 줄·도움말 문구 갱신, 골든 `tui_newhost_*` 안내 줄 1줄 갱신
- 01 의 `AWX_AUTO_NODEINFO` 단독 사용(awx_script `[Unreleased]`)은 auto_setup 이 더 이상 쓰지 않는다 (수동 실행용으로만 남음)

## [0.8.0] - 2026-10-13

### 추가
- **항목별 인라인 메뉴 `h`·`w`** (`menu_tui.go`): `m` 변경 메뉴와 같은 방식 — 화면을 바꾸지 않고 키 안내 줄 아래에 한 줄(좁으면 항목 단위 줄바꿈)로 열리고, 글자 또는 번호로 고르며 `Esc`/`q` 로 메뉴만 닫는다.
  - `h` 호스트 작업 (화면 2): `c` 체크+수정 · `t` 체크만 · `g` 재확인 · `d` 수동 완료 · `v` 결과 (번호 1~5). 전체 보기(`a`)에서는 `g`·`d` 만 표시. 화면 1 에서는 `h` 가 없음
  - `w` AWX: `1` AWX 실행 · `2` 신규 설치 양식. **`ww` / `w` `Enter` / `w` `1` 이 예전의 `w` 동작**. 3번 자리(킥스타트 파티션 변경)는 예정이라 항목을 만들지 않음
  - 예전 단축키 `c` `t` `g` `v` `d` `x` 는 **숨은 단축키로 그대로 동작** (메뉴는 같은 핸들러를 호출). 안내 줄에서만 빠지고 `?` 도움말에 표시. 안내 줄 둘째 줄: ` h 호스트 작업  w AWX·신규설치  m 변경  s gossh 작업  x 단독 체크`
- **신규 설치 양식** (`w`→2 / `auto_setup newhost [-user <이름>]`, os8_mgmt 에서만, `newhost*.go`): nodeinfo 를 쓸 수 없는 신규 설치 서버용. `hostname ip mac` 붙여넣기(구분자·순서 무관, MAC 4형식 → 소문자, 오류·중복은 고칠 때까지 진행 불가, `d`/`e` 로 고치기) → 템플릿 지정(conf `newhost_template_1~9` + user 별 `+` 추가, 범위·패턴·행 번호·`*`, 열 수정 `svr005 os=9.6`, 지정 없는 행은 이력 추천) → DNS 점검(a/n/s/q) → 미리보기 → `awx_dir/<user>.txt` 저장(기존 파일은 `newhost/<user>/backup/` 백업, `history/` 사본) → `AWX 실행할까요? [y/n]`. 저장 뒤 AWX 는 nodeinfo N 강제·대상 목록 자동 입력·DNS 를 점검했으면 `AWX_DNS_CHECKED=1`. 프로파일이 없으면 `AWX_AUTO` 없이 `AWX_AUTO_NODEINFO=n` 만 넘김. CLI 는 tty 필요, 저장했으면 종료코드 0
- **DNS 점검** (`dnscheck.go`): `/etc/resolv.conf` 의 DNS 서버(최대 3)에 직접 A 질의(외부 명령 없이 표준 라이브러리로 UDP/TCP 구현, `/etc/hosts`·getent 는 쓰지 않음, search/ndots 는 glibc 와 같은 순서). 판정 5종(일치·불일치·판단 불가·미등록·조회 실패), 중복 경고 3종(목록·DNS·PTR), 동시 32·질의당 2초·재시도 1회. 신규 설치 양식에서 사용하며 awx_script 01 `[4-1] dns_check` 와 같은 규칙
- **gossh 작업 기록** (`s` / `auto_setup s [-user <이름>]`, `work*.go`): 붙여넣은 호스트로 `/tmp/<소문자 3자리>` 목록을 만들어 gossh 를 실행하고 user 별로 기록. `n` 새 작업(같은 목록이면 재사용 질문) · `Enter` 항목 메뉴(같은 파일로 새 명령·재실행·실패/접속불가만 재실행·결과 보기 `less -R`·메모·호스트 목록) · `/` 호스트 검색. 실행 위치: 대수 ≤ `work_os6_over`(비면 1000) 는 os8 gossh + **접속불가만 os6_mgmt 경유 재실행**, 초과는 **전량 os6_mgmt 경유**(`ssh -t`, `bash -c`), os6_mgmt 에서 열면 대수와 상관없이 os6 gossh 로 바로 실행하고 기록은 os8_mgmt 로 중계(내부 `work-relay`). TUI 를 잠시 떠나 `script -q -f -c` 로 tty 를 유지한 채 실행(출력·색·위험어 확인 그대로)
  - 기록 `<AUTO_SETUP_DIR>/work/<user>/{index.jsonl, lists/, out/, relay/}`(0700/0600): 요약은 영구, 전체 출력(gzip)은 7일·user 당 200MB, `/tmp/<3자리>` 파일도 7일. 데몬이 하루 한 번 정리(`workCleanAll`), 위험어(reboot·halt 등)는 화면 확인 후 gossh 의 확인을 거침
- **세션 user** (`sessionuser.go`): `-user` 없이 실행하면 `s`·신규 설치 양식을 처음 열 때 한 번 고르고 TUI 를 닫을 때까지 기억(헤더 `[user: …]`). CLI `s`·`newhost` 는 시작할 때 묻고 `-user` 가 있으면 묻지 않음. `x`·`w`→1·`m` 은 기존처럼 매번 묻는다. `-user .`·`..` 는 거부
- **새 빈 변수 10개** — `newhost_template_1`~`9`, `work_os6_over` (모두 빈 값, 비면 기존과 같음). `conf/update_v0.8.0.sh`(앵커 `ldap_engine=`, `--dry`·`--undo`), `conf/vars.manifest` 0.8.0(32개), `conf/auto_setup.conf.example`
- **명령·키별 흐름도 `docs/svg/` 31개**(CLI 14 + TUI 17, 라이트/다크 대응) + `docs/svg/README.md` 목록 표. README 의 명령·키 표 행마다 링크
- **CI**: 저장소 루트 `.github/workflows/auto-setup.yml` (paths 필터, Go 1.26.5 `build`·`vet`·`test` + `bash -n`, Go 1.20 문법 호환 job)

### 변경
- **`w` 는 메뉴**가 됨 (위). 안내 줄·`?` 도움말·골든(`tui_*`·`tui_menus_*`·`tui_newhost_*`)을 새 구조에 맞춰 갱신. 닫혀 있을 때 화면 1·2 의 본문은 그대로이고 안내 줄 둘째 줄만 바뀜
- **os6_mgmt 원격 명령은 모두 `bash -c '<명령>'`**(`remoteBash`, `shq` 인용)으로 감싸 보냄 — 준비확인·os_check run·2차 체크·LDAP 백업·복원·원격 클라이언트 중계(`sh -c` → `bash -c`)·변경 엔진·gossh 작업. 로그인 셸이 csh/tcsh 여도 `$(...)`·`${f}` 가 bash 에서 해석된다. 명령에 `!` 가 있으면 tcsh history 치환을 피하려 base64(두 글자마다 `.`)로 전송, os6 gossh 래퍼 2종도 같은 형태. awx_script 01/02 의 os6 경로는 이미 `bash <파일>` 방식이라 영향 없음(점검만)
- 준비확인·`g` 의 os6 ssh 옵션을 다른 경로와 같은 `-o BatchMode=yes -o ConnectTimeout=10` 으로 통일
- 01 은 `AWX_AUTO_NODEINFO`(y|n)를 `AWX_AUTO` 없이도 존중 (awx_script `[Unreleased]` 참고)
- **빌드**: 폐쇄망은 **Go 1.26.5** 로 빌드·검증하고 `go.mod` 는 `go 1.20` 유지 — `auto_setup_os6` 는 Go 1.20.14 정적 빌드(변수 빈 값, 재빌드해 커밋). Go 1.20 문법·표준 라이브러리만 사용(외부 의존성 0)

### 수정
- **① 준비확인·`g` 의 무한 대기**: gossh(os8)·`ssh <os6_mgmt>`(os6) 호출 1회마다 **전체 60초 제한**(`checkTimeout`) — 넘으면 프로세스 그룹(gossh/ssh 와 자식)을 종료하고 받은 출력까지만 해석, 그 호스트는 무응답 + 사유 `시간 초과(60초)`
- **② csh/tcsh 로그인 셸**: os6_mgmt 로그인 셸이 csh/tcsh 면 `$(...)`·`${f}` 가 bash 로 해석되지 않거나 `!` 에서 `Event not found` 가 나던 문제 (위 `bash -c`·base64)
- **③ gossh 출력 해석 통일** (`parseGossh`): 준비확인 `parseCheck` 와 LDAP `splitGossh` 가 따로 해석하던 것을 하나로 — ANSI 색 제거, `호스트: 내용` 분리, `ERROR: 종료코드 N`·`SSH 접속 실패: …` 줄을 사유로 인식, 호스트 이름은 정확히 같은 이름 우선 → 짧은 이름(첫 `.` 앞)으로 맞춤(둘 이상과 겹치면 맞추지 않음). 출력이 있는데 호스트를 하나도 못 찾으면 원본 앞 5줄을 `auto_setup.log` 에 `[!] gossh 출력 해석 실패` 로 남기고(LDAP 은 bindpw 보호로 원본 미기록) `g` 결과 상세에 `출력 형식 불일치` 표시
- `os6_gossh` 경로·원격 인자는 모두 `shq` 를 거침 — 공백·`'` 가 있어도 깨지지 않음
- gossh 작업 기록 저장(`workWriteFile`)의 임시 파일 이름을 호출마다 고유하게 — 한 프로세스 안의 동시 저장이 같은 임시 파일을 밟아 rename 이 실패하던 경쟁 상태(race) 수정

### 테스트
- `go test ./...` **398 PASS (7 SKIP)** — Go 1.26.5 (폐쇄망 버전)로 build·vet·test, `auto_setup_os6` 는 Go 1.20.14
- E2E: `test/run_e2e.sh` 86 · `test/run_e2e2.sh` 194 · `test/run_change_os.sh` 173 · `test/run_change_lab.sh` 18 · `test/run_setup_scripts.sh` 54 · awx_script `test/run_tests.sh` 108
- 랩 전용 `tcsh_live_test.go`: `AS_TEST_LOGIN_MGMT=<user@host> go test -run ViaLoginShell .` — 로그인 셸이 tcsh 인 계정으로 진짜 ssh 접속해 준비확인·LDAP `gosshBatch`·2차 체크·gossh 작업·원격 클라이언트를 `!` 포함 명령까지 확인 (환경변수가 없으면 SKIP)
- 신규 단위 테스트: `menu_tui_test.go`·`newhost_test.go`·`newhost_tpl_test.go`·`newhost_tui_test.go`·`dnscheck_test.go`(가짜 DNS 서버)·`work_test.go`·`work_tui_test.go`·`sessionuser_test.go`, `check_test.go` 에 출력 해석·60초 제한(가짜 hang)·`bash -c` 인용 추가

### 주의사항
- **현장 업데이트**: `bash conf/update_v0.8.0.sh --dry` → 실행(새 변수 10개는 Enter 로 빈 값 유지) → `bash setup.sh` → `auto_setup --restart` → os6_mgmt 의 `auto_setup_os6` 교체. 새 동작은 바이너리에 있으므로 `setup.sh`·재기동·os6 바이너리 교체가 필수. v0.7.0 conf 가 먼저 적용돼 있어야 함(앵커 `ldap_engine=`)
- awx_script 의 DNS 점검은 `setup/update_unreleased_dns.sh` (01 만 변경, 현장 값 보존)
- **설정 변경 도구**: 신규 설치 양식은 `awx_dir/<user>.txt` 를 만든다(기존 파일은 백업). 설정 변경 후에는 랜덤 몇 대를 직접 확인하십시오
- 알려진 제한은 README 「알려진 제한」: 프로파일 없는 AWX 경로(01 의 나머지 질문은 직접 응답, 구버전 01 은 nodeinfo 도 N 직접 입력), 원격 인자 128KB, os6 로컬 보관분 미병합, os6 probe 세션 경로의 `!`

## [0.7.3] - 2026-10-12

### 추가
- **`auto_setup -user <이름>`**: 해당 user 의 job 만 보인다(`--admin`·`--plain` 과 함께 사용 가능, `-user=<이름>` 형식도 허용, 이름은 영문·숫자·`.` `_` `-`). 이때 `x`(체크스크립트 단독 실행)·`w`(AWX)·`m`(IP/LDAP 변경·원복) 의 **user 선택 단계를 건너뛰고** 그 user 로 바로 진행한다. 옵션 없이 실행하면 기존처럼 job 이 모두 보이고 user 를 묻는다. 화면 머리에 `[user: <이름>]` 표시. 수동 run(`c`/`t`)은 원래 job 의 user 를 쓴다. 새 파일 `userfilter.go`

### 변경 (진단)
- **재확인(g)·준비확인이 gossh 의 실패 사유를 보여 준다**: 응답이 없는 호스트의 사유(`SSH 접속 실패: …` 등 gossh 가 stderr 로 내는 메시지)를 버리지 않고 재확인 결과의 상세 열에 `os8 무응답 (사유) → os6 무응답 (사유)` 로 표시한다. 접속불가의 원인을 화면에서 바로 구분하기 위함
- os6_mgmt 경유 준비확인의 원격 명령을 `bash -c '…'` 로 감싸 os6_mgmt 로그인 셸 종류에 영향을 받지 않게 했다 (명령 내용은 동일)

### 설명
- 비고의 `LDAP 복원` 은 LDAP binddn 이 백업과 달라 복원이 **적용 완료**된 호스트다. 같은 줄이 노랗게 보이는 것은 비고에 `FAIL`/`실패` 문구(예: `2차 FAIL`)가 있을 때 비고 전체를 노랑으로 칠하기 때문이며 LDAP 복원 실패 표시가 아니다 (LDAP 복원 실패는 `LDAP 적용실패`)
- os6_mgmt 경유 호스트도 LDAP 백업·확인·복원이 os8 직접 접속과 같은 방식으로 실행된다 (v0.4.7~, 공유경로를 os6_mgmt 도 볼 수 있어야 함)

## [0.7.2] - 2026-10-12

### 추가
- **PageUp / PageDown**: 터미널의 `ESC[5~`·`ESC[6~` 를 키로 인식. 화면 1(그룹 목록)·화면 2(호스트표)는 한 화면(높이 − 8행)씩 이동하고, 결과 창·변경 화면(미리보기·결과)은 `b`/`Space` 와 같이 쪽 단위로 스크롤한다. 도움말(`?`)과 결과 창 안내 줄에 `PgUp/PgDn` 표시. 기존 키 동작은 그대로
- 테스트: `pgkeys_test.go`(키 파싱·변경 결과 화면 쪽 이동), 결과 창 골든(`tui_oneoff_*_60.golden`)의 안내 줄 갱신

## [0.7.1] - 2026-10-12

### 추가
- **`d` 수동 완료** (화면 2, TUI): 안내 줄 아래 인라인 프롬프트 `수동 완료 ▶ 1 이 호스트(<호스트명>)  2 그룹 미완료 N대   (Esc 취소)` → 번호 → 확인 줄 `… 을(를) 완료로 처리합니다 — 부팅 시각 검증 없이 인정되고 체크스크립트는 실행되지 않습니다. 계속? (y/n)` (`y` 만 진행). CLI `auto_setup done` 과 같은 동작이며 출처는 `manual`
  - 로컬 데몬(os8_mgmt)이면 `markDone(hosts, manual)`, 원격 클라이언트(os6_mgmt)이면 기존 `done` 중계 (그룹은 `done --job <id> --yml <그룹>`). 결과는 메시지 줄에 `완료 처리 요청: N대 (데몬이 5초 안에 반영)`, 실패는 사유 표시
  - 전체 그룹 보기(`a`)에서는 `1` 만. 이미 완료된 호스트는 호스트 선택지가 없고(그룹만), 호스트·그룹 모두 완료이거나 종료된 작업이면 안내만 하고 프롬프트를 열지 않음
  - 화면 2 안내 줄에 `d 수동 완료` 추가 (분홍), 도움말(`?`)에 설명 추가. `m`·`x`·`w` 와 동시에 열리지 않음. `--plain`·화면 1 은 영향 없음
- **테스트**: `done_tui_test.go`(프롬프트·확인·취소·allView·완료 호스트·종료 job·배타·원격 중계·폭 40~200·골든 `tui_donemenu_*_60.golden`), `anaconda_test.go`

### 변경
- **`m` 변경 메뉴가 인라인으로**: 전체 화면(`renderChangeMenu`)을 없애고, 현재 화면(1 또는 2)을 그대로 둔 채 키 안내 줄 바로 아래에 `변경 ▶ 1 IP 변경  2 LDAP 변경  3 IP+LDAP  4 원복  5 auto.appl 원복   (번호 선택, Esc/m 닫기)` 가 붙음 (켜진 항목만, 좁으면 항목 단위 줄바꿈). 높이 32줄 이상이면 위험 안내·항목별 설명이 이어짐
  - 열려 있는 동안 번호 외의 키는 무시하고 `Esc`/`m`/`q` 는 메뉴만 닫음 (`q` 가 TUI 를 종료하지 않음, 이전에는 `←` 도 닫았으나 이제 무시). 번호를 고르면 흐름(user → … → 결과)은 이전과 같이 전체 화면
  - 닫혀 있을 때 모든 화면은 v0.7.0 과 같음. 푸터가 길어지면 기존 `finish` 방식으로 본문 영역이 줄어듦
  - 골든 `tui_changemenu_*_60.golden` 은 인라인 형태로 재생성 (화면 1·2 + 메뉴)
- **준비확인 anaconda 감지**: ping X 를 보지 못했어도(`SeenDown=false`, 기동 전에 이미 내려갔다 올라온 호스트) anaconda 가 감지되면 `SeenDown=true`·`DownAt=지금` 으로 두고 단계를 **설치중** 으로 올림 (이전에는 대기에 머물렀음. gossh `-pm` 이 `~/.profile` 의 anaconda 로 설치중을 판정하는 것과 같은 기준). 종료된 job 은 단계 불변
- **g 재확인 설명**: 단계가 바뀌면 `단계 대기 → 설치중` 으로 표시 (안 바뀌면 종전처럼 `단계 설치중`)
- **화면 2 골든**: 안내 줄에 `d 수동 완료` 가 늘어 화면 2 골든(`tui_*_{plain,color}_*.golden` 의 detail 부분)이 바뀜. 화면 1 은 바이트 동일

### 주의사항
- **수동 완료는 검증이 없음**: 부팅 시각을 보지 않고 체크스크립트도 실행하지 않으므로 정말 끝난 호스트에만 사용. 확인 줄에서 `y` 가 아니면 아무것도 하지 않음

## [0.7.0] - 2026-10-12

### 추가
- **`m` 변경 메뉴** (화면 1·2 어디서나, os8_mgmt 에서만 실행). 켜진 항목만 표시하고 번호는 고정
  - `1` IP 변경 · `2` LDAP 변경 · `3` IP+LDAP 변경 (둘 다 켜졌을 때) · `4` 원복 (IP·LDAP·둘 다, 켜진 것만) · `5` auto.appl 원복 (LDAP 이 켜졌을 때)
  - 흐름: user 선택 (x 와 같은 메뉴) → [LDAP 계열: infra 메뉴 선택] → 대상 입력 (Ctrl+D) → [LDAP 계열: dry-run 자동 조회] → 미리보기 → `yes` 를 정확히 입력하면 실행 → 결과 화면. `yes` 가 아니면 아무것도 바꾸지 않고 취소
  - 입력은 `hostname ip` (공백·탭·쉼표·`=` 구분 허용, 정규화). GW 는 새 IP 의 마지막 옥텟을 `1` 로 고정. IP 는 ifcfg 만 바꾸고 재시작하지 않음
  - 원복과 auto.appl 원복은 user 의 이전 기록으로 대상을 자동 채움 (수정·붙여넣기로 대체 가능). 원복 종류는 메뉴 4 에서 IP / LDAP / 둘 다 중 고름
- **활성화**: conf `enable_ip_change`·`enable_ldap_change` (y/yes/1/true, 비우면 꺼짐). `auto_setup --admin` 으로 TUI 를 띄우면 conf 와 무관하게 켜짐 (데몬·request 에는 영향 없음). os6_mgmt 원격 TUI 에서 `m` 을 누르면 "os8_mgmt 에서 실행하세요" 안내만 표시
- **엔진 경로**: conf `ip_engine`·`ldap_engine` (비우면 auto_setup 실행 파일과 같은 디렉터리 → PATH). `ip-change-engine`·`ldap-config-engine` 을 **바이너리로 호출**하며 코드는 흡수하지 않음 (두 도구는 계속 독립 사용 가능)
- **변경용 conf 위치**: `ip_change.conf`·`ldap_config.conf`·`assets.txt` 는 auto_setup.conf 를 읽은 디렉터리 (없으면 `/etc/auto_setup`) 에서 읽음
- **실행 경로**: os8_mgmt 에서 먼저 실행하고, 응답 없는 호스트만 os6_mgmt 경유로 재시도 (`os6_mgmt`·`os6_gossh` 설정 시). 이미 route=os6 인 호스트는 처음부터 os6 경유. 경유는 엔진 `-gossh` 에 임시 래퍼 (0700, 실행 뒤 삭제) 를 넘겨 `ssh <os6_mgmt> <os6_gossh>` 로 전달
- **IP+LDAP**: IP 를 먼저 적용하고 IP 성공 호스트만 LDAP 적용. IP 실패 호스트는 결과에 `건너뜀(IP 실패)` 로 표시
- **원복 (메뉴 4)**: 엔진 기본값인 가장 최근 백업으로 되돌림. 대상 목록을 먼저 보여 주고 yes 확인
- **auto.appl 원복 (메뉴 5)**: 대상은 user 의 LDAP 적용 성공 호스트 중 `auto.appl 대기` 인 호스트. 노드의 `/root/auto_appl_restore.sh` 만 gossh 로 실행 (엔진 미사용, 서비스 재시작 없음)
- **기록**: `<state>/changes/<user>/<YYYYmmddHHMMSS>_<kind>.json` (디렉터리 0700, 파일 0600, 호스트별 결과). `auto_setup.log` 에 요약 한 줄. 기록 파일을 만들 수 없으면 실행하지 않음
- **취소**: 실행 중 `Ctrl+X` → `y` (q/Esc 는 무시). 프로세스 그룹에 SIGTERM, 3초 뒤 SIGKILL. 중단되면 "일부 호스트만 변경됐을 수 있음" 을 표시하고 호스트별 결과를 그대로 기록
- **진행 중 job 경고**: 대상이 추적 중인 job 의 호스트이면 미리보기에 빨간 경고만 표시. job·snapshot·데몬은 건드리지 않음
- **설치 (setup.sh)**: [5/6] 두 엔진을 `GOPROXY=off CGO_ENABLED=0` 으로 빌드해 `/usr/local/bin` 에 설치 (소스가 없으면 건너뜀). [6/6] conf 샘플 (`ip_change.conf`·`ldap_config.conf`(600)·`assets.txt`) 을 `/etc/auto_setup/` 에 복사하되 이미 있는 파일은 덮어쓰지 않음
- **conf 예시**: `conf/auto_setup.conf.example` 에 `enable_ip_change`·`enable_ldap_change`·`ip_engine`·`ldap_engine` 4개 추가 (모두 빈 값)
- **os6 엔진 바이너리**: `ip_change/bin/ip-change-engine_os6`, `ldap_setting/bin/ldap-config-engine_os6` (정적, +x). `build_os6.sh` 가 같이 빌드. RHEL 6 에서 엔진을 단독 실행할 때 쓰며, `m` 메뉴는 os8_mgmt 의 엔진을 호출
- **테스트·모의 환경**: `test/demo_change.sh` (실제 엔진 + 가짜 gossh 로 IP·LDAP·원복·auto.appl 원복 모의), `test/run_change_os.sh` (RHEL 7/8/9 fixture 판정, `-no-restart` 검증), `test/run_change_lab.sh` (.58 자기 자신, fixture 경로만 변경), `test/lib_change.sh`·`test/fake_gossh.sh`
- **레이아웃 테스트**: 메뉴 화면 골든 (`tui_changemenu_*_60.golden`), 변경 화면은 폭 60/80/120 에서 잘림 없이 줄바꿈되는지 테스트

### 변경
- **좁은 화면 줄바꿈**: 60칼 이상에서 모든 줄이 `…` 없이 줄바꿈됨. 헤더·job 줄·안내 줄·호스트 행 비고·변경 화면 표가 대상. 그룹 행이 폭에 안 들어가면 2줄 카드형 (1줄 yml 전체 이름, 2줄 구성·대수·막대·경과·%). 창 크기가 바뀌면 (SIGWINCH) 즉시 다시 그림. x·w 화면도 같은 규칙
- **안내 줄·도움말**: 변경 메뉴가 켜졌을 때만 안내 줄에 `m 변경` 한 항목 추가 (색 별도). 도움말 (`?`) 에 `m` 설명 추가
- **main.go**: `--admin` 플래그 (TUI 모드에서만 의미) 와 빈 변수 4개 (`enable_ip_change`·`enable_ldap_change`·`ip_engine`·`ldap_engine`)
- **ldap_setting**: `-no-restart` 플래그 추가 (기본 꺼짐. 플래그를 주지 않으면 생성 스크립트는 종전과 같음). auto_setup 의 LDAP 호출 (dry-run·적용·원복) 에는 항상 붙임
- **go.mod**: `ip_change`·`ldap_setting` 의 `go 1.26.5` → `go 1.20` (Go 1.20/1.25 빌드 가능, 동작 불변)

### 주의사항
- **꺼짐 상태 화면**: 기본 설정 (메뉴 꺼짐) 에서도 60~80칸의 `…` 잘림이 줄바꿈으로 바뀜. 내용·키·동작은 같지만 **화면이 바이트 단위로 같지는 않음**. 골든 `tui_*.golden` 의 차이는 모두 줄바꿈 변화
- **설정 변경 도구**: 적용 뒤 무작위 몇 대에 직접 접속해 반영을 확인하십시오. IP 는 ifcfg 만 바꾸므로 네트워크 재시작·재부팅 뒤에 반영됩니다. 노드 서비스는 재시작하지 않습니다 (LDAP 적용·원복 모두 `-no-restart`)
- **auto.appl**: LDAP 적용 후 새 auto.appl 은 `/etc/auto.appl.bak` 에 대기합니다. 메뉴 5 를 실행해야 반영됩니다
- **bindpw**: `ldap_config.conf` 에만 있고 퍼미션 600, 커밋 금지. 화면·기록·로그·임시 파일에는 남기지 않습니다 (`-print-script` 호출 안 함). 단 LDAP 엔진은 적용·원복 스크립트를 base64 로 gossh 명령줄에 싣기 때문에 os8·os6·대상의 `ps` 에 base64 문자열이 잠깐 보일 수 있습니다 (ldap_setting 의 기존 설계, 이번 버전에서 바꾸지 않음)
- **기존 현장 설정**: `conf/update_v0.7.0.sh` (미리보기 `--dry`, 되돌리기 `--undo`) 가 `auto_setup.conf` 에 새 변수 4개(`enable_ip_change`, `enable_ldap_change`, `ip_engine`, `ldap_engine`, 전부 빈 값)를 추가합니다. 메뉴를 켜려면 적용 후 `enable_ip_change=y` 등을 채우고 `auto_setup --restart` 하십시오 (데몬은 시작 때 한 번만 읽음; TUI 만 `--admin` 으로 임시로 켜는 것은 재시작 불필요)
- **진행 중 job**: 변경 대상이 추적 중인 job 의 호스트이면 IP 반영 뒤 추적이 끊기거나 설치·체크와 겹칠 수 있습니다 (경고만 표시)
- **검증 범위**: RHEL 7·9 실장비 동작은 fixture 검증까지입니다. 실장비 확인 항목은 `사용법.txt` 13절

### 테스트
- Go: `change*_test.go` (메뉴 가시성·`--admin`, 입력 정규화, 경유 폴백, IP→LDAP 건너뜀, 기록 권한·bindpw 미노출, 취소, 줄바꿈 골든), `conf_test.go` (새 키·엔진 찾기·`--admin` 인자), `tui_test.go` (SIGWINCH 즉시 재그리기), 골든 `testdata/tui_*.golden` 갱신 (줄바꿈)
- 모의·랩: `test/run_change_os.sh`, `test/run_change_lab.sh`, `test/demo_change.sh`
- 되돌리기: 태그 `backup-before-v0.7.0` (= origin/master c7906be)

## [0.6.2] - 2026-10-09

### 변경
- **키 안내 줄 자동 레이아웃**: 하단 키 안내가 길어져 읽기 어려워 터미널 폭에 맞춰 나눈다. 한 줄에 다 들어가면 한 줄, 아니면 **보기·이동**(위 줄)과 **작업**(아래 줄: `c` `t` `g` `v` `x` `w`)으로 나누고, 그래도 폭보다 길면 항목 단위로 줄바꿈한다(항목이 중간에서 잘리지 않음). 화면 1(목록)은 `x 단독 체크  w AWX` 를 둘째 줄로 뺐다. 색·키는 그대로. 결과 창·`x`·`w` 화면의 안내 줄도 같은 규칙을 쓴다
- 테스트: `TestHintLines`(폭 12~130 에서 폭 초과·항목 유실 없음), 골든 갱신
- 되돌리기: 태그 `backup-before-v0.6.2` (= v0.6.1, 6da5c55)

## [0.6.1] - 2026-10-09

### 추가 / 변경
- **키 안내 줄 색 구분**: 하단 `↑↓ 이동  ← 목록  f …  c 체크+수정  t 체크만  g 재확인 …` 줄을 항목별로 색을 나눠 읽기 쉽게 했다 — 이동 키 청록, `c`(수정 포함) 빨강, `t` 초록, `g` 노랑, `x` 자홍, `w` 파랑, 나머지 굵게. 색이 꺼지면(NO_COLOR·파이프) 글자는 종전과 완전히 같다. 줄이 길어 잘릴 때 `…` 가 두 번 나오던 `segLine` 버그도 수정
- **`w`(AWX 실행)에 user 선택 + 작업 대상 서버 목록 입력 단계 추가**: auto 여부(프로파일) → user 메뉴(`x` 와 같음) → `작업 대상 서버 목록을 붙여넣은 뒤 Ctrl+D`. 호스트명(공백·쉼표·| 구분)·12필드 줄을 줄 단위로 섞어 입력할 수 있고, 입력한 목록은 `awx_dir/<user>.txt` 를 덮어쓴다(백업 없음). 01 의 `등록 후 확인 : 작업 대상 서버 목록을 붙여넣으세요` 도 같은 호스트명으로 자동 입력된다. 비우고 Ctrl+D 면 건너뜀(기존 `<user>.txt` 사용). 01 에는 환경변수 `AWX_USER`(user 메뉴 건너뜀)·`AWX_VERIFY_FILE`(등록 후 확인 자동 입력)을 넘긴다 (awx_script 0.6.1, 환경변수가 없으면 01 은 종전과 동일)
- 테스트: `awxrun_test.go`(목록 파싱·`<user>.txt` 쓰기·환경변수·건너뜀·Esc), `mode_test.go`(키 안내 색). 골든(색 있는 화면) 갱신

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
