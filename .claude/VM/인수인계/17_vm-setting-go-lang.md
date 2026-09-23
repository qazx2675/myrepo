# 17. vm-setting-go-lang — 설정 적용 / VM 생성 / 호스트 등록 도구 4종

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🔴 **실제 vCenter 설정을 변경하고 VM/호스트를 생성·등록합니다** |
| 폴더 | `.claude/VM/vm-setting-go-lang/` |
| 바이너리 | `vm_affinity_bulk`, `vm_lpage_bulk`, `vm_create`, `vm_connect` (4개) |
| 인증 | 계정은 `-id` 플래그, 비밀번호는 `VC_PASSWORD` (+ `vm_connect`는 `ESXI_PASSWORD`도) |
| 특이점 | **한 폴더에 `main()`이 4개.** `go build .`는 실패합니다 |

---

## 1. 이 폴더는 무엇인가

`worklist`(호스트 목록) 기반으로 각 호스트의 `ev01`~`ev03` VM에 설정을 적용하거나, VM 자체를 생성하거나, ESXi 호스트를 vCenter에 등록하는 govmomi 기반 도구 4종입니다.

| 소스 파일 | 바이너리 | 하는 일 |
|---|---|---|
| `main_affinity.go` | `vm_affinity_bulk` | CPU affinity(`sched.vcpuN.affinity` 등 ExtraConfig) 설정 — 병렬(워커풀) |
| `main_lpage.go` | `vm_lpage_bulk` | HugePage/메모리 고정 + CPU 토폴로지(소켓당 코어 수, NUMA 노드) — 병렬 |
| `main_vm_create.go` | `vm_create` | BM 호스트별 EV01~EV03 VM 동적 생성 + CPU/메모리 예약·공유 설정 — 병렬, **v2** |
| `main_connect.go` | `vm_connect` | ESXi 호스트를 vCenter 클러스터/폴더/데이터센터에 등록(`AddHost`) — 병렬, **v2** |

> **`VM_setup/`과 기능이 겹칩니다.** 두 폴더 모두 affinity/lpage/vm_create를 갖고 있습니다. 차이는 이 폴더가 **병렬 처리(워커풀)와 v2 개선**을 반영한 버전이라는 점입니다. 어느 쪽을 쓸지는 인수 시 팀 관행을 확인하세요. 새로 시작한다면 설정 점검·교정은 [11번](./11_vm-param-check-usability-improvement.md)이 가장 낫습니다.

---

## 2. 빌드 (주의)

**이 폴더에는 파일명이 다른 독립적인 단일 파일 프로그램 4개가 들어 있습니다.** 같은 디렉토리에 있지만 각자 `package main`이고 `main()` 함수를 가지고 있어서:

```bash
go build .        # ❌ 실패 — 두 개 이상의 main() 충돌
go build *.go     # ❌ 실패
bash setup.sh     # ✅ 파일명을 지정해 4개를 각각 빌드
```

```bash
cd .claude/VM/vm-setting-go-lang
bash setup.sh
```

전역 명령어로 쓰려면:

```bash
sudo cp vm_affinity_bulk vm_lpage_bulk vm_create vm_connect /usr/local/bin/
```

> `go.mod`를 수정(의존성 버전 변경 등)했다면 인터넷 되는 환경에서 `go mod vendor`를 다시 실행해 `vendor/`를 갱신한 뒤 커밋해야 합니다.

---

## 3. 공통: 병렬 처리 방식

모든 도구가 **VM별로 `Reconfigure 전송 → 완료 대기(Wait)`를 한 워커가 통째로 담당**하는 워커풀 구조입니다.

- `vm_affinity_bulk` / `vm_connect` 는 `-concurrency`(기본 **20**), `vm_create` 는 `-prepConcurrency`(16) / `-taskConcurrency`(24)로 동시 처리 개수를 제한합니다.
- ⚠️ **`vm_lpage_bulk` 에는 `-concurrency` 플래그가 없습니다** (아래 5절 참고).
- 이전 순차 버전은 수 분~십수 분까지 걸렸지만 현재는 크게 단축됩니다.
- **출력 로그의 완료 순서가 뒤섞여 나올 수 있습니다** — 정상 동작입니다. 각 줄 자체는 깨지지 않습니다.

---

## 4. vm_affinity_bulk — CPU affinity 설정 🔴

