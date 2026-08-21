# VM 배포 정합성 자동 검증 도구 (vm-verifier)

OS 설치가 끝난 VM에 대해 vCenter MAC, DHCP 정적 예약, DNS, Guest OS 상태를 교차 대조해서 DHCP MAC 오기입 등으로 인한 역설치를 잡아내는 CLI 도구다. 자세한 설계 배경은 [PLAN.md](./PLAN.md) 참고.

에이전트리스: 대상 VM에 아무것도 설치하지 않는다. Guest OS 정보(hostname/IP)는 vCenter가 VMware Tools 하트비트로 이미 갖고 있는 값(`guest.hostName`/`guest.net`)을 읽을 뿐, 별도 게스트 로그인은 하지 않는다.

## 1. 빌드

```bash
cd ".claude/VM/vm_verifier"
bash setup.sh
```

## 2. 실행

대화형 편의 스크립트:
```bash
bash run.sh
```

직접 실행:
```bash
VC_USER='administrator@vsphere.local' VC_PASS='...' \
  ./vm-verifier -vc 192.168.0.50 -prefix svr01 -subnet 10.10.10.0
```

- `-prefix svr01` → `svr01ev01`, `svr01ev02`, `svr01ev03`을 대조 (그룹은 `-groups`로 조정 가능, 기본 `ev01,ev02,ev03`)
- `-subnet 10.10.10.0` → `/user/caedhcp/10.10.10.0` 파일을 로드
- 불일치가 하나라도 있으면 exit code 1 (자동 조치는 하지 않고 로그만 남김 — PLAN.md 2장)

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

## 6. 알려진 제약 / 다음 단계 (PLAN.md 6장·7장 참고)
- Race condition 대응(Tools 기동 직후 재시도) 미구현 — Tools가 완전히 뜬 뒤 실행할 것.
- 여러 VM 동시 검증 시 goroutine 병렬화 미구현 (v1은 순차 처리).
- UUID 이력은 로컬 JSON 파일(`vm-verifier-uuid-history.json`, git 미포함)에 저장.
- 트리거는 작업자 수동 실행만 지원 (이벤트 구독형 자동 트리거는 범위 밖).
