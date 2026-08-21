# VM 배포 정합성 자동 검증 도구 (vm-verifier) 계획서

원본: `vm_verifier_plan.md` 검토 후 논의를 반영해 구체화함.

## 1. 목적 및 배경
- VM 배포 시 DHCP MAC 수동 기입 오류로 인해 호스트네임에 해당하는 VM이 반대로 설치되는 사고가 발생.
- OS 설치 직후 vCenter API / DHCP 설정 파일 / DNS 레코드 / Guest OS 실제 상태를 상호 대조하여 오설치를 탐지한다.
- 원칙: 사람의 수동 체크는 신뢰하지 않는다. 4가지 정보원을 삼각 대조(Triangulation)해 불일치를 잡아낸다.

## 2. 실행 방식 — 확정
- **트리거: 작업자 수동 실행.** 이벤트 구독/데몬 방식은 v1 범위에서 제외. 작업자가 OS 설치 완료 후 CLI로 직접 실행한다.
  - 예: `vm-verifier check --hostname ev01 --vc 192.168.0.50`
- **불일치 발견 시 조치: 로그/알림만.** VM 강제 종료나 네트워크 격리 같은 자동 조치는 하지 않는다. Pass/Fail 판정과 상세 사유만 출력·기록하고, 이후 조치는 작업자가 수동으로 판단한다.
- **동시성:** 여러 VM(ev01~03)을 한 번에 검증할 경우 goroutine 기반 병렬 처리, worker pool로 동시성 제한 ([[rhel-esxi-troubleshooting]] 기준 — 무제한 goroutine 금지).

## 3. 인증 — 확정
기존 vCenter 접속 도구들과 동일한 환경변수 규약을 따른다 (`vm-param-setting-check` 등에서 이미 사용 중인 패턴).

| 용도 | 환경변수 |
|---|---|
| vCenter 접속 | `VC_USER` / `VC_PASS` (또는 `VCENTER_USER` / `VCENTER_PASS`) |
| Guest OS 접속 (Guest Operations API) | `GUEST_USER` / `GUEST_PASS` |

- Guest Operations API 호출 전 대상 VM에 VMware Tools가 실행 중인지 먼저 확인. Tools 미기동 시 "검증 불가(Inconclusive)"로 별도 처리하고 Fail과 구분한다.
- Guest 자격증명이 VM마다 다를 경우를 대비해, 단일 계정으로 안 될 경우 계정을 VM 그룹(ev01/02/03)별로 오버라이드할 수 있게 열어둔다 (v1은 단일 계정 우선).

## 4. DHCP 설정 파일 조회 — 확정
- 경로: `/user/caedhcp/{/24대역}` (예: `/user/caedhcp/10.10.10.0`)
- 검증 대상 VM의 IP 대역을 먼저 판별해 해당 `/24` 파일을 동적으로 로드.
- 정규식 기반으로 `host {hostname}ev01/02/03` 블록을 추출해 `hardware ethernet(MAC)` / `fixed-address(IP)`를 `map[string]DHCPConfig`로 구조화.
- **IP 비연속 허용:** IP가 연속되지 않아도(`ev01`=.15, `ev02`=.89 등) 문제 없음 — IP 순서가 아니라 호스트 선언 블록 자체를 신뢰 원천으로 삼는다.
- **예외 처리(반려 기준):** 파일 유실/읽기 권한 실패 시 무조건 검증 실패(Block) 처리. 우회해서 통과시키는 로직 금지.

## 5. 5단계 정합성 검증 알고리즘 — 확정
불일치가 하나라도 나오면 즉시 Fail 처리하고 나머지 단계도 계속 실행해 전체 결과를 함께 보고한다 (첫 실패에서 중단하지 않음 — 원인 파악에 유리).

| 단계 | 대상 | 내용 |
|---|---|---|
| 1 | vCenter MAC ↔ DHCP | vNIC MAC과 DHCP 파일의 해당 호스트 블록 MAC 대조 |
| 2 | OS Hostname ↔ VM Name | Guest OS hostname과 vCenter VM 이름/DHCP 호스트 식별자 대조 |
| 3 | 실제 할당 IP ↔ DHCP/DNS | Guest 인터페이스의 실제 IP가 DHCP fixed-address 및 DNS A 레코드와 일치하는지 |
| 4 | DNS 역방향 Lookup | 할당 IP로 PTR 조회 → 반환 hostname이 실제 OS hostname과 일치하는지 |
| 5 | VM UUID(DMI Product UUID) 이력 대조 | vCenter가 보고하는 VM UUID를 기록해두고, 동일 hostname에 대해 이전 검증 시점의 UUID와 다르면 별도 경고(재설치/복제 이력 가능성)로 표시. MAC/hostname/IP/DNS가 모두 일치해도 UUID가 바뀌었으면 Warning으로 별도 보고 (Fail과는 구분) |

## 6. 미확정 / 추가 논의 필요 (제안)
아래는 검토 중 발견한 항목으로, 아직 사용자 결정이 없어 v1 범위에 넣지 않았다. 필요 시 다음 논의에서 확정.

- **UUID 이력 저장소:** 5단계 UUID 대조를 하려면 과거 UUID를 어딘가에 저장해야 함 (로컬 파일? §6의 감사 로그와 같은 저장소?) — 감사 로그 저장소 확정 시 함께 결정.
- **DHCP/DNS 서버 종류:** DHCP는 `/user/caedhcp/` 경로 파일 기반으로 확정. DNS는 어떤 서버(BIND/PowerDNS 등)이고 조회 방식이 zone file 직접 파싱인지 API인지 미정 — PTR 조회는 우선 표준 `net.LookupAddr` 사용 예정이나, 특수 DNS 서버라면 별도 구현 필요.
- **감사 로그 저장소:** "암호화 해시 처리 후 중앙 로그 서버 직송"이 원본 계획에 있으나 로그 서버 종류(Syslog/파일 서버/기타)가 미정. v1은 우선 로컬 파일(JSON) 출력으로 시작하고 추후 확정.
- **Race condition 대응:** Tools 기동 직후 IP/hostname이 아직 안정화되지 않을 수 있어 재시도(retry + backoff) 로직이 필요. v1 구현 시 반영 예정.

## 7. 개발팀 제출 인수 기준 (Checklist) — 원본 유지 + 보완
- [ ] DHCP 파일 로드 실패 시 무조건 Block 처리 (우회 로직 금지)
- [ ] Goroutine + worker pool 기반 병렬 검증 (ev01~03 동시 처리)
- [ ] 에이전트리스: 대상 서버에 별도 데몬 설치 없이 vCenter API(govmomi) + Go 표준 라이브러리만 사용
- [ ] 검증 결과(Pass/Fail) 리포트 출력 — 로그/알림까지만, 자동 조치(강제종료 등) 없음
- [ ] VMware Tools 미기동 상태는 Fail이 아닌 별도 상태(Inconclusive)로 구분
- [ ] Guest 인증 실패와 정합성 불일치(Fail)를 로그에서 구분 가능하게 표기
- [ ] UUID 이력 불일치는 Fail과 분리된 Warning으로 표기

## 8. 다음 단계
1. §6 미확정 항목(UUID 이력 저장소, DNS 서버 종류, 감사 로그 저장소) 확정
2. Go 모듈 스캐폴딩 (`go.mod`, `main.go`) — [[home-test]] 규칙에 따라 실행용 bash 스크립트 + 실질 작업용 Go 스크립트 세트로 구성
3. `/user/caedhcp/` 샘플 파일로 파서 유닛 테스트
