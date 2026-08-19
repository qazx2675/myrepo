# vm-param-check (통합 체크+교정 도구)

vCenter에 있는 VM들이 고성능(High Performance) 설정 기준(CPU/메모리/NUMA 토폴로지, vCPU affinity, Shares, 호스트 전원정책 등)을 만족하는지 자동으로 점검하고, 원하면 그 자리에서 FAIL 항목을 안전장치(게이트)와 사용자 확인을 거쳐 자동 교정하고 재검증까지 마치는 도구입니다.

기존에는 "설정체크 실행 → CSV 확인 → 별도 설정수정 스크립트 실행"이 세 단계로 나뉘어 있었는데, 이 도구는 그걸 **바이너리 1개, 명령 1줄**로 통합했습니다. 설계 배경과 상세 판단 근거는 같은 폴더의 [`계획서.md`](./계획서.md)를 참고하세요.

또한 VM이 속한 vCenter 폴더 이름(예: `TST-CAE001-SAMP48c-QRST`)만으로 정상값(기대값) 옵션을 자동으로 채워주는 **폴더명 기반 스펙 자동매칭**(`-specRoot`)을 지원합니다 — 매번 `-cpu`/`-cores`/`-numa`/... 를 손으로 붙이지 않아도 됩니다. 자세한 사용법은 아래 "2. 사용 방법 > 2-4. 폴더명 기반 스펙 자동매칭" 절을 참고하세요.

```
[정상값 입력 또는 -specRoot 자동매칭] → 체크 → CSV 생성(-user 접미사) → (-fix 안 주면 여기서 끝)
                                     → 게이트(그룹 동질성 + 전원 OFF) → dry-run 확인 → y/N → 실제 적용 → 재검증 CSV
```

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

### 필요 환경
- Go 1.21 이상 (Rocky Linux에서 Go 1.26.5 기준으로 빌드·검증 완료)
- **인터넷 불필요** — `vendor/`에 의존성(`github.com/vmware/govmomi` 등)이 전부 포함되어 있어서 이 폴더 하나만 옮기면 폐쇄망에서도 바로 빌드됩니다. (폴더명 자동매칭 기능도 기존에 이미 vendor에 있던 `property` 패키지만 추가로 쓰므로, `vendor/`를 새로 채울 필요가 없습니다.)
- vCenter 접속 계정 (Reconfigure 권한 필요 — `-fix`로 실제 설정을 바꾸려면)

### 다운로드
**저장소 전체를 받는 경우:**
```bash
git clone <이 저장소 주소> myrepo
cd "myrepo/.claude/VM/vm-param-check-usability-improvement/vm-param-check"
```

**이 폴더만 필요한 경우** (예: 폐쇄망으로 옮기기 전에 이 폴더만 압축):
```bash
# 인터넷 되는 곳에서, 저장소를 받은 뒤
cd myrepo
tar czf vm-param-check.tar.gz ".claude/VM/vm-param-check-usability-improvement/vm-param-check"
```

### 빌드 (인터넷 여부와 무관 — vendor/ 포함되어 있음)
```bash
cd ".claude/VM/vm-param-check-usability-improvement/vm-param-check"
bash setup.sh
```
`setup.sh` 스크립트를 실행하면 내부적으로 `-mod=vendor` 플래그를 사용하여 인터넷에서 새로 받지 않고 `vendor/` 안의 소스만 그대로 씁니다.

의존성을 최신화하고 싶을 때만(인터넷 되는 환경에서):
```bash
go mod tidy && go mod vendor
go build -o vm-param-check .
```

### 폐쇄망(오프라인 서버)으로 이관
인터넷이 되는 환경에서 이 폴더를 통째로 압축해서 그대로 옮기면 됩니다 — `go.mod`/`go.sum`/`vendor/`가 전부 포함되어 있어서 부분 복사 없이 폴더 전체를 옮기는 게 핵심입니다.

```bash
# 1) 인터넷 되는 곳에서
cd myrepo
tar czf vm-param-check.tar.gz ".claude/VM/vm-param-check-usability-improvement/vm-param-check"

# 2) USB/scp 등으로 폐쇄망 서버로 파일 복사
scp vm-param-check.tar.gz user@폐쇄망서버:/path/to/dest/

# 3) 폐쇄망 서버에서 압축 해제 후 빌드
tar xzf vm-param-check.tar.gz
cd ".claude/VM/vm-param-check-usability-improvement/vm-param-check"
bash setup.sh
```

이 3단계만 하면 폐쇄망에서 인터넷 연결 없이 바로 빌드·실행됩니다. Go 자체가 안 깔려 있으면 Go 배포 바이너리(예: `go1.26.5.linux-amd64.tar.gz`)를 미리 받아서 `/usr/local/go`에 풀고 `PATH`에 `/usr/local/go/bin`을 추가하면 됩니다(이 단계도 인터넷 불필요, tar.gz 파일만 있으면 됨).

