# VMsetup — VM 생성·설정 도구 모음 (V2)

호스트(BM)당 VM을 **1~99대(ev01~ev99)** 만들고 affinity/lpage/포트그룹까지 설정하는 도구 모음입니다.
`vm_setup.sh`가 `../SPEC_DIR`(vm-param-check와 공유)의 스펙을 읽어 전체 과정을 한 번에 진행하며,
각 도구(`*-source/`)는 단독으로도 쓸 수 있습니다. 모든 도구는 내부적으로 병렬(워커풀) 처리합니다.

> V1(`.claude/VM/VM_setup`)에서 달라진 점은 [CHANGELOG.md](CHANGELOG.md)를 참고하세요. V1의 `vm-param-fix/`(대체된 구버전 오케스트레이터)는 V2에 넣지 않았습니다.
> 전체 작업 흐름은 [../WORKFLOW.md](../WORKFLOW.md)에 그려 두었습니다.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우, 설정 변경 후 무작위로 서버 몇 대를 골라 실제로 변경되었는지 직접 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

의존성이 `../govendor/`에 들어 있으므로 **V2 폴더만 받아도 인터넷 없이 빌드**됩니다(`setup.sh`가 `../../govendor/<버전>`을 `vendor`로 링크하고 `-mod=vendor`로 빌드). `vm_setup.sh`는 실행 파일이 없으면 스스로 빌드합니다.

```bash
cd VMsetup
bash ../setup.sh                          # V2 루트의 전체 빌드 스크립트 (도구 하나만: cd <도구>-source && bash setup.sh)
```

요구사항: Go 1.26.5 이상, Linux(Rocky Linux 8에서 검증).

서버 배치: `/home/SPEC_DIR`, `/home/VMsetup`, `/home/vm-param-check-usability-improvement`, `/home/govendor`가 나란히 있어야 합니다(V2 폴더 내용을 그대로 `/home`에 복사).

## 2. 사용 방법

### 한 번에 실행: `vm_setup.sh`

준비물:

| 파일 | 내용 |
|---|---|
| `VMsetup/<user>.txt` | 대상 BM 목록 (한 줄에 하나) |
| `SPEC_DIR/vswitch_<user>.txt` | `BM  포트그룹  VLAN` (BM당 여러 줄 가능 — 적은 만큼 포트그룹이 만들어짐) |
| `vcenter.txt` (선택) | vCenter 주소 목록, 한 줄에 하나. V2 폴더(`/home/vcenter.txt`)에 두며, 없으면 vm-param-check 폴더의 `vcenter.txt`를 씁니다. 예시: `../vcenter.txt.example` |

BM 이름은 FQDN(`esxi01.domain`)과 짧은 이름(`esxi01`) 모두 쓸 수 있습니다. vCenter에 어느 쪽으로 등록되어 있든 찾아냅니다(먼저 이름이 정확히 같은 호스트를 찾고, 없으면 첫 `.` 앞부분끼리 비교). 짧은 이름이 같은 호스트가 여러 대이면 어느 쪽인지 판단할 수 없으므로 오류로 알립니다. 두 파일의 BM 이름은 서로 같게 적어 주세요.

`vswitch_<user>.txt`의 포트그룹 컬럼에 `<폴더명>-cae-a-b-c-d` 대신 IP를 적어 두었다면 먼저 변환합니다(`/24` 가정, 마지막 옥텟은 0으로 고정).

```bash
bash vswitch_pgname.sh ../SPEC_DIR/vswitch_<user>.txt   # 형식이 아닌 줄만 폴더명을 물어보고 변환(Enter = 직전 폴더명), 원본은 .bak로 보존
```

```bash
export VC_PASSWORD='...'                  # 없으면 실행 중에 물어봄
./vm_setup.sh -u hong                     # vCenter는 vcenter.txt 목록에서 번호로 선택 (Enter = hong의 이전 실행 vCenter)
./vm_setup.sh -u hong -v 192.168.0.50     # vCenter를 직접 지정. -n을 붙이면 vCenter 변경 없이 계획까지만 확인
```

user별로 마지막에 실행한 vCenter를 `run_<user>/last_vcenter`에 기억해 두었다가, 다음 실행 때 `[INFO] hong 이전 실행 vCenter: ...`로 보여 주고 목록에도 표시합니다.

진행 순서:

