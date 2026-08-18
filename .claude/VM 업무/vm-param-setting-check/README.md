# vm-param-check 기술문서

vCenter에 있는 VM들이 고성능(High Performance) 설정 기준(CPU/메모리/NUMA 토폴로지, vCPU affinity, Shares, 호스트 전원정책 등)을 만족하는지 자동으로 점검해서, 콘솔 요약과 CSV 상세 로그로 OK/FAIL을 산출하는 도구입니다. (`PLAN.md` 참고)

> ⚠️ **주의사항 (Disclaimer)**
> 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

이 저장소는 Go 모듈 의존성(`github.com/vmware/govmomi` 등)을 `vendor/` 폴더에 통째로 포함하고 있습니다.
그래서 **폴더를 그대로 복사(또는 git clone)해서 옮기면 인터넷이 없는 폐쇄망에서도 빌드가 가능**합니다.

### 1) 저장소 받기

```bash
git clone <이 저장소 주소> vm-param-check
cd vm-param-check
```

인터넷이 안 되는 폐쇄망 서버로 옮길 때는 `git clone`이 된 이 폴더(`.git` 포함) 전체를 압축해서
그대로 복사해 넣으면 됩니다. (`vendor/`가 이미 포함돼 있으므로 다시 받을 필요 없음)

```bash
# 인터넷 되는 곳에서
tar czf vm-param-check.tar.gz vm-param-check/
# USB/scp 등으로 폐쇄망 서버에 복사 후
tar xzf vm-param-check.tar.gz
```

### 2) 빌드

**폐쇄망(오프라인, 기본 권장)** — Go만 설치되어 있으면 인터넷 없이 됩니다:

```bash
cd ".claude/VM 업무/vm-param-setting-check"
bash setup.sh
```

**인터넷이 되는 환경** — 의존성을 새로 받아 최신화하고 싶을 때만:

```bash
go mod tidy      # go.mod/go.sum/vendor 최신화 (인터넷 필요)
go mod vendor
go build -o vm-param-check .
```

Go 요구 버전: `go.mod`에 명시된 대로 **1.21 이상**. 폐쇄망 서버(Rocky Linux 등)에도 Go 바이너리를
(예: `go1.26.5.linux-amd64.tar.gz`) 미리 복사해서 `/usr/local/go`에 풀고 `PATH`에 `/usr/local/go/bin`을
추가하면 됩니다.

### 3) 실행

```bash
export VC_USER='administrator@vsphere.local'
export VC_PASS='...'
./vm-param-check -demo   # 먼저 데모 모드로 동작 확인 (vCenter 접속 없이)
```

### 4) 전역 명령어로 사용하기 (선택 사항)
빌드된 실행 바이너리를 매번 해당 폴더로 이동하지 않고 시스템 어디서든 기본 명령어처럼 편리하게 사용하려면, 환경 변수(`PATH`)가 지정된 기본 경로로 파일을 복사하거나 이동해 주시면 됩니다.
```bash
# 예: /usr/local/bin 경로로 복사하여 전역 명령어로 등록
sudo cp vm-param-check /usr/local/bin/
```
이후부터는 터미널 어느 경로에서나 `vm-param-check` 명령어만 입력하면 툴이 즉시 실행됩니다.

## 2. 사용 방법

- **전체 순회 모드 (기본)**: `-vcenterList`에 나열된 모든 vCenter의 VM 인벤토리 전체를 체크
- **단일/지정 대상 모드**: `-f <파일>`로 체크할 BM(VM) hostname 목록을 주면 그 VM들만 체크

### 사용 예시

**1. 전체 순회 모드 — vcenter.txt에 있는 모든 vCenter의 VM 전체 체크:**
```bash
./vm-param-check --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --shares-ev02=1000 --shares-ev03=1000 \
  --affinity-ev01=ev01.txt --affinity-ev02=ev02.txt --affinity-ev03=ev03.txt \
  --out=result.csv
```
(`--affinity-ev01`을 안 주면 ev01은 그대로 `-ht`/`-cores` 자동계산을 씁니다. 세 옵션 다 선택사항이라 필요한 것만 주면 됩니다.)

