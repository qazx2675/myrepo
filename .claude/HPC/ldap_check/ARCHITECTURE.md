# ARCHITECTURE.md — ldap_check

단일 bash 스크립트입니다. `ldap_check.sh` 를 위에서 아래로 읽으면 전체 흐름이 보입니다.

| 파일 | 역할 |
|---|---|
| `ldap_check.sh` | 검사 본체. 아래 블록 구조 |
| `ldap_config.conf.sample` | 기준값 예시. `../ldap_setting/conf/` 의 것과 같은 내용이어야 함 |
| `test_check.sh` | fixture 기반 회귀 테스트. 장비도 설정 엔진도 불필요 |
| `교육자료.html` | 제작 과정 기록 |

## `ldap_check.sh` 내부 블록

| 블록 | 하는 일 |
|---|---|
| 설정 파일 읽기 | `conf_get` / `conf_list` — sed 로 평문 key=value 파싱. jq 미사용 |
| 노드 실제값 수집 | resolv / chrony·ntp / ldap.conf / auto.appl 에서 awk 로 추출 |
| OS·s4 판정 | RHEL 메이저 + hostname 접두사로 `AUTH_FILE`, `TIME_FILE` 결정 |
| 1차 판별 | DNS+NTP 집합 → `INFRA_DNSNTP` |
| 2차 판별 | URI 집합 + BINDDN + BINDPW → `INFRA_LDAP` |
| 3차 판별 | auto.appl storage → `INFRA_APPL`, `SITE` |
| 교차 검증 | 셋이 모두 같아야 통과. 아니면 화면에는 안 찍고 바로 실패로 종료 |
| 파일별 검사 | 8개 파일을 각각 `report` 로 확인하되, 화면에는 안 찍고 RC 만 조용히 추적 |
| 최종 출력 | 맨 끝에 딱 한 줄만 찍음 — 성공 `INFO<TAB>LDAP<TAB><infra><TAB><site>`,
  실패 `FAIL<TAB>LDAP<TAB>UNDEFINED`(원인은 화면에 안 나오므로, 사람이 원인을
  봐야 하면 이 스크립트를 노드에서 직접 실행해 디버깅) |

## 수정 요청별로 볼 곳

| 요청 | 볼 곳 |
|---|---|
| "검사 항목 추가" | 파일별 검사 블록에 `report` 한 줄 추가 |
| "판별 근거 변경" | 1·2·3차 판별 블록 |
| "출력 형식 변경" | 맨 끝의 `INFO`/`FAIL` 한 줄 출력 블록 (`report()` 자체는 RC 만 추적, 화면에
  안 찍음 — gossh 로 여러 노드 결과를 모을 때 한 줄씩만 나오게 하기 위함,
  `deploy_ldap.sh` 의 `OK_CNT`/`NG_CNT` 집계와 짝) |
| "새 OS 버전 대응" | OS·s4 판정 블록 |

## 반드시 지킬 것

1. **`../ldap_setting/internal/render/apply_body.sh` 와 짝입니다.** 적용 쪽이 쓰는 값을
   바꾸면 이쪽 검사도 같이 고쳐야 합니다. 실제로 적용 쪽이 chrony 의 `pool` 줄을 안 지워서
   검사가 FAIL 을 낸 적이 있습니다 — 이 짝 구조 덕분에 발견한 버그입니다.
2. **집합 비교와 순서 비교를 구분하십시오.** DNS/NTP/URI 판별은 순서 무관 집합 비교이고,
   `ldap.conf` 의 URI 줄만 순서까지 엄격 비교합니다.
3. **사이트 판별에 URI 순서를 쓰지 마십시오.** `uri3 = NONE` 이면 a1 과 a4 의 URI 줄이
   같아집니다. 판별은 auto.appl storage 로만 합니다.
4. **jq 를 도입하지 마십시오.** 대상 노드에 없습니다.
5. **아무것도 쓰지 않는 스크립트입니다.** 파일을 수정하는 코드를 넣지 마십시오.