1. **스펙 할당** — 포트그룹 이름이 `<폴더명>-cae-a-b-c-d` 형식이면 그 폴더명으로 `SPEC_DIR` 스펙을 자동 매칭합니다(차수만 다른 폴더는 같은 스펙). BM→스펙 표를 보여 주고 `(y/n)`으로 확인합니다.
   - `n`을 고르거나 자동으로 정하지 못한 BM은 **SPEC_DIR 목록에서 번호로 선택**합니다(Enter = 직전 선택).
   - 목록에 없으면 `0`을 골라 **vim으로 새 스펙을 입력**합니다. 모든 항목이 `키=""` 상태로 열리고 설명은 주석으로 달려 있습니다. 저장하면 `SPEC_DIR/<folder>/`에 새 스펙 폴더가 생깁니다(`folder`가 규칙에 맞지 않거나 같은 스펙이 이미 있으면 오류를 안내하고 다시 편집). 이어서 **ev별 affinity**를 (1 직전 ev와 같은 파일 / 2 기존 파일 / 3 vim 입력) 중에서 고릅니다. ev01은 2·3만 고를 수 있고, ev02부터는 Enter가 1번입니다.
   - **affinity는 ev마다 필수**입니다(자동 계산 기능은 삭제됨). 스펙에 `affinity-evNN`이 없는 ev가 있으면 그 스펙은 자동 할당하지 않고 이유를 알려 줍니다. 여러 ev가 같은 파일을 써도 됩니다(`affinity-ev02=affinity_ev01.txt`).
2. **포트그룹 할당(네트워크 어댑터 1)** — BM에 포트그룹이 1개면 그 BM의 모든 VM에 붙이고, 여러 개면 스펙 폴더명과 이름이 맞는 것을 자동 선택합니다. VM→포트그룹 표를 `(y/n)`으로 확인하며, 자동으로 정하지 못한 VM이나 `n`을 고른 경우는 **번호로 선택**하고, 목록에 없으면 `0`을 골라 **vim**(`hostname=""`, `portgroup=""`)에서 지정합니다.
3. **vCenter 선택**(`-v`가 없을 때) — `vcenter.txt` 목록에서 번호로 고릅니다. `0`은 직접 입력, Enter는 이 user의 이전 실행 vCenter입니다.
4. **실행 계획 확인 후 실행**(한 번 더 y) — `vswitch_setting`(호스트 병렬) → 스펙별로 `vm_create` → `affinity_setting` → `lpage_setting` 순서로 실행합니다.
   - 호스트를 찾지 못하거나 포트그룹/VM 생성에 실패하면 **그 단계에서 멈추고** `[완료]`를 출력하지 않습니다. 이미 있는 포트그룹/VM은 실패로 보지 않고 건너뛰므로, 원인을 고친 뒤 다시 실행하면 이어서 진행됩니다.

`tag_setting`(사용자 지정 특성)은 스펙에 값이 없어 `vm_setup.sh`에 포함하지 않았습니다. 필요하면 단독으로 실행하세요.

### 단독 실행

도구별 옵션은 `*-source/README.md`(또는 `-h`)를 참고하세요. 공통 규칙은 다음과 같습니다.

- 접속: `-vcTargetIP`, `-id`, 비밀번호는 환경변수 `VC_PASSWORD`
- 대상 호스트 목록: `-worklistFile`(실행 폴더 기준 상대경로)
- **ev 규칙**: ev01 필수, ev 번호는 ev01부터 연속, **값이 없는 ev는 만들지 않음**

| 도구 | 하는 일 | ev 범위 |
|---|---|---|
| `vm_create-source` | 호스트별 VM 생성(+CPU/메모리 예약/Shares/부트 순서). `-mapFile`에 VM 이름을 키로 포트그룹 지정 가능 | `-vmCount` 1~99, `-ev01Cpu`~`-ev99Share` |
| `affinity_setting-source` | affinity 일괄 적용. `-vm_cnt` 범위의 ev마다 파일 필수(자동 계산 삭제, `-ht`는 받기만 하고 무시), 여러 ev에 같은 파일 지정 가능 | `-vm_cnt` 1~99, `-affinityFile01~99` |
| `lpage_setting-source` | HugePage/CPU 토폴로지 | `-ev01Cores/Sockets/Numa`~`-ev99...` |
| `tag_setting-source` | 사용자 지정 특성 | `-vmCount` 1~99 |
| `vswitch_setting-source` | BM vSwitch에 포트그룹 생성(호스트 병렬, `-concurrency`) | — |
| `nic_assign-source` | 만들어진 VM의 네트워크 어댑터 1 포트그룹 교체 + **연결됨/전원을 켤 때 연결** 체크 | — |
| `numa_preferht_setting-source` | `numa.vcpu.preferHT` 일괄 적용 | — |
| `license_assign-source`, `mac_info-source`, `main_conn-source` | 라이선스 할당 / MAC 정보(프로비저닝 목록) / ESXi 호스트를 vCenter에 병렬 등록 | — |

