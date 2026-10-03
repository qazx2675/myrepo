# ARCHITECTURE

## 폴더 구조 및 파일 역할

| 파일/폴더 | 역할 | 비고 |
|---|---|---|
| `main.go` | 최상단 빈 변수 8개 + CLI 분기(무인자 리포트·--start/--stop/--restart·snapshot·done·request 포함) + 상수(주기/대기/정체 시간) | 실행 대상 |
| `daemon.go` | 데몬 루프: queue 수거·경로 판별·ping 감시·준비확인·스케줄·run 큐 | 상태기계 중추 |
| `state.go` | Job/Host 구조체, JSON 원자 저장/복원 (재기동 후 이어하기) | 상태 영속화 |
| `icmp.go` | raw 소켓 ping (echo id+seq 로 호스트 매핑) | local 경로 |
| `probe.go` | probe 하위명령(os6_mgmt 쪽) + os6 장기 ssh 세션 관리 | os6 경로 |
| `check.go` | 준비확인: gossh -pm uptime, anaconda 감지, READY 판정 (local/os6) | 설치 완료 감지 |
| `runner.go` | os_check 실행(-auto), os6 gossh 래퍼 생성, code 파일 작성, wall 발송 | run 실행 |
| `ensure.go` | ensure 하위명령: flock + setsid + daemon 백그라운드 기동 | cron 훅 |
| `log.go` | /tmp/auto_setup/auto_setup.log 관리 (10MB 롤오버) | 로깅 |
| `notify.go` | wall 알림 발송 | 결과 공유 |
| `paths.go` | `os6_os_check_sh`·`awx_dir` 해석, dhcp.sh 링크 후보 (autofs 동일 경로 포함) | 2차 |
| `model.go` | 호스트 단계(Stage)·한글 라벨·정체 판정, 그룹(yml) 집합, Snapshot 구조체·BuildSnapshot (CLI·TUI·원격 공용) | 2차 |
| `client.go` | SnapshotSource(로컬/원격), gossh 원샷 전송·출력 복원 = 원격 클라이언트 모드 | 2차 |
| `ctl.go` | `--start/--stop/--restart`, snapshot/done/request 명령, 색 메시지 | 2차 |
| `tui.go` | TUI 루프: raw tty, 키 파싱, 화면 전환, 자동 갱신, 수동 OS 체크 요청 (입출력·시계 주입) | 2차 |
| `render.go` | 순수 렌더 함수(상태→문자열): 화면 1·2·도움말·plain, 색 규칙 | 2차, 골든 테스트 |
| `tty_linux.go` / `tty_other.go` / `tui_hook.go` | raw 터미널(termios ioctl)·창 크기 / 비 Linux 대체 / 리포트 진입 훅 | 2차 |
| `done.go` | 완료기록 인정 규칙·수거(`acceptDoneRecords`), 수동 완료(`markDone`) | 2차 |
| `requests.go` | 요청 채널 `requests/*.req` 기록·수거(manual-run / cancel), 수동 run 스케줄 | 2차 |
| `ldapbk.go` | LDAP 백업(전달 시점)·bindpw 해시 비교·백업 세트 복원 | 2차 |
| `second.go` | 2차(이중) 체크: os6_mgmt 실행·결과 회수·code 섹션 병합 | 2차 |
| `iface.go` | Pinger/Checker/Runner/Notifier/Clock + (2차) LdapBackup/Second 인터페이스 | 테스트 주입점 |
| `*_test.go` / `testdata/` | 단위 테스트(state·daemon·daemon2·icmp·probe·check·runner·client·ctl·done·ldapbk·model·render·requests·second·tui·paths), TUI 골든 파일 | 검증 |
| `setup.sh` | 빌드 + /usr/local/bin 설치 + cron + tmpfiles 등록 (멱등) | 설치 도구 |
| `build_os6.sh` | Go 1.20 정적 빌드 → auto_setup_os6 (CGO_ENABLED=0) | os6 빌드 |
| `test/` | 대화형 점검(manual_check.sh 1~17단계), 목업 E2E(run_e2e.sh 85건, run_e2e2.sh 177건), 공용(lib_e2e.sh), 스텁(gossh/ssh/wall) | 검증 하네스 |
| `README.md` | 설치·사용·옵션 가이드 | 배포 필수 |
| `CHANGELOG.md` | 변경 이력 | 배포 필수 |
| `ARCHITECTURE.md` | 이 문서 — 폴더 구조, 함수 역할, 수정 가이드 | 개발 참고 |
| `WORKFLOW.md` | 흐름도(텍스트 mermaid) | 개발/설명 용도 |
| `workflow.svg` | 흐름도(SVG) | 브라우저 열람 용도 |
| `PR_CHECKLIST.md` | 풀리퀘스트 검증 항목 | CI 참고 |
| `사용법.txt` | 명령어 중심 빠른 참고 | 운영 담당자 용도 |
| `.github/workflows/ci.yml` | 폴더 내 CI (bash/shellcheck/go test) | 자동 검증 |

