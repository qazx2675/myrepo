# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | 판정 로직 전체: 인자·설정 파싱, 호스트파일 파싱(출력 순서용), gossh 1차(정보수집)/2차(anaconda.log) 실행, 결과 파일 분류, 1차·2차 출력 파싱(CPU·stress-ng·H/T·OS), config_check.sh `-auto-check` 실행·판정(설정체크), 장애 목록(-er/error_list.txt) 읽기, 8열 출력 |
| `main_test.go` | 결과 파일 분류·2차 출력 파싱·문구 이스케이프 단위 테스트 |
| `execx.go` `execx_linux.go` `execx_other.go` | 외부 명령 실행(`runCmd`, gossh 는 `main.go` 의 `runGossh` 가 이를 사용): 시간 제한, 프로세스 그룹 SIGTERM → 5초 뒤 SIGKILL, 중지 요청 시 취소(`errTimeout`/`errCanceled`) |
| `state.go` | `-run` 상태 파일: `state.json` 원자 기록·읽기, `answer.json`·`stop.req`, 호스트 행·질문 구조, 결과표(`result.txt`) 문자열 |
| `worker.go` | `-run` 백그라운드 워커 상태기계(판정→설정수정→부하→stress-ng→완료). `executor` 인터페이스로만 외부 동작을 하므로 시험은 가짜 실행기·가짜 시각으로 돌림. 대상 선정, 질문/답, 재기동, 중지, 설정체크 `-` 규칙(`checkPass`) |
| `stress.go` | 실제 `executor`: 판정 재사용, `config_check -auto`, stress-test `-batch` 호출(`[감시]` 줄로 launch 완료 감지), CPU 감시 확인(btime + 1차 명령) |
| `daemon_linux.go` `daemon_other.go` | setsid 로 워커 분리, flock 으로 중복 워커 방지·생존 확인(리눅스 외는 단순 대체) |
| `run.go` | `-run` 진입: 새로 시작 / 다시 붙기 / 이어서 진행(y/N) / 결과 보기, user 선택(`USER_ROUTE`), 비 tty 일 때 글 출력 |
| `tui.go` `render.go` `tty_linux.go` `tty_other.go` | TUI: termios raw 모드·키 읽기·화면 갱신 / 상태 → 화면 문자열 순수 함수(골든 시험) / tty·창 크기 |
| `*_test.go` | 단위 시험: 판정·상태기계·렌더·TUI 키·실행기 시간 제한 |
| `go.mod` | `go 1.20`, 표준 라이브러리만 사용 (vendor 불필요) |
| `new_setup.sh` | bash 래퍼. `/etc/redhat-release` 가 6.x 이면 `bin/new_setup_os6`(옛 버전), 아니면 `bin/new_setup` 실행 (인자 그대로 전달). 설정 파일이 없고 `-c` 없고 stdin·stderr 가 tty 이면 `setup.sh` 실행을 제안(`NEW_SETUP_NO_PROMPT=1` 이면 생략) |
| `new_setup.conf.sample` | 설정 파일 예시 (`GOSSH`, `DISK_PATTERN`, `ERROR_PATTERN`, v2: `OS_CHECK_SH`, `OS_CHECK_EXCLUDE`, `CPU_INTEL_LOG/OK/LINE`, `CPU_AMD_LOG/OK/LINE`, `-run`: `STRESS_TEST`, `STRESS_TEST_CONF`, `USER_ROUTE`, `CPU_WAIT_START/INTERVAL/MAX`, `GOSSH_TIMEOUT`, `CHECK_TIMEOUT`, `STRESS_TIMEOUT`) |
| `setup.sh` | 최초 설정 도우미(대화형 bash). `-c <conf>`(기본 `setup.sh` 옆 `new_setup.conf`)·`--yes`·`--help`. 샘플 또는 기존 conf 에서 시작해 질문(기본·부하·고급) → 검증 경고·다시 입력 → `이전→새 값` 요약·확인 → 임시 파일 작성 후 `mv`(원자적), 이전 파일은 `<conf>.bak`. conf 폴더 밖(/etc 등)에는 쓰지 않음. 끝에 `<STRESS_TEST 폴더>/setup.sh` 가 있으면 `--user <u> [--conf <STRESS_TEST_CONF>]` 로 이어하기 제안(stress-test 도구 소유, tty 필요). 종료코드 0/1/2 |
| `troubleshoot.sh` | `-doctor` 래퍼. `new_setup.sh` 와 같은 바이너리 선택 후 `new_setup -doctor "$@"` 실행(읽기 전용) |
| `doctor.go` | `-doctor` 보고서: `docReport`(`[O]/[!]/[X]/[i]` 줄·문제 요약·종료코드), 1 환경 점검(바이너리·설정·gossh·OS_CHECK_SH·STRESS_TEST·USER_ROUTE·쓰기·위험어/`!`), 2 `-run` 작업 진단(state.json·워커·heartbeat·질문·stop.req·CPU 시간표·호스트·worker.log·stress-test 이력). 외부 동작은 `doctorDeps` 로 주입 |
| `doctor_host.go` | 3 호스트 단건 점검(`-host [-check]`: 임시 폴더에 한 줄 목록 → 1차·2차 명령 + btime·uptime·`ls`(csh 안전 조각) → 열 판정 근거 설명, `-check` 는 `-auto-check` 만) 과 진입점 `doctorMain`·인자 검사, 보고서 저장(`./new_setup_doctor_<시각>.txt`, 비밀번호 마스킹) |
| `doctor_test.go` | 진단 단위 시험: 상태 폴더 없음·워커 사망·질문 대기·heartbeat·stop.req·완료·CPU 시간표·launch 미확인 등 (가짜 의존성) |
| `test/run_setup_tests.sh` | `setup.sh`·`new_setup.sh` 첫 실행 제안 비대화형 시험(답을 파이프로 입력, 임시 폴더에서만 작업): 새로 만들기·값 지정·유지·`-` 비우기·경고/다시 입력·고급·원자 저장/백업·인자 오류·`-c`·stress-test 이어하기·비 tty 에서 제안 생략 |
| `build.sh` | 바이너리 빌드. `build.sh` → `bin/new_setup`, `build.sh os6` → `bin/new_setup_os6` (옛 방식, 현재 사용 안 함) |
| `bin/new_setup` | os8_mgmt 용 빌드 완료 바이너리 (커밋 대상) |
| `bin/new_setup_os6` | os6_mgmt(RHEL6) 용 **옛 버전**(v2 시기: 탭 출력, 제목 줄·NOTRUN·AMD 규칙 없음) 바이너리. 저장소에 남겨 두되 2026-11-04 결정으로 **더 이상 재빌드하지 않음** |
| `test/fake_gossh.sh` | gossh 대역. `FAKE_GOSSH_SCENARIO=<파일>` 의 `host<TAB>시나리오` 대로 결과 파일·출력을 흉내. 1차 `ok` 장비는 3번째 열 이후 `vendor= cpu= stress= tpc= kernel=` 로 정보수집 출력 흉내 |
| `test/fake_config_check.sh` | config_check.sh 대역. `-auto-check` 또는 `-auto` 를 `<user> <목록>` 과 함께 받아 (`FAKE_CC_STATE` 폴더가 있으면 `-auto` 가 그 호스트의 fail 을 pass 로 바꿈) `FAKE_CC_SCENARIO=<파일>`(`host<TAB>pass|fail|none`)대로 현재 폴더에 `check.res_<user>` 를 씀 (`-auto-check` 문자열 포함) |
| `test/run_tests.sh` | fake gossh 로 전체 시나리오·출력 형식 검증 |
| `test/fake_stress_test.sh` | stress-test 대역. `-user -hosts -steps a 또는 b -batch [-conf]` 를 받아 호출을 `FAKE_ST_LOG` 에 기록, `[감시]` 줄을 낸 뒤 `FAKE_MARK_DIR` 표식(`cpu_`/`ng_`/`reboot_`/`hang_<steps>`)으로 fake gossh 의 CPU·stress-ng·btime 값을 바꿈 |
| `test/run_flow_tests.sh` | `-run` 전체 흐름 시험(비 tty 로 워커를 띄우고 `answer.json` 으로 답): 1→5 단계, 설정수정 `-auto`, CPU/stress 대상 분리·순서, 재부팅 감지, 접속불가 무시, stress-test 멈춤→시간초과, `stop.req`, 워커 kill -9→재기동 |
| `.github/workflows/ci.yml` | gofmt·vet·test·shellcheck·run_tests.sh·run_flow_tests.sh·run_setup_tests.sh |
| `README.md` / `PR_CHECKLIST.md` | 사용 설명 / 변경 점검 목록 |
| `계획서.md` | 설계 결정, 가정·미확인 사항 |
| `계획서_run.md` | `-run` 설계 결정(인터뷰 결과)·상태기계·검증 계획 |

