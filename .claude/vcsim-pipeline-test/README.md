# vcsim-pipeline-test

govmomi의 내장 vcsim(VMware vCenter Simulator)을 이용한 **전체 파이프라인 통합 테스트 프레임워크**.

## 목적

실제 vCenter/ESXi 연결 없이 아래 파이프라인을 자동화 테스트한다:

```
connect → create → affinity/lpage → param-check → FAIL 수정 → 재검증
```

## 환경

| 항목 | 내용 |
| :--- | :--- |
| OS | Rocky Linux 8.10 |
| Go | 1.26.5 |
| govmomi | v0.55.1 (vendor/ 포함) |
| vcsim | govmomi/simulator 내장 (별도 설치 불필요) |

## 테스트 구조

```
vcsim-pipeline-test/
├── pipeline_test.go   # 전체 파이프라인 통합 테스트 (7개 Phase)
├── helpers_test.go    # VM 오브젝트 헬퍼 (newVMObject)
├── go.mod
├── go.sum
└── vendor/            # 폐쇄망 빌드용 (go mod vendor 완료)
```

### 테스트 Phase

| Phase | 내용 | 검증 항목 |
| :--- | :--- | :--- |
| Phase 1 | vcsim 서버 시작 + DC 연결 | 서버 정상 기동, Datacenter 조회 |
| Phase 2 | ESXi 호스트 10대 확인 | vm_connect 로직 (호스트 등록) |
| Phase 3 | VM 30개 생성 (BM 10대 × 3) | vm_create 로직 |
| Phase 4 | ExtraConfig/Shares 설정 | affinity/lpage 로직 |
| Phase 5 | 설정 체크 | vm-param-check 로직 (PASS=30) |
| Phase 6 | 의도적 FAIL → 수정 → 재검증 | FAIL 수정 루프 |
| Phase 7 | 전체 파이프라인 통합 | BM 10대, VM 30대 전체 흐름 |

## 빌드 및 실행

### Rocky Linux에서 실행 (폐쇄망 포함)

```bash
cd /root/vcsim-pipeline-test   # 또는 .claude/vcsim-pipeline-test

# 전체 테스트 실행 (vendor/ 사용)
go test -mod=vendor -v -timeout 180s ./...

# 특정 Phase만 실행
go test -mod=vendor -v -run TestPhase3_VMCreate ./...
go test -mod=vendor -v -run TestPhase6_FailFixLoop ./...
go test -mod=vendor -v -run TestPhase7_FullPipelineScale10BM ./...
```

### 인터넷 환경에서 실행

```bash
go test -mod=mod -v -timeout 180s ./...
```

## 스케일 확장

현재 소규모(BM 10대, VM 30대) 기준. 확장 시 `pipeline_test.go` 상단 상수 변경:

```go
const (
    bmCount = 10   // ← 50, 200, 800으로 단계적 확장
    vmPerBM = 3
)
```

> ⚠️ Rocky Linux 서버 RAM이 1.5GiB 여유이므로 BM 50대 이상은 메모리 모니터링 필요:
> ```bash
> watch -n 2 'free -h'
> ```

## 참고 파일

- `.claude/VM설정 go lang/` — vm_connect, vm_create, vm_affinity_bulk, vm_lpage_bulk
- `.claude/VM 매개변수설정체크/` — vm-param-check, FAIL 수정 도구
