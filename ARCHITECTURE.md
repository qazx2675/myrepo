# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | 판정 로직 전체: 인자·설정 파싱, 호스트파일 파싱(출력 순서용), gossh 1차(정보수집)/2차(anaconda.log) 실행, 결과 파일 분류, 1차·2차 출력 파싱(CPU·stress-ng·H/T·OS), config_check.sh `-auto-check` 실행·판정(설정체크), 장애 목록(-er/error_list.txt) 읽기, 8열 출력 |
| `main_test.go` | 결과 파일 분류·2차 출력 파싱·문구 이스케이프 단위 테스트 |
| `go.mod` | `go 1.20`, 표준 라이브러리만 사용 (vendor 불필요) |
| `new_setup.sh` | bash 래퍼. `/etc/redhat-release` 가 6.x 이면 `bin/new_setup_os6`(옛 버전), 아니면 `bin/new_setup` 실행 (인자 그대로 전달) |
| `new_setup.conf.sample` | 설정 파일 예시 (`GOSSH`, `DISK_PATTERN`, `ERROR_PATTERN`, v2: `OS_CHECK_SH`, `OS_CHECK_EXCLUDE`, `CPU_INTEL_LOG/OK/LINE`, `CPU_AMD_LOG/OK/LINE`) |
| `build.sh` | 바이너리 빌드. `build.sh` → `bin/new_setup`, `build.sh os6` → `bin/new_setup_os6` (옛 방식, 현재 사용 안 함) |
| `bin/new_setup` | os8_mgmt 용 빌드 완료 바이너리 (커밋 대상) |
| `bin/new_setup_os6` | os6_mgmt(RHEL6) 용 **옛 버전**(v2 시기: 탭 출력, 제목 줄·NOTRUN·AMD 규칙 없음) 바이너리. 저장소에 남겨 두되 2026-11-04 결정으로 **더 이상 재빌드하지 않음** |
| `test/fake_gossh.sh` | gossh 대역. `FAKE_GOSSH_SCENARIO=<파일>` 의 `host<TAB>시나리오` 대로 결과 파일·출력을 흉내. 1차 `ok` 장비는 3번째 열 이후 `vendor= cpu= stress= tpc= kernel=` 로 정보수집 출력 흉내 |
| `test/fake_config_check.sh` | config_check.sh 대역. `-auto-check <user> <목록>` 을 받아 `FAKE_CC_SCENARIO=<파일>`(`host<TAB>pass|fail|none`)대로 현재 폴더에 `check.res_<user>` 를 씀 (`-auto-check` 문자열 포함) |
| `test/run_tests.sh` | fake gossh 로 전체 시나리오·출력 형식 검증 |
| `.github/workflows/ci.yml` | gofmt·vet·test·run_tests.sh·shellcheck |
| `README.md` / `PR_CHECKLIST.md` | 사용 설명 / 변경 점검 목록 |
| `계획서.md` | 설계 결정, 가정·미확인 사항 |

수정 요청별로 볼 곳:

- **"판정 문구 추가·변경"** → 코드 수정 없이 `new_setup.conf` (`DISK_PATTERN` / `ERROR_PATTERN`)
- **"2차 로그 확인 명령 변경"** → `main.go` 의 2차 명령 생성부 + `main_test.go` + `test/fake_gossh.sh`
- **"CPU·stress-ng·H/T·OS 판정 변경"** → `main.go` 의 1차 정보수집 명령 생성부·파싱 + `main_test.go` + `test/fake_gossh.sh`
- **"설정체크 판정·실행 방식 변경"** → `main.go` 의 config_check 실행부 + `test/fake_config_check.sh`
- **"상태 추가·판정 규칙 변경"** → `main.go` 분류 로직 + 테스트 + README 3.3 표
- **"호스트파일 형식 변경"** → `main.go` 호스트 파싱 (gossh 와 같은 규칙을 유지해야 함)
- **"래퍼의 바이너리 선택 변경"** → `new_setup.sh`
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
