# PR_CHECKLIST

auto_setup 데몬 + CLI 풀리퀘스트 검증 항목. 모든 항목을 확인하고 체크한 후 커밋/푸시하세요.

## 빌드 및 문법 검사

- [ ] `bash -n setup.sh build_os6.sh conf/*.sh test/*.sh` 에러 없음
- [ ] `go vet ./...` 경고 없음
- [ ] `gofmt -l .` 출력 없음 (형식 통일)
- [ ] `shellcheck -S warning setup.sh build_os6.sh test/*.sh` 경고 0건 (os_check·01 도 신규 경고 0)

## 테스트 통과

- [ ] `go test ./...` 모든 케이스 통과
  - 포함: state, daemon, icmp, probe, check, runner 단위 테스트
  - 가짜 시계·pinger·checker·runner 사용
  
- [ ] `bash test/manual_check.sh -y` 자동 진행 완료
  - 단계 1~8: 점검 완료 (wall·설치 단계 제외)
  - 로그: `/tmp/auto_setup_manual/` 생성 및 정리됨
  
- [ ] `bash test/run_e2e.sh` 목업 E2E 통과 (root 필요)
  - 스크래치 사용, 원본 /tmp/auto_setup 미영향
  - 스텁 gossh/ssh/wall 동작 확인
  - 01 복사본 실행 → queue 생성 → 데몬 수거 → os_check 실행 → code 생성 → wall 메시지 확인

## 코드 규칙 (빈 변수)

- [ ] **main.go 최상단 빈 변수 (1차 4개는 아래, 2차 포함 8개는 "2차 항목" 참고)** (소스 커밋 시 항상 빈 값):
  ```go
  var (
    os6_mgmt      = ""
    os6_gossh     = ""
    os6_autosetup = ""
    os_check_sh   = ""
  )
  ```
  확인 명령:
  ```bash
  grep -nE '^\s+(os6_mgmt|os6_gossh|os6_autosetup|os_check_sh)\s+=\s+""' main.go
  ```
  결과: 4줄 모두 `= ""`

- [ ] 소스에 테스트 값(IP, 경로) 없음
  ```bash
  grep -r '192.168\|/tmp/auto_setup\|/root' *.go | grep -v 'const\|runsDir\|binDir'
  ```
  테스트 값은 test/ 하네스만 사용

## 런타임 검증 (os6 포함)

- [ ] `/usr/local/bin/auto_setup` 설치 권한 확인
  ```bash
  ls -l /usr/local/bin/auto_setup
  ```
  결과: `-rwxr-xr-x`

- [ ] crontab 등록 확인 (중복 없음)
  ```bash
  crontab -l | grep '/usr/local/bin/auto_setup ensure'
  ```
  결과: 1줄 (매분)

- [ ] tmpfiles 등록 확인
  ```bash
  cat /etc/tmpfiles.d/auto_setup.conf
  ```
  결과: `x /tmp/auto_setup`

- [ ] os6 정적 빌드 확인 (Go 1.20 서버 .60 에서)
  ```bash
  bash build_os6.sh
  file auto_setup_os6
  ```
  결과: `ELF 64-bit LSB executable … statically linked`

## auto_setup 특화 항목

- [ ] **01 전달 동작**
  - 02 성공 직후 queue 파일 생성 확인
  - 파일 내용: user=, time=, 호스트줄들
  - 02 실패 시 전달 없음 확인
  
- [ ] **queue 수거 및 상태 저장**
  - jobs/<jobid>.json 생성 확인
  - DNS 조회 완료 (동시 32 제한)
  - route 판별 (local/os6) 정상
  
- [ ] **ping 감시 (10초 주기)**
  - raw ICMP 동작 (root 필요)
  - 무응답 2회 연속 → seen_down=true
  - os6 장기 ssh 세션 자동 재시작
  
- [ ] **준비확인 (30초 주기)**
  - gossh -pm 응답 파싱 정상
  - anaconda 감지(_os_install 파일) 정상
  - READY: uptime 초 < (현재 − submitted)
  
