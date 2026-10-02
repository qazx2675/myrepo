# CHANGELOG

## [Unreleased]

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
