# VMsetup — VM 생성·설정 도구 모음 (V2)

호스트(BM)당 VM을 **1~99대(ev01~ev99)** 만들고 affinity/lpage/포트그룹까지 설정하는 도구들입니다.
스펙은 `../SPEC_DIR`(vm-param-check와 공유)에서 읽는 `vm_setup.sh`가 전체 과정을 묶어 주고,
각 도구(`*-source/`)는 단독으로도 쓸 수 있습니다. 모든 도구는 내부적으로 병렬(워커풀) 처리합니다.

> V1(`.claude/VM/VM_setup`)에서 달라진 점은 [CHANGELOG.md](CHANGELOG.md) 참고. V1의 `vm-param-fix/`(대체된 구버전 오케스트레이터)는 V2에 넣지 않았습니다.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

의존성은 `../govendor/`에 들어 있어 **V2 폴더만 받아도 인터넷 없이 빌드**됩니다(`setup.sh`가 `../../govendor/<버전>`을 `vendor`로 링크하고 `-mod=vendor`로 빌드). `vm_setup.sh`는 실행파일이 없으면 스스로 빌드합니다.

```bash
cd VMsetup
bash ../setup.sh                          # V2 루트의 전체 빌드 스크립트 (도구 하나만: cd <도구>-source && bash setup.sh)
```

요구사항: Go 1.26.5 이상, Linux(Rocky Linux 8에서 검증).

배치(서버): `/home/SPEC_DIR`, `/home/VMsetup`, `/home/vm-param-check-usability-improvement`, `/home/govendor` 가 나란히 있어야 합니다(V2 폴더 내용을 그대로 `/home`에 복사).

## 2. 사용 방법

### 한 번에: `vm_setup.sh`

준비물 2개:

| 파일 | 내용 |
|---|---|
| `VMsetup/<user>.txt` | 대상 BM 목록 (한 줄에 하나) |
| `SPEC_DIR/vswitch_<user>.txt` | `BM  포트그룹  VLAN` (BM당 여러 줄 가능 — 포트그룹이 여러 개 만들어짐) |
| `vcenter.txt` (선택) | vCenter 주소 목록, 한 줄에 하나. V2 폴더(`/home/vcenter.txt`)에 두고, 없으면 vm-param-check 폴더의 `vcenter.txt`를 쓴다. 예시: `../vcenter.txt.example` |

BM 이름은 FQDN(`esxi01.domain`)이든 짧은 이름(`esxi01`)이든 됩니다. vCenter에 어느 쪽으로 등록돼 있어도 찾습니다(정확히 같은 이름 → 없으면 첫 `.` 앞부분끼리 비교). 짧은 이름이 같은 호스트가 여러 대면 어느 쪽인지 알 수 없어 오류로 알립니다. 두 파일의 BM 이름은 서로 같게 적어주세요.

`vswitch_<user>.txt`의 포트그룹 컬럼에 `<폴더명>-cae-a-b-c-d` 대신 IP를 적어뒀다면, 먼저 변환한다(`/24` 가정, 마지막 옥텟 고정 0):

```bash
bash vswitch_pgname.sh ../SPEC_DIR/vswitch_<user>.txt   # 형식이 아닌 줄만 폴더명을 물어보고 변환(Enter = 직전 폴더명), 원본은 .bak로 보존
```

```bash
export VC_PASSWORD='...'                  # 없으면 실행 중에 물어봄
./vm_setup.sh -u hong                     # vCenter 는 vcenter.txt 목록에서 번호로 선택 (Enter = hong 의 이전 실행 vCenter)
./vm_setup.sh -u hong -v 192.168.0.50     # vCenter 를 직접 지정. -n 을 붙이면 vCenter 변경 없이 계획까지만 확인
```

user 별로 마지막으로 실행한 vCenter를 `run_<user>/last_vcenter`에 기억해서, 다음 실행 때 `[INFO] hong 이전 실행 vCenter: ...`로 보여주고 목록에서 표시합니다.

진행 순서:

1. **스펙 할당** — 포트그룹 이름이 `<폴더명>-cae-a-b-c-d` 형식이면 그 폴더명으로 `SPEC_DIR` 스펙을 자동 매칭(차수만 다른 폴더는 같은 스펙). BM→스펙 표를 보여주고 `(y/n)`으로 확인.
   - `n`이거나 자동으로 못 정한 BM은 **SPEC_DIR 목록에서 번호 선택** (Enter = 직전 선택).
   - 목록에 없으면 `0`을 골라 **vim으로 새 스펙 입력**. 모든 항목이 `키=""` 상태이고 설명은 주석이다. 저장하면 `SPEC_DIR/<folder>/`에 새 스펙 폴더가 생긴다(`folder`가 규칙에 안 맞거나 같은 스펙이 있으면 오류 안내 후 다시 편집). 이어서 **ev별 affinity**를 (1 직전 ev와 같은 파일 / 2 기존 파일 / 3 vim 입력) 중에서 고른다. ev01은 2·3만 가능하고, ev02부터는 Enter가 1번이다.
   - **affinity는 ev마다 필수**입니다(자동 계산은 삭제). 스펙에 `affinity-evNN`이 없는 ev가 있으면 그 스펙은 자동 할당하지 않고 이유를 알려줍니다. 여러 ev가 같은 파일을 써도 됩니다(`affinity-ev02=affinity_ev01.txt`).
2. **포트그룹 할당(네트워크 어댑터 1)** — BM에 포트그룹이 1개면 그 BM의 모든 VM에, 여러 개면 스펙 폴더명과 이름이 맞는 것을 자동 선택. VM→포트그룹 표를 `(y/n)`으로 확인, 자동으로 못 정한 VM/`n`이면 **번호 선택**, 목록에 없으면 `0`으로 **vim**(`hostname=""`, `portgroup=""`)에서 지정.
3. **vCenter 선택**(`-v`가 없을 때) — `vcenter.txt` 목록에서 번호로 고른다. `0`은 직접 입력, Enter는 이 user의 이전 실행 vCenter.
4. **실행 계획 확인 후 실행**(한 번 더 y) — `vswitch_setting`(호스트 병렬) → 스펙별로 `vm_create` → `affinity_setting` → `lpage_setting`.
   - 호스트를 못 찾거나 포트그룹/VM 생성에 실패하면 **그 단계에서 멈추고** `[완료]`를 출력하지 않습니다. 이미 있는 포트그룹/VM은 실패가 아니라 건너뜁니다 — 원인을 고친 뒤 다시 실행하면 이어서 진행됩니다.

`tag_setting`(사용자 지정 특성)은 스펙에 값이 없어 `vm_setup.sh`에 포함하지 않았습니다. 필요하면 단독으로 실행하세요.

### 단독 실행

각 도구의 옵션은 `*-source/README.md`(또는 `-h`)를 참고하세요. 공통 규칙:

- 접속: `-vcTargetIP`, `-id`, 비밀번호는 환경변수 `VC_PASSWORD`
- 대상 호스트 목록: `-worklistFile`(실행 폴더 기준 상대경로)
- **ev 규칙**: ev01 필수, ev 번호는 ev01부터 연속, **값이 없는 ev는 만들지 않음**

| 도구 | 하는 일 | ev 범위 |
|---|---|---|
| `vm_create-source` | 호스트별 VM 생성(+CPU/메모리 예약/Shares/부트순서). `-mapFile`에 VM 이름 키로 포트그룹 지정 가능 | `-vmCount` 1~99, `-ev01Cpu`~`-ev99Share` |
| `affinity_setting-source` | affinity 일괄 적용. `-vm_cnt` 범위의 ev마다 파일 필수(자동 계산 삭제, `-ht`는 받아서 무시), 여러 ev에 같은 파일 지정 가능 | `-vm_cnt` 1~99, `-affinityFile01~99` |
| `lpage_setting-source` | HugePage/CPU 토폴로지 | `-ev01Cores/Sockets/Numa`~`-ev99...` |
| `tag_setting-source` | 사용자 지정 특성 | `-vmCount` 1~99 |
| `vswitch_setting-source` | BM vSwitch에 포트그룹 생성(호스트 병렬, `-concurrency`) | — |
| `nic_assign-source` | 만들어진 VM의 네트워크 어댑터 1 포트그룹 교체 + **연결됨/전원을 켤 때 연결** 체크 | — |
| `numa_preferht_setting-source` | `numa.vcpu.preferHT` 일괄 적용 | — |
| `license_assign-source`, `mac_info-source`, `main_conn-source` | 라이선스 할당 / MAC 정보 / vCenter 접속 확인 | — |

데이터센터가 2개 이상이거나 폴더가 여러 단계여도 동작합니다(`vm_create`는 호스트가 속한 데이터센터를 자동으로 찾음, `-datacenter`로 한정 가능).

