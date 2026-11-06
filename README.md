# new_setup — 신규 OS 배포 후 설치 상태 판정 도구

신규 OS 배포 후 대상 장비 전체를 `gossh` 로 한 번에 점검해(설정체크는 관리서버에서 `config_check.sh` 실행), 장비마다 상태 하나(`완료` / `디스크미인식` / `설치정지` / `OS설치중` / `접속불가`)를 판정하고, 완료된 장비는 설정체크·CPU 테스트·stress-ng·H/T·OS 와 비고(장애 목록 사유)까지 8열로 출력하는 Go 기반 도구입니다. Go 바이너리 + bash 래퍼(`new_setup.sh`) 한 쌍으로 구성되며, 표준 라이브러리만 사용하므로 `vendor/` 가 필요 없습니다.

> 폴더·파일별 역할과 판정 흐름은 [ARCHITECTURE.md](ARCHITECTURE.md), 변경 시 점검 목록은 [PR_CHECKLIST.md](PR_CHECKLIST.md)를 참고하세요.

**목차**: 1 빌드·설치 / 2 사용 방법(**2.0 처음 설치 후 — `setup.sh` 대화형 설정**) / 3 옵션·상태별 상세 / 4 제약·한계 / 5 문서별 설명 / 6 `-run` 자동 진행 / **7 문제가 생겼을 때 — `troubleshoot.sh` 읽기 전용 진단**

| 하고 싶은 일 | 명령 |
|---|---|
| 처음 설치한 뒤 설정 파일 만들기 | `bash setup.sh` (2.0) |
| 설치 상태 판정 | `bash new_setup.sh <호스트파일>` (2장) |
| 설정수정·CPU·stress-ng 까지 자동 진행 | `bash new_setup.sh -run <호스트파일>` (6장) |
| 뭔가 이상할 때 원인 진단(읽기 전용) | `bash troubleshoot.sh ...` (7장) |

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 이 도구는 설정을 변경하지 않고 상태만 조회하지만, `OS설치중` 등 판정은 로그 문구 일치 여부에 기댄 추정이므로 중요한 장비는 무작위로 몇 대를 골라 직접 접속해 실제 상태를 확인하는 절차를 권장합니다.

## 1. 빌드 및 설치 방법

### 1.1 폐쇄망(회사) 환경 — 빌드 불필요

회사 환경에는 Go 가 없으므로 **빌드 완료된 바이너리를 저장소에 함께 커밋**해 두었습니다. 폴더를 통째로 내려받아 옮기면 바로 실행됩니다.

| 파일 | 대상 서버 |
|---|---|
| `bin/new_setup` | os8_mgmt (linux/amd64) |
| `bin/new_setup_os6` | os6_mgmt (RHEL6) 용 **옛 버전**(v2 시기 빌드: 탭 출력, 제목 줄·`NOTRUN`·AMD `Result`/`PASS` 규칙 없음). 저장소에는 남아 있지만 **더 이상 다시 빌드하지 않습니다**(2026-11-04 결정: OS6 빌드 중단) |

`new_setup.sh` 가 `/etc/redhat-release` 를 보고 RHEL 6.x 이면 `bin/new_setup_os6`, 그 외에는 `bin/new_setup` 을 자동으로 골라 실행하므로 사용자가 바이너리를 고를 필요는 없습니다.

```bash
# 인터넷이 되는 환경에서 저장소 다운로드 후 압축
tar czf new_setup.tar.gz ".claude/HPC/setup_check"

# 폐쇄망 서버에서
tar xzf new_setup.tar.gz
cd ".claude/HPC/setup_check"
chmod +x new_setup.sh bin/new_setup bin/new_setup_os6   # 실행 권한이 빠졌을 때만
```

필요 조건: 대상 서버에 `gossh` 가 설치되어 있어야 합니다(PATH 에 있거나 `new_setup.conf` 의 `GOSSH=` 로 경로 지정). 설정체크 열을 쓰려면 `-auto-check` 를 지원하는 `config_check.sh` 도 필요합니다(3.5 참고).

### 1.2 소스를 고쳐서 다시 빌드할 때 (Go 가 있는 호스트)

```bash
bash build.sh        # os8 용 → bin/new_setup
# bash build.sh os6  # (옛 방식·사용 안 함) os6 빌드는 2026-11-04 부터 중단
```

- `go.mod` 는 `go 1.20` 기준입니다. 그보다 새로운 문법·표준 라이브러리 API 를 쓰지 마세요.
- **소스를 바꿨다면 `bin/new_setup`(os8) 만 재빌드해 함께 커밋**해야 합니다(회사에 Go 가 없으므로). `bin/new_setup_os6` 는 재빌드하지 않고 옛 버전 그대로 둡니다. `.gitignore` 로 `bin/` 을 제외하지 마세요.

### 1.3 전역 명령어로 사용하기 (선택 사항)

`new_setup.sh` 는 자기 위치를 기준으로 `bin/` 과 `new_setup.conf` 를 찾으므로, 복사하지 말고 심볼릭 링크로 연결하는 것을 권장합니다.

```bash
ln -s "$(pwd)/new_setup.sh" /usr/local/bin/new_setup
# 이후 어디서든 new_setup <호스트파일>
# 현장 별칭이 필요하면 같은 방식으로: ln -s "$(pwd)/new_setup.sh" /usr/local/bin/setup_check
```

## 2. 사용 방법

### 2.0 처음 설치 후 — 대화형 설정 `setup.sh`

폴더를 옮겨 처음 쓸 때는 설정 파일(`new_setup.conf`)부터 만듭니다. 직접 편집하지 않고 질문에 답하면 됩니다.

```bash
bash setup.sh                  # setup.sh 옆의 new_setup.conf 를 만들거나 수정
bash setup.sh -c /경로/my.conf   # 다른 설정 파일 지정
bash setup.sh --yes            # 확인 질문 생략(시험·자동화용)
bash setup.sh --help
```

| 인자 | 설명 |
|---|---|
| `-c <설정파일>` | 쓸 설정 파일. 생략하면 `setup.sh` 옆의 `new_setup.conf` (new_setup 이 자동으로 찾는 위치) |
| `--yes` | 확인 질문 생략: 저장 확인=예, 경고 후 다시 입력=아니오(입력값 유지), stress-test 설정 이어하기=아니오 |
| `--help` | 도움말 |

- 설정 파일이 없으면 `new_setup.conf.sample` 을 복사해서 시작하고(주석 유지), 있으면 현재 값을 기본값으로 보여 줍니다.
- 입력 규칙: **Enter = 현재 값 유지, `-` = 값 비우기**. 경로가 없거나 실행할 수 없는 등 문제가 있으면 경고를 보이고 다시 입력할지 묻습니다(`n` 이면 입력값 그대로 유지).
- 질문은 세 묶음입니다.

| 묶음 | 키 |
|---|---|
| 기본 | `GOSSH`, `OS_CHECK_SH`, `OS_CHECK_EXCLUDE` |
| 부하(`-run`) | `STRESS_TEST`, `STRESS_TEST_CONF`, `USER_ROUTE` |
| 고급 (`y/N`, 기본 아니오) | CPU 판정(`CPU_INTEL_LOG/OK/LINE`, `CPU_AMD_LOG/OK/LINE`), CPU 감시 시간(`CPU_WAIT_START/INTERVAL/MAX`), 시간 제한(`GOSSH_TIMEOUT`, `CHECK_TIMEOUT`, `STRESS_TIMEOUT`) |

- 질문이 끝나면 **바뀐 값을 `이전 → 새 값` 으로 요약**해 보여 주고 저장할지 확인합니다. 저장은 임시 파일에 만든 뒤 `mv` 로 바꾸는 원자적 방식이며, 이전 파일은 `<설정파일>.bak` 로 남깁니다.
- 관리서버는 여러 사용자가 같이 쓰므로 **설정 파일이 있는 폴더 밖에는 아무것도 쓰지 않습니다**(`/etc` 등 공용 경로 없음).
- 배포 브랜치 `setup-check` 에는 부하테스트 도구가 `stress-test/` 폴더로 함께 들어 있습니다(원본은 master 의 `.claude/HPC/stress-test`, 단독 실행 가능). `STRESS_TEST` 가 비어 있고 `stress-test/stress-test` 가 있으면 그 경로를 기본값으로 보여 줍니다.
- 마지막에 `STRESS_TEST` 폴더에 `setup.sh` 가 있으면 stress-test 도구 자체 설정을 이어서 할지 묻습니다. `y` 면 `bash <STRESS_TEST 폴더>/setup.sh --user <사용자> [--conf <STRESS_TEST_CONF>]` 를 실행합니다. 이 스크립트는 **stress-test 도구의 것**이며(수정하지 않음) 터미널(tty)이 필요하고, AWX·repohost·템플릿 설정을 `<폴더>/conf/<사용자>_setting.conf` 에 채웁니다.
- 종료코드: 0 완료/변경 없음, 1 사용자 중단, 2 인자·파일 오류.
- **첫 실행 제안**: 래퍼 `new_setup.sh` 는 설정 파일을 찾지 못했고(`-c` 도 없음) 표준입력·표준오류가 터미널이면 `설정 파일이 없습니다. 지금 설정할까요? (Y/n)` 로 `setup.sh` 를 바로 실행하자고 합니다. 터미널이 아니면(파이프·cron·리다이렉트) 묻지 않고 기본값으로 진행하며, 환경변수 `NEW_SETUP_NO_PROMPT=1` 이면 항상 묻지 않습니다.