- [ ] **7분/3분 규칙**
  - 첫 READY 시각 고정 (연장 없음)
  - 전부 READY 시 즉시 실행
  - 늦은 READY 3분 묶음 실행
  - 각 run 은 별개 code 생성
  
- [ ] **os_check 실행(-auto)**
  - 1차 run: 전체 목록 전달
  - 재실행 run: 미처리 호스트만 전달
  - os6 호스트 포함 시 gossh 래퍼 경로 삽입
  - 실행: `cd /tmp/auto_setup/runs/<code> && bash os_check_sh -auto <user> targets.txt`
  
- [ ] **code 파일 생성 (codes/<code>.txt)**
  - 순서 확정: ① 결과 리포트 ② FAIL/NO FAIL ③ LDAP·Splunk·커널 요약 ④ 원본경로
  - FAIL 있는 호스트 줄 포함
  - FAIL 없는 호스트는 `NO FAIL : host1 host2 … (N대)` 형식
  - 블록 발췌 정확(LDAP 정보, SPLUNK 정보, 커널 버전, <infra> infra 커널 버전)
  
- [ ] **wall 메시지**
  ```
  [auto_setup] OS 설치 + 설정체크 완료 (user=<user>)
  host01 host02 host03 (3대)
  code : 4821   →  auto_setup code 4821
  ```
  완료 호스트 = processed (postapply 에 나온) 호스트

- [ ] **무기한 감시 및 cancel**
  - 남은 호스트 있으면 무기한 감시
  - `auto_setup cancel <jobid>` 로 종료 (jobs → done)
  - 완료 된 작업은 자동 종료
  
- [ ] **로그 파일**
  - /tmp/auto_setup/auto_setup.log 생성
  - 상태 변화·실행·오류만 기록 (주기 반복 로그 없음)
  - 10MB 초과 시 .1 로 1개만 보관

## 2차 항목 (v0.2.0)

- [ ] **빈 변수 전수 grep (커밋 전, 모두 `= ""`)**
  - main.go 9개 (os6_mgmt, os6_gossh, os6_autosetup, os_check_sh, awx_dir, os8_mgmt, os8_autosetup, os6_os_check_sh, ldap_share_dir)
    ```bash
    grep -nE '^\s+(os6_mgmt|os6_gossh|os6_autosetup|os_check_sh|awx_dir|os8_mgmt|os8_autosetup|os6_os_check_sh|ldap_share_dir)\s+=' main.go
    ```
    결과: 9줄 모두 `= ""`
  - os_check 2개 (auto_done_dir, auto_done_host), 01 1개 (auto_setup_host)
    ```bash
    grep -nE '^(auto_done_dir|auto_done_host|auto_setup_host)=' "../OS 환경설정 체크/os_check_final_annotated.sh" ../awx_script/01.AWX_nodeinfo_V2.sh
    ```
    결과: 3줄 모두 `=""`
  - 테스트 값·IP 없음 (테스트 값은 `-ldflags -X` 또는 복사본 sed 로만 주입)

- [ ] **양방향 동일성 diff 0**: 같은 상태 디렉터리에서 `status` / `snapshot` / `code N` 의 로컬 실행 출력 = 원격 클라이언트 모드(스텁 gossh 가 로컬 바이너리 호출) 출력 (`bash test/run_e2e2.sh a`)
  - os6_mgmt 에서의 01 전달(복사본에 `auto_setup_host` 채움, 스텁 gossh)이 os8 queue 에서 로컬 전달과 같은 결과인지

- [ ] **bindpw 미노출 grep**: LDAP 시나리오(`bash test/run_e2e2.sh e`)의 로그·`jobs/*.json`·`snapshot`·code·wall·TUI/plain 출력·gossh 명령줄 기록에 테스트 bindpw 값이 없을 것(비교 기준은 binddn 의 uid 값이며 기본 로그에도 uid 값은 남기지 않음). 복원은 공유경로(`ldap_share_dir`) 경유라 명령줄에 비밀이 없고, 복원 뒤 공유 임시 디렉터리는 비어 있을 것(0700/0600 관찰·삭제 확인 포함)
  ```bash
  grep -rF '<테스트 bindpw 값>' "$SCRATCH"/auto_setup/auto_setup.log "$SCRATCH"/auto_setup/jobs "$SCRATCH"/auto_setup/codes   # 결과 없음
  ```
  (예외: `ldapbak/` 백업 파일 자체 — 0600, 자동 삭제 안 됨)

