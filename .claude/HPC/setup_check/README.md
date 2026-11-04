# new_setup — 신규 OS 배포 후 설치 상태 판정 도구

신규 OS 배포 후 대상 장비 전체를 `gossh` 로 한 번에 점검해, 장비마다 상태 하나(`완료` / `디스크미인식` / `설치정지` / `OS설치중` / `접속불가`)를 판정해 출력하는 Go 기반 도구입니다. Go 바이너리 + bash 래퍼(`new_setup.sh`) 한 쌍으로 구성되며, 표준 라이브러리만 사용하므로 `vendor/` 가 필요 없습니다.

> 폴더·파일별 역할과 판정 흐름은 [ARCHITECTURE.md](ARCHITECTURE.md), 변경 시 점검 목록은 [PR_CHECKLIST.md](PR_CHECKLIST.md)를 참고하세요.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 이 도구는 설정을 변경하지 않고 상태만 조회하지만, `OS설치중` 등 판정은 로그 문구 일치 여부에 기댄 추정이므로 중요한 장비는 무작위로 몇 대를 골라 직접 접속해 실제 상태를 확인하는 절차를 권장합니다.

## 1. 빌드 및 설치 방법

### 1.1 폐쇄망(회사) 환경 — 빌드 불필요

회사 환경에는 Go 가 없으므로 **빌드 완료된 바이너리를 저장소에 함께 커밋**해 두었습니다. 폴더를 통째로 내려받아 옮기면 바로 실행됩니다.

| 파일 | 대상 서버 |
|---|---|
| `bin/new_setup` | os8_mgmt (linux/amd64) |
| `bin/new_setup_os6` | os6_mgmt (RHEL6, Go 1.20 으로 빌드) |

`new_setup.sh` 가 `/etc/redhat-release` 를 보고 RHEL 6.x 이면 `bin/new_setup_os6`, 그 외에는 `bin/new_setup` 을 자동으로 골라 실행하므로 사용자가 바이너리를 고를 필요는 없습니다.

```bash
# 인터넷이 되는 환경에서 저장소 다운로드 후 압축
tar czf new_setup.tar.gz ".claude/HPC/setup_check"

# 폐쇄망 서버에서
tar xzf new_setup.tar.gz
cd ".claude/HPC/setup_check"
chmod +x new_setup.sh bin/new_setup bin/new_setup_os6   # 실행 권한이 빠졌을 때만
```

필요 조건: 대상 서버에 `gossh` 가 설치되어 있어야 합니다(PATH 에 있거나 `new_setup.conf` 의 `GOSSH=` 로 경로 지정).

### 1.2 소스를 고쳐서 다시 빌드할 때 (Go 가 있는 호스트)

```bash
bash build.sh        # os8 용 → bin/new_setup
bash build.sh os6    # os6 용 → bin/new_setup_os6 (Go 1.20 이 있는 빌드 서버에서)
```

- `go.mod` 는 `go 1.20` 기준입니다(os6 빌드 서버의 Go 1.20 에 맞춤). 그보다 새로운 문법·표준 라이브러리 API 를 쓰지 마세요.
- **소스를 바꿨다면 두 바이너리를 모두 재빌드해 함께 커밋**해야 합니다(회사에 Go 가 없으므로). `.gitignore` 로 `bin/` 을 제외하지 마세요.

### 1.3 전역 명령어로 사용하기 (선택 사항)

`new_setup.sh` 는 자기 위치를 기준으로 `bin/` 과 `new_setup.conf` 를 찾으므로, 복사하지 말고 심볼릭 링크로 연결하는 것을 권장합니다.

```bash
ln -s "$(pwd)/new_setup.sh" /usr/local/bin/new_setup
# 이후 어디서든 new_setup <호스트파일>
```

## 2. 사용 방법

```bash
bash new_setup.sh <호스트파일>               # 화면에 결과 출력
bash new_setup.sh <호스트파일> > result.tsv   # 결과만 파일로 저장
bash new_setup.sh -c /경로/my.conf <호스트파일>   # 설정 파일 직접 지정
```

- `<호스트파일>` 은 gossh `-w` 에 그대로 넘기는 파일입니다(한 줄에 hostname 하나). 형식은 gossh 와 같습니다: 줄 앞뒤 공백 제거, 빈 줄·`#` 시작 줄 무시, 한 줄 안의 쉼표/공백 분리, `node[01-10]` 범위 확장(파일에서는 쉼표 분리가 먼저라 `a[1,3,5-7]` 은 gossh 도 깨지므로 쓰지 마세요), 중복은 먼저 나온 것만 유지.
- 출력은 **입력 파일 순서 그대로**, 헤더 없이 한 줄에 `hostname<TAB>상태` 입니다.
- stdout 에는 이 결과 줄만 나갑니다. 진행 안내·상태별 대수 요약·경고는 모두 stderr 로 나가므로 `> result.tsv` 로 저장해도 결과 파일이 깨끗합니다.
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
node01	완료
node02	OS설치중
node03	디스크미인식
node04	접속불가
node05	설치정지
```

`hostname` 과 상태 사이는 공백이 아니라 **탭 문자 1개**입니다(위 예시는 탭이 포함된 실제 출력). 한 줄을 정확히 2필드로 검증하려면 `awk -F'\t' 'NF!=2' result.tsv` 결과가 비어 있어야 합니다.

### 2.2 엑셀에 붙여넣기

탭 구분이므로 터미널에서 출력을 드래그해 복사한 뒤 엑셀 셀에 붙여넣으면 hostname/상태가 바로 2열로 들어갑니다. 또는 `> result.tsv` 로 저장한 파일을 엑셀에서 열면(구분 기호: 탭) 같은 결과를 얻습니다.

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
| `-c <설정파일>` | 선택. 설정 파일 경로. 생략하면 실행 파일과 같은 폴더의 `new_setup.conf`, 그것도 없으면 기본값으로 동작 |

그 밖의 옵션은 없습니다.

### 3.2 설정 파일 `new_setup.conf`

`new_setup.conf.sample` 을 복사해 `new_setup.conf` 로 만들어 사용합니다. `#` 로 시작하는 줄과 빈 줄은 무시합니다.

