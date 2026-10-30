# ARCHITECTURE.md

## 📁 프로젝트 파일 및 폴더별 역할

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | 애플리케이션 엔트리포인트, GUI/CLI 분기, CLI 플래그(`-cli -create-sample -target -sheets -out -worker -verifier -object -get-worker`) 처리, 로컬 HTTP 서버 및 윈도우 창 실행 |
| `config.go` | `config.json` 설정 파일 영구 관리 (`worker_name`, `verifier_name`, `target_object`, `last_target`) |
| `excel_com.go` | Excel COM 엔진 (`go-ole`). Target 시트 스캔(`ScanTargetSheets`), 섹션 동적 탐색(`FindSections`), 선택 시트 세로 적재 및 신규 엑셀 저장(`GenerateSOPExcel`). 샘플 Target 생성(`CreateSampleTarget`, `excelize`) 포함 |
| `handlers.go` | 웹 프론트엔드 REST API 핸들러 (`/api/config`, `/api/target/load`, `/api/generate`, `/api/create-sample-target`, `/api/open-file`, `/api/exit`, `/api/heartbeat`) |
| `web/` | 내장형 단일 웹 UI (HTML, CSS, JavaScript). `go:embed`로 실행 파일에 포함 |
| `web/index.html` | 작업자 등록 모달, 작업대상/검증자 입력, Target 파일 로드, 시트 순서 체크리스트, 결과 파일명 입력 화면 |
| `web/style.css` | 모던 다크 테마 반응형 스타일시트 |
| `web/app.js` | 큐(Queue) 기반 순서 체크 번호 부여, 저장된 작업대상/검증자 복원, 비동기 API 통신 로직 |
| `vendor/` | 폐쇄망(에어갭 환경) 오프라인 빌드를 위한 Go 의존성 패키지 보관소 (`go-ole`, `excelize/v2` 등) |
| `excel-test-runner.manifest` | 실행 파일 매니페스트 (`rsrc` 입력) |
| `app.ico` | 실행 파일 아이콘 (`rsrc` 입력) |
| `rsrc_windows_amd64.syso` | `rsrc`로 생성한 아이콘/매니페스트 리소스 오브젝트 (빌드 시 자동 링크) |
| `target.xlsx` | 원본 작업 시트 파일 (샘플). 프로그램은 읽기 전용으로만 열며 수정하지 않음 |
| `config.json` | 실행 시 생성/갱신되는 사용자 설정 파일 |
| `test_dod.go` | DoD v2.0 검증 항목 자동 검사 스크립트 (`//go:build ignore`, Windows + Excel 필요) |
| `run.bat` | `sop-generator.exe` 실행 배치 파일 |
| `run_cli_test.bat` | 샘플 생성 후 CLI로 SOP 생성까지 한 번에 실행하는 배치 파일 |
| `run_dod_test.bat` | DoD v2.0 항목 원클릭 자동 검증 배치 파일 |
| `.github/workflows/ci.yml` | `windows-latest`에서 vendor 기준 빌드 및 `go vet` 수행하는 CI 워크플로 |
| `sop-generator.exe` | 단일 실행 바이너리 (GUI / CLI 겸용) |

---

> **수정 팁**:
> - 섹션 탐색(헤더/타이틀 위치) 로직을 변경하려면 `excel_com.go`의 `FindSections`와 `matchHeaderRow`를 확인하세요.
> - 선택 시트 적재 위치, 행 높이 계산, 이미지/도형 배치 로직을 변경하려면 `excel_com.go`의 `GenerateSOPExcel`과 `buildColumnPlan`을 확인하세요.
> - 샘플 Target 양식을 바꾸려면 `excel_com.go`의 `CreateSampleTarget`을 수정하세요 (`test_dod.go`의 전제조건 검사와 함께 확인).
> - 작업자 모달 또는 UI 인터랙션을 수정하려면 `web/app.js` 및 `web/index.html`을 수정하세요.
> - API 요청/응답 구조를 바꾸려면 `handlers.go`와 `excel_com.go`의 `SOPGenerateRequest` / `SOPGenerateResult` / `TargetSheetInfo`를 함께 수정하세요.
> - 설정 저장 구조를 확장하려면 `config.go`의 `AppConfig` 구조체를 수정하세요.
