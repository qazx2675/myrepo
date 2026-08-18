# VM 파라미터 설정값 체크 도구 (vm-param-check) 계획서

## 1. 목적
govmomi로 vCenter에 접속하여 VM의 고성능(High Performance) 설정값을 실제값 기준으로
조회하고, 사용자가 지정한 기대값(플래그/affinity 파일)과 비교하여 OK/FAIL 및
불일치 상세 사유를 CSV로 산출한다.

## 2. 실행 모드
- **단일/지정 대상 모드**: 실행 시 대상 파일(affinity 파일 등)이 지정되면 해당 VM들만 체크
- **전체 순회 모드**: 대상 미지정 시 `vcenter.txt`에 나열된 모든 vCenter를 순회하며
  인벤토리 내 VM 전체를 체크
- `vcenter.txt` 포맷: 한 줄에 vCenter 주소 하나 (인증은 `VC_USER`/`VC_PASS` 등 공통 환경변수 사용, 기존 govmomi 도구와 동일 방식)

## 3. 체크 항목

### 3-0. VM 그룹 분류 — 확정
- vCenter에서 조회한 VM의 **hostname**(guest hostname 또는 VM명 — 구현 시 실제 조회 가능한 필드로 확정)에
  `ev01`, `ev02`, `ev03` 문자열이 포함되어 있는지로 그룹을 나눔
- ev01 그룹: 3-5(affinity), 3-4(shares) 등에서 **필수** 기대값 적용
- ev02, ev03 그룹: 대상 VM이 없으면 자연히 스킵되고, 각각의 `--affinity-evNN`/`--shares-evNN` 옵션 자체를 안 주면 값 비교 없이 스킵
- **단일 VM 조사 시 예외**: 조사 대상이 VM 1개뿐인 경우, ev02/ev03용 옵션이 정의되어 있어도 해당 체크는 무조건 스킵됨 (ev02/ev03 로직은 여러 VM을 대상으로 할 때만 의미가 있음)

### 3-1. 호스트 전원 관리 — 확정
- ESXi 호스트의 전원 정책을 vCenter API로 조회
- 기대값은 항상 고정: **High Performance**
- 실제값이 High Performance면 OK, 아니면 FAIL (플래그/파일 입력 불필요, 고정 기준값)

### 3-2. 메모리/스케줄러 고정 설정 (VM Advanced Config, 모든 VM 공통 고정 기대값)
| Key | 기대값 |
|---|---|
| sched.mem.lpage.enable1GPage | TRUE |
| sched.mem.prealloc | TRUE |
| sched.mem.prealloc.pinnedMainMem | TRUE |
| sched.swap.vmxSwapEnabled | FALSE |

> `sched.mem.pin`은 vCenter Advanced Config에 실제로 기록되지 않는 파라미터이므로 체크 대상에서 제외.

### 3-3. CPU 토폴로지 — 확정 (2곳 비교)
- `--cores=<소켓당 코어수>` → 기대값
  - vCenter Advanced Config의 `cpuid.coresPerSocket`과 비교
  - **동시에** VM 설정편집 → VM 옵션 → CPU 토폴로지의 "소켓당 코어 수" 값과도 비교 (govmomi `VirtualMachineConfigInfo.Hardware.NumCoresPerSocket`)
- `--numa=<NUMA 노드 값>` → 기대값
  - `numa.vcpu.maxPerVirtualNode`와 비교
  - **동시에** VM 옵션 → CPU 토폴로지의 "NUMA 노드 수"에 해당하는 값과도 비교 (govmomi `ExtraConfig`의 `numa.autosize.vcpu.maxPerVirtualNode` 또는 VM 옵션 UI 노출값 확인 필요 — 구현 시 실제 필드명 검증)
- 즉 코어수/NUMA 각각 "Advanced Config 값"과 "CPU 토폴로지 UI 값" 두 군데 모두 기대값과 일치해야 최종 OK

### 3-4. 가상 하드웨어 — 확정
- `--cpu=<vCPU 수>`
- `--mem=<메모리 GB>`
- `--disk=<디스크 GB>`
- 모든 게스트 메모리 예약(Reserve all guest memory): **옵션 없이 항상 "설정되어 있어야 함(고정 기대값)"으로 체크** (무조건 ON이 정상)
- 공유값(Shares) = **Ratio 값**을 의미
  - `--shares-ev01=<ev01 기대 ratio>` (필수), `--shares-ev02=<ev02 기대 ratio>` (옵션), `--shares-ev03=<ev03 기대 ratio>` (옵션) 형태로 그룹별 기대값을 받아 vCenter 실제 shares 값과 비교 → OK/FAIL
  - ev02/ev03는 대상 VM이 없거나(자연 스킵), 조사 대상이 총 1개뿐이면 옵션이 있어도 스킵

### 3-5. vCPU Affinity (핵심 로직) — 확정
- **VM 분류 기준**: VM의 **hostname에 포함된 문자열**로 자동 분류
  - hostname에 `ev01` 포함 → **ev01 그룹** (필수 체크)
  - hostname에 `ev02` 포함 → **ev02 그룹** (옵션 체크, affinity-ev02 파일이 있을 때만 수행)
  - hostname에 `ev03` 포함 → **ev03 그룹** (옵션 체크, affinity-ev03 파일이 있을 때만 수행)