- [ ] **완료기록 규칙**: 부팅 이전·전달 이전 기록 거부, 미래 +5분 초과 거부, 인정 후 `done/applied/` 이동·중복 run 없음 (`run_e2e2.sh b`), os_check 빈 변수일 때 출력·종료코드 변화 0
- [ ] **2차(이중) 체크**: 대상 = route=local 접속불가·FAIL 만(route=os6 제외), 최종 = 1차 OK 또는 2차 OK, `os6_os_check_sh="-"` 면 생략 (`run_e2e2.sh f`)
- [ ] **요청·데몬 제어**: manual-run 은 완료 여부와 상관없이 수락(거부는 job·그룹 없음/중복, `requests/rejected/` reason=), `--start/--stop/--restart` pid 정리, 원격 클라이언트에서 데몬 명령 거부 (`run_e2e2.sh c d`)
- [ ] **TUI**: 키 시퀀스 테스트(화면 1 → Enter 화면 2 → `c` y/n → 요청 파일 생성), 골든 렌더(색 on/off, 폭 80/120), 비 tty·`--plain` 텍스트 출력 (`go test ./...`, `manual_check.sh` 11단계)
- [ ] **직접 테스트 건수**: `bash test/run_e2e.sh` 85건, `bash test/run_e2e2.sh` 191건, awx `run_tests.sh` 54건 모두 PASS
- [ ] **README 순서·주의사항**: 빌드·설치 → 사용 방법 → 옵션별 상세 → 문서별 설명 → 주의사항(Disclaimer) → 전역 명령어, LDAP LDAP 복원은 공유경로 경유(명령줄에 비밀 없음, `ldap_share_dir` 는 대상 root 읽기 가능해야 함, 사용 후 자동 삭제)·`ldapbak/` 수동 삭제 안내 포함
- [ ] **버전 태그**: `git tag -a auto_setup-v0.2.0 -m "양방향 동일 실행·완료기록·LDAP 백업/복원·TUI (CHANGELOG 2026-10-03 항목)"`

## 변경 작업 항목 (v0.7.0, m 메뉴)

