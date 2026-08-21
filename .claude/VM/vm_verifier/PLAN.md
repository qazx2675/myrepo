# VM 배포 정합성 자동 검증 도구 (vm-verifier) 계획서

원본: `vm_verifier_plan.md` 검토 후 논의를 반영해 구체화함.

## 1. 목적 및 배경
- VM 배포 시 DHCP MAC 수동 기입 오류로 인해 호스트네임에 해당하는 VM이 반대로 설치되는 사고가 발생.
- OS 설치 직후 vCenter API / DHCP 설정 파일 / DNS 레코드 / Guest OS 실제 상태를 상호 대조하여 오설치를 탐지한다.
- 원칙: 사람의 수동 체크는 신뢰하지 않는다. 4가지 정보원을 삼각 대조(Triangulation)해 불일치를 잡아낸다.

## 2. 실행 방식 — 확정
- **트리거: 작업자 수동 실행.** 이벤트 구독/데몬 방식은 v1 범위에서 제외. 작업자가 OS 설치 완료 후 CLI로 직접 실행한다.
  - 예: `vm-verifier -vcenterList vcenter.txt -f targets.txt`
- **입력은 파일 기반.** `vcenter.txt`(vCenter 주소 목록)와 `-f`로 지정하는 대상 BM 접두어 목록 파일, 두 개로 실행한다. 단일 vCenter/단일 BM만 볼 때도 파일에 한 줄만 적어서 동일한 경로로 실행 — 별도 "단일 모드"는 두지 않는다.
- **불일치 발견 시 조치: 로그/알림만.** VM 강제 종료나 네트워크 격리 같은 자동 조치는 하지 않는다. Pass/Fail 판정과 상세 사유만 출력·기록하고, 이후 조치는 작업자가 수동으로 판단한다.
- **동시성 — 전 구간 병렬:** vCenter 접속/조회, 그룹별 DHCP 레코드 조회(DNS 조회 포함), BM 접두어별 VM 검증까지 전부 goroutine 기반 병렬 처리. worker pool로 동시 실행 수 제한 ([[rhel-esxi-troubleshooting]] 기준 — 무제한 goroutine 금지, 구현은 CPU 코어 수 기준 최대 16개). `-race` 디텍터로 데이터 레이스 없음 확인.
- **VM 수량 자동 파악:** BM 접두어마다 `{prefix}ev\d+` 패턴에 매칭되는 VM을 vCenter에 실제 등록된 만큼 전부 찾는다. 그룹 개수를 미리 지정할 필요 없음.

## 3. 인증 — 확정 (구현 시 단순화됨)
기존 vCenter 접속 도구들과 동일한 환경변수 규약을 따른다 (`vm-param-setting-check` 등에서 이미 사용 중인 패턴).

| 용도 | 환경변수 |
|---|---|
| vCenter 접속 | `VC_USER` / `VC_PASS` (또는 `VCENTER_USER` / `VCENTER_PASS`) |

- **Guest Operations API(별도 게스트 로그인)는 쓰지 않는다.** 대신 vCenter가 VMware Tools 하트비트로 이미 보고받은 `guest.hostName` / `guest.net`(IP) 값을 그대로 읽는다. 게스트 자격증명 관리·계정 오버라이드가 통째로 필요 없어지고, 진짜 에이전트리스가 된다.
- Tools가 실행 중이 아니면(`guest.toolsRunningStatus != guestToolsRunning`) 2~4단계는 "검증 불가(Inconclusive)"로 별도 처리하고 Fail과 구분한다.

## 4. DHCP 설정 파일 조회 — 확정 (DNS 기반 자동 판별로 변경)
- 경로: `/user/caedhcp/{3옥텟}` — 파일명은 IP의 앞 **3옥텟까지만** 쓴다. 4번째 옥텟(`.0` 등)은 붙이지 않는다 (예: `10.10.10.15` 대역 → 파일명 `10.10.10`, `1.1.1.1` 대역 → 파일명 `1.1.1`).
- **대역 파일은 `-subnet` 옵션 없이 자동으로 찾는다.** 검증 대상 hostname을 DNS로 조회(`net.LookupHost`)해서 IP를 얻고, 그 앞 3옥텟으로 파일 경로를 만들어 그 파일 하나만 로드한다 (`dhcp.Resolve`). DNS에 해당 hostname의 A 레코드가 미리 등록돼 있다는 전제(통상 DHCP 예약과 DNS 등록이 같이 이뤄짐).
- 정규식 기반으로 `host {hostname}` 블록을 추출해 `hardware ethernet(MAC)` / `fixed-address(IP)`를 구조화.
- **예외 처리(반려 기준):** DHCP 루트 디렉토리 자체가 없으면 실행 시작 시점에 무조건 검증 실패(Block). hostname 단위로는 DNS 조회 실패/대역 파일 없음/파일 안에 호스트 블록 없음 중 하나라도 해당하면 그 hostname만 1단계 Fail 처리(다른 hostname 검증은 계속 진행). 우회해서 통과시키는 로직 금지.
- **교차 설치(역설치) 스왑 탐지:** 같은 BM 접두어 그룹 안에서, 어떤 hostname의 실제 vCenter MAC이 자기 DHCP 예약 MAC과는 안 맞는데 **형제 hostname의 DHCP 예약 MAC과 일치**하면 "교차 설치 의심"으로 1단계 Fail 사유에 명시한다 (예: ev01이 ev02의 MAC으로, ev02가 ev01의 MAC으로 배포된 경우 — 원본 계획서가 잡으려던 핵심 시나리오).

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

