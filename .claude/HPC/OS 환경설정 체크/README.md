# OS 점검 및 환경설정 자동화 스크립트 (os_check_final_annotated.sh)

`gossh` 기반으로 OS 배포 후 상태 점검 → 환경설정 적용 → 재점검까지 한 번에 처리하는 자동화 스크립트입니다.

## 개요

- 대상 서버 목록(`${user}.txt`)에 대해 `gossh -pm`으로 접속 가능 여부를 분류하고,
- 접속 가능한 서버에 대해 OS 점검 스크립트(`run.sh`)를 실행한 뒤 LDAP/SPLUNK/LACP 정보를 리포트하고,
- 필요 시 기본/추가 환경설정을 적용한 뒤, 적용 대상만 재점검하여 FAIL 항목을 출력합니다.

## 사전 준비 (필수 수정 항목)

스크립트 상단의 아래 값들을 **실제 환경에 맞게 수정**해야 합니다.

| 항목 | 위치 | 설명 |
|---|---|---|
| `RUN_SH_DIR` | 스크립트 상단 | OS 점검 스크립트(`run.sh`)가 위치한 경로 |
| `SETTING_DIR` | 스크립트 상단 | `setting_insert.sh`, `appl_change.sh`, `setting.sh`가 위치한 경로 |
| `RCLOCAL_SH` | 스크립트 상단 | `rclocal.sh` 경로 |
| `select_user()` | 25번째 줄 근처 | 기존에 쓰시던 user 선택 로직을 그대로 붙여넣으세요. 결과값은 반드시 전역 변수 `user`에 담겨야 합니다 (`main()`의 공백 체크가 이 값을 사용). |
| `get_dhcp_info()` | TODO | DHCP 정보 조회 로직 미구현 |
| `check_ev_extra()` | TODO | `ev` 포함 호스트 추가 점검 로직 미구현 |

`select_user()`를 채우지 않으면 `main()` 실행 시 `user` 값이 비어 있어 바로 종료됩니다.

## 실행 방법

```bash
chmod +x os_check_final_annotated.sh
./os_check_final_annotated.sh
```

실행 흐름:

1. `select_user` 실행 → `user` 값 확보 → `${user}.txt` (대상 목록 파일)을 현재 디렉토리에서 찾음
2. `OS 체크를 진행하시겠습니까? (y/n)` 프롬프트
   - `y` 선택 시: `gossh -pm`으로 분류 → `run.sh` 실행 → LDAP/SPLUNK/LACP 정보 출력
3. `OS 환경설정을 수정하시겠습니까? (y/n/set)` 프롬프트
   - `y`: 기본 환경설정(`setting_insert.sh`, `rclocal.sh`, `appl_change.sh`) 적용
   - `set`: 기본 설정 + 추가 설정(`setting.sh`)까지 적용
   - 적용 후 자동으로 재점검(`run_post_apply_check`) → FAIL 항목만 리포트
4. 마지막에 결과 리포트(케이스별 메시지, DHCP 정보, usb0/BIOS 점검 대상, ev 호스트 등) 출력

## 결과 파일

| 파일명 | 생성 시점 | 내용 |
|---|---|---|
| `check.res_${user}` | 최초 OS 체크 | 전체 점검 결과 원본 |
| `check.res_${user}_postapply` | 설정 적용 후 재점검 | 설정 적용 대상만 재점검한 결과 |
| `ldap_case{N}_${user}` | LDAP 값이 2종류 이상일 때 | 케이스별 값/대상 호스트 |

## 이번 버전에서 변경된 사항

1. `report_ldap_info` : LDAP 값이 전부 동일하면 1줄만 출력, 2종류 이상이면 케이스별 파일로 분리
2. `report_splunk_info` : SPLUNK 값이 전부 동일하면 1줄만, 2종류 이상이면 "값: N대" 형태로 카운트
3. `check_lacp` : `bond_mode=802.3ad`인 호스트만 코멘트 출력 (그 외 모드는 TODO)
4. `run_check_script` : `(target_list, output_file)` 파라미터화 → 최초 점검/재점검에 재사용 가능
5. `run_post_apply_check` (신규) : 설정 적용 후 `SETTING_TARGET_LIST`를 재점검
6. `report_setting_check_fail` (신규) : 재점검 결과 파일에서 `FAIL`만 grep하여 출력

그 외 로직(접두사 분류, 메시지 케이스 6~9, cleanup 등)은 원본 그대로이며 수정하지 않았습니다.

## 수정 시 주의사항 (스크립트 내 주석 규칙)

스크립트 안에 아래 3가지 주석 태그로 수정 가이드가 표시되어 있습니다.

- `[수정필요]` : 실제 환경에 맞게 값을 채우거나 확인이 필요한 부분 (예: `select_user`, 파일명 규칙, FAIL 판정 기준)
- `[수정금지]` : 이번 변경과 무관한 기존 로직이므로 건드리면 안 되는 부분 (예: 접두사 분류, 메시지 케이스 6~9)
- `[연계]` : 다른 함수와 데이터를 주고받는 부분이라 함께 봐야 하는 부분 (예: `run_post_apply_check` ↔ `report_setting_check_fail`)

수정이 필요할 때는 `[수정필요]` 태그가 붙은 부분만 확인하면 됩니다.

## 함수 목록

| 함수 | 역할 |
|---|---|
| `select_user` | user 선택 (사용자 구현 필요) |
| `run_os_check` | `gossh -pm`으로 접속 가능 여부 분류 |
| `parse_pm_result` | 분류 결과를 UP/DOWN/PREFIX 그룹으로 파싱 |
| `run_check_script` | `run.sh` 실행 후 결과 파일 저장 (재사용 가능하게 파라미터화됨) |
| `run_post_apply_check` | 설정 적용 후 재점검 |
| `report_ldap_info` / `report_splunk_info` / `report_lacp_info` | 점검 결과 리포트 |
| `report_setting_check_fail` | 재점검 결과에서 FAIL만 출력 |
| `filter_svrauto_targets` | 설정 적용 대상 필터링 (접속가능 + svrauto 정상) |
| `apply_os_setting` / `apply_extra_setting` | 기본/추가 환경설정 적용 |
| `build_message_case6to9` | 상황별 안내 메시지 생성 |
| `report_dhcp_info` / `check_usb0_required` / `check_pice_bios` / `report_ev_hosts` | 추가 점검/안내 |
| `main` | 전체 흐름 제어 |
