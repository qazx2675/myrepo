# 12. vm_verifier — VM 배포 정합성 검증 (교차 설치 탐지)

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🟢 **읽기 전용.** 조회만 하고 아무것도 바꾸지 않습니다 |
| 폴더 | `.claude/VM/vm_verifier/` |
| 바이너리 | `vm-verifier` |
| 하는 일 | VM 생성 직후(파워온 전) vCenter가 인식한 vNIC MAC과 DHCP 예약 MAC을 대조해 **교차 설치(역설치)** 를 탐지 |
| 인증 | `VC_USER` / `VC_PASS` (환경변수) |
| 대표 명령 | `./vm-verifier -vcenterList vcenter.txt -f targets.txt -failonly` |
| 종료 코드 | 불일치가 하나라도 있으면 **1** |

---

## 1. 이 도구는 무엇인가

VM을 새로 만들면 가상 NIC에 MAC 주소가 부여됩니다. 이 MAC을 DHCP 서버에 등록해두면 원하는 IP와 OS 이미지가 자동 배포됩니다(Kickstart).

여기서 사고가 납니다. **DHCP에 MAC을 잘못 등록하면 A 서버에 깔려야 할 OS가 B 서버에 깔립니다** — 이걸 **교차 설치(역설치)** 라고 부릅니다.

**파워온 전에 잡아내는 게 핵심입니다.**

- OS 설치가 끝난 뒤 발견하면 → **재설치 필요**
- 파워온 전에 잡으면 → DHCP/네트워크 설정만 바로잡고 넘어감

**에이전트리스**입니다. 대상 VM에 아무것도 설치하지 않습니다 — 이 시점엔 게스트 OS 자체가 없어서 VMware Tools 조회 같은 것도 하지 않고, **vCenter API로 vNIC MAC만** 봅니다.

---

## 2. 언제 쓰나

VM을 대량 생성한 직후, **전원을 켜기 전에** 돌립니다. 신규 서버 구축 파이프라인에서 `vm_create` → **`vm_verifier`** → 파워온 순서로 넣으면 됩니다.

---

## 3. 준비물

### 3-1. 빌드

```bash
cd .claude/VM/vm_verifier
bash setup.sh
```

> `vendor/` 폴더가 없으면 실패합니다. 인터넷 되는 PC에서 `go mod vendor`를 먼저 실행해 `vendor/`를 만들고 폴더와 함께 옮겨야 합니다.

### 3-2. 인증

```bash
export VC_USER='administrator@vsphere.local'
read -rsp 'vCenter 비밀번호: ' VC_PASS; export VC_PASS; echo
```

### 3-3. 입력 파일

**`vcenter.txt`** — vCenter 주소를 한 줄에 하나씩. 모든 vCenter를 **병렬로** 접속해 VM 목록을 조회합니다.

```
192.168.0.50
192.168.0.51
```

**`targets.txt`** (`-f`로 지정, **필수**) — 검증할 BM 접두어를 한 줄에 하나씩 (`#` 주석 가능).

```
svr01
svr02
```

각 접두어에 대해 vCenter에 실제로 등록된 `{접두어}ev숫자` 패턴의 VM을 **개수 제한 없이 전부 자동으로 찾아서** 검증합니다. `svr01ev01`만 있으면 1대, `svr01ev01`~`svr01ev05`까지 있으면 5대 전부.

> **VM 이름을 그대로 적어도 됩니다.** 아래처럼 적으면 실행 전에 접두어(`svr01`, `svr02`)로 자동 변환하고, 중복을 없앤 뒤 이름순으로 정렬해서 진행합니다.
>
> ```
> svr01ev01
> svr01ev02
> svr02ev01
> ```
>
> 정리가 일어나면 시작할 때 `[INFO] 대상 목록 정리: 4줄 -> BM 접두어 2개 ...` 한 줄로 알려줍니다.
> `ev` + 숫자로 끝나지 않는 줄은 손대지 않고 그대로 접두어로 씁니다.