- **vCenter 간 VM명 중복 — 확정:** vCenter들의 host(VM)명은 보통 고유하므로 별도 식별 로직은 추가하지 않는다. 다만 혹시 중복되면 조용히 덮어쓰지 않고 빨간 깜빡임 경고(ANSI `\033[5;31m`)를 콘솔에 출력한다 — 실제로 2개의 vCenter 항목이 같은 VM명을 반환하는 상황으로 재현 테스트 완료.
- **UUID 이력 저장소 — 확정:** 실행 디렉토리의 `vm-verifier-uuid-history.json`(로컬 파일, git 미포함)에 hostname→UUID로 저장. 별도 중앙 저장소 불필요.
- **감사 로그 저장소 — 확정:** 원본 계획의 "암호화 해시 + 중앙 로그 서버 직송" 요구는 폐기. 대신 **불일치(FAIL/WARN)가 감지된 경우에만** 실행 디렉토리의 `LOG/vm-verifier-YYYYMMDD.log`에 append. 별도 로그 서버 불필요. PASS/INCONCLUSIVE만 있는 정상 실행은 로그를 남기지 않는다(노이즈 방지).
- **DNS 서버 종류 확인 방법 — 확정:** `check_dns_type.sh`로 SSH 접속 없이 CHAOS 클래스 쿼리(`dig version.bind/version.server chaos txt`)를 날려 원격에서 소프트웨어를 추정한다. 다만 이 방식은 대상 DNS가 CHAOS 쿼리를 막아두면(예: 퍼블릭 DNS) 응답이 안 올 수 있어, 그 경우엔 담당자 확인이 필요하다. **실제 운영 DNS가 어떤 소프트웨어인지는 아직 확인되지 않음** — PTR/A 레코드 조회 자체는 표준 `net.LookupHost`/`net.LookupAddr`로 구현되어 있어 일반적인 DNS라면 그대로 동작한다.
  - **용도:** 검증 로직 자체가 이 스크립트 결과에 의존하진 않는다(운영/트러블슈팅 보조 도구). §4에서 DHCP 대역 파일 자동 판별과 5단계 3/4단계가 전부 DNS 응답에 의존하게 되면서 중요도가 커졌다 — DNS 조회 실패가 났을 때 "DNS 서버 자체 특성(캐싱/CHAOS 차단 등) 때문인지" "진짜 오설치 때문인지" 구분하는 데 쓴다.
- **Race condition 대응 — 미해결:** Tools 기동 직후 IP/hostname이 아직 안정화되지 않을 수 있음. v1에는 재시도 로직이 없다 — 작업자가 수동 실행이므로 Tools가 완전히 뜬 뒤 실행하는 것으로 우선 대응(운영 절차로 커버), 추후 필요시 `-retry`/backoff 옵션 추가 검토.

## 7. 개발팀 제출 인수 기준 (Checklist) — 구현 완료 반영
- [x] DHCP 파일 로드 실패 시 무조건 Block 처리 (우회 로직 금지)
- [x] 에이전트리스: 대상 서버에 별도 데몬/게스트 로그인 없이 vCenter API(govmomi) + Go 표준 라이브러리만 사용
- [x] 검증 결과(Pass/Fail) 리포트 출력 — 로그/알림까지만, 자동 조치(강제종료 등) 없음
- [x] VMware Tools 미기동 상태는 Fail이 아닌 별도 상태(Inconclusive)로 구분
- [x] UUID 이력 불일치는 Fail과 분리된 Warning으로 표기
- [x] 불일치(FAIL/WARN) 감지 시에만 로컬 `LOG/` 폴더에 감사 로그 기록
- [x] Goroutine + worker pool 기반 병렬 검증 — vCenter 접속/DHCP 조회/5단계 검증 전부 병렬, 동시 실행 수는 CPU 코어 기준 최대 16개로 제한 (`-race` 디텍터로 데이터 레이스 없음 확인)
- [x] BM 접두어당 VM 개수는 옵션이 아니라 vCenter 실제 등록 수만큼 자동 파악
- [x] 같은 그룹 형제끼리 MAC이 뒤바뀐 교차 설치(역설치)를 별도로 탐지해 Fail 사유에 명시 (실제 vCenter VM으로 재현 테스트 완료)
- [ ] Race condition 대응(Tools 기동 직후 재시도/backoff) — 미구현, §6 참고

## 8. 구현 현황
`.claude/VM/vm_verifier/`에 Go 모듈로 구현 완료. 실제 vCenter(192.168.0.50) + `/user/caedhcp/` 샘플 파일로 end-to-end 테스트 완료(정상 케이스 PASS, MAC 오기입 케이스 FAIL 모두 확인). 상세 사용법은 [README.md](./README.md) 참고.

남은 작업은 §6의 "미해결" 항목(Race condition 대응)과 §7 체크리스트의 미구현 항목(goroutine 병렬화)뿐이다.
