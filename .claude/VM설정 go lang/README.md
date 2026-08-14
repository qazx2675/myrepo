# VM 설정 일괄 적용 도구 (vm_affinity_bulk / vm_lpage_bulk / vm_create / vm_connect)

worklist(호스트 목록) 기반으로 각 호스트의 `ev01`~`ev03` VM에 설정을 적용하거나, VM 자체를 생성하거나, ESXi 호스트 자체를 vCenter에 등록하는 govmomi 기반 도구 4종.

- **`main_affinity.go`** → `vm_affinity_bulk` : CPU affinity 설정(`sched.vcpuN.affinity` 등 ExtraConfig) — **병렬(워커풀)**
- **`main_lpage.go`** → `vm_lpage_bulk` : HugePage/메모리 고정 설정 + CPU 토폴로지(소켓당 코어 수, NUMA 노드) — **병렬(워커풀)**
- **`main_vm_create.go`** → `vm_create` : BM 호스트별 EV01~EV03 VM을 동적 생성(디스크/NIC/펌웨어 설정) + 생성 후 CPU/메모리 예약·공유 설정 — **병렬(워커풀), v2**
- **`main_connect.go`** → `vm_connect` (Phase 1) : ESXi 호스트를 vCenter의 클러스터/폴더/데이터센터에 등록(`AddHost`/`AddStandaloneHost`), SSL 미신뢰(SSLVerifyFault) 시 thumbprint 자동 재시도 — **병렬(워커풀), v2**

이 디렉토리에는 **파일명이 다른 독립적인 단일 파일 프로그램 4개**가 들어 있다. 같은 디렉토리에 있지만 각자 `package main`이고 `main()` 함수를 가지고 있어서, **`go build .`(디렉토리 전체 빌드)는 사용할 수 없다** — 반드시 파일명을 직접 지정해서 빌드해야 한다.

## main_connect.go — 병렬화 + 버그 수정 (v2)

**전 과정이 병렬로 처리되도록 재작성됨.** v1과 비교:

| 구간 | v1 | v2 (현재) |
|---|---|---|
| 이미 등록된 호스트 여부 확인 | 순차 (호스트 수만큼 반복) | **워커풀**(`-concurrency`, 기본 20)로 병렬 확인. 결과는 인덱스별 슬라이스에 기록 후, worklist 순서 그대로 정리해서 출력(완료 순서로 뒤섞이지 않게) |
| 호스트 등록(`AddHost` 전송+대기) | 병렬이지만 **동시성 제한 없음**(전체를 한 번에 goroutine으로 실행) | 동일한 워커풀(`-concurrency`)로 **제한된 동시성**으로 실행 |

### ⚠️ v1 코드 리뷰 중 vcsim으로 실제 재현한 버그 2건 (v2에서 수정함)

v1은 `finder.HostSystem()`/`finder.ClusterComputeResource()`/`finder.Folder()`를 호출하기 전에 `finder.SetDatacenter()`를 **한 번도 호출하지 않았다.** govmomi의 `find.Finder`는 이런 검색에 데이터센터 컨텍스트(`f.dc`)가 필요한데, 이게 없으면 내부적으로 `"please specify a datacenter"` 에러가 나서 호출이 항상 실패한다. vcsim으로 재현한 실제 증상:

1. **"이미 등록됨" 체크가 항상 실패** — 이미 vCenter에 등록된 호스트도 매번 "미등록"으로 잘못 판정되어, 실행할 때마다 이미 등록된 호스트에 대해서도 불필요하게(그리고 실패할 가능성이 높은) 재등록을 시도했다.
2. **`-folderName`에 클러스터/폴더 이름을 넣으면 항상 실패** — 실제로 존재하는 클러스터(예: vcsim의 `DC0_C0`)를 지정해도 `[오류] vCenter 내에 'DC0_C0' 위치를 찾을 수 없습니다`로 실패했다. 결과적으로 `-folderName`은 사실상 데이터센터 이름을 넣었을 때만 동작했고(그 경로만 데이터센터 컨텍스트가 필요 없는 검색이라 우연히 동작함), 파라미터 이름이 뜻하는 "폴더/클러스터" 지정 용도로는 쓸 수 없었다.

