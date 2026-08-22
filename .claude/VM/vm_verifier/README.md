# VM 배포 정합성 자동 검증 도구 (vm-verifier)

**VM 생성 직후(OS 설치/파워온 전)**에 vCenter가 인식한 vNIC MAC과 DHCP 정적 예약 MAC을 대조해서, DHCP MAC 오기입 등으로 인한 **교차 설치(역설치)**를 탐지하는 CLI 도구다. 자세한 설계 배경은 [PLAN.md](./PLAN.md) 참고.

파워온 전에 잡아내는 게 핵심이다 — OS 설치가 끝난 뒤에 발견하면 재설치가 필요하지만, 파워온 전에 잡으면 DHCP/네트워크 설정만 바로잡고 넘어갈 수 있다.

에이전트리스: 대상 VM에 아무것도 설치하지 않는다. 이 시점엔 Guest OS 자체가 아직 없어서 Guest Tools 조회 같은 것도 하지 않는다 — vCenter API로 vNIC MAC만 본다.

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
- 같은 BM 그룹의 형제끼리 MAC이 뒤바뀐 **교차 설치(역설치)**도 탐지한다 — 예: `svr01ev01`이 자기 DHCP 예약 MAC과는 안 맞는데 `svr01ev02`의 DHCP 예약 MAC과 일치하면, Fail 사유에 "교차 설치(역설치) 의심"이 명시된다.
- vCenter 접속, DHCP 조회(DNS 포함), 검증까지 전부 goroutine 기반으로 병렬 처리된다 (worker pool로 동시 실행 수를 CPU 코어 수 기준 최대 16개로 제한. `-race` 디텍터로 데이터 레이스 없음 확인).
- `vcenter.txt`/`targets.txt`는 실제 인프라 정보라 git에 올리지 않는다(.gitignore) — 위 예시를 보고 각자 환경에 맞게 만들 것.

### `-failonly` — 이상 있는 것만 출력

```bash
./vm-verifier -vcenterList vcenter.txt -f targets.txt -failonly
```

- 옵션 없이 실행하면 PASS/FAIL 전부 한 줄씩 출력한다 (도입 초기 전수 확인용).
- `-failonly`를 주면 FAIL만 출력한다. **전체가 PASS면** 다음처럼 요약 한 줄만 출력한다:
  ```
  검증 완료 — 이상 없음 (VM 5대, MAC 주소가 모두 DHCP 등록 정보와 일치)
  ```

대화형 편의 스크립트:
```bash
bash run.sh
```

불일치가 하나라도 있으면 exit code 1 (자동 조치는 하지 않고 로그만 남김 — PLAN.md 2장).

## 3. 감사 로그

FAIL이 감지된 hostname만 실행 디렉토리의 `LOG/vm-verifier-YYYYMMDD.log`에 append된다(git 미포함). 별도 중앙 로그 서버는 쓰지 않는다. `-failonly` 옵션과 무관하게 항상 이 규칙을 따른다.

## 4. 알려진 제약 (PLAN.md 6장·7장 참고)
- 이름이 여러 vCenter에 걸쳐 중복되면(동일 VM명이 vCenter A, B에 둘 다 있는 경우) 마지막으로 조회된 값으로 덮어쓰되, 빨간 깜빡임 경고를 콘솔에 출력한다 — 보통 vCenter 간 VM명은 고유하다는 전제라 이 이상의 별도 처리는 하지 않는다.
- 트리거는 작업자 수동 실행만 지원 (이벤트 구독형 자동 트리거는 범위 밖).
- hostname/IP/DNS PTR/UUID 이력 대조는 하지 않는다 — MAC 스왑 탐지 목적에만 집중 (PLAN.md 5장 "폐기된 항목" 참고).
