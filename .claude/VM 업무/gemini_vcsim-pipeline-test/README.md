# gemini_vcsim-pipeline-test

govmomi 내장 `vcsim`(VMware vCenter Simulator)을 이용한 **VM 생성/설정/점검/수정 전체 파이프라인 통합 테스트 프레임워크**.

---

## 1. 목적

실제 vCenter/ESXi 인프라 없이 독립된 가상 시뮬레이터 환경에서 아래 자동화 파이프라인의 전 과정을 검증합니다:

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
[Phase 6] 의도적 FAIL 발생 → 자동 수정 → 재검증 루프 (FAIL 수정 로직)
   ↓
[Phase 7] 전체 파이프라인 엔드투엔드(E2E) 통합 검증
```

---

## 2. 환경 정보

| 항목 | 내용 |
| :--- | :--- |
| **OS** | Rocky Linux 8.10 |
| **Go 버전** | Go 1.26.5 이상 |
| **의존성** | `github.com/vmware/govmomi v0.55.1` (오프라인용 `vendor/` 내장) |
| **시뮬레이터** | `govmomi/simulator` (외부 프로세스 설치 없이 테스트 코드 내에서 자동 라이프사이클 관리) |

---

## 3. 디렉토리 구조

```
gemini_vcsim-pipeline-test/
├── pipeline_test.go   # Phase 1 ~ Phase 7 통합 테스트 코드
├── helpers_test.go    # VM Reconfigure 및 관리 객체 래퍼 헬퍼
├── go.mod             # 모듈 정의
├── go.sum             # 체크섬
├── vendor/            # 폐쇄망 오프라인 빌드/테스트용 의존성 패키지
└── README.md          # 본 가이드 문서
```

---

## 4. 실행 방법

모든 명령어는 Rocky Linux 서버의 프로젝트 디렉토리(`/root/myrepo/.claude/VM 업무/gemini_vcsim-pipeline-test`)에서 수행합니다.

### 4.1 전체 파이프라인 일괄 실행

```bash
cd /root/myrepo/.claude/VM 업무/gemini_vcsim-pipeline-test

# 폐쇄망 / 오프라인 환경 (vendor 사용)
go test -mod=vendor -v -timeout 180s ./...

