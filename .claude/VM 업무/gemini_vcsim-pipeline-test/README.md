# gemini_vcsim-pipeline-test 기술문서

govmomi 내장 `vcsim`(VMware vCenter Simulator)을 이용한 **VM 생성/설정/점검/수정 전체 파이프라인 통합 테스트 프레임워크**.

> ⚠️ **주의사항 (Disclaimer)**
> 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

이 도구는 통합 테스트 프레임워크로 별도의 실행 바이너리 빌드 과정이 없으며 `go test` 명령어로 직접 실행합니다.
모든 명령어는 Rocky Linux 서버의 프로젝트 디렉토리(`/root/myrepo/.claude/VM 업무/gemini_vcsim-pipeline-test`)에서 수행합니다.

### 실행 명령어

```bash
cd "/root/myrepo/.claude/VM 업무/gemini_vcsim-pipeline-test"

# 폐쇄망 / 오프라인 환경 (vendor 사용)
go test -mod=vendor -v -timeout 180s ./...

# 인터넷 연결 환경
go test -mod=mod -v -timeout 180s ./...
```

### 5. 전역 명령어로 사용하기 (선택 사항)
본 도구는 `go test`를 통해 동작하므로 별도로 등록할 실행 파일은 없습니다. 셸 스크립트로 래핑하여 전역 명령어로 활용할 수 있습니다.
```bash
# 예시: test-vcsim 스크립트 작성 및 등록
echo '#!/bin/bash' > test-vcsim
echo 'cd "/root/myrepo/.claude/VM 업무/gemini_vcsim-pipeline-test" && go test -mod=vendor -v -timeout 180s ./...' >> test-vcsim
chmod +x test-vcsim
sudo cp test-vcsim /usr/local/bin/
```
이후부터는 터미널 어느 경로에서나 `test-vcsim` 명령어만 입력하면 테스트가 즉시 실행됩니다.

## 2. 사용 방법

특정 단계의 동작만 디버깅하거나 검증할 경우 `-run <테스트명>` 옵션을 사용하여 개별 실행할 수 있습니다.

### 사용 예시

```bash
# [Phase 1] vcsim 서버 기동 및 세션 연결 테스트
go test -mod=vendor -v -run TestPhase1_VcsimStartup ./...

# [Phase 2] ESXi 호스트 탐색 및 등록 테스트 (vm_connect)
go test -mod=vendor -v -run TestPhase2_HostConnectCheck ./...

# [Phase 3] VM 일괄 생성 테스트 (vm_create)
go test -mod=vendor -v -run TestPhase3_VMCreate ./...

# [Phase 4] 파라미터/Affinity 설정 적용 테스트 (affinity/lpage)
go test -mod=vendor -v -run TestPhase4_ExtraConfigApply ./...

# [Phase 5] VM 파라미터 점검 테스트 (vm-param-check)
go test -mod=vendor -v -run TestPhase5_ParamCheck ./...

# [Phase 6] FAIL 항목 자동 수정 루프 테스트 (FAIL 수정)
go test -mod=vendor -v -run TestPhase6_FailFixLoop ./...

# [Phase 7] 전체 파이프라인 E2E 통합 테스트
go test -mod=vendor -v -run TestPhase7_FullPipelineScale10BM ./...
```

## 3. 옵션별 상세 설명

별도의 실행 바이너리를 사용하지 않는 테스트 프레임워크이므로, 플래그(옵션) 대신 `go test`의 기본 `-run` 옵션을 사용해 테스트 대상을 지정합니다.

## 4. 문서별 고유 설명

### 파이프라인 목적

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

### 환경 정보

| 항목 | 내용 |
| :--- | :--- |
| **OS** | Rocky Linux 8.10 |
| **Go 버전** | Go 1.26.5 이상 |
| **의존성** | `github.com/vmware/govmomi v0.55.1` (오프라인용 `vendor/` 내장) |
| **시뮬레이터** | `govmomi/simulator` (외부 프로세스 설치 없이 테스트 코드 내에서 자동 라이프사이클 관리) |

### 디렉토리 구조

```
gemini_vcsim-pipeline-test/
├── pipeline_test.go   # Phase 1 ~ Phase 7 통합 테스트 코드
├── helpers_test.go    # VM Reconfigure 및 관리 객체 래퍼 헬퍼
├── go.mod             # 모듈 정의
├── go.sum             # 체크섬
├── vendor/            # 폐쇄망 오프라인 빌드/테스트용 의존성 패키지
└── README.md          # 본 가이드 문서
```

### 대규모 스케일 확장 테스트 (Scale-Up)

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
