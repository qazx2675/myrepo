# OS 환경설정 체크 스크립트 - 계획서

## 목적

기존 OS 점검/환경설정 스크립트(`os_check_orig.sh`)에 신규 요구사항을 반영하여, OS 배포 후 상태 점검부터 환경설정 적용 및 적용 결과 재검증까지 하나의 스크립트로 처리한다.

## 배경

- 기존 스크립트는 최초 OS 체크(LDAP/SPLUNK/LACP 확인) 이후 환경설정을 적용하는 기능만 있었고, 설정 적용 후 실제로 정상 반영되었는지 재검증하는 절차가 없었다.
- LDAP/SPLUNK 값이 서버마다 다를 경우 결과를 한눈에 파악하기 어려운 문제가 있었다.
- LACP 사용 여부를 실제 로직(`bond_mode=802.3ad`)으로 판단하도록 구체화할 필요가 있었다.

## 변경 범위

| 순번 | 항목 | 변경 내용 |
|---|---|---|
| 1 | `report_ldap_info` | 값이 동일하면 1줄 요약, 다르면 케이스별 파일(`ldap_case{N}_${user}`)로 분리 저장 |
| 2 | `report_splunk_info` | 값이 동일하면 1줄 요약, 다르면 "값: N대" 형태로 카운트 출력 |
| 3 | `check_lacp` | `bond_mode=802.3ad`인 호스트만 코멘트 출력하도록 실사용 로직 반영 |
| 4 | `run_check_script` | `(target_list, output_file)` 파라미터화 → 최초 점검/재점검 공용 함수로 재사용 |
| 5 | `run_post_apply_check` (신규) | 환경설정 적용 후 대상만 골라 재점검 |
| 6 | `report_setting_check_fail` (신규) | 재점검 결과에서 FAIL 항목만 추출하여 리포트 |

그 외(호스트 접두사 분류, 메시지 케이스 6~9, cleanup 등) 기존 로직은 변경하지 않았다.

## 진행 단계

1. **최초 점검 단계**: `gossh -pm`으로 접속 가능/불가 서버 분류 → `run.sh`로 OS 상태 점검 → LDAP/SPLUNK/LACP 리포트
2. **환경설정 적용 단계**: `y`(기본 설정) 또는 `set`(기본+추가 설정) 선택 → `filter_svrauto_targets`로 적용 대상 필터링 → 설정 스크립트 실행
3. **재검증 단계** (신규): 설정 적용 대상만 골라 재점검 → FAIL 항목만 리포트
4. **결과 리포트 단계**: 상황별 안내 메시지, DHCP 정보, usb0/BIOS 점검 대상, ev 호스트 등 최종 요약 출력

## 남은 작업 (TODO)

- `select_user()`: 기존에 사용하던 user 선택 로직 이식 필요
- `get_dhcp_info()`: DHCP 정보 조회 로직 미구현
- `check_ev_extra()`: ev 포함 호스트 추가 점검 로직 미구현
- LDAP 결과 파일명 규칙(`ldap_case{N}_${user}`) 및 저장 위치 확인 필요
- 재점검 결과 파일명 규칙(`check.res_${user}_postapply`) 확인 필요
- FAIL 판정 기준(`grep -i FAIL`)이 실제 운영 기준과 일치하는지 확인 필요

## 버전 이력

| 버전 | 파일명 | 설명 |
|---|---|---|
| v1 | `os_check_orig.sh` | 원본 스크립트 |
| v2~v3 (mock) | `os_check_mock_v2.sh`, `os_check_mock_v3.sh` | gossh mock 환경에서 기능 검증용 |
| 최종 | `os_check_final_annotated.sh` | mock 코드 제거, 실사용 스크립트에 반영할 최종본 (수정 필요/금지/연계 주석 포함) |
