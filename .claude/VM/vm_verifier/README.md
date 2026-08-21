# VM 배포 정합성 자동 검증 도구 (vm-verifier)

OS 설치가 끝난 VM에 대해 vCenter MAC, DHCP 정적 예약, DNS, Guest OS 상태를 교차 대조해서 DHCP MAC 오기입 등으로 인한 역설치를 잡아내는 CLI 도구다. 자세한 설계 배경은 [PLAN.md](./PLAN.md) 참고.

에이전트리스: 대상 VM에 아무것도 설치하지 않는다. Guest OS 정보(hostname/IP)는 vCenter가 VMware Tools 하트비트로 이미 갖고 있는 값(`guest.hostName`/`guest.net`)을 읽을 뿐, 별도 게스트 로그인은 하지 않는다.

## 1. 빌드

```bash
cd ".claude/VM/vm_verifier"
bash setup.sh
```

## 2. 실행

```bash
VC_USER='administrator@vsphere.local' VC_PASS='...' \
  ./vm-verifier -vcenterList vcenter.txt -f targets.txt
```

- `vcenter.txt`: vCenter 주소를 한 줄에 하나씩. 모든 vCenter를 **병렬로** 접속해 VM 목록을 조회한다.
  ```
  192.168.0.50
  192.168.0.51
  ```
- `targets.txt` (`-f`로 지정, 필수): 검증할 BM 접두어를 한 줄에 하나씩 (`#` 주석 가능).
  ```
  svr01
  svr02
  ```
  각 접두어에 대해 vCenter에 실제로 등록된 `{접두어}ev숫자` 패턴의 VM을 **개수 제한 없이 전부 자동으로 찾아서** 검증한다 (`svr01ev01`만 있으면 1대, `svr01ev01`~`svr01ev05`까지 있으면 5대 전부).
- DHCP 대역 파일은 `-subnet` 옵션 없이 자동으로 찾는다: 각 hostname을 DNS로 조회해 IP를 얻고, 그 앞 **3옥텟**으로 `-dhcp-root`(기본 `/user/caedhcp`) 아래 파일명을 만들어 그 파일 하나만 읽는다. **파일명은 3옥텟까지만** 쓴다 — `10.10.10.15` 대역이면 파일명 `10.10.10` (`10.10.10.0`처럼 4번째 옥텟을 붙이지 않음), `1.1.1.1` 대역이면 `1.1.1`.
- 같은 BM 그룹의 형제끼리 MAC이 뒤바뀐 **교차 설치(역설치)**도 탐지한다 — 예: `svr01ev01`이 자기 DHCP 예약 MAC과는 안 맞는데 `svr01ev02`의 DHCP 예약 MAC과 일치하면, 1단계 Fail 사유에 "교차 설치(역설치) 의심"이 명시된다.
- vCenter 접속, DHCP 조회(DNS 포함), 검증까지 전부 goroutine 기반으로 병렬 처리된다 (worker pool로 동시 실행 수를 CPU 코어 수 기준 최대 16개로 제한. `-race` 디텍터로 데이터 레이스 없음 확인).
- `vcenter.txt`/`targets.txt`는 실제 인프라 정보라 git에 올리지 않는다(.gitignore) — 위 예시를 보고 각자 환경에 맞게 만들 것.

대화형 편의 스크립트:
```bash
bash run.sh
```

불일치가 하나라도 있으면 exit code 1 (자동 조치는 하지 않고 로그만 남김 — PLAN.md 2장).

## 3. 5단계 검증

| 단계 | 내용 |
|---|---|
| 1 | vCenter vNIC MAC ↔ DHCP 파일 MAC |
| 2 | Guest OS hostname ↔ VM 이름 |
| 3 | Guest 실제 IP ↔ DHCP fixed-address / DNS A 레코드 |
| 4 | DNS PTR 역방향 조회 ↔ hostname |
| 5 | VM UUID 이력 대조 (재설치/복제 이력을 Warning으로 별도 표시, Fail과 구분) |

VMware Tools가 아직 안 켜져 있으면 2~4단계는 `INCONCLUSIVE`로 표시된다(Fail 아님) — Tools 기동 전 실행하면 정상적인 현상이다.

## 4. 감사 로그

불일치(FAIL 또는 WARN)가 감지된 hostname만 실행 디렉토리의 `LOG/vm-verifier-YYYYMMDD.log`에 append된다(git 미포함). 별도 중앙 로그 서버는 쓰지 않는다. 정상(PASS/INCONCLUSIVE)만 있으면 로그를 남기지 않는다.

## 5. DNS 서버 종류 확인

```bash
bash check_dns_type.sh
```
`/etc/resolv.conf`의 네임서버들에 SSH 접속 없이 CHAOS 클래스 쿼리(`version.bind`)를 날려 소프트웨어를 추정한다. 서버가 CHAOS 쿼리를 막아뒀으면 응답이 비어있을 수 있다.

검증 로직 자체가 이 결과에 의존하진 않는다 — DHCP 대역 파일 자동 판별과 3/4단계가 전부 DNS 응답에 의존하게 되면서, DNS 조회가 실패했을 때 "DNS 서버 특성 때문인지" "진짜 오설치 때문인지" 구분하는 운영/트러블슈팅 보조 도구다.

## 6. 알려진 제약 / 다음 단계 (PLAN.md 6장·7장 참고)
- Race condition 대응(Tools 기동 직후 재시도) 미구현 — Tools가 완전히 뜬 뒤 실행할 것.
- 이름이 여러 vCenter에 걸쳐 중복되면(동일 VM명이 vCenter A, B에 둘 다 있는 경우) 마지막으로 조회된 값으로 덮어쓰되, 빨간 깜빡임 경고를 콘솔에 출력한다 — 보통 vCenter 간 VM명은 고유하다는 전제라 이 이상의 별도 처리는 하지 않는다.
- UUID 이력은 로컬 JSON 파일(`vm-verifier-uuid-history.json`, git 미포함)에 저장.
- 트리거는 작업자 수동 실행만 지원 (이벤트 구독형 자동 트리거는 범위 밖).