### 전역 명령어로 사용하기 (선택 사항)
빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 이동하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 사용할 수 있습니다.

예시 (실행 파일을 `/usr/local/bin`으로 복사):
```bash
sudo cp vm-param-check /usr/local/bin/
# 이후 어느 위치에서나 vm-param-check 명령어로 실행 가능
```

## 2. 사용 방법

### 2-1. 인증
환경변수로 받습니다(둘 중 하나만 있으면 됨):
```bash
export VC_USER='administrator@vsphere.local'
export VC_PASS='...'
# 또는
export VCENTER_USER='...'
export VCENTER_PASS='...'
```

### 2-2. 실행 모드
- **전체 순회 모드 (기본)**: `-vcenterList`에 나열된 모든 vCenter의 VM 인벤토리 전체를 체크
- **단일/지정 대상 모드**: `-f <파일>`로 체크할 BM(VM) hostname 목록을 주면 그 VM들만 체크

먼저 실제 인프라 없이 동작을 확인하려면:
```bash
./vm-param-check -demo
```

### 2-3. 사용 예시 (옵션을 직접 지정)

**기본 체크만 (기존과 동일):**
```bash
./vm-param-check --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --shares-ev02=1000 --shares-ev03=1000 \
  --affinity-ev01=ev01.txt --affinity-ev02=ev02.txt --affinity-ev03=ev03.txt \
  --out=result.csv --user=kdh
```
→ `result_kdh.csv`, `result_kdh_summary.csv` 생성.

**체크 후 바로 교정까지 (대화형 확인):**
```bash
./vm-param-check -f kdh.txt --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --out=result.csv --user=kdh --fix
```
→ 체크 → CSV 저장 → 게이트 검증 → **변경 예정 내역을 전부 화면에 출력** → `(y/N)` 입력 대기.
`y`를 입력해야 실제로 vCenter 설정이 바뀝니다. `n`이나 그 외 입력은 취소(아무 것도 안 바뀜).

**자동화 파이프라인(스크립트)에서 확인 없이 바로 적용:**
```bash
./vm-param-check -f kdh.txt --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --out=result.csv --fix --yes
```
**주의**: `-yes`는 프롬프트를 생략하므로 실제 인프라를 건드리기 전에 반드시 `-fix` 없이(체크만) 또는 `-yes` 없이 한 번 먼저 돌려서 dry-run 결과를 눈으로 확인하는 걸 권장합니다.

**대수 많을 때 문제 있는 VM만 보기:**
```bash
./vm-param-check --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --onlyFail --out=result.csv
```

### 2-4. 폴더명 기반 스펙 자동매칭 (`-specRoot`) — 처음 쓰는 분도 바로 따라할 수 있게

**무엇을 해결하나요?** 위 2-3절 예시처럼 매번 `-cpu`/`-cores`/`-numa`/`-mem`/`-disk`/`-shares-ev01`/`-ht` 같은 옵션을 손으로 붙이지 않아도, 체크하려는 VM이 어느 vCenter 폴더에 들어있는지만 보고 알맞은 값을 자동으로 채워주는 기능입니다. 현업에서는 VM 스펙별로 폴더명이 이미 규칙화되어 있다는 점(`TST-CAE001-SAMP48c-QRST`처럼)을 이용합니다.

**딱 세 단계입니다.**

#### 1단계 — 스펙 저장소 폴더 하나를 정합니다

옵션 값들을 저장해 둘 로컬 디렉터리를 아무 이름으로나 하나 만듭니다(`-specRoot`는 그냥 경로 하나를 받는 옵션이라 이름이 강제되지 않습니다. 아래 예시와 `folder_setup.sh`/`vm_setting_check_insert.sh`는 `SPEC_DIR`로 통일해서 씁니다). 이 폴더는 vCenter 안에 있는 게 아니라, 이 도구를 실행하는 서버(Rocky Linux 등)의 로컬 디렉터리입니다.

```bash
mkdir -p ./SPEC_DIR
```

#### 2단계 — 스펙 디렉터리를 만듭니다 (직접 만들거나, `-initFolder`로 자동 생성)

**폴더명 규칙**: vCenter에서 VM이 들어있는 폴더 이름(예: `TST-CAE003-SAMP48c-QRST`)을 하이픈(`-`)으로 나눴을 때, **1·3·4번째 조각은 그대로 같아야 하고, 2번째 조각은 접두어(`CAE` 또는 `LSI`) 뒤의 숫자(차수)만 다르면 같은 스펙으로 인정**됩니다. 즉 `TST-CAE001-SAMP48c-QRST`와 `TST-CAE003-SAMP48c-QRST`는 (차수만 다르므로) **같은 스펙**으로 취급되고, `TST-CAE001-SAMP48c-QRST`와 `DEV-CAE001-SAMP48c-QRST`는 (1번째 조각이 다르므로) **다른 스펙**입니다. 접두어가 다르면(`TST-CAE001-SAMP48c-QRST` vs `TST-LSI001-SAMP48c-QRST`) 역시 **다른 스펙**입니다.

