# config_check.sh - OS 체크 + 환경설정 적용 + 재체크

`bash config_check.sh` 한 번으로 user 선택 → 작업 전 리스트 확인 → OS 체크 → (선택) 환경설정 적용 → 재체크 → 벤더 담당자 코멘트 생성까지 처리하는 bash 스크립트입니다. 병렬 실행은 `gossh` v2를 사용합니다.

> ⚠️ **주의사항 (Disclaimer)**
> 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.
> 설정 변경 스크립트(`y` / `set`)를 실행한 뒤에는 **랜덤한 서버 몇 대를 직접 접속해 실제로 변경되었는지 확인**하는 절차가 반드시 필요합니다. 스크립트가 출력하는 재체크 결과만 믿지 마세요.

---

## 1. 빌드 및 설치 방법

빌드는 필요 없습니다. bash 단일 스크립트라서 폴더를 그대로 내려받아 복사하면 폐쇄망에서도 바로 실행됩니다.

**요구사항**

| 항목 | 내용 |
|---|---|
| bash / awk / sed / sort / mktemp | RHEL·Rocky 기본 설치본 (awk는 gawk 4.x에서 검증) |
| `gossh` v2 | `-pm`, `-script`, `-w` 지원, 범위 표기(`host[01-03]`) 결과 파일을 만드는 버전 |
| 고정 경로 | 아래 "설정" 항목의 경로들이 실행 서버에 존재해야 함 |

**설정** — 스크립트 상단(`설정` 블록)의 값을 환경에 맞게 채웁니다.

| 변수 | 설명 |
|---|---|
| `home`, `setting_home`, `user_info_mn`, `user_info_output` | user 선택 스크립트 경로 |
| `appl_setting_file`, `setting_file`, `insert_file` | 환경설정 스크립트 (이미 존재, 실행만 함) |
| `check_script` | 체크 스크립트 (`run.sh`) |
| `vm_inventory` | VM inventory 파일 (실행 서버의 로컬 파일) |
| `os6_host` | 이 서버의 hostname이 이 값이면 `/user/siy/gossh/OS6`를 PATH 앞에 추가 |
| `uptime_enable_user` | uptime 확인 대상 user (`user1\|user2`) |
| `ai_server_list` | AI GPU 서버 hostname (`host1\|host2`, 완전 일치) |
| `ai_server_script` | AI GPU 서버에서 실행할 스크립트 |
| `dhcp_server` | DHCP 정보 조회 서버 (ssh 접속 가능해야 하며 `/root/a.sh`가 있어야 함) |

## 2. 사용 방법

```bash
bash config_check.sh
```

`${user}.txt`(호스트 목록, 공백/줄바꿈 혼용 가능)가 **스크립트를 실행한 디렉토리**에 있어야 합니다.

| 순서 | 화면 | 입력 |
|---|---|---|
| 1 | user 목록 출력 | 번호 입력 |
| 2 | 대상 리스트(30행 단위 열 출력, 공백으로 열 맞춤) + prefix별 수량 + 총 N EA | - |
| 3 | `작업을 진행하시겠습니까? (y/n)` | `y` / `n` (`uptime_enable_user`의 user는 y 후 uptime 출력) |
| 4 | 체크 결과: FAIL만(없으면 `NO FAIL`) → LDAP 요약 → 상태줄 | - |
| 5 | `환경설정을 수정하시겠습니까? (y/n/set)` | `y` / `n` / `set` |
| 6 | (y/set이면 적용 → 재체크) AI 서버 점검, 벤더 코멘트, DHCP, 기타 상태 서버, usb0, VWP | - |

## 3. 옵션별 상세 설명

### 환경설정 수정 (y / n / set)

| 입력 | 동작 |
|---|---|
| `n` | 설정 변경 없이 6단계 결과만 출력 |
| `y` | OK 호스트에 `insert_file; appl_setting_file` 를 gossh **1회**로 실행 후 재체크 |
| `set` | OK 호스트에 `insert_file; appl_setting_file; setting_file` 를 gossh **1회**로 실행 후 재체크 |

- 설정 스크립트의 출력은 `fail|error|fatal|denied|not found`가 들어간 줄만 최대 50줄 보여줍니다.
- 재체크는 OK였던 호스트만 대상으로 합니다. 설정 후 재부팅 등으로 접속이 끊긴 호스트는 최종 상태에서 pingX로 바뀝니다.

### 상태줄

`total=Nea<TAB>OK=Nea<TAB>pingX=Nea<TAB>pingO_sshx=Nea<TAB>nosvrauto=Nea<TAB>os_install=Nea<TAB>INFO=값`

- 항목 사이는 **탭**으로 구분합니다.
- `total`, `OK`를 제외한 항목은 **1 이상일 때만** 표시합니다.
- `total`은 OK + pingX + pingO_sshx + nosvrauto + os_install 과 같으면 초록, 다르면 **빨간색 깜빡임**입니다. 다르면 접속은 됐지만 체크 결과가 한 줄도 없는 호스트가 있다는 뜻이며, 그 호스트는 "기타 상태 서버"의 `no_output`으로 표시됩니다.

| 항목 | 의미 | 색 |
|---|---|---|
| OK | 접속되고 체크 결과가 있는 호스트 | 초록 |
| pingX | gossh 접속 타임아웃 / DNS 실패 (`_res_off`) 및 Ctrl+C로 취소된 호스트 | 빨강 |
| pingO_sshx | 22번 포트 refused 등 (`_res_refsed`) | 노랑 |
| nosvrauto | 접속은 되나 `/user/svrauto` 없음 (`_nosvrauto`) | 노랑 |
| os_install | OS 설치 중 (`~/.profile`에 anaconda) (`_os_install`) | 노랑 |
| INFO | 체크 결과 중 `INFO`와 `KERNEL`이 함께 있는 줄에서 탭 뒤의 문자열 (아래 참고) | 노랑 |