## 런타임 디렉터리 구조

`/tmp/auto_setup/` (또는 `$AUTO_SETUP_DIR`):

```
/tmp/auto_setup/
├── auto_setup.log         # 상태 변화·실행·오류만 (주기 로그 없음, 10MB 롤오버 1개)
├── auto_setup.lock        # ensure 의 flock 파일
├── auto_setup.pid         # (2차) 데몬 pid — --stop/--restart 가 사용
├── queue/
│   ├── <epoch>_<user>_<pid>.job    # 01 이 전달한 작업 (user=, time=, 호스트줄, 그룹 줄 yml=…/all=…)
│   └── ...
├── jobs/
│   ├── <jobid>.json       # 진행 중 작업 상태 (hosts, groups, runs, 스케줄·stage 정보)
│   └── done/
│       └── <jobid>.json   # 완료·취소된 작업 (정리 정책 없음)
├── done/                  # (2차) 완료기록: <host> 한 줄 "epoch user sha256 source"
│   └── applied/           #   인정되어 이동된 기록
├── requests/              # (2차) 요청 채널: <epoch>_<kind>.req (manual-run / cancel), 5초 주기 수거
│   ├── active/            #   수락된 manual-run (run 이 끝나면 삭제)
│   └── rejected/          #   거부된 요청 (마지막 줄 reason=, 정리 정책 없음)
├── ldapbak/               # (2차) LDAP 백업 (0700, 자동 삭제 안 됨, bindpw 포함 원본)
│   └── <jobid>/<host>/    #   ldap.conf resolv.conf nslcd|sssd.conf ntp|chrony.conf, meta.json (파일 0600)
├── runs/
│   ├── <code>/
│   │   ├── targets.txt    # 이 run 의 대상 호스트 목록
│   │   ├── dhcp.sh        # os_check 링크 (선택적)
│   │   ├── os_check.log   # os_check 실행 출력
│   │   └── second/        # (2차) 2차 체크: targets.txt, os_check.log, check.res_<user>* (정리 정책 없음)
│   └── ...
├── codes/
│   ├── <code>.txt         # 결과 코멘트 (결과리포트 + FAIL + LDAP·Splunk·커널 + 원본경로 [+ ### 2차 체크 (os6_mgmt)])
│   └── ...
└── bin/
    └── gossh              # os6 호스트 포함 run 일 때만 생성, 0755
```

- `/tmp/auto_setup/bin/gossh`: runner 에서 동적 생성, 실제로는 ssh 래퍼 (os6_mgmt 로 파일 전송)
- tmpfiles 등록(`x /tmp/auto_setup`): 무기한 감시 작업이 10일 tmp 정리로 삭제되지 않도록 제외

## 주요 함수 및 단계 매핑

### daemon.go 의 상태기계