**수정 방법**: `vm_create.go`와 동일한 패턴으로 `-datacenter` 플래그를 추가하고(데이터센터가 1개면 자동 선택, 여러 개면 필수), 데이터센터를 먼저 확정해서 `finder.SetDatacenter()`를 호출한 뒤에 클러스터/폴더/호스트 검색을 수행하도록 순서를 바꿨다. 병렬 사전 체크의 각 고루틴이 만드는 개별 `find.Finder` 인스턴스에도 각자 `SetDatacenter()`를 호출한다(Finder 인스턴스별로 상태가 분리되어 있어, 공유 finder의 SetDatacenter가 고루틴별 신규 Finder에 자동으로 반영되지 않기 때문).

### 검증 내용 (vcsim)

- **race detector** 빌드로 반복 실행, 데이터 레이스 없음 확인.
- 깨끗한 vcsim 인스턴스에서: 실제 존재하는 클러스터(`DC0_C0`)를 `-folderName`으로 지정 → 정상 감지됨(수정 전엔 실패).
- 깨끗한 vcsim 인스턴스에서: 이미 등록된 호스트 2대 + 신규 가짜 호스트 2대를 섞은 worklist로 실행 → 등록된 2대는 "이미 등록됨 (PASS)"로 정확히 걸러지고, 신규 2대만 등록 진행됨(수정 전엔 4대 전부 재등록 시도했음).
- `-concurrency 5`로 신규 호스트 5대를 클러스터 대상으로 동시 등록 → 전부 정상 완료, 레이스 없음.

### 옵션 (v2 신규/변경)

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-datacenter` | (없음) | 데이터센터 이름. 데이터센터가 1개뿐이면 생략 가능(자동 선택), 여러 개면 필수 |
| `-concurrency` | `20` | 등록 여부 확인 + 호스트 등록 전송+대기의 동시 처리 개수 제한 |
| `-folderName` | (필수) | 이제 **데이터센터가 확정된 상태에서** 그 안의 클러스터 또는 폴더 이름을 찾는다. 클러스터/폴더 둘 다 아니고 `-datacenter`(또는 자동 선택된 데이터센터)와 이름이 같으면 그 데이터센터의 기본 HostFolder를 사용 |

### 알려진 한계

- vcsim이 `AddHost`/`AddStandaloneHost`를 완전히 검증하지는 않는다(가짜 호스트명도 실패 없이 등록에 성공함) — 즉 **실제 ESXi 연결 실패, 잘못된 자격증명 등 vCenter가 실제로 반환하는 에러 상황은 vcsim으로 재현/검증할 수 없었다.** SSL thumbprint 자동 재시도 로직(`SSLVerifyFault` 처리) 자체도 vcsim에서 이 에러가 발생하지 않아 별도로 검증하지 못했다 — 코드 리뷰로만 확인함.

## main_vm_create.go — 병렬화 검토 결과 (v2)

**전 과정이 병렬로 처리되도록 재작성됨.** v1(순차 버전)과 비교하면:

| 구간 | v1 | v2 (현재) |
|---|---|---|
| VM/호스트 인벤토리 조회 | Finder로 재귀 순회 | **`ContainerView`로 1회 배치 조회** (VM 이름, 호스트+데이터스토어 목록) |
| 호스트별 데이터스토어 정보 조회 | 호스트×데이터스토어 중첩 순차 루프 | **호스트당 1회 배치 조회**(`property.Collector.Retrieve`) — 워커풀(`-prepConcurrency`, 기본 16)로 호스트 간 병렬 처리 |
| `CreateVM` 전송+대기 | 순차 | **워커풀**(`-taskConcurrency`, 기본 24) — 워커 하나가 VM 1대의 전송+Wait를 통째로 담당 |
| `Reconfigure` 전송+대기 | 순차 | **워커풀**(동일 `-taskConcurrency`) |
| 진행 상황 로그 | 없음(끝나고 일괄 출력) | 완료되는 즉시 `[N/전체]` 실시간 출력 |
| 전역 타임아웃 | 없음 | 60분 컨텍스트 타임아웃 추가 |

### 검증 내용 (vcsim)

1. **race detector** 빌드로 호스트 2대 × VM 2개(=4대), `-prepConcurrency 5 -taskConcurrency 5`로 실행 — **데이터 레이스 없음** 확인.
2. 생성된 VM의 실제 CPU/메모리 값을 `govc`로 직접 대조 — ev01(2vCPU/4GB)과 ev02(4vCPU/8GB)가 호스트 2대 모두 정확히 분리 적용됨, 교차 오염 없음 확인.
3. "생성 대상 VM 수"와 "리소스 설정 대상 VM 수"가 정확히 일치함을 확인 (생성된 VM이 재조회 실패 없이 전부 다음 단계로 이어짐).

### ⚠️ 테스트 중 발견한 기존 버그 (v1부터 존재, 이번 업로드에서 코드는 변경하지 않음)

`loadHostgroupMap`이 `hostgroup.txt`의 각 줄을 **공백 또는 콤마**(`[,\s]+`)로 분리한다. 그런데 **포트그룹(네트워크) 이름 자체에 공백이 들어가는 경우**(예: vSphere 기본 포트그룹명인 `"VM Network"`)가 실제로 매우 흔한데, 이 경우 `"BM호스트명 VM Network"` 줄이 `["BM호스트명", "VM", "Network"]` 3개 필드로 쪼개져서 **`fields[1]`인 `"VM"`만 네트워크 이름으로 잘못 사용된다.**

- vcsim으로 재현 테스트 결과: 존재하지 않는 네트워크 이름(`"VM"`)이 NIC의 `DeviceName`으로 전달되면서 **vcsim 시뮬레이터 자체가 nil 포인터 참조로 크래시**했다 (`simulator/virtual_machine.go:1300`, `ctx.Map.FindByName(...).Reference()`에서 조회 결과가 nil인데 바로 `.Reference()` 호출). 실제 vCenter는 vcsim처럼 프로세스가 죽지는 않고 API 에러를 반환할 가능성이 높지만(vcsim은 어디까지나 테스트 시뮬레이터), **어느 쪽이든 잘못된 네트워크 이름이 전달되어 해당 VM의 NIC 연결이 실패하거나 VM 생성 자체가 실패할 위험**은 동일하다.
- 재현 방법: `hostgroup.txt`에 공백이 포함된 포트그룹 이름(`"VM Network"`)을 넣고 실행하면 재현됨. 공백이 없는 이름(`"DC0_DVPG0"` 등)으로는 정상 동작.
- **요청에 따라 파서 로직(설정값 포함 기존 코드)은 이번 업로드에서 전혀 수정하지 않았다.** 실제 운영 환경의 `hostgroup.txt`에 공백이 포함된 포트그룹 이름이 등장할 가능성이 있다면 별도로 수정을 요청해달라.

### 옵션 (v2 신규)

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-prepConcurrency` | `16` | 호스트 사전 조사(데이터스토어/리소스풀 조회) 동시 처리 수 |
| `-taskConcurrency` | `24` | CreateVM/Reconfigure 전송+대기 동시 처리 수 |

