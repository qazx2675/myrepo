# VM 설정 일괄 적용 도구 (vm_affinity_bulk / vm_lpage_bulk / vm_create / vm_connect)

worklist(호스트 목록) 기반으로 각 호스트의 `ev01`~`ev03` VM에 설정을 적용하거나, VM 자체를 생성하거나, ESXi 호스트 자체를 vCenter에 등록하는 govmomi 기반 도구 4종.

- **`main_affinity.go`** → `vm_affinity_bulk` : CPU affinity 설정(`sched.vcpuN.affinity` 등 ExtraConfig) — **병렬(워커풀)**
- **`main_lpage.go`** → `vm_lpage_bulk` : HugePage/메모리 고정 설정 + CPU 토폴로지(소켓당 코어 수, NUMA 노드) — **병렬(워커풀)**
- **`main_vm_create.go`** → `vm_create` : BM 호스트별 EV01~EV03 VM을 동적 생성(디스크/NIC/펌웨어 설정) + 생성 후 CPU/메모리 예약·공유 설정 — **병렬(워커풀), v2**
- **`main_connect.go`** → `vm_connect` (Phase 1) : ESXi 호스트를 vCenter의 클러스터/폴더/데이터센터에 등록(`AddHost`/`AddStandaloneHost`), SSL 미신뢰(SSLVerifyFault) 시 thumbprint 자동 재시도 — **병렬(워커풀), v2**

이 디렉토리에는 **파일명이 다른 독립적인 단일 파일 프로그램 4개**가 들어 있다. 같은 디렉토리에 있지만 각자 `package main`이고 `main()` 함수를 가지고 있어서, **`go build .`(디렉토리 전체 빌드)는 사용할 수 없다** — 반드시 파일명을 직접 지정해서 빌드해야 한다.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

이 디렉토리에는 의존성(`govmomi`)을 미리 받아둔 **`vendor/` 디렉토리가 포함**되어 있다. 압축을 풀거나 clone한 직후 **인터넷 연결 없이 바로 빌드**할 수 있다.

```bash
cd ".claude/VM/vm-setting-go-lang"
bash setup.sh
```

- `setup.sh` 스크립트를 실행하면 내부적으로 `-mod=vendor` 플래그를 사용하여 `vendor/` 안의 의존성을 사용한다. 빠뜨리면 Go가 인터넷에서 재다운로드를 시도할 수 있다.
- **반드시 파일명을 직접 지정**해서 빌드해야 한다. `go build .` 나 `go build *.go`를 쓰면 두 파일이 같은 패키지로 취급되어 컴파일 오류가 난다.
- `go.mod`를 수정(의존성 버전 변경 등)했다면, 인터넷이 되는 환경에서 `go mod vendor`를 다시 실행해 `vendor/`를 갱신한 뒤 커밋해야 한다.

### 전역 명령어로 사용하기 (선택 사항)
빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 이동하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 사용할 수 있습니다.

예시 (실행 파일을 `/usr/local/bin`으로 복사):
```bash
sudo cp vm_affinity_bulk vm_lpage_bulk vm_create vm_connect /usr/local/bin/
# 이후 어느 위치에서나 명령어처럼 실행 가능
```

## 2. 사용 방법

### 2.1 공통: 병렬 처리(워커풀) 방식
두 도구 모두 VM별로 `Reconfigure 전송 → 완료 대기(Wait)`를 **한 워커가 통째로 담당**하는 워커풀 구조다. `-concurrency` 플래그(기본값 **20**)로 동시에 처리할 VM 개수를 제한한다.
- 이전 버전(순차 방식)은 수 분~십수 분까지 걸릴 수 있었으나, 현재 버전은 소요 시간이 크게 단축된다.
- 출력 로그는 여러 고루틴이 동시에 쓰기 때문에 완료 순서가 뒤섞여 나올 수 있다 (정상 동작 — 각 줄 자체는 안전하게 보호되어 깨지지 않는다).

### 2.2 vm_affinity_bulk 사용법
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

### 2.3 vm_lpage_bulk 사용법
```bash
export VC_PASSWORD='실제_비밀번호'

./vm_lpage_bulk -vcTargetIP 192.168.0.50 \
  -id administrator@vsphere.local \
  -ev01Cores 8 -ev01Sockets 2 -ev01Numa 4 \
  -ev02Cores 4 -ev02Sockets 1 -ev02Numa 2 \
  -concurrency 20
```

### 2.4 vm_create 사용법
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

### 2.5 vm_connect 사용법
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

## 3. 옵션별 상세 설명

### 3.1 vm_affinity_bulk 옵션
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

### 3.2 vm_lpage_bulk 옵션
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

### 3.3 vm_create 옵션
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