수정 요청별로 볼 곳:

- **"판정 문구 추가·변경"** → 코드 수정 없이 `new_setup.conf` (`DISK_PATTERN` / `ERROR_PATTERN`)
- **"2차 로그 확인 명령 변경"** → `main.go` 의 2차 명령 생성부 + `main_test.go` + `test/fake_gossh.sh`
- **"CPU·stress-ng·H/T·OS 판정 변경"** → `main.go` 의 1차 정보수집 명령 생성부·파싱 + `main_test.go` + `test/fake_gossh.sh`
- **"설정체크 판정·실행 방식 변경"** → `main.go` 의 config_check 실행부 + `test/fake_config_check.sh`
- **"상태 추가·판정 규칙 변경"** → `main.go` 분류 로직 + 테스트 + README 3.3 표
- **"호스트파일 형식 변경"** → `main.go` 호스트 파싱 (gossh 와 같은 규칙을 유지해야 함)
- **"`-run` 단계·대상 선정·질문 변경"** → `worker.go` + `worker_test.go` (+ `test/run_flow_tests.sh`), 화면은 `render.go`/`tui.go`
- **"stress-test 호출·CPU 감시 변경"** → `stress.go` + `worker.go` 의 `doLoad` + `test/fake_stress_test.sh`
- **"`-run` 시간 제한·중지 변경"** → `execx.go`, `worker.go`(`watchStop`)
- **"래퍼의 바이너리 선택 변경"** → `new_setup.sh` + `troubleshoot.sh` (같은 선택 로직 두 곳)
- **"설정 도우미 질문·검증·저장 방식 변경"** → `setup.sh` + `test/run_setup_tests.sh` (+ `new_setup.conf.sample`)
- **"진단(-doctor) 점검·안내 문구 변경"** → `doctor.go`(환경·`-run`) / `doctor_host.go`(단건) + `doctor_test.go`. 읽기 전용 유지
- **소스 변경 시** → `build.sh` 로 `bin/new_setup`(os8) 만 재빌드 후 함께 커밋 (`bin/new_setup_os6` 는 재빌드 안 함)

