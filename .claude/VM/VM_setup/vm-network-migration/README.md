# vm-network-migration

vCenter 에 등록된 여러 VM 의 가상 NIC 를 **지정한 포트그룹으로 일괄 이관**하는 도구입니다.

- 포트그룹을 **삭제하지 않습니다.** 다른 VM 이 쓰고 있어도 안전합니다.
- NIC 를 끊었다 붙이지 않고 **Reconfigure 1회로 백킹(backing)만 교체**하므로 게스트 입장에서는 순단으로 끝납니다.
- 변경 전 상태를 **롤백 CSV 로 자동 저장**하고, `-rollback` 으로 되돌릴 수 있습니다.
- 표준 vSwitch 포트그룹, 분산 스위치(DVS) 포트그룹을 모두 지원합니다.

> **포트그룹 "생성"은 이 도구의 역할이 아닙니다.** 이미 만들어져 있는 포트그룹을 전제로 동작하며,
> 생성이 필요하면 `-pg-cmd` 로 외부 생성 도구를 전체 작업 **전에 딱 한 번** 호출할 수 있습니다.
> 자세한 내용은 7. 알려진 한계 를 보세요.

---

## 1. 빌드 및 설치 방법

`vendor/` 를 함께 담고 있어 **인터넷이 없는 폐쇄망에서도 그대로 빌드**됩니다.
(빌드 중 `GOPROXY=off` 로 외부 접속을 아예 막습니다.)

### 1.1 필요한 것

| 항목 | 버전 | 비고 |
|---|---|---|
| Go | 1.21 이상 (개발/검증은 1.26.5) | 폐쇄망 서버에 미리 설치되어 있어야 합니다 |
| OS | Linux (x86_64) | RHEL/Rocky 8 이상에서 검증 |
| 네트워크 | vCenter 443/tcp 도달 | 인터넷은 필요 없습니다 |

### 1.2 내려받아서 빌드하기

```bash
git clone <저장소 주소>
cd <저장소>/.claude/VM/VM_setup/vm-network-migration
bash setup.sh
```

빌드가 끝나면 같은 폴더에 `vm-network-migration` 실행 파일이 생깁니다.
zip 으로 받은 경우에도 폴더를 통째로 풀고 `bash setup.sh` 만 실행하면 됩니다.

### 1.3 설정 파일 준비

```bash
cp vcenter.txt.example vcenter.txt
chmod 600 vcenter.txt
vi vcenter.txt

cp vmlist.txt.example vmlist.txt
vi vmlist.txt
```

`vcenter.txt` 에는 평문 비밀번호가 들어갑니다. 권한을 반드시 600 으로 좁히세요.
`vcenter.txt` 와 `vmlist.txt` 는 `.gitignore` 에 등록되어 있어 커밋되지 않습니다.

**`vcenter.txt` 형식**

```ini
VCENTER_HOST=vcenter.example.local
VCENTER_USER=administrator@vsphere.local
VCENTER_PASS=ChangeMe!
VCENTER_DATACENTER=Datacenter1
VCENTER_INSECURE=true
```

**`vmlist.txt` 형식** — 한 줄에 VM 하나. 빈 줄과 `#` 주석은 무시하고, 중복은 자동 제거합니다.

```text
vm-app-01
vm-web-02
vm-db-03
```

> 여기 적는 이름은 **게스트 OS 의 hostname 이 아니라 vCenter 인벤토리에 보이는 VM 이름**입니다.
> 둘이 다른 환경이라면 인벤토리 이름 기준으로 목록을 만드세요.

---

## 2. 사용 방법

`run.sh` 는 설정 파일 확인 → (소스가 바뀌었으면) 재빌드 → 실행까지 한 번에 해 주는 래퍼입니다.
넘긴 인자는 그대로 Go 바이너리에 전달됩니다. 바이너리를 직접 실행해도 동일합니다.

### 2.1 먼저 예행 연습 (권장 · 아무것도 바꾸지 않음)

```bash
./run.sh -to-portgroup=PG-NEW-100 -dry-run
```