| 키 | 설명 |
|---|---|
| `GOSSH=` | gossh 실행 파일 경로. **비우면 PATH 에서 찾음**. 어디에서도 못 찾으면 stderr 오류 후 종료코드 2 |
| `DISK_PATTERN=` | `디스크미인식` 판정 문구(고정 문자열, 여러 줄 가능). 하나도 없으면 기본 문구 `Disk "" given in clearpart command does not exist` 1개 사용 |
| `ERROR_PATTERN=` | `설치정지` 판정 문구(고정 문자열, 여러 줄 가능). **처음엔 비어 있고** 현장에서 실제 오류 문구를 확인해 채웁니다. 비어 있으면 `설치정지` 는 판정되지 않습니다 |

```ini
GOSSH=
DISK_PATTERN=Disk "" given in clearpart command does not exist
#ERROR_PATTERN=
```

- 문구는 정규식이 아닌 **고정 문자열**로 비교합니다.
- 2차 원격 명령은 변수·중괄호·exit 없이 `;` `&&` `||` 만 써서 root 로그인 셸이 csh/tcsh 여도 동작합니다(랩에서 확인).
- gossh 는 명령 문자열에 `reboot`/`poweroff`/`shutdown`/`halt`/`init 0`/`init 6`/`ddc` 가 들어 있으면 실행을 거부하므로, 설정 문구에 이런 위험어가 있으면 시작 시 오류로 알려 줍니다.

### 3.3 상태값과 판정 방법 (2단계 gossh 실행)

```
1차: gossh -w <호스트파일> -pm -script uptime
      결과 파일 <호스트파일>_res_off / _res_refsed / _res_cancel  → 접속불가
      결과 파일 <호스트파일>_os_install                           → 설치중 후보 → 2차
      그 밖(접속·명령 실행됨)                                      → 완료
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
- 1차에서 `-pm` 은 `~/.profile` 에 anaconda 가 있으면 "OS 설치중"으로 보고 **명령(uptime)을 실행하지 않고** 끝납니다. 그래서 설치중 후보만 2차로 다시 접속해 로그를 확인합니다.
- 실제 gossh 는 이전 실행의 결과 파일(`_os_install` 등)을 항상 지우지는 않으므로, 이 도구가 1차 실행 전에 해당 파일 5종을 직접 지웁니다. 지난 실행의 잔여물이 이번 결과에 섞이지 않습니다.
- 1차 실패 호스트는 gossh 가 스스로 한 번 더 재시도하므로 이 도구는 따로 재시도하지 않습니다.
- `-pm` 의 `/user/svrauto` 점검 결과(`_nosvrauto`)는 사용하지 않습니다.

### 3.4 로그 파일

gossh 의 stdout·stderr 는 화면에 섞지 않고 `<호스트파일>_new_setup.log` 에 모아 둡니다(실행 시작 시 비우고, 1차·2차 출력을 이어 붙임). 판정이 이상할 때 이 파일에서 원본 gossh 출력을 확인하세요. 2차에서 쓰는 임시 파일 `<호스트파일>_new_setup_retry` 와 그 결과 파일은 실행이 끝나면 삭제됩니다.

## 4. 제약 및 한계

- **로드 에버리지(load average)는 판정에 사용하지 않습니다.** 오직 로그 문구와 접속 결과로만 판정합니다.
- gossh 가 Ctrl+C 로 취소된 호스트(`_res_cancel`)는 `접속불가` 로 출력합니다(가정).
- 2차에서 다시 접속이 실패한 설치중 후보는 1차에서 접속이 됐던 장비이므로 `OS설치중` 으로 남기고 stderr 에 경고를 냅니다(재시도 실패 후보는 `OS설치중` 유지).
- 설치 중 sshd 환경에서 `/tmp/anaconda.log` 를 root 로 읽을 수 있다고 가정합니다. 읽을 수 없으면 `OS설치중` 으로 판정됩니다.
- `설치정지` 는 `ERROR_PATTERN` 을 채워야만 판정됩니다. 문구가 없으면 정지된 장비도 `OS설치중` 으로 보입니다.
- 로그 문구 기반 판정이므로 anaconda 의 출력 문구가 바뀌면 `DISK_PATTERN` 을 갱신해야 합니다.
- 범위 밖: 자동 재설치·조치, AWX 연동.

## 5. 문서별 설명

| 파일 | 설명 |
|---|---|
| `main.go` / `main_test.go` | 판정 로직 / 결과 파일 분류·2차 출력 파싱·문구 이스케이프 단위 테스트 |
| `go.mod` | `go 1.20`, 외부 의존성 없음 |
| `new_setup.sh` | bash 래퍼. OS 를 판별해 `bin/` 의 맞는 바이너리를 실행하고 인자를 그대로 전달 |
| `new_setup.conf.sample` | 설정 파일 예시 |
| `build.sh` | 바이너리 빌드 (`build.sh` = os8, `build.sh os6` = os6) |
| `bin/new_setup`, `bin/new_setup_os6` | 빌드 완료 바이너리 (폐쇄망 배포용, 커밋 대상) |
| `test/fake_gossh.sh`, `test/run_tests.sh` | gossh 대역과 시나리오 테스트 (`bash test/run_tests.sh`) |
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
