# 14. vm-network-migration — VM 네트워크 포트그룹 일괄 이관 + 자동 롤백

> ⚠️ **이 폴더는 현재 수정이 진행 중입니다.** 기능 추가·변경 요청이 언제든 들어올 수 있는 상태이며,
> 요청 시 [31_변경요청서_양식](./31_변경요청서_양식.md)에 맞춰 요청하면 수정이 가능합니다.
> 마지막 변경: **2026-09-02** (vCenter 계정 지정을 `-id` 플래그로 변경).
> 수정 후에는 반드시 `CHANGELOG.md`와 **이 문서**를 함께 갱신하세요 — 자세한 절차는 아래 12절.

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🔴 **실제로 VM의 네트워크 설정을 변경합니다** |
| 폴더 | `.claude/VM/vm-network-migration/` |
| 바이너리 | `bin/nm-backup`, `nm-pgcreate`, `nm-disconnect`, `nm-connect`, `nm-verify`, `nm-rollback` (6개) |
| 진입점 | **`./run.sh`** (전 과정 제어 + 자동 롤백) |
| 인증 | 계정은 `-id` 플래그, 비밀번호는 `VC_PASSWORD` 환경변수 |
| 대표 명령 | `./run.sh -u hong --dry-run` → `./run.sh -u hong` |
| 검증 환경 | Rocky Linux 8 / go1.26.5 / vCenter 8 (govmomi v0.55.1) |

---

## 1. 이 도구는 무엇인가

VM이 붙어 있는 **네트워크 포트그룹을 다른 포트그룹으로 한꺼번에 바꾸는** 도구입니다. 네트워크 대역 개편, VLAN 변경, 신규 네트워크 이관 같은 작업에 씁니다.

네트워크 변경은 **실패하면 VM이 통신 불능**이 되는 위험한 작업입니다. 그래서 이 도구는:

- 작업 전 상태를 **JSON 파일에 백업**하고
- 실패하면 **자동으로 원래 상태로 되돌리고**
- 모든 단계가 **멱등**(중복 실행해도 부작용 없음)이며
- **제어 계층(`run.sh`)과 실행 계층(Go 바이너리 6종)을 분리**했습니다.

## 2. 실행 순서 (왜 이 순서인가)

```
Step 0  nm-backup     현재 NIC 상태를 state_{user}.json 에 백업
   ↓
Step 2  nm-pgcreate   각 BM 호스트 vSwitch에 신규 포트그룹 생성
   ↓
Step 1  nm-disconnect 대상 VM의 NIC 연결 해제
   ↓
Step 3  nm-connect    신규 포트그룹으로 백킹 교체 후 재연결
   ↓
Step 4  nm-verify     인벤토리를 다시 읽어 실제 반영 여부 검증
   ─
롤백    nm-rollback   실패 시 (3-Undo → 1-Undo) 순으로 원복
```

> **단계 번호가 순서와 안 맞는 이유**: 계획서의 단계 번호를 유지하되, **포트그룹 생성(Step 2)을 연결 해제(Step 1)보다 먼저** 돌립니다. 해제~연결 사이의 **네트워크 단절 구간을 최대한 짧게** 만들기 위해서입니다.

---

## 3. 준비물

### 3-1. 빌드

```bash
cd .claude/VM/vm-network-migration
bash setup.sh
```

→ `bin/` 아래에 6개 바이너리가 생성됩니다. `setup.sh`가 `GOFLAGS=-mod=vendor`, `GOPROXY=off`를 강제하므로 인터넷을 조회하지 않습니다.

### 3-2. 입력 파일 준비

저장소에는 `.example` 파일만 있습니다. 확장자를 떼고 실제 값으로 채우세요.
`{user}`는 **작업 단위를 구분하는 임의의 토큰**입니다(예: `hong`).

```bash
cp vcenter.txt.example      vcenter.txt
cp user.txt.example         hong.txt
cp vswitch_user.txt.example vswitch_hong.txt
```

| 파일 | 생성 | 내용 |
|---|---|---|
| `vcenter.txt` | 수동 | 대상 vCenter 주소 목록 (한 줄에 하나) |
| `{user}.txt` | 수동 | 마이그레이션 대상 **VM 이름** 목록 |
| `vswitch_{user}.txt` | 수동 | `<BM호스트> <포트그룹> <VLAN>` |
| `state_{user}.json` | 자동 | **롤백용 원본 상태. 지우면 원복 불가** |
| `failed_{user}.txt` | 자동 | 직전 단계에서 실패한 VM 이름 |
| `rollback_failed_{user}.txt` | 자동 | 롤백까지 실패한 VM. **수동 확인 대상** |