가장 쉬운 방법은 `-initFolder`로 만드는 것입니다(빈 틀이 자동으로 생깁니다):

```bash
./vm-param-check -specRoot=./SPEC_DIR -initFolder="TST-CAE001-SAMP48c-QRST"
```

실행하면 이런 파일이 생깁니다 — `./SPEC_DIR/TST-CAE001-SAMP48c-QRST/TST-CAE001-SAMP48c-QRST_spec.txt`:

```
# TST-CAE001-SAMP48c-QRST 스펙 정의 파일
ht=
cores=
numa=
cpu=
mem=
disk=                 # GB. 쉼표로 여러 개 주면 그 중 하나만 맞아도 OK (예: 1024,1026)
shares-ev01=          # ratio 숫자(예: 4000) 또는 normal

# --- 선택: ev02 그룹 (없으면 ev02 관련 체크는 스킵됨) ---
# cores-ev02=
...
```

이 파일을 열어 값을 채웁니다(형식은 CLI 플래그와 동일 — 하이픈은 있어도 없어도 되고, `#` 뒤는 주석):

```
ht=on
cores=20
numa=20
cpu=40
mem=16
disk=100
shares-ev01=1000
```

Shares를 Custom ratio로 박지 않고 vCenter 기본값(Normal) 그대로 두는 스펙이라면 숫자 대신 `normal`이라고 적으면 됩니다. 이때는 ratio 숫자를 비교하지 않고 **CPU/메모리 Shares Level이 둘 다 `normal`인지**만 봅니다.

```
shares-ev01=normal
```

디스크 총량이 같은 스펙인데도 환산/파티션 차이로 몇 GB 갈리는 경우에는, 허용값을 쉼표로 여러 개 적으면 **그 중 하나와 맞으면 OK**로 판정합니다(`disk-ev02`/`disk-ev03`도 동일).

```
disk=1024,1026
```

이미 비슷한 스펙이 있다면 `-template`으로 그 값을 그대로 복사해서 시작할 수도 있습니다(다른 부분만 고치면 됨):

```bash
./vm-param-check -specRoot=./SPEC_DIR -initFolder="TST-CAE002-SAMP48c-WXYZ" -template="TST-CAE001-SAMP48c-QRST"
```

> 이미 같은 스펙으로 매칭되는 디렉터리가 있으면(차수만 달라도) 실수로 덮어쓰지 않도록 `-initFolder`가 자동으로 막습니다.

#### 3단계 — `-specRoot`로 체크를 실행합니다

기대값 옵션을 하나도 안 주고, `-specRoot`만 줍니다:

```bash
export VC_USER='administrator@vsphere.local'
export VC_PASS='...'
./vm-param-check -vcenterList=vcenter.txt -f=kdh.txt -specRoot=./SPEC_DIR -out=result.csv
```

내부적으로 이렇게 동작합니다.

1. `kdh.txt`에 적힌 대상 VM들이 vCenter의 어느 인벤토리 폴더에 속하는지 조회합니다.
2. 그 폴더 이름을 규칙에 따라 정규화해서, `-specRoot` 아래에서 같은 스펙으로 매칭되는 디렉터리를 찾습니다.
3. 찾은 `_spec.txt`의 값으로 `-cpu`/`-cores`/... 옵션을 자동으로 채우고, **무엇을 적용할지 화면에 전부 보여준 뒤** 확인을 받습니다:
   ```
   === 폴더명 기반 스펙 자동매칭 ===

   [스펙] SPEC_DIR/TST-CAE001-SAMP48c-QRST/TST-CAE001-SAMP48c-QRST_spec.txt
     vCenter 폴더: TST-CAE003-SAMP48c-QRST  (VM: 1번VM, 2번VM)
       [스펙적용] -ht=on
       [스펙적용] -cores=20
       ...

   위 스펙으로 진행할까요? (y/N):
   ```
4. `y`를 눌러야 체크가 진행됩니다. (`-yes`를 주면 이 확인만 생략됩니다 — 아래 "`-yes`의 범위" 참고)

**직접 지정한 옵션이 항상 우선합니다.** 예를 들어 `-specRoot=./SPEC_DIR -cores=99`처럼 같이 주면, `cores`는 스펙 파일 값이 아니라 직접 준 `99`가 쓰이고 화면에 `[수동 우선]`으로 표시됩니다. 나머지 옵션은 그대로 스펙에서 채워집니다.

