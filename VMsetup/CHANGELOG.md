# CHANGELOG

`VM_setup` 아래 도구들에 기능이 추가·수정될 때마다 이 파일에 날짜순(최신이 위)으로 기록합니다.

---

## 2026-09-22 — V2: `vm_setup.sh` 신설 (스펙·포트그룹 자동 할당 → y/n → 수동 선택 → vim) + home-test 실환경 검증

- **`vm_setup.sh`(신규)**: `VMsetup/<user>.txt`(BM 목록) + `SPEC_DIR/vswitch_<user>.txt`(BM 포트그룹 VLAN)로 스펙 결정 → 포트그룹 결정 → `vswitch_setting` → 스펙별 `vm_create` → `affinity_setting` → `lpage_setting`을 한 번에 실행한다. 사용법은 README "2. 사용 방법" 참고.
  - **스펙 자동 할당**: 포트그룹 이름 `<폴더명>-cae-a-b-c-d`에서 폴더명을 뽑아 `vm-param-check -specExport`로 SPEC_DIR 스펙을 찾는다(차수만 다른 폴더는 같은 스펙). BM→스펙 표를 보여주고 `(y/n)`. `n`이거나 자동으로 못 정한 BM은 목록에서 번호 선택(Enter=직전 선택), 목록에 없으면 `0`으로 vim.
  - **vim 스펙 입력**: 모든 항목이 `키=""` + 주석 설명. 저장하면 `SPEC_DIR/<folder>/`가 새로 생긴다(`folder`가 비었거나 CAE 규칙 위반이거나 같은 스펙이 이미 있으면 오류 안내 후 다시 편집, 검증은 `-initFolder`/`-specExport`를 그대로 재사용). 이어서 값이 있는 ev마다 **affinity를 자동 / 기존 파일 선택 / vim 입력** 중에서 고른다(vim 템플릿은 vCPU 수만큼 `sched.vcpuN.affinity=""` 줄).
  - **포트그룹(네트워크 어댑터 1) 할당**: BM에 포트그룹이 1개면 그 BM의 모든 VM, 여러 개면 스펙 폴더명과 이름이 맞는 것이 하나일 때 자동. VM→포트그룹 표를 `(y/n)`, 자동으로 못 정한 VM/`n`이면 번호 선택(**`a<번호>` = 그 BM의 VM 전체에 적용**), 목록에 없으면 vim(`hostname=""`, `portgroup=""`). 포트그룹 신규 생성(VLAN 입력)은 지원하지 않는다.
  - 실제 vCenter를 바꾸는 단계 직전에 실행 계획을 보여주고 한 번 더 확인한다. `-n`이면 계획까지만 보이고 종료(vCenter 접속 없음).
  - 스펙 값 → 도구 옵션: `cpu/mem/disk/shares-evNN` → `vm_create`(disk·shares 쉼표 목록은 첫 값), `ht` → `affinity_setting -ht`, `affinity-evNN` → `-affinityFileNN`, `cores`(소켓당)/`numa`(노드당 vCPU) → `lpage_setting` 총 코어(=cpu)/소켓 수/NUMA 노드 수.
  - **`affinity_setting`/`lpage_setting`에는 짧은 이름 목록(`vmbase_<k>.txt`)을 넘긴다**: 두 도구는 worklist 문자열을 그대로 VM 이름 접두어로 쓰고, `vm_create`는 BM 이름의 `.` 앞부분만 쓴다. `esxi-node-001.domain` 같은 BM이면 VM 이름은 `esxi-node-001ev01`인데 두 도구가 `esxi-node-001.domainev01`을 찾아 VM을 못 찾는다(실환경 `192.168.0.59` → `192ev03`에서 발견, vcsim에 도메인 붙은 호스트를 추가해 회귀 시험 추가).