# 인터넷 연결 환경
go test -mod=mod -v -timeout 180s ./...
```

---

## 5. Phase별 개별 테스트 방법 및 가이드

특정 단계의 동작만 디버깅하거나 검증할 경우 `-run <테스트명>` 옵션을 사용하여 개별 실행할 수 있습니다.

### [Phase 1] vcsim 서버 기동 및 세션 연결 테스트
- **대상 함수**: `TestPhase1_VcsimStartup`
- **검증 내용**: 인메모리 vcsim VPX 인스턴스를 띄우고 SDK 엔드포인트 세션 로그인 및 루트 Datacenter(`DC0`) 조회가 정상 작동하는지 확인합니다.
```bash
go test -mod=vendor -v -run TestPhase1_VcsimStartup ./...
```
- **성공 로그 예시**:
  ```text
  === RUN   TestPhase1_VcsimStartup
      pipeline_test.go:181: vcsim 서버 시작: http://user:pass@127.0.0.1:33837/sdk
      pipeline_test.go:189: [Phase 1] vcsim 연결 성공. DC: DC0
  --- PASS: TestPhase1_VcsimStartup (0.49s)
  ```

---

### [Phase 2] ESXi 호스트 탐색 및 등록 테스트 (vm_connect)
- **대상 함수**: `TestPhase2_HostConnectCheck`
- **검증 내용**: vCenter 내 클러스터에 정의된 BM 호스트 10대가 인벤토리에 정상 인식되는지 확인합니다.
```bash
go test -mod=vendor -v -run TestPhase2_HostConnectCheck ./...
```
- **성공 로그 예시**:
  ```text
  === RUN   TestPhase2_HostConnectCheck
      pipeline_test.go:204: [Phase 2] 호스트 10개 확인 (기대: 10)
      pipeline_test.go:209:   [01] DC0_C0_H0
      ...
      pipeline_test.go:209:   [10] DC0_C0_H9
  --- PASS: TestPhase2_HostConnectCheck (0.46s)
  ```

---

### [Phase 3] VM 일괄 생성 테스트 (vm_create)
- **대상 함수**: `TestPhase3_VMCreate`
- **검증 내용**: 10대의 BM 호스트에 각각 3개씩(ev01, ev02, ev03) 총 30대의 VM이 `CreateVM` 태스크를 통해 데이터스토어에 정상 프로비저닝되는지 확인합니다.
```bash
go test -mod=vendor -v -run TestPhase3_VMCreate ./...
```
- **성공 로그 예시**:
  ```text
  === RUN   TestPhase3_VMCreate
      pipeline_test.go:221:   createTestVMs: 30 VM 생성 완료
      pipeline_test.go:229: [Phase 3] VM 생성: 30/30
  --- PASS: TestPhase3_VMCreate (0.60s)
  ```

---

### [Phase 4] 파라미터/Affinity 설정 적용 테스트 (affinity/lpage)
- **대상 함수**: `TestPhase4_ExtraConfigApply`
- **검증 내용**: 생성된 30대의 VM에 `sched.mem.pin`, `sched.swap.vmxSwapEnabled`, ev01 전용 CPU Affinity(`sched.cpu.affinity`), 메모리 최대 고정(`MemoryReservationLockedToMax`), CPU/메모리 Shares 일괄 설정 태스크(`Reconfigure`)를 수행합니다.
```bash
go test -mod=vendor -v -run TestPhase4_ExtraConfigApply ./...
```
- **성공 로그 예시**:
  ```text
  === RUN   TestPhase4_ExtraConfigApply
      pipeline_test.go:243:   createTestVMs: 30 VM 생성 완료
      pipeline_test.go:284: [Phase 4] ExtraConfig 적용: 30 VM, 실패 0
  --- PASS: TestPhase4_ExtraConfigApply (0.63s)
  ```

---

### [Phase 5] VM 파라미터 점검 테스트 (vm-param-check)
- **대상 함수**: `TestPhase5_ParamCheck`
- **검증 내용**: VM 인벤토리 속성을 재조회하여 필수 메모리 고정 예약 및 자원 할당 정책이 30대 모두 `PASS`인지 검증합니다.
```bash
go test -mod=vendor -v -run TestPhase5_ParamCheck ./...
```
- **성공 로그 예시**:
  ```text
  === RUN   TestPhase5_ParamCheck
      pipeline_test.go:299:   createTestVMs: 30 VM 생성 완료
      pipeline_test.go:338: [Phase 5] 체크 완료: PASS=30, FAIL=0
  --- PASS: TestPhase5_ParamCheck (0.69s)
  ```

---

### [Phase 6] FAIL 항목 자동 수정 루프 테스트 (FAIL 수정)
- **대상 함수**: `TestPhase6_FailFixLoop`
- **검증 내용**: 의도적으로 규격에 맞지 않는 사양(낮은 CPU/메모리, 옵션 누락 등)을 가진 VM 9대를 생성(FAIL 상태)한 후, 수정 모듈을 통해 표준 스펙으로 `Reconfigure`하고 정상화(PASS)되는 전체 루프를 검증합니다.
```bash
go test -mod=vendor -v -run TestPhase6_FailFixLoop ./...
```
- **성공 로그 예시**:
  ```text
  === RUN   TestPhase6_FailFixLoop
      pipeline_test.go:390: [Phase 6-A] 의도적 FAIL VM 9개 생성 완료 (기대: 9)
      pipeline_test.go:419: [Phase 6-B] 수정 완료: 9/9
  --- PASS: TestPhase6_FailFixLoop (0.54s)
  ```

---

### [Phase 7] 전체 파이프라인 E2E 통합 테스트
- **대상 함수**: `TestPhase7_FullPipelineScale10BM`
- **검증 내용**: VM 생성부터 설정 주입, 최종 인벤토리 정합성 검증까지 단일 테스트 케이스에서 논스톱으로 검증합니다.
```bash
go test -mod=vendor -v -run TestPhase7_FullPipelineScale10BM ./...
```
- **성공 로그 예시**:
  ```text
  === RUN   TestPhase7_FullPipelineScale10BM
      pipeline_test.go:437:   createTestVMs: 30 VM 생성 완료
      pipeline_test.go:478: [Phase 7] 최종 VM: 30 (기대: 30), 설정 실패: 0
      pipeline_test.go:485: [Phase 7] 전체 파이프라인 통합 테스트 완료 ✓
  --- PASS: TestPhase7_FullPipelineScale10BM (0.75s)
  ```

---

## 6. 대규모 스케일 확장 테스트 (Scale-Up)

BM 호스트 수 및 VM 대수를 늘려 부하/메모리 테스트를 진행하려면 `pipeline_test.go` 상단의 상수를 조정한 후 실행합니다.

```go
const (
    bmCount = 50   // BM 호스트 수 (50 -> 200 -> 800 단계별 확장)
    vmPerBM = 3    // BM당 VM 수 (총 VM = bmCount * vmPerBM)
)
```

> **주의 (서버 리소스 모니터링)**:
> Rocky Linux 서버 가용 메모리가 약 1.5GiB 수준이므로, 대규모 스케일 테스트 시 다른 터미널에서 메모리 사용량을 반드시 모니터링하세요:
> ```bash
> watch -n 2 'free -h'
> ```