**대상이 여러 vCenter나 여러 폴더에 걸쳐 있어도 괜찮습니다.** VM마다 자기가 속한 폴더의 스펙을 각각 찾아서 적용하므로, `vcenter.txt`에 vCenter 여러 개를 넣고 `-f`로 서로 다른 스펙에 속한 VM들을 한꺼번에 대상으로 지정해도 각자 올바른 스펙으로 체크됩니다.

**요청한 대상을 못 찾으면 반드시 경고합니다.** `-f`로 지정한 VM 중 일부를 어느 vCenter에서도 못 찾았거나, vCenter 중 하나가 접속에 실패했으면 결과와 별개로 아래처럼 경고가 뜹니다(실행 초반과 마지막 양쪽에 다시 뜨므로 긴 로그에 묻히지 않습니다):

```
*** 경고: 요청한 대상을 전부 체크하지 못했습니다 ***
  어느 vCenter에서도 찾지 못한 대상 2대: 미확인VM1, 미확인VM2
  조회에 실패해 건너뛴 vCenter 1개: 192.168.0.60
```

#### 보조 스크립트

1·2단계는 `folder_setup.sh`(대화형으로 폴더명·값을 물어보고 `SPEC_DIR` 아래에 생성), 3단계는 `vm_setting_check_insert.sh`(상단 변수와 `set_user()`만 채워두면 `-f`/`-out`을 자동으로 구성)로 대신할 수 있습니다. 둘 다 이 폴더에 있고 `bash <스크립트명>`으로 실행합니다.

### 2-5. Task 폴더(임시 작업 폴더)에 있는 VM 체크하기

VM이 위 규칙에 맞는 정식 CAE 폴더가 아니라 `Task`처럼 임시로 모아둔 폴더에 있으면, 폴더 이름만으로는 스펙을 정할 수 없습니다. 이럴 때는 아래 순서로 자동 처리를 시도합니다.

1. **포트그룹명에서 유추**: VM에 붙은 네트워크 어댑터의 포트그룹 이름이 `<원래폴더명>-cae-옥텟-옥텟-옥텟-옥텟` 형태(예: `TST-CAE003-SAMP48c-QRST-cae-10-1-2-3`)면, 거기서 원래 폴더명을 복원해서 그 스펙으로 자동 진행합니다. 화면에는 이렇게 표시됩니다.
   ```
   vCenter 폴더: Task  (VM: 1번VM, 2번VM)  ※ CAE 규칙과 안 맞아 포트그룹 파싱으로 스펙 결정
   ```
2. **자동으로 못 정하면 직접 물어봅니다**: 포트그룹으로도 못 찾으면(또는 후보가 여러 개면) 아래처럼 물어봅니다. `?`를 입력하면 `-specRoot` 아래 사용 가능한 스펙 목록을 보여줍니다.
   ```
   이 VM에 적용할 스펙의 vCenter 폴더명을 입력하세요 (목록을 보려면 ? 입력):
   ```
   3번 시도해도 유효한 이름을 못 받으면 중단합니다(아무것도 바뀌지 않음).
3. **자동화 실행(`-yes`) 중에는 물어볼 수 없으므로 즉시 중단합니다.** 이런 VM이 섞여 있으면 `-yes` 없이 한 번 대화형으로 먼저 실행해서 해결한 뒤 자동화에 태우는 걸 권장합니다.

### `-yes`가 정확히 무엇을 생략하는지

확인 프롬프트는 두 종류이고 **성격이 다릅니다**.

| 확인 | 언제 뜨나 | `-yes`로 생략되나 |
|---|---|---|
| 스펙 자동매칭 확인 | `-specRoot`로 기대값을 자동으로 채웠을 때 | **생략됨** |
| 실제 설정 변경 확인 | `-fix`로 vCenter 설정을 실제로 바꾸기 직전 | **생략 안 됨 — 항상 물어봄** |

즉 `-fix -yes`로 실행해도 **실제로 설정을 바꾸는 순간의 확인은 절대 건너뛰지 않습니다.** 무인 실행(cron 등)에서 이 확인에 답할 수 없으면 그냥 "아무 것도 바꾸지 않고" 종료합니다 — 실수로 설정이 바뀌는 것보다 안전한 쪽으로 실패하도록 만든 설계입니다.

## 3. 옵션별 상세 설명

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
| `-disk <N[,N...]>` / `-disk-ev02` / `-disk-ev03` | 필수/옵션/옵션 | 기대값: 디스크 총량 GB. **쉼표로 여러 개를 주면 그 중 하나와 맞으면 OK** (예: `-disk=1024,1026`) — 같은 스펙인데도 환산/파티션 차이로 몇 GB 갈리는 경우를 위해서. 3개 옵션 모두 동일하게 지원 |
| `-shares-ev01 <N\|normal>` / `-shares-ev02` / `-shares-ev03` | 필수/옵션/옵션 | 기대값: CPU/메모리 Shares(ratio). `-shares-ev01`만 `normal`을 받으며, 이때는 ratio 숫자 대신 **CPU/메모리 Shares Level이 둘 다 `normal`인지**를 봅니다. `-shares-ev02`/`-shares-ev03`는 숫자만 |
| `-affinity-ev01 <path>` | (옵션) | ev01 기대 affinity 파일. 안 주면 `-ht`/`-cores` 기반 자동계산 |
| `-affinity-ev02 <path>` / `-affinity-ev03 <path>` | (옵션) | ev02/ev03 기대 affinity 파일. 안 주면 해당 그룹 affinity 체크 스킵 |

