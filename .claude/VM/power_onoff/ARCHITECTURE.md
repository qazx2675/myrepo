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
| setup.sh, install.sh, build_os6.sh | 오프라인 빌드 / `/usr/local/bin`·conf 설치 / OS6(Go 1.20) 정적 빌드 |
| vendor/, go.mod, go.sum | 폐쇄망 빌드용 의존성(govmomi 0.55.1) |
| bin_os6/ | OS6 용 미리 빌드한 실행파일(gzip) |
| *_test.go | 단위 테스트. `*_vcsim_test.go`, `vcenter_test.go` 는 빌드 태그 `vcsim`(시뮬레이터 필요) |
| test/ | `run_tests.sh`(전체), `guard_scenarios.sh`, `e2e_*`(vcsim 위 전체 흐름), `vcsim_server/` |
| dev/sync_test.sh | 로컬 → 빌드 서버 동기화 후 go 명령 실행 |

수정 요청이 "안전장치 단계 변경"이면 `guard.go`, "부팅 완료 기준 변경"이면 `bootcheck.go`, "순차/병렬 규칙 변경"이면 `power.go`(`RunOnSeq`/`RunReboot`),
"VWP 판정 변경"이면 `vwp.go`, "conf 항목 추가"면 `types.go` + `config.go` + `pwonoff.conf.example`, "허용 경로·user 규칙"이면 `config.go`/`target.go` 만 보면 됩니다.

작업 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.
