# vm-param-check (통합 체크+교정 도구)

vCenter에 있는 VM들이 고성능(High Performance) 설정 기준(CPU/메모리/NUMA 토폴로지, vCPU affinity,
Shares, 호스트 전원정책 등)을 만족하는지 자동으로 점검하고, 원하면 그 자리에서 FAIL 항목을
안전장치(게이트)와 사용자 확인을 거쳐 자동 교정하고 재검증까지 마치는 도구입니다.

기존에는 "설정체크 실행 → CSV 확인 → 별도 설정수정 스크립트 실행"이 세 단계로 나뉘어 있었는데,
이 도구는 그걸 **바이너리 1개, 명령 1줄**로 통합했습니다. 설계 배경과 상세 판단 근거는 같은 폴더의
[`계획서.md`](./계획서.md)를 참고하세요.

```
[정상값 입력] → 체크 → CSV 생성(-user 접미사) → (-fix 안 주면 여기서 끝)
             → 게이트(그룹 동질성 + 전원 OFF) → dry-run 확인 → y/N → 실제 적용 → 재검증 CSV
```

## 1. 필요 환경

- Go 1.21 이상 (Rocky Linux에서 Go 1.26.5 기준으로 빌드·검증 완료)
- **인터넷 불필요** — `vendor/`에 의존성(`github.com/vmware/govmomi` 등)이 전부 포함되어 있어서
  이 폴더 하나만 옮기면 폐쇄망에서도 바로 빌드됩니다.
- vCenter 접속 계정 (Reconfigure 권한 필요 — `-fix`로 실제 설정을 바꾸려면)

## 2. 다운로드

**저장소 전체를 받는 경우:**
```bash
git clone <이 저장소 주소> myrepo
cd "myrepo/.claude/VM 업무/VM 매개변수 체크 및 설정 통합 툴"
```

**이 폴더만 필요한 경우** (예: 폐쇄망으로 옮기기 전에 이 폴더만 압축):
```bash
# 인터넷 되는 곳에서, 저장소를 받은 뒤
cd myrepo
tar czf vm-param-check.tar.gz ".claude/VM 업무/VM 매개변수 체크 및 설정 통합 툴"
```

## 3. 빌드 (인터넷 여부와 무관 — vendor/ 포함되어 있음)

```bash
cd ".claude/VM 업무/VM 매개변수 체크 및 설정 통합 툴"
go build -mod=vendor -o vm-param-check .
```

`-mod=vendor`가 핵심입니다 — 인터넷에서 새로 받으려 하지 않고 `vendor/` 안의 소스만 그대로 씁니다.

의존성을 최신화하고 싶을 때만(인터넷 되는 환경에서):
```bash
go mod tidy && go mod vendor
go build -o vm-param-check .
```

## 4. 폐쇄망(오프라인 서버)으로 이관

인터넷이 되는 환경에서 이 폴더를 통째로 압축해서 그대로 옮기면 됩니다 — `go.mod`/`go.sum`/`vendor/`가
전부 포함되어 있어서 부분 복사 없이 폴더 전체를 옮기는 게 핵심입니다.

```bash
# 1) 인터넷 되는 곳에서
cd myrepo
tar czf vm-param-check.tar.gz ".claude/VM 업무/VM 매개변수 체크 및 설정 통합 툴"

# 2) USB/scp 등으로 폐쇄망 서버로 파일 복사
scp vm-param-check.tar.gz user@폐쇄망서버:/path/to/dest/

# 3) 폐쇄망 서버에서 압축 해제 후 빌드
tar xzf vm-param-check.tar.gz
cd ".claude/VM 업무/VM 매개변수 체크 및 설정 통합 툴"
go build -mod=vendor -o vm-param-check .
```

이 3단계만 하면 폐쇄망에서 인터넷 연결 없이 바로 빌드·실행됩니다. Go 자체가 안 깔려 있으면
Go 배포 바이너리(예: `go1.26.5.linux-amd64.tar.gz`)를 미리 받아서 `/usr/local/go`에 풀고
`PATH`에 `/usr/local/go/bin`을 추가하면 됩니다(이 단계도 인터넷 불필요, tar.gz 파일만 있으면 됨).

## 5. 인증

환경변수로 받습니다(둘 중 하나만 있으면 됨):

```bash
export VC_USER='administrator@vsphere.local'
export VC_PASS='...'
# 또는
export VCENTER_USER='...'
export VCENTER_PASS='...'
```

