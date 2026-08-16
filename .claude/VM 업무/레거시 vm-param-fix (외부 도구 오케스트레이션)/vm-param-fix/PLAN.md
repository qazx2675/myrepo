# vm-param-fix (FAIL 기반 매개변수 자동 교정 도구) 프로젝트 계획서

## 목표 및 범위

`vm-param-check`가 산출한 FAIL 항목을 사람이 하나씩 손으로 고치는 대신, 같은 저장소 안에 이미 존재하는 설정 적용 도구(`vm_affinity_bulk`, `vm_lpage_bulk` 등)를 자동으로 호출해서 실제 vCenter 설정을 교정하는 별도 Go 도구를 만든다.

**포함**: FAIL 항목 태깅 → 서버 그룹 동질성 검증 → VM 전원 OFF 검증 → 사용자 확인 → 태그별 외부 도구 호출 → 재검증/리포트.
**제외(이번 범위 아님)**: 이 도구 자체가 vCenter Reconfigure API를 직접 호출하는 것 — 실제 설정 변경은 전부 기존 `vm_affinity_bulk`/`vm_lpage_bulk`/전원정책 도구에 위임한다(재구현 아님, 오케스트레이션).

## 배경 — 이미 존재하는 도구 확인 결과 (추측 아님, 코드 직접 조사)

사용자가 예시로 준 3개 명령어(`affinity_setting`/`lpage_setting`/`power_setting`)의 플래그 이름을 저장소 실제 코드와 정확히 대조한 결과, 아래처럼 매핑된다 (사용자 확인: "명령어는 내가 말한 그대로 적용되어야 한다" — 값은 예시, 명령/플래그 이름은 그대로 사용):

| 태그 명령 (사용자 지정, 그대로 사용) | 실제 매칭되는 소스 | 상태 | 플래그 일치 근거 |
|---|---|---|---|
| `affinity_setting -vcTargetIP -worklistFile -affinityFile -id` | `old/go-lang/phase4-2-affinity/affinity.go` (레거시) | 병렬 아님(Task만 비동기 전송 후 일괄 Wait) | `-id`/`-vcTargetIP`/`-worklistFile`(기본 worklist.txt)/`-affinityFile`(기본 affinity.txt) 플래그명이 전부 정확히 일치. `-ht`(ON/OFF) 플래그도 있음(사용자 예시엔 생략됐지만 존재) |
| `power_setting -vcTargetIP -worklistFile -id` | `old/go-lang/phase4-3-power-policy/power_policy.go` (레거시) | **이미 병렬**(워커풀, `-concurrency` 기본 10) | `-id`/`-vcTargetIP`/`-worklistFile`(기본 worklist_bm.txt, ESXi 물리 호스트명)/`-concurrency` 전부 일치 |
| `lpage_setting [옵션 깃허브문서 참조]` | `.claude/VM설정 go lang/main_lpage.go` → 빌드명 `vm_lpage_bulk` (**현재 세대**) | **이미 병렬**(워커풀, `-concurrency` 기본 20) | 실제로 README에 문서화된 lpage 도구는 이것 하나뿐(레거시 phase4-4 버전은 문서 없음) — "깃허브문서 참조"가 가리키는 게 이 도구로 보임. 플래그: `-id`/`-vcTargetIP`/`-worklistFile`/`-ev01Cores`/`-ev01Sockets`/`-ev02Cores`/`-ev02Sockets`/`-ev01Numa`/`-ev02Numa`/`-applyTopology`/`-concurrency` |

즉 **affinity/power는 레거시 도구, lpage만 현재 세대 도구**를 그대로 서브프로세스로 호출하는 게 사용자가 준 명령어와 일치합니다. (열린 질문 1번 — "전원정책 도구를 레거시 그대로 쓸지" — 는 이걸로 답이 나온 걸로 보고 아래서 반영함.)