- [ ] **원복·LDAP 엔진 `-no-restart` 고정**: LDAP 엔진 호출 인자에 `-no-restart` 가 항상 붙음 (`grep -n 'ldapNoRestart' changerun.go`). `-print-script`·`-p` 는 호출하지 않음 (`grep -nE 'print-script|"-p"' changerun.go` 결과 없음). 테스트 `TestChangeLDAPArgsAlwaysNoRestart`, `bash test/run_change_os.sh` 에서 `RESTARTED` 0건
- [ ] **bindpw 미노출**: `go test ./...` 의 `TestChangeSecretMask`·`TestChangeSecretNowhere` 통과. 데모 실행 뒤 `grep -rl DEMO_BINDPW_ "$DEMO_DIR/data"` 결과 없음 (기록·로그·임시 파일). gossh 명령줄은 base64 로 싣기 때문에 평문 grep 으로는 잡히지 않음 (README 주의사항 참고)
- [ ] **꺼짐 상태**: conf 의 `enable_*` 를 비우고 `--admin` 없이 실행 → `m` 무반응, 안내 줄에 `m 변경` 없음 (`TestChangeKeyOffIsNoop`). 화면은 내용·키가 같아야 하며, 60~80칸에서 `…` 잘림이 줄바꿈으로 바뀐 것만 허용 (`git diff -- testdata` 의 차이가 줄바꿈뿐인지 확인). **화면 바이트 동일은 아님**
- [ ] **엔진 os6 바이너리 재빌드·커밋**: 엔진 소스를 바꾸면 `bash build_os6.sh` (.60, Go 1.20) 로 `ip_change/bin/ip-change-engine_os6`, `ldap_setting/bin/ldap-config-engine_os6` 를 다시 빌드, `file` 로 statically linked 확인, +x 유지
- [ ] **폭 60/80/120 줄바꿈**: `TestChangeRenderWrapNoTruncation`, `TestChangeMenuLinesWrapNoTruncation` 통과, 메뉴 골든 `tui_changemenu_*_60.golden` 갱신 (폭 60 미만은 최선)
- [ ] **go.mod**: `ip_change/go.mod`·`ldap_setting/go.mod` 가 `go 1.20`. Go 1.20 (.60) 에서 `go vet`·`go test`·빌드 통과
- [ ] **랩 검증**: `bash test/run_change_os.sh` (끝 줄 `PASS=<n> FAIL=<m>`, FAIL=0), `bash test/run_change_lab.sh` (종료코드 3 이면 자기 접속 불가 → 결과에 사유를 적고 `run_change_os.sh` 결과로 대체), `bash test/demo_change.sh` 후 `--stop`
- [ ] **conf 샘플**: `conf/auto_setup.conf.example` 에 `enable_ip_change`·`enable_ldap_change`·`ip_engine`·`ldap_engine` 이 빈 값으로 있음. `conf/auto_setup.conf` 는 커밋하지 않음
- [ ] **setup.sh**: 엔진 소스가 없거나 빌드가 실패해도 설치 전체가 중단되지 않음 (건너뜀·경고 메시지), `ldap_config.conf` 는 600
- [ ] **문서**: README 「변경 작업」 절, CHANGELOG 0.7.0, WORKFLOW 「변경 작업 흐름」(mermaid), ARCHITECTURE 「반드시 지킬 것」, `사용법.txt` 13절 실장비 체크리스트가 서로 어긋나지 않음

## 인라인 메뉴·수동 완료·anaconda 감지 항목 (v0.7.1)

- [ ] **m 메뉴 인라인**: 현재 화면이 그대로 그려진 채 키 안내 줄 바로 아래에 메뉴 줄 (`TestChangeKeyOnShowsHintAndMenu`). 번호 외 키 무시, `Esc`/`m`/`q` 는 메뉴만 닫고 TUI 를 종료하지 않음. 닫으면 열기 전과 바이트 동일 (`TestInlineMenuOpenCloseByteIdentical`)
- [ ] **폭 40~120 줄바꿈**: `TestChangeMenuLinesWrapNoTruncation` (… 잘림 없음), 골든 `tui_changemenu_*_60.golden`·`tui_donemenu_*_60.golden`
- [ ] **d 수동 완료**: 선택 → 확인(y 만 진행) → 완료기록(출처 manual) / 원격은 `done` 중계 (`TestDoneKey*`). 전체 보기는 `1` 만, 완료·종료 job 은 안내만, m·x·w 와 동시에 열리지 않음
- [ ] **화면 2 안내 줄**: `d 수동 완료` 포함, 폭 40~200 에서 넘치지 않음 (`TestDoneHintAndPromptWidths`). 화면 1·`--plain` 은 변화 없음
- [ ] **anaconda 감지**: ping X 없이 anaconda 응답이면 설치중·SeenDown·DownAt (`anaconda_test.go`), g 재확인 `단계 대기 → 설치중`, 종료 job 은 단계 불변
- [ ] **문서**: CHANGELOG 0.7.1, README 키 표·「변경 작업」, `사용법.txt`, ARCHITECTURE, 도움말(`?`)이 서로 어긋나지 않음

## v0.8.0 항목 (메뉴 h·w · 신규 설치 양식 · DNS 점검 · gossh 작업 · 결함 수정 ①②③)