### 2.0.1 판정 실행

```bash
bash new_setup.sh <호스트파일>               # 화면에 결과 출력 (현재 경로의 new_setup.conf 자동 사용)
bash new_setup.sh <호스트파일> > result.txt   # 결과만 파일로 저장
bash new_setup.sh -c /경로/my.conf <호스트파일>   # 설정 파일 직접 지정
```

- `<호스트파일>` 은 gossh `-w` 에 그대로 넘기는 파일입니다(한 줄에 hostname 하나). 형식은 gossh 와 같습니다: 줄 앞뒤 공백 제거, 빈 줄·`#` 시작 줄 무시, 한 줄 안의 쉼표/공백 분리, `node[01-10]` 범위 확장(파일에서는 쉼표 분리가 먼저라 `a[1,3,5-7]` 은 gossh 도 깨지므로 쓰지 마세요), 중복은 먼저 나온 것만 유지.
- 현재 경로에 `new_setup.conf` 가 있으면 `new_setup.sh <호스트파일>` 은 `new_setup.sh -c ./new_setup.conf <호스트파일>` 과 같습니다.
- 출력은 열을 `|` 로 구분한 8열이며, **첫 줄은 항상 제목 줄**(`hostname|상태|설정체크|CPU테스트|stress-ng|H/T|OS|비고`), 그 아래에 **입력 파일 순서 그대로** 장비당 한 줄입니다.
- stdout 에는 제목 줄과 결과 줄만 나갑니다. 진행 안내·상태별 대수 요약·경고는 모두 stderr 로 나가므로 `> result.txt` 로 저장해도 결과 파일이 깨끗합니다.
- 작업 폴더는 호스트파일이 있는 폴더 그대로 쓰며, gossh 결과 파일(`<호스트파일>_res_off` 등)과 로그도 그 옆에 생깁니다.
- 설정수정·CPU 테스트·stress-ng 까지 이어서 자동 진행하려면 `bash new_setup.sh -run <호스트파일>` 을 씁니다(6장).
- 결과가 이상하거나 `-run` 이 멈춘 것 같으면 `bash troubleshoot.sh ...` 로 원인을 확인합니다(7장, 읽기 전용).

### 2.1 출력 예시

호스트파일 `a`:

```
node01
node02
node03
node04
node05
```

실행:

```
$ bash new_setup.sh a
hostname|상태|설정체크|CPU테스트|stress-ng|H/T|OS|비고
node01|완료|O|O|O|ON|4.18.0-553.el8_10.x86_64|
node02|완료|O|NOTRUN|X|ON|4.18.0-553.el8_10.x86_64|메모리 불량
node03|OS설치중|-|-|-|-|-|
node04|접속불가|-|-|-|-|-|전원 케이블 불량
node05|설치정지|-|-|-|-|-|
```

열 사이는 공백이 아니라 **세로선 `|` 1개**이고, 첫 줄은 항상 제목 줄입니다(위 예시 값은 예시). 제목 줄과 모든 결과 줄이 정확히 8필드인지 검증하려면 `awk -F'|' 'NF!=8' result.txt` 결과가 비어 있어야 합니다. 상태가 `완료` 가 아닌 장비는 3~7열을 모두 `-` 로 채우고, 비고(8열)는 장애 목록에 없으면 빈칸이므로 열 수는 항상 8개입니다.

| 열 | 값 |
|---|---|
| 1 hostname | 호스트파일의 이름 |
| 2 상태 | `완료` / `디스크미인식` / `설치정지` / `OS설치중` / `접속불가` |
| 3 설정체크 | `O` / `X` / `출력없음` / `-` |
| 4 CPU테스트 | `O` / `X` / `NOTRUN` |
| 5 stress-ng | `O` / `NOTRUN` |
| 6 H/T | `ON` / `OFF` / `-` |
| 7 OS | `uname -r` 값 / `-` |
| 8 비고 | `-er` 장애 목록(생략 시 현재 경로 `error_list.txt`)의 사유. 목록에 없으면 빈칸 |

각 열의 의미와 판정 규칙은 3.4 를 참고하세요.

### 2.2 엑셀에 붙여넣기

`|` 구분이므로 터미널에서 출력을 드래그해 복사한 뒤 엑셀 셀에 붙여넣고, **데이터 > 텍스트 나누기 > 구분 기호 > 기타 `|`** 로 나누면 8열로 들어갑니다(탭은 복사 때 공백으로 바뀌는 경우가 있어 `|` 를 씁니다).

저장한 파일(`> result.txt`)을 엑셀에서 열 때는 **더블클릭으로 열지 마세요.** 엑셀이 CP949 로 읽어 한글이 깨집니다. 대신 **데이터 > 텍스트/CSV에서 가져오기** 에서 파일을 고르고 **파일 원본: 65001: 유니코드(UTF-8)**, **구분 기호: `|`** 를 선택하세요.

터미널(PuTTY/MobaXterm 등)에서 한글이 깨져 보이면 터미널의 문자 인코딩(Character set)을 **UTF-8** 로 바꾸세요.

### 2.3 종료코드

| 코드 | 의미 |
|---|---|
| 0 | 판정 완료 (장비 상태와는 무관 — `접속불가` 가 있어도 0) |
| 1 | 사용법 오류 (인자 없음·호스트파일 없음 등) |
| 2 | 설정/gossh 실행 오류 (gossh 를 찾지 못함, 설정 파일 오류, 설정 문구에 위험어 포함 등) |

## 3. 옵션 및 상태별 상세 설명

### 3.1 옵션

| 옵션/인자 | 설명 |
|---|---|
| `<호스트파일>` | 필수. gossh `-w` 에 그대로 전달하는 호스트 목록 파일 |
| `-c <설정파일>` | 선택. 설정 파일 경로. 생략하면 `new_setup.conf` 를 **현재 실행 경로 → 실행 파일 폴더 → (`bin/` 안이면) 그 위 폴더(래퍼 옆)** 순서로 찾아 처음 찾은 것을 쓰고(사용한 파일은 stderr 에 표시), 어디에도 없으면 기본값으로 동작. 즉 현재 경로에 `new_setup.conf` 가 있으면 `new_setup.sh <호스트파일>` 은 `new_setup.sh -c ./new_setup.conf <호스트파일>` 과 같음 |
| `-er <장애목록파일>` | 선택. 줄마다 `hostname<탭>장애사유` 형식인 파일. 해당 호스트 줄의 마지막 **비고** 열에 사유를 적음. 생략하면 **현재 실행 경로의 `error_list.txt`** 가 있으면 자동 사용(stderr 에 `장애 목록: error_list.txt` 표시), 없으면 비고는 모두 빈칸. 지정한 파일이 없으면 종료코드 2. **탭으로만 구분**하며(사유에 공백이 들어가도 됨) 첫 탭 앞이 호스트, 뒤 전체가 사유이고 호스트는 대소문자 무시로 정확히 일치해야 함. 같은 호스트가 여러 줄이면 `, ` 로 이어 붙이고, 사유 안의 `|` 는 `/` 로 바꿈. 빈 줄·`#` 주석·탭 없는 줄(공백으로만 구분한 줄)·사유 없는 줄은 무시 |
| `-run` | 선택. 판정 뒤 설정수정·CPU·stress-ng 까지 자동 진행하는 TUI 모드(6장). 상태는 현재 경로의 `./<호스트파일>_run/` |
| `-new` | `-run` 과 함께만. 이전 상태 폴더 `./<호스트파일>_run` 을 `.old-<시각>` 으로 보관(이름 바꿈)하고 처음부터 시작. 워커가 살아 있으면 거절(6.3) |
| `-user <사용자>` | `-run` 전용. config_check `-auto` / stress-test `-user` 에 쓸 사용자(설정 `USER_ROUTE` 가 있으면 메뉴로 고름, 기본 `$USER`) |

그 밖의 옵션은 없습니다.

### 3.2 설정 파일 `new_setup.conf`

`new_setup.conf.sample` 을 복사해 `new_setup.conf` 로 만들어 사용합니다. `#` 로 시작하는 줄과 빈 줄은 무시합니다.