> ⚠️ `vcenter.txt` / `targets.txt`는 실제 인프라 정보라 **git에 올리지 않습니다**(`.gitignore`). 각자 환경에 맞게 만드세요.

### 3-4. DHCP 파일 접근

DHCP 대역 파일은 `-subnet` 같은 옵션 없이 **자동으로 찾습니다.**

1. 각 hostname을 **DNS로 조회**해 IP를 얻고
2. 그 IP의 **앞 3옥텟**으로 `-dhcp-root`(기본 `/user/caedhcp`) 아래 파일명을 만들어
3. **그 파일 하나만** 읽습니다

> **파일명은 3옥텟까지만 씁니다.**
> `10.10.10.15` 대역 → 파일명 `10.10.10` (`10.10.10.0`처럼 4번째 옥텟을 붙이지 않음)
> `1.1.1.1` 대역 → 파일명 `1.1.1`

따라서 도구를 실행하는 서버에서 **DNS 조회가 되어야 하고, `/user/caedhcp` 경로를 읽을 수 있어야** 합니다.

---

## 4. 사용 방법

### 4-1. 기본 실행

```bash
VC_USER='administrator@vsphere.local' VC_PASS='...' \
  ./vm-verifier -vcenterList vcenter.txt -f targets.txt
```

옵션 없이 실행하면 PASS/FAIL을 전부 한 줄씩 출력합니다 (도입 초기 전수 확인용).

### 4-2. 이상 있는 것만 보기

```bash
./vm-verifier -vcenterList vcenter.txt -f targets.txt -failonly
```

FAIL만 출력합니다. **전체가 PASS면** 요약 한 줄만 나옵니다:

```
검증 완료 — 이상 없음 (VM 5대, MAC 주소가 모두 DHCP 등록 정보와 일치)
```

### 4-3. 대화형 실행 (편의 스크립트)

```bash
bash run.sh
```

바이너리가 없으면 먼저 빌드하고, `VC_USER`/`VC_PASS`와 파일 경로를 하나씩 물어봅니다. 옵션을 외우기 싫을 때 편합니다.

---

