# 11. vm-param-check — VM 설정 점검 + 자동 교정 (주력 도구)

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🟢 체크만 할 때 / 🔴 **`-fix`를 붙이면 실제 vCenter 설정을 바꿉니다** |
| 폴더 | `.claude/VM/vm-param-check-usability-improvement/vm-param-check/` |
| 바이너리 | `vm-param-check` |
| 하는 일 | VM들이 고성능 설정 기준(CPU/메모리/NUMA/affinity/Shares/전원정책)에 맞는지 점검하고, 원하면 그 자리에서 자동 교정 후 재검증 |
| 인증 | `VC_USER` / `VC_PASS` (환경변수) |
| 대표 명령 | `./vm-param-check -vcenterList=vcenter.txt -f=targets.txt -specRoot=./SPEC_DIR -out=result.csv` |
| 연습 명령 | `./vm-param-check -demo` (vCenter 접속 없음, 아무것도 안 바꿈) |

> **이 저장소에서 가장 중요한 도구입니다.** 구버전 도구 3개(`vm-param-setting-check`, `VM_setup/vm-param-fix`, 외부 설정 스크립트들)가 하던 일을 **바이너리 하나, 명령 한 줄**로 통합했습니다. 새로 시작한다면 이것만 쓰면 됩니다.

---

## 1. 이 도구는 무엇인가

CAE 해석용 VM은 성능에 직결되는 설정값이 여러 개 있습니다(vCPU 수, 소켓당 코어 수, NUMA, CPU affinity, 메모리 예약, Shares 등 — 개념은 [01_기초지식](./01_기초지식.md) 참고). 이 값들이 **기준(기대값)과 맞는지** 확인하고, 틀렸으면 **고치는** 도구입니다.

동작 흐름:

```
[기대값 입력 또는 -specRoot 자동매칭]
        ↓
     체크 (vCenter 조회 → 비교)
        ↓
   CSV 생성 (상세 + 요약)
        ↓
  ── -fix 안 주면 여기서 끝 ──
        ↓
  게이트 검증 (그룹 동질성 + 전원 OFF)
        ↓
  dry-run 출력 → "정말 바꿀까요? (y/N)"
        ↓
     실제 적용 (병렬 Reconfigure)
        ↓
     재검증 → 재검증 CSV
```

---

## 2. 언제 쓰나

| 상황 | 실행 방법 |
|---|---|
| 신규 서버 구축 후 설정이 제대로 들어갔는지 확인 | 체크 모드 (`-fix` 없이) |
| 정기 점검 — 전체 VM이 기준에 맞는지 스캔 | 체크 모드 + `-onlyFail` |
| 점검 결과 틀린 게 나와서 고쳐야 할 때 | `-fix` (VM 전원 OFF 필수) |
| 스펙이 바뀌어서 대량으로 재적용해야 할 때 | `-specRoot` + `-fix` |

---

## 3. 준비물

1. **빌드된 바이너리** — `bash setup.sh`
2. **vCenter 계정** — 체크만 하면 읽기 전용, `-fix`를 쓰려면 **Reconfigure 권한**
3. **`vcenter.txt`** — vCenter 주소 목록 (한 줄에 하나)
4. **대상 VM 목록 파일** — `-f`로 지정. 없으면 인벤토리 전체가 대상이 됩니다
5. **기대값** — 두 가지 방법 중 하나
   - 방법 A: 명령행 옵션으로 직접 (`-cpu=16 -cores=8 ...`)
   - 방법 B: **`-specRoot` 자동매칭** (권장, 아래 5절)

---

## 4. 사용 방법

### 4-0. 먼저 데모로 감 잡기 (vCenter 접속 없음)

```bash
./vm-param-check -demo
```

가짜 VM 3대(정상 1대, FAIL 1대, 개수 불일치 1대)에 대한 결과가 색깔로 출력됩니다. 출력 형식을 익히는 용도입니다.

### 4-1. 인증 정보 설정

```bash
export VC_USER='administrator@vsphere.local'
read -rsp 'vCenter 비밀번호: ' VC_PASS; export VC_PASS; echo
```

### 4-2. 체크만 (기대값 직접 지정)