- **ev01 그룹**: 자동 계산 기대값 사용 (파일 입력 없음, 항상 체크됨)
  - HT 상태 + 코어수로 산출:
    - HT ON: VM 1개당 코어수만큼 `sched.vcpu[i].affinity` 생성, `[0,1],[2,3]...` 페어 방식으로 i 증가
    - HT OFF: `sched.vcpu[i].affinity[i]` = `[i]` 단일값
  - HT ON/OFF 여부는 **플래그로 입력받음** (`--ht=on` / `--ht=off`)
- **ev02 / ev03 그룹**: 각각 `sched.vcpu0.affinity = 0` 형식의 **affinity 파일**을 옵션으로 받아 그 값을 기대값으로 사용 (`--affinity-ev02`, `--affinity-ev03`)
  - 해당 옵션이 주어지지 않으면 그 그룹은 체크를 건너뜀(스킵) — 대상 VM이 아예 없어도 오류 아님
  - **조사 대상 VM이 총 1개뿐인 경우, `--affinity-ev02`/`--affinity-ev03`가 정의되어 있어도 해당 체크는 스킵** (VM이 여러 대일 때만 ev02/ev03 그룹 분류·체크가 동작)
  - ev01과 동일한 비교 방식(기대값 vs vCenter 실제값)을 적용하되, 기대값 산출 방식만 다름 (자동계산 vs 파일)
- **비교 로직**: ev01/ev02/ev03는 서로 비교 대상이 아니라, hostname으로 분류된 각 그룹이 각자의 기대값 소스(자동계산 or 파일) 기준으로 vCenter 실제값과 비교하여 OK/FAIL 산출

### 3-6. 네트워크 (포트그룹) — 확정
- OK/FAIL 판정 없음
- VM에 연결된 네트워크 어댑터의 **포트그룹 이름만 조회하여 CSV에 기록** (정보성 컬럼)

## 4. 남은 확인 사항
- CSV 파일명 규칙: 통합 1개 CSV로 확정됨. 타임스탬프 방식(예: `vm-param-check_20260814_153000.csv`)을 기본 적용, 필요시 옵션으로 변경 가능하게.

## 5. 출력 형식 — 확정
- **콘솔**: VM별 OK/FAIL 요약 표
- **CSV**: 전체 대상 VM(ev01 + ev02 + 기타 순회 대상) 통합 1개 파일, 항목(Key) 단위 상세 로그
  - **OK/FAIL 여부와 무관하게 조사한 모든 설정값을 빠짐없이 CSV에 기록** (OK도 포함, 필터링 없음)
  - 컬럼 예: `VM명, 소스(ev01/ev02/-), 항목Key, 기대값, 실제값, 결과(OK/FAIL/설정없음/정보), 비고`
  - 설정값 자체가 없는 경우: 결과 = "설정없음" (FAIL과 구분)
  - 값이 다른 경우: 기대값/실제값 모두 기록해 어떻게 다른지 확인 가능하도록 함
  - 네트워크 포트그룹 항목: 결과 = "정보" (OK/FAIL 미적용), 실제값 컬럼에 포트그룹명 기록

## 6. 대략적인 아키텍처 (Go)
```
vm-param-check/
├── main.go              # 플래그 파싱, 실행 모드 분기
├── config/
│   └── targets.go       # vcenter.txt / affinity 파일 로더
├── vcenter/
│   └── client.go        # govmomi 연결, VM AdvancedConfig 조회
├── checker/
│   ├── fixed.go         # 3-2 고정값 체크
│   ├── topology.go      # 3-3 CPU 토폴로지 체크
│   ├── hardware.go      # 3-4 가상 하드웨어 체크
│   └── affinity.go      # 3-5 affinity 계산/비교
├── report/
│   ├── console.go       # 콘솔 표 출력
│   └── csv.go           # CSV 상세 로그
└── vcenter.txt          # 전체 순회 대상 목록
```

## 7. 예상 실행 예시
```bash
./vm-param-check --ht=on --cores=8 --numa=2 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --shares-ev02=1000 --shares-ev03=1000 \
  --affinity-ev02=ev02.txt --affinity-ev03=ev03.txt \
  --out=result.csv

./vm-param-check --ht=on --cores=8 --numa=2 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 \
  --out=result.csv

./vm-param-check --ht=on --cores=8 --numa=2 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --shares-ev02=1000 --shares-ev03=1000 \
  --affinity-ev02=ev02.txt --affinity-ev03=ev03.txt
```

**구현 시 참고**: 이전 vCenter Go 작업(vm-lab-scripts/vm-create-optimized)에서 govmomi 연결 패턴, view.CreateContainerView 배치조회, ContainerView 활용법을 이미 검증해뒀으니 그 패턴 재사용 가능. vCenter 계정은 administrator@vsphere.local 또는 lscsystems@vsphere.local + VC_PASSWORD 사용.
