# 10. VM_setup — 개별 설정 적용 도구 모음 (9종)

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🔴 대부분 **실제 vCenter/ESXi 설정을 변경**합니다 (`mac_info`만 읽기 전용) |
| 폴더 | `.claude/VM/VM_setup/` |
| 구성 | 독립적인 소스+`vendor/`를 갖춘 도구 9개 + 오케스트레이터 1개 |
| 인증 | 계정은 `-id` 플래그(기본 `lscsystems@vsphere.local`), 비밀번호는 **`VC_PASSWORD`** 환경변수 |
| 빌드 | 각 하위 폴더에서 `bash setup.sh` (개별 빌드) |

> ⚠️ **`vm-param-fix/`는 새 프로젝트에 쓰지 마세요.** 같은 기능이 [`vm-param-check`의 `-fix` 옵션](./11_vm-param-check-usability-improvement.md)에 내장되어 외부 도구 없이 동작합니다. 나머지 개별 도구(affinity/lpage/tag/vswitch/license/vm생성 등)는 그대로 사용 가능합니다.

---

## 1. 도구 목록

| 폴더 | 바이너리 | 하는 일 | 위험도 |
|---|---|---|---|
| `affinity_setting-source/` | `affinity_setting` | CPU affinity(`sched.vcpuN.affinity`) 일괄 설정 | 🔴 |
| `lpage_setting-source/` | `lpage_setting` | HugePage/메모리 고정 + CPU 토폴로지(소켓/코어/NUMA) 설정 | 🔴 |
| `numa_preferht_setting-source/` | `numa_preferht_setting` | `numa.vcpu.preferHT=TRUE` 일괄 적용 | 🔴 |
| `tag_setting-source/` | `tag_setting` | Custom Attribute(`DEPT_NAME`/`PURPOSE`/`VM_TYPE`) 설정 | 🔴 |
| `vswitch_setting-source/` | `vswitch_setting` | vSwitch에 포트그룹 일괄 생성 | 🔴 |
| `vm_create-source/` | `vm_create` | BM 호스트별 ev01~ev03 VM 생성 | 🔴 |
| `license_assign-source/` | `license_assign` | 평가판 호스트를 찾아 정식 라이선스 할당 (**대화형**) | 🔴 |
| `main_conn-source/` | `main_conn` | ESXi 호스트를 vCenter 클러스터에 등록 (**옵션 없음, 소스 하드코딩**) | 🔴 |
| `mac_info-source/` | `mac_info` | VM MAC 주소 조회 → 프로비저닝 리스트 생성 | 🟢 |
| `vm-param-fix/` | `vm-param-fix` | 체크 CSV를 태그별로 분류해 위 도구들을 호출하는 오케스트레이터 (**레거시**) | 🔴 |

---

## 2. 공통 사항

### 빌드

각 폴더가 독립적입니다. 필요한 것만 빌드하세요.

```bash
cd .claude/VM/VM_setup/affinity_setting-source && bash setup.sh   # → affinity_setting
cd ../lpage_setting-source                      && bash setup.sh   # → lpage_setting
cd ../tag_setting-source                        && bash setup.sh   # → tag_setting
# ... 나머지도 동일
```

빌드된 실행 파일을 `/usr/local/bin`에 복사하면 어디서든 명령어처럼 쓸 수 있습니다.

```bash
sudo cp affinity_setting-source/affinity_setting lpage_setting-source/lpage_setting /usr/local/bin/
```

### 인증 (모든 도구 공통)

```bash
read -rsp 'vCenter 비밀번호: ' VC_PASSWORD; export VC_PASSWORD; echo
```

- 계정은 `-id` 플래그. 기본값은 **`lscsystems@vsphere.local`** 입니다 — 다른 계정을 쓰려면 반드시 명시하세요.
- 비밀번호 환경변수는 **`VC_PASSWORD`** 입니다 (`VC_PASS` 아님 — [11번 도구](./11_vm-param-check-usability-improvement.md)와 다릅니다).

### 공통 입력 파일

