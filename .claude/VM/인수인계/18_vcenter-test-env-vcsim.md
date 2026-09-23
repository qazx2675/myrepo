# 18. vcenter-test-env-vcsim — 실 vCenter 구조를 복제한 테스트 환경

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🟡 **테스트용.** 실 vCenter는 **읽기만** 하고, 재현은 로컬 시뮬레이터 위에서만 |
| 폴더 | `.claude/VM/vcenter-test-env-vcsim/` |
| 바이너리 | `vc-test-env` |
| 하는 일 | 실 vCenter의 인벤토리 구조·설정을 읽어와 **vcsim(가짜 vCenter)** 위에 똑같이 재현 |
| 인증 | `VC_USER` / `VC_PASS` (실 vCenter에서 추출할 때만 필요) |
| vcsim 주소 | **`127.0.0.1:54321` 고정** |

---

## 1. 이 도구는 무엇인가

다른 도구(`vm-param-check` 등)를 테스트하고 싶은데, **운영 중인 실제 vCenter를 대상으로 돌리기는 위험합니다.**

이 도구는 실 vCenter의 구조(데이터센터/폴더/클러스터/호스트/VM/네트워크)와 일부 설정값을 **읽어서 레시피(JSON)로 저장**하고, 그 레시피로 **vcsim 위에 이름·구조가 똑같은 가짜 환경을 만듭니다.**

```
[실 vCenter 192.168.0.50]  ──읽기만──>  [레시피 JSON]  ──재현──>  [vcsim 127.0.0.1:54321]
                                        ~/.vc-test-env/recipes/         ↑
                                                                다른 도구를 여기에 붙여서 테스트
```

**한 번 레시피를 뽑아두면 그 뒤로는 실 vCenter 접속 없이** vcsim을 계속 재기동할 수 있습니다. 폐쇄망 이관에도 유리합니다.

---

## 2. 빌드

```bash
cd .claude/VM/vcenter-test-env-vcsim
GOFLAGS=-mod=vendor go build -o vc-test-env .
# 또는
bash setup.sh
```

`-mod=vendor`가 핵심입니다 — `vendor/`에 govmomi가 통째로 들어 있어 인터넷 없이 빌드됩니다.

---

## 3. 사용 방법

### 3-1. 인증 (실 vCenter에서 추출할 때만)

```bash
export VC_USER='administrator@vsphere.local'
export VC_PASS='...'
```

### 3-2. 기본 실행 — 테스트 환경 통째로 기동

```bash
./vc-test-env
```

- 처음 실행하면 접속할 vCenter 주소를 물어봅니다.
- 이후로는 히스토리에서 고르거나(2개 이상일 때), 1개뿐이면 자동으로 씁니다.
- 특정 vCenter를 바로 지정: `./vc-test-env -vc=192.168.0.50`
- 캐시 대신 실 vCenter에서 다시 추출: `./vc-test-env -vc=192.168.0.50 -refresh`

실행되면:

1. (필요시) 실 vCenter에서 구조/설정 추출 → `~/.vc-test-env/recipes/`에 캐시
2. vcsim을 로컬에 기동하고 그 구조를 재생성
3. 재생성된 구조를 트리로 출력
4. vcsim 접속 주소와 예시 명령어 출력
5. **Ctrl+C 누르기 전까지 vcsim을 계속 띄워둠**

### 3-3. 다른 터미널에서 도구를 vcsim에 붙이기

```bash
./vm-param-check -vcTargetIP=127.0.0.1:54321 -id=administrator@vsphere.local ...
```

PowerCLI로도 됩니다:

```powershell
Connect-VIServer -Server 127.0.0.1:54321 -User administrator@vsphere.local -Password 아무값 -Force
Get-VM
Get-Cluster
```

> vcsim은 **계정/비밀번호 값 자체를 검사하지 않습니다.** 아무 문자열이나 넣어도 접속됩니다.

### 3-4. 포트 충돌

vcsim은 항상 `127.0.0.1:54321` 고정 포트로 뜹니다. 이전에 백그라운드로 띄워두고 잊어버린 프로세스가 있으면 기동 직전에 자동 감지해서 물어봅니다.

```
포트 54321를 이미 사용 중인 프로세스가 있습니다: PID 51761 (vc-test-env)
이 프로세스를 종료하고 계속하시겠습니까? (y/N):
```