기존 플래그(`-ev01Cpu` 등 VM 스펙, `-firmware`, `-vmCount` 등)의 기본값과 동작은 전혀 변경되지 않았다.

## 빌드 (다운로드 후 처음 할 일 — 오프라인 가능)

이 디렉토리에는 의존성(`govmomi`)을 미리 받아둔 **`vendor/` 디렉토리가 포함**되어 있다. 압축을 풀거나 clone한 직후 **인터넷 연결 없이 바로 빌드**할 수 있다.

```bash
cd ".claude/VM설정 go lang"

# affinity 도구
go build -mod=vendor -o vm_affinity_bulk main_affinity.go

# lpage(HugePage/CPU 토폴로지) 도구
go build -mod=vendor -o vm_lpage_bulk main_lpage.go
```

- `-mod=vendor`를 꼭 붙여야 `vendor/` 안의 의존성을 사용한다. 빠뜨리면 Go가 인터넷에서 재다운로드를 시도할 수 있다.
- **반드시 파일명(`main_affinity.go` / `main_lpage.go`)을 직접 지정**해서 빌드해야 한다. `go build .` 나 `go build *.go`를 쓰면 두 파일이 같은 패키지로 취급되어 `main redeclared`, `vmJob redeclared` 등의 컴파일 오류가 난다 (두 프로그램이 각자 동일한 이름의 헬퍼 타입/함수를 독립적으로 가지고 있기 때문 — 정상이다, 파일명을 지정해서 빌드하면 문제 없다).
- `go.mod`를 수정(의존성 버전 변경 등)했다면, 인터넷이 되는 환경에서 `go mod vendor`를 다시 실행해 `vendor/`를 갱신한 뒤 커밋해야 한다.

## 공통: 병렬 처리(워커풀) 방식