- **`tag_setting`은 포함하지 않았다**: 스펙에 DEPT_NAME/PURPOSE/VM_TYPE 값이 없어서 단독 실행한다.
- **검증 — vcsim(록키)**: `검증/vmsetup_test.sh` 21건 PASS — 자동 할당 실행(4대 생성/포트그룹/affinity/lpage/shares), 스펙 후보 2개 모호 → 수동 선택 + 포트그룹 2개는 스펙 폴더명으로 자동 선택, vim 경로(잘못된 폴더명 → 다시 편집 → 저장, ev01 자동/ev02 vim affinity, 포트그룹 수동/vim), `n` 전체 수동, `a<번호>`, 도메인이 붙은 BM(`bm1.example.com` → `bm1ev01`), `-n`이 vCenter를 바꾸지 않음. vim은 가짜 편집기(`VM_SETUP_EDITOR`)로 템플릿을 채워 시험했고 **실제 vim 화면은 확인하지 않았다**.
- **검증 — home-test 실환경(vCenter 192.168.0.50, ESXi 192.168.0.59)**: 테스트용 데이터센터 `V2TEST-DC`를 하나 더 만들어 데이터센터 2개 상태에서 진행했고, 끝나고 VM·포트그룹·데이터센터를 전부 지워 원래 인벤토리와 같음을 확인했다(기존 `192ev01`/`192ev02`도 테스트 전후 설정 덤프 동일).
  - **수정 전 도구 재현**: `vm_create`는 `데이터센터가 2개 존재하여 자동 선택이 불가합니다`로 종료, `lpage_setting`은 `please specify a datacenter`로 실패.
  - `vswitch_setting`: 같은 BM에 포트그룹 2개(VLAN 3901/3902) 생성.
  - `vm_create`(`-datacenter` 없이, ev01~ev10): 이미 있는 `192ev01`/`192ev02`는 건너뛰고 ev03~ev10 **8대를 3.7초**에 생성, `-mapFile` VM 키로 ev05만 다른 포트그룹.
  - `lpage_setting`: ev03~ev10 8대 성공(기존 두 VM은 대상에서 제외).
  - `nic_assign`: 전원 꺼진 VM은 `conn=false/start=true`(전원 켤 때 연결 체크), **전원을 켠 VM(`192ev04`)에서 포트그룹을 바꿔도 `conn=true/start=true`**, 재실행은 "이미 적용됨".
  - `vm-param-check -specRoot`: VM 폴더가 CAE 규칙이 아니라서 포트그룹 이름으로 스펙 폴더를 유추해 ev03~ev10 스펙(`-cpu-ev10` 등)이 적용되고 핵심 항목이 전부 OK.
  - **실환경에서는 `vm_setup.sh`를 끝까지 돌리지 않았다**: 랩 호스트 이름이 `192.168.0.59`라 VM 이름 접두어가 `192`가 되어, 이미 있는 `192ev01`/`192ev02`에 `affinity_setting`(ev01부터 적용)이 적용되기 때문이다. 대신 `vm_setup.sh -n`으로 실행 계획을 뽑아 그 계획의 도구·옵션을 그대로 실행했다. `vm_setup.sh`의 실행 흐름 자체는 vcsim에서 시험했다.

## 2026-09-22 — V2: 호스트당 VM 1~10대, 데이터센터 여러 개 대응, vswitch 병렬화, nic_assign 신설

V2(`.claude/VM/V2/VMsetup`)는 `.claude/VM/VM_setup` 복사본에서 시작했다. 원본 폴더는 수정하지 않았다(`vm-param-fix`는 V2에서 제외).

- **VM 1~10대(`vm_create`, `affinity_setting`, `lpage_setting`, `tag_setting`)**: ev01~ev03 고정이던 플래그를 반복문으로 ev01~ev10까지 등록한다. 기존 이름(`-ev01Cpu`, `-affinityFile02`, `-ev03Cores` 등)은 그대로다.
  - `vm_create`: `-vmCount` 1~10. **ev01 필수**, ev 번호는 **ev01부터 연속**이어야 한다(예: ev02 없이 ev03 → 에러로 중단). **값(`-evNNCpu`)이 없는 ev는 만들지 않는다** — `-vmCount`가 값이 있는 ev 수보다 크면 경고 후 있는 만큼만 만든다.
    - **동작 변경 1**: 예전 ev03 기본값(1 CPU/1GB/20GB/Share 1000)을 없앴다. `-vmCount=3`에 ev03 값을 안 주면 예전에는 기본값으로 ev03을 만들었지만 이제는 만들지 않는다.
    - **동작 변경 2(완화)**: 예전에는 `-vmCount=1`이어도 `-ev02Cpu`가 없으면 종료했다. 이제는 ev01만 있으면 된다.
  - `affinity_setting`: `-vm_cnt` 1~10, `-affinityFile01`~`-affinityFile10`.
  - `lpage_setting`: `-ev01Cores/Sockets/Numa`~`-ev10...`.
  - `tag_setting`: `-vmCount` 상한 10 검사만 추가(예전에는 상한 없음). 이미 병렬 + vCenter 전체 조회 구조였다.