주요 확인 사항:
- **인증 방식 불일치**: `vm-param-check`는 `VC_USER`/`VC_PASS`를 쓰는데, 위 3개 도구는 전부 `VC_PASSWORD` 하나만 씀(ID는 `-id` 플래그). 새 도구가 이 차이를 그대로 흡수해야 함.
- **affinity_setting(레거시)의 ev02 처리 방식**: `-affinityFile` 하나만 받아서 그 안의 key=value 맵을 매칭되는 모든 ev02 VM에 동일하게 적용한다. ev01은 파일과 무관하게 실제 vCPU 수 + `-ht`로 자동계산. **ev03은 아예 지원하지 않음**(레거시라 ev01/ev02까지만).
- **`vm_lpage_bulk`(lpage 태그)는 vCPU/코어/NUMA 토폴로지를 하드웨어 레벨(`NumCoresPerSocket`, `VirtualNuma`)로 직접 바꾼다** — 이런 하드웨어 리컨피그는 통상 VM 전원이 꺼져 있어야 한다. 사용자가 요구한 "VM 1대라도 켜져 있으면 불가" 게이트는 여기서 특히 중요.
- **power_setting(레거시)은 worklist가 VM이 아니라 ESXi 물리 호스트명 목록**이라는 점이 affinity/lpage(둘 다 BM=VM 기준 worklist)와 다름 — 이 새 도구가 태그별로 서로 다른 worklist 파일을 만들어서 넘겨야 함.
- **ev03 지원이 도구마다 다르다**: lpage(현재세대)와 affinity(레거시) 둘 다 ev03을 지원하지 않음. (참고로 현재세대 `vm_affinity_bulk`는 ev03을 지원하지만, 사용자가 지정한 `affinity_setting` 명령 형태는 레거시 쪽과 일치하므로 이번 계획에서는 레거시 기준으로 감.)

## 전체 파이프라인

```
vm-param-check 결과 CSV(상세)
        │
        ▼
[1] FAIL/설정없음 항목에 태그 부여 (아래 매핑표)
        │
        ▼
[2] 그룹 동질성 사전검증 ─┐
[3] VM 전원상태(OFF) 검증 ─┤ 병렬로 동시 실행
        │  (하나라도 실패 시 여기서 즉시 중단, 아무것도 안 바꿈)
        ▼
[4] "무엇을 어떻게 바꿀 것인지" dry-run 요약 출력
        │
        ▼
[5] 사용자에게 명시적 확인 요청 (y/N) ── 아니오면 중단
        │
        ▼
[6] 태그별로 해당 외부 도구를 병렬 호출 (affinity / lpage / power 각각 독립적이라 동시 실행 가능)
        │
        ▼
[7] 각 도구 실행 결과 취합 + vm-param-check 재실행 방식으로 재검증 리포트
```

### FAIL Key → 태그 매핑표 (초안)

| vm-param-check의 Key/Source | 태그 | 담당 도구 |
|---|---|---|
| `sched.vcpuN.affinity` (ev01/ev02/ev03 전부) | `affinity` | `affinity_setting` (레거시 `affinity.go`, **ev03 미지원**이라 실제 적용 도구 교체 필요) |
| `sched.mem.lpage.enable1GPage`, `sched.mem.prealloc*`, `sched.swap.vmxSwapEnabled`, `cpuid.coresPerSocket`, `hardware.numCoresPerSocket`, `numa.vcpu.maxPerVirtualNode`, `config.numaInfo.coresPerNumaNode` (ev01/ev02/ev03 전부) | `lpage` | `lpage_setting` (현재세대 `vm_lpage_bulk`, **ev03 미지원**이라 실제 적용 도구 교체 필요) |
| `host power policy` (source=`host`) | `power` | `power_setting` (레거시 `power_policy.go`) |
| `config.hardware.numCPU`, `config.hardware.memoryMB`, 디스크 용량, Shares, `config.memoryReservationLockedToMax`, 네트워크 | **수동조치 필요** (자동교정 대상 아님) | — |

vCPU 수/메모리/디스크/Reserve-all-guest-memory는 지금 조사한 3개 도구 중 어느 것도 딱 맞게 커버하지 않아서(일부는 `vm_lpage_bulk`가 `applyTopology`로 vCPU까지 건드리긴 하지만 메모리/디스크는 아예 없음), 이번 1차 범위에서는 **자동교정 대상에서 제외**하고 "수동조치 필요"로만 표시하는 걸 기본안으로 제안합니다.

## 안전장치 설계

### 1) 그룹 동질성 검증
작업대상 파일(worklist)에 나열된 모든 BM(호스트)의 ev01/ev02/ev03 VM을 병렬로 조회해서 아래 값이 그룹 내에서 전부 동일한지 확인:
- ev01/ev02/ev03 그룹 간 VM 대수(구성이 몇 대인지) 일치 여부
- ev01 Shares ratio, ev02 Shares ratio
- vCPU 수, 코어당 소켓 수, 메모리 GB, 디스크 GB
- NUMA 노드 구성(`numa.vcpu.maxPerVirtualNode`), HT(하이퍼스레딩) 상태

