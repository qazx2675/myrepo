# vm-param-check

vCenter에 있는 VM들이 고성능(High Performance) 설정 기준(CPU/메모리/NUMA 토폴로지, vCPU affinity, Shares, 호스트 전원정책 등)을 만족하는지 자동으로 점검해서, 콘솔 요약과 CSV 상세 로그로 OK/FAIL을 산출하는 도구입니다. (`PLAN.md` 참고)

## 빌드

```bash
go mod tidy   # gopkg.in/yaml.v3 등 의존성 다운로드 (인터넷 필요)
go build -o vm-param-check .
```

## 실행 모드

- **전체 순회 모드 (기본)**: `-vcenterList`에 나열된 모든 vCenter의 VM 인벤토리 전체를 체크
- **단일/지정 대상 모드**: `-f <파일>`로 체크할 BM(VM) hostname 목록을 주면 그 VM들만 체크

## 옵션 전체 목록

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-vcenterList <path>` | `vcenter.txt` | 전체 순회 모드에서 사용할 vCenter 주소 목록 파일 (한 줄에 하나) |
| `-f <path>` | (없음) | 단일/지정 대상 모드: 체크할 BM(VM) hostname 목록 파일 (한 줄에 하나, `#` 주석 가능). 지정 시 `-vcenterList`의 vCenter들 안에서 이 hostname들만 체크. 미지정 시 인벤토리 전체 체크 |
| `-ht <on\|off>` | (필수) | 하이퍼스레딩 상태 — ev01 그룹 affinity 자동계산에 사용 |
| `-cores <N>` | (필수) | 기대값: 소켓당 코어 수 |
| `-numa <N>` | (필수) | 기대값: NUMA 노드당 최대 vCPU(코어) 수 |
| `-cpu <N>` | (필수) | 기대값: vCPU 수 |
| `-mem <N>` | (필수) | 기대값: 메모리 GB |
| `-disk <N>` | (필수) | 기대값: 디스크 총량 GB |
| `-shares-ev01 <N>` | (필수) | 기대값: ev01 그룹 Shares(ratio) — CPU/메모리 Shares 둘 다 이 값으로 체크 |
| `-shares-ev02 <N>` | (옵션) | 기대값: ev02 그룹 Shares(ratio). 안 주면 ev02 shares 체크 스킵 |
| `-shares-ev03 <N>` | (옵션) | 기대값: ev03 그룹 Shares(ratio). 안 주면 ev03 shares 체크 스킵 |
| `-affinity-ev02 <path>` | (옵션) | ev02 그룹 기대 affinity 파일. 안 주면 ev02 affinity 체크 스킵 |
| `-affinity-ev03 <path>` | (옵션) | ev03 그룹 기대 affinity 파일. 안 주면 ev03 affinity 체크 스킵 |
| `-out <path>` | (없음 → 타임스탬프 자동생성) | 상세 CSV 출력 경로. 같은 이름에 `_summary`가 붙은 요약 CSV가 하나 더 생성됨 |
| `-onlyFail` | `false` | PASS(문제 없음)인 VM은 콘솔/CSV 모두에서 제외하고 FAIL/설정없음이 있는 VM만 출력 (대수 많을 때 가독성용) |
| `-noColor` | `false` | 콘솔 출력에서 ANSI 컬러(FAIL=빨강/설정없음=노랑/PASS=초록)를 끔 — 컬러 미지원 터미널/파일 리다이렉트용 |
| `-demo` | `false` | vCenter에 연결하지 않고, affinity 항목이 많은 8~16vCPU급 가짜 VM 3대(OK/FAIL/개수불일치 케이스)로 콘솔+CSV 출력을 보여주는 데모 모드. 다른 모든 플래그를 무시하고 고정된 데모 기대값 사용 |

## 사용 예시

**1. 전체 순회 모드 — vcenter.txt에 있는 모든 vCenter의 VM 전체 체크:**
```bash
./vm-param-check --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --shares-ev02=1000 --shares-ev03=1000 \
  --affinity-ev02=ev02.txt --affinity-ev03=ev03.txt \
  --out=result.csv
```