```bash
export VC_PASSWORD='실제_비밀번호'

# ev01만
./vm_affinity_bulk -vcTargetIP 192.168.0.50 -vm_cnt 1 -affinityFile01 affinity_ev01.txt

# ev01~ev03 전체, 동시 처리 30개
./vm_affinity_bulk -vcTargetIP 192.168.0.50 -vm_cnt 3 \
  -id administrator@vsphere.local \
  -affinityFile01 affinity_ev01.txt \
  -affinityFile02 affinity_ev02.txt \
  -affinityFile03 /etc/vmcfg/affinity_ev03.txt \
  -concurrency 30
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-vcTargetIP` | (필수) | vCenter 접속 IP |
| `-worklistFile` | `worklist.txt` | 대상 호스트 목록 (한 줄에 호스트 하나) |
| `-vm_cnt` | `2` | 호스트당 대상 VM 개수 (1=ev01, 2=ev01~ev02, 3=ev01~ev03) |
| `-affinityFile01/02/03` | (`vm_cnt`에 따라 필수) | 각 ev0N 슬롯에 적용할 affinity 설정 파일 |
| `-affinityFile` | (없음) | **[구버전 호환]** `-affinityFile02` 미지정 시 ev02 설정으로 사용 |
| `-ht <ON\|OFF>` | `ON` | 설정 파일 내용이 `AUTO`일 때만 사용. ON은 `N*2,N*2+1`, OFF는 `N` 형태로 생성 |
| `-concurrency <N>` | `20` | 동시 처리(전송+대기) VM 개수 제한 |

---

## 5. vm_lpage_bulk — HugePage/CPU 토폴로지 설정 🔴

```bash
export VC_PASSWORD='실제_비밀번호'

./vm_lpage_bulk -vcTargetIP 192.168.0.50 -id administrator@vsphere.local \
  -ev01Cores 8 -ev01Sockets 2 -ev01Numa 4 \
  -ev02Cores 4 -ev02Sockets 1 -ev02Numa 2
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-vcTargetIP` | (필수) | vCenter 접속 IP |
| `-worklistFile` | `worklist.txt` | 대상 호스트 목록 |
| `-ev01Cores` / `-ev01Sockets` | `0` (필수) | ev01 코어 수 / 소켓 수. **코어 수가 소켓 수로 나누어떨어져야 함** |
| `-ev02Cores` / `-ev02Sockets` | `0` (필수) | ev02 코어 수 / 소켓 수 |
| `-ev01Numa` / `-ev02Numa` | `0` (미적용) | NUMA 노드 수. 지정 시 코어 수가 이 값으로도 나누어떨어져야 함 |
| `-applyTopology` | `true` | CPU 토폴로지를 실제로 적용할지. `false`면 ExtraConfig만 적용 |

> ⚠️ **원본 README와 다른 점 (소스 확인 결과)**
> `vm-setting-go-lang/README.md`는 이 도구에 `-concurrency`(기본 20) 옵션이 있다고 적고 있지만,
> **`main_lpage.go`에는 해당 플래그가 정의되어 있지 않습니다.** 주면 `flag provided but not defined` 오류가 납니다.
> **ev03 그룹 옵션(`-ev03Cores` 등)도 없습니다** — ev01/ev02만 처리합니다.
> ev03까지 필요하거나 동시 처리 수를 조절해야 하면 [10. VM_setup](./10_VM_setup.md)의 `lpage_setting`을 쓰세요
> (그쪽은 ev01~ev03 + `-concurrency`를 전부 지원합니다).

---

## 6. vm_create — VM 생성 🔴 (v2)