> **affinity 비교는 순서를 무시합니다.** `sched.vcpuN.affinity`는 "이 vCPU를 돌릴 수 있는 물리 CPU 목록"이라 순서에 의미가 없으므로(우선순위가 아니고, ESXi 스케줄러가 그 안에서 배치), `31,29,27,25,23,21,19,17`과 `17,19,21,23,25,27,29,31`은 **같은 설정으로 보고 OK**입니다. 단 개수는 맞춰서 보므로 `16,17`과 `16,17,17`은 다른 것으로 판정합니다(잘못 들어간 중복을 조용히 통과시키지 않기 위해). 리포트에는 vCenter에 실제로 박힌 원본 문자열이 그대로 표시됩니다.
| `-out <path>` | 타임스탬프 자동생성 | 상세 CSV 경로. `_summary` 붙은 요약 CSV가 하나 더 생성됨 |
| `-user <이름>` | (없음) | **CSV 파일명 접미사**. `-out=result.csv -user=kdh` → `result_kdh.csv`, `result_kdh_summary.csv`. 여러 사람이 동시에 실행할 때 파일명 충돌 방지용 |
| `-onlyFail` | `false` | PASS인 VM은 **상세**(콘솔/상세 CSV)에서 제외하고, FAIL/설정없음/미지원 있는 VM만 출력. **VM별 요약(콘솔 맨 아래 표 + 요약 CSV)에는 PASS 서버도 그대로 나옵니다** — 어떤 서버가 검사됐고 이상 없었는지가 요약에서 사라지면 안 되므로 |
| `-noColor` | `false` | 콘솔 ANSI 컬러 끔 |
| `-demo` | `false` | vCenter 연결 없이 합성 VM 3대로 동작 확인 |
| `-scale <N>` | `0` | vCenter 연결 없이 N대 규모 합성 VM으로 대량 환경 출력 시뮬레이션 |

### 폴더명 기반 스펙 자동매칭 관련 (신규 — 위 2-4절 참고)
| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-specRoot <path>` | (없음) | 스펙 정의 파일들이 모여 있는 로컬 루트 경로. 지정하면 체크 대상 VM이 속한 vCenter 인벤토리 폴더 이름을 조회해서, 같은 스펙으로 간주되는 하위 디렉터리의 `<디렉터리명>_spec.txt`를 찾아 기대값 옵션을 자동으로 채운다. **직접 준 옵션이 항상 우선**하며, 적용 전에 확인을 한 번 받는다(단, 이 확인은 `-yes`로 생략 가능 — 아래 `-fix` 관련 표의 `-yes` 설명 참고) |
| `-initFolder <폴더명>` | (없음) | **vCenter에 연결하지 않고** `-specRoot` 아래에 이 이름의 스펙 디렉터리와 `<이름>_spec.txt` 스캐폴드를 만들고 종료한다. 이름은 CAE 폴더 규칙(하이픈 4개 레코드, 2번째가 `CAE<숫자>` 또는 `LSI<숫자>`)을 따라야 한다. 이미 같은 스펙(차수만 달라도)이 있으면 덮어쓰지 않고 에러로 중단 |
| `-template <폴더명>` | (없음) | `-initFolder`와 함께 사용: 값을 그대로 복사해올 기존 스펙의 vCenter 폴더 이름. 안 주면 값이 비어있는 빈 틀만 생성 |

### 교정(-fix) 관련 (신규)
| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-fix` | `false` | 체크 완료 후 FAIL/설정없음 항목을 **게이트 검증 → dry-run 확인 → 자동교정 → 재검증**까지 이어서 진행. 안 주면 기존과 동일하게 체크+CSV까지만 |
| `-yes` | `false` | `-specRoot`로 자동매칭된 스펙에 대한 확인 프롬프트만 생략한다. **`-fix`가 실제로 vCenter 설정을 바꾸기 직전의 확인은 이 옵션과 무관하게 항상 물어본다** — 실수로 무인 변경되는 경로를 아예 만들지 않기 위함(자세한 비교표는 2-4절 끝 "`-yes`가 정확히 무엇을 생략하는지" 참고) |
| `-fixConcurrency <N>` | `20` | `-fix` 적용 시 동시 Reconfigure 처리 개수 |
| `-fixOut <path>` | 원본 CSV 이름 기준 자동생성 | 재검증 CSV 경로 (`_recheck_<타임스탬프>` 접미사) |