두 도구 모두 VM별로 `Reconfigure 전송 → 완료 대기(Wait)`를 **한 워커가 통째로 담당**하는 워커풀 구조다. `-concurrency` 플래그(기본값 **20**)로 동시에 처리할 VM 개수를 제한한다.

- 이전 버전(순차 방식)은 "전송을 다 하고 나서 하나씩 순서대로 대기"하는 구조라, VM이 수백~수천 대 규모(예: 호스트 700~800대 × VM 2개 = 1,400~1,600대)일 경우 API 왕복이 순차적으로 누적되어 수 분~십수 분까지 걸릴 수 있었다.
- 현재 버전은 `-concurrency`개의 고루틴이 동시에 각자 담당한 VM의 전송+대기를 수행하므로, 이 구간의 소요 시간이 크게 단축된다.
- `-concurrency` 값을 낮추면(예: vCenter 부하가 걱정될 때) 동시 요청 수를 줄일 수 있고, 높이면(네트워크/서버 여유가 충분할 때) 더 빠르게 처리할 수 있다.
- 출력 로그는 여러 고루틴이 동시에 쓰기 때문에 완료 순서가 뒤섞여 나올 수 있다 (정상 동작 — 각 줄 자체는 안전하게 보호되어 깨지지 않는다).

## vm_affinity_bulk 사용법

```bash
export VC_PASSWORD='실제_비밀번호'

# ev01만
./vm_affinity_bulk -vcTargetIP 192.168.0.50 -vm_cnt 1 \
  -affinityFile01 affinity_ev01.txt

# ev01~ev03 전체, 동시 처리 30개로 조정
./vm_affinity_bulk -vcTargetIP 192.168.0.50 -vm_cnt 3 \
  -id administrator@vsphere.local \
  -affinityFile01 affinity_ev01.txt \
  -affinityFile02 affinity_ev02.txt \
  -affinityFile03 /etc/vmcfg/affinity_ev03.txt \
  -concurrency 30
```

### 옵션

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-vcTargetIP` | (필수) | vCenter 접속 IP |
| `-worklistFile` | `worklist.txt` | 대상 호스트 목록 (한 줄에 호스트 하나) |
| `-vm_cnt` | `2` | 호스트당 대상 VM 개수 (1=ev01, 2=ev01~ev02, 3=ev01~ev03) |
| `-affinityFile01/02/03` | (vm_cnt에 따라 필수) | 각 ev0N 슬롯에 적용할 affinity 설정 파일 |
| `-affinityFile` | (없음) | [구버전 호환] `-affinityFile02` 미지정 시 이 값을 ev02 설정으로 사용 |
| `-ht` | `ON` | 설정 파일 내용이 `AUTO`일 때만 사용. `sched.vcpuN.affinity` 값을 HT ON은 `N*2,N*2+1`, OFF는 `N` 형태로 생성 |
| `-concurrency` | **20** | 동시 처리(전송+대기) VM 개수 제한 |

환경 변수 `VC_PASSWORD`가 반드시 설정되어 있어야 한다 (없으면 즉시 종료).

### 설정 파일 형식

- `key=value` 한 줄씩. 예: `sched.vcpu0.affinity=0,1`
- 값(과 키)을 큰따옴표(`"`) 또는 작은따옴표(`'`)로 감싸도 자동으로 제거된 뒤 적용된다 (`stripQuotes`).
  예: `sched.vcpu0.affinity="0,1"` → 실제 적용값은 `0,1` (따옴표 제거됨)
- 파일 전체가 `AUTO` 한 줄뿐이면, vCPU 수를 조회해서 `-ht` 옵션에 따라 1:1 매핑을 자동 생성한다.
- `#`으로 시작하는 줄은 주석으로 무시된다.
- `key=value` 형식이 아니거나 key가 비어 있으면 프로그램 시작 시점에 즉시 오류로 종료된다.

### 동작 방식

1. `-affinityFile0N` 파일들을 파싱(`loadAffinitySpec`) — 형식 오류 시 여기서 즉시 종료
2. `worklistFile`의 각 호스트에 `ev01`~`ev0N` 접미사를 붙여 대상 VM 이름 목록 생성
3. vCenter 전체 데이터센터를 **동시에**(`-concurrency` 제한) 순회하며 이름이 일치하는 VM을 찾음 (없으면 "PASS"로 건너뜀)
4. 워커풀이 VM마다 `Reconfigure` 전송 + `Wait` 완료 대기를 병렬로 수행
5. Task가 성공한 VM만 `config.extraConfig`를 재조회(단일 배치 호출)해서 실제로 보낸 값과 일치하는지 검증
6. 최종 결과를 "성공/실패/스킵/적용불일치" 건수로 요약 출력, 문제가 있으면 종료 코드 2