### 3.4 vm_connect 옵션 (v2 신규/변경)
| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-vcTargetIP` | (필수) | vCenter 접속 IP |
| `-folderName` | (필수) | 이제 **데이터센터가 확정된 상태에서** 그 안의 클러스터 또는 폴더 이름을 찾는다. 클러스터/폴더 둘 다 아니고 `-datacenter`(또는 자동 선택된 데이터센터)와 이름이 같으면 그 데이터센터의 기본 HostFolder를 사용 |
| `-datacenter` | (없음) | 데이터센터 이름. 1개뿐이면 생략 가능(자동 선택), 여러 개면 필수 |
| `-worklistFile` | `worklist.txt` | 등록할 ESXi 호스트 목록 (한 줄에 하나, IP 또는 FQDN) |
| `-concurrency` | **20** | "이미 등록됨" 확인 + 호스트 등록 전송+대기의 동시 처리 개수 제한 |

## 4. 문서별 고유 설명

### 4.1 main_connect.go — 병렬화 + 버그 수정 (v2)
**전 과정이 병렬로 처리되도록 재작성됨.** v1과 비교:
| 구간 | v1 | v2 (현재) |
|---|---|---|
| 이미 등록된 호스트 여부 확인 | 순차 (호스트 수만큼 반복) | **워커풀**(`-concurrency`, 기본 20)로 병렬 확인. 결과는 인덱스별 슬라이스에 기록 후, worklist 순서 그대로 정리해서 출력(완료 순서로 뒤섞이지 않게) |
| 호스트 등록(`AddHost` 전송+대기) | 병렬이지만 **동시성 제한 없음**(전체를 한 번에 goroutine으로 실행) | 동일한 워커풀(`-concurrency`)로 **제한된 동시성**으로 실행 |

#### ⚠️ v1 코드 리뷰 중 vcsim으로 실제 재현한 버그 2건 (v2에서 수정함)
v1은 `finder.HostSystem()`/`finder.ClusterComputeResource()`/`finder.Folder()`를 호출하기 전에 `finder.SetDatacenter()`를 **한 번도 호출하지 않았다.** govmomi의 `find.Finder`는 이런 검색에 데이터센터 컨텍스트(`f.dc`)가 필요한데, 이게 없으면 내부적으로 `"please specify a datacenter"` 에러가 나서 호출이 항상 실패한다. 
- **"이미 등록됨" 체크가 항상 실패**, **`-folderName`에 클러스터/폴더 이름을 넣으면 항상 실패** 등의 증상이 있었다.

#### 알려진 한계
vcsim이 `AddHost`/`AddStandaloneHost`를 완전히 검증하지는 않는다(가짜 호스트명도 실패 없이 등록에 성공함) — 즉 **실제 ESXi 연결 실패, 잘못된 자격증명 등 vCenter가 실제로 반환하는 에러 상황은 vcsim으로 재현/검증할 수 없었다.** 

### 4.2 main_vm_create.go — 병렬화 검토 결과 (v2)
**전 과정이 병렬로 처리되도록 재작성됨.** v1(순차 버전)과 비교하면:
| 구간 | v1 | v2 (현재) |
|---|---|---|
| VM/호스트 인벤토리 조회 | Finder로 재귀 순회 | **`ContainerView`로 1회 배치 조회** (VM 이름, 호스트+데이터스토어 목록) |
| 호스트별 데이터스토어 정보 조회 | 호스트×데이터스토어 중첩 순차 루프 | **호스트당 1회 배치 조회**(`property.Collector.Retrieve`) — 워커풀(`-prepConcurrency`, 기본 16)로 호스트 간 병렬 처리 |
| `CreateVM` / `Reconfigure` 전송+대기 | 순차 | **워커풀**(`-taskConcurrency`, 기본 24) — 워커 하나가 VM 1대의 전송+Wait를 통째로 담당 |
| 진행 상황 로그 | 없음(끝나고 일괄 출력) | 완료되는 즉시 `[N/전체]` 실시간 출력 |
| 전역 타임아웃 | 없음 | 60분 컨텍스트 타임아웃 추가 |

#### ⚠️ 테스트 중 발견한 기존 버그 (v1부터 존재, 이번 업로드에서 코드는 변경하지 않음)
`loadHostgroupMap`이 `hostgroup.txt`의 각 줄을 **공백 또는 콤마**(`[,\s]+`)로 분리한다. 그런데 **포트그룹(네트워크) 이름 자체에 공백이 들어가는 경우**(예: vSphere 기본 포트그룹명인 `"VM Network"`)가 실제로 매우 흔한데, 이 경우 잘못 파싱된다. 이 문제는 현재 버전에 남아있으므로 주의.

### 4.3 실패 시 나오는 메시지 (원인별, 공통)
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