## 6. 실행 모드

- **전체 순회 모드 (기본)**: `-vcenterList`에 나열된 모든 vCenter의 VM 인벤토리 전체를 체크
- **단일/지정 대상 모드**: `-f <파일>`로 체크할 BM(VM) hostname 목록을 주면 그 VM들만 체크

먼저 실제 인프라 없이 동작을 확인하려면:
```bash
./vm-param-check -demo
```

## 7. 옵션 전체 목록

### 체크 관련 (기존과 동일)

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-vcenterList <path>` | `vcenter.txt` | 전체 순회 모드에서 사용할 vCenter 주소 목록 파일 (한 줄에 하나) |
| `-f <path>` | (없음) | 단일/지정 대상 모드: 체크할 BM(VM) hostname 목록 파일 (한 줄에 하나, `#` 주석 가능) |
| `-ht <on\|off>` | (필수) | 하이퍼스레딩 상태 — ev01 그룹 affinity 자동계산에 사용 |
| `-cores <N>` / `-cores-ev02` / `-cores-ev03` | 필수/옵션/옵션 | 기대값: 소켓당 코어 수 |
| `-numa <N>` / `-numa-ev02` / `-numa-ev03` | 필수/옵션/옵션 | 기대값: NUMA 노드당 최대 vCPU(코어) 수 |
| `-cpu <N>` / `-cpu-ev02` / `-cpu-ev03` | 필수/옵션/옵션 | 기대값: vCPU 수 |
| `-mem <N>` / `-mem-ev02` / `-mem-ev03` | 필수/옵션/옵션 | 기대값: 메모리 GB |
| `-disk <N>` / `-disk-ev02` / `-disk-ev03` | 필수/옵션/옵션 | 기대값: 디스크 총량 GB |
| `-shares-ev01 <N>` / `-shares-ev02` / `-shares-ev03` | 필수/옵션/옵션 | 기대값: CPU/메모리 Shares(ratio) |
| `-affinity-ev01 <path>` | (옵션) | ev01 기대 affinity 파일. 안 주면 `-ht`/`-cores` 기반 자동계산 |
| `-affinity-ev02 <path>` / `-affinity-ev03 <path>` | (옵션) | ev02/ev03 기대 affinity 파일. 안 주면 해당 그룹 affinity 체크 스킵 |
| `-out <path>` | 타임스탬프 자동생성 | 상세 CSV 경로. `_summary` 붙은 요약 CSV가 하나 더 생성됨 |
| `-user <이름>` | (없음) | **CSV 파일명 접미사**. `-out=result.csv -user=kdh` → `result_kdh.csv`, `result_kdh_summary.csv`. 여러 사람이 동시에 실행할 때 파일명 충돌 방지용 |
| `-onlyFail` | `false` | PASS인 VM은 콘솔/CSV 모두에서 제외, FAIL/설정없음 있는 VM만 출력 |
| `-noColor` | `false` | 콘솔 ANSI 컬러 끔 |
| `-demo` | `false` | vCenter 연결 없이 합성 VM 3대로 동작 확인 |
| `-scale <N>` | `0` | vCenter 연결 없이 N대 규모 합성 VM으로 대량 환경 출력 시뮬레이션 |

### 교정(-fix) 관련 (신규)

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-fix` | `false` | 체크 완료 후 FAIL/설정없음 항목을 **게이트 검증 → dry-run 확인 → 자동교정 → 재검증**까지 이어서 진행. 안 주면 기존과 동일하게 체크+CSV까지만 |
| `-yes` | `false` | `-fix`와 함께: 변경 확인 프롬프트(y/N)를 생략하고 바로 적용 (자동화용, **신중히 사용**) |
| `-fixConcurrency <N>` | `20` | `-fix` 적용 시 동시 Reconfigure 처리 개수 |
| `-fixOut <path>` | 원본 CSV 이름 기준 자동생성 | 재검증 CSV 경로 (`_recheck_<타임스탬프>` 접미사) |

## 8. 사용 예시

**8-1. 기본 체크만 (기존과 동일):**
```bash
./vm-param-check --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --shares-ev02=1000 --shares-ev03=1000 \
  --affinity-ev01=ev01.txt --affinity-ev02=ev02.txt --affinity-ev03=ev03.txt \
  --out=result.csv --user=kdh