```bash
echo '192.168.0.50' > vcenter.txt
printf 'svr01ev01\nsvr01ev02\n' > targets.txt

./vm-param-check -vcenterList=vcenter.txt -f=targets.txt \
  --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 \
  --out=result.csv --user=kdh
```

→ `result_kdh.csv`(상세)와 `result_kdh_summary.csv`(VM 1대당 한 줄 요약)가 생깁니다.

### 4-3. 체크 후 바로 교정 🔴

```bash
./vm-param-check -f targets.txt \
  --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 --shares-ev01=2000 \
  --out=result.csv --fix
```

실행하면:

1. 체크 → CSV 저장
2. **게이트 검증** (아래 7절) — 실패하면 여기서 중단, 아무것도 안 바뀜
3. **바꿀 내용을 전부 화면에 출력** (dry-run)
4. `(y/N)` 입력 대기 → **`y`를 입력해야 실제로 바뀝니다**
5. 적용 → 재검증 → 재검증 CSV

### 4-4. 문제 있는 것만 보기

```bash
./vm-param-check --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --onlyFail --out=result.csv
```

VM이 수백 대일 때 필수입니다. 단, **요약 표에는 PASS 서버도 그대로 나옵니다** — "이 서버가 정상이었는지, 아예 검사가 안 된 건지"를 구분할 수 있어야 하기 때문입니다.

---

## 5. `-specRoot` 스펙 자동매칭 (권장 사용법)

매번 `-cpu -cores -numa -mem -disk -shares-ev01 -ht`를 손으로 붙이는 건 번거롭고 오타 위험도 큽니다. **VM이 들어 있는 vCenter 폴더 이름만 보고 기대값을 자동으로 채우는** 기능이 있습니다.

### 왜 되나

현업에서 VM 폴더 이름이 이미 스펙별로 규칙화되어 있기 때문입니다: `TST-CAE001-SAMP48c-QRST`

### 폴더명 매칭 규칙

폴더 이름을 하이픈(`-`)으로 나눴을 때 **정확히 4조각**이어야 하고:

| 조각 | 비교 방식 |
|---|---|
| 1번째 (`TST`) | 완전히 같아야 함 |
| 2번째 (`CAE001`) | 접두어(`CAE`/`LSI`)는 같아야 하고, **뒤의 숫자(차수)는 무시** |
| 3번째 (`SAMP48c`) | 완전히 같아야 함 |
| 4번째 (`QRST`) | 완전히 같아야 함 |

| 비교 | 결과 |
|---|---|
| `TST-CAE001-SAMP48c-QRST` vs `TST-CAE003-SAMP48c-QRST` | **같은 스펙** (차수만 다름) |
| `TST-CAE001-SAMP48c-QRST` vs `DEV-CAE001-SAMP48c-QRST` | 다른 스펙 (1번째 다름) |
| `TST-CAE001-SAMP48c-QRST` vs `TST-LSI001-SAMP48c-QRST` | 다른 스펙 (접두어 다름) |

> 허용 접두어(`CAE`/`LSI`)는 `config/spec.go`의 `caeRecord` 정규식에 고정되어 있습니다. **새 접두어가 생기면 그 한 줄에 추가**하면 됩니다. (오타 폴더가 규칙에 맞는 것처럼 인식되지 않도록 아무 영문자나 받지는 않습니다.)

### 3단계로 쓰기

#### 1단계 — 스펙을 모아둘 로컬 폴더 만들기

```bash
mkdir -p ./SPEC_DIR
```

> 이 폴더는 **도구를 실행하는 리눅스 서버의 로컬 디렉터리**입니다. vCenter 안에 있는 게 아닙니다. 이름은 아무거나 되지만 문서와 보조 스크립트는 `SPEC_DIR`로 통일해서 씁니다.

#### 2단계 — 스펙 파일 만들기

```bash
./vm-param-check -specRoot=./SPEC_DIR -initFolder="TST-CAE001-SAMP48c-QRST"
```

→ `./SPEC_DIR/TST-CAE001-SAMP48c-QRST/TST-CAE001-SAMP48c-QRST_spec.txt` 가 빈 틀로 생성됩니다.