```bash
export VC_PASSWORD='실제_비밀번호'

./vm_create -vcTargetIP 192.168.0.50 -id administrator@vsphere.local \
  -vmCount 2 \
  -ev01Cpu 2 -ev01Mem 4 -ev01Disk 40 -ev01Share 1000 \
  -ev02Cpu 4 -ev02Mem 8 -ev02Disk 60 -ev02Share 2000 \
  -mapFile hostgroup.txt -firmware efi \
  -prepConcurrency 16 -taskConcurrency 24
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-vcTargetIP` | (필수) | vCenter 접속 IP |
| `-worklistFile` | `worklist.txt` | 대상 BM 호스트 목록 (VM을 실제로 붙일 ESXi 호스트) |
| `-vmCount` | `2` | 호스트당 생성할 VM 개수 (1~3, ev01~ev0N) |
| `-mapFile` | `hostgroup.txt` | `"BM호스트명 포트그룹이름"` 네트워크 매핑 파일 |
| `-firmware` | `efi` | `bios` 또는 `efi` |
| `-datacenter` | (없음) | 데이터센터 이름. 1개뿐이면 생략 가능, 여러 개면 필수 |
| `-ev01Cpu` / `-ev01Mem` / `-ev01Disk` / `-ev01Share` | `0` (필수) | ev01 vCPU / 메모리(GB) / 디스크(GB) / Shares |
| `-ev02Cpu` / `-ev02Mem` / `-ev02Disk` / `-ev02Share` | `0` (필수) | ev02 스펙 |
| `-ev03Cpu` / `-ev03Mem` / `-ev03Disk` / `-ev03Share` | `1` / `1` / `20` / `1000` | ev03 스펙 (`-vmCount=3`일 때만) |
| `-prepConcurrency` | `16` | 호스트 사전 조사(데이터스토어/리소스풀 조회) 동시 처리 수 |
| `-taskConcurrency` | `24` | `CreateVM`/`Reconfigure` 전송+대기 동시 처리 수 |

> ⚠️ **원본 README와 다른 점 (소스 확인 결과)**
> - README의 사용 예시에 `-folderName MyCluster`가 들어 있지만, **`vm_create`에는 `-folderName` 플래그가 없습니다**
>   (`-folderName`은 `vm_connect` 전용). 주면 `flag provided but not defined` 오류가 납니다.
> - **`-evNNShare`는 정수(`flag.Int`)입니다.** `nomal`/`normal` 같은 문자열은 받지 않습니다.
>   문자열 Shares(`nomal`)와 `-guestId` 옵션이 필요하면 [10. VM_setup](./10_VM_setup.md)의 `vm_create-source`를 쓰세요.

### v1 → v2 개선

| 구간 | v1 | v2 (현재) |
|---|---|---|
| VM/호스트 인벤토리 조회 | Finder로 재귀 순회 | **`ContainerView`로 1회 배치 조회** |
| 호스트별 데이터스토어 조회 | 호스트×데이터스토어 중첩 순차 루프 | **호스트당 1회 배치 조회** + 워커풀(`-prepConcurrency`) |
| `CreateVM`/`Reconfigure` | 순차 | **워커풀**(`-taskConcurrency`) |
| 진행 로그 | 없음 (끝나고 일괄) | 완료 즉시 `[N/전체]` 실시간 출력 |
| 전역 타임아웃 | 없음 | **60분** 컨텍스트 타임아웃 |

### ⚠️ 알려진 버그 (v1부터 존재, 미수정)

`loadHostgroupMap`이 `hostgroup.txt`의 각 줄을 **공백 또는 콤마**(`[,\s]+`)로 분리합니다. **포트그룹 이름 자체에 공백이 들어가면**(예: vSphere 기본 포트그룹명 `VM Network`) 잘못 파싱됩니다. 매우 흔한 케이스이니 공백 없는 포트그룹 이름을 쓰세요.

---

## 7. vm_connect — ESXi 호스트 등록 🔴 (v2)

```bash
export VC_PASSWORD='실제_비밀번호'
export ESXI_PASSWORD='ESXi_root_비밀번호'

# 클러스터에 등록
./vm_connect -vcTargetIP 192.168.0.50 -id administrator@vsphere.local \
  -folderName MyCluster -concurrency 20

# 데이터센터가 여러 개일 때
./vm_connect -vcTargetIP 192.168.0.50 -folderName MyCluster -datacenter DC01 \
  -worklistFile esxi_hosts.txt
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-vcTargetIP` | (필수) | vCenter 접속 IP |
| `-folderName` | (필수) | **데이터센터가 확정된 상태에서** 그 안의 클러스터 또는 폴더 이름. 둘 다 아니고 데이터센터 이름과 같으면 그 데이터센터의 기본 HostFolder 사용 |
| `-datacenter` | (없음) | 데이터센터 이름. 1개뿐이면 생략 가능, 여러 개면 필수 |
| `-worklistFile` | `worklist.txt` | 등록할 ESXi 호스트 목록 (IP 또는 FQDN) |
| `-concurrency <N>` | `20` | "이미 등록됨" 확인 + 호스트 등록의 동시 처리 개수 |

- SSL 미신뢰(`SSLVerifyFault`) 시 **thumbprint를 자동 추출해 재시도**합니다.

### v1에서 고친 버그 2건 (vcsim으로 재현 확인)