**`worklist.txt`** — 물리 호스트(BM) 이름을 한 줄에 하나씩. 도구가 여기에 `ev01`/`ev02`/`ev03`을 붙여 대상 VM 이름을 만듭니다.

```
svr01
svr02
```

---

## 3. affinity_setting — CPU affinity 설정 🔴

### 무엇을 하나

각 VM의 `sched.vcpu0.affinity`, `sched.vcpu1.affinity`, ... ExtraConfig에 "이 vCPU가 돌 수 있는 물리 CPU 번호"를 설정합니다.

### 사용법

```bash
export VC_PASSWORD='...'

# ev01만
./affinity_setting -vcTargetIP 192.168.0.50 -vm_cnt 1 -affinityFile01 affinity_ev01.txt

# ev01~ev03 전체, 동시 처리 30개
./affinity_setting -vcTargetIP 192.168.0.50 -vm_cnt 3 \
  -id administrator@vsphere.local \
  -affinityFile01 affinity_ev01.txt \
  -affinityFile02 affinity_ev02.txt \
  -affinityFile03 affinity_ev03.txt \
  -concurrency 30
```

### 옵션

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcTargetIP` | (없음) | ✅ | vCenter 접속 IP |
| `-id` | `lscsystems@vsphere.local` | | vCenter 로그인 계정 |
| `-worklistFile` | `worklist.txt` | | 대상 호스트 목록 파일 |
| `-vm_cnt` | `2` | | 호스트당 대상 VM 개수 (1=ev01, 2=ev01~ev02, 3=ev01~ev03) |
| `-affinityFile01` | (없음) | 조건부 | ev01 affinity 설정 파일. **미지정 시 `-ht`로 1:1 자동계산** |
| `-affinityFile02` | (없음) | 조건부 | ev02용 |
| `-affinityFile03` | (없음) | 조건부 | ev03용 |
| `-affinityFile` | (없음) | | **[구버전 호환]** `-affinityFile02` 미지정 시 ev02 설정으로 사용 |
| `-ht <ON\|OFF>` | `ON` | | 설정 파일 내용이 `AUTO`일 때만 사용. ON: `0=0,1` 형태 / OFF: `0=0` 형태 |
| `-concurrency <N>` | `20` | | 동시 처리 개수 (VM 목록 조회 / Reconfigure 전송+대기 전 구간) |

### affinity 파일 형식

```
0=0,1
1=2,3
2=4,5
```

또는 전체를 `AUTO`로 두면 `-ht`에 따라 자동 계산합니다.

### 특징

- 설정 후 **실제 반영값을 재조회해서 검증**합니다. 불일치하면 `[VM명] 실제 적용 불일치: key(기대=X,실제=Y)`로 알려줍니다.

---

## 4. lpage_setting — HugePage + CPU 토폴로지 설정 🔴

### 사용법

```bash
export VC_PASSWORD='...'

./lpage_setting -vcTargetIP 192.168.0.50 \
  -id administrator@vsphere.local \
  -ev01Cores 8 -ev01Sockets 2 -ev01Numa 4 \
  -ev02Cores 4 -ev02Sockets 1 -ev02Numa 2 \
  -concurrency 20