- **데이터센터 2개 이상 / 폴더 여러 단계**:
  - `vm_create`: 예전에는 데이터센터가 2개 이상이면 `-datacenter` 없이 종료했다. 이제는 데이터센터별 goroutine으로 VM/호스트를 병렬 배치 조회해서, 호스트가 속한 데이터센터의 VM 폴더에 만든다. VM 이름 중복 검사는 모든 데이터센터 대상. 같은 이름의 호스트가 여러 데이터센터에 있으면 그 호스트만 건너뛴다. `-datacenter`를 주면 예전처럼 그 데이터센터로 한정한다.
  - `lpage_setting`: `finder.DefaultDatacenter()`(에러 무시) → vCenter 최상위부터 `ContainerView`로 VM 이름을 1회 조회. 예전에는 다중 DC에서 `please specify a datacenter`로 실패했다(vcsim 재현).
  - `mac_info`, `main_conn`: 데이터센터를 전부 돌며 같은 방식으로 찾는다(데이터센터 1개면 예전과 같은 조회·같은 출력 순서).
  - `affinity_setting`, `numa_preferht_setting`, `tag_setting`, `vswitch_setting`, `license_assign`은 원래 다중 DC에서 동작하는 구조라 이 부분은 변경 없음.
- **`vswitch_setting` 병렬화**: 호스트를 하나씩 순차 처리하던 것을 호스트 단위 워커풀(`-concurrency`, 기본 20)로 바꿨다. 같은 호스트의 포트그룹 여러 개는 호스트 안에서 순서대로 만든다. 출력 문구는 같고, 호스트별로 묶어서 완료순으로 찍는다. **한 BM에 포트그룹을 2개 이상 적는 것은 예전 코드도 지원했다**(수정 전 바이너리로 vcsim에서 2개 생성 확인).
- **`vm_create -mapFile` VM 이름 키**: 매핑 파일에 `bm001ev02 PG-B`처럼 VM 이름으로 적은 줄이 있으면 그 VM만 해당 포트그룹을 쓴다. 없으면 예전처럼 BM 이름 줄을 따른다(한 BM에 포트그룹이 여러 개일 때 VM별로 다르게 붙이기 위함).
- **`nic_assign-source` 신설**: 이미 만들어진 VM의 네트워크 어댑터 1을 할당표(`VM이름 포트그룹`)대로 바꾸고 "연결됨"과 "전원을 켤 때 연결"을 체크한다. `vm-network-migration` Step 3(`nm-connect`)의 `SetPortgroup` → `EnsureConnectState` 방식(반영 후 API로 다시 읽어 확인, 아니면 연결 상태만 재전송, 최대 5회)을 따른다. 전원이 꺼진 VM은 "전원을 켤 때 연결"만 확인한다. VM끼리는 워커풀 병렬, 이미 원하는 상태면 건드리지 않는다(멱등).
- **빌드**: 의존성을 `V2/govendor/`(govmomi 0.55.1-standard, 0.39.0)에 두고 각 `setup.sh`가 `../../govendor/...`를 `vendor`로 링크한다 → **V2 폴더만 받아도 폐쇄망 빌드 가능**. `license_assign-source`는 원래처럼 자체 `vendor/`를 쓴다.
- **영향 범위**: `vm_create-source/main.go`, `affinity_setting-source/main.go`, `lpage_setting-source/main.go`, `tag_setting-source/main.go`, `vswitch_setting-source/main.go`, `mac_info-source/main.go`, `main_conn-source/main.go`, `nic_assign-source/`(신규), 각 `setup.sh`(vendor 링크 경로).
- **검증(록키 192.168.0.58, vcsim)**: 10개 모듈 `go build`/`go vet` 통과, `nic_assign` 단위 테스트 통과. vcsim 시나리오 23건 전부 PASS:
  - **회귀**: 수정 전/후 바이너리를 같은 인벤토리(DC 1개, 호스트 4대)에 돌려 `vm_create`(ev01~ev03 12대)·`affinity_setting`·`lpage_setting` 결과를 VM 설정 덤프(numCPU/메모리/예약/Shares/extraConfig/부트순서/디스크/NIC)로 비교 — **차이 0건**, 출력 로그도 동일.
  - **10대**: 호스트 4대 × 10대 = 40대 생성, ev10 Shares=normal, affinity/lpage ev10까지 적용.
  - **규칙**: ev02 없이 ev03 → 에러, ev03 값 없음 → 2대만 생성+경고, `-vmCount=1`+ev02 없음 → 정상.
  - **데이터센터 3개 + 호스트/VM 폴더 3단계**: vm_create/lpage/affinity/vswitch/mac_info 정상. 같은 환경에서 수정 전 vm_create는 종료, 수정 전 lpage는 실패함을 확인.
  - **포트그룹 2개**: 같은 BM에 PG-A/PG-B 생성 → `-mapFile`에서 BM 키=PG-A, VM 키(ev02)=PG-B로 생성 → `nic_assign`으로 ev01/ev03을 PG-B로 변경(전원 켤 때 연결 체크), 재실행 시 "이미 적용됨".
  - **속도**: 데이터센터 1개·호스트 20대에서 `vm_create` 조회 경로 10회 평균 수정 전 0.0275s / 수정 후 0.0275s. 데이터센터 3개·폴더 3단계·호스트 60대는 0.0534s.
  - **미검증**: 전원이 켜진 VM의 "연결됨" 체크는 vcsim이 런타임 연결을 흉내내지 않아 실환경(home-test)에서 확인 예정.