**1-1. ev02/ev03가 스펙이 다를 때 — 코어수/NUMA/vCPU/메모리/디스크도 그룹별로 다르게:**
```bash
./vm-param-check --ht=on \
  --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 --shares-ev01=2000 \
  --cores-ev02=4 --numa-ev02=4 --cpu-ev02=8 --mem-ev02=32 --disk-ev02=200 --shares-ev02=1000 \
  --out=result.csv
```

**2. 지정 대상 모드 — hostname 목록 파일로 특정 VM들만:**
```bash
# kdh.txt: 체크할 BM(VM) hostname을 한 줄씩
cat kdh.txt
# 192ev01
# 192ev02

./vm-param-check -f kdh.txt --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --shares-ev02=1000 \
  --affinity-ev01=ev01.txt --affinity-ev02=ev02.txt \
  --out=result.csv
```

**3. 대수 많을 때 문제 있는 VM만 보기:**
```bash
./vm-param-check --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --onlyFail --out=result.csv
```

**4. 컬러 없이(파일로 리다이렉트하거나 컬러 미지원 터미널):**
```bash
./vm-param-check --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --noColor --out=result.csv > report.txt
```

**5. 실제 인프라 없이 동작만 확인 — 데모 모드:**
```bash
./vm-param-check -demo
```

인증은 공통 환경변수를 사용합니다: `VC_USER`/`VC_PASS` (없으면 `VCENTER_USER`/`VCENTER_PASS`로 폴백).
```bash
export VC_USER='administrator@vsphere.local'
export VC_PASS='...'
```

## 3. 옵션별 상세 설명

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-vcenterList <path>` | `vcenter.txt` | 전체 순회 모드에서 사용할 vCenter 주소 목록 파일 (한 줄에 하나) |
| `-f <path>` | (없음) | 단일/지정 대상 모드: 체크할 BM(VM) hostname 목록 파일 (한 줄에 하나, `#` 주석 가능). 지정 시 `-vcenterList`의 vCenter들 안에서 이 hostname들만 체크. 미지정 시 인벤토리 전체 체크 |
| `-ht <on\|off>` | (필수) | 하이퍼스레딩 상태 — ev01 그룹 affinity 자동계산에 사용 |
| `-cores <N>` | (필수) | 기대값: 소켓당 코어 수 — ev01 및 미분류(ev01/02/03 어디에도 안 속하는) VM에 적용 |
| `-cores-ev02 <N>` | (옵션) | 기대값: ev02 그룹 소켓당 코어 수. 안 주면 ev02 코어수 체크 스킵 |
| `-cores-ev03 <N>` | (옵션) | 기대값: ev03 그룹 소켓당 코어 수. 안 주면 ev03 코어수 체크 스킵 |
| `-numa <N>` | (필수) | 기대값: NUMA 노드당 최대 vCPU(코어) 수 — ev01 및 미분류 VM에 적용 |
| `-numa-ev02 <N>` | (옵션) | 기대값: ev02 그룹 NUMA 노드당 최대 vCPU 수. 안 주면 ev02 NUMA 체크 스킵 |
| `-numa-ev03 <N>` | (옵션) | 기대값: ev03 그룹 NUMA 노드당 최대 vCPU 수. 안 주면 ev03 NUMA 체크 스킵 |
| `-cpu <N>` | (필수) | 기대값: vCPU 수 — ev01 및 미분류 VM에 적용 |
| `-cpu-ev02 <N>` | (옵션) | 기대값: ev02 그룹 vCPU 수. 안 주면 ev02 vCPU 체크 스킵 |
| `-cpu-ev03 <N>` | (옵션) | 기대값: ev03 그룹 vCPU 수. 안 주면 ev03 vCPU 체크 스킵 |
| `-mem <N>` | (필수) | 기대값: 메모리 GB — ev01 및 미분류 VM에 적용 |
| `-mem-ev02 <N>` | (옵션) | 기대값: ev02 그룹 메모리 GB. 안 주면 ev02 메모리 체크 스킵 |
| `-mem-ev03 <N>` | (옵션) | 기대값: ev03 그룹 메모리 GB. 안 주면 ev03 메모리 체크 스킵 |
| `-disk <N>` | (필수) | 기대값: 디스크 총량 GB — ev01 및 미분류 VM에 적용 |
| `-disk-ev02 <N>` | (옵션) | 기대값: ev02 그룹 디스크 총량 GB. 안 주면 ev02 디스크 체크 스킵 |
| `-disk-ev03 <N>` | (옵션) | 기대값: ev03 그룹 디스크 총량 GB. 안 주면 ev03 디스크 체크 스킵 |
| `-shares-ev01 <N>` | (필수) | 기대값: ev01 그룹 Shares(ratio) — CPU/메모리 Shares 둘 다 이 값으로 체크 |
| `-shares-ev02 <N>` | (옵션) | 기대값: ev02 그룹 Shares(ratio). 안 주면 ev02 shares 체크 스킵 |
| `-shares-ev03 <N>` | (옵션) | 기대값: ev03 그룹 Shares(ratio). 안 주면 ev03 shares 체크 스킵 |
| `-affinity-ev01 <path>` | (옵션) | ev01 그룹 기대 affinity 파일. **안 주면 기존과 동일하게 `-ht`/`-cores` 기반 자동계산 사용**. 주면 자동계산 대신 이 파일값으로 비교 |
| `-affinity-ev02 <path>` | (옵션) | ev02 그룹 기대 affinity 파일. 안 주면 ev02 affinity 체크 스킵 |
| `-affinity-ev03 <path>` | (옵션) | ev03 그룹 기대 affinity 파일. 안 주면 ev03 affinity 체크 스킵 |
| `-out <path>` | (없음 → 타임스탬프 자동생성) | 상세 CSV 출력 경로. 같은 이름에 `_summary`가 붙은 요약 CSV가 하나 더 생성됨 |
| `-onlyFail` | `false` | PASS(문제 없음)인 VM은 콘솔/CSV 모두에서 제외하고 FAIL/설정없음이 있는 VM만 출력 (대수 많을 때 가독성용). **콘솔 상세 섹션도 이때는 FAIL/설정없음 항목만** 보여줌. 미지정(기본)이면 콘솔 상세 섹션에 OK/정보 항목까지 전부 출력됨 |
| `-noColor` | `false` | 콘솔 출력에서 ANSI 컬러(FAIL=빨강/설정없음=노랑/PASS=초록)를 끔 — 컬러 미지원 터미널/파일 리다이렉트용 |
| `-demo` | `false` | vCenter에 연결하지 않고, affinity 항목이 많은 가짜 VM 3대로 콘솔+CSV 출력을 보여주는 데모 모드 |