## 4. 문서별 고유 설명

### 4.1 `-fix` 파이프라인 상세
#### 게이트 (실제 변경 전 안전장치)
두 검증을 통과해야만 dry-run 확인 단계로 넘어갑니다. 하나라도 실패하면 **아무것도 바꾸지 않고 즉시 중단**합니다.
- **그룹 동질성**: ev01끼리, ev02끼리, ev03끼리 vCPU/코어수/메모리/디스크/CPU Shares/NUMA/HT가 전부 같아야 하고, ev01/ev02/ev03 그룹 간 VM 대수도 서로 같아야 함
- **전원 OFF**: 교정 대상 VM이 전부 꺼져 있어야 함 (CPU 토폴로지를 하드웨어 레벨로 직접 바꾸기 때문)

#### 자동교정 대상 vs 수동조치 대상
| 자동교정 (fixable) | 수동조치 (manual — 이 도구가 다루지 않음) |
|---|---|
| `sched.mem.lpage.enable1GPage` / `sched.mem.prealloc` / `sched.mem.prealloc.pinnedMainMem` / `sched.swap.vmxSwapEnabled` (고정값) | 메모리(`config.hardware.memoryMB`) |
| `cpuid.coresPerSocket` (Advanced Config) | 디스크 총 용량 |
| `hardware.numCoresPerSocket` (CPU 토폴로지 UI → `NumCoresPerSocket`) | CPU/메모리 Shares(ratio) |
| `numa.vcpu.maxPerVirtualNode` (Advanced Config) | 호스트 전원정책 |
| `config.numaInfo.coresPerNumaNode` (CPU 토폴로지 UI → `VirtualNuma.CoresPerNumaNode`, 설정편집 > VM 옵션 > CPU 토폴로지 화면의 그 필드) | "모든 게스트 메모리 예약" |
| `sched.vcpuN.affinity` (ev01은 `-ht`+vCPU로 자동계산, ev02/ev03는 `-affinity-evNN` 파일) | 네트워크 포트그룹 |
| `config.hardware.numCPU` (vCPU 수, 코어수와 함께 조합으로 교정) | |

**핵심 원칙**: 체크 단계에서 계산한 기대값을 재해석 없이 그대로 기록합니다. vCPU 수/소켓당 코어 수는 서로 나누어떨어져야 하므로(vSphere 제약), 둘 중 하나만 FAIL이어도 기대값 조합으로 함께 맞춥니다. 나누어떨어지지 않는 조합이면 계획 산출 단계에서 에러로 멈춥니다(아무것도 바꾸지 않음).
VM 1대당 vCenter `Reconfigure`를 **정확히 한 번만** 호출합니다 — Advanced Config, CPU 토폴로지, affinity를 전부 하나의 요청에 담아서 보냅니다.

#### dry-run + 확인
적용 전에 VM별로 "무엇을 어떤 값에서 어떤 값으로 바꿀지"를 전부 출력하고, 수동조치 대상 항목은 따로 개수만 요약해서 보여줍니다. `-yes`가 없으면 `y`를 입력해야 다음 단계로 진행됩니다.

#### 재검증
적용이 끝나면 교정된 VM만 골라 vCenter에서 다시 조회해서 **최초 체크와 동일한 판정 로직**으로 다시 검사하고, 결과를 콘솔 + CSV(`_recheck_<타임스탬프>`)로 남깁니다. 남은 FAIL/설정없음이 있으면 경고로 표시됩니다(대부분 원래부터 수동조치 대상이었던 항목).

### 4.2 콘솔 출력 / 출력 파일 형식
콘솔은 `[1]` 상세 섹션을 먼저 보여주고, 맨 아래에 `[2] VM별 요약 표`가 나옵니다. 대수가 많으면 상세가 길어져서 요약이 스크롤 밖으로 밀리기 때문에, **마지막에 보이는 것이 요약**이 되도록 아래에 둡니다.

`-onlyFail`이면 **상세**는 FAIL이 있는 VM만, 그 안에서도 FAIL/설정없음/미지원 항목만 보여줍니다. 하지만 **요약 표에는 PASS 서버도 전부 나옵니다** — 여기서까지 빠지면 "이 서버가 정상이었는지, 아예 검사가 안 된 건지"를 구분할 수 없기 때문입니다.

CSV 2종(상세/요약)이 생성됩니다.
- **상세 CSV**: `VM명, 소스, 항목Key, 기대값, 실제값, 결과, 비고` — OK 포함(`-onlyFail` 지정 시엔 콘솔 상세와 동일하게 PASS VM 제외)
- **요약 CSV**: `VM명, 전체결과, OK, FAIL, 설정없음, 미지원, 정보` — VM 1대당 한 줄. `-onlyFail`이어도 PASS 서버 포함

