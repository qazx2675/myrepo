# CHANGELOG.md — ip_change

## 2026-09-11

- `-rollback` 서브모드 추가 — 각 대상 노드에서 `<ifcfg>.bak.<STAMP>` 를 되돌립니다
  (`ldap-config-engine -rollback` 과 대칭). `-rollback-to <STAMP>` 로 시점 지정 가능.
  - 새 임베드 스크립트 `internal/render/rollback_body.sh` + `render.RollbackScript`
  - `-rollback` 시 대상 파일은 `hostname` 한 컬럼만 있어도 됩니다 (`target.LoadHosts`)
  - 최신 STAMP 백업을 가진 ifcfg 가 둘 이상이면 실패 → `-rollback-to` 로 지정
  - 통합 스크립트(`Network_Change_Integration_Script`) 의 `change.sh rollback` 이 이걸 호출합니다

## 2026-09-09

- 최초 작성. `hostname 변경될ip` 목록으로 RHEL 대상 노드의 `ifcfg-*` 내 `IPADDR`/`GATEWAY`
  를 gossh 로 일괄 변경하는 Go 엔진 + bash 래퍼.
  - 게이트웨이는 변경 IP의 마지막 옥텟을 `.1` 로 고정 계산 (모든 대상 `/24` 전제)
  - 네트워크 서비스 재시작 없음
  - 원본 ifcfg 는 `.bak.<시각>` 백업 후 `IPADDR`/`GATEWAY` 두 키만 갱신 (통째 덮어쓰기 아님)
  - RHEL 9+ 대비 `conf` 의 `rhel9_path` 항목 (ifcfg 형식 가정, keyfile 형식 미지원)
  - 대상 노드에는 Go 바이너리가 아니라 **bash 스크립트를 base64 로 주입** (`ldap_setting` 과
    동일한 방식) — RHEL6/9 바이너리 호환 이슈 자체가 없음
  - 관리 노드 출력에 ANSI 색상 적용 (성공=녹색, 실패=굵은 빨강, 안내=청록). 대상 노드로
    전송되는 스크립트 출력에는 영향 없음

## 2026-09-09 (수정)

- 현재 서비스 IP 조회(`getent hosts`)가 IPv6 를 IPv4 보다 먼저 반환하는 노드에서
  엉뚱한 IPv6 값을 "현재 서비스 IP" 로 오인해 ifcfg 탐색에 실패하던 문제 수정.
  이제 `getent hosts` 출력 중 IPv4(점 4개짜리) 줄만 골라 씁니다. 실제 랩(192.168.0.58)에서
  `getent hosts $(hostname)` 가 `fe80::...` 를 먼저 찍는 것을 확인해 재현·검증했습니다.

## 2026-09-09 (개선)

- 실행 전 작업 대상 목록(hostname -> 변경 예정 IP)을 화면에 출력하도록 추가.
- 결과 출력 형식을 `hostname 기존IP -> 변경IP (GW 게이트웨이)` 로 변경 (기존: `hostname 변경IP 게이트웨이`).
  - `apply_body.sh` 는 이제 `RESULT|OK|host|기존IP|변경IP|GW` / `RESULT|FAIL|host|사유` 형태의
    기계용 한 줄만 찍고, `cmd/ip-change-engine/main.go` 의 `formatResultLine` 이 화면 표시 형식을 만든다.

## 2026-09-09 (수정 2)

- 현재 서비스 IP 확인 방식을 `getent hosts $(hostname)` 에서 `hostname -I`
  (실패 시 `ip -4 addr show scope global`) 로 교체. 실제 랩에서 작업대상 서버의
  /etc/hosts·DNS 항목에 IPv4 가 아예 없고(또는 IPv6 만 등록되어) getent 로는
  현재 IP 를 알 수 없어 "getent hosts 로 현재 서비스 IP(IPv4)를 확인할 수
  없습니다" FAIL 이 발생하는 것을 확인했다. hostname 해석에 기대지 않고 이
  노드에 실제로 할당된 IPv4 주소를 인터페이스에서 직접 조회하도록 바꿔
  /etc/hosts·DNS 설정 상태와 무관하게 동작하게 했다.
