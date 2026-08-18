# vc-test-env

실 vCenter의 인벤토리 구조(데이터센터/폴더/클러스터/호스트/VM/네트워크)와 일부 설정값을 읽어와서 [vcsim](https://github.com/vmware/govmomi/tree/main/vcsim) 위에 동일하게 재현하는 도구입니다. 목적은 실 vCenter를 건드리지 않고, 실 vCenter와 이름·구조가 같은 테스트 환경에서 다른 govmomi 기반 도구나 PowerCLI 스크립트를 테스트하는 것입니다.

> **상태**: Rocky Linux(192.168.0.58, govmomi v0.55.1)에서 실 vCenter(192.168.0.50)를 대상으로 `extract`/`tree`/`build`(vcsim 재생성)/`diff`/PowerCLI 접속까지 전부 실제로 빌드·실행해서 검증 완료. 아래 "알려진 한계" 항목만 남아 있음.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

### 필요 환경
- Go 1.21 이상 (Rocky Linux 7.6.4에서 govmomi v0.55.1 기준으로 빌드·검증 완료)
- 최종 실행 대상: RHEL 8.10(폐쇄망), PowerCLI 설치되어 있음
- **인터넷은 필요 없습니다** — `vendor/` 디렉터리에 의존성(govmomi 등)이 이미 통째로 포함되어 있어서, 이 폴더만 옮기면 바로 오프라인 빌드가 됩니다.

### 다운로드
이 도구는 저장소 안의 `.claude/VM 업무/vcenter-test-env-vcsim/` 폴더 하나에 전부 들어 있습니다.

**저장소 전체를 받는 경우:**
```bash
git clone <이 저장소 주소> myrepo
cd myrepo/.claude/VM\ 업무/vcenter\ 테스트환경구축\ \(vcsim\)/
```

**이 폴더만 필요한 경우** (예: 폐쇄망으로 옮기기 전에 이 폴더만 압축):
```bash
# 인터넷 되는 곳에서, 저장소를 받은 뒤
cd myrepo
tar czf vc-test-env.tar.gz ".claude/VM 업무/vcenter-test-env-vcsim"
```

### 빌드 (인터넷 여부와 무관 — vendor/ 포함되어 있음)
폴더 안으로 들어가서 바로 빌드합니다:
```bash
cd ".claude/VM 업무/vcenter-test-env-vcsim"
GOFLAGS=-mod=vendor go build -o vc-test-env .
```
`-mod=vendor`가 핵심입니다 — 인터넷에서 새로 받으려 하지 않고 `vendor/` 안의 소스만 그대로 씁니다. 빌드가 끝나면 이 폴더 안에 `vc-test-env` 실행파일이 생깁니다.
(의존성을 최신화하고 싶을 때만, 인터넷 되는 환경에서 `go mod tidy && go mod vendor`로 `vendor/`를 다시 채우면 됩니다 — 평소엔 필요 없습니다.)

### 폐쇄망(RHEL 8.10)으로 이관
인터넷이 되는 환경에서 받은 뒤, `vendor/`가 포함된 이 폴더 전체를 압축해서 그대로 옮기면 됩니다.
```bash
# 1) 인터넷 되는 곳에서 (예: Rocky Linux)
cd myrepo
tar czf vc-test-env.tar.gz ".claude/VM 업무/vcenter-test-env-vcsim"

# 2) USB/scp 등으로 폐쇄망 RHEL 8.10 서버로 파일 복사

# 3) 폐쇄망 서버에서 압축 해제 후 빌드
tar xzf vc-test-env.tar.gz
cd ".claude/VM 업무/vcenter-test-env-vcsim"
GOFLAGS=-mod=vendor go build -o vc-test-env .
```

### 전역 명령어로 사용하기 (선택 사항)
빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 이동하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 사용할 수 있습니다.

예시 (실행 파일을 `/usr/local/bin`으로 복사):
```bash
sudo cp vc-test-env /usr/local/bin/
# 이후 어느 위치에서나 vc-test-env 명령어로 실행 가능
```

## 2. 사용 방법

### 인증
실 vCenter 접속용 자격증명은 환경변수로 받습니다(기존 `vm-param-check`와 동일한 변수명):
```bash
export VC_USER='administrator@vsphere.local'
export VC_PASS='...'
```

### 기본 실행 — 테스트 환경 통째로 기동
```bash
./vc-test-env
```
- 처음 실행하면 접속할 vCenter 주소를 물어봅니다.
- 이후로는 히스토리에 기억된 vCenter 중에서 고르거나(2개 이상일 때), 1개뿐이면 자동으로 씁니다.
- 특정 vCenter를 바로 지정하려면: `./vc-test-env -vc=192.168.0.50`
- 캐시된 레시피 대신 실 vCenter에서 다시 추출하려면: `./vc-test-env -vc=192.168.0.50 -refresh`

실행되면:
1. (필요시) 실 vCenter에서 구조/설정 추출 → `~/.vc-test-env/recipes/`에 캐시
2. vcsim을 로컬에 기동하고 그 구조를 그대로 재생성
3. 재생성된 구조를 트리로 출력
4. vcsim 접속 주소와 예시 명령어 출력
5. Ctrl+C 누르기 전까지 vcsim을 계속 띄워둠

vcsim은 항상 **`127.0.0.1:54321` 고정 포트**로 뜹니다(`internal/builder/build.go`의 `Port` 상수). 이미 이 포트를 다른 프로세스가 쓰고 있으면 기동에 실패하니, 그럴 땐 그 프로세스를 먼저 종료하세요.

이 상태에서 **다른 터미널**을 열어 기존 도구를 vcsim 주소(`127.0.0.1:54321`)로 실행하면 됩니다:
```bash
./vm-param-check -vcTargetIP=127.0.0.1:54321 -id=administrator@vsphere.local ...
```

PowerCLI에서 직접 확인하고 싶으면:
```powershell
Connect-VIServer -Server 127.0.0.1:54321 -User administrator@vsphere.local -Password 아무값 -Force
Get-VM
Get-Cluster
```

### 테스트 환경을 다른 서버로 이동해서 사용하려면

추출된 레시피 데이터(`~/.vc-test-env/recipes/`)와 실행 파일을 함께 다른 서버로 넘기면, 해당 서버에서는 실제 vCenter에 연결하지 않고도 기존 환경을 완벽히 동일하게 재현할 수 있습니다.

이를 위해 함께 제공되는 `export_vcsim_env.sh` 스크립트를 사용하면 한 번에 이관할 수 있습니다.

```bash
# 사용법 (현재 vcsim 폴더 내에서 실행)
bash export_vcsim_env.sh <이동할_대상_서버_IP>

# 예시
bash export_vcsim_env.sh 192.168.0.60
```

**스크립트 내부 동작 과정:**
1. 현재 프로그램 폴더와 `~/.vc-test-env/` 폴더를 하나로 압축 (`/tmp/vcsim_with_recipes.tar.gz`)
2. 대상 서버의 `/root/` 경로로 파일 전송 (대상 서버 비밀번호 필요)
3. 대상 서버에서 압축 해제 및 임시 파일 자동 삭제
4. 이후 대상 서버에 접속하여 `/root/vcenter-test-env-vcsim/vc-test-env`를 바로 실행 가능

## 3. 옵션별 상세 설명

```bash
# 레시피만 추출/갱신 (vcsim 기동 없이)
./vc-test-env extract -vc=192.168.0.50

# 대상(실 vCenter든 vcsim이든)의 구조를 트리로 출력
./vc-test-env tree -vc=192.168.0.50
./vc-test-env tree -vc=127.0.0.1:54321

# 원본 레시피와 떠 있는 vcsim을 필드 단위로 비교
./vc-test-env diff -vc=192.168.0.50 -sim=127.0.0.1:54321
```

## 4. 문서별 고유 설명

### 4.1 지금 다루는 항목 (1차 범위)
- 구조: 데이터센터, VM 폴더, 네트워크 폴더, 클러스터, 호스트, VM, 네트워크(이름)
- VM: vCPU 수, 코어/소켓 수, 메모리MB, CPU Affinity, `sched.mem.lpage.enable1GPage` / `sched.mem.prealloc*` / `sched.swap.vmxSwapEnabled` / `numa.vcpu.maxPerVirtualNode`
- VM 네트워크 어댑터: 포트그룹 이름 + 커넥트/디스커넥트 상태를 실제 디바이스로 재현

**범위 밖(추가 안 함)**: VM "설정 편집"의 나머지 항목(부팅옵션/비디오카드/USB 등), 호스트 "구성" 탭 전체, 디스크는 용량만 추적하고 디바이스로 재현 안 함.

### 4.2 필드 추가하는 법 (확장 지점)
새로 추적해야 할 VM 설정 항목이 생기면 `internal/fields/fields.go`의 `VMFields` 슬라이스에 항목 하나만 추가하면 됩니다. `extract`/`tree`/`build`/`diff` 전부 자동으로 반영됩니다.
```go
extraConfigField("새로운.ExtraConfig.키"),
```
ExtraConfig 형태가 아닌 구조화된 필드는 `Field{Key, Extract, Apply}`를 직접 작성해서 추가하면 됩니다.

### 4.3 검증 결과 (192.168.0.50 대상, Rocky Linux에서 실제 실행)
- `extract`: ExtraConfig 키가 실제로 정확한 이름으로 잡힘 — 확인 완료.
- `tree`: 실 vCenter 구조가 정확히 나옴 — 확인 완료. 클러스터가 없는 독립형 호스트 환경도 처리하도록 수정함.
- `build`: vcsim에 동일 구조 재생성 성공 — 확인 완료.
- `diff`: 원본과 재생성본 비교 결과 "차이 없음" — 확인 완료.
- PowerCLI: `Connect-VIServer` + `Get-VM`으로 재생성된 VM 이름/CPU/메모리가 실제와 동일하게 조회됨 — 확인 완료.
- 네트워크 어댑터: `vm-param-check`로 재생성본을 체크했을 때 동일하게 나옴 — 확인 완료.

### 4.4 알려진 한계 / TODO
- **씨드 호스트가 하나 더 보임**: 데이터스토어 연결을 위해 사용한 트릭 때문에 빈 호스트가 하나 더 보이나, 실제 VM 동작에는 영향 없음.
- 데이터센터가 2개 이상인 레시피는 아직 완전히 지원 안 함.
- 네트워크는 포트그룹 이름/커넥트 상태만 재현하고 VLAN ID는 아직 안 함.
- 호스트 전원정책은 읽기만 하고 vcsim에 재현하는 건 TODO.
- 디스크는 용량만 추적하고 실제 VirtualDisk 디바이스로는 재현하지 않음.
