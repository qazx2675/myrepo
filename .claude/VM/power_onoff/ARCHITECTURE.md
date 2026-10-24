# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| main.go | CLI 진입점. user/vCenter/기능 선택, 계획표 출력, VWP 확인 → 안전장치 → 실행 → 결과를 순서대로 조립 |
| types.go | 공용 타입: `Config`, `VMInfo`, `Action`(기능 1~4), 종료코드, `ConfPath`(`/etc/vcetner_pwon_off/pwonoff.conf`) |
| config.go, config_unix.go | conf 파싱·검증(알 수 없는 키/중복 키 거부), root 소유·권한 검사, **허용 경로(allowed_dir) 검사** |
| target.go | `on_off_<user>.txt` 읽기(이 이름만, 심볼릭 링크 거부), VM 이름 규칙 `<BM>evNN`, 정렬 |
| vcenter.go | govmomi 접속, 대상 VM 조회(전원/실제 호스트/폴더/포트그룹/lpage/Tools/IP), 비밀번호(암호 파일→직접), 이전 vCenter 기억 |
| vwp.go | VWP 판정(폴더 경로·포트그룹, 대소문자 무시)과 경고 배너 |
| guard.go | **안전장치 5단계**(코드→Enter→60초 이름 입력→`YeS`→카운트다운), VWP 확인 코드, `--demo` |
| ui.go | 색상 출력, 공용 stdin 리더 `LineReader`(프로세스당 하나, 타임아웃 입력) |
| power.go | 전원 작업: Off(Guest 종료→강제 확인), 일괄 On, **lpage BM ev 단계별 순차 On(같은 ev 동시 → 전부 부팅 대기 → 다음 ev)**, 재부팅, 단계별 c/s/q 질문 |
| power_govmomi.go | `VMOps` 의 실제 구현(vCenter 태스크 호출) |
| bootcheck.go | 부팅 완료 판정: Tools+heartbeat+IP → 22 포트 직접 → os6_mgmt 경유 |
| runlog.go | 진행 로그(질문 중 일시 정지·버퍼), 결과 표(`PrintSummary`) |
| auditlog.go | 실행 기록 `allowed_dir/run_<user>/…log` |
| crosscheck.go | **교차검증 관문**(conf `cross_check=on`): 확인자 선택(`-c`/번호), 안전장치 직후 `cross_check --run` → 승인되면 `--start` 호출(토큰은 자식 프로세스 환경변수로만), 실행 파일 사전 확인, 오류 분류·감사 기록 |
| setup.sh, install.sh, build_os6.sh | 오프라인 빌드 / `/usr/local/bin`·conf 설치 / OS6(Go 1.20) 정적 빌드. **cross_check 도 함께**: 빌드(`cross_check/bin/cross_check`), `/etc/cross_check/`·`/var/lib/cross_check` 구성, admin 패스워드 생성, root crontab 등록, 데몬 기동, `bin_os6/cross_check.gz` |
| vendor/, go.mod, go.sum | 폐쇄망 빌드용 의존성(govmomi 0.55.1) |
| bin_os6/ | OS6 용 미리 빌드한 실행파일(gzip) |
| *_test.go | 단위 테스트. `*_vcsim_test.go`, `vcenter_test.go` 는 빌드 태그 `vcsim`(시뮬레이터 필요) |
| test/ | `run_tests.sh`(전체), `guard_scenarios.sh`, `e2e_*`(vcsim 위 전체 흐름), `vcsim_server/` |
| dev/sync_test.sh | 로컬 → 빌드 서버 동기화 후 go 명령 실행 |
| cross_check/ | **교차검증 데몬**(별도 Go 모듈 `cross_check/go.mod`, 표준 라이브러리만). 사용법·정책·보안 모델은 [cross_check/README.md](cross_check/README.md) |
| cross_check/cmd/cross_check/ | CLI 진입점: `--user`/`--run`/`--start`/`--admin`/`--daemon`/`--ensure`, 종료 코드, `CROSS_CHECK_*` 환경변수, `--run` 출력 규약(stdout 두 줄), 데몬 분리 기동(`detach_*.go`) |
| cross_check/internal/daemon/ | 데몬 본체와 클라이언트: `server.go`(상태 전이·한도·승인 만료), `ttl.go`(승인 유효시간 범위·`FormatTTL`·만료 사유), `protocol.go`(op·상태·중단 사유·한도 상수), `auth.go`(auth.conf·패스워드 규칙·실패 지연), `conf.go`(cross_check.conf·대상 파일 검사), `client.go`, `lock_*.go`(flock), `peercred_*.go`(감사용 uid) |
| cross_check/internal/tui/ | 사용자·관리자 화면: `user.go`, `admin.go`, `session.go`(등록 흐름), `style.go`(표·색·승인 유효시간 정책 문구), `lineio.go`, `term_*.go`(에코 끄기) |
| cross_check/internal/store/ | 작업 기록 `jobs.json`(대기/승인/중단, 원자적 저장, 재시작 시 열린 작업 중단) |
| cross_check/scripts/ | `cc_cron.sh`(root crontab 줄 등록·삭제·확인), `cc_admin_pw.sh`(admin 무작위 패스워드) |
| cross_check/test/e2e.sh, playground.sh, playground_worker.sh | 실제 데몬을 임시 경로로 띄우는 격리 E2E (`go test` 는 `cross_check/` 안에서 따로 실행) / 손으로 해 보는 격리 놀이터(데몬·시험 계정·작업자 시뮬레이터, `/etc`·`/var`·crontab 불사용) |
| cross_check/PLAN.md, INTERVIEW.md | 원 계획서 / 결정 기록(Q1~Q31) |

수정 요청이 "안전장치 단계 변경"이면 `guard.go`, "부팅 완료 기준 변경"이면 `bootcheck.go`, "순차/병렬 규칙 변경"이면 `power.go`(`RunOnSeq`/`RunReboot`),
"VWP 판정 변경"이면 `vwp.go`, "conf 항목 추가"면 `types.go` + `config.go` + `pwonoff.conf.example`, "허용 경로·user 규칙"이면 `config.go`/`target.go`, "교차검증 관문(확인자 선택·호출 순서)"이면 `crosscheck.go`(conf 키는 `types.go` + `config.go` + `pwonoff.conf.example`), "교차검증 데몬의 상태·사유·한도·인증"이면 `cross_check/internal/daemon/`, "교차검증 화면·명령"이면 `cross_check/internal/tui/`, "cross_check 옵션·환경변수·종료 코드"이면 `cross_check/cmd/cross_check/main.go` 만 보면 됩니다.

작업 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.
