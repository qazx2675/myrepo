# VM 배포 정합성 자동 검증 도구 (vm-verifier) 계획서

원본: `vm_verifier_plan.md` 검토 후 논의를 반영해 구체화함. 최초 설계는 vCenter/DHCP/DNS/Guest OS
4개 정보원을 5단계로 교차 대조하는 것이었으나, 실사용 목적이 "MAC 오기입으로 인한 교차 설치(역설치)
탐지"로 좁혀지면서 vCenter MAC ↔ DHCP MAC 대조 하나로 단순화됨 (§5 참고).

## 1. 목적 및 배경
- VM 배포 시 DHCP MAC 수동 기입 오류로 인해 호스트네임에 해당하는 VM이 반대로 설치되는 사고가 발생.
- **VM 생성 직후(파워온/OS 설치 전)** vCenter가 인식한 vNIC MAC과 DHCP 예약 MAC을 대조해서 오설치를 탐지한다.
- 원칙: 사람의 수동 체크는 신뢰하지 않는다.

## 2. 실행 방식 — 확정
- **트리거: 작업자 수동 실행, 시점은 VM 생성 직후(파워온 전).** OS를 설치하기 전에 미리 잡아내야 실수 시 재작업(OS 재설치 등)을 피할 수 있다. 이벤트 구독/데몬 방식은 범위 밖.
  - 예: `vm-verifier -vcenterList vcenter.txt -f targets.txt`
- **입력은 파일 기반.** `vcenter.txt`(vCenter 주소 목록)와 `-f`로 지정하는 대상 BM 접두어 목록 파일, 두 개로 실행한다. 단일 vCenter/단일 BM만 볼 때도 파일에 한 줄만 적어서 동일한 경로로 실행 — 별도 "단일 모드"는 두지 않는다.
- **불일치 발견 시 조치: 로그/알림만.** VM 강제 종료나 네트워크 격리 같은 자동 조치는 하지 않는다. Pass/Fail 판정과 상세 사유만 출력·기록하고, 이후 조치는 작업자가 수동으로 판단한다.
- **출력 모드 — `-failonly` 옵션 확정:**
  - 옵션 없이 실행하면 대상 전체(PASS 포함)를 한 줄씩 출력 — 도입 초기 검증 기간에 전수 확인 용도.
  - `-failonly`를 주면 FAIL만 출력한다. 전체가 PASS면 `검증 완료 — 이상 없음 (VM N대, MAC 주소가 모두 DHCP 등록 정보와 일치)` 요약 한 줄만 출력한다.
  - 감사 로그(`LOG/`)는 옵션과 무관하게 항상 FAIL만 기록 (§6).
- **동시성 — 전 구간 병렬:** vCenter 접속/조회, 그룹별 DHCP 레코드 조회(DNS 조회 포함), BM 접두어별 검증까지 전부 goroutine 기반 병렬 처리. worker pool로 동시 실행 수 제한 ([[rhel-esxi-troubleshooting]] 기준 — 무제한 goroutine 금지, 구현은 CPU 코어 수 기준 최대 16개). `-race` 디텍터로 데이터 레이스 없음 확인.
- **VM 수량 자동 파악:** BM 접두어마다 `{prefix}ev\d+` 패턴에 매칭되는 VM을 vCenter에 실제 등록된 만큼 전부 찾는다. 그룹 개수를 미리 지정할 필요 없음.

## 3. 인증 — 확정
기존 vCenter 접속 도구들과 동일한 환경변수 규약을 따른다 (`vm-param-setting-check` 등에서 이미 사용 중인 패턴).

| 용도 | 환경변수 |
|---|---|
| vCenter 접속 | `VC_USER` / `VC_PASS` (또는 `VCENTER_USER` / `VCENTER_PASS`) |

Guest OS 조회는 아예 하지 않는다 — VM 생성 직후(파워온 전)에 쓰는 도구라 Guest Tools 자체가 켜져 있을 수 없다. 게스트 로그인/자격증명 관리가 통째로 필요 없는, 가장 단순한 형태의 에이전트리스.

## 4. DHCP 설정 파일 조회 — 확정
- 경로: `/user/caedhcp/{3옥텟}` — 파일명은 IP의 앞 **3옥텟까지만** 쓴다. 4번째 옥텟(`.0` 등)은 붙이지 않는다 (예: `10.10.10.15` 대역 → 파일명 `10.10.10`, `1.1.1.1` 대역 → 파일명 `1.1.1`).
- **대역 파일은 `-subnet` 옵션 없이 자동으로 찾는다.** 검증 대상 hostname을 DNS로 조회(`net.LookupHost`)해서 IP를 얻고, 그 앞 3옥텟으로 파일 경로를 만들어 그 파일 하나만 로드한다 (`dhcp.Resolve`). DNS에 해당 hostname의 A 레코드가 미리 등록돼 있다는 전제(통상 DHCP 예약과 DNS 등록이 같이 이뤄지고, OS 설치보다 훨씬 전에 등록되므로 타이밍 문제 없음).
- 정규식 기반으로 `host {hostname}` 블록을 추출해 `hardware ethernet(MAC)` / `fixed-address(IP)`를 구조화.
- **예외 처리(반려 기준):** DHCP 루트 디렉토리 자체가 없으면 실행 시작 시점에 무조건 검증 실패(Block). hostname 단위로는 DNS 조회 실패/대역 파일 없음/파일 안에 호스트 블록 없음 중 하나라도 해당하면 그 hostname만 Fail 처리(다른 hostname 검증은 계속 진행). 우회해서 통과시키는 로직 금지.

