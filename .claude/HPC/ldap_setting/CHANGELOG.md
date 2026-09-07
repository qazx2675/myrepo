# CHANGELOG — ldap_setting

## 2026-09-07

### 신규
- 최초 구현. Go 설정 엔진 + bash 배포 래퍼.
- `ldap_config.conf` 를 평문 `key = value` 형식으로 설계. 대상 노드에 jq 가 없어도 동작.
- 인프라 × 사이트 2차원 스키마. 인프라별로 DNS/NTP/URI/binddn/bindpw 를 따로 정의.
- 사이트별 적용 스크립트 하나를 만들어 gossh 로 주입. RHEL 버전과 s4 판정은 노드 현장에서
  수행하므로 gossh 호출이 노드 수가 아니라 사이트 수만큼으로 끝남.
- 전송은 base64 파이프. gossh 에 파일 전송 기능이 없고, 따옴표·개행으로 명령이 깨지지
  않기 때문. `scp` 미사용.
- 설정 적용을 파일 전체 교체가 아닌 **키 단위 수술적 갱신**으로 구현.
  변경 전 `.bak.<타임스탬프>` 백업.
- 바뀐 파일에 대응하는 서비스만 재시작 (`nslcd` / `sssd` + `sss_cache -E` / `autofs` / `ntpd` / `chronyd`).
- RHEL 7 이하 `nslcd` + `ntp`, 8 이상 `sssd` + `chrony` 분기.
  `s4` 접두사 호스트는 8 이상이어도 `nslcd` + `ntp` 강제 (규칙은 conf 로 제어).
- `-infra` 를 기본값 없는 필수 인자로 지정. 다른 인프라 값을 실수로 적용하는 사고 방지.
- `-root` / `ROOT` 로 fixture 디렉터리에 대고 테스트 가능. 이 모드에서는 서비스를 재시작하지 않음.
- `-dry-run` 은 diff 를 보여주고 파일도 서비스도 건드리지 않음.
- 설정 파일 검증을 시작 시점에 수행 — storage 중복, 존재하지 않는 uri_order 참조, 필수 키 누락은
  한 대도 건드리기 전에 오류로 중단.
- `test_all.sh` 추가 — 장비 없이 14개 시나리오 왕복 검증.

### 구현 중 수정한 버그
- `set_ini_in_section` 이 awk `-v` 로 정규식을 넘겨 `\[` 가 `[` 로 해석되면서 sssd.conf 를
  빈 파일로 만들던 문제. 환경변수 + `ENVIRON[]` 로 전환하고 섹션 판정을 문자열 접두사 비교로 변경.
  추가로 `commit_file` 에 "원본이 비어있지 않은데 결과가 비면 반영하지 않는다" 가드 추가.
- `ROOT` 를 지정한 테스트 모드에서 실제 서비스를 재시작하던 문제.
  ROOT 가 있으면 재시작을 건너뛰도록 수정.
- chrony.conf 의 기존 `pool` 줄을 제거하지 않아 검증이 FAIL 되던 문제.
  `server` 와 함께 `pool` 도 제거하도록 수정.

### 알려진 제약
- `ldap_config.conf` 의 `bindpw` 는 평문입니다. 퍼미션 600 유지 및 커밋 금지.
- 원격 적용 시 bindpw 가 담긴 스크립트가 잠시 `/root/ldap_apply.sh` 로 내려갑니다(실행 후 삭제).
- `resolv.conf` 는 NetworkManager 가 재기동 시 덮어쓸 수 있습니다. NM 관리 제외는 범위 밖입니다.
- `sssd.conf` 에 `[domain/...]` 섹션이 없으면 FAIL 처리하며 파일을 새로 만들지 않습니다.