| 단계 | 함수 | 입력 | 출력/부작용 | 설명 |
|---|---|---|---|---|
| ① | `doPoll()` | queue 디렉터리(5초 주기) | Job 로드 또는 생성 | 새 작업 수거, 중복 호스트는 기존 job 에서 제거 |
| ② | `doLookup()` | 호스트명 목록 | host → ip 매핑(net.LookupHost, 동시 32) | DNS 조회(job 수거 시 1회) |
| ③ | `selectRoute()` | 로컬 ping 결과 | host → route 배정(local/os6) | 로컬 ping 실패 → os6 경로 |
| ④ | `doPing()` | 10초 주기 | host 의 up/down 판정, seen_down 설정 | raw ICMP 송수신, 미스 카운트 → down |
| ⑤ | `doCheck()` | 30초 주기(후보만) | READY 호스트 판정 | gossh -pm uptime && anaconda 아님 && uptime < 경과시간 |
| ⑥ | `scheduleRun()` | READY 변화 | run 이름 및 호스트 결정(7분/3분) | 첫 READY + 7분 또는 전부 READY 시 즉시 실행 |
| ⑦ | `doRun()` | run 큐 | os_check 호출, code 생성, wall 발송 | 동시에 1개 run만 실행, 실패해도 다음 run 진행 |
| ⑧ | (감시 계속) | | | 완료 호스트 제외 후 무기한 감시(cancel 까지) |

### check.go 의 준비확인

- **후보**: (미처리 && ping up && seen_down) || (기동 직후 1회)
- **local**: `gossh -pm -script -w <tmpfile> "cat /proc/uptime"`; `<tmpfile>_os_install` 에 없으면 anaconda 아님
- **os6**: ssh 로 os6_mgmt 에 임시 파일 생성 → gossh 실행 → 결과 수집

### runner.go 의 code 파일 생성

`codes/<code>.txt` 순서(확정):

1. **결과 리포트**: os_check.log 의 `############### 결과 리포트 ###############` ~ `###########################################` 블록 그대로
2. **설정체크**: `check.res_<user>_postapply` 에서 FAIL 포함 줄만, FAIL 없는 호스트는 `NO FAIL : host1 host2 … (N대)`
3. **요약 블록**: `===== LDAP 정보`, `===== SPLUNK 정보`, `===== 커널 버전`, `===== <infra> infra 커널 버전` 블록 발췌
4. **원본 경로**: `원본 : /tmp/auto_setup/runs/<code>/os_check.log`

### probe.go 의 os6 세션

- 상시 유지: `ssh -o BatchMode=yes -o ServerAliveInterval=30 <os6_mgmt> "<os6_autosetup>/auto_setup probe -i 10s"`
- stdin 으로 호스트 목록 전달 → stdout 에서 `host up|down` 파싱
- 세션 끊김 시 자동 재시작

## 2차: 양방향 동일 실행·요청 채널·완료기록·LDAP·2차 체크

### 원격 클라이언트 (client.go / ctl.go)

- `os8_mgmt` 가 비어 있지 않으면 `newSource()` 가 `remoteSource` 를 돌려주고 status/snapshot/code/cancel/done/request·리포트가 `remoteRun()` 으로 간다.
- 전송: 임시 목록 파일에 `<os8_mgmt>` 를 쓰고 `gossh -pm -script -w <f> "sh -c '<os8_autosetup> <하위명령> …'"`. 원격에서 stdout·stderr 를 파일로 받아 base64 + 표지줄(`==RC==`/`==O==`/`==E==`)로 실어 오고 `parseRemote()` 가 `host: ` 접두를 떼어 로컬과 같은 바이트를 복원한다.
- `--start/--stop/--restart/daemon/ensure` 는 원격 모드에서 거부(`remoteOnlyMsg`). 로컬 모드의 `--start/--stop/--restart` 는 pid 파일(`auto_setup.pid`)로 기동 확인·SIGTERM 대기.

### 단계(stage)와 스냅샷 (model.go)