`y`면 종료 후 진행, 그 외면 실행 중단(수동 정리 후 재시도).

### 3-5. 다른 서버로 통째로 이관

레시피(`~/.vc-test-env/recipes/`)와 실행 파일을 함께 옮기면, 그 서버에서는 **실 vCenter 접속 없이** 같은 환경을 재현할 수 있습니다.

```bash
bash export_vcsim_env.sh 192.168.0.60
```

내부 동작:

1. 현재 프로그램 폴더와 `~/.vc-test-env/`를 하나로 압축 (`/tmp/vcsim_with_recipes.tar.gz`)
2. 대상 서버 `/root/`로 전송 (대상 서버 비밀번호 필요)
3. 대상 서버에서 압축 해제 및 임시 파일 자동 삭제
4. 이후 `/root/vcenter-test-env-vcsim/vc-test-env` 실행 가능

---

## 4. 서브커맨드 (옵션 상세)

| 명령 | 설명 |
|---|---|
| `./vc-test-env` | 기본: 레시피로 vcsim을 띄우고 계속 유지 |
| `./vc-test-env -vc=<주소>` | 대상 vCenter 지정 |
| `./vc-test-env -vc=<주소> -refresh` | 캐시 무시하고 실 vCenter에서 다시 추출 |
| `./vc-test-env extract -vc=192.168.0.50` | **레시피만 추출/갱신** (vcsim 기동 없이) |
| `./vc-test-env tree -vc=192.168.0.50` | 대상(실 vCenter든 vcsim이든)의 구조를 트리로 출력 |
| `./vc-test-env diff -vc=192.168.0.50 -sim=127.0.0.1:54321` | 원본 레시피와 떠 있는 vcsim을 **필드 단위로 비교** |

---

## 5. 무엇을 복제하나 (1차 범위)

### 구조

데이터센터, VM 폴더, 네트워크 폴더, 클러스터, 호스트, VM, 네트워크(이름)

### VM 설정

- vCPU 수, 코어/소켓 수, 메모리MB
- 구조화된 CPU Affinity (`Config.CpuAffinity.AffinitySet`)
- per-vCPU ExtraConfig CPU Affinity (`sched.vcpu<N>.affinity`) — vCPU 개수만큼 동적으로 생기는 키라 고정 목록이 아니라 패턴으로 스캔
- `sched.mem.lpage.enable1GPage`, `sched.mem.prealloc*`, `sched.swap.vmxSwapEnabled`, `numa.vcpu.maxPerVirtualNode`
- 네트워크 어댑터: 포트그룹 이름 + 커넥트/디스커넥트 상태를 **실제 디바이스로 재현**

> **실제로 겪은 함정**: 두 CPU Affinity 방식은 서로 다른 저장 위치라 실 vCenter에서 어느 한쪽만 값이 있고 다른 쪽은 비어있을 수 있습니다. `affinity_setting` 도구로 설정한 VM은 `Config.CpuAffinity`가 아니라 **`sched.vcpu<N>.affinity` ExtraConfig에만** 값이 들어갑니다.

### 범위 밖

VM "설정 편집"의 나머지 항목(부팅옵션/비디오카드/USB 등), 호스트 "구성" 탭 전체. 디스크는 **용량만 추적하고 디바이스로 재현하지 않습니다.**

---

## 6. 필드 추가하는 법 (확장 지점)

새로 추적해야 할 VM 설정 항목이 생기면 `internal/fields/fields.go`의 `VMFields` 슬라이스에 **한 줄만 추가**하면 됩니다. `extract`/`tree`/`build`/`diff` 전부 자동 반영됩니다.

```go
extraConfigField("새로운.ExtraConfig.키"),
```

ExtraConfig 형태가 아닌 구조화된 필드는 `Field{Key, Extract, Apply}`를 직접 작성합니다.

---

## 7. 검증 결과

192.168.0.50 대상, Rocky Linux에서 실제 실행:

- `extract`: ExtraConfig 키가 정확한 이름으로 잡힘 ✅
- `tree`: 실 vCenter 구조가 정확히 나옴 ✅ (클러스터 없는 독립형 호스트 환경도 처리)
- `build`: vcsim에 동일 구조 재생성 성공 ✅
- `diff`: 원본과 재생성본 비교 결과 "차이 없음" ✅
- PowerCLI: `Connect-VIServer` + `Get-VM`으로 VM 이름/CPU/메모리 동일 조회 ✅
- 네트워크 어댑터: `vm-param-check`로 재생성본 체크 시 동일 ✅