데이터센터가 2개 이상이거나 폴더가 여러 단계여도 동작합니다(`vm_create`는 호스트가 속한 데이터센터를 자동으로 찾으며, `-datacenter`로 한정할 수 있음).

## 3. 옵션별 상세 설명

`vm_setup.sh` 옵션:

| 옵션 | 설명 |
|---|---|
| `-u <user>` | (필수) 작업 이름 — `<user>.txt`, `SPEC_DIR/vswitch_<user>.txt`를 찾는 데 사용 |
| `-v <ip>` | vCenter IP (환경변수 `VC_IP`도 가능). 없으면 `vcenter.txt` 목록에서 번호로 선택(Enter = 이전 실행) |
| `-i <id>` | vCenter 계정 (기본 `administrator@vsphere.local`) |
| `-s <dir>` | SPEC_DIR 경로 (기본 `../SPEC_DIR`) |
| `-w <vswitch>` | 포트그룹을 만들 가상 스위치 (기본 `vSwitch0`) |
| `-c <n>` | vswitch/affinity/lpage 동시 처리 수 |
| `-n` | 확인만 — 스펙·포트그룹 할당과 실행 계획까지 보여 주고 종료(vCenter 변경 없음) |

환경변수 `VM_SETUP_EDITOR`로 vim 대신 다른 편집기를 쓸 수 있습니다.

스펙 값이 각 도구 옵션으로 바뀌는 규칙:

| 스펙 키 | 전달되는 옵션 |
|---|---|
| `cpu/mem/disk/shares-evNN` | `vm_create -evNNCpu/Mem/Disk/Share` (disk·shares에 쉼표 목록이 있으면 첫 값) |
| `affinity-evNN` | `-affinityFileNN` (ev마다 필수) |
| `ht` | VM 생성에는 쓰지 않음 (vm-param-check 체크용) |
| `cores`(소켓당 코어 수), `numa`(NUMA 노드당 vCPU 수) | `lpage_setting`의 총 코어(=cpu)/소켓 수/NUMA 노드 수 |

## 4. 문서별 고유 설명

```
VMsetup/
├── vm_setup.sh                 # 전체 과정(스펙·포트그룹 할당 → 생성 → 설정) 실행 스크립트
├── README.md / CHANGELOG.md    # 이 문서 / 변경 이력
├── vswitch_pgname.sh           # vswitch_<user>.txt의 IP 포트그룹명을 <폴더명>-cae-a-b-c-0으로 변환
├── <user>.txt                  # (사용자 파일, git 제외) BM 목록
├── run_<user>/                 # (자동 생성, git 제외) 이번 실행의 worklist/hostgroup/vswitch 입력 사본 + last_vcenter(이전 실행 vCenter)
└── *-source/                   # 도구별 Go 소스 + setup.sh (+ README.md)
```

알려진 한계:

- `lpage_setting`은 NUMA 노드당 코어 수를 스펙의 numa 값에 맞추지만, `numa.vcpu.maxPerVirtualNode`는 기존 동작(코어 수)대로 씁니다. `vm-param-check` 결과에 FAIL이 나오면 `-fix`로 교정하세요.
- 전원이 켜진 VM의 "연결됨" 체크는 vcsim에서 재현되지 않아 실제 vCenter(home-test)에서만 확인할 수 있습니다.
- `vm-param-check/folder_setup.sh`는 ev01~ev03까지만 묻습니다. ev04~ev99 스펙은 `vm_setup.sh`의 vim 입력(스펙 수동 입력)으로 만들거나 `_spec.txt`를 직접 편집하세요.
- `vm_setup.sh`의 vim 스펙 템플릿에는 ev10까지만 빈 틀이 있습니다. ev11~ev99는 같은 규칙의 줄(`cpu-ev11="4"` 등)을 직접 추가하면 저장됩니다.
- 각 도구의 `-h` 도움말은 ev02~ev99 옵션을 `-ev02~99Cpu`처럼 한 줄로 묶어 보여 줍니다(실제 옵션 이름은 `-ev02Cpu`, `-ev57Cpu` … 그대로).
- 포트그룹을 새로 만들면서 VM에 붙이는 기능(vim에서 VLAN 입력)은 지원하지 않습니다. 새 포트그룹은 `vswitch_<user>.txt`에 적어 `vswitch_setting`으로 만드세요.

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH에 포함된 디렉터리로 복사하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 쓸 수 있습니다.

```bash
sudo cp vm_create-source/vm_create nic_assign-source/nic_assign /usr/local/bin/
# vm_setup.sh는 같은 폴더의 *-source/ 실행 파일과 ../SPEC_DIR를 기준으로 동작하므로 PATH로 옮기지 말고 그대로 실행한다
```