```

### 옵션

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-vcTargetIP` | (필수) | vCenter 접속 IP |
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-worklistFile` | `worklist.txt` | 대상 호스트 목록 |
| `-ev01Cores` / `-ev02Cores` / `-ev03Cores` | `0` | 그룹별 코어 수. **`0`(미지정)이면 그 그룹은 처리하지 않음** |
| `-ev01Sockets` / `-ev02Sockets` / `-ev03Sockets` | `0` | 그룹별 소켓 수. `-evNNCores` 지정 시 **필수** |
| `-ev01Numa` / `-ev02Numa` / `-ev03Numa` | `0` | 그룹별 NUMA 노드 수. `0`이면 토폴로지 NUMA 설정 생략 |
| `-applyTopology` | `true` | CPU 토폴로지(소켓당 코어 수/NUMA 노드)를 실제로 적용할지. `false`면 ExtraConfig만 |
| `-concurrency <N>` | `20` | 동시 처리 개수 |

### 사전 검증

vCenter에 접속하기 **전에** 값을 검사합니다:

- 코어 수가 소켓 수로 나누어떨어져야 함 → `[ev01] 코어 수(5)가 소켓 수(2)로 나누어떨어지지 않습니다.`
- NUMA를 지정하면 코어 수가 그 값으로도 나누어떨어져야 함

---

## 5. numa_preferht_setting — preferHT 일괄 적용 🔴

### 무엇을 하나

`-f`로 지정한 VM 목록에 `numa.vcpu.preferHT=TRUE`를 병렬로 적용합니다. 모든 VM에 공통 적용되는 단일 설정이라 **ev01/ev02/ev03 그룹 구분이 없습니다.**

### 조건

- **전원이 꺼져 있는(PoweredOff) VM에만 적용됩니다.** 켜져 있으면 건너뛰고 `[VM명] 전원 ON 상태 — 스킵 (PASS)`를 출력합니다.
- 목록에 있는데 vCenter에서 못 찾은 VM은 경고만 남기고 계속 진행합니다.

### 사용법

```bash
export VC_PASSWORD='...'
printf '192ev01\n192ev02\n' > targets.txt
./numa_preferht_setting -vc 192.168.0.50 -f targets.txt
```

### 옵션

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vc <IP[:포트]>` | (없음) | ✅ | vCenter 접속 주소. ⚠️ **다른 도구와 달리 `-vcTargetIP`가 아니라 `-vc`입니다** |
| `-id <계정>` | `lscsystems@vsphere.local` | | vCenter 로그인 계정 |
| `-f <path>` | (없음) | ✅ | 대상 **VM 이름** 목록 파일 (한 줄에 하나, `#` 주석 가능) |
| `-concurrency <N>` | `20` | | 동시 처리 개수 (VM 목록 조회 + Reconfigure 양쪽) |

### 동작

1. `-f` 파일의 VM 이름들로 정규식을 만들어 **전체 데이터센터를 동시에 순회**하며 대상 VM을 찾음
2. 찾은 VM 전체의 전원 상태를 **단일 배치 조회**로 확인
3. 없는 VM/전원 ON VM은 사전 스킵
4. 워커풀로 각 VM마다 Reconfigure 전송 → 완료 대기
5. 성공한 VM만 `config.extraConfig`를 **재조회해서 실제로 `TRUE`가 반영됐는지 검증**
6. "성공/실패/스킵/적용불일치" 건수로 요약, 문제가 있으면 **종료 코드 2**

### 관련

이 도구가 적용한 값은 [`vm-param-check`의 `-preferHT`](./11_vm-param-check-usability-improvement.md) 플래그로 점검할 수 있습니다. 그 도구는 `-fix`로 교정도 하므로, 새로 시작한다면 `vm-param-check`만 써도 됩니다.

---

## 6. tag_setting — Custom Attribute 설정 🔴

### 사전 조건 ⚠️

**`DEPT_NAME` / `PURPOSE` / `VM_TYPE` 이라는 이름의 Custom Attribute가 vCenter에 미리 정의되어 있어야 합니다.** (vSphere Client의 "태그 및 사용자 지정 특성" 메뉴에서 사전 생성) 정의되어 있지 않으면 `SetCustomValue` 호출이 에러를 반환할 수 있습니다.

