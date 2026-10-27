# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| main.go | CLI 진입점. user/vCenter/기능 선택, 계획표 출력, VWP 확인 → 안전장치 → 실행 → 결과를 순서대로 조립. 메뉴·비밀번호 입력 제한(`readChoice`/`hiddenPrompt`/`askLine`, 초과 시 `failInput` 이 취소·감사 기록) |
| types.go | 공용 타입: `Config`, `VMInfo`, `Action`(기능 1~4), 종료코드, `ConfPath`(`/etc/vcetner_pwon_off/pwonoff.conf`) |
| config.go, config_unix.go | conf 파싱·검증(알 수 없는 키/중복 키 거부), root 소유·권한 검사, **허용 경로(allowed_dir) 검사** |
| target.go | `on_off_<user>.txt` 읽기(이 이름만, 심볼릭 링크 거부), VM 이름 규칙 `<BM>evNN`, 정렬 |
| vcenter.go | govmomi 접속, 대상 VM 조회(전원/실제 호스트/폴더/포트그룹/lpage/Tools/IP), 비밀번호(암호 파일→직접), 이전 vCenter 기억 |
| vwp.go | VWP 판정(폴더 경로·포트그룹, 대소문자 무시)과 경고 배너 |
| guard.go | **안전장치 5단계**: 기존 방식(작업 확인 문구→Enter→60초 이름 입력→`YeS`→카운트다운)과 교차검증 방식(작업 확인 문구→확인자 선택→승인 대기→`YeS`→카운트다운→`--start`, `GuardOpts.Checker/CrossCheck/AfterCountdown` 훅), 4/5 `ls`(대상 다시 보기), 질문마다 입력 제한(`GuardOpts.StepTimeout` = conf `input_timeout_sec`, 교차검증 4/5 는 `ConfirmDeadline` = 승인 마감), VWP 담당자 확인 문구(`WorkPhrase`·`VWPPhrase` 고정 문구, `ConfirmPhrase`), `--demo`(시험 전용 `PWONOFF_TEST_*` 환경변수는 데모에서만) |
| ui.go | 색상 출력, 공용 stdin 리더 `LineReader`(프로세스당 하나, 타임아웃 입력 `ReadLineTimeout`/`ReadLineLimit`), 취소 사유 `입력 시간 N분 초과`(`inputTimeoutReason`) |
| power.go | 전원 작업: Off(Guest 종료→강제 확인), 일괄 On, **lpage BM ev 단계별 순차 On(같은 ev 동시 → 전부 부팅 대기 → 다음 ev)**, 재부팅, 단계별 c/s/q 질문(입력 시간 초과 → 강제 PowerOff 는 No, c/s/q 는 q) |
| power_govmomi.go | `VMOps` 의 실제 구현(vCenter 태스크 호출) |
| bootcheck.go | 부팅 완료 판정: Tools+heartbeat+IP → 22 포트 직접 → os6_mgmt 경유 |
| runlog.go | 진행 로그(질문 중 일시 정지·버퍼), 결과 표(`PrintSummary`) |
| auditlog.go | 실행 기록 `allowed_dir/run_<user>/…log` |
| crosscheck.go | **교차검증 관문**(conf `cross_check=on`, 기본): `CrossCheckGate.Hook` 으로 안전장치에 끼움 — 2/5 `SelectChecker`(`-c`/번호 메뉴, 본인 제외, 3회 재입력), 3/5 `Approve`(`cross_check --run`, VM 이름 입력 대신. 작업자 화면에 대상 목록이 출력되고 확인자가 붙여넣어 승인), 5/5 카운트다운 직후 `Start`(`--start`), 전원 작업 뒤 결과 보고(`--finish --result done\|aborted`) — token 은 관문 안에만 두고 자식 프로세스 환경변수로만 전달·1회용, `--run --caller` 로 자기 실행 파일 이름(`vm_power_onoff`)을 넘김, 실행 파일 사전 확인(없으면 `cross_check=off` 안내), 오류 분류·감사 기록, `--run` stderr 의 승인 마감 줄(`승인 유효시간: … — HH:MM:SS 까지`)을 읽어 4/5 입력 마감으로 쓰는 `ccDeadlineSniffer` |
| setup.sh, install.sh, build_os6.sh | 오프라인 빌드(고정 틀: `GOPROXY=off`, `-mod=vendor`) / `/usr/local/bin`·conf 설치 / OS6(Go 1.20) 정적 빌드. **cross_check 도 함께**: 빌드(`cross_check/bin/cross_check`, 별도 모듈), `/etc/cross_check/`·`/var/lib/cross_check` 구성, admin 패스워드 생성, root crontab 등록, 데몬 기동, `bin_os6/cross_check.gz` |
| vendor/, go.mod, go.sum | 폐쇄망 빌드용 의존성(govmomi 0.55.1) |
| bin_os6/ | OS6 용 미리 빌드한 실행파일(gzip) |
| *_test.go | 단위 테스트. `*_vcsim_test.go`, `vcenter_test.go` 는 빌드 태그 `vcsim`(시뮬레이터 필요) |
| test/ | `run_tests.sh`(전체), `guard_scenarios.sh`, `e2e_vcsim.*`+`e2e_driver.py`(vcsim 위 전체 흐름, 기존 방식), `e2e_vcsim_crosscheck.*`(vcsim + 격리 cross_check 데몬, 교차검증 방식), `vcsim_server/` |
| dev/sync_test.sh | 로컬 → 빌드 서버 동기화 후 go 명령 실행 |
| README.md, 사용법.txt, 계획서.md | 사용자 문서(설치·사용·옵션·주의사항) / 수동 시험 명령어 모음 / 요구사항·설계 결정 |
| ARCHITECTURE.md, WORKFLOW.md, workflow.svg | 이 표 / 실행 흐름도(mermaid)와 렌더한 SVG — 흐름이 바뀌면 둘 다 고침 |
| CHANGELOG.md, PR_CHECKLIST.md | 변경 이력과 배포 태그 규칙 / 수정·배포 전 체크리스트(vm_power_onoff, cross_check 각각) |
| .github/workflows/ci.yml | CI: vm_power_onoff(vendor, `-mod=vendor`)와 cross_check(별도 모듈)를 각각 빌드·vet·테스트 |
| cross_check/ | **교차검증 데몬**(별도 Go 모듈 `cross_check/go.mod`, go 1.20, 표준 라이브러리만 — vendor 없음). 사용법·정책·보안 모델은 [cross_check/README.md](cross_check/README.md) |
| cross_check/cmd/cross_check/ | CLI 진입점: `--user`/`--run`/`--start`/`--finish`/`--admin`/`--daemon`/`--ensure`/`--plain`/`--caller`, 종료 코드, `CROSS_CHECK_*` 환경변수, `--run` 출력 규약(stdout 두 줄), 데몬 분리 기동(`detach_*.go`), 호출 스크립트 이름 정하기(`caller.go`: flag → 환경변수 → 부모 프로세스) |
| cross_check/internal/daemon/ | 데몬 본체와 클라이언트: `server.go`(상태 전이·단계(작업중/작업완료/작업중단)·한도·승인 만료·작업 제한 시간·SIGHUP 다시 읽기), `ttl.go`(승인 유효시간·작업 제한 범위·`FormatTTL`·만료 사유), `protocol.go`(op·상태·단계·중단 사유·한도 상수·`caller` 정리), `auth.go`(auth.conf·패스워드 규칙·실패 지연), `conf.go`(cross_check.conf: target_dir·users·auth·approval_ttl_sec·work_timeout_sec, 대상 파일 검사), `client.go`, `lock_*.go`(flock), `peercred_*.go`(감사용 uid) |
| cross_check/internal/tui/ | 사용자·관리자 화면. **전체 화면**(`screen.go` 루프·raw 모드 `tty_linux.go`, `screen_user.go`·`screen_admin.go`·`screen_common.go` 화면, `render.go` 폭·줄바꿈·색 부품, `keys.go` 키 해석·붙여넣기 구간, `flow.go` 붙여넣기 상자·흐름 틀)과 **줄 입력 화면**(대체: `user.go`, `admin.go`, `session.go` 등록 흐름, `lineio.go`, `term_*.go` 에코 끄기), `style.go`(표·색·승인 유효시간 정책 문구) |
| cross_check/internal/verify/ | 확인자가 붙여넣은 대상 파싱(줄바꿈·탭·공백 구분)과 작업 대상 집합 대조(빠짐·남음·중복·빈 입력) |
| cross_check/internal/store/ | 작업 기록 `jobs.json`(대기/승인/중단 + 승인 후 단계 작업중/작업완료/작업중단, 호출 스크립트, 원자적 저장, 재시작 시 열린 작업 정리) |
| cross_check/scripts/ | `cc_cron.sh`(root crontab 줄 등록·삭제·확인), `cc_admin_pw.sh`(admin 무작위 패스워드) |
| cross_check/test/ | `e2e.sh`(실제 데몬을 임시 경로로 띄우는 격리 E2E — 붙여넣기 불일치·`--finish`·작업 제한 포함), `pty_e2e.sh`(실제 pty 에서 전체 화면 TUI 시험), `playground.sh`·`playground_worker.sh`(손으로 해 보는 격리 놀이터. `/etc`·`/var`·crontab 불사용). `go test` 는 `cross_check/` 안에서 따로 실행 |
| cross_check/PLAN.md, INTERVIEW.md | 원 계획서(수정하지 않음) / 결정 기록(Q1~Q40, 계획서 지시사항 점검표 포함) |

