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

## 3. 인증 — 확정 (구현 시 단순화됨)
기존 vCenter 접속 도구들과 동일한 환경변수 규약을 따른다 (`vm-param-setting-check` 등에서 이미 사용 중인 패턴).

| 용도 | 환경변수 |
|---|---|
| vCenter 접속 | `VC_USER` / `VC_PASS` (또는 `VCENTER_USER` / `VCENTER_PASS`) |

- **Guest Operations API(별도 게스트 로그인)는 쓰지 않는다.** 대신 vCenter가 VMware Tools 하트비트로 이미 보고받은 `guest.hostName` / `guest.net`(IP) 값을 그대로 읽는다. 게스트 자격증명 관리·계정 오버라이드가 통째로 필요 없어지고, 진짜 에이전트리스가 된다.
- Tools가 실행 중이 아니면(`guest.toolsRunningStatus != guestToolsRunning`) 2~4단계는 "검증 불가(Inconclusive)"로 별도 처리하고 Fail과 구분한다.

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

## 6. 확정된 미해결 항목 / 여전히 남은 항목

- **UUID 이력 저장소 — 확정:** 실행 디렉토리의 `vm-verifier-uuid-history.json`(로컬 파일, git 미포함)에 hostname→UUID로 저장. 별도 중앙 저장소 불필요.
- **감사 로그 저장소 — 확정:** 원본 계획의 "암호화 해시 + 중앙 로그 서버 직송" 요구는 폐기. 대신 **불일치(FAIL/WARN)가 감지된 경우에만** 실행 디렉토리의 `LOG/vm-verifier-YYYYMMDD.log`에 append. 별도 로그 서버 불필요. PASS/INCONCLUSIVE만 있는 정상 실행은 로그를 남기지 않는다(노이즈 방지).
- **DNS 서버 종류 확인 방법 — 확정:** `check_dns_type.sh`로 SSH 접속 없이 CHAOS 클래스 쿼리(`dig version.bind/version.server chaos txt`)를 날려 원격에서 소프트웨어를 추정한다. 다만 이 방식은 대상 DNS가 CHAOS 쿼리를 막아두면(예: 퍼블릭 DNS) 응답이 안 올 수 있어, 그 경우엔 담당자 확인이 필요하다. **실제 운영 DNS가 어떤 소프트웨어인지는 아직 확인되지 않음** — PTR/A 레코드 조회 자체는 표준 `net.LookupHost`/`net.LookupAddr`로 구현되어 있어 일반적인 DNS라면 그대로 동작한다.
- **Race condition 대응 — 미해결:** Tools 기동 직후 IP/hostname이 아직 안정화되지 않을 수 있음. v1에는 재시도 로직이 없다 — 작업자가 수동 실행이므로 Tools가 완전히 뜬 뒤 실행하는 것으로 우선 대응(운영 절차로 커버), 추후 필요시 `-retry`/backoff 옵션 추가 검토.

## 7. 개발팀 제출 인수 기준 (Checklist) — 구현 완료 반영
- [x] DHCP 파일 로드 실패 시 무조건 Block 처리 (우회 로직 금지)
- [x] 에이전트리스: 대상 서버에 별도 데몬/게스트 로그인 없이 vCenter API(govmomi) + Go 표준 라이브러리만 사용
- [x] 검증 결과(Pass/Fail) 리포트 출력 — 로그/알림까지만, 자동 조치(강제종료 등) 없음
- [x] VMware Tools 미기동 상태는 Fail이 아닌 별도 상태(Inconclusive)로 구분
- [x] UUID 이력 불일치는 Fail과 분리된 Warning으로 표기
- [x] 불일치(FAIL/WARN) 감지 시에만 로컬 `LOG/` 폴더에 감사 로그 기록
- [ ] Goroutine + worker pool 기반 병렬 검증 (ev01~03 동시 처리) — v1은 순차 처리, 미구현
- [ ] Race condition 대응(재시도/backoff) — 미구현, §6 참고

## 8. 구현 현황
`.claude/VM/vm_verifier/`에 Go 모듈로 구현 완료. 실제 vCenter(192.168.0.50) + `/user/caedhcp/` 샘플 파일로 end-to-end 테스트 완료(정상 케이스 PASS, MAC 오기입 케이스 FAIL 모두 확인). 상세 사용법은 [README.md](./README.md) 참고.

남은 작업은 §6의 "미해결" 항목(Race condition 대응)과 §7 체크리스트의 미구현 항목(goroutine 병렬화)뿐이다.