## 3. 옵션별 상세 설명

`vm_setup.sh` 옵션:

| 옵션 | 설명 |
|---|---|
| `-u <user>` | (필수) 작업 이름 — `<user>.txt`, `SPEC_DIR/vswitch_<user>.txt` |
| `-v <ip>` | vCenter IP (환경변수 `VC_IP`도 가능). 없으면 `vcenter.txt` 목록에서 번호 선택(Enter = 이전 실행) |
| `-i <id>` | vCenter 계정 (기본 `administrator@vsphere.local`) |
| `-s <dir>` | SPEC_DIR 경로 (기본 `../SPEC_DIR`) |
| `-w <vswitch>` | 포트그룹을 만들 가상 스위치 (기본 `vSwitch0`) |
| `-c <n>` | vswitch/affinity/lpage 동시 처리 수 |
| `-n` | 확인만 — 스펙·포트그룹 할당과 실행 계획까지 보이고 종료(vCenter 변경 없음) |

환경변수 `VM_SETUP_EDITOR`로 vim 대신 다른 편집기를 쓸 수 있습니다.

스펙 값이 각 도구 옵션으로 바뀌는 규칙: `cpu/mem/disk/shares-evNN` → `vm_create -evNNCpu/Mem/Disk/Share`(disk·shares에 쉼표 목록이 있으면 첫 값), `affinity-evNN` → `-affinityFileNN`(ev마다 필수), `ht`는 VM 생성에 쓰지 않음(vm-param-check 체크용), `cores`(소켓당 코어 수)·`numa`(NUMA 노드당 vCPU 수) → `lpage_setting`의 총 코어(=cpu)/소켓 수/NUMA 노드 수.

## 4. 문서별 고유 설명

```
VMsetup/
├── vm_setup.sh                 # 전체 과정(스펙·포트그룹 할당 → 생성 → 설정) 실행 스크립트
├── README.md / CHANGELOG.md    # 이 문서 / 변경 이력
├── vswitch_pgname.sh            # vswitch_<user>.txt 의 IP 포트그룹명을 <폴더명>-cae-a-b-c-0 으로 변환
├── <user>.txt                  # (사용자 파일, git 제외) BM 목록
├── run_<user>/                 # (자동 생성, git 제외) 이번 실행의 worklist/hostgroup/vswitch 입력 사본 + last_vcenter(이전 실행 vCenter)
└── *-source/                   # 도구별 Go 소스 + setup.sh (+ README.md)
```

알려진 한계:

- `lpage_setting`은 NUMA 노드당 코어 수를 스펙 numa 값에 맞추지만 `numa.vcpu.maxPerVirtualNode`는 기존 동작(코어 수)대로 씁니다 — `vm-param-check` 결과에서 FAIL이 나오면 `-fix`로 교정하세요.
- 전원이 켜진 VM의 "연결됨" 체크는 vcsim에서 재현되지 않아 실제 vCenter(home-test)에서만 확인할 수 있습니다.
- `vm-param-check/folder_setup.sh`는 ev01~ev03까지만 묻습니다. ev04~ev99 스펙은 `vm_setup.sh`의 vim 입력(스펙 수동 입력)으로 만들거나 `_spec.txt`를 직접 편집하세요.
- `vm_setup.sh`의 vim 스펙 템플릿은 ev10까지만 빈 틀이 있습니다. ev11~ev99는 같은 규칙의 줄(`cpu-ev11="4"` 등)을 직접 추가하면 저장됩니다.
- 각 도구의 `-h` 도움말은 ev02~ev99 옵션을 `-ev02~99Cpu`처럼 한 줄로 묶어 보여줍니다(실제 옵션 이름은 `-ev02Cpu`, `-ev57Cpu` … 그대로).
- 포트그룹을 새로 만들면서 VM에 붙이는 것(vim에서 VLAN 입력)은 지원하지 않습니다. 새 포트그룹은 `vswitch_<user>.txt`에 적어 `vswitch_setting`으로 만드세요.

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH에 포함된 디렉터리로 복사하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 사용할 수 있습니다.

```bash
sudo cp vm_create-source/vm_create nic_assign-source/nic_assign /usr/local/bin/
# vm_setup.sh 는 같은 폴더의 *-source/ 실행파일과 ../SPEC_DIR 를 기준으로 동작하므로 PATH 로 옮기지 말고 그대로 실행한다
```
