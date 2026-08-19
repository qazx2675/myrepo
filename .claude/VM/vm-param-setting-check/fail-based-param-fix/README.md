# vm-param-fix 기술문서

`vm-param-check`가 낸 상세 CSV의 FAIL/설정없음 항목을 태그(affinity/lpage/power)로 분류해서,
각 태그를 담당하는 기존 외부 도구(`affinity_setting`/`lpage_setting`/`power_setting`)를
호출해 실제 vCenter 설정을 교정하는 도구입니다. (`PLAN.md` 참고)

> ⚠️ **주의사항 (Disclaimer)**
> 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

`vm-param-check`와 동일하게 `vendor/`를 포함하고 있어 **폐쇄망에서도 오프라인 빌드**가 됩니다.

### 1) 저장소 받기 및 빌드

```bash
git clone <이 저장소 주소> myrepo
cd "myrepo/.claude/VM/vm-param-setting-check/fail-based-param-fix"
bash setup.sh
```

### 2) 외부 도구 3종 빌드

`vm-param-fix`는 아래 3개 바이너리를 서브프로세스로 호출합니다. 미리 빌드해서 PATH 또는
`-affinityTool`/`-lpageTool`/`-powerTool`로 지정한 경로에 둬야 합니다.

| 태그 | 기본 경로 | 이 저장소에서 빌드하는 법(참고용 — 실제 운영 환경은 이미 병렬화된 사내 버전을 사용) |
|---|---|---|
| `affinity` | `./affinity_setting` | `cd old/go-lang/phase4-2-affinity && bash setup.sh` |
| `lpage` | `./lpage_setting` | `cd ".claude/VM/vm-setting-go-lang" && bash setup.sh` |
| `power` | `./power_setting` | `cd old/go-lang/phase4-3-power-policy && bash setup.sh` |

**참고**: 이 저장소의 `old/go-lang/phase4-2-affinity`, `phase4-3-power-policy`는 레거시(테스트용) 버전입니다.
실제 운영 환경에는 병렬로 구현된 별도 버전이 있으면 그걸 같은 이름/플래그로 두고 쓰면 됩니다 —
`vm-param-fix`는 플래그 인터페이스(`-vcTargetIP`/`-worklistFile`/`-affinityFile`/`-id` 등)만
맞으면 어떤 구현체든 그대로 호출합니다.

### 3) 재검증 외부 도구 준비

설정 적용 후 자동으로 `vm-param-check`를 다시 돌려 재검증합니다. `-recheckTool`(기본
`./vm-param-check`)에 그 바이너리를 미리 빌드해서 둬야 합니다. (형제 프로젝트 `../vm-param-check` 참고)

### 4) 전역 명령어로 사용하기 (선택 사항)
빌드된 실행 바이너리를 매번 해당 폴더로 이동하지 않고 시스템 어디서든 기본 명령어처럼 편리하게 사용하려면, 환경 변수(`PATH`)가 지정된 기본 경로로 파일을 복사하거나 이동해 주시면 됩니다.
```bash
# 예: /usr/local/bin 경로로 복사하여 전역 명령어로 등록
sudo cp vm-param-fix /usr/local/bin/
```
이후부터는 터미널 어느 경로에서나 `vm-param-fix` 명령어만 입력하면 툴이 즉시 실행됩니다.

## 2. 사용 방법

```bash
export VC_PASSWORD='실제_비밀번호'

./vm-param-fix \
  -checkResult=result.csv \
  -vcTargetIP=192.168.0.50 \
  -id=administrator@vsphere.local
```

### 동작 순서

1. CSV를 읽어 FAIL/설정없음 항목을 태그(affinity/lpage/power/manual)로 분류
2. **[게이트] 그룹 동질성 검증**: 같은 ev01끼리, 같은 ev02끼리, 같은 ev03끼리 vCPU/코어수/메모리/디스크/Shares/NUMA/HT가 전부 같은지, 그리고 ev01/ev02/ev03 그룹 간 VM 대수가 서로 같은지 실시간 조회로 확인 — 하나라도 다르면 무엇이 다른지 보여주고 즉시 중단
3. **[게이트] 전원 OFF 검증**: 교정 대상 VM이 한 대라도 켜져 있으면 그 이름을 보여주고 즉시 중단
4. dry-run으로 "어떤 태그가 어떤 명령으로 실행될 예정인지" 전부 출력
5. `y` 입력으로 명시적 확인 (그 외 입력은 전부 취소로 처리 — 아무것도 바뀌지 않음)
6. affinity/lpage/power 태그를 **병렬로** 동시 실행 (서로 독립적인 작업이라 순서를 기다릴 필요 없음)
7. 전부 성공하면 `vm-param-check`를 원래 CSV의 VM 전체 대상으로 재실행해서 결과를 다시 CSV로 저장

## 3. 옵션별 상세 설명

