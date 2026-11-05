# 계획서 — new_setup -run (설정수정·CPU·stress-ng 자동 진행 TUI)

작성 2026-11-05. 기존 `new_setup <파일>` (결과표 출력)은 그대로 두고 `-run` 을 추가한다.

## 0. 목적

접속 가능한 서버(상태 `완료`)의 **설정체크 / CPU테스트 / stress-ng** 세 열을 모두 `O` 로 만드는 과정을
한 도구로 진행한다. 단계가 넘어갈 때마다 결과를 다시 체크하고 사용자에게 **계속 / 재시도 / 제외하고 계속 / 중지** 를 묻는다.

## 1. 인터뷰 결정 사항

| 항목 | 결정 |
|---|---|
| 실행 | `new_setup -run [-c conf] [-er 파일] [-user u] <호스트파일>`. 같은 경로에서 다시 치면 진행 중 작업에 재접속 |
| 지속 | 작업은 setsid 로 띄운 **백그라운드 워커**가 하고 상태를 파일로 기록. TUI 는 상태를 보여주고 질문에 답함. 터미널이 끊겨도 작업 계속 |
| 상태 위치 | **현재 실행 경로** `./<호스트파일이름>_run/` (/etc 등 공용 경로에 아무것도 만들지 않음) |
| user | stress-test 와 같은 방식: conf `USER_ROUTE` 가 있으면 `info_mn.sh` 메뉴 → `info.sh <번호>`, 없으면 `-user` → `$USER`. 처음 한 번 정해 상태에 저장 |
| 설정수정 | auto_setup 과 같이 `config_check.sh -auto <user> <목록>` (체크 + 수정). 재체크는 기존 `-auto-check` |
| CPU 시작 조건 | **[변경] 설정체크 `-` 는 `OS_CHECK_SH` 가 비었을 때만 통과(O 와 동일). 있는데 `-` 이면 실패로 보고 설정수정·x 대상.** 설정체크가 **모두 O** 가 된 뒤 CPU 단계로. O 가 안 되는 호스트(예: `출력없음`=체크경로 미마운트)는 단계 질문에서 **제외하고 계속** 을 골라 뺄 수 있음(비고에 표시) |
| CPU 대상 | 설정체크 O 이고 CPU `NOTRUN`. Intel/AMD 분리 yaml·템플릿은 stress-test `-steps a` 가 처리 |
| stress-ng 대상 | CPU `O` 이고 stress-ng 가 O 아님. **이번 CPU 대상에 든 호스트는 제외** |
| 동시 실행 | CPU(stress-test -steps a)와 stress-ng(-steps b)는 **등록만 순서대로**(첫 번째의 등록·launch 가 끝난 뒤 두 번째 시작), 감시는 동시 |
| CPU 완료 확인 | launch 후 **47분**부터 **2분 간격**으로 conf 의 Intel/AMD 완료 양식(CPU 열 O)을 확인, **30분**(설정값) 동안 안 되면 미완료로 두고 질문 |
| 서버 꺼짐 | **[구현]** CPU 테스트 중(launch~확인 종료) `CPU_WAIT_INTERVAL` 마다 가벼운 확인(btime + 응답, 시작 전에는 CPU 열을 보지 않음). btime 이 바뀌면 비고 `실행도중 재부팅됨`, 응답 없으면 `실행도중 응답없음` |
| stress-ng 값 | `O` / `NOTRUN` (X 없음). messages 에 문자열 없으면 NOTRUN. stress-test 감시가 load ≥ 3(`load_threshold`) 이면 실행된 것으로 보고, 다음 재체크에서 messages 문자열이 보이면 O |
| CPU 뒤 stress-ng | CPU 완료 확인이 끝나면 재체크 → CPU O 가 된 호스트를 stress-ng 대상으로 올려 질문(계속이면 실행) — 같은 날 이어서 진행되도록 상태에 기억 |
| 행(hang) 대응 | 모든 외부 명령(gossh·config_check·stress-test)에 시간 제한. 넘으면 프로세스 그룹 SIGTERM → 5초 뒤 SIGKILL, 해당 단계 `시간초과` 로 질문 |
| 장애 목록 | `-er`/error_list.txt 는 비고 표시용일 뿐, 대상 선정에 영향 없음 |
| stress-test | **소스 수정 없음**. `-batch` 로 호출만 하며 단독 실행도 그대로 가능 |
| 대상 범위 | 상태 `완료` 호스트만 작업. 접속불가·OS설치중 등은 표에만 표시 |

