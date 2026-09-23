# 20. gemini_vcsim-pipeline-test — 전 과정 통합 테스트 프레임워크

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🟡 **테스트용.** vcsim 시뮬레이터 위에서만 동작하며 실제 인프라를 건드리지 않습니다 |
| 폴더 | `.claude/VM/gemini_vcsim-pipeline-test/` |
| 바이너리 | **없음.** `go test`로 실행하는 테스트 코드입니다 |
| 하는 일 | VM 생성 → 설정 → 점검 → 자동수정 전 과정(Phase 1~7)을 vcsim 위에서 검증 |
| 실행 | `go test -mod=vendor -v -timeout 180s ./...` |

---

## 1. 이 폴더는 무엇인가

**실제 vCenter/ESXi 인프라 없이** 자동화 파이프라인 전체를 검증하는 통합 테스트 프레임워크입니다. govmomi에 내장된 vcsim을 테스트 코드 안에서 자동으로 띄우고 내립니다(외부 프로세스 설치 불필요).

```
[Phase 1] vcsim 구동 및 vCenter API 연결
   ↓
[Phase 2] ESXi 호스트 탐색 및 등록 (vm_connect 로직)
   ↓
[Phase 3] BM별 다중 VM 생성 (vm_create 로직)
   ↓
[Phase 4] ExtraConfig / Shares / 메모리 예약 일괄 설정 (affinity/lpage 로직)
   ↓
[Phase 5] VM 설정 파라미터 점검 (vm-param-check 로직)
   ↓
[Phase 6] 의도적 FAIL 발생 → 자동 수정 → 재검증 루프
   ↓
[Phase 7] 전체 파이프라인 엔드투엔드(E2E) 통합 검증
```

**언제 쓰나**: 도구 코드를 고친 뒤 회귀가 없는지 확인할 때. 실 vCenter를 쓸 수 없는 환경에서 로직을 검증할 때.

---

## 2. 실행 방법

별도의 빌드 과정이 없습니다.

```bash
cd .claude/VM/gemini_vcsim-pipeline-test

# 폐쇄망 / 오프라인 환경 (vendor 사용)
go test -mod=vendor -v -timeout 180s ./...

# 인터넷 연결 환경
go test -mod=mod -v -timeout 180s ./...
```

### Phase별 개별 실행

특정 단계만 디버깅할 때는 `-run` 옵션을 씁니다.

```bash
go test -mod=vendor -v -run TestPhase1_VcsimStartup ./...          # vcsim 기동 + 세션 연결
go test -mod=vendor -v -run TestPhase2_HostConnectCheck ./...      # 호스트 탐색/등록
go test -mod=vendor -v -run TestPhase3_VMCreate ./...              # VM 일괄 생성
go test -mod=vendor -v -run TestPhase4_ExtraConfigApply ./...      # 파라미터/Affinity 설정
go test -mod=vendor -v -run TestPhase5_ParamCheck ./...            # 파라미터 점검
go test -mod=vendor -v -run TestPhase6_FailFixLoop ./...           # FAIL 자동수정 루프
go test -mod=vendor -v -run TestPhase7_FullPipelineScale10BM ./... # E2E 통합
```

### 셸 스크립트로 감싸기 (선택)

```bash
echo '#!/bin/bash' > test-vcsim
echo 'cd "<프로젝트 경로>" && go test -mod=vendor -v -timeout 180s ./...' >> test-vcsim
chmod +x test-vcsim && sudo cp test-vcsim /usr/local/bin/
```

---

## 3. 옵션

**별도 실행 바이너리가 없으므로 플래그(옵션)가 없습니다.** `go test`의 표준 옵션을 씁니다.

| 옵션 | 설명 |
|---|---|
| `-run <테스트명>` | 실행할 테스트 지정 (위 Phase 목록 참고) |
| `-v` | 상세 출력 |
| `-timeout 180s` | 제한 시간. 스케일 테스트 시 늘려야 할 수 있음 |
| `-mod=vendor` | 폐쇄망: `vendor/` 사용 |

---

## 4. 대규모 스케일 테스트

BM 호스트 수와 VM 대수를 늘려 부하/메모리 테스트를 하려면 `pipeline_test.go` 상단 상수를 조정합니다.

```go
const (
    bmCount = 50   // BM 호스트 수 (50 -> 200 -> 800 단계별 확장)
    vmPerBM = 3    // BM당 VM 수 (총 VM = bmCount * vmPerBM)
)
```

> ⚠️ **서버 리소스 모니터링 필수**
> Rocky Linux 서버 가용 메모리가 약 1.5GiB 수준이므로, 대규모 스케일 테스트 시 다른 터미널에서 반드시 모니터링하세요.
>
> ```bash
> watch -n 2 'free -h'
> ```

---

## 5. 환경 정보

| 항목 | 내용 |
|---|---|
| OS | Rocky Linux 8.10 |
| Go | 1.26.5 이상 |
| 의존성 | `github.com/vmware/govmomi v0.55.1` (오프라인용 `vendor/` 내장) |
| 시뮬레이터 | `govmomi/simulator` — 외부 프로세스 설치 없이 **테스트 코드 내에서 자동 라이프사이클 관리** |

---

## 6. 파일 구조

```
gemini_vcsim-pipeline-test/
├── README.md          # 1차 자료
├── pipeline_test.go   # ★ Phase 1~7 통합 테스트 코드 (상단 상수로 스케일 조정)
├── helpers_test.go    # VM Reconfigure 및 관리 객체 래퍼 헬퍼
├── go.mod / go.sum
└── vendor/
```

---

## 7. 관련 문서

- 1차 자료: `gemini_vcsim-pipeline-test/README.md`
- 검증 대상 도구들: [11. vm-param-check](./11_vm-param-check-usability-improvement.md), [17. vm-setting-go-lang](./17_vm-setting-go-lang.md)
- 실 vCenter 구조를 복제한 테스트 환경: [18. vc-test-env](./18_vcenter-test-env-vcsim.md)
