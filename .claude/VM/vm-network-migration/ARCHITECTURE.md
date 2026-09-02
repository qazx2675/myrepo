# ARCHITECTURE.md

`vm-network-migration` 의 구조 요약입니다. 전체 코드를 다 읽지 않아도 어디를 고쳐야 하는지 알 수 있도록 정리했습니다.

## 파일별 역할

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | CLI 진입점. 플래그 파싱 → 목록/자격증명 로드 → **vCenter 병렬 조사** → VM 위치 해석 → 포트그룹 사전 검증 → 워커 풀 병렬 이관 → 리포트/롤백 저장 → 종료 코드 결정 |
| `config.go` | 목록 파일 파싱(`LoadLines` — `vcenter.txt` / `vmlist.txt`, 중복 제거), `VC_USER`/`VC_PASS` 환경 변수 자격증명 로드 |
| `vsphere.go` | vCenter 접속(insecure 고정), `surveyVCenter`/`surveyDatacenter` 병렬 조사, VM 이름 색인, DVPG key→name 색인, NIC 목록 조회, 포트그룹 백킹 생성 |
| `migrate.go` | **핵심 로직.** VM 1대의 NIC 선택 → 백킹 교체 Reconfigure → 재조회 검증. `Result` 구조체 정의 |
| `report.go` | 콘솔 요약 출력, `report_*.csv` 쓰기, `rollback_*.csv` 읽기/쓰기 |
| `setup.sh` | 폐쇄망 오프라인 빌드 (`-mod=vendor`, `GOPROXY=off`) |
| `run.sh` | 실행 편의 래퍼. 자격증명/목록 파일 확인 → 소스 변경 시 재빌드 → 실행 → 사후 확인 안내 출력 |
| `vendor/` | govmomi 등 서드파티 의존성. 직접 수정하지 않습니다 |

## 데이터 흐름

```
VC_USER / VC_PASS (환경 변수) ─┐
vcenter.txt (주소 N줄) ────────┼→ main.run()
vmlist.txt  (VM 이름) ─────────┘        │
                                        ↓
        [동시] surveyVCenter × N        ← vCenter 끼리 동시
                    ↓
        [동시] surveyDatacenter × M     ← 데이터센터 끼리 동시
                    ↓
          buildVMIndex ‖ buildNetworkIndex   ← 둘도 동시
                    ↓
        locations: VM 이름 → [{vc, dc, ref}]
                    ↓
        [동시] resolveBacking × (dc, pg) 조합
                    ↓
        [동시 -concurrency] migrateVM × VM
                    ↓
              []Result → printSummary
                         writeReportCSV
                         writeRollbackCSV
```

## 핵심 타입

| 타입 | 위치 | 의미 |
|---|---|---|
| `vcConn` | `vsphere.go` | vCenter 한 대의 접속 + 그 안의 데이터센터 색인 목록 + 조사 에러 |
| `dcIndex` | `vsphere.go` | 데이터센터 하나의 VM 이름 색인, 동명 VM 목록, 네트워크 색인 |
| `vmLocation` | `main.go` | VM 하나가 어느 vCenter/데이터센터에 있는지 (`vcIdx`, `dcIdx`, `ref`) |
| `task` | `main.go` | 처리할 VM 한 건 (롤백 모드면 대상 vCenter 가 고정됨) |
| `Result` | `migrate.go` | VM 한 대의 처리 결과. CSV 두 종류의 원본 |

## 설계상 지켜야 할 원칙

이 네 가지는 안전성의 근거라서, 고칠 때 깨뜨리지 않도록 주의하세요.

1. **VM 을 건드리기 전에 실패할 수 있는 것은 모두 먼저 확인한다.**
   vCenter 조사와 포트그룹 백킹 확인(`resolveBacking`)은 이관 고루틴을 띄우기 **전에** 끝냅니다.
   여기서 실패하면 VM 은 한 대도 변경되지 않은 상태로 종료됩니다.
2. **vCenter 한 대라도 조사에 실패하면 전체를 중단한다.**
   그대로 진행하면 그 vCenter 의 VM 이 "찾을 수 없음"으로 잘못 보고되어,
   이관되지 않은 VM 을 처리된 것으로 착각하게 됩니다.
3. **NIC 를 끊지 않는다.**
   Reconfigure 한 번으로 `Backing` 만 교체합니다. 중간에 실패해도 NIC 가 끊긴 채로 남지 않습니다.
   ("끊기 → 생성 → 붙이기" 방식으로 되돌리지 마세요.)
4. **`find.Finder` 를 고루틴에서 공유하지 않는다.**
   `Finder` 는 내부 상태를 가져 동시 사용에 안전하지 않습니다.
   VM 조회는 Finder 가 아니라 `ContainerView` 로 만든 이름 색인을 쓰고,
   `resolveBacking` 은 호출할 때마다 Finder 를 새로 만듭니다.

## 병렬성 정리

| 단계 | 동시성 | 제한 |
|---|---|---|
| vCenter 접속·조사 | 전부 동시 | 없음 (목록 개수만큼) |
| 데이터센터 조사 | 전부 동시 | 없음 |
| VM 색인 / 네트워크 색인 | 서로 동시 | 없음 |
| 포트그룹 백킹 확인 | 전부 동시 | 없음 |
| VM 이관 | 동시 | `-concurrency` (기본 8) |

조회는 읽기라서 제한하지 않고, **쓰기(Reconfigure)만 제한**합니다.

## 자주 있는 수정 요청 → 볼 파일

| 요청 | 보면 되는 곳 |
|---|---|
| 새 CLI 옵션 추가 | `main.go` 의 `run()` 플래그 블록, 필요하면 `migrate.go` 의 `migrateOptions` |
| vCenter 별로 계정을 다르게 | `config.go` 의 `loadCredentials`, `main.go` 의 `surveyVCenter` 호출부 |
| 일부 vCenter 실패해도 계속 진행 | `main.go` 의 `failedVC` 분기 (원칙 2를 깨는 변경이라 신중히) |
| NIC 선택 규칙 변경 (예: MAC 으로 지정) | `migrate.go` 의 NIC 선택 분기 |
| 검증 조건 강화 (예: ping 확인 추가) | `migrate.go` 의 "검증" 구간 |
| 리포트 항목/형식 변경 | `report.go` |
| 새로운 백킹 종류 지원 (NSX 등) | `vsphere.go` 의 `portgroupName` |
| 전원 상태별 동작 변경 | `migrate.go` 의 `power` 분기 |

## 검증 이력

- 빌드/정적분석: Rocky Linux 8 (go1.26.5), `go build -mod=vendor ./...`, `go vet -mod=vendor ./...`, `gofmt` 통과
- 실환경 검증: 실 vCenter 대상으로
  환경 변수 누락(exit 2) → dry-run → 실제 이관 → 재실행(멱등성 SKIPPED) → 롤백 → 원상복구,
  그리고 vCenter 목록에 접속 불가 주소를 섞었을 때 VM 무손상 중단까지 확인