`vswitch_{user}.txt` 예시:

```
# <BM호스트명>  <포트그룹명>  <VLAN ID>
BM1hostname.domain  WEB_PORTGROUP_100  100
BM2hostname.domain  DB_PORTGROUP_200   200
```

> ⚠️ **1번 컬럼은 VM 이름이 아니라 BM(ESXi 호스트) 이름입니다.**
> 포트그룹은 호스트의 vSwitch 위에 만들어지므로 생성 단위가 호스트이고, 어떤 VM이 어느 포트그룹으로 갈지는 **그 VM이 올라가 있는 호스트**(`vm.runtime.host`)로 결정됩니다.
> **이관 대상 VM이 있는 호스트는 반드시 한 줄만 적으십시오.** 여러 줄이면 대상을 특정할 수 없어 백업 단계에서 실패 처리됩니다.

### 3-3. 자격증명

```bash
read -rsp 'vCenter 비밀번호: ' VC_PASSWORD; export VC_PASSWORD; echo
```

- 계정 ID는 **`-id`(`run.sh`는 `--id`)** 로 지정합니다. 안 주면 **`lscsystems@vsphere.local`** 로 접속합니다.
- 비밀번호 환경변수는 `VC_PASSWORD`가 기본이고, `VC_PASS` / `VCENTER_PASS`도 인식합니다.

```bash
./run.sh -u hong --id administrator@vsphere.local
```

---

## 4. 사용 방법

### 4-1. 통합 실행 (권장)

```bash
# 1) 먼저 dry-run — 아무것도 변경하지 않고 무엇이 바뀔지만 출력
./run.sh -u hong --dry-run

# 2) 실제 실행
./run.sh -u hong
```

`-u`를 생략하면 `vswitch_*.txt` 목록에서 대화형으로 고릅니다.

### 4-2. 단계별 개별 실행

각 바이너리는 독립 실행 가능하고 플래그 이름이 전부 같습니다.

```bash
./bin/nm-backup     -user=hong
./bin/nm-pgcreate   -user=hong -target-vswitch=vSwitch0
./bin/nm-disconnect -user=hong
./bin/nm-connect    -user=hong
./bin/nm-verify     -user=hong
```

> 개별 바이너리만 실행했다면 **반드시 마지막에 `nm-verify`를 돌리십시오.** vCenter는 존재하지 않는 포트그룹 이름도 NIC 설정에 받아주기 때문에, "호스트에 그 포트그룹이 실제로 있는지" 확인은 Step 4에서만 합니다.

### 4-3. 롤백

```bash
./run.sh -u hong --rollback                              # 상태 파일의 전체 VM 원복
./bin/nm-rollback -user=hong -vm=VM1                     # 특정 VM만 원복
./bin/nm-rollback -user=hong -only-file=failed_hong.txt  # 실패한 VM만 원복
```

### 4-4. 중간 실패 후 재실행

한 번이라도 실행하면 `state_{user}.json`이 남습니다. **이 파일이 작업 전 원본 기록**이므로 함부로 덮어쓰면 안 됩니다. 재실행하면 `run.sh`가 어떻게 할지 물어봅니다.

| 선택 | 플래그 | 의미 |
|---|---|---|
| 이어서 진행 | `--resume` | 기존 백업을 그대로 쓰고 Step 0을 건너뜀 |
| 새로 백업 | `--force-backup` | **현재** 상태를 원본으로 덮어씀 (**원본 기록 소실**) |

> ⚠️ 이미 일부 VM이 변경된 상태에서 `--force-backup`을 쓰면 **"변경된 상태"가 원본으로 기록되어 롤백이 무의미해집니다.** 원복이 목적이라면 `--rollback`을 먼저 쓰십시오.

### 4-5. 부분 실패 시 동작

일부 VM만 실패하면 **실패한 VM만 원복**한 뒤 상태 파일에서 제외하고, **나머지 VM으로 다음 단계를 계속 진행**합니다.

여기서 통째로 멈추면 이미 연결이 끊긴(Step 1 성공) VM들이 신규 포트그룹에 붙지 못한 채 **네트워크가 죽은 상태로 남기** 때문입니다. 이 경우 `run.sh`는 종료 코드 **1**과 함께 되돌린 VM을 알려줍니다.