### 옵션

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcTargetIP` | (없음) | ✅ | vCenter 접속 IP |
| `-id` | `lscsystems@vsphere.local` | | vCenter 로그인 계정 |
| `-hostListFile` | `hostlist.txt` | | 대상 물리 호스트 목록 파일 (⚠️ `worklist.txt`가 아님) |
| `-vmCount` | `1` | | 호스트 1대당 대상 VM 수(`ev01`~`ev0N`). 1 이상 |
| `-deptNames` | `""` | ✅ | 순번별 `DEPT_NAME` 값 (콤마 구분). **최소 `-vmCount`개 필요** |
| `-purposes` | `""` | ✅ | 순번별 `PURPOSE` 값 (콤마 구분) |
| `-vmTypes` | `""` | ✅ | 순번별 `VM_TYPE` 값 (콤마 구분) |

> `-vmCount=2`인데 `-deptNames`에 값이 1개뿐이면 **vCenter 접속 전에** `-deptNames 값이 부족합니다...` 에러로 종료합니다. 값이 더 많은 것은 허용되며 초과분은 무시됩니다.

### 동작 특이점

- 데이터센터 지정 없이 RootFolder부터 ContainerView로 **vCenter 전체 VM을 1회 배치 조회**합니다 (데이터센터가 여러 개여도 "please specify a datacenter" 에러가 안 남).
- 동일 이름의 VM이 2개 이상이면 모호하다고 판단해 **SKIP**합니다.
- **동시성 제한이 없습니다** — 대상이 매우 많으면 vCenter 부하에 주의하세요.

---

## 7. vswitch_setting — 포트그룹 일괄 생성 🔴

### 옵션

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-vcTargetIP` | (필수) | vCenter 접속 IP/호스트명. 누락 시 실행 중단 |
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-worklistFile` | `worklist.txt` | **vSwitch/포트그룹 설정 파일** (실행 디렉토리 기준 상대 경로) |
| `-targetVSwitch` | `vSwitch0` | 포트그룹을 생성할 대상 표준 가상 스위치 |

### 동작

1. 설정 파일을 파싱해 `(호스트, 포트그룹, VLAN)` 목록 생성
2. `HostSystem` 컨테이너 뷰로 전체 호스트의 `name`, `configManager.networkSystem`을 1회 수집
3. 호스트별로 `HostNetworkSystem.AddPortGroup` 호출
4. 이미 존재하면(`AlreadyExists`) 스킵, 그 외 에러는 출력 후 계속 진행

### 한계

- 포트그룹은 **`-targetVSwitch` 하나에만** 생성합니다. 호스트별로 다른 vSwitch에 만들려면 코드 수정 필요.
- 호스트를 인벤토리에서 못 찾거나 `networkSystem`이 없으면 그 호스트를 건너뜁니다(전체는 중단되지 않음).
- 포트그룹 정책은 **vSwitch 기본 정책을 상속**합니다(빈 `HostNetworkPolicy`로 생성).

> 포트그룹 생성 + VM 이관 + 롤백까지 통합된 도구가 필요하면 [14. vm-network-migration](./14_vm-network-migration.md)을 쓰세요.

---

## 8. vm_create — VM 생성 🔴

### 사용법

```bash
export VC_PASSWORD='...'

./vm_create -vcTargetIP 192.168.0.50 -id administrator@vsphere.local \
  -vmCount 2 \
  -ev01Cpu 2 -ev01Mem 4 -ev01Disk 40 -ev01Share 1000 \
  -ev02Cpu 4 -ev02Mem 8 -ev02Disk 60 -ev02Share 2000 \
  -mapFile hostgroup.txt -firmware efi \
  -prepConcurrency 16 -taskConcurrency 24
