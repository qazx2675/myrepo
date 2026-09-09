# CHANGELOG.md — ip_change

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