결과 값은 5가지: `OK`(일치) / `FAIL`(설정은 있으나 다름) / `설정없음`(아예 미설정) / `미지원`(대상이 vcsim 시뮬레이터일 때만 나타남 — 실제 vCenter엔 있지만 vcsim이 구현하지 않아 값 자체를 조회할 수 없는 필드. 아래 4.6절 "알려진 한계" 참고) / `정보`(판정 없는 정보성 항목).

### 4.3 체크 항목 카테고리 (소스 컬럼)
| 소스 | 의미 |
|---|---|
| `-` (공통설정) | 모든 VM 공통 — 메모리/스케줄러 설정, CPU 토폴로지, vCPU/메모리/디스크 수치, 메모리 예약 |
| `host` | VM이 돌고 있는 ESXi 호스트의 전원 정책 (기대값 항상 High Performance) |
| `ev01` | hostname에 `ev01` 포함 — affinity(자동계산), Shares — 항상 필수 체크 |
| `ev02` / `ev03` | hostname에 `ev02`/`ev03` 포함 — affinity(파일 기반), Shares — 옵션 줬을 때만, VM 1대뿐이면 스킵 |
| `network` | 네트워크 어댑터 포트그룹 이름 (판정 없음, 정보성) |

### 4.4 폴더명 기반 스펙 자동매칭 상세

#### 매칭 규칙
폴더명을 하이픈(`-`)으로 나눴을 때 정확히 4개 조각이어야 하고, **1·3·4번째 조각은 완전히 같아야, 2번째 조각은 접두어(`CAE`/`LSI`) 뒤 숫자(차수)만 정규화(무시)하고 비교**합니다. 예: `TST-CAE001-SAMP48c-QRST`와 `TST-CAE003-SAMP48c-QRST`는 같은 스펙, `TST-CAE001-SAMP48c-QRST`와 `DEV-CAE001-SAMP48c-QRST`는 다른 스펙입니다. **접두어는 정규화하지 않으므로** `TST-CAE001-SAMP48c-QRST`와 `TST-LSI001-SAMP48c-QRST`도 서로 다른 스펙입니다. 이 규칙에 안 맞는 폴더명(4개 조각이 아니거나 2번째가 `CAE<숫자>`/`LSI<숫자>` 형태가 아닌 경우, 예: `Task`)은 아래 "Task 폴더 예외 처리"로 넘어갑니다.

허용 접두어는 `config/spec.go`의 `caeRecord` 정규식에 목록으로 고정되어 있습니다. 새 접두어가 생기면 그 한 줄에 추가하면 됩니다(오타 폴더가 규칙에 맞는 것처럼 인식되지 않도록 아무 영문자나 받지는 않습니다).

#### 대상 VM만 조회 (성능)
`-f`로 대상을 지정하면, 예전에는 인벤토리 전체 VM의 무거운 속성(하드웨어/ExtraConfig 등)을 받아온 뒤 클라이언트에서 걸러냈습니다. 지금은 가벼운 이름 목록만 먼저 조회해서 대상 VM의 moref를 추린 뒤, 그 대상에 대해서만 무거운 속성을 조회합니다(`property.Collector.Retrieve`). vcsim으로 측정한 결과, 대상은 항상 2대로 고정한 채 인벤토리 규모만 키워보면:

| 인벤토리 규모 | 개선 전 | 개선 후 |
|---|---|---|
| 200대 | 0.23초 | 0.04초 |
| 1,000대 | 1.02초 | 0.09초 |
| 3,000대 | 3.22초 | 0.20초 |

개선 전은 인벤토리 규모에 거의 정비례해서 느려지지만, 개선 후에는 거의 영향을 받지 않습니다. 전체 순회 모드(`-f` 없이)는 어차피 전부 필요하므로 기존과 동일하게 동작합니다.

#### Task 폴더 예외 처리 상세
VM이 속한 폴더가 위 매칭 규칙에 안 맞으면(예: 임시 작업용 `Task` 폴더), VM 단위로 아래 순서를 시도합니다.
1. VM에 붙은 네트워크 어댑터의 포트그룹 이름들에서 `<원래폴더명>-cae-옥텟-옥텟-옥텟-옥텟` 패턴을 찾아 원래 폴더명을 복원하고, 그 이름으로 스펙이 실제로 있는지 확인합니다. **후보가 정확히 하나**면 자동으로 그 스펙을 씁니다.
2. 후보가 없거나 여러 개면(자동으로 하나를 고를 근거가 없으면) 사람에게 직접 입력받습니다(`?`로 사용 가능한 스펙 목록 조회 가능, 3회 시도 후 중단).
3. `-yes`가 켜져 있으면 2번(대화형 입력)으로 들어갈 수 없으므로, 자동으로 못 정하는 즉시 중단합니다.