| 키 | 설명 |
|---|---|
| `GOSSH=` | gossh 실행 파일 경로(따옴표·`~/`·폴더만 적어도 됨 — 폴더면 그 안의 `gossh`). **비우면 PATH 에서 찾음**. 경로에 파일이 없으면 그 경로를 알려 주고 종료코드 2. 어디에서도 못 찾으면 stderr 오류 후 종료코드 2 |
| `DISK_PATTERN=` | `디스크미인식` 판정 문구(고정 문자열, 여러 줄 가능). 하나도 없으면 기본 문구 `Disk "" given in clearpart command does not exist` 1개 사용 |
| `ERROR_PATTERN=` | `설치정지` 판정 문구(고정 문자열, 여러 줄 가능). **처음엔 비어 있고** 현장에서 실제 오류 문구를 확인해 채웁니다. 비어 있으면 `설치정지` 는 판정되지 않습니다 |
| `OS_CHECK_SH=` | (v2) 관리서버의 `config_check.sh` 경로. **기본 빈 값 — 비우면 설정체크 열은 `-`**. `-auto-check` 를 지원하는 버전이어야 합니다 |
| `OS_CHECK_EXCLUDE=` | (v2) `\|` 로 이은 제외 문자열(예: `asdf\|qwer`). **기본 빈 값**. 하나라도 hostname 에 포함되면 FAIL 이어도 설정체크 `O` |
| `CPU_INTEL_LOG=` | (v2) Intel CPU 진단 로그 경로. 기본 `/var/cpudiag/result.log`. 파일 이름에 `*`/`?` 를 쓰면 그 폴더에서 이름이 맞는 **가장 최근 파일**을 씁니다(폴더 부분에는 와일드카드·공백 불가) |
| `CPU_INTEL_OK=` | (v2) 위 로그에 있어야 할 성공 문구(고정 문자열, 대소문자 무시). 기본 `Completed successfully. No issues detected.` |
| `CPU_INTEL_LINE=` | (v3) Intel 판정 대상 줄을 고르는 문자열. **기본 빈 값** — 비어 있으면 로그 어디에든 성공 문구가 있으면 `O`. 값이 있으면 그 문자열이 든 줄만 검사해 **모든 줄**에 성공 문구가 있어야 `O` |
| `CPU_AMD_LOG=` | (v3) AMD CPU 진단 로그 경로. 기본 `/var/cpudiag/results.json`. 와일드카드 규칙은 Intel 과 같음 |
| `CPU_AMD_OK=` | (v3) AMD 성공 문구. 기본 `PASS` |
| `CPU_AMD_LINE=` | (v3) AMD 판정 대상 줄 문자열. 기본 `Result` — 현장 수동 확인(`grep Result /var/cpudiag/results.json` 결과가 모두 PASS)과 같은 규칙: `Result` 가 든 줄이 1개 이상이고 모두 `PASS` 를 포함해야 `O` |
| `STRESS_TEST=` 외 | (`-run`) `STRESS_TEST`, `STRESS_TEST_CONF`, `USER_ROUTE`, `CPU_WAIT_START`/`CPU_WAIT_INTERVAL`/`CPU_WAIT_MAX`, `GOSSH_TIMEOUT`/`CHECK_TIMEOUT`/`STRESS_TIMEOUT` — 6.5 의 표 참고. `-run` 이 아니면 쓰이지 않음(`CHECK_TIMEOUT` 은 기본 모드의 설정체크에도 적용) |

- `CPU_*` 값은 앞뒤 따옴표를 붙여도 됩니다(`GOSSH` 처럼 떼어 줍니다). 줄 검색·성공 문구 비교는 대소문자를 구분하지 않습니다.
- 특정 벤더의 CPU 검사를 끄려면 그 벤더의 `LOG` 를 비웁니다(예: `CPU_AMD_LOG=`) — 그 벤더 장비는 `X` 입니다.

그 밖의 값은 앞뒤 공백만 제거합니다.

```ini
GOSSH=
DISK_PATTERN=Disk "" given in clearpart command does not exist
#ERROR_PATTERN=

# v2
OS_CHECK_SH=
OS_CHECK_EXCLUDE=
CPU_INTEL_LOG=/var/cpudiag/result.log
CPU_INTEL_OK=Completed successfully. No issues detected.
#CPU_INTEL_LINE=
CPU_AMD_LOG=/var/cpudiag/results.json
CPU_AMD_OK=PASS
CPU_AMD_LINE=Result
```

- 문구는 정규식이 아닌 **고정 문자열**로 비교합니다.
- 1차(정보수집)·2차 원격 명령은 변수·중괄호·exit 없이 `;` `&&` `||` 만 써서 root 로그인 셸이 csh/tcsh 여도 동작합니다(랩에서 확인).
- gossh 는 명령 문자열에 `reboot`/`poweroff`/`shutdown`/`halt`/`init 0`/`init 6`/`ddc` 가 들어 있으면 실행을 거부하므로, 설정 문구에 이런 위험어가 있으면 시작 시 오류로 알려 줍니다.

### 3.3 상태값과 판정 방법 (2단계 gossh 실행)

```
1차: gossh -w <호스트파일> -pm -script '<정보수집 명령>'
      결과 파일 <호스트파일>_res_off / _res_refsed / _res_cancel  → 접속불가
      결과 파일 <호스트파일>_os_install                           → 설치중 후보 → 2차
      그 밖(접속·명령 실행됨)                                      → 완료 + CPU·stress-ng·H/T·OS 판정
2차: 후보만 임시 목록으로 gossh -w <임시목록> -script '<로그 확인 명령>'
      DISK_PATTERN 일치                                           → 디스크미인식
      ERROR_PATTERN 일치                                          → 설치정지
      둘 다 없음 / 로그 없음 / 2차 접속 실패                       → OS설치중
```

| 상태 | 의미 | 판정 방법 |
|---|---|---|
| `완료` | OS 설치가 끝나고 정상 접속·명령 실행됨 | 1차에서 접속에 성공하고 설치중/접속불가 표시가 없음 |
| `디스크미인식` | 설치 중 디스크를 찾지 못해 멈춤 | 2차에서 `/tmp/anaconda.log` 에 `DISK_PATTERN` 문구가 있음 |
| `설치정지` | 설치 중 오류로 멈춤 | 2차에서 `ERROR_PATTERN` 문구가 있음 |
| `OS설치중` | 설치가 진행 중이거나, 원인 불명 | 설치중 후보인데 위 문구가 없음 / 로그 파일 없음 / 2차 접속 실패 |
| `접속불가` | 접속 자체가 안 됨 | 1차 결과 파일 `_res_off`(타임아웃·DNS 실패·세션 실패) 또는 `_res_refsed`(포트 닫힘), 또는 `_res_cancel` |

- 한 장비에 여러 문구가 함께 있으면 우선순위는 `디스크미인식` > `설치정지` > `OS설치중` 입니다.
- 1차에서 `-pm` 은 `~/.profile` 에 anaconda 가 있으면 "OS 설치중"으로 보고 **명령(정보수집)을 실행하지 않고** 끝납니다. 그래서 설치중 후보만 2차로 다시 접속해 로그를 확인합니다.
- 실제 gossh 는 이전 실행의 결과 파일(`_os_install` 등)을 항상 지우지는 않으므로, 이 도구가 1차 실행 전에 해당 파일 5종을 직접 지웁니다. 지난 실행의 잔여물이 이번 결과에 섞이지 않습니다.
- 1차 실패 호스트는 gossh 가 스스로 한 번 더 재시도하므로 이 도구는 따로 재시도하지 않습니다.
- `-pm` 의 `/user/svrauto` 점검 결과(`_nosvrauto`)는 사용하지 않습니다.

### 3.4 v2 열의 의미와 판정 규칙

상태가 `완료` 인 장비만 아래 5열을 채우고, 그 외 상태는 모두 `-` 입니다.

