# 16. vm-param-setting-check — VM 설정 점검 (구버전, 체크 전용)

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🟢 읽기 전용 (교정 기능 없음) |
| 폴더 | `.claude/VM/vm-param-setting-check/` |
| 바이너리 | `vm-param-check` (이름이 같지만 **11번 도구와 다른 구버전 바이너리**입니다) |
| 상태 | **레거시.** [11. vm-param-check](./11_vm-param-check-usability-improvement.md)로 대체됨 |
| 인증 | `VC_USER` / `VC_PASS` |

> **새로 시작한다면 이 폴더를 쓰지 마세요.** [11번 도구](./11_vm-param-check-usability-improvement.md)가 이 도구의 모든 기능 + 자동교정(`-fix`) + 스펙 자동매칭(`-specRoot`) + 2단계 조회(성능)를 포함합니다.
> 이 폴더는 **삭제하지 않고 남겨둔 것**이며, 과거 결과를 재현하거나 비교해야 할 때만 씁니다.

---

## 1. 11번 도구와 무엇이 다른가

| 항목 | 16번 (이 문서, 구버전) | 11번 (현행) |
|---|---|---|
| 자동 교정 (`-fix`) | ❌ 없음 | ✅ 있음 (게이트 + dry-run + 재검증) |
| 스펙 자동매칭 (`-specRoot`) | ❌ 없음 — 매번 옵션을 손으로 입력 | ✅ 폴더명으로 자동 |
| `-disk` 다중값 | ❌ 단일값만 | ✅ `1024,1026` 쉼표 허용 |
| `-shares` `normal` 지원 | ❌ ratio 숫자만 | ✅ 숫자/`normal`/혼합 목록 |
| `-preferHT` 체크 | ❌ 없음 | ✅ 있음 |
| `-user` (CSV 접미사) | ❌ 없음 | ✅ 있음 |
| 대상 지정 시 조회 속도 | 인벤토리 크기에 비례 (3,000대 → 3.22초) | 2단계 조회 (3,000대 → 0.20초) |
| `-onlyFail` 동작 | 콘솔/CSV 모두에서 PASS 제외 | 상세에서만 제외, **요약에는 PASS 포함** |
| 못 찾은 대상 경고 | 조용히 넘어감 | 반드시 경고 |

> **NUMA Auto 모드 버그 수정(2026-08-23)은 이 도구에도 함께 적용**되었습니다. `autoCoresPerNumaNode=true`인 VM의 `coresPerNumaNode`를 무시하고 "설정없음"으로 처리합니다.

---

## 2. 빌드와 실행

```bash
cd .claude/VM/vm-param-setting-check
bash setup.sh

export VC_USER='administrator@vsphere.local'
read -rsp 'vCenter 비밀번호: ' VC_PASS; export VC_PASS; echo

./vm-param-check -demo    # vCenter 접속 없이 동작 확인
```

실제 체크:

```bash
./vm-param-check -vcenterList=vcenter.txt -f=kdh.txt \
  -ht=on -cores=8 -numa=8 -cpu=16 -mem=64 -disk=500 \
  -shares-ev01=2000 -out=result.csv
```

---

## 3. 옵션 상세표

### 대상 지정

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-vcenterList <path>` | `vcenter.txt` | vCenter 주소 목록 파일 |
| `-f <path>` | (없음) | 체크할 VM hostname 목록 파일. 미지정 시 인벤토리 전체 |

### 기대값 (공통/ev01)

| 플래그 | 필수 | 설명 |
|---|---|---|
| `-ht <on\|off>` | ✅ | 하이퍼스레딩 상태 (ev01 affinity 자동계산에 사용) |
| `-cores <N>` | ✅ | 소켓당 코어 수 |
| `-numa <N>` | ✅ | NUMA 노드당 최대 vCPU 수 |
| `-cpu <N>` | ✅ | vCPU 수 |
| `-mem <N>` | ✅ | 메모리 GB |
| `-disk <N>` | ✅ | 디스크 총량 GB (**단일값만** — 11번과 달리 쉼표 불가) |
| `-shares-ev01 <N>` | ✅ | ev01 Shares **ratio 숫자만** (`normal` 불가) |

### 그룹별 (전부 옵션, 안 주면 스킵)

`-cores-ev02` / `-cores-ev03` / `-numa-ev02` / `-numa-ev03` / `-cpu-ev02` / `-cpu-ev03` / `-mem-ev02` / `-mem-ev03` / `-disk-ev02` / `-disk-ev03` / `-shares-ev02` / `-shares-ev03`

### affinity

| 플래그 | 설명 |
|---|---|
| `-affinity-ev01 <path>` | 안 주면 `-ht`/`-cores` 기반 자동계산 |
| `-affinity-ev02 <path>` / `-affinity-ev03 <path>` | 안 주면 해당 그룹 affinity 체크 스킵 |

### 출력

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-out <path>` | 타임스탬프 자동생성 | 상세 CSV 경로 (`_summary` 요약 CSV도 함께 생성) |
| `-onlyFail` | `false` | PASS인 VM을 **콘솔/CSV 모두에서** 제외 |
| `-noColor` | `false` | ANSI 컬러 끔 |
| `-demo` | `false` | vCenter 접속 없이 가짜 VM 3대로 데모 |

---

## 4. 하위 폴더: fail-based-param-fix

`vm-param-setting-check/fail-based-param-fix/`에는 **레거시 외부 도구 오케스트레이션 방식**의 교정 도구가 있습니다. `VM_setup/vm-param-fix/`와 같은 계열입니다.

> 역시 [11번 도구의 `-fix`](./11_vm-param-check-usability-improvement.md)로 대체되었습니다. 새로 쓰지 마세요.

---

## 5. 파일 구조

```
vm-param-setting-check/
├── README.md               # 1차 자료
├── PLAN.md                 # 설계 배경
├── main.go                 # CLI 진입점
├── demo.go
├── setup.sh
├── checker/                # 점검 로직 (11번의 checker/와 유사)
├── config/                 # 대상 목록 로딩
├── model/                  # 데이터 모델
├── report/                 # 콘솔/CSV 출력
├── vcenter/                # vCenter 접속
├── fail-based-param-fix/   # 레거시 교정 오케스트레이터
└── vendor/
```

---

## 6. 관련 문서

- 1차 자료: `vm-param-setting-check/README.md`
- **현행 대체 도구: [11. vm-param-check](./11_vm-param-check-usability-improvement.md)**
