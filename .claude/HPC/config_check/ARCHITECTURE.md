# ARCHITECTURE

`config_check.sh` 한 파일로 구성됩니다. 무엇을 바꾸려면 어디를 보면 되는지 정리한 표입니다.

## 구성 요소

| 위치 (스크립트 내) | 역할 | 바꿀 일이 생기면 |
|---|---|---|
| `설정` 블록 | 고정 경로, os6_host / uptime_enable_user / ai_server_* / dhcp_server | 환경이 바뀔 때 여기만 수정 |
| `gsh()` | gossh 실행 래퍼. 실행 구간에서만 INT 무시(gossh의 Ctrl+C 동작 보존) | gossh 호출은 항상 `gsh`로 |
| `VENDOR_AWK` | 호스트 접두사 → 벤더(D/L/J/T/W/X) | 벤더 접두사 추가/변경 |
| `EXPAND_AWK` | gossh 결과 파일의 압축 표기(`host[01-03]`, 다차원 포함)를 풀어 `호스트<TAB>태그`로 출력 | gossh 결과 파일 형식이 바뀔 때 |
| 1. user 선택 | `info_mn.sh` 출력 → 번호 입력 → `info.sh`로 user 확정 | |
| 2. 리스트 확인 | 목록 파싱(공백/줄바꿈, 중복 제거), 30행 단위 열 출력, prefix 집계 | 출력 형식 변경 |
| 3. 작업 진행 | y/n, uptime | |
| `run_check` | gossh `-pm` 1회 실행 → 상태 분류(`.state`) → FAIL/usb0/LDAP 추출 | 체크 결과 형식(OK/FAIL/INFO) 변경 |
| `report_fail` / `report_multi` / `report_status` | FAIL, LDAP·INFO 값 요약(`report_multi`), 상태줄 출력 | 출력 문구·색 변경 |
| `do_check` | run_check + 세 리포트를 묶은 것 (최초 체크/재체크 공용) | |
| 5. 환경설정 | y/n/set → OK 호스트에 `;`로 묶은 스크립트를 gossh 1회로 실행 → 재체크 → 상태 병합 | 설정 스크립트 목록 변경 |
| 6. 마무리 | AI 서버, 벤더/공통 코멘트, DHCP, 기타 상태, usb0, VWP | 코멘트 문구 변경 |

## 임시 파일 (`/tmp/config_check.XXXXXX/`, 종료 시 삭제)

| 파일 | 내용 |
|---|---|
| `hosts` | 정리된 대상 목록 (gossh `-w` 입력) |
| `<이름>.out` / `.err` | gossh stdout(정렬됨) / stderr (`이름` = `first`, `recheck`) |
| `<이름>.set` | gossh 결과 파일을 푼 `호스트<TAB>태그` |
| `<이름>.state` | `호스트<TAB>OK\|OFF\|REF\|NOSV\|INST` (모든 대상) |
| `<이름>.fail` / `.usb0` / `.ldap` | OK 호스트의 FAIL 줄 / usb0 호스트 / LDAP 값 |
| `final.state` | 재체크 결과를 첫 체크 상태 위에 덮은 최종 상태 (y/set일 때만) |
| `V_<벤더>.ok` / `.off` | 벤더별 코멘트용 목록 |

## 데이터 흐름

```
${user}.txt → hosts → gossh -pm (1회) ─┬→ stdout → 정렬 → FAIL / usb0 / LDAP
                                        └→ _res_off·_res_refsed·_os_install·_nosvrauto
                                              → EXPAND_AWK → state (OK/OFF/REF/NOSV/INST)
state ─→ 상태줄, 설정 적용 대상(OK), 벤더 코멘트, DHCP, 기타 상태
```

## 설계 메모

- 상태 분류는 gossh가 만드는 결과 파일 이름(`_res_off`, `_res_refsed`, `_os_install`, `_nosvrauto`, `_res_cancel`)에 의존합니다. gossh 버전을 올릴 때 이 이름이 유지되는지 확인하세요.
- 호스트 처리(파싱·집계·분류)는 모두 awk로 처리해 호스트 수가 늘어도 bash 루프가 없습니다. 5,000대 기준 스크립트 자체 오버헤드는 0.4초 미만입니다(gossh 실행 시간 제외).