## 2026-09-02 — `vswitch_setting-source` 코드 교체 (HostGroup 매핑 → vSwitch 포트그룹 생성)

- 메일(`go lang 모음 V2`)의 `main_vs.txt`로 `main.go` 전체를 교체했다. 기존 코드는 폴더명과 달리 클러스터 HostGroup(DRS 그룹)을 매핑하는 도구였는데, 새 코드는 폴더명 그대로 **`worklist.txt`의 (호스트, 포트그룹, VLAN)을 읽어 각 호스트 `vSwitch0`에 포트그룹을 일괄 생성**한다(`HostNetworkSystem.AddPortGroup`).
- 플래그 변경: `-url`/`-cluster`/`-concurrency` → `-vcTargetIP`(필수), `-id`, `-worklistFile`, `-targetVSwitch`. 비밀번호는 환경변수 `VC_PASSWORD`로 받는다.
- 오래된 빌드 산출물 `vswitch_setting`(옛 로직 바이너리)을 삭제했다. 폐쇄망에서 `bash setup.sh`로 재빌드해야 한다.
- `README.md`를 새 동작/플래그에 맞게 다시 작성했다.
- `go.mod`/`go.sum`/`vendor/`는 그대로 두었다. 새 코드가 쓰는 패키지(`object`, `view`, `mo`, `types`)는 모두 기존 `vendor/`에 포함돼 있다.
- **검증**: 이 환경에 Go 툴체인이 없어 빌드/실행 검증은 하지 못했다. 소스 교체와 import 대상이 vendor에 존재하는지만 확인.

## 2026-08-25 — `vm_create-source` 게스트 OS 지정 옵션(`-guestId`) 추가

- **`-guestId` 플래그 신설 (`main.go`)**: 기존에는 `rhel8_64Guest`가 소스에 하드코딩돼 있어 다른 OS로 만들려면 소스를 고쳐야 했다. 이제 `-guestId=rhel9_64Guest`처럼 인자로 지정할 수 있고, **아무것도 주지 않으면 종전과 동일하게 `rhel8_64Guest`가 기본값**으로 쓰인다.
  - 게스트 OS 식별자 목록은 vSphere 버전마다 달라지므로 도구에서 화이트리스트로 막지 않는다(막으면 새 OS가 나올 때마다 소스를 고쳐야 함). 빈 값만 즉시 거르고, 실제 유효성은 vCenter가 `CreateVM` 단계에서 판정한다.
  - 시작 시 `[INFO] 게스트 OS: <값> / 펌웨어: <값>`을 출력해서 어떤 값으로 만들어지는지 바로 확인할 수 있게 했다.
- **VM 생성 실패가 조용히 무시되던 문제 보완 (`main.go`)**: 예전에는 `CreateVM`/Task 실패 시 아무 출력 없이 넘어가서, 예컨대 `-guestId`에 오타가 있으면 `생성 대상 VM 12대`라고 찍은 뒤 아무것도 만들어지지 않은 채 `새로 생성할 VM이 없거나...`로 끝나 원인을 알 수 없었다. 이제 실패 사유를 출력하고(대수가 많을 때 로그가 넘치지 않도록 **처음 5건까지만** 상세 출력), 마지막에 **실패 총 건수**와 `-guestId` 확인 안내를 요약해 준다. 성공 경로의 동작·출력은 이전과 동일하다.
- **검증**: vcsim 기준 ① 옵션 미지정 → `[INFO] 게스트 OS: rhel8_64Guest`, 생성된 VM의 실제 `guestId`도 `rhel8_64Guest` ② `-guestId rhel9_64Guest` → 실제 `guestId`가 `rhel9_64Guest`로 반영됨(독립 호스트/클러스터 호스트 양쪽에서 확인) ③ 빈 값/공백만 준 경우 즉시 종료 — 3가지 모두 확인.
  - 잘못된 식별자에 대한 실패 처리는 vcsim이 `guestId`를 검증하지 않아 시뮬레이터로는 재현되지 않으므로, 소스 사본에 생성 실패를 강제 주입해 별도로 검증함(12건 실패 시 상세 5건 + `실패 12건 (전체 12건 중)` 요약이 정상 출력됨을 확인).