## 판정 흐름

```
new_setup.sh <호스트파일>
  └ OS 판별 (/etc/redhat-release 6.x ?) → bin/new_setup_os6 | bin/new_setup

main (new_setup [-c 설정파일] [-er 장애목록파일] <호스트파일>)
  [0] 설정 읽기 (-c → 현재 경로 → 실행 파일 폴더 → bin/ 위 폴더의 new_setup.conf)
      키: GOSSH / DISK_PATTERN / ERROR_PATTERN / OS_CHECK_SH / OS_CHECK_EXCLUDE / CPU_INTEL_LOG·OK·LINE / CPU_AMD_LOG·OK·LINE (값 따옴표 제거)
      gossh 위치 결정 (GOSSH → 비면 PATH)  ── 없음/설정 오류 → exit 2
      호스트파일 읽기 → 출력 순서 목록 (gossh 와 같은 규칙, 중복 제거)
      <호스트파일>_new_setup.log 비우기
      이전 실행의 gossh 결과 파일 5종(_res_off 등) 삭제 (실제 gossh 가 항상 지우지는 않음)

  [1] 1차 (정보수집): gossh -w <호스트파일> -pm -script '<정보수집 명령>'   (-pm 은 설치중 장비의 명령 실행을 건너뜀)
        ├ _res_off, _res_refsed, _res_cancel 에 있는 호스트 ──→ 접속불가
        ├ _os_install 에 있는 호스트 ────────────────────────→ 설치중 후보 → [2]
        └ 나머지 ───────────────────────────────────────────→ 완료
            + 출력 파싱: vendor_id → 벤더, NS_CPU_INTEL/NS_CPU_AMD → CPU테스트 O (그 벤더 로그 파일이 없다는 표식 → NOTRUN, 나머지 X;
              AMD 기본: LINE=Result 인 줄이 1개 이상이고 모두 PASS),
              NS_STRESS → stress-ng, Thread(s) per core → H/T, NS_KERNEL → OS

  [2] 2차 (anaconda.log, 설치중 후보가 있을 때만):
        후보를 <호스트파일>_new_setup_retry 에 기록
        gossh -w <그 파일> -script '<anaconda.log 확인 명령>'
          stdout "host: NS_DISK"  ──→ 디스크미인식   (우선순위 1)
          stdout "host: NS_ERR"   ──→ 설치정지       (우선순위 2)
          "NS_NOLOG" / 출력 없음 / 2차 접속 실패 ──→ OS설치중 (실패 시 stderr 경고)
          (원격 명령은 항상 끝에 NS_DONE 을 찍는다 — 표식이 전혀 없으면 응답 없음으로 보고 경고)
        임시 파일과 그 결과 파일 삭제

  [3] 3차 (설정체크, OS_CHECK_SH 설정 시 · 관리서버에서 실행):
        임시 폴더에 완료 장비만 targets.txt 로 기록
        bash <OS_CHECK_SH> -auto-check new_setup targets.txt   (config_check.sh 가 gossh -pm 으로 각 장비에서 run.sh pd 실행)
        -auto-check 미지원 스크립트 → 실행하지 않고 설정체크 '-' + stderr 경고
        check.res_new_setup 호스트별: FAIL 줄 있음 → X / 줄 있고 FAIL 없음 → O / 줄 없음 → 출력없음
        OS_CHECK_EXCLUDE 문자열이 hostname 에 포함 → 무조건 O
        화면 출력은 <호스트파일>_new_setup.log 에 이어 붙이고 임시 폴더 삭제

  [4] stdout 첫 줄은 제목 줄 "hostname|상태|설정체크|CPU테스트|stress-ng|H/T|OS|비고", 이어서 입력 순서대로 장비당 같은 8열('|' 구분) 한 줄. 비고 = parseErrorList(-er 파일, 없으면 ./error_list.txt) 의 사유
      완료가 아닌 장비는 나머지 5열이 모두 '-'
      stderr 에 상태별 대수 요약
      exit 0 (판정 완료) / 1 (사용법 오류) / 2 (설정·gossh 오류)

gossh·config_check 의 stdout/stderr 는 모두 <호스트파일>_new_setup.log 로 (화면에 섞지 않음)
```

