# VM 배포 정합성 자동 검증 도구 (vm-verifier)

**VM 생성 직후(OS 설치·파워온 전)**에 vCenter가 인식한 vNIC MAC과 DHCP 정적 예약 MAC을 대조해, DHCP MAC 오기입 등으로 생기는 **교차 설치(역설치)**를 찾아내는 CLI 도구입니다. 설계 배경은 [PLAN.md](./PLAN.md), 작업 흐름은 [WORKFLOW.md](./WORKFLOW.md)를 참고하세요.

핵심은 **파워온 전에 잡아내는 것**입니다. OS 설치가 끝난 뒤에 발견하면 재설치가 필요하지만, 파워온 전에 잡으면 DHCP/네트워크 설정만 바로잡고 넘어갈 수 있습니다.

**에이전트리스** — 대상 VM에 아무것도 설치하지 않습니다. 이 시점에는 Guest OS가 아직 없으므로 Guest Tools 조회도 하지 않고, vCenter API로 vNIC MAC만 확인합니다.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 이 도구는 조회·대조만 하고 설정을 바꾸지 않지만, FAIL이 나와 DHCP/네트워크 설정을 수정했다면 수정 후 무작위로 서버 몇 대를 골라 실제로 반영되었는지 직접 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

```bash
cd ".claude/VM/vm_verifier"
bash setup.sh
```

`setup.sh`는 공통 의존성(`../../공통/govendor/govmomi-0.39.0`)을 `vendor`로 링크한 뒤 `-mod=vendor`로 빌드하므로 인터넷 없이 빌드됩니다. 이 폴더만 떼어 가면 링크 대상이 없으므로, 폐쇄망으로 옮길 때는 `.claude/공통/govendor/`도 함께 옮기세요.

## 2. 사용 방법

```bash
VC_USER='administrator@vsphere.local' VC_PASS='...' \
  ./vm-verifier -vcenterList vcenter.txt -f targets.txt
```

대화형으로 빌드·계정·옵션을 차례로 물어보는 편의 스크립트도 있습니다.

```bash
bash run.sh
```

### 입력 파일

- **`vcenter.txt`** — vCenter 주소를 한 줄에 하나씩 적습니다. 모든 vCenter에 **병렬로** 접속해 VM 목록을 조회합니다.
  ```
  192.168.0.50
  192.168.0.51
  ```
- **`targets.txt`** (`-f`로 지정, 필수) — 검증할 BM 접두어를 한 줄에 하나씩 적습니다(`#` 주석 가능).
  ```
  svr01
  svr02
  ```
  접두어마다 vCenter에 실제로 등록된 `{접두어}ev숫자` 패턴의 VM을 **개수 제한 없이 모두 자동으로 찾아** 검증합니다(`svr01ev01`만 있으면 1대, `svr01ev01`~`svr01ev05`가 있으면 5대 전부).

  **VM 이름을 그대로 적어도 됩니다.** 아래처럼 적으면 실행 전에 접두어(`svr01`, `svr02`)로 자동 변환하고, 중복을 없앤 뒤 이름순으로 정렬해 진행합니다. 어차피 접두어마다 VM을 모두 찾으므로 결과는 접두어로 적었을 때와 같습니다.
  ```
  svr01ev01
  svr01ev02
  svr02ev01
  svr02ev02
  ```
  목록을 정리했다면 시작할 때 `[INFO] 대상 목록 정리: 4줄 -> BM 접두어 2개 ...` 한 줄로 알려 줍니다. `ev` + 숫자로 끝나지 않는 줄은 손대지 않고 그대로 접두어로 씁니다.
- `vcenter.txt`/`targets.txt`는 실제 인프라 정보이므로 git에 올리지 않습니다(.gitignore). 위 예시를 참고해 각자 환경에 맞게 만드세요.

### 동작 방식

- **DHCP 대역 파일 자동 탐색** — `-subnet` 옵션 없이 자동으로 찾습니다. 각 hostname을 DNS로 조회해 IP를 얻고, 그 앞 **3옥텟**으로 `-dhcp-root`(기본 `/user/caedhcp`) 아래 파일명을 만들어 그 파일 하나만 읽습니다. **파일명은 3옥텟까지만** 씁니다 — `10.10.10.15` 대역이면 파일명은 `10.10.10`(`10.10.10.0`처럼 4번째 옥텟을 붙이지 않음), `1.1.1.1` 대역이면 `1.1.1`입니다.
- **교차 설치(역설치) 탐지** — 같은 BM 그룹의 형제 VM끼리 MAC이 뒤바뀐 경우도 찾아냅니다. 예를 들어 `svr01ev01`이 자기 DHCP 예약 MAC과는 맞지 않는데 `svr01ev02`의 DHCP 예약 MAC과 일치하면, FAIL 사유에 "교차 설치(역설치) 의심"이 표시됩니다.
- **전 과정 병렬 처리** — vCenter 접속, DHCP 조회(DNS 포함), 검증을 모두 goroutine으로 병렬 처리합니다(worker pool로 동시 실행 수 제한, `-race` 디텍터로 데이터 레이스 없음 확인).
- **조회 성능** — vCenter에서는 인벤토리 전체의 이름만 먼저 가볍게 훑고, 대상 접두어에 해당하는 VM만 무거운 장치 정보(`config.hardware.device`)를 배치 조회합니다. DHCP 대역 파일도 대역당 한 번만 읽어 캐시하고, DNS 조회는 그룹 구분 없이 한꺼번에 병렬 처리합니다. 그래서 인벤토리가 크거나 대상이 많아도 조회 시간이 급격히 늘지 않습니다.
- **종료 코드** — 불일치가 하나라도 있으면 exit code 1로 끝납니다. 자동 조치는 하지 않고 로그만 남깁니다(PLAN.md 2장).