v1은 `finder.HostSystem()`/`ClusterComputeResource()`/`Folder()`를 호출하기 전에 **`finder.SetDatacenter()`를 한 번도 호출하지 않았습니다.** govmomi의 `find.Finder`는 이런 검색에 데이터센터 컨텍스트가 필요한데, 없으면 내부적으로 `"please specify a datacenter"` 에러가 나서 호출이 항상 실패합니다.

증상: **"이미 등록됨" 체크가 항상 실패**, **`-folderName`에 클러스터/폴더 이름을 넣으면 항상 실패**

### 한계

vcsim이 `AddHost`/`AddStandaloneHost`를 완전히 검증하지 않습니다(가짜 호스트명도 실패 없이 성공함). 즉 **실제 ESXi 연결 실패나 잘못된 자격증명 같은 에러 상황은 vcsim으로 재현·검증할 수 없었습니다.**

---

## 8. 실패 시 나오는 메시지 (원인별)

| 상황 | 메시지 예 | 발생 시점 |
|---|---|---|
| 설정 파일 없음 (affinity) | `ev01 파일을 찾을 수 없습니다: <경로>` | 시작 직후 (vCenter 접속 전) |
| 파일이 `key=value` 형식이 아님 | `ev01 설정 파일 오류 (<경로>): N번째 줄 형식 오류 (key=value 아님): <내용>` | 시작 직후 |
| 코어/소켓/NUMA가 안 나누어떨어짐 | `[ev01] 코어 수(5)가 소켓 수(2)로 나누어떨어지지 않습니다.` | 시작 직후 |
| `VC_PASSWORD` 미설정 | `인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.` | 시작 직후 |
| vCenter 접속 실패 | `vCenter 접속 실패: <상세>` | 접속 시도 시 |
| worklist와 매칭되는 VM 없음 | `worklist 와 매칭되는 VM 을 vCenter 에서 찾지 못했습니다.` | VM 조회 후 |
| 특정 VM이 없음 | `[호스트명ev0N] 경고: 대상 VM 이 존재하지 않습니다. (PASS)` | 해당 VM만 스킵, 전체는 계속 |
| Reconfigure 거부 | `[VM명] Reconfigure 명령 전송 실패: <상세>` | Task 전송 시 |
| Task 실패 | `[VM명] 작업 실패: <상세>` | Task 완료 대기 중 |
| (affinity만) 반영값 불일치 | `[VM명] 실제 적용 불일치: key(기대=X,실제=Y)` | 재조회 검증 시 |

---

## 9. 파일 구조

```
vm-setting-go-lang/
├── README.md          # 1차 자료
├── PLAN.md            # 개발/변경 계획 메모
├── main_affinity.go   # vm_affinity_bulk
├── main_lpage.go      # vm_lpage_bulk
├── main_vm_create.go  # vm_create
├── main_connect.go    # vm_connect
├── setup.sh           # ★ 파일명을 지정해 4개 바이너리를 각각 빌드
└── vendor/
```

---

## 10. 원본 README의 알려진 오류

이 문서를 만들면서 소스(`main_*.go`의 `flag` 정의)와 대조한 결과, 폴더의 `README.md`에 아래 오류가 있었습니다.
**옵션의 정확한 사실은 항상 소스의 `flag` 정의를 기준으로 하세요.**

| 위치 | README의 서술 | 실제 소스 |
|---|---|---|
| `vm_lpage_bulk` 옵션표 | `-concurrency` 기본 20 | **해당 플래그 없음** |
| `vm_lpage_bulk` | (ev03 언급 없음이나 다른 도구와 혼동하기 쉬움) | ev01/ev02만 지원 |
| `vm_create` 사용 예시 | `-folderName MyCluster` | **`vm_create`에 `-folderName` 없음** (`vm_connect` 전용) |
| `vm_create` 옵션표 | Share에 `nomal` 허용 | 정수만 허용 (`nomal`은 `VM_setup/vm_create-source` 쪽 기능) |

> 이 오류들을 README에서도 고치려면 [31_변경요청서_양식](./31_변경요청서_양식.md)의 B-3(문서 수정) 양식을 쓰세요.

---

## 11. 관련 문서

- 1차 자료: `vm-setting-go-lang/README.md` (위 10절의 오류 주의)
- 같은 기능의 다른 구현: [10. VM_setup](./10_VM_setup.md)
- 설정 점검·교정 통합 도구: [11. vm-param-check](./11_vm-param-check-usability-improvement.md)