하나라도 다르면 **어떤 호스트의 무엇이 얼마나 다른지 표로 보여주고 즉시 중단**. (예: "host03의 ev01은 disk=300GB인데 나머지는 500GB")

### 2) 전원 OFF 게이트
대상 VM 전체의 `Runtime.PowerState`를 병렬 조회. `poweredOn`이 1대라도 있으면 그 VM 이름을 나열하고 즉시 중단 — affinity/전원정책 설정은 켜진 채로도 적용 자체는 되지만, `vm_lpage_bulk`의 하드웨어 토폴로지 변경은 켜진 채로 시도하면 실패하거나 예기치 않은 동작을 할 수 있어서, 이 도구가 다루는 태그 전체에 대해 일괄적으로 "전원 OFF 필수"를 강제하는 게 가장 안전.

### 3) 확인 없이는 절대 실행 안 함
`[4]` dry-run 단계에서 "이 태그는 이 도구로, 이런 worklist/옵션으로 호출될 예정"을 전부 보여준 뒤, 명시적 `y` 입력 전까지는 어떤 외부 도구도 호출하지 않음. (`-yes`/`--force` 같은 스킵 플래그는 처음부터 만들지 않는 걸 제안 — 실수로 무인 실행되는 경로 자체를 없애기 위함.)

## 외부 도구 연동 방식

Go의 `os/exec`로 각 도구 바이너리를 서브프로세스로 실행하는 방식을 제안합니다.

- **장점**: `affinity_setting`/`lpage_setting`/`power_setting`은 이미 검증된 독립 실행형 도구라 로직을 다시 구현하거나 복사할 필요가 없음(surgical). 각 도구가 바뀌어도 이 오케스트레이터는 커맨드라인 인터페이스만 맞으면 그대로 동작.
- **단점**: 이 도구를 실행하는 서버/VM에 세 바이너리가 미리 빌드되어 PATH 또는 지정 경로에 있어야 함 — 배포 시 4개 바이너리(`vm-param-check` 포함하면 5개)를 다 준비해야 하는 부담이 생김. `affinity_setting`/`power_setting`은 저장소의 `old/go-lang/` 아래 있어서, 빌드하려면 이 새 도구의 README에 그 소스 경로에서 빌드하는 법도 같이 적어둬야 함.
- **대안(비권장)**: 세 도구의 로직을 Go 패키지로 import — 이러면 배포는 바이너리 1개로 끝나지만, 서로 다른 3개 프로젝트의 `main` 로직을 라이브러리로 리팩터링해야 해서 손이 훨씬 많이 감(원래 도구들 건드리게 됨, surgical 원칙 위배).

## 아키텍처 (초안)

```
FAIL기반 매개변수 수정/
├── main.go              # 플래그 파싱, 파이프라인 진입점
├── PLAN.md               # 이 계획서
├── README.md              # 설치/빌드/사용법 (vm-param-check와 동일 수준)
├── tagger/
│   └── tagger.go          # vm-param-check CSV → 태그 분류
├── homogeneity/
│   └── check.go           # 그룹 동질성 병렬 검증
├── power/
│   └── check.go           # VM 전원상태 병렬 검증
├── runner/
│   └── runner.go          # 태그별 외부 도구 병렬 실행(os/exec)
└── vendor/                 # 오프라인 빌드용
```

## 실행 예시 (초안, 확정 아님)

```bash
export VC_PASSWORD='...'
./vm-param-fix \
  -checkResult=result.csv \          # vm-param-check가 낸 상세 CSV
  -vcTargetIP=192.168.0.50 \
  -id=administrator@vsphere.local \
  -affinityTool=./affinity_setting \  # 레거시 phase4-2-affinity 빌드 결과물
  -lpageTool=./lpage_setting \        # 현재세대 vm_lpage_bulk 빌드 결과물(이름만 리네임)
  -powerTool=./power_setting          # 레거시 phase4-3-power-policy 빌드 결과물
                                       # 경로는 전부 옵션으로 지정, PATH 의존 안 함
```

## Git 업로드 계획