## 2. 흐름 (워커 상태기계)

```
[1 판정]  기존 판정(1차 정보수집·2차 anaconda·설정체크 -auto-check) → 표 갱신
   │ 설정체크 O 아닌 완료 호스트 있음 → 질문(계속=설정수정 / 제외하고 계속 / 재시도=재판정 / 중지)
[2 설정수정] config_check -auto (O 아닌 호스트) → [1 판정] 재체크 → 질문 … 모두 O(또는 제외) 이면
[3 부하]  CPU 대상 → stress-test -steps a  (등록·launch 끝나면)  stress 대상 → stress-test -steps b
          stress 쪽: stress-test 종료 후 재체크
          CPU 쪽: launch 시각 기록 → 2분마다 btime·응답 감시 → 47분부터 2분마다 CPU 열 확인(최대 30분)
   → 재체크 → 질문(계속 / 재시도 / 제외하고 계속 / 중지)
[4 stress-ng] CPU O 가 된(이번 CPU 대상) 호스트 중 stress-ng 가 O 아님 → stress-test -steps b → 재체크 → 질문
[5 완료]  접속 가능 호스트 3열이 모두 O(또는 제외) → 최종 표를 ./<호스트파일>_run/result.txt 로 저장
```

- 질문 선택지: `c` 계속(다음 단계) / `r` 재시도(같은 단계 다시) / `x` O 아닌 호스트 제외하고 계속 / `s` 작업 중지.
- 각 단계 전에 대상이 없으면 그 단계를 건너뛴다(질문 없이 다음 단계 판정으로).

## 3. 상태 파일 (`./<호스트파일>_run/`)

| 파일 | 내용 |
|---|---|
| `state.json` | 단계·질문·호스트별 열 값·진행 비고·user·시각(launch, 마지막 확인) — 임시파일 → rename 원자 기록 |
| `answer.json` | TUI 가 쓴 답(질문 ID + 선택). 워커가 1초마다 읽고 지움 |
| `worker.pid` / `lock` | 워커 pid, flock(중복 워커 방지) |
| `worker.log` | 워커 진행 로그·외부 명령 출력 요약 |
| `lists/` | 단계별 대상 호스트 파일(stress-test·config_check 입력) |
| `result.txt` | 최종 결과표(`|` 8열) |

재접속: `-run` 실행 시 상태 디렉터리가 있으면 ① 워커 살아있음 → TUI 만 붙음 ② 워커 없음 + 미완료 → "이어서 진행할까요?" 후 워커 재기동(저장된 단계부터, 진행 중이던 외부 명령은 다시 실행하지 않고 재체크부터) ③ 완료 → 결과 표시.
tty 가 아니면 TUI 대신 현재 상태 한 번 출력 후 종료.

## 4. TUI

- auto_setup 규칙: 외부 라이브러리 없이 termios raw tty(`syscall` ioctl, linux), 화면은 상태 → 문자열 순수 함수(테스트 가능), 1초 갱신, 창 크기 반영, `NO_COLOR`.
- 화면: 머리(user·단계·경과·워커 상태) / 단계 진행 줄(1~5, 현재 강조) / 호스트표(`hostname|상태|설정체크|CPU|stress-ng|비고`, ↑↓ PgUp/PgDn) / 질문 줄 / 안내 줄.
- 키: `c` `r` `x` `s`(질문 있을 때, s 는 y/n 확인) / **[추가] `S` 질문이 없을 때 중지(y/N 확인, `stop.req` 로 실행 중 외부 명령도 취소)** / `f` O 아닌 호스트만 / `q` TUI 나가기(작업은 계속) / `?` 도움말.

## 5. 설정(new_setup.conf) 추가 키

| 키 | 기본 | 설명 |
|---|---|---|
| `STRESS_TEST` | (없음) | stress-test 바이너리 경로. 비면 부하 단계에서 오류 안내 |
| `STRESS_TEST_CONF` | (없음) | stress-test `-conf` 로 넘김(비면 stress-test 기본 탐색, 작업 폴더는 바이너리 폴더) |
| `USER_ROUTE` | (없음) | info_mn.sh/info.sh 폴더 |
| `CPU_WAIT_START` | `47m` | launch 후 CPU 완료 확인 시작 |
| `CPU_WAIT_INTERVAL` | `2m` | 확인·꺼짐 감시 간격 |
| `CPU_WAIT_MAX` | `30m` | 확인 시작 후 최대 대기 |
| `GOSSH_TIMEOUT` / `CHECK_TIMEOUT` / `STRESS_TIMEOUT` | `10m` / `40m` / `60m` | 외부 명령 시간 제한 |