---

## 8. 알려진 한계 / TODO

- **씨드 호스트가 하나 더 보입니다.** 데이터스토어 연결 트릭 때문에 빈 호스트가 하나 더 보이나 실제 VM 동작에는 영향 없음.
- 데이터센터가 **2개 이상인 레시피는 아직 완전 지원 안 함**.
- 네트워크는 포트그룹 이름/커넥트 상태만 재현하고 **VLAN ID는 아직 안 함**.
- 호스트 전원정책은 읽기만 하고 vcsim에 재현하는 건 TODO.
- 디스크는 용량만 추적, VirtualDisk 디바이스로 재현 안 함.

### PowerCLI `Get-View` 에러 (govmomi 시뮬레이터 자체 한계)

`Get-View`로 VM의 `Runtime`/`Summary`(또는 속성 지정 없이 전체)를 가져오면 PowerCLI에서 에러가 납니다:

```
Error in deserializing body of reply message for operation 'RetrieveProperties'
```

원인은 `VirtualMachineRuntimeInfo.OperationNotSupported` 필드가 vcsim에서 PowerCLI의 .NET SOAP 클라이언트가 못 읽는 형태로 직렬화되기 때문입니다. **이 도구의 문제가 아니라 govmomi 시뮬레이터 자체의 한계**입니다.

우회 방법:

```powershell
# 필요한 속성만 좁혀서 조회 (Runtime/Summary를 빼면 정상)
Get-View -ViewType VirtualMachine -Property Name,Config,Guest

# 또는 고수준 cmdlet 사용 (정상 동작 확인됨)
Get-VM
Get-VM | Select-Object ...
```

### 캐시 오염 복구

`-vc`에 이 도구 자신이 띄우는 vcsim 주소(`127.0.0.1:54321`)를 실수로 지정하면 **지금은 즉시 에러로 막힙니다.** 과거 이 가드가 없던 버전에서 캐시가 오염됐다면:

```bash
# ~/.vc-test-env/history.json 에서 127.0.0.1:54321 항목 삭제
rm ~/.vc-test-env/recipes/127.0.0.1_54321.json
```

실 vCenter 레시피(예: `192.168.0.50`)는 건드리지 않습니다.

---

## 9. 파일 구조

```
vcenter-test-env-vcsim/
├── README.md              # 1차 자료
├── PLAN.md                # 개발/변경 계획 메모
├── main.go                # CLI 진입점 (기본 실행, extract/tree/diff 서브커맨드)
├── setup.sh               # 폐쇄망 빌드
├── export_vcsim_env.sh    # 실행 파일 + 레시피를 다른 서버로 통째로 이관
├── internal/
│   ├── builder/     # 레시피로 vcsim 위에 인벤토리 재생성 (Port 상수 = 54321)
│   ├── connect/     # 실 vCenter/vcsim 접속
│   ├── fields/      # ★ 추적 대상 VM 설정 필드 정의 (확장 지점)
│   ├── history/     # 접속했던 vCenter 이력 관리
│   ├── inventory/   # 인벤토리 구조 모델 (walk.go에서 affinity 패턴 스캔)
│   ├── portcheck/   # 54321 포트 사용 중 감지
│   ├── recipe/      # 레시피 저장/로딩
│   └── tree/        # 트리 출력
├── vc-test-env/                        # (하위) 관련 파일
├── vcenter-powershell-autocompletion/  # 실습용 PowerShell 설치 + 자동완성 프로필
└── vendor/
```

레시피 캐시 위치: **`~/.vc-test-env/recipes/<vCenter주소>.json`**, 히스토리: `~/.vc-test-env/history.json`

---

## 10. 관련 문서

- 1차 자료: `vcenter-test-env-vcsim/README.md`
- 이 도구 + vm-param-check를 묶은 폐쇄망 패키지: [19. integrated-vm-param-check-test-tool](./19_integrated-vm-param-check-test-tool.md)
- 일반 업무용 PowerShell 설치: [21. powershell](./21_powershell.md)