```

### 옵션

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcTargetIP` | (없음) | ✅ | vCenter 접속 IP |
| `-id` | `lscsystems@vsphere.local` | | vCenter 로그인 계정 |
| `-worklistFile` | `worklist.txt` | | 대상 물리 호스트 목록 |
| `-mapFile` | `hostgroup.txt` | | `"BM호스트 포트그룹이름"` 네트워크 매핑 파일 |
| `-vmCount` | `2` | | 호스트 1대당 생성할 VM 수. **1~3만 지원** (그 외는 즉시 종료) |
| `-firmware` | `efi` | | `bios` 또는 `efi`. **정상 부팅이 확인된 값은 `efi`(권장)** |
| `-guestId` | `rhel8_64Guest` | | 게스트 OS 식별자. 예: `rhel9_64Guest`, `centos8_64Guest`. 도구가 화이트리스트로 막지 않고 **vCenter가 유효성을 판정** |
| `-datacenter` | (없음) | 조건부 | 데이터센터가 여러 개일 때만 필수 |
| `-prepConcurrency` | `16` | | 호스트 사전 조사(데이터스토어/리소스풀 조회) 동시 처리 수. 500대 규모 기준 12~24 권장 |
| `-taskConcurrency` | `24` | | `CreateVM`/`Reconfigure` 동시 실행 수. 500대 규모 기준 16~32 권장 |
| `-ev01Cpu` / `-ev02Cpu` | `0` | ✅ | vCPU 수 (0이면 안 됨) |
| `-ev01Mem` / `-ev02Mem` | `0` | | 메모리(GB) |
| `-ev01Disk` / `-ev02Disk` | `0` | | 디스크(GB) |
| `-ev01Share` / `-ev02Share` | `"0"` | | CPU·메모리 Shares. 숫자 또는 `nomal` (아래 참고) |
| `-ev03Cpu` / `-ev03Mem` / `-ev03Disk` / `-ev03Share` | `1` / `1` / `20` / `"1000"` | | ev03 스펙 (`-vmCount=3`일 때만 사용) |

### Share 값

| 값 | 결과 |
|---|---|
| 숫자 (예: `1000`) | `SharesLevelCustom` + 해당 ratio |
| `nomal` / `normal` (대소문자 무시) | `SharesLevelNormal` (자동 계산되는 표준 공유값) |
| 그 외 | **vCenter 접속 전에** 에러로 종료 |

> `nomal`은 오탈자가 아니라 의도적으로 허용된 값입니다. `normal`도 됩니다.

### ⚠️ 알려진 버그

`hostgroup.txt`의 각 줄을 **공백 또는 콤마**로 분리하기 때문에, **포트그룹 이름 자체에 공백이 들어가면 잘못 파싱됩니다** (예: vSphere 기본 포트그룹명 `VM Network`). 공백 없는 이름을 쓰세요. 이 문제는 현재 버전에 그대로 남아 있습니다.

---

## 9. license_assign — 라이선스 할당 🔴 (대화형)

### ⚠️ 주의

**대화형 도구입니다.** 표준입력(stdin)으로 라이선스 번호를 직접 입력해야 하며, **사람이 지켜보지 않는 자동화 파이프라인/크론에 넣으면 프롬프트 대기 상태로 멈춥니다.**