## 4. 문서별 고유 설명

### 콘솔 출력
콘솔은 항상 `[1] VM별 요약 표`(대수/OK/FAIL/설정없음/정보 집계)를 먼저 보여주고, 그 아래 `[2]` 상세 섹션이 이어집니다.
- **기본(= `-onlyFail` 미지정)**: `[2]`는 모든 VM의 모든 항목(OK/FAIL/설정없음/정보)을 빠짐없이 보여줍니다 — CSV 상세와 동일한 정보를 콘솔에서도 확인할 수 있습니다. VM이 많으면 그만큼 출력도 길어집니다.
- **`-onlyFail`**: `[2]`는 FAIL이 있는 VM만, 그 안에서도 FAIL/설정없음 항목만 보여줘서 문제만 빠르게 훑어볼 수 있습니다.

### 출력 파일
`-out=result.csv`로 실행하면 파일 2개가 생성됩니다.
- **`result.csv` (상세)**: 조사한 모든 설정값을 항목(Key) 단위로 빠짐없이 기록 (OK 포함, 필터링 없음). 컬럼: `VM명, 소스, 항목Key, 기대값, 실제값, 결과, 비고`
- **`result_summary.csv` (요약)**: VM 1대당 한 줄. 컬럼: `VM명, 전체결과, OK, FAIL, 설정없음, 정보`

상세 CSV는 VM명 → 소스(카테고리) → 항목Key 순으로 정렬되어 있고, `sched.vcpuN.affinity` 같은 번호가 붙는 Key는 자연수 순서(0,1,2...10,11...)로 정렬됩니다.

결과(`결과` 컬럼) 값은 4가지입니다:
- `OK`: 기대값과 실제값 일치
- `FAIL`: 값이 설정돼 있지만 기대값과 다름
- `설정없음`: 해당 설정 자체가 없음 (FAIL과 구분 — 아예 미설정 상태)
- `정보`: OK/FAIL 판정이 없는 정보성 항목 (네트워크 포트그룹 등)