수정 요청이 "안전장치 단계 변경"이면 `guard.go`, "부팅 완료 기준 변경"이면 `bootcheck.go`, "순차/병렬 규칙 변경"이면 `power.go`(`RunOnSeq`/`RunReboot`),
"VWP 판정 변경"이면 `vwp.go`, "conf 항목 추가"면 `types.go` + `config.go` + `pwonoff.conf.example`, "입력 제한 시간(계속 대기 금지)"이면 conf `input_timeout_sec`(`config.go`)와 각 질문(`guard.go`·`main.go`·`crosscheck.go` `SelectChecker`/`ConfirmDeadline`·`power.go` `RunOff`/`askChoice`), "허용 경로·user 규칙"이면 `config.go`/`target.go`, "교차검증 관문(확인자 선택·호출 순서)"이면 `crosscheck.go`(안전장치 단계에 끼우는 자리는 `guard.go` 의 `ccStages`/`AfterCountdown`, conf 키는 `types.go` + `config.go` + `pwonoff.conf.example`), "교차검증 데몬의 상태·단계·사유·한도·인증·conf 키"이면 `cross_check/internal/daemon/`(`server.go`, `protocol.go`, `conf.go`), "붙여넣기 대조 규칙"이면 `cross_check/internal/verify/`, "교차검증 전체 화면·키"이면 `cross_check/internal/tui/screen*.go`(줄 입력 화면은 `user.go`/`admin.go`), "cross_check 옵션·환경변수·종료 코드·호출 스크립트 감지"이면 `cross_check/cmd/cross_check/`(`main.go`, `caller.go`) 만 보면 됩니다. cross_check 의 `approval_ttl_sec`·인증 conf 를 바꾸면 `cross_check/README.md` 도 함께 고칩니다.

작업 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.