| 열 | 의미 | 판정 규칙 |
|---|---|---|
| 설정체크 | 관리서버에서 `config_check.sh -auto-check` 로 점검한 결과 | 해당 호스트 결과 줄 중 첫 토큰이 `FAIL` 인 줄이 있으면 `X`, 줄이 있고 FAIL 이 없으면 `O`, 줄이 하나도 없으면 `출력없음`. `OS_CHECK_EXCLUDE` 문자열이 hostname 에 포함되면 위와 무관하게 `O`. `OS_CHECK_SH` 가 비었거나 `-auto-check` 미지원·실행 실패면 `-`. `-run` 에서는 `OS_CHECK_SH` 가 비었을 때의 `-` 만 통과로 보고, 있는데 `-` 이면 실행 실패라 통과가 아님 |
| CPU테스트 | CPU 진단 로그에 성공 문구가 있는지 | `/proc/cpuinfo` 의 `vendor_id` 로 벤더 판별. 그 벤더의 로그 파일이 **없으면 `NOTRUN`**(부하테스트를 아직 안 함). Intel: `CPU_INTEL_LOG` 에 `CPU_INTEL_OK` 가 있으면 `O`(`CPU_INTEL_LINE` 을 지정하면 그 문자열이 든 줄만 검사해 모두 성공 문구 포함). AMD: 기본 규칙은 `CPU_AMD_LOG`(`/var/cpudiag/results.json`)에서 `Result` 가 든 줄이 1개 이상이고 모두 `PASS` 를 포함하면 `O`. 그 외(문구 없음·줄 없음·LOG 비움·벤더 불명)는 `X` |
| stress-ng | 부하 테스트를 돌린 흔적 | `/var/log/messages` 와 로테이션된 `messages-*`(압축 안 된 것)에 `stress-ng` 가 있으면 `O`, 없으면 `NOTRUN` |
| H/T | 하이퍼스레딩 | `lscpu` 의 `Thread(s) per core:` 가 2 이상이면 `ON`, 1 이면 `OFF`, 값을 못 얻으면 `-` |
| OS | 커널 버전 | `uname -r` 값 그대로, 못 얻으면 `-` |

### 3.5 설정체크 실행 방식과 config_check 요구사항

- **관리서버에서** `config_check.sh` 를 실행합니다(대상 장비로 보내지 않음). config_check.sh 가 직접 gossh `-pm` 으로 각 장비에서 `run.sh pd` 를 돌립니다.
- 임시 작업 폴더를 만들어 `targets.txt` 에 **완료 장비만** 쓰고 `bash <OS_CHECK_SH> -auto-check new_setup targets.txt` 를 실행합니다. 결과는 작업 폴더의 `check.res_new_setup` 에서 호스트별로 읽고, 끝나면 임시 폴더를 지웁니다. 화면 출력은 `<호스트파일>_new_setup.log` 에 이어 붙입니다.
- `config_check.sh` 는 **`-auto-check` 를 지원해야 합니다**(환경설정 수정·재점검·완료기록 없이 체크만 수행). 스크립트에 `-auto-check` 문자열이 없으면(구버전) 사용자 선택 메뉴에서 멈추므로 실행하지 않고 설정체크 열을 `-` 로 두고 stderr 에 경고합니다.
- gossh 가 필요합니다(`GOSSH` 폴더가 실행 시 PATH 앞에 추가됨). `OS_CHECK_SH` 와 같은 폴더에 `dhcp.sh` 가 있으면 작업 폴더로 링크합니다.
- 결과 `출력없음` 은 config_check 결과 파일에 그 호스트의 체크 결과 줄(첫 단어가 `OK`/`FAIL`/`INFO` 처럼 영문 대문자)이 하나도 없을 때입니다(접속 실패, 체크 스크립트 경로 미마운트 — 보통 `nosvrauto=` 로 집계됨). 경로가 없어 "bash: …: No such file or directory" 같은 셸 오류 줄만 남은 경우도 `출력없음` 입니다.

### 3.6 로그 파일

gossh 의 stdout·stderr 는 화면에 섞지 않고 `<호스트파일>_new_setup.log` 에 모아 둡니다(실행 시작 시 비우고, 1차·2차 출력을 이어 붙임). 판정이 이상할 때 이 파일에서 원본 gossh 출력을 확인하세요. 2차에서 쓰는 임시 파일 `<호스트파일>_new_setup_retry` 와 그 결과 파일은 실행이 끝나면 삭제됩니다.

## 4. 제약 및 한계

- **로드 에버리지(load average)는 판정에 사용하지 않습니다.** 오직 로그 문구와 접속 결과로만 판정합니다.
- gossh 가 Ctrl+C 로 취소된 호스트(`_res_cancel`)는 `접속불가` 로 출력합니다(가정).
- 2차에서 다시 접속이 실패한 설치중 후보는 1차에서 접속이 됐던 장비이므로 `OS설치중` 으로 남기고 stderr 에 경고를 냅니다(재시도 실패 후보는 `OS설치중` 유지).
- 설치 중 sshd 환경에서 `/tmp/anaconda.log` 를 root 로 읽을 수 있다고 가정합니다. 읽을 수 없으면 `OS설치중` 으로 판정됩니다.
- `설치정지` 는 `ERROR_PATTERN` 을 채워야만 판정됩니다. 문구가 없으면 정지된 장비도 `OS설치중` 으로 보입니다.
- 로그 문구 기반 판정이므로 anaconda 의 출력 문구가 바뀌면 `DISK_PATTERN` 을 갱신해야 합니다.
- `OS_CHECK_EXCLUDE` 에 걸린 호스트는 설정체크 출력이 없어도 `O` 로 봅니다.
- stress-ng 검색은 `/var/log` 아래 이름이 `messages` 로 시작하는 일반 파일만 대상이며 압축(`.gz`) 로테이션분은 보지 않습니다.
- AMD CPU 진단은 현장 수동 확인 방식(`grep Result /var/cpudiag/results.json` 이 모두 PASS)을 기본 규칙으로 구현했습니다. 실제 AMD 장비의 로그 형식과 다르면 `CPU_AMD_*` 로 조정하세요.
- 실제 anaconda 설치 환경, 회사 운영 `run.sh`, AMD CPU 진단 로그는 랩에서 재현할 수 없어 실장비에서 확인하지 못했습니다.
- 범위 밖: 자동 재설치·조치, AWX 연동. (설정수정·부하 테스트 자동 진행은 `-run` 모드만 하며 기본 모드는 여전히 조회만 합니다.)

## 5. 문서별 설명

| 파일 | 설명 |
|---|---|
| `main.go` / `main_test.go` | 판정 로직 / 결과 파일 분류·2차 출력 파싱·문구 이스케이프 단위 테스트 |
| `run.go` `worker.go` `state.go` `input.go` `stress.go` `execx*.go` `daemon_*.go` | `-run` 진입·재접속 / 백그라운드 워커 상태기계 / 상태 파일(state.json 등) / stress-ng 종료 시각 검사 / stress-test 호출·CPU 감시 확인 / 외부 명령 시간 제한(프로세스 그룹 kill) / setsid·lock |
| `tui.go` `render.go` `tty_*.go` | `-run` 화면(raw 모드 TUI, 상태 → 문자열 순수 함수)·키 처리·tty |
| `*_test.go` | 단위 시험(워커 상태기계는 가짜 실행기·가짜 시각으로) |
| `go.mod` | `go 1.20`, 외부 의존성 없음 |
| `new_setup.sh` | bash 래퍼. OS 를 판별해 `bin/` 의 맞는 바이너리를 실행하고 인자를 그대로 전달. 설정 파일이 없고 터미널이면 `setup.sh` 실행을 제안(`NEW_SETUP_NO_PROMPT=1` 로 끔) |
| `setup.sh` | 최초 설정 도우미(대화형, 2.0). `new_setup.conf` 를 질문으로 만들거나 수정 |
| `troubleshoot.sh` | 읽기 전용 문제 진단 래퍼(7장). `new_setup -doctor ...` 실행 |
| `doctor.go` `doctor_host.go` `doctor_test.go` | `-doctor` 진단 본체(환경 점검·`-run` 작업 진단) / 호스트 단건 점검·진입점 / 단위 시험 |
| `test/run_setup_tests.sh` | `setup.sh`·`new_setup.sh` 첫 실행 제안 비대화형 시험 (`bash test/run_setup_tests.sh`) |
| `new_setup.conf.sample` | 설정 파일 예시 |
| `build.sh` | 바이너리 빌드 (`build.sh` = os8. `build.sh os6` 는 옛 방식으로 현재 사용 안 함) |
| `bin/new_setup`, `bin/new_setup_os6` | 빌드 완료 바이너리 (폐쇄망 배포용, 커밋 대상). `new_setup_os6` 는 옛 버전이며 더 이상 재빌드하지 않음 |
| `test/fake_gossh.sh`, `test/fake_config_check.sh`, `test/run_tests.sh` | gossh·config_check.sh 대역과 시나리오 테스트 (`bash test/run_tests.sh`) |
| `test/fake_stress_test.sh`, `test/run_flow_tests.sh` | stress-test 대역과 `-run` 전체 흐름 시험 (`bash test/run_flow_tests.sh`, 종료 시각 입력·인벤토리 사용 중 질문 시나리오 포함, 약 1분 30초) |
| `ARCHITECTURE.md` | 폴더·파일 역할과 판정 흐름 |
| `PR_CHECKLIST.md` | 변경 시 점검 목록 |
| `.github/workflows/ci.yml` | gofmt·vet·test·shellcheck CI |
| `계획서.md` | 설계 결정과 가정·미확인 사항 |

### 5.1 테스트 실행