```
# TST-CAE001-SAMP48c-QRST 스펙 정의 파일
ht=
cores=
numa=
cpu=
mem=
disk=                 # GB. 쉼표로 여러 개 주면 그 중 하나만 맞아도 OK (예: 1024,1026)
shares-ev01=          # ratio 숫자(예: 4000) 또는 normal, 쉼표로 여러 개 가능(예: 4000,normal)

# --- 선택: ev02 그룹 (없으면 ev02 관련 체크는 스킵됨) ---
# cores-ev02=
...
```

값을 채웁니다 (형식은 CLI 플래그와 동일. 하이픈은 있어도 없어도 되고 `#` 뒤는 주석):

```
ht=on
cores=20
numa=20
cpu=40
mem=16
disk=100
shares-ev01=1000
```

기존 스펙을 복사해서 시작하려면:

```bash
./vm-param-check -specRoot=./SPEC_DIR -initFolder="TST-CAE002-SAMP48c-WXYZ" -template="TST-CAE001-SAMP48c-QRST"
```

> 이미 같은 스펙으로 매칭되는 디렉터리가 있으면(차수만 달라도) **덮어쓰지 않고 에러로 중단**합니다.

**대화형으로 만들려면** 보조 스크립트를 쓰세요: `bash folder_setup.sh` — 폴더명과 필수값을 하나씩 물어봅니다.

#### 3단계 — 실행

```bash
./vm-param-check -vcenterList=vcenter.txt -f=targets.txt -specRoot=./SPEC_DIR -out=result.csv
```

내부 동작:

1. 대상 VM들이 vCenter의 어느 폴더에 속하는지 조회
2. 폴더 이름을 정규화해서 `SPEC_DIR` 아래 매칭되는 스펙을 찾음
3. **무엇을 적용할지 전부 화면에 보여주고 확인을 받음**

```
=== 폴더명 기반 스펙 자동매칭 ===

[스펙] SPEC_DIR/TST-CAE001-SAMP48c-QRST/TST-CAE001-SAMP48c-QRST_spec.txt
  vCenter 폴더: TST-CAE003-SAMP48c-QRST  (VM: svr01ev01, svr01ev02)
    [스펙적용] -ht=on
    [스펙적용] -cores=20
    ...

위 스펙으로 진행할까요? (y/N):
```

4. `y`를 눌러야 진행

### 알아둘 점

- **직접 준 옵션이 항상 우선합니다.** `-specRoot=./SPEC_DIR -cores=99`면 `cores`는 99가 쓰이고 화면에 `[수동 우선]`으로 표시됩니다.
- **여러 vCenter, 여러 폴더에 흩어져 있어도 됩니다.** VM마다 자기 폴더의 스펙을 각각 찾습니다.
- **못 찾은 대상은 반드시 경고합니다** (실행 초반과 마지막 양쪽에 표시되어 긴 로그에 묻히지 않음):

```
*** 경고: 요청한 대상을 전부 체크하지 못했습니다 ***
  어느 vCenter에서도 찾지 못한 대상 2대: 미확인VM1, 미확인VM2
  조회에 실패해 건너뛴 vCenter 1개: 192.168.0.60
```

- 스펙 파일은 **git에 커밋하는 것을 권장**합니다. 인증정보가 아니라 스펙 정의값이라 민감정보가 아니고, 팀이 함께 쓰는 게 낫습니다.
- **`_spec.txt`에는 기대값 옵션만 쓸 수 있습니다.** `-fix`나 `-out` 같은 동작 플래그를 스펙 파일에 넣으면 거부됩니다 — 스펙 파일 하나로 의도치 않게 실제 설정 변경까지 이어지는 걸 막기 위한 의도적 제한입니다.

### Task 폴더(임시 폴더)에 있는 VM

VM이 규칙에 맞는 폴더가 아니라 `Task` 같은 임시 폴더에 있으면 폴더명만으로 스펙을 정할 수 없습니다. 이때는:

1. **포트그룹명에서 유추** — 포트그룹이 `TST-CAE003-SAMP48c-QRST-cae-10-1-2-3` 형태면 앞부분에서 원래 폴더명을 복원합니다. 후보가 **정확히 하나**면 자동 진행.
2. **못 정하면 직접 물어봅니다** — `?`를 입력하면 사용 가능한 스펙 목록을 보여줍니다. 3번 시도해도 유효한 이름이 안 나오면 중단(아무것도 안 바뀜).
3. **`-yes`가 켜져 있으면 물어볼 수 없으므로 즉시 중단**합니다. 이런 VM이 섞여 있으면 `-yes` 없이 한 번 대화형으로 먼저 돌려 해결한 뒤 자동화에 태우세요.

---

## 6. 옵션 상세표

### 6-1. 대상 지정

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcenterList <path>` | `vcenter.txt` | | vCenter 주소 목록 파일 (한 줄에 하나) |
| `-f <path>` | (없음) | | 체크할 VM hostname 목록 파일. **안 주면 인벤토리 전체가 대상** |

> `-f`를 안 주면 vCenter의 모든 VM을 체크합니다. 처음 쓸 때는 반드시 `-f`로 2~3대만 지정하세요.

### 6-2. 기대값 — 공통/ev01

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-ht <on\|off>` | (없음) | ✅ | 하이퍼스레딩 상태. **ev01 affinity 자동계산에 사용** |
| `-cores <N>` | `0` | ✅ | 소켓당 코어 수 |
| `-numa <N>` | `0` | ✅ | NUMA 노드당 최대 vCPU(코어) 수 |
| `-cpu <N>` | `0` | ✅ | vCPU 수 |
| `-mem <N>` | `0` | ✅ | 메모리 GB |
| `-disk <N[,N...]>` | (없음) | ✅ | 디스크 총량 GB. **쉼표로 여러 개 주면 그 중 하나만 맞아도 OK** (예: `1024,1026`) — 환산/파티션 차이로 몇 GB 갈리는 경우 대비 |
| `-shares-ev01 <값>` | (없음) | ✅ | ev01 그룹 CPU/메모리 Shares. ratio 숫자(`4000`) 또는 `normal`, 쉼표로 여러 개(`4000,normal`) — **목록 중 하나만 맞아도 OK**(CPU/메모리 각각 독립 판정) |
| `-preferHT <값>` | (없음) | | `numa.vcpu.preferHT` 기대값(예: `TRUE`). 그룹 구분 없이 전체 VM 공통. **값을 줬을 때만 체크**하며, 안 주면 이 항목 자체가 출력에서 빠짐. 값을 줬는데 VM에 키가 없으면 "설정없음"이 아니라 **FAIL** |

> `-cores`/`-numa`/`-cpu`/`-mem`/`-disk`/`-shares-ev01`은 **ev01 그룹과 미분류 VM**(이름에 ev01/02/03이 없는 VM)에 적용됩니다.

### 6-3. 기대값 — ev02 / ev03 그룹

전부 **옵션**입니다. **안 주면 그 그룹의 해당 항목 체크를 건너뜁니다.**

| 플래그 | 설명 |
|---|---|
| `-cores-ev02` / `-cores-ev03` | 그룹별 소켓당 코어 수 |
| `-numa-ev02` / `-numa-ev03` | 그룹별 NUMA 노드당 최대 vCPU |
| `-cpu-ev02` / `-cpu-ev03` | 그룹별 vCPU 수 |
| `-mem-ev02` / `-mem-ev03` | 그룹별 메모리 GB |
| `-disk-ev02` / `-disk-ev03` | 그룹별 디스크 총량 GB (쉼표 다중값 지원) |
| `-shares-ev02` / `-shares-ev03` | 그룹별 Shares (`-shares-ev01`과 동일 문법) |

