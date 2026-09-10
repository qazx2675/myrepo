# vm-network-migration

vSphere 환경에서 VM 의 네트워크 **포트 그룹(Port Group)** 을 일괄 변경하고, 실패 시
작업 전 상태로 자동 복구(롤백)하는 도구입니다.

제어 계층(Bash `run.sh`)과 실행 계층(Go 바이너리 6종)을 분리했고, 전 과정은
Worker Pool 로 병렬 처리합니다. 모든 단계는 멱등(중복 실행해도 부작용 없음)입니다.

> ⚠️ **이 도구는 실제로 VM 의 네트워크 설정을 변경(write)합니다.**

---

## ⚠️ 주의사항 (Disclaimer)

본 로그 분석 관련 스크립트 및 툴은 **100% 신뢰하기보다는 참고용(보조 도구)으로
사용하는 것을 권장**합니다.

이 도구는 설정 변경 스크립트이므로, **설정 변경 후 반드시 랜덤한 서버 몇 대를 골라
vCenter 에서 직접 확인**해서 의도한 포트그룹으로 실제로 변경되었는지 눈으로
검증하십시오. Step 4 자동 검증을 통과했다는 것이 곧 서비스 정상을 뜻하지는
않습니다 — 이 도구는 게스트 OS 내부의 IP/라우팅/통신 상태는 확인하지 않습니다.

---

## 1. 빌드 및 설치 방법

### 1.1 요구사항

| 항목 | 값 |
|---|---|
| Go | 1.26.5 이상 (`go.mod` 기준) |
| OS | Linux (Rocky Linux 8 에서 검증) |
| 네트워크 | vCenter 443/tcp 접근 |

### 1.2 빌드 (폐쇄망 지원)

의존성이 `vendor/` 에 모두 포함되어 있어, **이 폴더를 통째로 내려받으면 인터넷 없이
빌드**됩니다. 별도의 `go mod download` 나 프록시 설정이 필요 없습니다.

```bash
git clone <저장소 주소>
cd <저장소>/.claude/VM/vm-network-migration
bash setup.sh
```

`setup.sh` 는 `GOFLAGS=-mod=vendor`, `GOPROXY=off` 를 강제하므로 인터넷을 조회하지
않습니다. 빌드가 끝나면 `bin/` 아래에 6개 바이너리가 생깁니다.

```
bin/nm-backup  bin/nm-pgcreate  bin/nm-disconnect
bin/nm-connect bin/nm-verify    bin/nm-rollback
```

### 1.3 입력 파일 준비

저장소에는 `*.example` 파일만 들어 있습니다. 확장자를 떼고 실제 값으로 채우십시오.
(`{user}` 는 작업 단위를 구분하는 임의의 토큰입니다. 예: `hong`)

```bash
cp vcenter.txt.example      vcenter.txt
cp user.txt.example         hong.txt
cp vswitch_user.txt.example vswitch_hong.txt
```

### 1.4 자격증명

계정 ID 는 `-id`(`run.sh` 는 `--id`)로 지정하고, **비밀번호만 환경변수로** 넘깁니다.
비밀번호를 명령행에 적으면 셸 히스토리와 프로세스 목록(`ps`)에 남기 때문입니다.

```bash
read -rsp 'vCenter 비밀번호: ' VC_PASSWORD; export VC_PASSWORD; echo
```

`-id` 를 주지 않으면 **`lscsystems@vsphere.local`** 로 접속합니다.
다른 계정을 쓰려면 지정하십시오.

```bash
./run.sh -u hong --id administrator@vsphere.local
```

비밀번호 환경변수는 `VC_PASSWORD` 가 기본이고, `VC_PASS` / `VCENTER_PASS` 도
대체 이름으로 인식합니다.

### 1.5 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH 에 포함된 디렉터리로 옮기거나, 실행 파일이 있는 경로를
PATH 에 추가하면 어디서든 명령어처럼 사용할 수 있습니다.

```bash
sudo cp bin/nm-* /usr/local/bin/
# 이후 어느 위치에서나 실행 가능
nm-backup -user=hong
```

단, `run.sh` 는 자기 위치 기준으로 `bin/` 을 찾고 입력 파일은 **현재 디렉터리**에서
읽으므로, 전체 워크플로우를 돌릴 때는 프로젝트 폴더 안에서 실행하십시오.