## -run 흐름 (new_setup -run [-c conf] [-er 파일] [-user u] <호스트파일>)

```
new_setup -run  (run.go)
  ├ ./<호스트파일>_run/ 없음 → 사용자 선택(USER_ROUTE 메뉴 | -user | $USER) → state.json 생성 → 워커 기동 → 화면
  ├ 있고 워커 살아 있음(lock) → 화면만 붙음
  ├ 있고 워커 없음·미완료     → "이어서 진행할까요?(y/N)" → 워커 재기동 → 화면
  └ 끝남                      → 결과 화면 + 결과 파일 경로          (tty 가 아니면 TUI 대신 글 한 번 출력)

워커 (new_setup -run-worker <상태폴더>, setsid, flock, worker.go)  ── executor 로만 외부 동작
  1 판정      judge(): 1차·2차·설정체크(-auto-check) → 표 갱신 → 질문(c/r/x/s)
  2 설정수정  config_check.sh -auto <user> <설정체크 O 아님> → 재판정 → 질문
  3 부하      CPU 대상(설정체크 통과·CPU NOTRUN): stress-test -steps a
              └ stdout 의 "[감시]" 줄이 처음 보이면(등록·launch 끝) launch 시각 기록 + btime 기록
              stress-ng 대상(CPU O·stress-ng O 아님·CPU 대상 아님): 그 뒤에 stress-test -steps b (감시는 동시)
              CPU 감시: CPU_WAIT_INTERVAL 마다 btime·응답, CPU_WAIT_START 부터 CPU 열 값도 확인, CPU_WAIT_MAX 에 마감
              → 재판정 → 질문
  4 stress-ng 이번 CPU 대상 중 CPU O 가 된 호스트: stress-test -steps b → 재판정 → 질문
  5 완료      result.txt 저장(중지여도 저장)
  ※ 대상이 없는 단계는 건너뜀. 각 외부 명령은 시간 제한(→ 시간초과 질문 r/s), stop.req 가 오면 즉시 취소.
  ※ 설정체크 통과 = O, 또는 OS_CHECK_SH 가 비어 있을 때의 '-'. OS_CHECK_SH 가 있는데 '-' 이면 통과 아님(설정수정·x 대상).
  ※ 재기동(kill -9 등): 진행 중이던 외부 명령은 다시 실행하지 않고 재체크부터. CPU launch 기록이 있고 감시 미완이면 감시만 이어 감.

상태 폴더: state.json(워커만 씀, 원자 기록) / answer.json(TUI → 워커) / stop.req / worker.pid·lock / worker.log / lists/ / result.txt
TUI (tui.go, render.go): state.json 을 1초마다 읽어 그림, 키 c r x s S f q ? ↑↓ — 답은 answer.json, 중지는 stop.req 도 가능
```