### 체크 항목 카테고리 (소스 컬럼)

| 소스 | 의미 |
|---|---|
| `-` (공통설정) | 모든 VM 공통 고정값 체크 — 메모리/스케줄러 설정(`sched.mem.*`), CPU 토폴로지(코어수/NUMA, Advanced Config + UI 값 각각), vCPU/메모리/디스크 수치, "모든 게스트 메모리 예약" |
| `host` | 이 VM이 돌고 있는 ESXi 호스트의 전원 정책 (기대값 항상 High Performance) |
| `ev01` | hostname에 `ev01` 포함된 VM — vCPU affinity(HT on/off + 코어수로 자동계산), Shares(CPU/메모리) — 항상 필수 체크 |
| `ev02` | hostname에 `ev02` 포함된 VM — affinity(파일 기반), Shares(CPU/메모리) — `-affinity-ev02`/`-shares-ev02` 옵션을 줬을 때만, VM이 1대뿐이면 스킵 |
| `ev03` | ev03 그룹 |
| `network` | VM에 붙은 네트워크 어댑터의 포트그룹 이름 (판정 없음, 정보성). |

### 체크 항목 상세 (실제로 조회/비교하는 값)

**공통 고정값 — 모든 VM**
| Key | 기대값 | 비고 |
|---|---|---|
| ESXi 호스트 전원 정책 | High Performance | VM이 돌고 있는 호스트 기준, 항상 고정 |
| sched.mem.lpage.enable1GPage | TRUE | VM Advanced Config |
| sched.mem.prealloc | TRUE | VM Advanced Config |
| sched.mem.prealloc.pinnedMainMem | TRUE | VM Advanced Config |
| sched.swap.vmxSwapEnabled | FALSE | VM Advanced Config |
| config.memoryReservationLockedToMax | true | "모든 게스트 메모리 예약" — 항상 켜져 있어야 함 |
| cpuid.coresPerSocket | (그룹별 지정) | Advanced Config |
| hardware.numCoresPerSocket | (그룹별 지정) | VM 옵션 → CPU 토폴로지 UI 값 |
| numa.vcpu.maxPerVirtualNode | (그룹별 지정) | Advanced Config |
| config.numaInfo.coresPerNumaNode | (그룹별 지정) | CPU 토폴로지 UI 값 (vSphere API 8.0.0.1+ 필요) |
| config.hardware.numCPU | (그룹별 지정) | vCPU 수 |
| config.hardware.memoryMB | (그룹별 지정) | 메모리 크기 |
| 디스크 총 용량 | (그룹별 지정) | 연결된 모든 VirtualDisk 용량 합산 |
| 네트워크 포트그룹 | (판정 없음) | 어댑터의 포트그룹 이름 |

**ev01 / ev02 / ev03 그룹별**
| 그룹 | 조건 | 체크 항목 | 기대값 산출 방식 |
|---|---|---|---|
| ev01 | hostname에 `ev01` 포함 | 코어수/NUMA/vCPU/메모리/디스크, vCPU affinity, Shares | `-cores`/`-numa`/`-cpu`/`-mem`/`-disk`/`-shares-ev01` 항상 체크 |
| ev02 | hostname에 `ev02` 포함 | 위와 동일 | 각각의 `-ev02` 옵션 사용 |
| ev03 | hostname에 `ev03` 포함 | 위와 동일 | 각각의 `-ev03` 옵션 사용 |

### 데이터센터 범위 및 알려진 한계
- **데이터센터 무관 전체 탐색**: vCenter 조회는 `ServiceContent.RootFolder`(vCenter 최상위, 모든 Datacenter의 부모)를 재귀적으로 순회합니다.
- **병렬 처리**: `-vcenterList`에 vCenter가 여러 개면 접속·조회를 동시에 진행합니다(하나씩 순서대로 기다리지 않음).
- **알려진 한계**:
  - NUMA "코어수/UI 값" 비교 중 하나는 `config.numaInfo.coresPerNumaNode`(vSphere API 8.0.0.1+ 필요)를 쓰는데, 이 필드가 없는 구버전 vCenter에서는 "설정없음"으로 나옵니다.
  - Shares는 계획서에 CPU/메모리 구분이 없어 `-shares-evNN` 값 하나를 CPU/메모리 Shares 양쪽에 동일하게 적용합니다.
