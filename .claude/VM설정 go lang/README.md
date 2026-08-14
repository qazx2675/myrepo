# VM 설정 일괄 적용 도구 (vm_affinity_bulk / vm_lpage_bulk / vm_create)

worklist(호스트 목록) 기반으로 각 호스트의 `ev01`~`ev03` VM에 설정을 적용하거나 VM 자체를 생성하는 govmomi 기반 도구 3종.

- **`main_affinity.go`** → `vm_affinity_bulk` : CPU affinity 설정(`sched.vcpuN.affinity` 등 ExtraConfig) — **병렬(워커풀)**
- **`main_lpage.go`** → `vm_lpage_bulk` : HugePage/메모리 고정 설정 + CPU 토폴로지(소켓당 코어 수, NUMA 노드) — **병렬(워커풀)**
- **`main_vm_create.go`** → `vm_create` : BM 호스트별 EV01~EV03 VM을 동적 생성(디스크/NIC/펌웨어 설정) + 생성 후 CPU/메모리 예약·공유 설정 — **순차(sequential) 방식** (아래 "병렬화 대상에서 제외" 참고)

이 디렉토리에는 **파일명이 다른 독립적인 단일 파일 프로그램 3개**가 들어 있다. 같은 디렉토리에 있지만 각자 `package main`이고 `main()` 함수를 가지고 있어서, **`go build .`(디렉토리 전체 빌드)는 사용할 수 없다** — 반드시 파일명을 직접 지정해서 빌드해야 한다.

## main_vm_create.go — 병렬화 대상에서 제외

`vm_affinity_bulk`/`vm_lpage_bulk`와 달리 **`vm_vm_create`는 병렬화하지 않고 원본 로직을 그대로 유지**했다 (요청에 따라 기존 설정값/동작을 변경하지 않음). 참고로 분석한 병렬성 현황은 다음과 같다 — **전 과정이 순차(sequential)** 이며, 앞선 두 도구보다도 순차 API 호출 지점이 많다:

| 구간 | 방식 | 비고 |
|---|---|---|
| 호스트별 `HostSystem` 조회 | 순차 | 호스트 수만큼 반복 |
| 호스트별 `Properties(datastore)` 조회 | 순차 | 호스트 수만큼 반복 |
| **호스트×데이터스토어별 `Properties(summary)` 조회** | **순차, 중첩 루프** | 호스트 수 × 데이터스토어 수만큼 반복 — 가장 API 호출이 많이 누적되는 지점 |
| 호스트별 `ResourcePool`/`DefaultFolder` 조회 | 순차 | 호스트 수만큼 반복 (`DefaultFolder`는 매번 동일한 값이라 사실상 중복 호출) |
| `CreateVM` Task 전송 | 순차 | VM 수만큼 반복 (응답만 받고 안 기다림) |
| 생성 Task `Wait` | 순차 | VM 수만큼 반복 |
| Reconfigure 대상 `finder.VirtualMachine(name)` 조회 | 순차 | VM 수만큼 반복 (이름 기반 개별 검색) |
| `Reconfigure` Task 전송 | 순차 | VM 수만큼 반복 |
| Reconfigure Task `Wait` | 순차 | VM 수만큼 반복 |

대규모(호스트 수백 대) 실행 시 `vm_affinity_bulk`/`vm_lpage_bulk`의 이전(v1) 순차 버전보다도 더 많은 순차 API 호출이 발생할 수 있다 (특히 호스트×데이터스토어 중첩 루프). 병렬화가 필요하면 별도로 요청해달라 — 기존 설정값(플래그 기본값, VM 스펙 로직 등)은 이번 업로드에서 전혀 변경하지 않았다.

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