## 5. 검증 로직 — 확정 (단일 항목으로 단순화)
vCenter vNIC MAC과 DHCP 예약 MAC, 딱 하나만 대조한다.

| 대상 | 내용 |
|---|---|
| vCenter MAC ↔ DHCP | vNIC MAC과 DHCP 파일의 해당 호스트 블록 MAC 대조. 일치하면 PASS, 아니면 FAIL |

**교차 설치(역설치) 스왑 탐지:** 같은 BM 접두어 그룹 안에서, 어떤 hostname의 실제 vCenter MAC이 자기 DHCP 예약 MAC과는 안 맞는데 **형제 hostname의 DHCP 예약 MAC과 일치**하면 "교차 설치 의심"으로 Fail 사유에 명시한다 (예: ev01이 ev02의 MAC으로, ev02가 ev01의 MAC으로 배포된 경우 — 이 도구가 잡으려는 핵심 시나리오).

### 폐기된 항목 (더 이상 검증하지 않음)
아래는 원본 계획에 있었으나, 목적이 "MAC 스왑 탐지"로 좁혀지면서 제거함. VM 생성 직후(파워온 전)에 도는 도구라 애초에 판정할 정보 자체가 없다.
- OS Hostname ↔ VM Name 대조 (Guest Tools 필요)
- 실제 할당 IP ↔ DHCP/DNS A 레코드 대조 (Guest Tools 필요)
- DNS 역방향(PTR) 조회 대조 (Guest Tools 필요)
- VM UUID 이력 대조(재설치/복제 이력 추적) — 애초에 "OS 재설치 여부"가 아니라 "vCenter VM 객체 자체가 삭제 후 재생성됐는지"만 잡을 수 있는 값이라, 이 도구의 목적(MAC 스왑 탐지)과 무관해서 제거

## 6. 확정된 항목

- **vCenter 간 VM명 중복 — 확정:** vCenter들의 host(VM)명은 보통 고유하므로 별도 식별 로직은 추가하지 않는다. 다만 혹시 중복되면 조용히 덮어쓰지 않고 빨간 깜빡임 경고(ANSI `\033[5;31m`)를 콘솔에 출력한다 — 실제로 2개의 vCenter 항목이 같은 VM명을 반환하는 상황으로 재현 테스트 완료.
- **감사 로그 저장소 — 확정:** 원본 계획의 "암호화 해시 + 중앙 로그 서버 직송" 요구는 폐기. 대신 **FAIL이 감지된 경우에만** 실행 디렉토리의 `LOG/vm-verifier-YYYYMMDD.log`에 append. 별도 로그 서버 불필요. `-failonly` 옵션과 무관하게 항상 이 규칙을 따른다.
- **DNS 서버 종류 확인 — 불필요 판단(확정), 도구 제거:** SSH 없이 CHAOS 쿼리로 DNS 소프트웨어를 추정하는 `check_dns_type.sh`를 만들었으나, 검증 로직이 그 결과에 의존하지 않는 운영 보조 도구였고 실익이 적어 삭제함. PTR/A 레코드 조회는 표준 `net.LookupHost`/`net.LookupAddr`로 구현되어 있어 일반적인 DNS 서버라면 그대로 동작한다.
- **Race condition 대응 — 불필요 판단(확정), 구현 안 함:** DNS 조회(`net.LookupHost`)는 `/etc/resolv.conf`에 등록된 서버에 단순 질의만 하는 상태 없는(stateless) 호출이라 타이밍 문제가 없다. DHCP/DNS 등록 자체가 OS 설치보다 훨씬 전에 이뤄지므로 실제 환경에서 싱크 문제가 없었던 것으로 확인됨.

## 7. 개발팀 제출 인수 기준 (Checklist) — 구현 완료 반영
- [x] DHCP 파일 로드 실패 시 무조건 Block 처리 (우회 로직 금지)
- [x] 에이전트리스: 대상 서버에 별도 데몬/게스트 로그인 없이 vCenter API(govmomi) + Go 표준 라이브러리만 사용
- [x] 검증 결과(Pass/Fail) 리포트 출력 — 로그/알림까지만, 자동 조치(강제종료 등) 없음
- [x] FAIL 감지 시에만 로컬 `LOG/` 폴더에 감사 로그 기록
- [x] Goroutine + worker pool 기반 병렬 검증 — vCenter 접속/DHCP 조회/검증 전부 병렬, 동시 실행 수는 CPU 코어 기준 최대 16개로 제한 (`-race` 디텍터로 데이터 레이스 없음 확인)
- [x] BM 접두어당 VM 개수는 옵션이 아니라 vCenter 실제 등록 수만큼 자동 파악
- [x] 같은 그룹 형제끼리 MAC이 뒤바뀐 교차 설치(역설치)를 별도로 탐지해 Fail 사유에 명시 (실제 vCenter VM으로 재현 테스트 완료)
- [x] `-failonly` 옵션 — 전체 PASS면 요약 한 줄, 아니면 FAIL만 출력 (실제 vCenter VM으로 재현 테스트 완료)
- [x] Race condition 대응 — 검토 결과 불필요로 확정 (§6 참고), 구현 안 함

## 8. 구현 현황
`.claude/VM/vm_verifier/`에 Go 모듈로 구현 완료. 실제 vCenter(192.168.0.50) VM로 end-to-end 테스트 완료 — 정상 케이스 PASS(`-failonly` 요약 한 줄 포함), MAC 오기입/교차 설치(역설치) 케이스 FAIL, vCenter 간 VM명 중복 경고까지 전부 실제 인프라로 재현 테스트 완료. 상세 사용법은 [README.md](./README.md) 참고.

§6·§7의 확정 항목은 모두 반영 완료. v1 범위는 마무리됨.