- 경로: `.claude/VM 매개변수설정체크/FAIL기반 매개변수 수정/` (요청하신 대로, 체크 도구 하위)
- `vendor/` 포함해서 오프라인 빌드 가능하게
- README에 vm-param-check와 동일한 수준으로: 체크리스트 항목 설명, git clone → 오프라인 빌드 → 실행법, 그리고 **이 도구가 실제로 설정을 "쓰는" 도구라는 경고 문구**(체크 도구와 달리 되돌리기 어려움)를 명확히 넣음

## 열린 질문 — 최종 결정 (구현 완료 시점 기준)

1. ~~전원정책 도구~~ → **해결**: 레거시 `power_policy.go`를 그대로 서브프로세스로 호출.
2. ~~인증 방식 통일~~ → **해결**: `vm-param-fix` 자체는 `VC_PASSWORD` + `-id`를 표준으로 사용(3개 외부 도구와 동일). 재검증 단계에서만 내부적으로 `VC_USER`/`VC_PASS`로 변환해서 `vm-param-check`를 호출.
3. **동질성 판정 범위**: 구현에서는 vCPU 수/코어당소켓수/메모리MB/디스크GB/CPU Shares(레벨+ratio)/NUMA(`numa.vcpu.maxPerVirtualNode`)/HT(`sched.vcpu0.affinity`의 콤마 구분 pCPU 개수)를 비교하고, ev01/ev02/ev03 그룹 간 VM 대수도 서로 같은지 확인한다.
4. **ev03 지원 범위**: ev03 관련 FAIL도 ev01/ev02와 동일한 기준으로 태그가 부여된다(자동교정 대상에서 배제하지 않음) — 향후 외부 도구가 ev03을 지원하면 별도 코드 수정 없이 그대로 적용된다. 단, 이 저장소에 포함된 레거시 `affinity_setting`/현재세대 `lpage_setting`은 실제로 ev03을 처리하지 않으므로 운영에서는 ev03을 지원하는 버전의 외부 도구를 지정해야 한다 — README "알려진 한계"에 명시.
5. ~~재검증 방식~~ → **해결**: 태그별 도구 실행 후 `vm-param-check`를 원래 CSV에 있던 VM 전체 대상으로 다시 통째로 재실행(부분 재조회 아님). 전역 기대값(cpu/cores/numa/mem/disk/shares/ht)은 원래 CSV에서 그대로 복원해서 넘기므로 사용자가 다시 입력할 필요 없음.
6. ~~`affinity_setting`이 병렬이 아닌 문제~~ → **해결**: 사용자 확인 — 회사 실제 환경의 `affinity_setting`/`power_setting`은 이미 병렬로 구현되어 있음(이 저장소의 레거시 파일은 테스트용 참고 코드일 뿐). `vm-param-fix`는 세 도구를 손대지 않고 그대로 서브프로세스로 부르므로, 실제 환경에서는 태그별 도구 자체도 병렬 동작한다.

## 테스트 방식 (사용자 지정)

실제 `affinity_setting`/`lpage_setting`/`power_setting`/`vm-param-check` 바이너리가 없는 개발 환경에서는, 커맨드가 올바른 인자로 정확히 호출되는지만 확인하면 된다는 지침에 따라 `echo`로 표준입력을 그대로 찍어주는 mock 스크립트로 대체해서 파이프라인 전체(태그 분류 → 동질성/전원 게이트 → dry-run → 확인 → 병렬 실행 → 재검증)를 검증했다. 실제 vCenter(192.168.0.50)의 `192ev01`/`192ev02`를 대상으로 한 라이브 테스트에서 동질성/전원 게이트, 태그 매핑, 병렬 실행, 재검증까지 전부 정상 동작 확인(레이스 디텍터 포함, 데이터 레이스 없음).

## 모델 추천

구현 자체는 vm-param-check처럼 스펙이 명확한 Go CLI 작업이라 **Sonnet 5로 충분**합니다. 다만 이번 도구는 체크만 하던 이전 것과 달리 **실제로 vCenter 설정을 변경(write)하는 도구**라, 안전장치(동질성 검증/전원 OFF 게이트/확인 플로우) 로직은 구현 후 여러 실패 시나리오(그룹 불일치, 전원 켜진 VM 섞임, 도구 실행 중 일부만 실패 등)로 꼼꼼히 검증하는 단계를 반드시 거쳐야 합니다.