### 6-4. affinity

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-affinity-ev01 <path>` | (없음) | ev01 기대 affinity 파일. **안 주면 `-ht`/`-cores` 기반으로 자동계산** |
| `-affinity-ev02 <path>` | (없음) | ev02 기대 affinity 파일. 안 주면 ev02 affinity 체크 **스킵** |
| `-affinity-ev03 <path>` | (없음) | ev03 기대 affinity 파일. 안 주면 ev03 affinity 체크 **스킵** |

> **비교는 순서를 무시합니다.** `31,29,27,25`와 `25,27,29,31`은 같은 설정으로 봅니다. 단 개수는 봅니다 — `16,17`과 `16,17,17`은 다릅니다.

### 6-5. 스펙 자동매칭

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-specRoot <path>` | (없음) | 스펙 파일들이 모인 로컬 루트 경로. 지정하면 폴더명으로 기대값을 자동으로 채움 |
| `-initFolder <폴더명>` | (없음) | **vCenter 접속 없이** `-specRoot` 아래에 스펙 디렉터리+틀을 만들고 종료. 이름이 CAE 폴더 규칙에 맞아야 함. 같은 스펙이 이미 있으면 에러로 중단 |
| `-template <폴더명>` | (없음) | `-initFolder`와 함께: 값을 복사해올 기존 스펙 이름. 안 주면 빈 틀 |

### 6-6. 출력

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-out <path>` | 타임스탬프 자동생성 | 상세 CSV 경로. `_summary` 붙은 요약 CSV가 하나 더 생김 |
| `-user <이름>` | (없음) | **CSV 파일명 접미사**. `-out=result.csv -user=kdh` → `result_kdh.csv`, `result_kdh_summary.csv`. 여러 사람이 동시에 돌릴 때 충돌 방지 |
| `-onlyFail` | `false` | PASS인 VM을 **상세**에서 제외. **요약 표에는 PASS도 그대로 나옴** |
| `-noColor` | `false` | ANSI 컬러 끔 (파일 리다이렉트나 컬러 미지원 터미널용) |

### 6-7. 교정 🔴

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-fix` | `false` | 🔴 체크 후 게이트 검증 → dry-run → 확인 → **실제 적용** → 재검증까지 진행 |
| `-yes` | `false` | **`-specRoot` 자동매칭 확인만** 생략. ⚠️ **`-fix`의 실제 변경 확인은 이 옵션과 무관하게 항상 물어봅니다** |
| `-fixConcurrency <N>` | `20` | 동시 Reconfigure 처리 개수 |
| `-fixOut <path>` | 자동생성 | 재검증 CSV 경로 (`_recheck_<타임스탬프>` 접미사) |

#### `-yes`가 정확히 무엇을 생략하는가

| 확인 | 언제 뜨나 | `-yes`로 생략되나 |
|---|---|---|
| 스펙 자동매칭 확인 | `-specRoot`로 기대값을 채웠을 때 | **생략됨** |
| **실제 설정 변경 확인** | `-fix`로 vCenter를 바꾸기 직전 | **생략 안 됨 — 항상 물어봄** |

`-fix -yes`로 실행해도 **실제로 설정을 바꾸는 순간의 확인은 절대 건너뛰지 않습니다.** cron 같은 무인 실행에서 이 확인에 답할 수 없으면 **아무것도 바꾸지 않고 종료**합니다. 실수로 설정이 바뀌는 것보다 안전한 쪽으로 실패하도록 만든 설계입니다.