```
[DRY-RUN] vm-app-01    변경 예정: PG-OLD-010 -> PG-NEW-100 (NIC key=4000, 전원=poweredOn)
[DRY-RUN] vm-web-02    변경 예정: PG-OLD-010 -> PG-NEW-100 (NIC key=4000, 전원=poweredOn)
총 2대 | 성공 0 | 건너뜀 0 | 실패 0 | 예행 2
```

무엇이 어디서 어디로 바뀌는지 확인한 뒤 실제 실행으로 넘어가세요.

### 2.2 실제 이관

```bash
./run.sh -to-portgroup=PG-NEW-100
```

정상 종료하면 두 개의 파일이 생깁니다.

- `report_YYYYmmdd_HHMMSS.csv` — 전체 처리 결과
- `rollback_YYYYmmdd_HHMMSS.csv` — **실제로 바뀐 VM 의 이전 포트그룹.** 되돌릴 때 필요하니 반드시 보관하세요.

### 2.3 NIC 가 여러 개일 때 — 바꿀 NIC 지정

방법 A) "이 포트그룹에 붙어 있는 NIC" 만 바꾸기 (권장)

```bash
./run.sh -to-portgroup=PG-NEW-100 -from-portgroup=PG-OLD-010
```

방법 B) 순번으로 지정 (device key 오름차순, 0 = 첫 번째)

```bash
./run.sh -to-portgroup=PG-NEW-100 -nic-index=1
```

방법 A 는 해당 포트그룹에 붙은 NIC 가 없는 VM 을 `SKIPPED` 로 넘기므로, 대상이 섞여 있어도 안전합니다.

### 2.4 단계적 적용 (카나리 → 확대)

전 VM 을 한 번에 바꾸지 마세요. 목록을 나눠서 단계적으로 진행하는 것을 권장합니다.

```bash
head -1 vmlist.txt > canary.txt
./run.sh -vm-file=canary.txt -to-portgroup=PG-NEW-100
```

카나리 1대를 게스트 접속으로 확인한 뒤 나머지를 진행합니다.

```bash
./run.sh -to-portgroup=PG-NEW-100 -concurrency=8
```

### 2.5 되돌리기 (롤백)

```bash
./run.sh -rollback=rollback_20260901_143000.csv
```

롤백 모드는 CSV 에 기록된 **VM · NIC device key · 이전 포트그룹**을 그대로 복원합니다.
NIC 순번이 아니라 device key 로 찾기 때문에 그 사이 NIC 가 추가/삭제되어도 엉뚱한 NIC 를 건드리지 않습니다.

### 2.6 포트그룹 생성 도구를 함께 호출하기

```bash
./run.sh -to-portgroup=PG-NEW-100 -pg-cmd="/opt/vswitch_setting/setup_pg.sh {{PG}}"
```

`{{PG}}` 가 `-to-portgroup` 값으로 치환되어 **전체 작업 전에 한 번만** 실행됩니다.
VM 마다 실행하지 않습니다. 같은 포트그룹을 동시에 여러 번 만들다 충돌하는 문제를 막기 위해서입니다.
명령이 실패하면 VM 을 한 대도 건드리지 않고 즉시 중단합니다.

### 2.7 종료 코드

| 코드 | 의미 |
|---|---|
| `0` | 전부 성공(또는 건너뜀 / 예행) |
| `1` | 1대 이상 실패, 또는 접속·포트그룹 조회 실패 |
| `2` | 인자·설정 파일 오류 (vCenter 에 접속하기 전에 중단) |

---