nosvrauto / os_install / pingO_sshx 호스트의 체크 결과는 FAIL·LDAP·usb0 집계에서 제외됩니다(어차피 정상 동작하지 않는 대상).

### LDAP 요약 (상태줄 바로 위)

- 모두 같은 값: `LDAP : infra`
- 2종류 이상: 노란색 `[경고] LDAP infra가 2개 이상입니다` + `LDAP : infra(1980ea) / Undefined configuration(20ea)` + 소수 값의 호스트 목록
- LDAP 줄이 없는 OK 호스트가 있으면 `LDAP 미확인 N대 : 호스트...` (노란색)

체크 스크립트는 정상이면 `hostname: INFO<TAB>ldap<TAB>infra`, 정의되지 않았으면 `hostname: FAIL<TAB>ldap<TAB>Undefined configuration` 형식의 줄을 출력해야 합니다(`ldap` 대소문자 무관). 값 부분이 그대로 요약에 쓰입니다.

### INFO 요약 (상태줄 맨 끝)

체크 결과에서 `INFO`와 `KERNEL`이 함께 들어 있는 줄(`hostname: INFO<TAB>Std 26Year asdf KERNEL RHEL1`)의 **탭 뒤 문자열**을 모아 상태줄 끝에 `INFO=Std 26Year asdf KERNEL RHEL1`로 표시합니다.

- 값이 하나면 그대로 표시합니다.
- 2종류 이상이면 LDAP과 같은 규칙입니다. 상태줄에는 `INFO=값A(6ea) / 값B(2ea)`, 그 위에 노란색 경고와 소수 값의 호스트 목록을 출력합니다.

### 6단계 출력

| 항목 | 조건 | 내용 |
|---|---|---|
| AI GPU 서버 | 대상에 `ai_server_list` 호스트가 있음 | OK 호스트에서 `ai_server_script` 실행 + 노란색 안내 |
| 벤더 코멘트 | 벤더별 조건 | D(`^c ^h ^sh ^s2h ^s3h ^s4h`), S(D 규칙을 제외한 `^s`, L과 같은 코멘트), L(`^p`), J(`^l`), T(`^d`), W(`^m`) |
| 공통 코멘트 | 전체 대상이 모두 OK | `@대 OS 설치 완료하였습니다.` (D 코멘트와 함께 출력) |
| DHCP 정보 | pingX 호스트가 있음 | 접속불가 목록을 DHCP 서버로 보내 `/root/a.sh` 실행 결과를 바로 출력 (로컬/원격에 파일이 남지 않음) |
| 기타 상태 서버 | pingO_sshx / nosvrauto / os_install 서버가 있음 | 상태별 대수와 호스트 |
| usb0 | FAIL + usb0 + interface 줄이 있음 | `하기서버들은 usb0 disabled가 필요합니다.` + hostname |
| VWP | 호스트명에 `ev`가 있고 inventory에 `vwp` 줄이 있음 | 빨간색 깜빡임 경고 + 해당 줄 |

- 벤더 접두사에 해당하지 않는 접속불가 호스트는 `[벤더 미분류 접속불가]`로 따로 표시합니다.
- 코멘트의 "접속완료 서버"는 OK 호스트만입니다.

## 4. 문서별 설명

| 파일 | 설명 |
|---|---|
| `config_check.sh` | 스크립트 본체 |
| `계획서.md` | 요구사항·설계 결정·검증 계획 |
| `ARCHITECTURE.md` | 함수/파일 → 역할 표 |
| `PR_CHECKLIST.md` | 수정 후 확인 체크리스트 |
| `CHANGELOG.md` | 변경 이력 |

## 5. 전역 명령어로 사용하기 (선택)

```bash
sudo cp config_check.sh /usr/local/bin/config_check
```

목록 파일(`${user}.txt`)은 항상 **실행한 디렉토리**를 기준으로 찾으므로, 작업 디렉토리로 이동한 뒤 실행하세요.

## 6. 동작상 알아둘 점

- 체크는 대상 전체에 `gossh -pm -script -w 목록 "bash run.sh pd; :"` **1회**만 실행합니다. `run.sh`는 FAIL이 없으면 `OK`만 출력하고, `pd` 인자를 주면 OK가 아닌 결과값(LDAP, INFO 등)까지 출력하므로 스크립트가 `pd`를 붙여 실행합니다. `; :`는 run.sh가 0이 아닌 값으로 끝나도 gossh가 결과를 stderr로 보내지 않게 하기 위한 것입니다.
- 접속은 됐지만 체크 결과가 한 줄도 없는 호스트는 OK로 세지 않고(`no_output`), 설정 적용 대상에서도 제외됩니다.
- 명령에 `/user/` 경로가 있어 gossh가 동시 접속을 350으로 자동 제한합니다(autofs 보호). 스크립트는 이를 우회하지 않습니다.
- 결과 원본은 실행 디렉토리의 `check.res_${user}`에 남습니다(재체크하면 덮어씀). 그 외 중간 파일은 `/tmp/config_check.XXXXXX`에 만들고 종료 시(Ctrl+C 포함) 삭제합니다.
- gossh 실행 중 Ctrl+C는 gossh 동작을 따릅니다(1회 진행 호스트 표시, 2회 걸린 호스트 취소, 3회 중단). 3회 중단하면 그때까지의 결과로 계속 진행합니다.
- 최종 확인은 수동으로: 설정 적용 후 무작위 서버 몇 대에 접속해 실제 반영 여부를 확인하세요.