---

## 2. 사용 방법

### 2.1 통합 실행 (권장)

`run.sh` 한 번이면 [백업 → 생성 → 해제 → 연결 → 검증] 전 과정이 수행되고,
실패 시 자동 롤백까지 처리됩니다.

```bash
# 1) 먼저 dry-run 으로 무엇이 바뀔지 확인 (아무것도 변경하지 않습니다)
./run.sh -u hong --dry-run

# 2) 실제 실행
./run.sh -u hong
```

`-u` 를 생략하면 `vswitch_*.txt` 목록에서 대화형으로 고릅니다.

### 2.2 실행 순서

계획서의 단계 번호를 유지하되, **포트그룹 생성(Step 2)을 연결 해제(Step 1)보다 먼저**
돌립니다. 해제~연결 사이의 네트워크 단절 구간을 최대한 짧게 만들기 위해서입니다.

| 순서 | 단계 | 바이너리 | 하는 일 |
|---|---|---|---|
| 1 | Step 0 | `nm-backup` | 현재 NIC 상태를 `state_{user}.json` 에 백업 |
| 2 | Step 2 | `nm-pgcreate` | 각 BM 호스트 vSwitch 에 신규 포트그룹 생성 |
| 3 | Step 1 | `nm-disconnect` | 대상 VM 의 NIC 연결 해제 |
| 4 | Step 3 | `nm-connect` | 신규 포트그룹으로 NIC 백킹 교체 후 재연결 |
| 5 | Step 4 | `nm-verify` | 인벤토리를 다시 읽어 실제 반영 여부 검증 |
| — | 롤백 | `nm-rollback` | 실패 시 (3-Undo → 1-Undo) 순으로 원복 |

### 2.3 단계별 개별 실행

각 바이너리는 독립 실행이 가능하고 플래그 이름이 모두 같습니다.

```bash
./bin/nm-backup     -user=hong
./bin/nm-pgcreate   -user=hong -target-vswitch=vSwitch0
./bin/nm-disconnect -user=hong
./bin/nm-connect    -user=hong
./bin/nm-verify     -user=hong
```

### 2.4 롤백

```bash
./run.sh -u hong --rollback          # 상태 파일의 전체 VM 원복
./bin/nm-rollback -user=hong -vm=VM1 # 특정 VM 만 원복
./bin/nm-rollback -user=hong -only-file=failed_hong.txt  # 실패한 VM 만 원복
```

### 2.5 중간 실패 후 재실행

한 번이라도 실행하면 `state_{user}.json` 이 남습니다. 이 파일이 **작업 전 원본 기록**
이므로 함부로 덮어쓰면 안 됩니다. 재실행 시 `run.sh` 가 어떻게 할지 물어봅니다.

| 선택 | 플래그 | 의미 |
|---|---|---|
| 이어서 진행 | `--resume` | 기존 백업을 그대로 쓰고 Step 0 을 건너뜁니다 |
| 새로 백업 | `--force-backup` | **현재** 상태를 원본으로 덮어씁니다 (원본 기록 소실) |

> 이미 일부 VM 이 변경된 상태에서 `--force-backup` 을 쓰면 "변경된 상태"가 원본으로
> 기록되어 롤백이 무의미해집니다. 원복이 목적이라면 `--rollback` 을 먼저 쓰십시오.

### 2.6 부분 실패 시 동작

일부 VM 만 실패하면 **실패한 VM 만 원복**한 뒤 상태 파일에서 제외하고, **나머지 VM
으로 다음 단계를 계속 진행**합니다. 여기서 통째로 멈추면 이미 연결이 끊긴(Step 1
성공) VM 들이 신규 포트그룹에 붙지 못한 채 네트워크가 죽은 상태로 남기 때문입니다.

이 경우 `run.sh` 는 종료 코드 **1** 과 함께 되돌린 VM 을 알려줍니다.

### 2.7 출력 색상

`nm-*` 바이너리는 `[Step N]` 제목과 건별 결과(성공/스킵/실패/예정)에 ANSI 색상을
입힙니다. 표준 출력이 터미널이 아니거나(파일/파이프로 리다이렉트) `NO_COLOR`
환경변수가 설정돼 있으면 자동으로 꺼집니다.