---

## 5. 옵션 상세표

### 5-1. `run.sh` 옵션

| 옵션 | 기본값 | 설명 |
|---|---|---|
| `-u`, `--user <토큰>` | (대화형 선택) | 작업 단위 토큰. 나머지 파일 이름을 결정 |
| `--id <계정>` | `lscsystems@vsphere.local` | vCenter 로그인 계정 ID |
| `-c`, `--concurrency <N>` | `8` | 동시에 처리할 VM 수. vCenter 부하 조절 |
| `--nic-index <N>` | `0` | 대상 가상 NIC 순번. `0` = 네트워크 어댑터 1 |
| `--vswitch <이름>` | `vSwitch0` | 포트그룹을 만들 대상 표준 가상 스위치 |
| `--dry-run` | (꺼짐) | **실제 변경 없이** 무엇이 바뀔지만 출력. 별도 임시 상태 파일을 쓰고 끝나면 지움 |
| `-y`, `--yes` | (꺼짐) | 확인 프롬프트 건너뜀. 자동화용 |
| `--rollback` | (꺼짐) | 마이그레이션 없이 롤백만 수행 |
| `--resume` | (꺼짐) | Step 0을 건너뛰고 기존 상태 파일 사용 |
| `--force-backup` | (꺼짐) | 기존 상태 파일을 현재 상태로 덮어씀 |
| `-h`, `--help` | — | 도움말 |