같은 `Task` 폴더 안에 서로 다른 스펙의 VM이 섞여 있어도 VM마다 개별적으로 해석하므로 문제없이 나뉩니다. dry-run 화면에도 "CAE 규칙과 안 맞아 포트그룹 파싱으로 스펙 결정"처럼 **일반 폴더명 매칭과 예외 경로를 구분해서** 표시합니다.

### 4.5 검증 이력
- **단위 테스트**: `go test -mod=vendor ./...` — `fixer`(계획 산출/게이트), `config`(폴더명 매칭/스펙 파싱/포트그룹 파싱/`-initFolder`) 패키지 전부 통과
- **vcsim(재현 환경)**: 실제 vCenter 192.168.0.50의 `1번VM`/`2번VM` 구조를 그대로 재현한 vcsim에서 `-fix` 파이프라인 전체(게이트 → dry-run → 취소 → 적용 → 재검증) 실행 확인. `cpuid.coresPerSocket`, `numa.vcpu.maxPerVirtualNode`, `NumCoresPerSocket` 하드웨어 필드 교정 정상 동작 확인
- **실제 vCenter(192.168.0.50)**: `1번VM`/`2번VM` 대상으로 동일 파이프라인 실행, 적용 후 재검증에서 전부 OK 전환 확인. vSphere Client의 설정 편집 > VM 옵션 > CPU 토폴로지 화면에서 "Cores per Socket"/"NUMA 노드당 코어 수"가 실제 숫자로 표시되는 것까지 확인
- **폴더명 자동매칭**: vcsim에 실제와 같은 형태의 CAE 폴더/`Task` 폴더를 만들어 종단 테스트 — 정상 폴더 매칭(차수만 다른 경우 포함), 수동 옵션 우선, 확인 취소, 매칭 실패, 다중 vCenter에 흩어진 대상이 각자 다른 스펙으로 매칭되는 경우, 포트그룹 유추, 대화형 폴백까지 전부 확인. 매번 `-specRoot` 없이 실행한 결과가 이 CSV들과 완전히 동일함을 확인해 기존 사용 방식에 회귀가 없음을 검증

### 4.6 알려진 한계
- `config.numaInfo.coresPerNumaNode`(CPU 토폴로지 UI의 NUMA 노드당 코어 수)는 vSphere API 8.0.0.1+ 필요. 이 필드가 없는 구버전 vCenter에서는 "설정없음"으로만 나오고 교정도 반영되지 않을 수 있습니다.
- **vcsim(`127.0.0.1:54321`) 시뮬레이터는 아래 3개 필드를 자체적으로 구현하지 않습니다** — vcsim을 대상으로 체크할 때만 이 필드들이 `설정없음`이 아니라 `미지원`으로 구분 표시되고(값이 없는 게 아니라 "이 시뮬레이터로는 확인 자체가 불가능하다"는 의미), `-fix` 대상에서도 자동 제외됩니다. 실제 vCenter를 대상으로 할 때는 이 판정이 전혀 개입하지 않고 항상 정상적으로 OK/FAIL/설정없음이 나옵니다(검증 이력의 192.168.0.50 실측 참고).
  - `config.memoryReservationLockedToMax` ("모든 게스트 메모리 예약")
  - `config.numaInfo.coresPerNumaNode` (NUMA 노드당 코어 수, CPU 토폴로지 UI 값)
  - `cpuid.coresPerSocket` (소켓당 코어 수, Advanced Config)
- Shares는 CPU/메모리 구분 없이 `-shares-evNN` 값 하나를 양쪽에 동일하게 적용합니다.
- 호스트 고성능 전원정책 변경은 이번 범위에 포함하지 않습니다(체크만, 자동교정 대상 아님).
- `_spec.txt`는 기대값 관련 옵션(`-cpu`/`-cores`/`-affinity-evNN` 등)만 지정할 수 있습니다. `-fix`/`-out` 같은 동작 플래그는 스펙 파일에서 지정하면 거부됩니다 — 스펙 파일 하나로 의도치 않게 실제 설정 변경까지 이어지는 걸 막기 위한 의도적인 제한입니다.
- 여러 vCenter를 동시에 순회할 때 **같은 이름의 VM이 서로 다른 vCenter에 동시에 존재하는 경우는 지원 범위 밖**입니다(현재 운용 환경에서는 vCenter별로 관리 대상 호스트가 겹치지 않아 발생하지 않는 것으로 확인됨).

### 4.7 기존 개별 도구와의 관계
저장소의 `vm-param-setting-check/`(체크 전용)와 `vm-param-setting-check/fail-based-param-fix/`(레거시 외부 도구 오케스트레이션 방식)는 삭제하지 않고 그대로 남겨뒀습니다. 이 폴더는 그 둘을 대체하는 통합본입니다 — 새로 시작하는 경우 이 폴더만 쓰면 됩니다.