### 옵션

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcTargetIP` | (없음) | ✅ | vCenter 접속 IP |
| `-id` | `lscsystems@vsphere.local` | | vCenter 로그인 계정 |
| `-worklistFile` | `worklist.txt` | | 대상 물리 호스트(BM) 목록 파일 |

### 동작 순서

1. vCenter 접속 → License Manager 초기화
2. `ContainerView`로 전체 `HostSystem`(이름 + CPU 코어 수) 1회 조회
3. **[1단계: 점검]** 호스트마다 현재 할당된 라이선스를 조회
   - 키가 `00000-00000-00000-00000-00000`(평가판 자리표시자)이거나 Edition Key에 `eval`이 포함되면 **평가판으로 판정**하고 작업 대상에 추가
   - 요약(총 호스트 수 / 스킵 수 / 작업 대상 수 / 필요 총 코어 수) 출력
   - 평가판 호스트가 하나도 없으면 **정상 종료(코드 0)**
4. **[2단계: 대화형 할당 루프]**
   1. vCenter에 등록된 라이선스 전체를 표(Index/제품명/키/총량/사용중/잔여)로 출력
   2. **라이선스 Index 번호 입력** 요청 (`q` 입력 시 중단)
   3. 선택한 라이선스의 잔여 코어가 허용하는 한, 대상 호스트를 순서대로 실제 할당
   4. 잔여량 부족으로 못 받은 호스트는 다음 라운드로 이월 → **다른 라이선스**를 추가 선택

---

## 10. main_conn — ESXi 호스트 등록 🔴 (옵션 없음)

### ⚠️ 이 도구만 특이합니다

**커맨드라인 플래그가 없습니다.** 대상 호스트 정보(IP/계정/비밀번호)와 vCenter 접속 정보가 **소스코드에 하드코딩**되어 있습니다. 실행 전에 `main.go`를 열어 값을 직접 수정한 뒤 다시 빌드해야 합니다.

### 동작

1. vCenter에 **1회만** 로그인(세션을 모든 goroutine이 공유)
2. 대상 클러스터(`clusterName`) 조회
3. `hosts` 목록을 goroutine으로 처리하되 세마포어로 **동시 5대**까지만 제한
4. 각 호스트에 `AddHost_Task` 호출
   - SSL 미신뢰 오류(`SSLVerifyFault`) 시 응답의 Thumbprint를 자동 추출해 **1회 재시도**
5. 호스트별 `[SUCCESS]`/`[FAIL]` 출력

### 한계

- 대상이 바뀔 때마다 **소스 수정 + 재빌드** 필요
- **비밀번호가 소스코드에 평문으로 남습니다.** 실제 값으로 채운 뒤에는 커밋/공유 전에 반드시 플레이스홀더로 되돌리세요
- 동시성(5)도 코드 내 상수, 전체 타임아웃 10분 고정

> 플래그로 제어 가능한 대안이 있습니다: [17. vm-setting-go-lang](./17_vm-setting-go-lang.md)의 `vm_connect` (v2, 병렬 + 데이터센터 지정 지원)

---

## 11. mac_info — MAC 주소 조회 🟢

### 무엇을 하나

`ev01`/`ev02`/... VM들의 MAC 주소를 조회해서 **Kickstart 프로비저닝용 텍스트 파일**(`Provisioning_List_<vCenterIP>.txt`)을 생성합니다. **읽기 전용**입니다.

### 옵션

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcTargetIP` | (없음) | ✅ | vCenter 접속 IP |
| `-id` | `lscsystems@vsphere.local` | | vCenter 로그인 계정 |
| `-worklistFile` | `worklist.txt` | | 대상 물리 호스트 목록 |
| `-arg1` | (없음) | ✅ | 출력 라인의 2번째 인자 (예: 사이트/그룹 식별자) |
| `-argInt` | `0` | | 출력 라인의 정수 인자 (예: 순번) |
| `-argStr` | (없음) | ✅ | 출력 라인의 문자열 인자 (예: Kickstart 프로파일 이름) |

### 출력 형식

```
VM VM <arg1> <VM이름> <VM이름>_DNS_AND_TOOLS_NOT_FOUND <MAC> eth0 sda sda5 <argInt> <argStr> uefi
```

### 한계

- **IP 조회 로직이 없어 IP 필드는 항상 `<VM이름>_DNS_AND_TOOLS_NOT_FOUND` 고정값**입니다. 실제 IP가 필요하면 VMware Tools의 `guest.ipAddress` 속성을 추가 조회하도록 확장해야 합니다.
- MAC이 없으면 `MAC_NOT_FOUND`로 채웁니다.

---

## 12. vm-param-fix — 오케스트레이터 (레거시) 🔴

> ⚠️ **새 프로젝트에는 쓰지 마세요.** [`vm-param-check -fix`](./11_vm-param-check-usability-improvement.md)가 같은 일을 외부 도구 없이 합니다.

### 무엇을 하나

`vm-param-check`가 낸 CSV를 읽어 태그(affinity/lpage/power)별로 분류하고, 각각 담당하는 **외부 바이너리 3개를 호출**하는 오케스트레이터입니다.

### 사용법

세 도구를 `vm-param-fix`와 같은 디렉토리에 모아두고 실행합니다.

```bash
VC_PASSWORD='<비밀번호>' ./vm-param-fix \
  -checkResult=<체크CSV> -vcTargetIP=<vCenter> -id=<계정> \
  -affinityTool=./affinity_setting -lpageTool=./lpage_setting -powerTool=./power_setting \
  -recheckTool=<vm-param-check 경로>
```