## vm_lpage_bulk 사용법

```bash
export VC_PASSWORD='실제_비밀번호'

./vm_lpage_bulk -vcTargetIP 192.168.0.50 \
  -id administrator@vsphere.local \
  -ev01Cores 8 -ev01Sockets 2 -ev01Numa 4 \
  -ev02Cores 4 -ev02Sockets 1 -ev02Numa 2 \
  -concurrency 20
```

### 옵션

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-vcTargetIP` | (필수) | vCenter 접속 IP |
| `-worklistFile` | `worklist.txt` | 대상 호스트 목록 |
| `-ev01Cores` / `-ev01Sockets` | (필수) | ev01 vCPU 수 / 소켓 수 (코어 수가 소켓 수로 나누어떨어져야 함) |
| `-ev02Cores` / `-ev02Sockets` | (필수) | ev02 vCPU 수 / 소켓 수 |
| `-ev01Numa` / `-ev02Numa` | `0` (미적용) | NUMA 노드 수. 지정 시 코어 수가 이 값으로 나누어떨어져야 함 |
| `-applyTopology` | `true` | vCPU/소켓당 코어/NUMA 토폴로지를 실제로 적용할지 여부 (false면 ExtraConfig만 적용) |
| `-concurrency` | **20** | 동시 처리(전송+대기) VM 개수 제한 |

### 적용되는 설정

모든 대상 VM에 공통으로 다음 ExtraConfig가 적용된다:

| 키 | 값 | 의미 |
|---|---|---|
| `sched.mem.lpage.enable1GPage` | `TRUE` | 1GB HugePage 사용 |
| `sched.mem.pin` | `TRUE` | 메모리 고정 |
| `sched.mem.prealloc` | `TRUE` | 메모리 사전 할당 |
| `sched.mem.prealloc.pinnedMainMem` | `TRUE` | 고정 메모리 사전 할당 |
| `sched.swap.vmxSwapEnabled` | `FALSE` | VMX 스왑 비활성화 |
| `numa.vcpu.maxPerVirtualNode` | **소켓당 코어 수** | (NUMA 노드당 코어 수가 아니라 **소켓당 코어 수**를 의도적으로 사용하도록 정해짐) |

`-applyTopology`가 true(기본값)면 추가로 `config.hardware.numCPU`, `config.hardware.numCoresPerSocket`, (NUMA 노드 수 지정 시) `VirtualNuma.CoresPerNumaNode`가 VM 하드웨어 설정에 직접 반영된다.

### 검증(입력값 사전 체크)

vCenter에 접속하기 전에 아래 조건을 검증하고, 위반 시 즉시 종료한다:

- 코어 수가 소켓 수로 나누어떨어지지 않음: `[ev0N] 코어 수(X)가 소켓 수(Y)로 나누어떨어지지 않습니다.`
- 코어 수가 NUMA 노드 수로 나누어떨어지지 않음: `[ev0N] 코어 수(X)가 NUMA 노드 수(Y)로 나누어떨어지지 않습니다.`

## vm_create 사용법

BM(물리) 호스트별로 EV01~EV03 VM을 동적으로 생성(SCSI/디스크/NIC 구성 + 펌웨어 선택)하고, 생성 직후 CPU/메모리 예약·공유(Shares) 설정까지 이어서 적용한다.

```bash
export VC_PASSWORD='실제_비밀번호'

./vm_create -vcTargetIP 192.168.0.50 \
  -id administrator@vsphere.local \
  -folderName MyCluster \
  -vmCount 2 \
  -ev01Cpu 2 -ev01Mem 4 -ev01Disk 40 -ev01Share 1000 \
  -ev02Cpu 4 -ev02Mem 8 -ev02Disk 60 -ev02Share 2000 \
  -mapFile hostgroup.txt \
  -firmware efi \
  -prepConcurrency 16 -taskConcurrency 24