```bash
NO_COLOR=1 ./bin/nm-backup -user=hong   # 색 없이 실행
```

### 2.8 폐쇄망 배포 시 로컬 확장판 (`portgroup_change.sh`)

일부 폐쇄망 배포 환경에서는 `run.sh` 를 **`portgroup_change.sh`** 로 이름을 바꾸고
현지 운영 절차에 맞춘 편의 기능을 추가해 씁니다. **이 저장소에는 그 로컬 확장
코드가 포함되어 있지 않습니다** — 폐쇄망 환경에 직접 추가된 것으로, 이 문서에는
동작 개요만 기록해 둡니다.

- 실행하면(`bash portgroup_change.sh`) 대상 사용자를 대화형으로 고르는 것은
  `run.sh` 와 동일합니다.
- 고른 사용자 값으로 `{user}.txt` / `vswitch_${user}.txt` 를 참조하는 것도 동일합니다.
- **추가 기능 1 — VLAN 자동 0 처리**: BM(ESXi 호스트)과 VM 이 같은 네트워크 대역에
  있으면 VLAN 값을 자동으로 `0`(태깅 없음)으로 바꿔줍니다. "같은 대역"을 정확히
  어떤 기준(서브넷 마스크 등)으로 판정하는지는 이 저장소가 관리하는 부분이 아니라
  기록하지 않습니다.
- **추가 기능 2 — 입력 형식 자동 변환**: `vswitch_${user}.txt` 는 원래
  `<BM호스트명> <포트그룹명> <VLAN>` 3컬럼을 요구하지만(§4.2), 이 확장판은
  `<BM호스트명> <VM_IP> <VLAN>` 형식으로 입력해도 필요한 형식으로 자동 변환해
  줍니다. 포트그룹명을 정확히 어떤 규칙으로 만들어내는지는 이 저장소가 관리하는
  부분이 아니라 기록하지 않습니다.
- 그 외 실행 방식(단계 순서, 롤백, dry-run, 출력 등)은 `run.sh` 와 동일합니다 —
  §2.1~§2.7 을 그대로 참고하십시오.

이 저장소의 `run.sh` 자체는 위 두 기능을 포함하지 않습니다. `run.sh` 를 직접
쓰는 환경이라면 `vswitch_{user}.txt` 는 항상 3컬럼(BM호스트/포트그룹명/VLAN)
형식이어야 합니다 (§4.2 예시 참고).

---

## 3. 옵션별 상세 설명

### 3.1 `run.sh` 옵션

| 옵션 | 기본값 | 설명 |
|---|---|---|
| `-u`, `--user <토큰>` | (대화형 선택) | 작업 단위 토큰. 나머지 파일 이름을 결정합니다. |
| `--id <계정>` | `lscsystems@vsphere.local` | vCenter 로그인 계정 ID. |
| `-c`, `--concurrency <N>` | `8` | 동시에 처리할 VM 수. vCenter 부하를 조절합니다. |
| `--nic-index <N>` | `0` | 대상 가상 NIC 순번. `0` = 네트워크 어댑터 1. |
| `--vswitch <이름>` | `vSwitch0` | 포트그룹을 만들 대상 표준 가상 스위치. |
| `--dry-run` | (꺼짐) | 실제 변경 없이 무엇이 바뀔지만 출력. 별도 임시 상태 파일을 쓰고 끝나면 지웁니다. |
| `-y`, `--yes` | (꺼짐) | 확인 프롬프트를 건너뜁니다. 자동화용. |
| `--rollback` | (꺼짐) | 마이그레이션 없이 롤백만 수행합니다. |
| `--resume` | (꺼짐) | Step 0 을 건너뛰고 기존 상태 파일을 씁니다. |
| `--force-backup` | (꺼짐) | 기존 상태 파일을 현재 상태로 덮어씁니다. |
| `-h`, `--help` | — | 도움말. |