## 설정 도우미 흐름 (setup.sh [-c conf] [--yes])

```
new_setup.sh <인자>   (설정 파일 없음 · -c 없음 · stdin/stderr 가 tty · NEW_SETUP_NO_PROMPT 비어 있음)
  └ "지금 설정할까요? (Y/n)" → bash setup.sh   (아니오/비 tty 는 기본값으로 계속)

setup.sh
  [0] 인자 검사 (-c 폴더/없는 폴더 → exit 2), 시작점 = 기존 conf 또는 new_setup.conf.sample (주석·줄 유지)
  [1] 질문: 기본(GOSSH, OS_CHECK_SH, OS_CHECK_EXCLUDE) → 부하(STRESS_TEST, STRESS_TEST_CONF, USER_ROUTE) → 고급 y/N(CPU_* 판정, CPU_WAIT_*, *_TIMEOUT)
        Enter = 유지, '-' = 비움, 경로·값 검증 경고 → 다시 입력(y/N)
  [2] 요약(이전 → 새 값) → 저장 확인 → 임시 파일 작성 → 기존 conf 를 <conf>.bak 으로 → mv (conf 폴더 밖에는 쓰지 않음)
  [3] <STRESS_TEST 폴더>/setup.sh 가 있으면 이어하기 제안 → bash <폴더>/setup.sh --user <u> [--conf <STRESS_TEST_CONF>]
        (stress-test 도구 소유 · tty 필요 · <폴더>/conf/<u>_setting.conf 에 AWX/repohost/템플릿 설정)
  종료코드 0 완료·변경 없음 / 1 사용자 중단 / 2 인자·파일 오류
```

## 진단(-doctor) 흐름 (troubleshoot.sh [-c conf] [-user u] [<호스트파일> | -host <호스트> [-check]])

```
troubleshoot.sh → bin/new_setup -doctor ...   (os6 바이너리에는 -doctor 없음)
doctorMain (doctor_host.go) → 인자 검사(오류 exit 2) → runDoctor
  [1] 환경 점검 (항상, doctor.go)            설정·gossh(-h 로 옵션 확인)·OS_CHECK_SH·bash·STRESS_TEST(+conf)·USER_ROUTE·쓰기 권한·위험어·'!'·CPU 문구
  [2] -run 작업 진단 (호스트파일 + ./<호스트파일>_run/ 이 있을 때)
        state.json 읽기 → 버전·경로·conf·호스트 변경 → last_error → 워커 생존(lock/pid)·heartbeat(15초) → 질문·answer.json → stop.req
        → CPU 등록·감시 시간표(launch 10분 초과·마감·다음 확인 지연) → 호스트별 표 → worker.log·stress-test 이력
  [3] 호스트 단건 점검 (-host)
        임시 폴더에 호스트 한 줄 → gossh 1차·2차 명령 + btime·uptime·ls → 열별 판정 근거 설명 → (-check) config_check -auto-check
  요약(문제 목록 + 해결 안내) → stdout + ./new_setup_doctor_<시각>.txt(비밀번호 마스킹)
  종료코드 0 문제 없음 / 1 [!]·[X] 있음 / 2 인자 오류
  ※ 읽기 전용: conf·상태 폴더·원격 호스트를 바꾸지 않는다. 원격 명령은 csh/tcsh 에서도 동작하는 조각만 쓴다.
```