### 5-2. Go 바이너리 공통 옵션

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-user` | (필수) | 작업 단위 토큰 |
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 ID |
| `-vcenter-file` | `vcenter.txt` | vCenter 주소 목록 |
| `-state-file` | `state_{user}.json` | 롤백용 상태 파일 (`run.sh`가 dry-run일 때 임시 경로로 돌림) |
| `-nic-index` | `0` | 대상 NIC 순번 |
| `-concurrency` | `8` | 동시 처리 수 |
| `-dry-run` | `false` | 변경 없이 예정 내용만 출력 |

> `{user}.txt`(대상 VM), `vswitch_{user}.txt`(신규 네트워크 설정), `failed_{user}.txt`(실패 목록)는 `-user`에서 파생되며 **따로 지정할 수 없습니다.**
> 전체 작업 제한 시간은 **30분 고정**입니다.

### 5-3. 바이너리별 고유 옵션

| 바이너리 | 플래그 | 기본값 | 설명 |
|---|---|---|---|
| `nm-backup` | `-force` | `false` | 상태 파일이 이미 있어도 덮어씀 |
| `nm-pgcreate` | `-target-vswitch` | `vSwitch0` | 포트그룹을 만들 표준 가상 스위치 |
| `nm-rollback` | `-vm` | — | 이 VM만 롤백. 여러 번 지정 가능 |
| `nm-rollback` | `-only-file` | — | 롤백할 VM 목록 파일 (보통 `failed_{user}.txt`) |
| `nm-rollback` | `-prune` | `false` | 원복 성공한 VM을 상태 파일에서 제외 |

### 5-4. 종료 코드

| 코드 | 의미 | 대처 |
|---|---|---|
| `0` | 전부 성공 (또는 이미 원하는 상태라 변경 불필요) | 표본 확인 |
| `1` | 일부/전부 실패. `run.sh`는 이 코드를 보고 롤백을 시작 | 로그와 `failed_{user}.txt` 확인 |
| `2` | 설정/입력 오류. **VM을 하나도 건드리지 않았습니다** | 입력 파일/비밀번호/`-id` 확인 |
| `3` | (`run.sh` 전용) **자동 롤백까지 실패. 수동 확인 필요** | `rollback_failed_{user}.txt`의 VM을 vSphere Client에서 직접 확인 |

---

## 6. 상태 파일과 롤백

`state_{user}.json`은 VM별로 아래를 기록합니다. **UUID를 함께 남기므로 VM 이름이 바뀌어도 다시 찾을 수 있습니다.**

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

롤백 순서:

1. **(Step 3-Undo)** 신규 포트그룹 연결을 해제
2. **(Step 1-Undo)** `orig_portgroup`으로 백킹을 되돌리고 원래 연결 상태를 복원

> **생성한 포트그룹은 지우지 않습니다.** 다른 VM이 이미 그 포트그룹을 쓰고 있을 수 있고, 비어 있는 포트그룹이 남는 것은 무해하기 때문입니다.

---

## 7. 자주 나는 오류와 해결

| 증상 | 원인 | 해결 |
|---|---|---|
| 종료 코드 2로 즉시 중단 | 비밀번호 미설정, `-user` 누락, `-id` 빈 값 | vCenter 접속 전에 막힌 것. **VM은 하나도 안 건드림**. 입력 확인 |
| 특정 VM이 백업 단계에서 실패 | 그 VM이 있는 호스트가 `vswitch_{user}.txt`에 **여러 줄** 있음 | 호스트당 한 줄만 남기기 |
| 종료 코드 3 | 자동 롤백까지 실패 | `rollback_failed_{user}.txt`를 열어 해당 VM을 **수동으로** vSphere Client에서 원복 |
| 재실행 시 무엇을 할지 물어봄 | `state_{user}.json`이 남아 있음 | 원복이 목적이면 `--rollback`, 이어서 하려면 `--resume`. `--force-backup`은 신중히 |
| 포트그룹은 생겼는데 VM이 안 옮겨짐 | 분산 스위치(DVS) 대상이거나 nic-index가 다름 | 표준 vSwitch만 지원. `--nic-index` 확인 |
| 검증(Step 4)은 통과했는데 통신 안 됨 | 게스트 OS 내부 IP/라우팅 문제 | 이 도구의 범위 밖. 게스트에서 확인 |

---

## 8. 주의사항과 한계

- 🔴 **설정 변경 후 랜덤한 서버 몇 대를 vSphere Client에서 직접 확인**하십시오. Step 4 검증 통과가 곧 서비스 정상을 뜻하지는 않습니다.
- **게스트 내부는 확인하지 않습니다.** Step 4는 vCenter 인벤토리 기준으로 NIC의 포트그룹과 연결 상태, 그 포트그룹이 호스트에 실제로 존재하는지까지만 봅니다.
- **VM당 NIC 1장만 다룹니다.** `-nic-index`로 어느 NIC인지 고르며, 한 번의 실행에서 여러 NIC을 동시에 옮기지 않습니다.
- **표준 vSwitch 전용입니다.** 포트그룹 생성은 `HostNetworkSystem.AddPortGroup`을 쓰므로 **분산 스위치(DVS)에는 만들 수 없습니다.**
- **한 호스트에 포트그룹 여러 줄이면 그 호스트의 VM은 이관 대상에서 제외**됩니다(포트그룹 생성 자체는 여러 줄 모두 처리).
- **포트그룹 정책은 vSwitch 기본값을 상속합니다.** 티밍/보안 정책을 개별 지정하지 않습니다.
- vCenter는 **존재하지 않는 포트그룹 이름도 NIC 설정에 받아줍니다.** 그래서 `nm-verify`를 반드시 마지막에 돌려야 합니다.

---

## 9. 검증 이력

Rocky Linux 8 / go1.26.5 / vCenter 8 (govmomi v0.55.1) 환경에서 확인:

- 빈 모듈 캐시 + `GOPROXY=off`로 **폐쇄망 오프라인 빌드** 성공
- `gofmt` / `go vet` / `go build` 통과
- 실 vCenter 대상 백업 → 생성 → 해제 → 연결 → 검증 전 과정 성공
- 재실행 시 전 단계 **멱등**(스킵) 동작 확인
- 전체 롤백 / 특정 VM 선택 롤백 후 포트그룹·`connected`·`startConnected`가 백업값과 정확히 일치함을 독립 조회로 확인
- 부분 실패 주입 시 **실패분만 원복 → 상태 파일에서 제외 → 나머지로 계속 진행** 확인
- 자동 롤백까지 실패하는 경우 종료 코드 3 + 수동 확인 안내 확인
- 비밀번호/`-user` 누락, `-id` 빈 값이면 vCenter 접속 전에 종료 코드 2로 중단 확인

---

## 10. 파일 구조

```
vm-network-migration/
├── README.md            # 1차 자료 (빌드/사용/옵션)
├── ARCHITECTURE.md      # 폴더·파일별 역할 표 ← 어디를 고쳐야 할지 찾을 때
├── CHANGELOG.md         # 날짜순 변경 기록 (최신이 위) ← 수정 시 반드시 갱신
├── PR_CHECKLIST.md      # 배포/수정 전 확인 목록 ← 수정 후 반드시 확인
├── run.sh               # ★ 전체 워크플로우 제어 + 자동 롤백 (사용자 진입점)
├── setup.sh             # 폐쇄망 오프라인 빌드
├── cmd/                 # 단계별 Go 바이너리 소스
│   ├── backup/     nm-backup     (Step 0)
│   ├── pgcreate/   nm-pgcreate   (Step 2)
│   ├── disconnect/ nm-disconnect (Step 1)
│   ├── connect/    nm-connect    (Step 3)
│   ├── verify/     nm-verify     (Step 4)
│   └── rollback/   nm-rollback   (롤백)
├── internal/
│   ├── cli/         # 공통 플래그 파싱
│   ├── config/      # 설정/입력 파일 로딩, 비밀번호 읽기
│   ├── pool/        # 워커풀 (동시 실행 제한)
│   ├── state/       # state_{user}.json 읽기/쓰기
│   ├── steps/       # 단계별 공통 로직
│   └── vsphere/     # vCenter 세션, NIC 조작
├── .github/workflows/ci.yml  # CI (gofmt/vet/build/test)
├── *.example        # 입력 파일 서식 예시
└── vendor/          # 의존성 (건드리지 말 것)
```

---

## 11. 관련 문서

- 1차 자료: `vm-network-migration/README.md`
- 파일별 역할: `vm-network-migration/ARCHITECTURE.md`
- 변경 이력: `vm-network-migration/CHANGELOG.md`
- 수정 전 확인 목록: `vm-network-migration/PR_CHECKLIST.md`
- 포트그룹 생성만 따로 하려면: [10. VM_setup](./10_VM_setup.md)의 `vswitch_setting-source`

---

## 12. 이 폴더를 수정할 때 (진행 중 폴더 전용 절차)

이 폴더는 **계속 수정될 것을 전제로** 만들어져 있습니다. 수정 요청이 들어오면 아래 순서를 따르세요.

### 12-1. 요청하기

[31_변경요청서_양식](./31_변경요청서_양식.md)을 복사해 채웁니다. 이 폴더용으로 미리 채워진 값:

```markdown
## 변경 요청

