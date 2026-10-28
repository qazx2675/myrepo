# ARCHITECTURE.md

## 📁 프로젝트 파일 및 폴더별 역할

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | 애플리케이션 엔트리포인트, GUI/CLI 분기, 로컬 HTTP 서버 및 윈도우 창 실행 |
| `config.go` | `config.json` 설정 파일 영구 관리 (작업자 이름, 대/중분류, 양식 목록) |
| `excel_com.go` | In-Process Windows COM 엔진 (`go-ole`), 양식 복제, 하단 플레이스홀더 탐색, H2 병합셀+도형 세로 적재 조합, 신규 엑셀 저장 |
| `handlers.go` | 웹 프론트엔드 REST API 핸들러 (설정, 시트 스캔, 양식 관리, SOP 생성, 파일 열기) |
| `web/` | 내장형 단일 웹 UI (HTML, CSS, JavaScript) |
| `web/index.html` | 작업자 등록 모달, 시트 순서 체크리스트, 대/중분류/상세내용 입력 화면 |
| `web/style.css` | 모던 다크 테마 반응형 스타일시트 |
| `web/app.js` | 큐(Queue) 기반 순서 체크 번호 부여, 양식 관리, 비동기 API 통신 로직 |
| `templates/` | 기본 표준 양식(`default_template.xlsx`) 및 사용자 커스텀 양식 보관 폴더 |
| `vendor/` | 폐쇄망(에어갭 환경) 오프라인 빌드를 위한 Go 의존성 패키지 보관소 |
| `test_dod.go` | DoD v1.2 조건정의서 16개 검증 항목 자동 검사 스크립트 |
| `run_dod_test.bat` | 조건정의서 16개 항목 원클릭 자동 검증 배치 파일 |
| `excel-test-runner.exe` | 단일 실행 바이너리 (GUI / CLI 겸용) |

---

> **수정 팁**:
> - 작업방법 셀 병합 위치나 이미지/도형 배치 로직을 변경하려면 `excel_com.go`의 `GenerateSOPExcel` 함수를 확인하세요.
> - 작업자 모달 또는 UI 인터랙션을 수정하려면 `web/app.js` 및 `web/index.html`을 수정하세요.
> - 설정 저장 구조를 확장하려면 `config.go`의 `AppConfig` 구조체를 수정하세요.