### 6-8. 테스트 모드 (vCenter 접속 없음)

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-demo` | `false` | 합성 VM 3대로 출력 형식 확인 |
| `-scale <N>` | `0` | 합성 VM N대로 대량 환경 출력 시뮬레이션 |

---

## 7. `-fix` 파이프라인 상세 🔴

### 게이트 (실제 변경 전 안전장치)

**두 검증을 모두 통과해야만** dry-run 단계로 넘어갑니다. 하나라도 실패하면 **아무것도 바꾸지 않고 즉시 중단**합니다.

| 게이트 | 내용 | 실패 시 대처 |
|---|---|---|
| **그룹 동질성** | ev01끼리, ev02끼리, ev03끼리 vCPU/코어수/메모리/디스크/Shares/NUMA/HT가 전부 같아야 하고, 그룹 간 VM 대수도 같아야 함 | 스펙이 다른 VM이 섞여 있는 것. 대상 목록을 나눠서 따로 실행 |
| **전원 OFF** | 교정 대상 VM이 **전부 꺼져 있어야 함** | CPU 토폴로지를 하드웨어 레벨로 바꾸기 때문. VM을 끄고 재실행 |

### 자동교정 대상 vs 수동조치 대상

| ✅ 자동교정 (도구가 고침) | ❌ 수동조치 (사람이 vSphere Client에서) |
|---|---|
| `sched.mem.lpage.enable1GPage` | 메모리(`config.hardware.memoryMB`) |
| `sched.mem.prealloc` | 디스크 총 용량 |
| `sched.mem.prealloc.pinnedMainMem` | CPU/메모리 Shares(ratio) |
| `sched.swap.vmxSwapEnabled` | **호스트 전원정책** |
| `cpuid.coresPerSocket` | "모든 게스트 메모리 예약" |
| `hardware.numCoresPerSocket` (CPU 토폴로지) | 네트워크 포트그룹 |
| `numa.vcpu.maxPerVirtualNode` | |
| `numa.vcpu.preferHT` (`-preferHT` 지정 시에만) | |
| `config.numaInfo.coresPerNumaNode` | |
| `sched.vcpuN.affinity` | |
| `config.hardware.numCPU` (코어수와 조합으로) | |

**핵심 원칙**: 체크 단계에서 계산한 기대값을 재해석 없이 그대로 적용합니다. vCPU 수와 소켓당 코어 수는 서로 나누어떨어져야 하므로(vSphere 제약) 둘 중 하나만 FAIL이어도 조합으로 함께 맞춥니다. 나누어떨어지지 않는 조합이면 **계획 산출 단계에서 에러로 멈춥니다**(아무것도 안 바뀜).

**VM 1대당 Reconfigure를 정확히 한 번만** 호출합니다 — Advanced Config, CPU 토폴로지, affinity를 전부 하나의 요청에 담습니다.

### 재검증

적용이 끝나면 교정된 VM만 골라 다시 조회해서 **최초 체크와 동일한 판정 로직**으로 재검사하고, 결과를 콘솔 + CSV(`_recheck_<타임스탬프>`)로 남깁니다. 남은 FAIL이 있으면 경고로 표시됩니다(대부분 원래부터 수동조치 대상이었던 항목).

---

## 8. 결과 해석

### 판정 값 5가지

| 값 | 의미 | 조치 |
|---|---|---|
| `OK` | 기대값과 일치 | 없음 |
| `FAIL` | 설정은 있으나 값이 다름 | 교정 필요 |
| `설정없음` | 아예 설정이 안 되어 있음 | 교정 필요 |
| `미지원` | **vcsim 시뮬레이터를 대상으로 할 때만 나타남.** 실제 vCenter엔 있지만 vcsim이 구현하지 않아 조회 자체가 불가능한 필드 | 실 vCenter에서는 나오지 않음. 무시 |
| `정보` | 판정 없는 정보성 항목 (예: 포트그룹 이름) | 없음 |

### 콘솔 출력

`[1] 상세` 섹션이 먼저 나오고 **맨 아래에 `[2] VM별 요약 표`** 가 나옵니다. 대수가 많으면 상세가 길어져 요약이 스크롤 밖으로 밀리므로, **마지막에 보이는 것이 요약**이 되게 배치했습니다.

색깔: FAIL=빨강, 설정없음=노랑, PASS=초록 (`-noColor`로 끌 수 있음)

### CSV 2종

| 파일 | 컬럼 | `-onlyFail` 영향 |
|---|---|---|
| 상세 CSV | `VM명, 소스, 항목Key, 기대값, 실제값, 결과, 비고` | PASS VM 제외됨 |
| 요약 CSV (`_summary`) | `VM명, 전체결과, OK, FAIL, 설정없음, 미지원, 정보` | **PASS 서버도 포함** |

### "소스" 컬럼의 의미

| 소스 | 의미 |
|---|---|
| `-` (공통설정) | 모든 VM 공통 — 메모리/스케줄러 설정, CPU 토폴로지, vCPU/메모리/디스크 수치, 메모리 예약 |
| `host` | VM이 올라간 ESXi 호스트의 전원 정책 (기대값 항상 High Performance) |
| `ev01` | hostname에 `ev01` 포함 — affinity(자동계산), Shares. **항상 필수 체크** |
| `ev02` / `ev03` | hostname에 `ev02`/`ev03` 포함 — 옵션 줬을 때만. VM이 1대뿐이면 스킵 |
| `network` | 네트워크 어댑터 포트그룹 이름 (판정 없음, 정보성) |

---

## 9. 보조 스크립트

| 스크립트 | 대신해주는 것 | 사용법 |
|---|---|---|
| `folder_setup.sh` | 스펙 디렉터리/틀 생성(5절 1~2단계)을 대화형으로 | `bash folder_setup.sh` |
| `vm_setting_check_insert.sh` | 체크/교정 실행(4절)을 변수 설정만으로 | 스크립트 상단 `VC_USER`/`VC_PASS`/`VCENTER_LIST`/`SPEC_ROOT`와 `set_user()`의 `user` 값을 채운 뒤 `bash vm_setting_check_insert.sh` |
| `update_deploy.sh` | 원격 서버(`/root/vm-param-check-usability-improvement/vm-param-check`)에 최신 소스를 배포/재빌드 | 상위 폴더에 있음 |
| `real_test.sh` / `scale_test.sh` | 실 vCenter / 스케일 테스트 | 개발용 |

> `vm_setting_check_insert.sh`는 "실제로 설정을 변경(-fix)하시겠습니까?"를 먼저 묻고, `y`를 답해도 `vm-param-check` 자체의 최종 확인을 한 번 더 거칩니다(**이중 확인**).

---

## 10. 자주 나는 오류와 해결

| 증상 | 원인 | 해결 |
|---|---|---|
| `인증 정보 로드 실패` | `VC_USER`/`VC_PASS` 미설정 | `export VC_USER=... VC_PASS=...` (이 도구는 `VC_PASSWORD`가 아니라 `VC_PASS`) |
| 게이트에서 "그룹 동질성 실패" | 대상에 스펙이 다른 VM이 섞임 | 대상 목록을 스펙별로 나눠서 실행 |
| 게이트에서 "전원 OFF 아님" | 대상 VM이 켜져 있음 | VM을 끄고 재실행 |
| `-fix` 후에도 FAIL이 남음 | 수동조치 대상 항목(메모리/디스크/Shares/전원정책) | vSphere Client에서 직접 변경 |
| 스펙을 못 찾음 | 폴더명이 규칙에 안 맞거나 스펙 파일이 없음 | `-initFolder`로 스펙 생성, 또는 대화형 입력에서 `?`로 목록 확인 |
| 대상 VM을 못 찾았다는 경고 | 이름 오타, 다른 vCenter에 있음, vCenter 접속 실패 | `vcenter.txt`에 해당 vCenter가 있는지 확인 |
| `go build` 시 네트워크 요청 | `-mod=vendor` 누락 | `bash setup.sh` 사용 |
| `설정없음`이 잔뜩 나옴 | 구버전 vCenter(8.0.0.1 미만)라 `config.numaInfo.coresPerNumaNode` 필드가 없음 | 알려진 한계. 아래 참고 |

---

## 11. 주의사항과 한계

- 🔴 **설정 변경 후 랜덤 표본 확인은 필수입니다.** 도구가 "성공"이라고 해도 vSphere Client에서 몇 대는 눈으로 확인하세요.
- `config.numaInfo.coresPerNumaNode`는 **vSphere API 8.0.0.1 이상**이 필요합니다. 구버전에서는 "설정없음"으로만 나오고 교정도 반영되지 않을 수 있습니다.
- **vcsim 시뮬레이터는 아래 3개 필드를 구현하지 않습니다.** vcsim 대상일 때만 `미지원`으로 표시되고 `-fix` 대상에서도 제외됩니다. 실 vCenter에서는 이 판정이 전혀 개입하지 않습니다.
  - `config.memoryReservationLockedToMax` ("모든 게스트 메모리 예약")
  - `config.numaInfo.coresPerNumaNode`
  - `cpuid.coresPerSocket`
- Shares는 CPU/메모리 구분 없이 `-shares-evNN` 값 하나를 양쪽에 동일하게 적용합니다.
- **호스트 전원정책은 체크만 하고 자동교정하지 않습니다.** 교정이 필요하면 `VM_setup/vm-param-fix/power_setting` 바이너리를 써야 합니다 ([10번 문서](./10_VM_setup.md) 참고).
- 같은 이름의 VM이 서로 다른 vCenter에 동시에 존재하는 경우는 **지원 범위 밖**입니다.
- `_spec.txt`에는 기대값 옵션만 쓸 수 있습니다(동작 플래그 거부).

---

## 12. 성능 (참고)

`-f`로 대상을 지정하면 "가벼운 이름 목록 조회 → 대상만 무거운 속성 조회"의 2단계로 동작합니다. 그래서 인벤토리가 커도 느려지지 않습니다.

| 인벤토리 규모 (대상은 항상 2대) | 개선 전 | 개선 후 |
|---|---|---|
| 200대 | 0.23초 | 0.04초 |
| 1,000대 | 1.02초 | 0.09초 |
| 3,000대 | 3.22초 | 0.20초 |

전체 순회 모드(`-f` 없이)는 어차피 전부 필요하므로 기존과 동일합니다.

---

## 13. 파일 구조

```
vm-param-check-usability-improvement/
├── README.md                    # 상위 개요 (무엇이 개선됐나)
├── CHANGELOG.md                 # 날짜별 변경 이력 ← 수정 시 반드시 갱신
├── 계획서.md                      # 설계 배경/검증 근거
├── update_deploy.sh             # 원격 배포 스크립트
└── vm-param-check/              # ★ 실제 도구
    ├── README.md                 # 1차 자료 (가장 정확)
    ├── main.go                   # CLI 진입점 (옵션 파싱, -specRoot 병합, 실행 흐름)
    ├── demo.go / scaletest.go    # -demo / -scale 모드
    ├── setup.sh                  # 폐쇄망 빌드
    ├── folder_setup.sh           # 스펙 생성 대화형 래퍼
    ├── vm_setting_check_insert.sh # 체크/교정 실행 래퍼 (이중 확인)
    ├── checker/                  # 점검 로직 ← "판정 기준"을 고칠 때
    │   ├── hardware.go             # CPU/메모리/디스크/shares
    │   ├── topology.go             # cores/NUMA (Auto 모드 예외 포함)
    │   ├── affinity.go             # CPU affinity
    │   ├── power.go                # 전원 정책
    │   └── preferht.go             # numa.vcpu.preferHT
    ├── config/                   # 스펙 자동매칭 ← 폴더명 규칙을 고칠 때
    │   ├── spec.go                 # 폴더명 정규화 + _spec.txt 파싱 (caeRecord 정규식)
    │   ├── init.go                 # -initFolder 스캐폴드
    │   ├── portgroup.go            # Task 폴더 예외 처리
    │   └── targets.go              # 대상 목록 로딩
    ├── fixer/                    # -fix 파이프라인 ← 교정 로직을 고칠 때
    │   ├── plan.go                 # 교정 계획(diff) 생성
    │   ├── apply.go                # 워커풀 병렬 적용
    │   ├── gates.go                # 안전장치 (동질성, 전원 OFF)
    │   └── describe.go             # dry-run 출력
    ├── model/types.go            # 공용 데이터 모델
    ├── report/                   # 콘솔/CSV 출력 ← 출력 형식을 고칠 때
    ├── vcenter/client.go         # vCenter API 접속 (2단계 조회)
    ├── testfiles/                # 테스트용 샘플
    └── vendor/                   # 의존성 (건드리지 말 것)
```

---

## 14. 관련 문서

- 1차 자료: `vm-param-check-usability-improvement/vm-param-check/README.md`
- 설계 배경: 같은 폴더의 `계획서.md`
- 변경 이력: `vm-param-check-usability-improvement/CHANGELOG.md`
- 구버전 체크 전용 도구: [16. vm-param-setting-check](./16_vm-param-setting-check.md)
- 테스트 환경 만들기: [18. vcenter-test-env-vcsim](./18_vcenter-test-env-vcsim.md), [19. 통합 테스트 패키지](./19_integrated-vm-param-check-test-tool.md)
- 이 도구를 수정하려면: [30_유지보수_AI_활용가이드](./30_유지보수_AI_활용가이드.md)