```bash
go test ./...
bash test/run_tests.sh   # fake gossh 로 모든 상태 시나리오 확인
bash test/run_flow_tests.sh   # -run 전체 흐름(설정수정→CPU/stress-ng→재부팅 감지→재기동→시간초과→중지, 종료 시각 입력 -end, 인벤토리 사용 중 r/x/중지/시간제한 일시정지)
bash test/run_setup_tests.sh  # setup.sh 대화형 설정(답을 파이프로 입력)·new_setup.sh 첫 실행 제안
```

실제 gossh 가 있는 환경(랩)에서 `완료`/`접속불가` 몇 대로 한 번 더 확인하는 것을 권장합니다. 설치중 계열 상태는 대상 장비에 임시로 `~/.profile` 에 `anaconda` 한 줄과 `/tmp/anaconda.log` 를 만들어 재현할 수 있으며(끝나면 반드시 원복), 랩에서 이 방식으로 확인했습니다.

## 6. `-run` — 설정수정·CPU·stress-ng 자동 진행 (TUI)

```bash
bash new_setup.sh -run [-new] [-c <설정파일>] [-er <장애목록파일>] [-user <사용자>] <호스트파일>
```

`-new`: 이전 상태 폴더(`./<호스트파일>_run`)가 있으면 지우지 않고 `<호스트파일>_run.old-<YYYYMMDD-HHMMSS>` 로 이름을 바꿔 보관한 뒤 처음부터 새로 시작합니다(6.3). 옵션은 호스트파일보다 앞에 씁니다.

### 6.1 목적

접속 가능한 서버(상태 `완료`)의 **설정체크 / CPU테스트 / stress-ng** 세 열을 모두 `O` 로 만드는 과정을 한 도구로 진행합니다. 단계가 넘어갈 때마다 결과를 다시 체크(재판정)하고 사용자에게 **계속 / 재시도 / 제외하고 계속 / 중지** 를 묻습니다. 기본 사용(`new_setup <호스트파일>`, 결과표 출력)은 그대로이며 `-run` 은 별도 모드입니다. **stress-test 는 소스를 고치지 않고 `-batch` 로 호출만 하므로 stress-test 는 단독으로도 그대로 쓸 수 있습니다.**

### 6.2 흐름

```
[1 판정]  기존 판정(1차 정보수집·2차 anaconda·설정체크 -auto-check) → 표 갱신 → 질문
[2 설정수정] 설정체크가 O 아닌 완료 호스트: config_check.sh -auto <user> <목록> → 재판정 → 질문
[3 부하]  (CPU X 호스트가 있으면 부하 확인 뒤 재실행 여부 y/n 질문 → 6.2) (stress-ng 대상이 있으면 먼저 종료 시각 5개 값 입력) CPU 대상: stress-test -steps a   /   stress-ng 대상: stress-test -steps b -end y,m,d,h,min
          (CPU 의 등록·launch 가 끝난 뒤 stress-ng 시작, 감시는 동시) → 재판정 → 질문
[4 stress-ng] (종료 시각 입력) 이번 CPU 대상 중 CPU O 가 된 호스트 → stress-test -steps b -end … → 재판정 → 질문
[5 완료]  접속 가능 호스트 3열이 모두 O(또는 제외) → ./<호스트파일>_run/result.txt 저장
```

- 질문 선택지: `c` 계속(다음 단계) / `r` 재시도(같은 단계 다시) / `x` O 아닌 호스트를 제외하고 계속(비고에 `제외`) / `s` 작업 중지. 대상이 없는 단계는 질문 없이 건너뜁니다.
- **CPU `X` 호스트 재실행 질문(부하 단계 시작 때 한 번)**: 이전 CPU 테스트를 강제로 멈추면 `/var/cpudiag` 가 남아 CPU 열이 `NOTRUN` 이 아니라 `X` 가 되어 위 대상 선정에서 영영 빠집니다. 그래서 워커는 부하 단계를 **새로 시작할 때**(재기동 후 감시 이어가기는 제외) 설정체크 통과 + CPU `X` 인 완료 호스트가 있으면 gossh 로 `cat /proc/loadavg` 를 보내 1분 부하를 잽니다. **부하 3 미만**이면 멈춘 흔적으로 보고 후보, **3 이상**(아직 도는 중)이거나 **응답 없음**이면 묻지 않고 건너뜁니다(질문 문구·`worker.log` 에 건너뛴 대수와 이유). 후보가 있으면 호스트별 부하와 함께 한 번 묻습니다: `y` 모두 CPU 재실행에 포함(그 호스트는 `NOTRUN` 호스트와 같은 CPU 묶음으로 launch → 감시 → `O` 가 되면 평소처럼 stress-ng 단계 대상) / `n` 모두 제외(이번엔 `X` 그대로) / `s` 중지. 호스트별 선택은 없습니다. `n` 이고 돌릴 다른 대상도 없으면 부하 단계를 질문 없이 넘깁니다. 부하 단계에 들어올 때마다 한 번만 묻고, 같은 단계에서 `r` 로 다시 해도 다시 묻지 않습니다(고른 호스트는 `r` 때도 계속 대상). 재실행 표시(`state.json` 의 `cpu_retry`)는 CPU 가 `O` 가 되거나 부하 단계가 끝나면 꺼집니다. 부하 기준 3 은 코드 상수(`cpuRetryMaxLoad`)입니다.
- 대상 선정: **CPU** = 설정체크 O 이고 CPU `NOTRUN`(+ 위 질문에서 `y` 를 고른 `X` 호스트). **stress-ng(3단계)** = 설정체크 O, CPU `O`, stress-ng 가 O 아님, **이번 CPU 대상 아님(겹치지 않음)**. **4단계** = 3단계에서 CPU 를 돌려 `O` 가 된 호스트 중 stress-ng 가 O 아님. 접속불가·OS설치중 등 `완료` 가 아닌 호스트는 표에만 보이고 아무 작업도 하지 않습니다.
- **설정체크 `-` 의 의미**: `OS_CHECK_SH` 가 **비어 있을 때만** 통과(O 와 같이 취급)입니다. `OS_CHECK_SH` 가 있는데 `-` 이면 판정 중 config_check 실행이 실패한 것이므로 통과가 아니며, 설정수정·재시도 대상이고 `x`(제외) 목록에 듭니다.
- **CPU 완료 확인·꺼짐 감시**: CPU launch 후 `CPU_WAIT_INTERVAL`(기본 2분)마다 가벼운 확인(부팅 시각 btime + 응답)을 하고, `CPU_WAIT_START`(기본 47분)부터는 CPU 열 값도 확인하다가 모두 `O` 가 되거나 `CPU_WAIT_MAX`(기본 30분)가 지나면 끝냅니다. btime 이 60초 넘게 바뀌면 비고 `실행도중 재부팅됨`, 응답이 없으면 `실행도중 응답없음` 입니다.
- **stress-ng 값**은 `O` / `NOTRUN` 만 있습니다(X 없음). stress-test 감시가 load 기준 이상이면 실행된 것으로 보고, 다음 재체크에서 `/var/log/messages` 에 문자열이 보이면 `O` 가 됩니다.
- **행(hang) 대응**: 모든 외부 명령(gossh·config_check·stress-test)에 시간 제한(`GOSSH_TIMEOUT` / `CHECK_TIMEOUT` / `STRESS_TIMEOUT`)을 둡니다. 넘으면 프로세스 그룹에 SIGTERM, 5초 뒤 SIGKILL 하고 그 단계를 `시간초과` 로 질문(`r`/`s`)합니다.
- user 는 stress-test 와 같은 방식입니다: 설정 `USER_ROUTE` 가 있으면 `info_mn.sh` 메뉴 → `info.sh <번호>` 출력, 없으면 `-user`, 그것도 없으면 `$USER`. 처음 한 번 정해 상태에 저장합니다.
- **stress-ng 종료 시각 입력**: stress-ng 를 돌리기 **전에**(3단계는 CPU 를 띄우기 전, 4단계도 실행 전) 종료 시각 5개 값(`end_year` `end_month` `end_day` `end_hour` `end_min`)을 묻습니다. 3단계에서 stress-ng 대상이 없으면(CPU 만) 묻지 않습니다. 재시도(`r`)로 단계를 다시 하면 다시 묻습니다. 값은 입력한 글자 그대로(`07` 등) `stress-test -steps b -end <연>,<월>,<일>,<시>,<분>` 으로 넘기고(CPU `-steps a` 에는 넘기지 않음) `state.json` 의 `stress_end` 와 화면 머리에 `stress-ng 종료 시각: 2026-11-07 18:00` 으로 보입니다. 검사 규칙은 stress-test `-batch` 와 같습니다: 연 4자리(올해 이상), 월 1-12, 일 1-31(없는 날짜 거절), 시 0-23, 분 0-59, 합친 시각이 지금보다 뒤. 키 이름은 setup_check 의 고정 표시이고, 실제 AWX survey 변수는 stress-test conf 가 정합니다.
- **CPU 가 O 가 돼야 stress-ng**: 부하 단계에서 CPU 는 `설정체크 O + CPU NOTRUN` 호스트(예 svr1·svr3), stress-ng 는 `CPU O + stress-ng O 아님` 호스트(예 svr2)에 함께 돌립니다. CPU 를 돌린 호스트의 stress-ng 는 CPU 가 O 가 된 뒤 stress-ng 단계(4)에서 돌립니다. 부하·stress-ng 단계가 끝났을 때 O 가 아닌 대상이 남아 있으면 질문에 `c`(계속)가 없고 `r` 다시 / `x` 그 호스트 빼고 계속 / `s` 중지만 고를 수 있습니다(NOTRUN 인데 다음 단계·완료로 넘어가지 않게).
- **gossh 전달**: stress-test 를 부를 때 환경변수 `GOSSH` 가 비어 있으면 new_setup 이 쓰는 gossh 경로를 넘깁니다(단독 실행의 `stress_run.sh` 와 같은 효과). stress-test conf 의 `gossh` 키가 있으면 그것이 우선입니다.
- **실행 중 설정 고치기**: 워커는 단계를 시작할 때마다(재시도 `r` 포함) new_setup.conf 를 다시 읽습니다. 예를 들어 `STRESS_TEST_CONF` 가 틀려 부하 단계가 실패하면 설정 파일을 고친 뒤 `r` 만 누르면 됩니다(워커 로그에 `설정 파일 다시 읽음`). `GOSSH` 경로는 워커 시작 때 값을 계속 씁니다. `STRESS_TEST`·`STRESS_TEST_CONF` 상대경로는 명령을 실행한 폴더가 아니라 **new_setup.conf 가 있는 폴더 기준**입니다.
- **인벤토리 사용 중 질문**: stress-test 는 CPU 템플릿을 실행할 때 템플릿의 인벤토리를 사용자 인벤토리로 바꿨다가 launch 직후 `DEFAULT` 로 되돌립니다(자세한 동작은 stress-test README). 템플릿 인벤토리가 이미 `DEFAULT` 가 아니면(다른 사람이 사용 중) stress-test 가 `[인벤토리사용중] <메시지>` 줄을 내고 stdin 을 기다립니다. 워커는 이 줄을 보면 **단계는 그대로 둔 채** 질문을 띄웁니다(메시지 포함, stress-ng 템플릿은 개인 인벤토리를 그대로 써서 이 질문이 없음): `r` 다시 확인(그 인벤토리 사용자가 `DEFAULT` 로 돌려놓은 뒤) / `x` 이 묶음 중단(stress-test 가 그 묶음을 실패 처리, 종료코드 1 → 단계 질문에 안내). 답은 stress-test stdin 으로 `r` / `x` 한 줄로 전달됩니다. 이 질문에 답을 기다리는 동안은 `STRESS_TIMEOUT` 이 **멈추고**, 답하면 처음부터 다시 잽니다. CPU 와 stress-ng 가 동시에 걸리면 질문은 한 번에 하나씩 차례로 뜹니다. 작업 중지(`S` / `stop.req`)는 이 질문 중에도 되며 `x` 로 처리됩니다.

