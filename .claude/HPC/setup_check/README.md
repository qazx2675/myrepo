# new_setup — 신규 OS 배포 후 설치 상태 판정 도구

신규 OS 배포 후 대상 장비 전체를 `gossh` 로 한 번에 점검해(설정체크는 관리서버에서 `config_check.sh` 실행), 장비마다 상태 하나(`완료` / `디스크미인식` / `설치정지` / `OS설치중` / `접속불가`)를 판정하고, 완료된 장비는 설정체크·CPU 테스트·stress-ng·H/T·OS 와 비고(장애 목록 사유)까지 8열로 출력하는 Go 기반 도구입니다. Go 바이너리 + bash 래퍼(`new_setup.sh`) 한 쌍으로 구성되며, 표준 라이브러리만 사용하므로 `vendor/` 가 필요 없습니다.

> 폴더·파일별 역할과 판정 흐름은 [ARCHITECTURE.md](ARCHITECTURE.md), 변경 시 점검 목록은 [PR_CHECKLIST.md](PR_CHECKLIST.md)를 참고하세요.

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
| 5 stress-ng | `O` / `X` |
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
| `-er <장애목록파일>` | 선택. 줄마다 `hostname 장애사유` 형식인 파일. 해당 호스트 줄의 마지막 **비고** 열에 사유를 적음. 생략하면 **현재 실행 경로의 `error_list.txt`** 가 있으면 자동 사용(stderr 에 `장애 목록: error_list.txt` 표시), 없으면 비고는 모두 빈칸. 지정한 파일이 없으면 종료코드 2. 첫 공백(스페이스/탭) 앞이 호스트, 뒤 전체가 사유이며 호스트는 대소문자 무시로 정확히 일치해야 함. 같은 호스트가 여러 줄이면 `, ` 로 이어 붙이고, 사유 안의 `|` 는 `/` 로 바꿈. 빈 줄·`#` 주석·사유 없는 줄은 무시 |

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
| 설정체크 | 관리서버에서 `config_check.sh -auto-check` 로 점검한 결과 | 해당 호스트 결과 줄 중 첫 토큰이 `FAIL` 인 줄이 있으면 `X`, 줄이 있고 FAIL 이 없으면 `O`, 줄이 하나도 없으면 `출력없음`. `OS_CHECK_EXCLUDE` 문자열이 hostname 에 포함되면 위와 무관하게 `O`. `OS_CHECK_SH` 가 비었거나 `-auto-check` 미지원 스크립트면 `-` |
| CPU테스트 | CPU 진단 로그에 성공 문구가 있는지 | `/proc/cpuinfo` 의 `vendor_id` 로 벤더 판별. 그 벤더의 로그 파일이 **없으면 `NOTRUN`**(부하테스트를 아직 안 함). Intel: `CPU_INTEL_LOG` 에 `CPU_INTEL_OK` 가 있으면 `O`(`CPU_INTEL_LINE` 을 지정하면 그 문자열이 든 줄만 검사해 모두 성공 문구 포함). AMD: 기본 규칙은 `CPU_AMD_LOG`(`/var/cpudiag/results.json`)에서 `Result` 가 든 줄이 1개 이상이고 모두 `PASS` 를 포함하면 `O`. 그 외(문구 없음·줄 없음·LOG 비움·벤더 불명)는 `X` |
| stress-ng | 부하 테스트를 돌린 흔적 | `/var/log/messages` 와 로테이션된 `messages-*`(압축 안 된 것)에 `stress-ng` 가 있으면 `O`, 없으면 `X` |
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
- 범위 밖: 자동 재설치·조치, AWX 연동.

## 5. 문서별 설명

| 파일 | 설명 |
|---|---|
| `main.go` / `main_test.go` | 판정 로직 / 결과 파일 분류·2차 출력 파싱·문구 이스케이프 단위 테스트 |
| `go.mod` | `go 1.20`, 외부 의존성 없음 |
| `new_setup.sh` | bash 래퍼. OS 를 판별해 `bin/` 의 맞는 바이너리를 실행하고 인자를 그대로 전달 |
| `new_setup.conf.sample` | 설정 파일 예시 |
| `build.sh` | 바이너리 빌드 (`build.sh` = os8. `build.sh os6` 는 옛 방식으로 현재 사용 안 함) |
| `bin/new_setup`, `bin/new_setup_os6` | 빌드 완료 바이너리 (폐쇄망 배포용, 커밋 대상). `new_setup_os6` 는 옛 버전이며 더 이상 재빌드하지 않음 |
| `test/fake_gossh.sh`, `test/fake_config_check.sh`, `test/run_tests.sh` | gossh·config_check.sh 대역과 시나리오 테스트 (`bash test/run_tests.sh`) |
| `ARCHITECTURE.md` | 폴더·파일 역할과 판정 흐름 |
| `PR_CHECKLIST.md` | 변경 시 점검 목록 |
| `.github/workflows/ci.yml` | gofmt·vet·test·shellcheck CI |
| `계획서.md` | 설계 결정과 가정·미확인 사항 |

### 5.1 테스트 실행

```bash
go test ./...
bash test/run_tests.sh   # fake gossh 로 모든 상태 시나리오 확인
```

실제 gossh 가 있는 환경(랩)에서 `완료`/`접속불가` 몇 대로 한 번 더 확인하는 것을 권장합니다. 설치중 계열 상태는 대상 장비에 임시로 `~/.profile` 에 `anaconda` 한 줄과 `/tmp/anaconda.log` 를 만들어 재현할 수 있으며(끝나면 반드시 원복), 랩에서 이 방식으로 확인했습니다.