- 대상 저장소: qazx2675/myrepo
- 대상 경로: .claude/VM/vm-network-migration
- 참고 문서: README.md → ARCHITECTURE.md → CHANGELOG.md → PR_CHECKLIST.md (이 순서로 읽고 시작)

### 요청 내용
(무엇을 바꾸고 싶은지 한두 문장)

### 제약 / 유지할 것
- 롤백 안전장치(state 파일 기반 원복, 부분 실패 시 실패분만 원복 후 계속 진행)는 건드리지 말 것
- 종료 코드 규약(0/1/2/3)을 바꾸지 말 것
- 오프라인 빌드 유지 — 새 외부 의존성 추가 금지 (vendor/ 사용 중)
- 6개 바이너리의 플래그 이름 일관성 유지
- run.sh 와 Go 바이너리의 역할 분리 유지 (제어는 run.sh, 실행은 바이너리)

### 완료 기준
- gofmt / go vet / go build / go test ./... 전부 통과
- vcsim 또는 실 vCenter로 최소 1개 시나리오 재현 검증 (dry-run → 실행 → 롤백)
- CHANGELOG.md 에 오늘 날짜로 항목 추가
- PR_CHECKLIST.md 항목 전부 확인
- README.md 및 인수인계 문서 14번의 옵션표/한계 절 갱신
```

### 12-2. 수정 후 반드시 할 일

1. `CHANGELOG.md`에 **최신이 위**로 항목 추가 (형식은 기존 항목 그대로)
2. `PR_CHECKLIST.md` 항목 확인
3. `README.md`의 옵션표 갱신
4. **이 문서(14번)의 5절 옵션표와 8절 한계 갱신** — 문서가 두 벌로 갈라지지 않게
5. `python3 build_handbook.py`로 HTML 재생성

### 12-3. 절대 바꾸면 안 되는 것

| 항목 | 이유 |
|---|---|
| `state_{user}.json`의 스키마 | 이미 남아 있는 상태 파일로 롤백이 안 됨 |
| 종료 코드 0/1/2/3의 의미 | `run.sh`의 분기와 운영 자동화가 이 값에 의존 |
| "부분 실패 시 나머지로 계속 진행" 동작 | 여기서 멈추면 연결이 끊긴 VM이 방치됨 |
| 비밀번호를 환경변수로만 받는 것 | 명령행에 쓰면 히스토리/`ps`에 남음 |
| `vendor/` 기반 오프라인 빌드 | 폐쇄망에서 빌드 불가능해짐 |
