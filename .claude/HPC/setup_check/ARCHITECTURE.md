# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | 판정 로직 전체: 인자·설정 파싱, 호스트파일 파싱(출력 순서용), gossh 1차/2차 실행, 결과 파일 분류, 2차 출력 파싱, 출력 |
| `main_test.go` | 결과 파일 분류·2차 출력 파싱·문구 이스케이프 단위 테스트 |
| `go.mod` | `go 1.20`, 표준 라이브러리만 사용 (vendor 불필요) |
| `new_setup.sh` | bash 래퍼. `/etc/redhat-release` 가 6.x 이면 `bin/new_setup_os6`, 아니면 `bin/new_setup` 실행 (인자 그대로 전달) |
| `new_setup.conf.sample` | 설정 파일 예시 (`GOSSH`, `DISK_PATTERN`, `ERROR_PATTERN`) |
| `build.sh` | 바이너리 빌드. `build.sh` → `bin/new_setup`, `build.sh os6` → `bin/new_setup_os6` |
| `bin/new_setup` | os8_mgmt 용 빌드 완료 바이너리 (커밋 대상) |
| `bin/new_setup_os6` | os6_mgmt(RHEL6) 용 빌드 완료 바이너리 (커밋 대상 — 회사에 Go 없음) |
| `test/fake_gossh.sh` | gossh 대역. `FAKE_GOSSH_SCENARIO=<파일>` 의 `host<TAB>시나리오` 대로 결과 파일·출력을 흉내 |
| `test/run_tests.sh` | fake gossh 로 전체 시나리오·출력 형식 검증 |
| `.github/workflows/ci.yml` | gofmt·vet·test·run_tests.sh·shellcheck |
| `README.md` / `PR_CHECKLIST.md` | 사용 설명 / 변경 점검 목록 |
| `계획서.md` | 설계 결정, 가정·미확인 사항 |

수정 요청별로 볼 곳:

- **"판정 문구 추가·변경"** → 코드 수정 없이 `new_setup.conf` (`DISK_PATTERN` / `ERROR_PATTERN`)
- **"2차 로그 확인 명령 변경"** → `main.go` 의 2차 명령 생성부 + `main_test.go` + `test/fake_gossh.sh`
- **"상태 추가·판정 규칙 변경"** → `main.go` 분류 로직 + 테스트 + README 3.3 표
- **"호스트파일 형식 변경"** → `main.go` 호스트 파싱 (gossh 와 같은 규칙을 유지해야 함)
- **"래퍼의 바이너리 선택 변경"** → `new_setup.sh`
- **소스 변경 시** → `build.sh` / `build.sh os6` 로 두 바이너리 재빌드 후 함께 커밋

## 판정 흐름

```
new_setup.sh <호스트파일>
  └ OS 판별 (/etc/redhat-release 6.x ?) → bin/new_setup_os6 | bin/new_setup

main (new_setup [-c 설정파일] <호스트파일>)
  [0] 설정 읽기 (new_setup.conf: GOSSH / DISK_PATTERN / ERROR_PATTERN)
      gossh 위치 결정 (GOSSH → 비면 PATH)  ── 없음/설정 오류 → exit 2
      호스트파일 읽기 → 출력 순서 목록 (gossh 와 같은 규칙, 중복 제거)
      <호스트파일>_new_setup.log 비우기
      이전 실행의 gossh 결과 파일 5종(_res_off 등) 삭제 (실제 gossh 가 항상 지우지는 않음)

  [1] 1차: gossh -w <호스트파일> -pm -script uptime
        ├ _res_off, _res_refsed, _res_cancel 에 있는 호스트 ──→ 접속불가
        ├ _os_install 에 있는 호스트 ────────────────────────→ 설치중 후보 → [2]
        └ 나머지 ───────────────────────────────────────────→ 완료

  [2] 2차 (후보가 있을 때만):
        후보를 <호스트파일>_new_setup_retry 에 기록
        gossh -w <그 파일> -script '<anaconda.log 확인 명령>'
          stdout "host: NS_DISK"  ──→ 디스크미인식   (우선순위 1)
          stdout "host: NS_ERR"   ──→ 설치정지       (우선순위 2)
          "NS_NOLOG" / 출력 없음 / 2차 접속 실패 ──→ OS설치중 (실패 시 stderr 경고)
          (원격 명령은 항상 끝에 NS_DONE 을 찍는다 — 표식이 전혀 없으면 응답 없음으로 보고 경고)
        임시 파일과 그 결과 파일 삭제

  [3] 입력 순서대로 stdout 에 "hostname<TAB>상태"
      stderr 에 상태별 대수 요약
      exit 0 (판정 완료) / 1 (사용법 오류) / 2 (설정·gossh 오류)

gossh 의 stdout/stderr 는 모두 <호스트파일>_new_setup.log 로 (화면에 섞지 않음)
```