- `-checkResult`: `vm-param-check`가 낸 **상세** CSV(`result.csv`, `_summary.csv` 아님). FAIL/설정없음 항목뿐 아니라 OK 항목도 포함된 CSV여야 기대값(vCPU/코어/NUMA/메모리/디스크/Shares)을 정확히 복원할 수 있습니다. `vm-param-check`가 `-cores-ev02`/`-cpu-ev02` 등으로 ev01과 다른 ev02 스펙을 체크한 CSV라면, 이 값들도 ev02 전용으로 따로 복원해서 `lpage_setting`에 `-ev02Cores`/`-ev02Numa`를 ev01과 다르게 넘깁니다(ev01/ev02 스펙이 정말 다른 환경도 지원). ev03는 코어/NUMA 관련 FAIL(`lpage` 태그)이 있으면 `lpage_setting`이 ev03를 지원하지 않아 자동교정을 거부하고 수동조치를 안내합니다.
- `-vcTargetIP` / `-id`: 필수·옵션 각각 vm-param-check와 동일한 의미.
- `-affinityFile`: ev02 affinity FAIL이 있을 때만 필요. `vm-param-check`를 처음 돌릴 때 썼던 `--affinity-ev02` 파일과 같은 것을 그대로 넘기면 됩니다(ev01은 자동계산이라 불필요).
- `-affinityTool` / `-lpageTool` / `-powerTool` / `-recheckTool`: 각 외부 도구 경로(기본값은 현재 디렉토리의 동명 바이너리).
- `-workDir`: 실행 중 생성되는 worklist 파일들을 저장할 디렉토리(기본 `.`).
- `-out`: 재검증 CSV 출력 경로(미지정 시 타임스탬프 자동생성).

## 4. 문서별 고유 설명

### 이 도구가 하지 않는 것
이 도구는 vCenter Reconfigure API를 직접 호출하지 않습니다. 실제 설정 변경은 전부 기존에
검증된 외부 도구(`affinity_setting`, `lpage_setting`, `power_setting`)에 위임하고, 이 도구는
"무엇을 어떤 순서로 안전하게 호출할지"만 담당하는 오케스트레이터입니다.

### 태그 분류 기준

| Key | 태그 |
|---|---|
| `sched.vcpuN.affinity` | `affinity` |
| `sched.mem.lpage.enable1GPage`, `sched.mem.prealloc`, `sched.mem.prealloc.pinnedMainMem`, `sched.swap.vmxSwapEnabled`, `cpuid.coresPerSocket`, `hardware.numCoresPerSocket (CPU 토폴로지 UI)`, `numa.vcpu.maxPerVirtualNode`, `config.numaInfo.coresPerNumaNode (CPU 토폴로지 UI)` | `lpage` |
| `host power policy` | `power` |
| 그 외(vCPU 수, 메모리, 디스크, Shares, Reserve all guest memory, 네트워크) | **manual** — 이 도구가 다루지 않음, 사람이 직접 조치 |

ev03(hostname에 `ev03` 포함) 관련 항목도 ev01/ev02와 동일한 기준으로 태그가 부여됩니다
(향후 `affinity_setting`/`lpage_setting`이 ev03을 지원하게 되면 별도 수정 없이 그대로
적용됩니다). 단, 현재 이 저장소의 레거시 `affinity_setting`(`old/go-lang/phase4-2-affinity`)과
현재세대 `lpage_setting`(`vm_lpage_bulk`)은 실제로 ev03을 처리하지 않으므로, ev03 대상이
있는 상태에서 이 도구들을 그대로 쓰면 해당 도구가 ev03 worklist 항목을 무시하거나 에러를
낼 수 있습니다 — ev03을 실제로 적용하려면 ev03을 지원하는 버전의 외부 도구를
`-affinityTool`/`-lpageTool`로 지정해야 합니다.

### 테스트 방법 (실제 외부 도구 없이)

`affinity_setting`/`lpage_setting`/`power_setting`/`vm-param-check`가 아직 준비되지 않은
환경에서는, 넘길 커맨드가 올바른지만 확인하면 되므로 인자를 그대로 echo하는 스크립트로
대체해서 테스트할 수 있습니다.

```bash
cat > affinity_setting <<'EOF'
#!/usr/bin/env bash
echo "[MOCK $(basename "$0")] called with: $@"
exit 0
EOF
chmod +x affinity_setting
# lpage_setting, power_setting도 동일하게 만들면 됨
```

이렇게 해두고 `vm-param-fix`를 실행하면, 동질성/전원 게이트는 실제 vCenter에 그대로
조회하면서(진짜 안전장치 동작 확인) 실제 설정 변경만 mock으로 대체되어 dry-run/확인/병렬
실행/재검증 파이프라인 전체를 안전하게 검증할 수 있습니다.

### 알려진 한계

- 그룹 동질성 검증은 vCPU 수/코어당 소켓 수/메모리/디스크/CPU Shares/NUMA(`numa.vcpu.maxPerVirtualNode`
  extraConfig 값)/HT(`sched.vcpu0.affinity` extraConfig 값이 콤마로 구분된 pCPU 2개 이상인지)와
  ev01/ev02/ev03 그룹 간 VM 대수를 비교합니다. NUMA/HT 값이 vCenter에 아예 설정되어 있지 않은
  VM은 "설정없음" 상태로 취급되어 같은 미설정 상태끼리는 동일한 것으로 봅니다.
- vCPU 수/메모리/디스크/Shares/Reserve-all-guest-memory FAIL은 자동교정 대상이 아니라
  manual로만 표시됩니다(현재 조사된 외부 도구 중 이 값들을 정확히 커버하는 게 없음).
- `-affinityFile`(ev02용)을 안 주면 ev02 affinity FAIL이 있어도 ev01만 교정되고 ev02는
  건너뜁니다(경고 메시지로 안내).
- ev03도 다른 그룹과 동일하게 태그가 부여되지만, 이 저장소에 포함된 레거시/현재세대
  외부 도구는 ev03 적용을 지원하지 않습니다 — ev03을 실제로 교정하려면 이를 지원하는
  버전의 외부 도구를 지정해야 합니다.