## 3. 옵션별 상세 설명

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-to-portgroup` | (없음) | **필수.** 이관할 목표 포트그룹 이름. 실행 전에 존재 여부를 먼저 확인하며, 없으면 VM 을 한 대도 건드리지 않고 종료합니다. `-rollback` 사용 시에는 지정하지 않습니다. |
| `-from-portgroup` | `""` | 지정하면 **이 포트그룹에 연결된 NIC 만** 대상으로 삼습니다. 해당 NIC 가 없는 VM 은 `SKIPPED`. NIC 가 여러 개인 환경에서 가장 안전한 지정 방식입니다. |
| `-nic-index` | `0` | `-from-portgroup` 을 쓰지 않을 때 바꿀 NIC 순번. device key 오름차순이라 실행할 때마다 같은 NIC 를 가리킵니다. |
| `-vm-file` | `vmlist.txt` | 대상 VM 이름 목록 파일. 빈 줄/`#` 주석 무시, 중복 자동 제거. |
| `-vcenter-file` | `vcenter.txt` | vCenter 접속 설정 파일. 필수 항목(HOST/USER/PASS)이 비어 있으면 접속 시도 없이 종료합니다. |
| `-concurrency` | `8` | 동시에 처리할 VM 수. 전 VM 동시 변경은 장애 시 피해가 크고 vCenter 태스크도 몰리므로 기본값을 8로 제한했습니다. 대규모 작업이라도 16~24 를 넘기지 않기를 권장합니다. |
| `-dry-run` | `false` | 실제 변경 없이 어느 VM 의 어느 NIC 가 어디서 어디로 바뀔지만 출력합니다. 리포트 CSV 는 남지만 롤백 파일은 만들지 않습니다. |
| `-rollback` | `""` | 롤백 CSV 경로. 지정하면 롤백 모드로 동작하며 `-to-portgroup` / `-vm-file` 은 무시됩니다. |
| `-pg-cmd` | `""` | 마이그레이션 시작 전 **한 번만** 실행할 외부 명령. `{{PG}}` 가 목표 포트그룹 이름으로 치환됩니다. `/bin/bash -c` 로 실행하고 출력을 그대로 보여줍니다. 실패하면 전체 중단. |
| `-vm-timeout` | `3m` | VM 1대당 최대 처리 시간. 초과하면 그 VM 만 실패 처리되고 나머지는 계속 진행합니다. |
| `-out-dir` | `.` | 리포트/롤백 CSV 를 저장할 디렉터리. |
| `-version` | - | 버전만 출력하고 종료. |

### 3.1 결과 상태값

| 상태 | 의미 | 조치 |
|---|---|---|
| `SUCCESS` | 백킹 교체 + 검증 통과 | 롤백 CSV 에 기록됨 |
| `SKIPPED` | 이미 목표 포트그룹이거나, `-from-portgroup` 에 해당하는 NIC 없음 | 정상. 재실행해도 안전합니다 |
| `FAILED` | VM 못 찾음 / NIC 없음 / Reconfigure 실패 / 검증 불일치 | 리포트 CSV 의 `message` 확인 |
| `DRY-RUN` | 예행 연습 결과 | - |

---

## 4. 동작 순서

```
1) 설정·목록 읽기 (중복 제거)
2) [-pg-cmd 지정 시] 포트그룹 생성 명령 1회 실행
3) vCenter 접속 -> 데이터센터 조회
4) 인벤토리 전체를 훑어 VM 이름 색인 생성 (폴더 깊이 무관, 동명이인 경고)
5) 목표 포트그룹 존재 확인 + 백킹 정보 생성   <- 여기서 실패하면 VM 무손상 종료
6) [동시 N대] VM 별 처리
     - 현재 NIC 와 현재 포트그룹 확인
     - 이미 목표 포트그룹이면 SKIP
     - Reconfigure 1회로 백킹 교체 (전원 On 이면 Connected=true)
     - 하드웨어를 다시 읽어 같은 NIC key 의 포트그룹이 실제로 바뀌었는지 검증
7) 결과 요약 출력 + report CSV + rollback CSV 저장
8) 실패가 1건이라도 있으면 exit 1
```

---

## 5. 문서/파일별 설명

| 파일 | 설명 |
|---|---|
| `README.md` | 이 문서. 빌드 → 사용 → 옵션 순서 |
| `ARCHITECTURE.md` | 소스 파일별 역할. 코드를 고칠 때 어디를 보면 되는지 |
| `PR_CHECKLIST.md` | 배포/수정 전 확인 목록 |
| `CHANGELOG.md` | 날짜순 변경 이력 |
| `setup.sh` | 폐쇄망 오프라인 빌드 (`-mod=vendor`, `GOPROXY=off`) |
| `run.sh` | 실행 편의 래퍼. 설정 확인 → 재빌드 → 실행 → 주의 안내 |
| `main.go` | CLI 진입점. 플래그 파싱, 사전 검증, 병렬 실행, 리포트 저장 |
| `config.go` | `vcenter.txt` / `vmlist.txt` 파싱 |
| `vsphere.go` | vCenter 접속, VM·네트워크 색인, NIC 조회 |
| `migrate.go` | VM 1대의 백킹 교체 + 검증 (핵심 로직) |
| `report.go` | 결과 요약 출력, report/rollback CSV 입출력 |
| `vcenter.txt.example` | 접속 설정 예시 |
| `vmlist.txt.example` | 대상 목록 예시 |
| `vendor/` | 빌드에 필요한 서드파티 패키지 (govmomi 등). 문서화 대상 아님 |