### 3.2 Go 바이너리 공통 옵션

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-user` | (필수) | 작업 단위 토큰. 아래 파일 이름을 결정합니다. |
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 ID. |
| `-vcenter-file` | `vcenter.txt` | vCenter 주소 목록 (한 줄에 하나). |
| `-state-file` | `state_{user}.json` | 롤백용 상태 파일. `run.sh` 가 dry-run 일 때 임시 경로로 돌립니다. |
| `-nic-index` | `0` | 대상 NIC 순번. |
| `-concurrency` | `8` | 동시 처리 수. |
| `-dry-run` | `false` | 변경 없이 예정 내용만 출력. |

`{user}.txt`(대상 VM), `vswitch_{user}.txt`(신규 네트워크 설정),
`failed_{user}.txt`(실패 목록)는 `-user` 에서 파생되며 따로 지정할 수 없습니다.
전체 작업 제한 시간은 30분 고정입니다.

### 3.3 바이너리별 고유 옵션

| 바이너리 | 플래그 | 기본값 | 설명 |
|---|---|---|---|
| `nm-backup` | `-force` | `false` | 상태 파일이 이미 있어도 덮어씁니다. |
| `nm-pgcreate` | `-target-vswitch` | `vSwitch0` | 포트그룹을 만들 표준 가상 스위치. |
| `nm-rollback` | `-vm` | — | 이 VM 만 롤백. 여러 번 지정 가능. |
| `nm-rollback` | `-only-file` | — | 롤백할 VM 목록 파일 (보통 `failed_{user}.txt`). |
| `nm-rollback` | `-prune` | `false` | 원복 성공한 VM 을 상태 파일에서 제외합니다. |

### 3.4 종료 코드

| 코드 | 의미 |
|---|---|
| `0` | 전부 성공 (또는 이미 원하는 상태라 변경 불필요) |
| `1` | 일부/전부 실패. `run.sh` 는 이 코드를 보고 롤백을 시작합니다. |
| `2` | 설정/입력 오류. **VM 을 하나도 건드리지 않았습니다.** |
| `3` | (`run.sh` 전용) 자동 롤백까지 실패. **수동 확인 필요.** |

---

## 4. 문서별 설명

### 4.1 파일 구성

| 파일 | 역할 |
|---|---|
| `README.md` | 이 문서 (빌드/사용/옵션) |
| `ARCHITECTURE.md` | 폴더·파일별 역할 표. 어디를 고쳐야 할지 찾을 때 |
| `CHANGELOG.md` | 날짜순 변경 기록 (최신이 위) |
| `PR_CHECKLIST.md` | 배포/수정 전 확인 목록 |
| `run.sh` | 전체 워크플로우 제어 + 자동 롤백 (사용자 진입점) |
| `setup.sh` | 폐쇄망 오프라인 빌드 |
| `cmd/` | 단계별 Go 바이너리 소스 |
| `internal/` | 공유 패키지 (설정/상태/vSphere/워커풀) |
| `vendor/` | 빌드에 필요한 Go 의존성 (서드파티, 문서화 대상 제외) |
| `*.example` | 입력 파일 서식 예시 |

### 4.2 데이터 파일

| 파일 | 생성 | 내용 |
|---|---|---|
| `vcenter.txt` | 수동 | 대상 vCenter 주소 목록 |
| `{user}.txt` | 수동 | 마이그레이션 대상 VM 이름 목록 |
| `vswitch_{user}.txt` | 수동 | `<BM호스트> <포트그룹> <VLAN>` |
| `state_{user}.json` | 자동 | **롤백용 원본 상태.** 지우면 원복 불가 |
| `failed_{user}.txt` | 자동 | 직전 단계에서 실패한 VM 이름 |
| `rollback_failed_{user}.txt` | 자동 | 롤백까지 실패한 VM. 수동 확인 대상 |

`vswitch_{user}.txt` 예시:

```
# <BM호스트명>  <포트그룹명>  <VLAN ID>
BM1hostname.domain  WEB_PORTGROUP_100  100
BM2hostname.domain  DB_PORTGROUP_200   200
```

> **1번 컬럼은 VM 이름이 아니라 BM(ESXi 호스트) 이름입니다.** 포트그룹은 호스트의
> vSwitch 위에 만들어지므로 생성 단위가 호스트이고, 어떤 VM 이 어느 포트그룹으로
> 갈지는 "그 VM 이 올라가 있는 호스트"(`vm.runtime.host`)로 결정됩니다.
> 이관 대상 VM 이 있는 호스트는 반드시 **한 줄만** 적으십시오.

### 4.3 상태 파일과 롤백

`state_{user}.json` 은 VM 별로 아래를 기록합니다. UUID 를 함께 남기므로 VM 이름이
바뀌어도 다시 찾을 수 있습니다.

```json
{
  "vm_name": "192ev01",
  "vm_uuid": "4217f2f9-...",
  "vcenter": "192.168.0.50",
  "bm_host": "192.168.0.59",
  "nic_key": 4000,
  "orig_portgroup": "test_hostgroup_123",
  "orig_connected": false,
  "orig_start_connected": true,
  "target_portgroup": "WEB_PORTGROUP_100",
  "target_vlan": 100
}
```

롤백은 계획서의 역순 실행을 따릅니다.

1. **(Step 3-Undo)** 신규 포트그룹 연결을 해제
2. **(Step 1-Undo)** `orig_portgroup` 으로 백킹을 되돌리고 원래 연결 상태를 복원

**생성한 포트그룹은 지우지 않습니다.** 다른 VM 이 이미 그 포트그룹을 쓰고 있을 수
있고, 비어 있는 포트그룹이 남는 것은 무해하기 때문입니다.

---

## 5. 알려진 한계

- **게스트 내부는 확인하지 않습니다.** Step 4 는 vCenter 인벤토리 기준으로 NIC 의
  포트그룹과 연결 상태, 그리고 그 포트그룹이 호스트에 실제로 존재하는지까지만
  확인합니다. 게스트 OS 의 IP/라우팅/실제 통신 여부는 검증 범위 밖입니다.
- **VM 당 NIC 1장만 다룹니다.** `-nic-index` 로 어느 NIC 인지 고르며, 한 번의 실행에서
  여러 NIC 을 동시에 옮기지는 않습니다.
- **표준 vSwitch 전용입니다.** 포트그룹 생성은 `HostNetworkSystem.AddPortGroup` 을
  쓰므로 분산 스위치(DVS)에는 만들 수 없습니다. 기존 NIC 이 DVS 에 붙어 있는 경우
  백업/원복 시 포트그룹 이름은 읽지만, 이관 대상은 표준 포트그룹입니다.
- **한 호스트에 포트그룹 여러 줄이면 이관 대상에서 제외됩니다.** 어느 포트그룹으로
  옮길지 정할 수 없어 백업 단계에서 해당 VM 을 실패 처리합니다(생성 자체는 여러 줄
  모두 처리됩니다).
- **포트그룹 정책은 vSwitch 기본값을 상속합니다.** `HostNetworkPolicy` 를 빈 값으로
  생성하므로 티밍/보안 정책을 개별 지정하지 않습니다.
- **vCenter 는 존재하지 않는 포트그룹 이름도 NIC 설정에 받아줍니다.** 그래서 Step 4 는
  이름 일치뿐 아니라 호스트에 실제 포트그룹이 있는지도 확인합니다. 다만 이 검증은
  Step 4 를 돌려야만 수행되므로, 개별 바이너리만 실행했다면 반드시 `nm-verify` 를
  마지막에 돌리십시오.

---

## 6. 검증 이력

Rocky Linux 8 / go1.26.5 / vCenter 8 (govmomi v0.55.1) 환경에서 확인했습니다.

- 빈 모듈 캐시 + `GOPROXY=off` 로 **폐쇄망 오프라인 빌드** 성공
- `gofmt` / `go vet` / `go build` 통과
- 실제 vCenter 대상으로 백업 → 생성 → 해제 → 연결 → 검증 전 과정 성공
- 재실행 시 전 단계 **멱등**(스킵) 동작 확인
- 전체 롤백 / 특정 VM 선택 롤백 후 포트그룹·`connected`·`startConnected` 가
  백업값과 정확히 일치함을 독립 조회로 확인
- 부분 실패 주입 시 **실패분만 원복 → 상태 파일에서 제외 → 나머지로 계속 진행** 확인
- 자동 롤백까지 실패하는 경우 종료 코드 3 + 수동 확인 안내 확인
- 비밀번호/`-user` 누락, `-id` 빈 값이면 vCenter 접속 전에 종료 코드 2 로 중단 확인
- `-id` 미지정 시 기본 계정 `lscsystems@vsphere.local` 로 접속하고, `-id` 지정 시
  해당 계정으로 접속함을 실 vCenter 로 확인

자세한 내용은 [`CHANGELOG.md`](CHANGELOG.md) 참고.
