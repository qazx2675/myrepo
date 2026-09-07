# CHANGELOG — ldap_check

## 2026-09-07

### 신규
- 최초 구현. 순수 bash, jq 미사용.
- 3중 교차 검증 도입 — DNS+NTP / LDAP(URI 집합 + binddn + bindpw) / auto.appl storage 가
  모두 같은 인프라를 가리킬 때만 정상으로 판정.
- 사이트 판별 키를 auto.appl storage 로 확정. URI 순서는 사이트끼리 겹칠 수 있어
  (`uri3 = NONE` 일 때 a1·a4 동일) 판별에 쓰지 않고 검증에만 사용.
- OS 분기: RHEL 7 이하 `nslcd` + `ntp`, 8 이상 `sssd` + `chrony`.
  hostname 이 `s4` 로 시작하면 8 이상이어도 `nslcd` + `ntp` 로 검사 (규칙은 conf 로 제어).
- NTP 소스는 `server` 와 `pool` 을 모두 셈.
- 출력을 탭 구분으로 통일해 gossh 결과 집계가 가능하도록 함.
- 종료 코드 정의: 0 전부 OK / 1 설정 불일치 / 2 판별 불가.
- `test_check.sh` 추가 — 장비 없이 10개 시나리오 검증.

### 알려진 제약
- 설정 파일의 내용만 검사합니다. 서비스 기동 상태나 실제 LDAP 바인드 성공 여부는 확인하지 않습니다.
- `bindpw` 는 문자열 비교이며 실제 바인드 테스트가 아닙니다.