**2. 지정 대상 모드 — hostname 목록 파일로 특정 VM들만:**
```bash
# kdh.txt: 체크할 BM(VM) hostname을 한 줄씩
cat kdh.txt
# 192ev01
# 192ev02

./vm-param-check -f kdh.txt --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --out=result.csv
```

**3. 대수 많을 때 문제 있는 VM만 보기:**
```bash
./vm-param-check --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --onlyFail --out=result.csv
```

**4. 컬러 없이(파일로 리다이렉트하거나 컬러 미지원 터미널):**
```bash
./vm-param-check --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 \
  --shares-ev01=2000 --noColor --out=result.csv > report.txt
```

**5. 실제 인프라 없이 동작만 확인 — 데모 모드:**
```bash
./vm-param-check -demo
```

인증은 공통 환경변수를 사용합니다: `VC_USER`/`VC_PASS` (없으면 `VCENTER_USER`/`VCENTER_PASS`로 폴백).

```bash
export VC_USER='administrator@vsphere.local'
export VC_PASS='...'
```

## 출력 파일

`-out=result.csv`로 실행하면 파일 2개가 생성됩니다.

- **`result.csv` (상세)**: 조사한 모든 설정값을 항목(Key) 단위로 빠짐없이 기록 (OK 포함, 필터링 없음). 컬럼: `VM명, 소스, 항목Key, 기대값, 실제값, 결과, 비고`
- **`result_summary.csv` (요약)**: VM 1대당 한 줄. 컬럼: `VM명, 전체결과, OK, FAIL, 설정없음, 정보`

상세 CSV는 VM명 → 소스(카테고리) → 항목Key 순으로 정렬되어 있고, `sched.vcpuN.affinity` 같은 번호가 붙는 Key는 자연수 순서(0,1,2...10,11...)로 정렬됩니다.

결과(`결과` 컬럼) 값은 4가지입니다:
- `OK`: 기대값과 실제값 일치
- `FAIL`: 값이 설정돼 있지만 기대값과 다름
- `설정없음`: 해당 설정 자체가 없음 (FAIL과 구분 — 아예 미설정 상태)
- `정보`: OK/FAIL 판정이 없는 정보성 항목 (네트워크 포트그룹 등)

## 체크 항목 카테고리 (소스 컬럼)

| 소스 | 의미 |
|---|---|
| `-` (공통설정) | 모든 VM 공통 고정값 체크 — 메모리/스케줄러 설정(`sched.mem.*`), CPU 토폴로지(코어수/NUMA, Advanced Config + UI 값 각각), vCPU/메모리/디스크 수치, "모든 게스트 메모리 예약" |
| `host` | 이 VM이 돌고 있는 ESXi 호스트의 전원 정책 (기대값 항상 High Performance) |
| `ev01` | hostname에 `ev01` 포함된 VM — vCPU affinity(HT on/off + 코어수로 자동계산), Shares(CPU/메모리) — 항상 필수 체크 |
| `ev02` | hostname에 `ev02` 포함된 VM — affinity(파일 기반), Shares(CPU/메모리) — `-affinity-ev02`/`-shares-ev02` 옵션을 줬을 때만, VM이 1대뿐이면 스킵 |
| `ev03` | ev02와 동일하되 `ev03` 그룹 |
| `network` | 연결된 네트워크 어댑터의 포트그룹 이름 (판정 없음, 정보성) |

## 알려진 한계

- NUMA "코어수/UI 값" 비교 중 하나는 `config.numaInfo.coresPerNumaNode`(vSphere API 8.0.0.1+ 필요)를 쓰는데, 이 필드가 없는 구버전 vCenter에서는 "설정없음"으로 나옵니다.
- Shares는 계획서에 CPU/메모리 구분이 없어 `-shares-evNN` 값 하나를 CPU/메모리 Shares 양쪽에 동일하게 적용합니다.