```
→ `result_kdh.csv`, `result_kdh_summary.csv` 생성.

**8-2. 체크 후 바로 교정까지 (대화형 확인):**
```bash
./vm-param-check -f kdh.txt --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --out=result.csv --user=kdh --fix
```
→ 체크 → CSV 저장 → 게이트 검증 → **변경 예정 내역을 전부 화면에 출력** → `(y/N)` 입력 대기.
`y`를 입력해야 실제로 vCenter 설정이 바뀝니다. `n`이나 그 외 입력은 취소(아무 것도 안 바뀜).

**8-3. 자동화 파이프라인(스크립트)에서 확인 없이 바로 적용:**
```bash
./vm-param-check -f kdh.txt --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --out=result.csv --fix --yes
```
**주의**: `-yes`는 프롬프트를 생략하므로 실제 인프라를 건드리기 전에 반드시 `-fix` 없이(체크만) 또는
`-yes` 없이 한 번 먼저 돌려서 dry-run 결과를 눈으로 확인하는 걸 권장합니다.

**8-4. 대수 많을 때 문제 있는 VM만 보기:**
```bash
./vm-param-check --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --onlyFail --out=result.csv
```

## 9. `-fix` 파이프라인 상세

### 9-1. 게이트 (실제 변경 전 안전장치)

두 검증을 통과해야만 dry-run 확인 단계로 넘어갑니다. 하나라도 실패하면 **아무것도 바꾸지 않고
즉시 중단**합니다.

- **그룹 동질성**: ev01끼리, ev02끼리, ev03끼리 vCPU/코어수/메모리/디스크/CPU Shares/NUMA/HT가
  전부 같아야 하고, ev01/ev02/ev03 그룹 간 VM 대수도 서로 같아야 함
- **전원 OFF**: 교정 대상 VM이 전부 꺼져 있어야 함 (CPU 토폴로지를 하드웨어 레벨로 직접 바꾸기 때문)

### 9-2. 자동교정 대상 vs 수동조치 대상

| 자동교정 (fixable) | 수동조치 (manual — 이 도구가 다루지 않음) |
|---|---|
| `sched.mem.lpage.enable1GPage` / `sched.mem.prealloc` / `sched.mem.prealloc.pinnedMainMem` / `sched.swap.vmxSwapEnabled` (고정값) | 메모리(`config.hardware.memoryMB`) |
| `cpuid.coresPerSocket` (Advanced Config) | 디스크 총 용량 |
| `hardware.numCoresPerSocket` (CPU 토폴로지 UI → `NumCoresPerSocket`) | CPU/메모리 Shares(ratio) |
| `numa.vcpu.maxPerVirtualNode` (Advanced Config) | 호스트 전원정책 |
| `config.numaInfo.coresPerNumaNode` (CPU 토폴로지 UI → `VirtualNuma.CoresPerNumaNode`, 설정편집 > VM 옵션 > CPU 토폴로지 화면의 그 필드) | "모든 게스트 메모리 예약" |
| `sched.vcpuN.affinity` (ev01은 `-ht`+vCPU로 자동계산, ev02/ev03는 `-affinity-evNN` 파일) | 네트워크 포트그룹 |
| `config.hardware.numCPU` (vCPU 수, 코어수와 함께 조합으로 교정) | |

**핵심 원칙**: 체크 단계에서 계산한 기대값을 재해석 없이 그대로 기록합니다. vCPU 수/소켓당 코어 수는
서로 나누어떨어져야 하므로(vSphere 제약), 둘 중 하나만 FAIL이어도 기대값 조합으로 함께 맞춥니다.
나누어떨어지지 않는 조합이면 계획 산출 단계에서 에러로 멈춥니다(아무것도 바꾸지 않음).

VM 1대당 vCenter `Reconfigure`를 **정확히 한 번만** 호출합니다 — Advanced Config, CPU 토폴로지,
affinity를 전부 하나의 요청에 담아서 보냅니다.

### 9-3. dry-run + 확인

적용 전에 VM별로 "무엇을 어떤 값에서 어떤 값으로 바꿀지"를 전부 출력하고, 수동조치 대상 항목은
따로 개수만 요약해서 보여줍니다. `-yes`가 없으면 `y`를 입력해야 다음 단계로 진행됩니다.

### 9-4. 재검증

적용이 끝나면 교정된 VM만 골라 vCenter에서 다시 조회해서 **최초 체크와 동일한 판정 로직**으로
다시 검사하고, 결과를 콘솔 + CSV(`_recheck_<타임스탬프>`)로 남깁니다. 남은 FAIL/설정없음이 있으면
경고로 표시됩니다(대부분 원래부터 수동조치 대상이었던 항목).

## 10. 콘솔 출력 / 출력 파일 형식

콘솔은 항상 `[1] VM별 요약 표`를 먼저 보여주고 `[2]` 상세 섹션이 이어집니다. `-onlyFail`이면
FAIL이 있는 VM만, 그 안에서도 FAIL/설정없음 항목만 보여줍니다.

CSV 2종(상세/요약)이 생성됩니다.
- **상세 CSV**: `VM명, 소스, 항목Key, 기대값, 실제값, 결과, 비고` — OK 포함, 필터링 없음(`-onlyFail`
  지정 시엔 콘솔과 동일하게 PASS VM 제외)
- **요약 CSV**: `VM명, 전체결과, OK, FAIL, 설정없음, 정보` — VM 1대당 한 줄

결과 값은 4가지: `OK`(일치) / `FAIL`(설정은 있으나 다름) / `설정없음`(아예 미설정) / `정보`(판정 없는
정보성 항목).

## 11. 체크 항목 카테고리 (소스 컬럼)

| 소스 | 의미 |
|---|---|
| `-` (공통설정) | 모든 VM 공통 — 메모리/스케줄러 설정, CPU 토폴로지, vCPU/메모리/디스크 수치, 메모리 예약 |
| `host` | VM이 돌고 있는 ESXi 호스트의 전원 정책 (기대값 항상 High Performance) |
| `ev01` | hostname에 `ev01` 포함 — affinity(자동계산), Shares — 항상 필수 체크 |
| `ev02` / `ev03` | hostname에 `ev02`/`ev03` 포함 — affinity(파일 기반), Shares — 옵션 줬을 때만, VM 1대뿐이면 스킵 |
| `network` | 네트워크 어댑터 포트그룹 이름 (판정 없음, 정보성) |

## 12. 검증 이력

- **단위 테스트**: `go test -mod=vendor ./fixer/` — 계획 산출/게이트 로직 10건 통과
- **vcsim(재현 환경)**: 실제 vCenter 192.168.0.50의 `192ev01`/`192ev02` 구조를 그대로 재현한
  vcsim에서 `-fix` 파이프라인 전체(게이트 → dry-run → 취소 → 적용 → 재검증) 실행 확인.
  `cpuid.coresPerSocket`, `numa.vcpu.maxPerVirtualNode`, `NumCoresPerSocket` 하드웨어 필드 교정
  정상 동작 확인
- **실제 vCenter(192.168.0.50)**: `192ev01`/`192ev02` 대상으로 동일 파이프라인 실행, 적용 후
  재검증에서 전부 OK 전환 확인. vSphere Client의 설정 편집 > VM 옵션 > CPU 토폴로지 화면에서
  "Cores per Socket"/"NUMA 노드당 코어 수"가 실제 숫자로 표시되는 것까지 확인

## 13. 알려진 한계

- `config.numaInfo.coresPerNumaNode`(CPU 토폴로지 UI의 NUMA 노드당 코어 수)는 vSphere API
  8.0.0.1+ 필요. 이 필드가 없는 구버전 vCenter에서는 "설정없음"으로만 나오고 교정도 반영되지 않을
  수 있습니다.
- **vcsim(govmomi v0.55.1) 시뮬레이터는 `VirtualNuma`/`NumaInfo` 필드를 구현하지 않습니다** —
  vcsim으로 테스트하면 이 항목만 교정 후에도 "설정없음"으로 남습니다(시뮬레이터 한계, 실제
  vCenter에서는 정상 반영됨 — 12장 검증 이력 참고).
- Shares는 CPU/메모리 구분 없이 `-shares-evNN` 값 하나를 양쪽에 동일하게 적용합니다.
- 호스트 고성능 전원정책 변경은 이번 범위에 포함하지 않습니다(체크만, 자동교정 대상 아님).

## 14. 기존 개별 도구와의 관계

저장소의 `VM 매개변수설정체크/`(체크 전용)와 `VM 매개변수설정체크/FAIL기반 매개변수 수정/`(레거시
외부 도구 오케스트레이션 방식)는 삭제하지 않고 그대로 남겨뒀습니다. 이 폴더는 그 둘을 대체하는
통합본입니다 — 새로 시작하는 경우 이 폴더만 쓰면 됩니다.
