# CHANGELOG

날짜 내림차순으로 기록합니다.

## 2026-09-02 — v0.2.0

### 변경 (호환성 깨짐)

- **`vcenter.txt` 형식이 바뀌었습니다.** `KEY=VALUE` 설정 파일에서 **vCenter 주소만 한 줄에 하나씩**
  적는 목록 파일로 변경. 기존 파일은 그대로 쓸 수 없으니 `vcenter.txt.example` 을 참고해 다시 만드세요.
- **계정을 환경 변수로 받습니다.** `VC_USER`/`VC_PASS`(대체: `VCENTER_USER`/`VCENTER_PASS`).
  설정 파일에 평문 비밀번호를 두지 않기 위한 것으로, `vm-param-check` 와 같은 규약입니다.
- **`VCENTER_INSECURE` 옵션을 없앴습니다.** 자체서명 인증서 환경이 전제라 insecure 고정으로 단순화했습니다.
- **`VCENTER_DATACENTER` 옵션을 없앴습니다.** 각 vCenter 의 모든 데이터센터를 자동으로 조회합니다.
- 롤백 CSV 에 `vcenter` 컬럼이 추가되어, **v0.1.0 에서 만든 롤백 파일은 v0.2.0 으로 읽을 수 없습니다.**
  이관 직전 버전으로 롤백하거나, 기존 CSV 맨 앞에 vCenter 주소 컬럼을 채워 넣으세요.

### 추가

- **vCenter 여러 대 동시 지원.** `vcenter.txt` 에 여러 줄을 적으면 모두 동시에 접속·조회합니다.
  VM 이 어느 vCenter/데이터센터에 있는지 자동으로 찾아 목록을 나눌 필요가 없습니다.
- 조회 전 과정 병렬화: vCenter 끼리, 데이터센터 끼리, VM 색인과 네트워크 색인까지 모두 동시 실행.
- vCenter 한 대라도 조사에 실패하면 VM 을 건드리지 않고 중단 (잘못된 "찾을 수 없음" 판정 방지).
- VM 이름이 vCenter/데이터센터를 넘어 중복되면 해당 VM 만 `FAILED` 로 처리하고 위치를 알려줍니다.
- 리포트 CSV 에 `vcenter` / `datacenter` 컬럼 추가. vCenter 가 여러 대면 콘솔 요약에도 주소 표시.

### 검증

실 vCenter 대상으로 환경 변수 누락(exit 2) → dry-run → 실제 이관 → 재실행(SKIPPED) → 롤백 → 원상복구,
그리고 vCenter 목록에 접속 불가 주소를 섞었을 때 VM 무손상 중단까지 확인.


## 2026-09-01 — v0.1.0 (초기 릴리스)

### 추가

- vCenter 상의 여러 VM 가상 NIC 를 지정 포트그룹으로 일괄 이관하는 Go CLI (`main.go`, `config.go`, `vsphere.go`, `migrate.go`, `report.go`)
- 폐쇄망 오프라인 빌드 스크립트 `setup.sh` (`-mod=vendor`, `GOPROXY=off`) 및 `vendor/` 동봉 (govmomi v0.55.1)
- 실행 편의 bash 래퍼 `run.sh` — 설정 확인 → 소스 변경 시 재빌드 → 실행 → 사후 확인 안내
- `-dry-run` 예행 모드
- `rollback_*.csv` 자동 생성 및 `-rollback` 복원 모드 (NIC device key 기준 복원)
- `report_*.csv` 결과 리포트, 실패 발생 시 종료 코드 `1`
- `-from-portgroup` / `-nic-index` 로 변경할 NIC 지정
- `-pg-cmd` 로 외부 포트그룹 생성 도구를 전체 작업 전 1회 호출
- `-concurrency` 동시 실행 수 제한 (기본 8)
- 문서 `README.md`, `ARCHITECTURE.md`, `PR_CHECKLIST.md`, CI 워크플로

### 설계 결정

원안(계획서)의 "NIC 연결 해제 → 포트그룹 생성 → 재연결" 3단계를
**"사전 포트그룹 확인 → 백킹 교체 Reconfigure 1회 → 검증"** 으로 변경했습니다.

- 원안은 게스트 네트워크를 의도적으로 끊었다가 붙이는 구조여서 다운타임이 발생하고,
  중간 단계에서 실패하면 VM 이 끊긴 채로 남았습니다.
- 포트그룹 생성을 VM 마다 병렬 호출하면 동일 포트그룹을 동시에 여러 번 만들려는 경합이 생겨,
  생성 호출을 전체 작업 전 1회로 옮겼습니다.
- 전 VM 동시 처리는 장애 시 피해가 크므로 동시 실행 수를 제한하고 카나리 절차를 문서화했습니다.
- `find.Finder` 는 고루틴 동시 사용에 안전하지 않아, VM 조회를 `ContainerView` 기반 이름 색인으로 교체했습니다.

### 검증

- Rocky Linux 8 / go1.26.5 에서 `go build -mod=vendor ./...`, `go vet -mod=vendor ./...` 통과
- 실 vCenter 대상으로 dry-run → 실제 이관 → 재실행(SKIPPED) → 롤백 → 원상복구 전 과정 확인
