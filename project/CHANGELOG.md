# CHANGELOG

## 2026-09-23 — ev02 짝 우선순위, 목록 저장('s')
- 추가: 대상 호스트네임이 `...ev02` 로 끝나면(BM 하나를 `ev01`/`ev02` 가 공유하는
  명명 규칙), 워커 배정 직전 짝(`ev01`)의 `uptime` 1분 부하평균을 확인해 BM
  물리 코어 이상이면 워커 슬롯을 잡지 않고 뒤로 미루고 2분마다 재확인(`internal/target`
  PairHostname/BareMetalName, `internal/vsphere` FindHosts/PhysicalCoreCount/LoadAverage1,
  `cmd/vm-ip-change` waitForPairIdle).
- 추가: 목록 화면에서 `s` 를 누르면 그 시점의 완료/실패/진행중 전체 목록을
  `vm-ip-change-snapshot-<시각>.txt` 로 저장(`internal/tui` saveSnapshot).
- `cmd/fake-vcenter`: VM 이름을 `test-vm0001..` 대신 `host0001ev01`/`host0001ev02..` 로
  변경(짝 명명 규칙과 동일). BM(가짜 ESXi 호스트, `-cores`)과 `-busy-ev01`(짝 우선순위
  확인 시나리오용, 부하 고정) 옵션 추가.
- `cmd/fake-vcenter`: `-pair-mode both/ev02-only/ev01-only` 추가 — list.txt(변경
  대상)에 짝 중 무엇을 넣을지 선택(ev02 짝 우선순위 확인이 list.txt 구성에 따라
  다른 코드 경로를 타므로: 짝을 list.txt 안에서 찾는지, vCenter 인벤토리에서만
  찾는지, 애초에 짝이 없는지). `e2e-test.sh` 가 세 구성을 모두 돌리도록 변경.
- `cmd/fake-vcenter`: `-busy-ev01-for` 추가 — 과점유 시뮬레이션을 그 시간이 지나면
  풀리게 만들어, 밀렸던 `ev02` 가 다음 2분 재확인에서 실제로 우선순위가 다시
  올라가 처리되는 것까지 눈으로 볼 수 있게 함(기존엔 영원히 과점유만 흉내 냈음).
  `-vms 120` 규모의 확장 시나리오로 직접 검증(밀린 대상들이 약 2분 10초 뒤
  일제히 완료로 바뀜). README §5/`사용법.txt` [6]/[6-1] 갱신.

## 2026-09-23 — 시험 환경 (v1.1.0)
- 추가: `cmd/fake-vcenter` + `fake-vcenter.sh` — vcsim 기반 가짜 vCenter. VM 수, 느린 VM,
  실패 VM, 전원꺼짐, 없는 호스트를 옵션으로 만들고 종료 시 VM 별 실제 적용 결과를 검증.
- 추가: `e2e-test.sh` — 무인 끝까지 시험(PASS/FAIL), CI 에서도 실행.
- 추가: CI 워크플로(master `.github/workflows/vm-ip-change.yml`, 브랜치 `.github/workflows/ci.yml`),
  `ARCHITECTURE.md`, `PR_CHECKLIST.md`, 상위 폴더 `사용법.txt`.
- vendor: govmomi `simulator` 패키지 추가(같은 v0.55.1, 새 모듈 없음).

## 2026-09-23 — 진행 화면 페이지 보기, Ctrl+C 3회 종료
- 진행 화면(60초 이후)을 전체 대상 페이지 보기로 변경(↑/↓, ←/→·PgUp/PgDn, Enter).
- 끝난 대상의 경과 시간을 끝난 시점에서 고정.
- 1000대에서도 가볍게: 보이는 페이지만, 줄 단위 덮어쓰기, 바뀐 게 없으면 출력 안 함.
- Ctrl+C 는 5초 안에 3번 눌러야 즉시 종료(되돌리기 없음, 종료코드 130). raw 모드에서 Ctrl+C 가
  안 먹히던 문제 수정. 종료 시 도중에 끊긴 대상과 수동 복구 스크립트 경로 출력.
- 제거: Ctrl+C 1회 롤백 흐름(vsphere.Revert, 취소됨/롤백됨 상태).

## 2026-09-16 — 진행 화면 수정
- raw 모드에서 줄바꿈이 깨져 목록이 가로로 이어지던 문제 수정(`\r\n`).
- 대체 화면 버퍼 사용으로 스크롤백 누적 제거, 갱신 주기 1초.

## 2026-09-15 — 병렬 처리
- 최대 16대 동시 처리, 게스트 명령은 타임아웃 없이 끝까지 대기.
- IP 변경을 연결 확인 → IP 설정 적용 → 연결 재기동 3단계로 분리.
- 진행률 표시와 60초 이후 대화형 목록/상세보기 추가.

## 최초 — vCenter API 기반 VM IP 자동변경 도구