### `-failonly` — 이상 있는 것만 출력

```bash
./vm-verifier -vcenterList vcenter.txt -f targets.txt -failonly
```

- 옵션 없이 실행하면 PASS/FAIL을 모두 한 줄씩 출력합니다(도입 초기 전수 확인용).
- `-failonly`를 주면 FAIL만 출력합니다. **전체가 PASS이면** 다음과 같은 요약 한 줄만 출력합니다.
  ```
  검증 완료 — 이상 없음 (VM 5대, MAC 주소가 모두 DHCP 등록 정보와 일치)
  ```

## 3. 옵션별 상세 설명

| 옵션 | 기본값 | 설명 |
|---|---|---|
| `-vcenterList` | `vcenter.txt` | vCenter 주소 목록 파일 (한 줄에 하나) |
| `-f` | (필수) | 검증할 BM 접두어 목록 파일 (한 줄에 하나, `#` 주석 가능). VM 이름을 적어도 접두어로 자동 변환 |
| `-dhcp-root` | `/user/caedhcp` | DHCP 설정 파일 루트 경로 |
| `-failonly` | `false` | PASS는 출력하지 않고 FAIL만 출력. 전부 PASS면 요약 한 줄만 출력 |
| `-concurrency` | `32` | DNS 조회/DHCP 파일 조회/검증 동시 실행 수. 대부분 네트워크·파일 I/O 대기이므로 CPU 코어 수보다 크게 잡아도 됨 |

계정은 환경변수 `VC_USER`, `VC_PASS`로 전달합니다.

## 4. 문서별 고유 설명

### 4.1 감사 로그

FAIL이 감지된 hostname만 실행 디렉토리의 `LOG/vm-verifier-YYYYMMDD.log`에 추가(append)됩니다(git 미포함). 별도 중앙 로그 서버는 쓰지 않으며, `-failonly` 옵션과 관계없이 항상 이 규칙을 따릅니다.

### 4.2 알려진 제약 (PLAN.md 6장·7장 참고)

- 같은 VM 이름이 여러 vCenter에 걸쳐 있으면(vCenter A, B 모두에 있는 경우) 마지막으로 조회된 값으로 덮어쓰고, 콘솔에 빨간 깜빡임 경고를 출력합니다. vCenter 간 VM 이름은 고유하다는 전제이므로 그 이상의 처리는 하지 않습니다.
- 작업자가 수동으로 실행하는 방식만 지원합니다(이벤트 구독형 자동 트리거는 범위 밖).
- hostname/IP/DNS PTR/UUID 이력 대조는 하지 않습니다. MAC 스왑 탐지 목적에만 집중합니다(PLAN.md 5장 "폐기된 항목" 참고).

### 4.3 디렉토리 구조

```
vm_verifier/
├── README.md / PLAN.md / CHANGELOG.md   # 이 문서 / 설계 배경 / 변경 이력
├── WORKFLOW.md / ARCHITECTURE.md        # 작업 흐름도 / 폴더별 역할
├── PR_CHECKLIST.md                      # 수정·배포 전 체크리스트
├── main.go / main_test.go               # CLI 진입점(옵션, 대상 목록 정리, 전체 흐름)
├── vc/                                  # vCenter 병렬 접속, VM 이름 조회 → 대상 VM만 vNIC MAC 배치 조회
├── dhcp/                                # DNS로 대역 결정, DHCP 대역 파일 읽기·캐시
├── verify/                              # MAC 대조, 형제 VM 간 교차 설치 판정
├── auditlog/                            # FAIL 감사 로그(LOG/vm-verifier-YYYYMMDD.log)
├── setup.sh                             # 오프라인 빌드 (공통 govendor 링크)
└── run.sh                               # 대화형 실행 스크립트
```

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH에 포함된 디렉터리로 복사하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 쓸 수 있습니다.

```bash
sudo cp vm-verifier /usr/local/bin/
# 이후 어느 위치에서나 vm-verifier 명령어로 실행 가능 (LOG/는 실행한 디렉토리에 생김)
```