- [ ] **Go 1.26.5 빌드·검증 + Go 1.20 호환**: 폐쇄망 버전으로 `export PATH=/opt/go1.26.5/bin:$PATH GOTOOLCHAIN=local; go build ./... && go vet ./... && go test ./...` 통과 (`go test` 398 PASS·7 SKIP 안팎). `go.mod` 는 `go 1.20` 유지. **Go 1.20 문법·표준 라이브러리만** — `grep -nE '(^|[^A-Za-z0-9_.])(min|max)\(|"slices"|"maps"|"log/slog"' *.go` 로 내장 min/max·slices·maps·slog 가 없는지 확인 (자체 함수 `rMin`·`rMax` 는 해당 없음, 주석 안의 `max(` 는 무시). range-over-int 와 for 루프 변수의 새 의미는 grep 으로 안 잡히므로 **Go 1.20 호환 job(CI) 또는 .60 의 Go 1.20 빌드**로 확인. 외부 의존성 추가 없음 (`go.mod` 에 require 없음)
- [ ] **os6 바이너리**: 소스를 바꿨으면 Go 1.20 서버(.60)에서 `bash build_os6.sh` 로 `auto_setup_os6` 를 재빌드해 **같은 커밋에 포함** (`file auto_setup_os6` 에 `statically linked`, 변수는 빈 값). 회사 환경에는 Go 가 없으므로 .gitignore 로 빼지 않음
- [ ] **tcsh 확인 (랩)**: os6_mgmt 로 보내는 원격 명령을 만들거나 고쳤으면 `AS_TEST_LOGIN_MGMT=<user@host> go test -run ViaLoginShell -v .` (로그인 셸이 tcsh 인 계정) — SKIP 이 아니라 PASS. 원격 명령은 `remoteBash`/`shq` 를 거치고(`grep -n 'sh -c' client.go` 에 옛 `sh -c` 없음), 래퍼 2종(`os6WrapperScript`·`chgOS6WrapperScript`)은 같은 형태 (`TestOS6WrappersLoginTcsh`, `TestRemoteBashQuoting`, `TestRemoteBashBang`)
- [ ] **결함 ①③**: 준비확인·`g` 의 60초 제한과 프로세스 그룹 종료(`TestCheckTimeoutKillsGroup`), gossh 출력 해석 통일(`TestParseGossh`, `TestParseCheckFQDNAndMismatch` — FQDN↔짧은 이름·ANSI·형식 불일치). LDAP 수집 응답 원본은 로그에 남지 않음 (bindpw)
- [ ] **메뉴 h·w**: 안내 줄·도움말(`?`)·골든(`tui_menus_*` 등)이 새 구조와 일치, 숨은 단축키 `c t g v d x` 동작 유지, 메뉴 닫힘 상태는 열기 전과 바이트 동일, 폭 40~120 에서 `…` 잘림 없음 (`menu_tui_test.go`). `ww`·`w` `Enter` 가 예전의 AWX 실행
- [ ] **신규 설치 양식**: 저장 전에는 어떤 파일도 바뀌지 않음, 저장 시 기존 `<user>.txt` 백업(실패하면 중단)·`history/` 사본, 디렉터리 0700·파일 0600, MAC 소문자·중복·형식 오류 차단, AWX 연결은 nodeinfo N 강제·대상 목록 자동 입력·`AWX_DNS_CHECKED` (`newhost_test.go`, `newhost_tui_test.go`, `awxrun_test.go`)
- [ ] **DNS 점검**: Go(`dnscheck.go`)와 bash(01 `dns_check`)의 판정 5종·중복 경고 3종·속도 상수가 같음 (`dnscheck_test.go`, awx `bash ../awx_script/test/run_tests.sh` 108건 — DNS 케이스 21~23). 01 auto 모드는 `/dev/tty` 로 묻고 못 물으면 중단, `AWX_DNS_CHECKED=1` 은 표만 출력. `test/fake_dns.py` 는 랩·CI 전용(운영 설치 대상 아님)
- [ ] **gossh 작업 `s`**: 실행 위치 규칙(os8 ≤ 기준 → os8 + 접속불가만 os6 재실행 / 초과 → 전량 os6 / os6_mgmt 에서 열면 os6 직접), 기록 `work/<user>/` 0700·0600, 출력 7일·user 당 200MB·`/tmp/<3자리>` 7일 정리, 위험어 확인 (`work_test.go`, `work_tui_test.go`)
- [ ] **새 conf 변수는 빈 값으로만**: `main.go`·`conf/auto_setup.conf.example`·`conf/vars.manifest`(32개)에 `newhost_template_1~9`·`work_os6_over` 가 빈 값, 비면 기존과 같은 동작. `conf/auto_setup.conf` 는 커밋하지 않음
- [ ] **update_v0.8.0.sh 생성기 검증**: `bash -n conf/update_v0.8.0.sh`, 마지막 줄이 `#__END_OF_UPDATE__` 이고 `awk 'END{print NR}' conf/update_v0.8.0.sh` 가 파일 머리의 `GUS_EXPECT_LINES` 와 같음 (직접 고치지 말고 `vars.manifest` 수정 후 **재생성**). `bash test/run_setup_scripts.sh` 54건 PASS — `--dry` 무변경·멱등·`--undo` 바이트 동일·v0.6.0→v0.7.0→v0.8.0 순차 적용·기존 값 보존. awx_script `setup/update_unreleased_dns.sh` 도 같은 검증 (`GUS_EXPECT_LINES`, 01 만 변경)
- [ ] **SVG 재생성**: 명령·키 흐름이 바뀐 곳은 `docs/svg/*.svg`(단계와 맨 아래 "코드:" 줄)·`workflow.svg`·awx_script `workflow.svg`([4-1] 단계)를 함께 고침. 모든 SVG 가 `xmllint --noout` 를 통과 (없으면 python `xml.dom.minidom` 파싱), `<style>` 의 라이트/다크 CSS 변수 유지. `docs/svg/README.md` 표와 파일 목록이 일치 (README·WORKFLOW 의 상대 링크가 실제 파일을 가리키는지 스크립트로 확인)
- [ ] **CI**: `.github/workflows/auto-setup.yml` 이 `.claude/HPC/auto_setup/**` 와 워크플로 파일 자신으로 paths 필터, working-directory 가 `.claude/HPC/auto_setup`, Go 1.26.5 `build`·`vet`·`test` + `bash -n setup.sh build_os6.sh conf/*.sh test/*.sh`, Go 1.20 문법 호환 job (build·vet)
- [ ] **문서**: README(키 표 메뉴 구조·새 절 4개·빈 변수 표·알려진 제한)·CHANGELOG 0.8.0·ARCHITECTURE(새 파일 행 누락 0, 「수정 요청 가이드」 새 항목)·WORKFLOW(mermaid + SVG)·`사용법.txt` 14절·`docs/svg/README.md` 가 서로 어긋나지 않음. awx_script: README·CHANGELOG `[Unreleased]`·`사용법.txt`·WORKFLOW·`workflow.svg`·ARCHITECTURE
- [ ] **버전 태그**: `git tag -a auto_setup-v0.8.0 -m "메뉴 h·w, 신규 설치 양식, DNS 점검, gossh 작업 기록, 결함 ①②③ (CHANGELOG 2026-10-13 항목)"`