| 단계 | 값 | 설명 |
|---|---|---|
| 대기 | `queued` | 전달됨, 아직 ping X 를 못 봄 |
| 배포중 | `deploying` | ping X(seen_down) |
| 설치중 | `installing` | anaconda 확인됨 |
| 부팅확인 | `booting` | seen_down 후 ping O, READY 아님 |
| READY | `ready` | 새 OS 부팅 확인, run 대기 |
| LDAP확인 | `ldap` | READY 직후 LDAP 비교·복원 중 |
| 체크중 / 2차체크 | `checking` / `second` | os_check run / 2차 체크 진행 중 |
| 완료 | `done` | processed (run / 완료기록 / 수동) |
| 정체 | `stuck` | seen_down 후 `installStuck`(60분)을 넘겨도 READY 안 됨 (`effectiveStage`, 데몬이 없어도 계산) |
| 실패 | `failed` | os_check 비정상 종료 연속 `maxFails`(3)회 |

`snapshot` JSON 은 같은 디렉터리 내용 + 같은 시각이면 같은 바이트(정렬·시각 형식 고정). 로컬 실행과 원격 클라이언트 출력이 같음을 E2E(a)가 검증한다.

### 요청 채널 (requests.go)

`writeRequest()` 가 `requests/<epoch>_<kind>.req`(key=value 줄)를 임시파일 → link 로 원자 기록 → 데몬이 `collectRequests()` 로 수거. `manual-run` 은 그룹 전체가 완료일 때만 수락(→ `requests/active/`, run 큐에서 `scheduleManual()` 이 자동 run 이 없을 때 실행), 거부는 `requests/rejected/<같은 이름>` + 마지막 `reason=` 줄. `cancel` 은 `auto_setup cancel` 과 동일.

### 완료기록 (done.go ↔ os_check)

- os_check 의 `auto_record_done()` (빈 변수 `auto_done_dir`/`auto_done_host` 가 있을 때만)가 `done/<host>` 에 `epoch user sha256 source` 한 줄을 원자 기록(`auto_done_host` 는 gossh 원샷 500대 단위, base64 에 2글자마다 `.` 삽입).
- 데몬 `acceptDoneRecords()` + `judgeDoneRecord()`: 전달 이후 · 마지막 부팅 이후만 인정, 미래 시각 +5분 초과 거부, 재설치 증거 전이면 보류. 인정 시 `done_src=external|manual`, 기록은 `done/applied/` 로 이동.

### LDAP 백업/복원 (ldapbk.go, daemon.go `ldapBackup`/`ldapApply`)

- 백업: job 수거 직후 ping O·seen_down 아님 호스트에 gossh 로 파일 수집 → `ldapbak/<jobid>/<host>/`(0700/0600). 대상은 `lbFilePaths` 표(ldap.conf·resolv.conf·nslcd.conf·sssd.conf·ntp.conf·chrony.conf), OS·s4 분기는 `detectScript()`.
- 복원: READY 직후 os_check 전에 새 OS 의 bindpw sha256 만 비교 → `same` 생략 / `diff` 면 백업 세트를 `restoreScript()` 로 복원(권한·소유 유지, restorecon, 서비스 재시작). 복원 파일 내용이 base64 로 gossh 명령줄에 실린다(`ldapApplyTimeout` 45초). bindpw 는 로그·상태·반환값·meta.json 어디에도 남기지 않는다.

### 2차(이중) 체크 (second.go, daemon.go `selectSecondTargets`)

1차 run 결과에서 route=local 의 접속불가(postapply 에 없음)·FAIL 호스트만 대상(route=os6·미READY 제외, 비정상 종료면 없음). `secondSSH()` 로 os6_mgmt 임시 디렉터리에서 `os_check -auto` 실행 → tar 회수 → `runs/<code>/second/` → code 파일에 `### 2차 체크 (os6_mgmt)` 섹션 이어붙임. 처리 = 1차 OK 또는 2차 OK. `os6_mgmt` 또는 `os6OSCheckPath()` 가 비면 생략.

### TUI (tui.go / render.go)

화면 그리기는 상태→문자열 순수 함수(`renderOverview`/`renderDetail`/`renderHelp`/`renderPlain`)이고, 입출력·크기·tick·시계는 `tuiEnv` 로 주입해 키 시퀀스 테스트가 가능하다. raw tty 는 termios ioctl(`tty_linux.go`). 비 tty·`--plain` 이면 색 없는 텍스트를 한 번 출력.