## 2026-08-25 — `vm_create-source` 속도 개선 (동작 변경 없음)

대상 호스트가 많을수록 vCenter 왕복(round-trip) 횟수가 선형으로 늘어나던 구간들을 전부 배치 조회로 바꾸고, VM당 Task 수를 줄였습니다. **생성되는 VM의 최종 설정값은 이전과 완전히 동일합니다.**

- **VM당 Task 2회 → 1회 (`main.go`)**: 예전에는 `CreateVM`으로 만든 뒤 별도 `Reconfigure` Task로 메모리 예약·CPU/메모리 Shares·`sched.mem.*` extraConfig·Secure Boot 해제를 넣었다. 이 값들은 생성 시점에 이미 확정돼 있고 device key에 의존하지 않으므로 **생성 스펙에 함께 담아** Task 1회로 처리하도록 바꿨다.
  - 단, **부팅 순서(BootOrder)만은 생성 이후에 남겨뒀다** — 실제 device key는 VM이 만들어진 뒤에야 확정되므로, 생성 스펙에 임시 음수 key로 넣으면 동작이 달라질 위험이 있어 의도적으로 합치지 않았다.
- **인벤토리 재귀 탐색 제거 (`main.go`)**: 설정 단계에서 VM마다 `finder.VirtualMachine()`으로 인벤토리를 다시 뒤지던 것을, 생성 Task가 돌려주는 MoRef(`task.WaitForResult()`)를 그대로 쓰도록 바꿨다. VM 대수가 많을수록 이 탐색이 급격히 느려지던 구간이 통째로 사라진다.
- **디바이스 목록 배치 조회 (`main.go`)**: 부팅 순서를 정하려고 VM마다 `Properties()`를 호출하던 것을, 생성된 VM 전체에 대해 1회 배치 조회로 바꿨다.
- **데이터스토어 배치 조회 (`main.go`)**: 사전조사 goroutine 안에서 호스트마다 `pc.Retrieve(datastore)`를 부르던 것을, 전체 호스트의 데이터스토어를 중복 제거해 1회 배치 조회하도록 바꿨다. 선택 로직은 그대로 각 호스트 자신의 데이터스토어 안에서만 최대 여유공간을 고르므로 결과는 동일하다.
- **리소스풀 배치 조회 (`main.go`)**: `HostSystem.ResourcePool()`은 내부적으로 `parent` 조회 + `ComputeResource` 조회로 **호스트당 2회** 왕복이 발생한다. 여러 호스트가 같은 클러스터를 공유하므로 부모를 중복 제거한 뒤 `ComputeResource`/`ClusterComputeResource` 타입별로 한 번씩만 조회하도록 바꿨다(govmomi 원본과 동일한 타입 분기).
  - 그 결과 **사전조사 goroutine 안에서는 vCenter 왕복이 아예 발생하지 않는다**(전부 맵 조회).
- **검증**: 변경 전(git HEAD) 바이너리와 변경 후 바이너리를 각각 별도 vcsim 인스턴스에 돌려 결과를 비교함. 독립 호스트(`ComputeResource`)와 클러스터 호스트(`ClusterComputeResource`)를 모두 포함한 4개 호스트 × `-vmCount=3` = **12대 생성**, 커스텀 ratio(`4000`)와 `nomal` 두 Share 모드를 모두 사용.
  - VM별 `numCPU`/`memoryMB`/`firmware`/`guestId`/`memoryReservationLockedToMax`/메모리 예약/CPU·메모리 Shares(level+ratio)/`sched.*` extraConfig/Secure Boot/부트 순서를 덤프해 비교 — **차이 0건**.
  - 디바이스(ParaVirtual SCSI, 디스크 용량, vmxnet3 NIC 포트그룹)도 12대 전부 비교 — **차이 0건**.
  - 재실행 시 이미 존재하는 VM을 건너뛰는 멱등성도 그대로 유지됨을 확인.
- **출력 문구 변경**: 2단계 진행 메시지가 `리소스 설정 대상 VM N대` → `부팅 순서 설정 대상 VM N대`로 바뀌었다(리소스 설정이 생성 단계로 옮겨갔으므로). 그 외 출력은 동일.