### 6.3 작업 상태 폴더와 재접속

작업은 `setsid` 로 띄운 **백그라운드 워커**가 하고, TUI 는 상태를 보여 주며 질문에 답합니다. **터미널이 끊겨도 작업은 계속**됩니다. 상태는 **현재 실행 경로**의 `./<호스트파일이름>_run/` 에 기록하고 그 밖(`/etc` 등)에는 아무것도 만들지 않습니다.

| 파일 | 내용 |
|---|---|
| `state.json` | 단계·질문·호스트별 열 값·진행 비고·user·stress-ng 종료 시각(`stress_end`)·시각(임시파일 → rename 원자 기록) |
| `answer.json` | TUI 가 쓴 답 `{"id":"<질문 id>","choice":"c"}`(입력 질문이면 `"values":{…}` 추가). 워커가 1초마다 읽고 지움 |
| `stop.req` | 중지 요청. 워커가 보면 실행 중인 외부 명령을 끝내고 중지 |
| `worker.pid` / `lock` | 워커 pid / flock(중복 워커 방지) |
| `worker.log` | 워커 진행 로그·외부 명령 출력 |
| `lists/` | 단계별 대상 호스트 파일(config_check·stress-test·감시 입력) |
| `result.txt` | 최종 결과표(`\|` 8열). `-auto` 작업이면 표 아래에 완료(OOO)·남음·포기 대수와 호스트별 시도 횟수·포기 단계·마지막 오류(`\|` 구분)가 붙음 |

- **다시 붙기**: 같은 경로에서 같은 명령(`new_setup -run <호스트파일>`)을 실행합니다. 워커가 살아 있으면 화면만 붙고(`-user` 무시), 워커가 죽었는데 미완료면 "이어서 진행할까요? (y/N)" 후 워커를 다시 띄웁니다(진행 중이던 외부 명령은 다시 실행하지 않고 재체크부터. CPU launch 가 기록돼 있고 감시가 안 끝났으면 다시 launch 하지 않고 감시를 이어 감). 끝난 작업이면 결과를 보여 줍니다.
- **처음부터 다시(`-new`)**: `new_setup -run -new <호스트파일>`. 상태 폴더가 있으면 **지우지 않고** `<호스트파일>_run.old-<YYYYMMDD-HHMMSS>` 로 이름을 바꿔 보관(같은 이름이 있으면 `-2`, `-3` …)하고 보관 위치를 안내한 뒤 새 작업으로 시작합니다(사용자 선택부터). 워커가 **살아 있으면 거절**(종료코드 2)하므로 먼저 화면에 붙어 `S`/`s` 로 중지하고 워커가 끝난 뒤 다시 실행하세요. 상태 폴더가 없으면 `-new` 는 아무 영향이 없습니다. `-new` 는 `-run` 과 함께만 쓸 수 있습니다. 옛 `*.old-*` 폴더는 필요 없을 때 직접 지우세요(도구는 지우지 않음).
- tty 가 아니면(파이프·리다이렉트·cron 등) TUI 없이 현재 상태를 글로 한 번 출력하고 끝납니다.
- 중지: 질문 중 `s`, 질문이 없을 때 `S`(둘 다 y/N 확인) 또는 `stop.req` 파일 생성. 중지해도 그때까지의 표를 `result.txt` 로 저장합니다.
- 입력 질문의 답 형식(비대화형·시험용): `answer.json` = `{"id":"q5","choice":"c","values":{"end_year":"2026","end_month":"11","end_day":"07","end_hour":"18","end_min":"00"}}`. 워커도 같은 규칙으로 다시 검사해 틀린 값은 무시하고(`worker.log` 에 `무시한 답: 입력값 오류`) 계속 기다립니다. 비 tty 의 글 출력(`new_setup -run`)에는 질문과 `입력 항목:` 만 보이고 답하지는 않습니다.
- stress-test 는 이제 **CPU 용 템플릿 하나**를 쓰고, CPU 템플릿을 실행하는 동안 그 인벤토리를 `DEFAULT` ↔ 사용자 이름(`username`, 이름에 포함돼도 됨) 인벤토리로 바꿨다 되돌립니다(stress-ng 템플릿은 바꾸지 않음). 이 도구는 그 동작을 `-end` 전달과 `[인벤토리사용중]` 줄 응답으로만 연동하며, 자세한 내용(설정 키·되돌리기 실패 처리 등)은 stress-test 의 README 를 보세요.

### 6.4 TUI 키

| 키 | 동작 |
|---|---|
| `c` `r` `x` `s` | 질문에 답(질문이 떠 있을 때. `s` 는 y/N 확인) |
| `y` `n` | CPU `X` 호스트 재실행 질문(부하 단계 시작): 모두 포함 / 모두 제외 (`s` 는 중지) |
| `l` | 화면 맨 아래 **로그 창**(`worker.log` 끝 최대 8줄) 켜기/끄기 — 기본 켜짐 |
| `S` | 질문이 없을 때 작업 중지(y/N 확인) |
| `i` `Enter` | 값 입력 질문(stress-ng 종료 시각)에서 입력 시작 |
| 입력 중: 숫자 / `Backspace` / `Enter` / `Esc` | 숫자 입력(연도 4자리) / 지움 / 다음 항목(틀리면 그 자리에서 오류를 보이고 같은 항목을 다시 묻고, 마지막 뒤 합친 시각이 틀리면 처음부터) / 입력 취소. 끝나면 화면에 `입력됨: …` 이 보이고 `c` 로 그 값을 제출(`i` 다시 입력, `s` 중지) |
| `r` `x` (인벤토리 사용 중 질문) | 다시 확인 / 이 묶음 중단. 이 질문에는 `s` 가 없으므로 작업 전체를 멈추려면 `S` |
| `v` | **결과 판** 켜기/끄기(호스트표 자리에 `\|` 구분 표·대수를 보임, `-auto` 아니어도 됨. 6.7) |
| `f` | O 아닌 호스트만 보기 토글 |
| `↑` `↓` `PgUp` `PgDn` `Home` `End` | 호스트표 스크롤 |
| `q` | 화면만 나가기(작업은 계속) |
| `?` | 도움말 |