## 기존 사용법 불변 확인

- [ ] **01.AWX_nodeinfo_V2.sh 불변**
  - 프롬프트·출력 순서 동일
  - 종료코드 동일
  - auto_setup 전달은 로그 1줄뿐 (프롬프트·옵션 없음)
  
- [ ] **os_check_final_annotated.sh 호환성**
  - 인자 없이 실행: 100% 기존과 동일 (read -rp 두 프롬프트)
  - `-auto` 옵션 추가 시에만 분기 (프롬프트 없음)
  - 기존 테스트 전부 PASS 유지
  
## 문서 갱신 확인

- [ ] **README.md**: 설치·사용·옵션·문서별 설명·주의사항·버전 태그
- [ ] **CHANGELOG.md**: 2026-10-02(v0.1.0) 항목 유지 + 2026-10-03(v0.2.0) 항목 (신규·변경·주의)
- [ ] **ARCHITECTURE.md**: 폴더 구조·파일 역할·런타임 디렉터리·함수 매핑
- [ ] **WORKFLOW.md**: mermaid 흐름도 + 단계별 설명
- [ ] **workflow.svg**: 정적 SVG 흐름도 (v0.8.0 반영, 다크/라이트 지원) + `docs/svg/` 명령·키별 31개
- [ ] **PR_CHECKLIST.md**: 이 체크리스트
- [ ] **사용법.txt**: 명령어 중심 빠른 참고 (맨 위 1-7 섹션)
- [ ] **.github/workflows/ci.yml**: setup-go + go test/vet/build + bash -n (저장소 루트 `.github/workflows/auto-setup.yml` 은 경로 필터 CI)