```

### 옵션

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-vcTargetIP` | (필수) | vCenter 접속 IP |
| `-worklistFile` | `worklist.txt` | 대상 BM 호스트 목록 (한 줄에 호스트 하나, VM을 실제로 붙일 ESXi 호스트) |
| `-vmCount` | `2` | 호스트당 생성할 VM 개수 (1~3, ev01~ev0N) |
| `-mapFile` | `hostgroup.txt` | `"BM호스트명 포트그룹이름"` 형식의 네트워크 매핑 파일 (공백/콤마 구분자, 포트그룹 이름에 공백이 있으면 오작동하니 주의 — 아래 "알려진 문제" 참고) |
| `-firmware` | `efi` | `bios` 또는 `efi` |
| `-datacenter` | (없음) | 데이터센터 이름. 1개뿐이면 생략 가능(자동 선택), 여러 개면 필수 |
| `-ev01Cpu/Mem/Disk/Share` | (필수, 단 ev01/ev02) | ev01 vCPU 수 / 메모리(GB) / 디스크(GB) / CPU·메모리 Shares 값 |
| `-ev02Cpu/Mem/Disk/Share` | (필수) | ev02 스펙 |
| `-ev03Cpu/Mem/Disk/Share` | `1`/`1`/`20`/`1000` | ev03 스펙 (vmCount=3일 때만 사용) |
| `-prepConcurrency` | **16** | 호스트 사전 조사(데이터스토어/리소스풀 조회) 동시 처리 수 |
| `-taskConcurrency` | **24** | `CreateVM`/`Reconfigure` 전송+대기 동시 처리 수 |

환경 변수 `VC_PASSWORD`가 반드시 설정되어 있어야 한다 (없으면 즉시 종료).

### 동작 방식

1. VM/호스트 인벤토리를 `ContainerView`로 1회 배치 조회
2. worklist의 각 BM 호스트를 `-prepConcurrency` 워커풀로 병렬 조사 — 여유 공간이 가장 큰 데이터스토어 선택, 리소스풀 확인, `mapFile`에서 네트워크(포트그룹) 매핑
3. 아직 존재하지 않는 VM(`ev01`~`ev0N`)만 골라 `-taskConcurrency` 워커풀로 `CreateVM` 전송+대기 (SCSI 컨트롤러 Paravirtual, 디스크 Thick Lazy Zeroed, NIC vmxnet3)
4. 생성된 VM에 대해 다시 `-taskConcurrency` 워커풀로 `Reconfigure` 전송+대기 — CPU/메모리 Shares(Custom), 메모리 예약(`MemoryReservationLockedToMax`), 부팅 순서(디스크→NIC), `sched.mem.pin` 등 ExtraConfig 적용
5. "생성 대상 VM 수"와 "리소스 설정 대상 VM 수"를 출력 — 두 숫자가 다르면 일부 VM이 생성은 됐지만 재조회에 실패했거나(드묾) `CreateVM` 자체가 실패한 것이므로 로그에서 원인 확인 필요

### 알려진 문제

`loadHostgroupMap`이 `mapFile`의 각 줄을 **공백 또는 콤마**로 분리하기 때문에, 포트그룹 이름 자체에 공백이 들어가면(예: vSphere 기본 포트그룹명 `"VM Network"`) 잘못 잘려서 엉뚱한 네트워크 이름이 전달된다. 공백 없는 포트그룹 이름(예: 분산 포트그룹)을 쓰거나, 이 문제가 실제로 걸리면 파서 수정을 요청해달라.

## vm_connect 사용법

worklist에 있는 ESXi 호스트들을 vCenter의 지정한 클러스터/폴더에 병렬로 등록한다. SSL 인증서가 미신뢰 상태(자가서명 인증서, 폐쇄망 환경 등)면 `SSLVerifyFault`에서 실제 thumbprint를 꺼내 자동으로 한 번 재시도한다.

```bash
export VC_PASSWORD='실제_비밀번호'
export ESXI_PASSWORD='ESXi_root_비밀번호'

# 클러스터에 등록
./vm_connect -vcTargetIP 192.168.0.50 \
  -id administrator@vsphere.local \
  -folderName MyCluster \
  -concurrency 20

# 데이터센터가 여러 개일 때는 -datacenter로 명시
./vm_connect -vcTargetIP 192.168.0.50 \
  -folderName MyCluster -datacenter DC01 \
  -worklistFile esxi_hosts.txt
```