**로그 창**: 호스트표·질문 영역 아래(맨 아래)에 `worker.log` 의 최근 줄을 보여 줍니다. 터미널 높이에 맞춰 제목 포함 최대 9줄(로그 8줄)까지 쓰되 표가 최소 3줄은 보이도록 남는 자리만 쓰고, 질문·선택지는 절대 밀어내지 않으며 화면이 너무 작으면 창이 줄거나 빠집니다. 줄은 터미널 폭(한글 2칸)에 맞춰 잘리고 제어 문자는 제거됩니다. 파일은 끝 64KB 만 읽어 로그가 커도 가볍습니다. 비 tty 글 출력에는 로그 창이 없습니다.

화면은 외부 라이브러리 없이 termios raw 모드로 그리며 1초마다 갱신하고 창 크기를 반영합니다. `NO_COLOR` 가 있으면 색을 쓰지 않습니다.

### 6.5 `-run` 설정 키

`new_setup.conf` 에 아래 키를 추가로 씁니다(모두 선택). 시간 값은 Go 형식(`47m`, `30s`, `1h30m`) 또는 숫자(분)이며, 잘못된 값은 경고 후 기본값을 씁니다.

| 키 | 기본 | 설명 |
|---|---|---|
| `STRESS_TEST` | (없음) | stress-test 바이너리 경로. 비면 부하 단계에서 오류 안내 후 질문. 호출은 바이너리 폴더에서 `-user <u> -hosts <목록> -steps a|b -batch [-end <연,월,일,시,분>(b 일 때)] [-conf <파일>]`. stdin 은 인벤토리 사용 중 질문의 답(`r`/`x`) 통로 |\|b -batch [-conf <파일>]` |
| `STRESS_TEST_CONF` | (없음) | stress-test 의 `-conf` 로 넘김. **비우는 것을 권장**: 비면 stress-test 가 자기 폴더의 `conf/<선택한 사용자>_setting.conf` 를 찾음(사용자를 고르면 파일 이름도 따라감). 상대경로는 new_setup.conf 가 있는 폴더 기준 |
| `USER_ROUTE` | (없음) | `info_mn.sh`/`info.sh` 가 있는 폴더(user 선택 메뉴) |
| `CPU_WAIT_START` | `47m` | CPU launch 후 완료 확인(CPU 열 값)을 시작할 때까지 |
| `CPU_WAIT_INTERVAL` | `2m` | 확인(btime·응답)·꺼짐 감시 간격 |
| `CPU_WAIT_MAX` | `30m` | 완료 확인 시작 후 최대 대기 |
| `GOSSH_TIMEOUT` | `10m` | gossh 호출 시간 제한 |
| `CHECK_TIMEOUT` | `40m` | config_check.sh 시간 제한 |
| `STRESS_TIMEOUT` | `60m` | stress-test 시간 제한 |
| `AUTO_LIMIT` | `12h` | (`-auto`) 시작 후 이 시간이 지나면 새 작업을 시작하지 않음 |
| `AUTO_RECHECK` | `10m` | (`-auto`) 접속 안 되는 호스트 재판정 간격 |
| `AUTO_CPU_WAIT` | `50m` | (`-auto`) CPU launch 후 첫 판정까지(45분 + 여유 5분) |
| `AUTO_CPU_INTERVAL` | `5m` | (`-auto`) 첫 판정에서 O 가 아니면 다시 판정하는 간격(launch 실패 뒤 다시 시도까지도) |
| `AUTO_CPU_MAX` | `30m` | (`-auto`) 첫 판정 뒤 더 기다리는 최대 시간(넘어도 O 가 아니면 실패 1회) |

### 6.6 한계와 미확인

- **실제 AWX 템플릿, 실제 CPU 45분 테스트, 실장비 재부팅은 확인하지 못했습니다.** 랩에서는 fake gossh·config_check·stress-test 로 1→5 단계 전체와 재기동·시간초과·중지를, 실제 gossh(.58/.60, .60 은 csh)로 판정·감시 확인만 검증했습니다(`test/run_flow_tests.sh`).
- 설정수정은 `config_check.sh -auto <user>` 가 체크와 수정을 함께 한다는 가정(auto_setup 과 같은 방식)입니다. 수정 결과는 항상 `-auto-check` 재판정으로 확인합니다.
- stress-test `-batch` 의 `[인벤토리사용중] ...` 줄(+ stdin 응답)과 `-end` 인자도 같은 방식의 약속입니다. 실제 AWX 에서의 인벤토리 바꿈·되돌림은 확인하지 못했고 fake stress-test 로만 시험했습니다.
- stress-test `-batch` 가 감시에 들어갈 때 내는 `[감시] ...` 줄을 "등록·launch 끝남" 신호로 씁니다. stress-test 출력 형식이 바뀌면 이 신호가 안 잡힐 수 있습니다(그 경우 CPU 가 launch 되지 않은 것으로 보고 `r`/`s` 질문).
- 장애 목록(`-er`/`error_list.txt`)은 비고 표시용일 뿐 대상 선정에 영향을 주지 않습니다.
- Linux 전용(os8 바이너리). 다른 OS 용 빌드는 개발·단위 시험용이며 중복 워커 방지(lock)·TUI 가 없습니다. os6 바이너리(`bin/new_setup_os6`)에는 `-run` 과 `-doctor` 가 없습니다.

### 6.7 `-run -auto` — 질문 없이 끝까지 (자동 진행)

```bash
bash new_setup.sh -run -auto [-new] [-c <설정파일>] [-er <장애목록파일>] [-user <사용자>] <호스트파일>
# 예) new_setup -run -auto hosts
```

모든 호스트를 **완료 | 설정체크 O | CPU O | stress-ng O** 로 만들 때까지 사람 개입 없이 진행합니다. 워커·TUI·상태 폴더는 `-run` 과 같고, **`-auto` 가 없는 `-run` 의 동작은 바뀌지 않습니다.** 흐름도는 `auto_flow.svg`, 설계는 `계획서_auto.md`.

**동작 요약**

1. **판정**: 시작 때 각 호스트를 완료(접속 가능) / 미완료로 나눕니다. 미완료 호스트는 시작부터 `AUTO_RECHECK`(10분)마다 다시 판정하고, 완료가 되면 대기열 뒤에 붙어 아래를 똑같이 거칩니다.
2. **설정수정**: 완료 호스트의 설정체크가 X 이면 `config_check -auto` 로 수정하고 다시 체크합니다. O 가 될 때까지 호스트당 최대 3회, 넘으면 그 호스트는 **포기**(비고 `포기(설정수정)`)로 표시하고 이후 단계에서 뺍니다. CPU 를 기다리는 동안에도 계속합니다.
3. **CPU**: 설정체크 O 이고 CPU 가 X/NOTRUN 인 호스트가 대기열입니다. 묶음(= 그 시점의 대기열 전체)은 한 번에 하나만 launch 합니다. launch 후 `AUTO_CPU_WAIT`(50분)에 판정하고, O 가 아니면 `AUTO_CPU_INTERVAL`(5분)마다 최대 `AUTO_CPU_MAX`(30분) 더 확인합니다. 그래도 아니면 실패 1회로 세고 대기열 뒤로 보냅니다. 실패 3회면 포기합니다. 기존 "부하 < 3 재실행" 질문에는 자동으로 '다시'를 고릅니다.
4. **stress-ng**: CPU 가 O 이고 stress-ng 가 X/NOTRUN 인 호스트만 대상입니다. 종료 시각은 묻지 않고 **launch 하는 날 +2일 00:00(로컬 시간)** 으로 자동 정합니다. launch 가 정상이면 바로 `O` 로 표시하고, launch 실패는 1회로 세어 최대 3회까지 합니다.
5. **12시간 제한**: 시작 후 `AUTO_LIMIT`(12시간)이 지나면 새 작업을 시작하지 않습니다. 실행 중인 설정수정·stress-ng 와 이미 launch 한 CPU 의 판정(50분 + 30분)은 끝까지 마치고, 결과를 기록한 뒤 자동으로 종료합니다.

**사람이 답하는 질문은 두 가지뿐**입니다(나머지는 자동). 답을 기다리는 동안에도 설정수정·재판정·12시간 타이머는 계속 갑니다.

| 질문 | 키 | 설명 |
|---|---|---|
| **yaml 바뀜** | `y` / `s` | launch 직전에 AWX 인벤토리의 yml·호스트가 우리가 등록한 것과 다르면(다른 사람이 같은 인벤토리를 쓴 경우) 바뀐 yml 이름과 호스트 수를 보이고 묻습니다. `y` = 우리 yaml 로 다시 등록해 덮어쓰고(다른 작업의 내용은 `worker.log` 에 남김) launch 계속, `s` = 작업 중지. 첫 launch 는 묻지 않습니다 |
| **인벤토리 사용 중** | `r` / `x` | 기존 질문(6.2): 다시 확인 / 이 묶음 중단 |

**TUI**: 머리가 `new_setup -run -auto` 로 바뀌고, 단계 줄 대신 `[자동 진행] 남은 시간 … | 완료(OOO) n대 | 남음 n대 | 포기 n대` 줄이 보입니다(자동 진행 중에는 단계가 `판정` 에 머물러 있으므로 단계 줄을 쓰지 않음).

**`v` 결과 판**: `v` 로 호스트표 자리에 결과 판을 켜고, 다시 누르면 호스트표로 돌아갑니다(`f`·스크롤 그대로 적용, `-auto` 가 아니어도 사용). 열은 공백이 아니라 `|` 로 나눕니다(`result.txt` 와 같은 모양, 폭 때문에 H/T·OS 열은 뺌).

```
완료(OOO) 1대|남음 2대|포기 1대
남은 시간 11시간 00분(제한 12시간 00분)|다음 확인 09:07(7분 후)      <- -auto 만
지금 하는 일: 자동 진행 — 남은 시간 11시간 00분, CPU 묶음 1대 다음 판정 09:20   <- -auto 만
hostname|상태|설정체크|CPU테스트|stress-ng|시도(수정/CPU/ng)|비고|마지막오류   <- 시도·마지막오류 열은 -auto 만
ev02|완료|O|NOTRUN|NOTRUN|0/1/0|CPU 실행중|
ev03|완료|X|-|-|3/0/0|제외, 포기(설정수정)|설정체크 X 가/3회 수정 뒤에도 그대로
```

`-auto` 가 아니면 대수 줄과 `hostname|상태|설정체크|CPU테스트|stress-ng|비고` 표만 보입니다.

**result.txt**: 끝나면(또는 중지하면) 기존 8열 `|` 표 아래에 빈 줄, `완료(OOO) n대|남음 n대|포기 n대`, 그리고 `hostname|설정수정시도|CPU시도|stress-ng시도|포기|마지막오류` 표(시도·포기·오류가 있는 호스트만)가 붙습니다. `-auto` 가 아니면 표만 나옵니다.

**다시 실행**: 같은 명령으로 다시 붙으면 **시도 횟수·시작 시각·CPU 묶음 판정 시간표를 이어 갑니다**(12시간은 처음 시작 시각부터 계속 흐름). 뒤에서 돌던 stress-test 는 기다리지 않습니다. **처음부터**(시도 횟수 0, 시작 시각 새로)는 `new_setup -run -new -auto <호스트파일>` 입니다. 이미 `-auto` 없이 시작한 작업에 `-auto` 만 붙이면 무시됩니다(알림이 나옴).

**한계**: 실제 AWX 에서의 yaml 덮어쓰기·45분 CPU 테스트는 실장비로 확인하지 못했고 fake stress-test 로만 시험했습니다(6.6). `AUTO_*` 값은 워커가 시작할 때 읽습니다.

## 7. 문제가 생겼을 때 — `troubleshoot.sh` (읽기 전용 진단)

`new_setup` 결과가 이상하거나 `-run` 이 멈춘 것 같을 때 원인을 찾아 주는 보고서를 만듭니다. `troubleshoot.sh` 는 `new_setup.sh` 와 같은 방식으로 바이너리를 골라 `new_setup -doctor ...` 를 실행하는 래퍼입니다.

```bash
bash troubleshoot.sh [-c 설정파일] [-user 사용자]                 # 환경 점검만
bash troubleshoot.sh [-c 설정파일] <호스트파일>                    # 환경 점검 + 그 경로의 -run 작업 진단
bash troubleshoot.sh [-c 설정파일] -host <호스트> [-check]         # 환경 점검 + 호스트 한 대 점검
```

- **읽기 전용**입니다. 설정 파일·`-run` 상태 폴더·원격 호스트를 바꾸지 않습니다(원격에는 읽기 명령만 보냄). `-check` 도 config_check `-auto-check`(조회)만 돌리며 수정 모드(`-auto`)는 절대 실행하지 않습니다.
- 보고서는 화면에 나오고 `./new_setup_doctor_<시각>.txt` 에도 저장됩니다. 저장본은 **비밀번호가 마스킹**됩니다.
- 줄 머리표: `[O]` 정상, `[!]` 주의, `[X]` 오류, `[i]` 참고. 문제 항목에는 `-> 해결:` 안내가 붙고, 끝의 요약에 모아 보여 줍니다.
- 종료코드: 0 문제 없음, 1 문제(`[!]`·`[X]`) 있음, 2 인자 오류.
- `-user` 는 stress-test conf(`<사용자>_setting.conf`)를 찾을 사용자를 알려 줍니다(없으면 `STRESS_USER` → `$USER`).

### 7.1 세 부분

| 부분 | 언제 | 내용 |
|---|---|---|
| 1. 환경 점검 | 항상 | 바이너리·설정 파일(경고)·`gossh`(경로·실행·필요 옵션)·`OS_CHECK_SH`(`-auto-check`/`-auto` 지원)·`bash`·`STRESS_TEST`·`STRESS_TEST_CONF`·`USER_ROUTE`·현재 경로 쓰기 권한·설정 문구의 gossh 위험어(reboot 등)와 csh 에서 깨지는 `!`·CPU 판정 문구 비어 있음 |
| 2. `-run` 작업 진단 | `<호스트파일>` 을 줬고 `./<호스트파일>_run/` 이 있을 때 | `state.json`·워커 생존·heartbeat·진행 단계·stress-ng 종료 시각·대기 중인 질문(입력·인벤토리 사용 중 포함)과 `answer.json`·`stop.req`·CPU 등록/감시 시간표·호스트별 상태·`worker.log`·stress-test 이력 |
| 3. 호스트 단건 점검 | `-host <호스트>` | 실제 1차·2차 명령과 부팅 시각(btime)·uptime·CPU 로그 `ls` 를 gossh 로 그 한 대에만 보내고, **각 열이 왜 그 값인지** 판정 근거를 설명. `-check` 를 더하면 `-auto-check` 도 실행 |

### 7.2 자주 만나는 상황과 안내

| 보고서 내용 | 뜻과 조치 |
|---|---|
| 워커가 죽어 있습니다(○○ 단계 진행 중이었음) | 단계 도중 워커가 죽은 것(서버 재부팅·kill 등). **같은 명령(`new_setup.sh -run <호스트파일>`)을 다시 실행하면 이어서 진행**(끝난 외부 명령은 다시 하지 않고 재체크부터) |
| 질문 ○○ 에 대한 답을 기다리는 중 | TUI 에서 현재 질문에 답해야 진행됨. 질문 내용·선택지를 보고서에 보여 줌. `answer.json` 이 깨졌거나 질문 id 가 달라 무시되는 경우도 알려 줌 |
| heartbeat 가 오래됨(15초 이상) | 워커가 멈춘(hang) 듯함. 보고서의 pid 를 `kill -9` 한 뒤 남은 `gossh`·`stress-test`·`config_check` 프로세스를 확인하고 다시 실행 |
| 중지 요청(`stop.req`)이 있습니다 | 곧 중지됨. 이어서 하려면 `stop.req` 를 지우고 같은 명령으로 재실행(그대로 두면 재기동 즉시 중지) |
| CPU 마감이 지났는데 감시가 안 끝남 / 다음 확인 시각 지연 | `CPU_WAIT_MAX` 마감·gossh 응답 지연. `worker.log` 의 `CPU 감시 확인 실패` 줄과 `GOSSH_TIMEOUT` 확인 |
| stress-test 출력에 `[감시]` 줄이 없음 | CPU 등록·launch 가 끝나지 않음(AWX 등록·launch 오류). `worker.log` 의 stress-test 출력과 stress-test 이력의 `error=` 확인 |
| CPU 테스트 중 재부팅된 호스트 / 응답 없는 호스트 | 부팅 이유(`last -x`)·전원·네트워크 확인. 응답이 돌아오면 다음 확인 때 자동 해제 |
| 접속불가 호스트(결과 파일에 없음·`_res_off` 등) | `-host` 단건 점검이 `ping`·이름 풀이·22 포트·root 키 인증 확인 순서를 안내 |
| `state.json` 버전·경로 불일치 | 같은 버전 바이너리로, 작업을 시작한 경로에서 재실행 |

주의: `bin/new_setup_os6`(옛 버전)에는 `-run` 도 `-doctor` 도 없으므로 `troubleshoot.sh` 는 RHEL6 에서 쓸 수 없습니다(os8 바이너리 전용).