## 최종 체크 (커밋 전)

```bash
# 1. 빌드 및 정적 검사
bash -n setup.sh build_os6.sh conf/*.sh test/*.sh
go vet ./... && gofmt -l .
shellcheck -S warning setup.sh build_os6.sh test/*.sh

# 2. 단위 테스트
go test ./...

# 3. 목업 E2E (root 필요)
sudo bash test/run_e2e.sh      # 86건
sudo bash test/run_e2e2.sh     # 194건
bash test/run_setup_scripts.sh # 54건
bash ../awx_script/test/run_tests.sh   # 108건

# 4. 빈 변수 확인 (main.go 8개 / os_check 2개 + 01 1개, 모두 "")
grep -nE '^\s+(os6_mgmt|os6_gossh|os6_autosetup|os_check_sh|awx_dir|os8_mgmt|os8_autosetup|os6_os_check_sh|ldap_share_dir)\s+=' main.go
grep -nE '^(auto_done_dir|auto_done_host|auto_setup_host)=' "../OS 환경설정 체크/os_check_final_annotated.sh" ../awx_script/01.AWX_nodeinfo_V2.sh

# 5. git 상태 확인
git status
git diff main.go  # 빈 변수 확인

# 6. 커밋
git add .
git commit -m "feat(auto_setup): 양방향 동일 실행·완료기록·LDAP 백업/복원·TUI (v0.2.0)"
git push origin <브랜치>
```

## 커밋 메시지 양식

```
feat(auto_setup): OS 설치 → 설정체크 자동 연계 데몬 및 CLI

- main.go: 최상단 빈 변수 4개, CLI 분기 (daemon/ensure/code/status/cancel/probe)
- daemon.go: 상태기계, queue 수거, 경로 판별, ping 감시, 준비확인, 7분/3분 스케줄, run 큐
- runner.go: os_check 실행(-auto), code 생성, wall 발송
- setup.sh: 빌드·설치·cron·tmpfiles 등록 (멱등)
- build_os6.sh: Go 1.20 정적 빌드 (auto_setup_os6)
- test: 대화형 점검(manual_check.sh), 목업 E2E(run_e2e.sh), 스텁
- 문서: README/CHANGELOG/ARCHITECTURE/WORKFLOW/workflow.svg/PR_CHECKLIST

계획서 §8 결정사항 반영:
- cron PATH 보강, os6 probe 로그 억제
- runner 오류 대기 로그 억제, 중단 run code 재사용 방지

기존 01/os_check 사용법 불변
```

## 검증 체크리스트 (최종)

- [ ] 모든 go 문법 검사 통과
- [ ] 모든 shell 문법 검사 통과
- [ ] Go 단위 테스트 PASS
- [ ] E2E 테스트 PASS
- [ ] 빈 변수 비어있음 (main.go 8개, os_check 2개, 01 1개)
- [ ] 테스트 값 없음
- [ ] cron 등록 확인
- [ ] tmpfiles 등록 확인
- [ ] os6 정적 빌드 성공
- [ ] 문서 모두 갱신
- [ ] 기존 01/os_check 사용법 불변
- [ ] git 커밋 메시지 양식 준수