시간 값은 Go duration(`47m`, `30s`) 또는 숫자(분).

## 6. 변경 범위

- `main.go` 판정: stress-ng 문자열 없음 → `NOTRUN` (X 제거), 판정 로직을 `-run` 워커에서 재사용할 수 있게 함수로 분리(동작 불변).
- 새 파일: `run.go`(진입·재접속·워커 기동), `worker.go`(상태기계), `state.go`(상태 파일), `execx.go`(시간 제한·프로세스 그룹), `stress.go`(stress-test 연동·CPU 감시), `tui.go`·`render.go`·`tty_linux.go`·`tty_other.go`.
- stress-test 폴더는 건드리지 않음.
- Go 1.20, 표준 라이브러리만, os8 바이너리만 빌드.

## 7. 검증

- 단위: 상태기계 전이(가짜 실행기 주입), 대상 선정(CPU·stress 겹침 제외), 꺼짐/재부팅 판정, 시간 제한 kill, 렌더 골든.
- 통합(랩 .58): fake gossh + fake config_check(`-auto` 면 FAIL → NO FAIL) + fake stress-test(launch 기록 후 종료), 시간 값 초 단위로 줄여 1→5 단계 전체, 워커 kill 후 재접속·재기동, 명령 hang → 시간초과.
- 실제 gossh 로 .58/.60 대상 판정·재접속 확인(부하 템플릿은 fake).
- 미검증 예정: 실제 AWX 템플릿·실제 CPU 45분 테스트·실장비 재부팅.

## 8. 진행 결과 (2026-11-05, Step 4)

| 항목 | 상태 |
|---|---|
| 판정 로직 분리(`judge`)·stress-ng `NOTRUN` | 완료 |
| 외부 명령 시간 제한·프로세스 그룹 kill(`execx.go`) | 완료 |
| 워커 상태기계·상태 파일·중복 방지·재기동(`worker.go`, `state.go`, `daemon_*.go`) | 완료 |
| stress-test 연동·CPU 감시(`stress.go`) | 완료 (stress-test 폴더 무수정) |
| `-run` 진입·재접속(`run.go`)·TUI(`tui.go`, `render.go`, `tty_*.go`) | 완료 |
| 설정체크 `-` 규칙 | **변경 완료**: `OS_CHECK_SH` 가 비었을 때만 통과. 있는데 `-` 이면 통과 아님(설정수정·재시도·`x` 목록에 듦). `runState.check_off` 로 화면 필터(`f`)도 같은 규칙 |
| 중지 | **추가**: 질문 중 `s`, 질문 없을 때 `S`, `stop.req`. 실행 중인 외부 명령을 즉시 취소하고 `result.txt` 저장 |
| CPU 감시 | **구체화**: launch 직후 btime 기록, 이후 `CPU_WAIT_INTERVAL` 마다 가벼운 확인(btime·응답), `CPU_WAIT_START` 부터 CPU 열 값 확인, `CPU_WAIT_MAX` 마감 |
| 단위 시험 | 완료 (`go test ./...`) |
| 통합 시험 `test/run_flow_tests.sh` | 완료: 1→5 단계, `-auto` 사용자, CPU/stress 대상 분리·순서, 재부팅 감지, 접속불가 무시, 시간초과 → 재시도, `stop.req`, 워커 kill -9 → 재기동, `-` 규칙 |
| 실제 gossh(.58/.60) 비 tty 실행 | 완료: 접속불가 호스트는 표에만 표시, 흐름 종료. `.60`(csh)에서 btime 감시 확인 동작 |
| README·ARCHITECTURE·PR_CHECKLIST·conf 샘플·`new_setup.sh`·`build.sh`(os6 은 전체 `*.go` 복사, 실행 안 함)·CI | 갱신 완료 |
| os8 바이너리 `bin/new_setup` 재빌드 | 완료 (os6 는 빌드 안 함) |

미검증: 실제 AWX 템플릿·실제 CPU 45분 테스트·실장비 재부팅·실제 config_check.sh `-auto`(수정) 동작·실제 stress-test `[감시]` 줄(소스 기준으로만 확인).
