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

## 2026-09-08

### 신규 — `/wappl` 검사 추가
- `ldap_setting` 쪽에서 `/etc/auto.appl` 에 `/wappl` 줄을 추가로 쓸 수 있게 되어
  (`infra.<이름>.site.<사이트>.wappl_mount` 설정 시) 짝을 맞춤.
- `infra.$INFRA.site.$SITE.wappl_mount` 가 설정된 사이트에서만 `/wappl` 의
  mountpoint 를 검사(선택 항목 — 미설정 사이트는 지금처럼 검사하지 않음).
- `ldap_config.conf.sample` 에 `wappl_mount` 예시 추가(zxcv/a1).

### 검증
- `test_check.sh` 10/10 PASS. 기준 fixture(zxcv/a1)에 `/wappl` 줄을 추가하고
  OK 건수 기대값을 7→8 로 갱신.
