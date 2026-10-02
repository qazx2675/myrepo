# ARCHITECTURE

## 폴더 구조 및 파일 역할

| 파일/폴더 | 역할 | 비고 |
|---|---|---|
| `main.go` | 최상단 빈 변수 4개 + CLI 분기 + 상수(주기/대기 시간) | 실행 대상 |
| `daemon.go` | 데몬 루프: queue 수거·경로 판별·ping 감시·준비확인·스케줄·run 큐 | 상태기계 중추 |
| `state.go` | Job/Host 구조체, JSON 원자 저장/복원 (재기동 후 이어하기) | 상태 영속화 |
| `icmp.go` | raw 소켓 ping (echo id+seq 로 호스트 매핑) | local 경로 |
| `probe.go` | probe 하위명령(os6_mgmt 쪽) + os6 장기 ssh 세션 관리 | os6 경로 |
| `check.go` | 준비확인: gossh -pm uptime, anaconda 감지, READY 판정 (local/os6) | 설치 완료 감지 |
| `runner.go` | os_check 실행(-auto), os6 gossh 래퍼 생성, code 파일 작성, wall 발송 | run 실행 |
| `ensure.go` | ensure 하위명령: flock + setsid + daemon 백그라운드 기동 | cron 훅 |
| `log.go` | /tmp/auto_setup/auto_setup.log 관리 (10MB 롤오버) | 로깅 |
| `notify.go` | wall 알림 발송 | 결과 공유 |
| `iface.go` | Pinger/Checker/Runner/Notifier/Clock 인터페이스 | 테스트 주입점 |
| `*_test.go` | 단위 테스트: state_test, daemon_test, icmp_test, probe_test, check_test, runner_test | 검증 |
| `setup.sh` | 빌드 + /usr/local/bin 설치 + cron + tmpfiles 등록 (멱등) | 설치 도구 |
| `build_os6.sh` | Go 1.20 정적 빌드 → auto_setup_os6 (CGO_ENABLED=0) | os6 빌드 |
| `test/` | 대화형 점검, 목업 E2E, 스텁(gossh/ssh/wall) | 검증 하네스 |
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
├── queue/
│   ├── <epoch>_<user>_<pid>.job    # 01 이 전달한 작업 (user=, time=, 호스트줄)
│   └── ...
├── jobs/
│   ├── <jobid>.json       # 진행 중 작업 상태 (hosts, runs, 스케줄 정보)
│   └── done/
│       └── <jobid>.json   # 완료·취소된 작업
├── runs/
│   ├── <code>/
│   │   ├── targets.txt    # 이 run 의 대상 호스트 목록
│   │   ├── dhcp.sh        # os_check 링크 (선택적)
│   │   └── os_check.log   # os_check 실행 출력
│   └── ...
├── codes/
│   ├── <code>.txt         # 결과 코멘트 (결과리포트 + FAIL + LDAP·Splunk·커널 + 원본경로)
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

## 수정 요청 가이드

| 요청 | 확인 대상 | 참고 |
|---|---|---|
| 빈 변수 4개 변경 | `main.go` 최상단, README 빈 변수 표 | 커밋 시 빈 값 유지 |
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

## 테스트 주입점 (iface.go)

테스트에서 다음 구현체를 교체해 검증합니다:

| 인터페이스 | 테스트 구현 | 목적 |
|---|---|---|
| `Pinger` | `fakePinger` | ICMP 응답 시뮬레이션(up/down 시퀀스) |
| `Checker` | `fakeChecker` | 준비확인 결과(READY/미준비) 시뮬레이션 |
| `Runner` | `fakeRunner` | os_check 실행 결과(코드·호스트·abnormal) 시뮬레이션 |
| `Notifier` | `fakeNotifier` | wall 메시지 캡처 |
| `Clock` | `fakeClock` | 시간 진행(타이머 테스트: 7분/3분) |

- 단위 테스트(state_test, daemon_test 등)에서 가짜를 직접 주입
- E2E(run_e2e.sh)에서 bash 스텁(gossh, ssh, wall) 사용

## 흐름도

전체 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.