## 수정 요청 가이드

| 요청 | 확인 대상 | 참고 |
|---|---|---|
| 빈 변수 변경 (main.go 8개 / os_check 2개 / 01 1개) | `main.go`·os_check·01 최상단, README 빈 변수 표 | 커밋 시 빈 값 유지 |
| 주기/대기 시간 변경 | `main.go` 상수 (`pingInterval` 등) | daemon 루프에 반영됨 |
| ping 방식 변경 | `icmp.go` (현재: raw 소켓) | 성능·권한 영향 |
| 준비확인 로직 변경 | `check.go` (현재: uptime + anaconda) | §9-2 테스트 수정 필요 |
| 7분/3분 규칙 변경 | `daemon.go` `scheduleRun()` (현재: bootWait/lateWait) | 테스트 수정 필요 |
| os_check 실행 위치 변경 | `runner.go` `Run()` (현재: `/tmp/auto_setup/runs/<code>/`) | code 경로도 함께 변경 |
| code 파일 포맷 변경 | `runner.go` `buildCodeText()` | 순서 고정(계획서 §5-9) |
| wall 메시지 변경 | `runner.go` (현재: 호스트 가로 + code) | 안내는 README 에서 수정 |
| 로그 정책 변경 | `log.go` (현재: 상태·실행·오류만, 10MB 롤오버 1개) | 디스크 영향 검토 필수 |
| 기존 01·os_check 사용법 | 반드시 불변 | 계획서 C1 제약 |
| os6 빌드 호환성 | `build_os6.sh`, Go 1.20 (.60) | CGO_ENABLED=0 유지 |
| 정체 기준 변경 | `main.go` `installStuck`, `model.go` `effectiveStage` | 골든·단위 테스트 수정 |
| 완료기록 인정 규칙 변경 | `done.go` `judgeDoneRecord`, os_check `auto_record_done` | 위조·시각 규칙 테스트 필수 |
| LDAP 대상 파일·OS 분기 변경 | `ldapbk.go` `lbFilePaths`/`detectScript` | `ldap_check.sh` 와 동기화, bindpw 미노출 확인 |
| 2차 체크 대상·병합 변경 | `daemon.go` `selectSecondTargets`, `second.go` | code 섹션 포맷 |
| TUI 화면·키 변경 | `render.go`(순수 함수), `tui.go`(키) | 골든 파일 갱신 (`testdata/tui_*.golden`) |
| 요청 종류 추가 | `requests.go` `collectRequests`, `ctl.go` `requestArgsOK` | 원격 중계 인자 검증도 함께 |

## 테스트 주입점 (iface.go)

테스트에서 다음 구현체를 교체해 검증합니다:

| 인터페이스 | 테스트 구현 | 목적 |
|---|---|---|
| `Pinger` | `fakePinger` | ICMP 응답 시뮬레이션(up/down 시퀀스) |
| `Checker` | `fakeChecker` | 준비확인 결과(READY/미준비) 시뮬레이션 |
| `Runner` | `fakeRunner` | os_check 실행 결과(코드·호스트·abnormal) 시뮬레이션 |
| `Notifier` | `fakeNotifier` | wall 메시지 캡처 |
| `Clock` | `fakeClock` | 시간 진행(타이머 테스트: 7분/3분) |
| `LdapBackup` | 가짜 gossh/ssh(PATH 스텁) 또는 fake 구현 | LDAP same/diff/백업없음·bindpw 미노출 |
| `Second` | 가짜 구현 / PATH 의 ssh 스텁 | 2차 대상 선별·병합 |
| `tuiEnv` | 가짜 In/Out/Size/Tick/Now | 키 시퀀스·골든 렌더 |

- 단위 테스트(state_test, daemon_test 등)에서 가짜를 직접 주입
- E2E(run_e2e.sh)에서 bash 스텁(gossh, ssh, wall) 사용

## 흐름도

전체 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.