### 옵션

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-vcTargetIP` | (필수) | vCenter 접속 IP |
| `-folderName` | (필수) | 데이터센터 내 클러스터 또는 폴더 이름. 데이터센터 이름과 같으면 그 데이터센터의 기본 HostFolder 사용 |
| `-datacenter` | (없음) | 데이터센터 이름. 1개뿐이면 생략 가능(자동 선택), 여러 개면 필수 |
| `-worklistFile` | `worklist.txt` | 등록할 ESXi 호스트 목록 (한 줄에 하나, IP 또는 FQDN) |
| `-concurrency` | **20** | "이미 등록됨" 확인 + 호스트 등록 전송+대기의 동시 처리 개수 제한 |

환경 변수 `VC_PASSWORD`(vCenter 로그인)와 `ESXI_PASSWORD`(각 ESXi 호스트의 root 비밀번호 — 모든 대상 호스트가 동일한 root 비밀번호를 쓴다고 가정)가 반드시 설정되어 있어야 한다.

### 동작 방식

1. `-datacenter`(또는 자동 선택된 유일한 데이터센터)로 데이터센터 컨텍스트를 먼저 확정
2. `-folderName`으로 클러스터 → 폴더 → (데이터센터 자체 이름과 일치 시) 기본 HostFolder 순으로 대상 위치 탐색
3. worklist의 각 호스트가 이미 vCenter에 등록되어 있는지 `-concurrency` 워커풀로 병렬 확인 — 이미 있으면 "PASS", 없으면 등록 대상에 포함
4. 등록 대상 호스트를 `-concurrency` 워커풀로 병렬 등록 (`AddHost`/`AddStandaloneHost` 전송 + 완료 대기) — `root` 계정, `-force` 강제 등록
5. `SSLVerifyFault` 발생 시 응답에 담긴 실제 thumbprint를 `spec.SslThumbprint`에 채워 자동 재시도
6. 성공/실패 호스트를 요약 출력

## 실패 시 나오는 메시지 (원인별, 공통)

| 상황 | 메시지 예 | 발생 시점 |
|---|---|---|
| 설정 파일이 없음 (affinity 도구) | `ev01 파일을 찾을 수 없습니다: <경로>` | 시작 직후 (vCenter 접속 전) |
| 파일이 `key=value` 형식이 아님 (affinity 도구) | `ev01 설정 파일 오류 (<경로>): N번째 줄 형식 오류 (key=value 아님): <내용>` | 시작 직후 |
| 코어/소켓/NUMA 값이 나누어떨어지지 않음 (lpage 도구) | `[ev01] 코어 수(5)가 소켓 수(2)로 나누어떨어지지 않습니다.` | 시작 직후 (vCenter 접속 전) |
| VC_PASSWORD 미설정 | `인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.` | 시작 직후 |
| vCenter 접속 실패 | `vCenter 접속 실패: <상세>` / `작업 중 오류 발생: <상세>` | 접속 시도 시 |
| worklist와 매칭되는 VM이 vCenter에 하나도 없음 | `worklist 와 매칭되는 VM 을 vCenter 에서 찾지 못했습니다.` | VM 조회 후 |
| 특정 VM이 vCenter에 없음 | `[호스트명ev0N] 경고: 대상 VM 이 존재하지 않습니다. (PASS)` | VM별 처리 중 (해당 VM만 스킵, 전체는 계속 진행) |
| Reconfigure 명령 자체가 거부됨 | `[VM명] Reconfigure 명령 전송 실패: <상세>` / `설정 요청 실패: <상세>` | Task 전송 시 |
| Task는 전송됐지만 vCenter에서 실패 처리됨 | `[VM명] 작업 실패: <상세>` | Task 완료 대기 중 |
| (affinity 도구만) 실제 반영값이 보낸 값과 다름 | `[VM명] 실제 적용 불일치: key(기대=X,실제=Y)` | 재조회 검증 시 |

`ExtraConfig`는 VMX의 자유 형식 key-value 저장소라서, **존재하지 않는/의미 없는 키를 넣어도 vCenter/ESXi 단에서 형식 자체를 거부하지는 않는다.** "없는 설정"을 넣었을 때 나는 실패는 대부분 **① 파일 파싱 단계(형식 오류)** 이거나, **② 재조회 검증 단계(값 불일치, affinity 도구만 해당)** 이지, "그런 설정 키는 없다"는 형태의 vCenter API 에러가 아니다.
