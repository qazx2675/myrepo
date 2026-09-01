# ARCHITECTURE.md

`vm-network-migration` 의 구조 요약입니다. 전체 코드를 다 읽지 않아도 어디를 고쳐야 하는지 알 수 있도록 정리했습니다.

## 파일별 역할

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | CLI 진입점. 플래그 파싱 → 설정/목록 로드 → vCenter 접속 → **사전 검증** → 워커 풀로 병렬 처리 → 리포트/롤백 저장 → 종료 코드 결정 |
| `config.go` | `vcenter.txt`(KEY=VALUE)와 `vmlist.txt`(줄 단위) 파싱. 필수 항목 검증, VM 이름 중복 제거 |
| `vsphere.go` | vCenter 접속, 데이터센터 하위 VM 이름 색인 생성, DVPG key→name 색인, NIC 목록 조회, 포트그룹 백킹 생성 |
| `migrate.go` | **핵심 로직.** VM 1대의 NIC 선택 → 백킹 교체 Reconfigure → 재조회 검증. `Result` 구조체 정의 |
| `report.go` | 콘솔 요약 출력, `report_*.csv` 쓰기, `rollback_*.csv` 읽기/쓰기 |
| `setup.sh` | 폐쇄망 오프라인 빌드 (`-mod=vendor`, `GOPROXY=off`) |
| `run.sh` | 실행 편의 래퍼. 설정 파일 확인 → 소스 변경 시 재빌드 → 실행 → 사후 확인 안내 출력 |
| `vendor/` | govmomi 등 서드파티 의존성. 직접 수정하지 않습니다 |

## 데이터 흐름

```
vcenter.txt ─┐
             ├→ main.run() ─→ connectVCenter ─→ buildVMIndex   (name → MoRef)
vmlist.txt  ─┘                              └─→ buildNetworkIndex (dvpgKey → name)
                                            └─→ resolveBacking  (목표 PG → backing)
                                                       │
                              워커 풀(-concurrency) ────┴→ migrateVM(VM 1대)
                                                              │
                                              []Result ───────┴→ printSummary
                                                                writeReportCSV
                                                                writeRollbackCSV
```

## 설계상 지켜야 할 원칙

이 세 가지는 안전성의 근거라서, 고칠 때 깨뜨리지 않도록 주의하세요.

1. **VM 을 건드리기 전에 실패할 수 있는 것은 모두 먼저 확인한다.**
   목표 포트그룹 존재 확인과 백킹 생성(`resolveBacking`)은 고루틴을 띄우기 **전에** 끝냅니다.
   여기서 실패하면 VM 은 한 대도 변경되지 않은 상태로 종료됩니다.
2. **NIC 를 끊지 않는다.**
   Reconfigure 한 번으로 `Backing` 만 교체합니다. 중간에 실패해도 NIC 가 끊긴 채로 남지 않습니다.
   ("끊기 → 생성 → 붙이기" 방식으로 되돌리지 마세요.)
3. **`Finder` 를 고루틴에서 공유하지 않는다.**
   `find.Finder` 는 내부 상태를 가져 동시 사용에 안전하지 않습니다.
   그래서 VM 조회는 Finder 가 아니라 `ContainerView` 로 만든 이름 색인(`buildVMIndex`)을 씁니다.

## 자주 있는 수정 요청 → 볼 파일

| 요청 | 보면 되는 곳 |
|---|---|
| 새 CLI 옵션 추가 | `main.go` 의 `run()` 플래그 블록, 필요하면 `migrate.go` 의 `migrateOptions` |
| NIC 선택 규칙 변경 (예: MAC 으로 지정) | `migrate.go` 의 NIC 선택 분기 |
| 검증 조건 강화 (예: ping 확인 추가) | `migrate.go` 의 "검증" 구간 |
| 리포트 항목/형식 변경 | `report.go` |
| 설정 파일 항목 추가 | `config.go` 의 `loadVCenterConfig` |
| 새로운 백킹 종류 지원 (NSX 등) | `vsphere.go` 의 `portgroupName` |
| 전원 상태별 동작 변경 | `migrate.go` 의 `power` 분기 |

## 검증 이력

- 빌드/정적분석: Rocky Linux 8 (go1.26.5), `go build -mod=vendor ./...`, `go vet -mod=vendor ./...` 통과
- 실환경 검증: vCenter 실서버 대상 dry-run → 실제 이관 → 재실행(멱등성 SKIPPED) → 롤백 → 원상복구 전 과정 확인