### 옵션

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-checkResult <path>` | (없음) | ✅ | `vm-param-check`가 낸 상세 CSV 경로 |
| `-vcTargetIP` | (없음) | ✅ | vCenter 접속 IP |
| `-id` | `lscsystems@vsphere.local` | | vCenter 로그인 계정 |
| `-affinityTool` | `./affinity_setting` | | affinity 태그 담당 외부 도구 경로 |
| `-lpageTool` | `./lpage_setting` | | lpage 태그 담당 외부 도구 경로 |
| `-powerTool` | `./power_setting` | | power 태그 담당 외부 도구 경로 |
| `-recheckTool` | `./vm-param-check` | | 적용 후 재검증에 쓸 `vm-param-check` 경로 |
| `-affinityFile` | (없음) | 조건부 | ev02 affinity 기대값 파일 (ev02 affinity FAIL이 있을 때만 필요) |
| `-workDir` | `.` | | 생성되는 worklist 파일들을 저장할 디렉토리 |
| `-out` | 타임스탬프 자동생성 | | 재검증 CSV 출력 경로 |
| `-scale <N>` | `0` | | 테스트용: 실제 vCenter/CSV 없이 N대 규모 합성 데이터로 시뮬레이션 |
| `-scaleMismatch` | `false` | | `-scale`과 함께: VM 1대 스펙을 일부러 다르게 만들어 동질성 게이트를 테스트 |

---

## 13. ⚠️ power_setting — 소스가 없는 바이너리

**`vm-param-fix/power_setting`은 컴파일된 바이너리만 존재합니다.**

`power_setting`의 Go 소스 코드를 **Rocky Linux 어디에서도 확실하게 찾지 못했습니다.** 로컬 파일 여러 개(`/root/pro/main.go` 등)를 대조해봤지만, 실행 바이너리의 플래그 구성(`-vcTargetIP`, `-worklistFile`, `-worklistBmFile` 등)과 정확히 일치하는 소스를 확정하지 못했습니다.

| 사실 | 의미 |
|---|---|
| 소스 없음 | **재빌드 불가능** |
| 바이너리만 있음 | **이 파일이 유일한 사본입니다. 절대 삭제하지 마세요.** |
| 새 통합 도구(`vm-param-check`)에 이 기능이 없음 | 호스트 전원정책은 **체크만** 하고 자동교정하지 않음 |

**이 기능이 다시 필요해지면** 둘 중 하나입니다:

1. `power_setting` 바이너리를 그대로 재사용 (vm-param-fix 오케스트레이터를 통해)
2. 같은 로직을 처음부터 새로 작성

---

## 14. 파일 구조

```
VM_setup/
├── README.md                     # 1차 자료
├── CHANGELOG.md                  # 변경 이력
├── vm-param-fix/                 # 오케스트레이터 (레거시) + power_setting 바이너리
│   ├── main.go, gates.go, report.go, scaletest.go, vcenter.go
│   ├── power_setting             # ⚠️ 소스 없는 바이너리 — 삭제 금지
│   ├── PLAN.md / README.md
│   └── setup.sh / test.sh
├── affinity_setting-source/      # CPU affinity 설정
├── lpage_setting-source/         # HugePage/CPU 토폴로지 설정
├── numa_preferht_setting-source/ # preferHT 일괄 적용
├── tag_setting-source/           # Custom Attribute 설정
├── vswitch_setting-source/       # 포트그룹 생성
├── vm_create-source/             # VM 생성
├── license_assign-source/        # 라이선스 할당 (대화형)
├── mac_info-source/              # MAC 조회 (읽기 전용)
└── main_conn-source/             # 호스트 등록 (옵션 없음)
```

각 하위 폴더는 `README.md` + `main.go` + `go.mod`/`go.sum` + `setup.sh` + `vendor/` 구성입니다.

---

## 15. 관련 문서

- 1차 자료: `VM_setup/README.md` 및 각 하위 폴더의 `README.md`
- 통합 대체 도구: [11. vm-param-check](./11_vm-param-check-usability-improvement.md)
- 비슷한 기능의 다른 도구 모음: [17. vm-setting-go-lang](./17_vm-setting-go-lang.md)
- 네트워크 이관 통합 도구: [14. vm-network-migration](./14_vm-network-migration.md)