## 5. 옵션 상세표

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcenterList <path>` | `vcenter.txt` | | vCenter 주소 목록 파일 (한 줄에 하나) |
| `-f <path>` | (없음) | ✅ | 검증할 **BM 접두어** 목록 파일. VM 이름을 그대로 적어도 자동 변환됨 |
| `-dhcp-root <path>` | `/user/caedhcp` | | DHCP 설정 파일 루트 경로 |
| `-failonly` | `false` | | PASS는 출력하지 않고 FAIL만 출력. 전부 PASS면 요약 한 줄 |
| `-concurrency <N>` | `32` | | DNS 조회/DHCP 파일 조회 및 검증 동시 실행 수. 대부분 네트워크·파일 I/O 대기라 **CPU 코어 수보다 크게 잡아도 됨** |

---

## 6. 결과 해석

### 교차 설치(역설치) 탐지

같은 BM 그룹의 형제끼리 MAC이 뒤바뀐 경우도 탐지합니다.

예: `svr01ev01`이 자기 DHCP 예약 MAC과는 안 맞는데 **`svr01ev02`의 DHCP 예약 MAC과 일치**하면, Fail 사유에 **"교차 설치(역설치) 의심"** 이 명시됩니다.

### 종료 코드

| 코드 | 의미 |
|---|---|
| `0` | 전부 일치 |
| `1` | 불일치가 하나라도 있음 |

**자동 조치는 하지 않습니다.** 로그만 남깁니다.

### 감사 로그

**FAIL이 감지된 hostname만** 실행 디렉토리의 `LOG/vm-verifier-YYYYMMDD.log`에 append됩니다(git 미포함). 별도 중앙 로그 서버는 쓰지 않습니다. `-failonly` 옵션과 무관하게 항상 이 규칙을 따릅니다.

---

## 7. 성능

- vCenter 접속, DHCP 조회(DNS 포함), 검증까지 전부 goroutine 기반 병렬 처리 (`-race` 디텍터로 데이터 레이스 없음 확인)
- vCenter에서는 인벤토리 전체의 **이름만 먼저 가볍게** 훑고, 대상 접두어에 해당하는 VM에 대해서만 무거운 장치 정보(`config.hardware.device`)를 배치 조회
- DHCP 대역 파일도 **대역당 한 번만 읽어 캐시**, DNS 조회는 그룹 구분 없이 한 번에 병렬 처리

그래서 vCenter 인벤토리가 크거나 대상이 많아도 조회 시간이 급격히 늘지 않습니다.

---

## 8. 자주 나는 오류와 해결

| 증상 | 원인 | 해결 |
|---|---|---|
| `-f`가 필수라는 에러 | 대상 목록 파일 미지정 | `-f targets.txt` 추가 |
| 인증 실패 | `VC_PASSWORD`를 설정함 | 이 도구는 **`VC_PASS`** 입니다 (`VC_PASSWORD` 아님) |
| DHCP 파일을 못 찾음 | DNS 조회 실패, 또는 `-dhcp-root` 경로가 다름 | `nslookup <hostname>`으로 DNS 확인, `ls /user/caedhcp` 확인 |
| DHCP 파일명이 `10.10.10.0`인데 못 읽음 | 도구는 **3옥텟**(`10.10.10`)으로만 찾음 | DHCP 파일명 규칙을 확인. 규칙이 다르면 코드 수정 필요 |
| 빨간 깜빡임 경고가 나옴 | 같은 VM 이름이 여러 vCenter에 중복 존재 | 아래 "한계" 참고 |
| `vendor/ 없음` | 빌드 시 vendor 누락 | 인터넷 되는 PC에서 `go mod vendor` 후 폴더째 이동 |

---

## 9. 주의사항과 한계

- 이름이 **여러 vCenter에 걸쳐 중복**되면(동일 VM명이 vCenter A, B에 둘 다 있는 경우) 마지막으로 조회된 값으로 덮어쓰되, **빨간 깜빡임 경고**를 콘솔에 출력합니다. 보통 vCenter 간 VM명은 고유하다는 전제라 이 이상의 처리는 하지 않습니다.
- 트리거는 **작업자 수동 실행만** 지원합니다 (이벤트 구독형 자동 트리거는 범위 밖).
- hostname/IP/DNS PTR/UUID 이력 대조는 하지 않습니다 — **MAC 스왑 탐지 목적에만** 집중합니다.
- 자동 조치를 하지 않습니다. FAIL이 나오면 사람이 DHCP 설정을 고쳐야 합니다.

---

## 10. 파일 구조

```
vm_verifier/
├── README.md        # 1차 자료
├── PLAN.md          # 설계 배경 (왜 파워온 전인지, 무엇을 폐기했는지)
├── CHANGELOG.md     # 변경 이력 ← 수정 시 갱신
├── main.go          # CLI 진입점, 옵션 파싱
├── main_test.go
├── run.sh           # 대화형 편의 스크립트
├── setup.sh         # 폐쇄망 빌드
├── vc/              # vCenter 조회
│   ├── vc.go          # 세션, VM 목록 조회
│   └── devices.go     # vNIC MAC 추출
├── dhcp/            # DHCP 예약 조회
│   ├── dhcp.go        # 대역 파일 파싱
│   ├── cache_test.go
│   └── dhcp_test.go
├── verify/verify.go # 대조 판정 로직 ← "무엇을 FAIL로 볼지"를 고칠 때
├── auditlog/        # LOG/ 파일 기록
└── vendor/          # 의존성
```

---

## 11. 관련 문서

- 1차 자료: `vm_verifier/README.md`
- 설계 배경: `vm_verifier/PLAN.md`
- MAC 조회만 따로 하려면: [10. VM_setup](./10_VM_setup.md)의 `mac_info-source`
- VM 생성: [10. VM_setup](./10_VM_setup.md)의 `vm_create-source`, [17. vm-setting-go-lang](./17_vm-setting-go-lang.md)