---

## 6. 전역 명령어로 사용하기 (선택 사항)

여러 디렉터리에서 쓰고 싶으면 빌드된 바이너리를 `PATH` 에 넣습니다.

```bash
sudo install -m 0755 vm-network-migration /usr/local/bin/vm-network-migration
```

설치 후 확인:

```bash
vm-network-migration -version
```

전역 설치 시에는 실행하는 **현재 디렉터리**의 `vcenter.txt` / `vmlist.txt` 를 읽습니다.
설정을 한곳에 모아두려면 경로를 명시하세요.

```bash
vm-network-migration -vcenter-file=/etc/vm-migration/vcenter.txt -vm-file=/etc/vm-migration/vmlist.txt -to-portgroup=PG-NEW-100 -out-dir=/var/log/vm-migration
```

---

## 7. 알려진 한계

- **포트그룹을 만들지 않습니다.** 목표 포트그룹은 미리 생성되어 있어야 하며, 없으면 시작 단계에서 중단합니다.
  생성 도구를 `-pg-cmd` 로 연결해 쓰세요.
- **게스트 OS 내부는 건드리지 않습니다.** 포트그룹 변경에 VLAN 변경이 함께 일어나는 경우,
  고정 IP 를 쓰는 VM 은 게스트 안에서 IP/게이트웨이를 직접 바꿔 주어야 통신이 됩니다.
  이 도구가 `SUCCESS` 를 냈다는 것은 **vSphere 레벨의 연결이 바뀌었다는 뜻일 뿐**, L3 통신이 된다는 보장이 아닙니다.
- **전원이 꺼진 VM** 은 `StartConnected` 만 켭니다. 실제 연결은 부팅 시점에 이루어집니다.
- **동명이인 VM** (인벤토리에 같은 이름이 둘 이상) 은 경고를 출력하고 먼저 찾은 쪽을 씁니다.
  이런 환경에서는 이름을 먼저 정리한 뒤 사용하세요.
- **롤백 파일이 없으면 되돌릴 수 없습니다.** `rollback_*.csv` 를 지우지 마세요.

---

## 8. 주의사항 (Disclaimer)

> **본 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.**
>
> 이 도구는 **vCenter 의 VM 설정을 실제로 변경(write)합니다.**
> 실행 결과의 `SUCCESS` 판정은 vCenter API 가 돌려준 값을 근거로 한 것이며, 실제 서비스 통신까지 보장하지 않습니다.
>
> **설정 변경 후에는 반드시 변경된 서버 중 무작위로 몇 대를 골라, vCenter UI 또는 게스트 OS 에 직접 접속해서
> 포트그룹과 통신 상태가 의도한 대로 변경되었는지 직접 확인하십시오.**
>
> 그 밖에 권장하는 사항:
> - 운영 반영 전 `-dry-run` 으로 변경 내역을 먼저 확인하세요.
> - 전 VM 을 한 번에 바꾸지 말고 카나리 1대 → 소규모 → 전체 순으로 진행하세요.
> - `rollback_*.csv` 를 안전한 곳에 보관하세요. 이 파일이 유일한 복구 수단입니다.
> - 작업은 승인된 변경 작업 창(maintenance window) 안에서 수행하세요.

### 필요한 vCenter 권한

| 권한 | 용도 |
|---|---|
| `Network.Assign` | NIC 를 포트그룹에 연결 |
| `VirtualMachine.Config.EditDevice` | 가상 NIC 설정 변경 |
| `System.Read` (읽기 전용) | 인벤토리 조회 |

권한이 부족하면 Reconfigure 단계에서 `NoPermission` 오류가 납니다. 가장 흔한 실패 원인입니다.
