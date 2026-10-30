# CHANGELOG

## [v2.0.0] - 2026-10-30

### 변경 사항 (Breaking Change)
- **실행 파일명 변경**: `excel-test-runner.exe` -> `sop-generator.exe` (구 실행 파일 삭제). 빌드 명령도 `-o sop-generator.exe`로 변경.
- **대분류 / 중분류 / 상세내용 입력 삭제**: 웹 UI 입력란, `/api/config/category` API, `config.json`의 분류 정보, CLI 플래그(`-major`, `-minor`, `-detail`)를 모두 제거.
- **양식(Template) 기능 삭제**: `templates/` 폴더, 기본 양식 생성(`-create-default-template`), 양식 업로드/선택 UI 및 `/api/templates/*` API, 플래그 `-template` 제거.
- **CLI 플래그 정리**: `-cli -create-sample -target -sheets -out -worker -verifier -object -get-worker` 만 사용.
- **API 정리**: `/api/config/worker`, `/api/target/sheets` 제거. 설정은 `/api/config`, 시트 조회는 `/api/target/load`, 생성은 `/api/generate`로 통합.

### 추가된 기능
- **작업대상 / 검증자 입력 (`web/index.html`, `web/app.js`)**: 작업대상(`target_object`), 검증자(`verifier_name`) 입력란 추가. 입력값은 다음 실행 시 복원.
- **작업자 / 작업대상 / 검증자 영구 저장 (`config.go`)**: `config.json`에 `worker_name`, `verifier_name`, `target_object`, `last_target` 저장. 생성 성공 시 검증자, 작업대상, 마지막 Target 경로 갱신.
- **섹션 동적 탐색 (`excel_com.go` `FindSections`)**: 고정 좌표 없이 `사전작업` / `본작업` / `사후작업` 섹션과 8개 헤더(Step, 작업내용, 작업자에 의한 사전 작업 내용, 작업대상, 검증방법, 작업자, 검증자, 작업결과)를 시트마다 찾음.
- **Step 번호 고정**: 사전작업=1, 본작업=2, 사후작업=3.
- **선택 시트 적재 (`GenerateSOPExcel`)**: 체크한 시트들의 `작업내용`, `작업자에 의한 사전 작업 내용`, `검증방법`을 각 섹션의 병합 셀에 선택 순서대로 세로 적재. 각 블록 앞에 `1. [시트이름]` 헤더 표기.
- **글꼴 및 이미지 보존**: 원본 시트의 글꼴/크기와 병합 셀 내 이미지/도형을 결과 파일에 그대로 복사하고, 행 높이를 자동 확장하여 겹치지 않게 배치.
- **기준 시트 복제 방식**: 첫 번째 선택 시트를 새 통합문서로 통째 복사한 뒤 데이터 병합 셀만 비우고 채움. 원본 Target 파일은 ReadOnly로 열어 읽기만 하며 수정하지 않음.
- **결과 파일명 처리**: 빈 값이면 `SOP_결과본.xlsx`, 확장자가 없으면 `.xlsx` 부착, 경로 성분은 제거하고 실행 파일 폴더에 저장.
- **샘플 Target 생성 (`CreateSampleTarget`)**: 시트 3개(`1_OS설치`, `2_네트워크설정`, `3_보안점검`), 시트마다 다른 행 위치의 3섹션, 14pt 글꼴, 이미지 포함.
- **DoD v2.0 검증 스크립트**: `test_dod.go`, `run_dod_test.bat` (Windows + Excel 필요). 실행 결과(E2E)는 Windows 환경에서 확인 필요.

### 유지 / 기타
- 폐쇄망 오프라인 빌드 지원 (`vendor/`, `go build -mod=vendor`) 유지. 외부 의존성 추가 없음.
- CI (`.github/workflows/ci.yml`): `windows-latest`에서 vendor 기준 빌드 및 `go vet` 실행 (대상 브랜치: `sop`, `master`, `main`).

---

## [v1.2.0] - 2026-10-28

### 추가 및 완성된 기능 (Definition of Done v1.2 16개 항목 전체 통과)
- **폐쇄망 제약 준수 신규 엑셀 생성 엔진 (`excel_com.go`)**:
  - 원본 `target.xlsx` 및 템플릿 엑셀 덮어쓰기 없이 Excel COM 인프로세스 세션을 통해 새 통합문서 생성 및 프로그램 위치에 신규 파일 저장.
  - 사용자가 결과 파일명을 직접 지정 가능 (미지정 시 `SOP_완료.xlsx` 기본값).
- **작업자 이름 관리 시스템 (`config.go`)**:
  - 최초 실행 시 `config.json` 유무를 확인하여 작업자 이름 등록 모달 표출.
  - 프로그램 재실행 후에도 작업자 이름 영구 기억.
  - 상단 헤더의 `[작업자 변경]` 버튼을 통해 언제든 즉시 수정 및 저장.
  - 미확정된 작업자/확인자 엑셀 셀 매핑 제외.
- **분류 및 상세내용 입력 (`web/index.html`, `web/app.js`)**:
  - 대분류 / 중분류 드롭다운 선택 및 직접 입력 기능 구현, 결과 엑셀 병합 셀에 정확히 반영.
  - 상세내용 멀티라인 줄바꿈 보존 엑셀 기록.
- **작업방법(시트별 H2 병합 셀 + 도형) 순서 조합 (`excel_com.go`)**:
  - Target 엑셀의 시트들을 스캔하고, 사용자가 체크한 순서(1., 2., 3...) 엄격 유지.
  - `1. [시트이름]` 자동 표기 및 H2 본문 텍스트 결합.
  - 단일 병합 셀 안에 각 시트의 도형 및 이미지를 겹치지 않게 세로 적재(Vertical Stacking) 계산 및 배치.
- **유동적 양식(Template) 생성 및 관리**:
  - `B8`, `C8`, `G8`, `H8` 기준 기본 표준 양식 생성 기능 내장 (`templates/default_template.xlsx`).
  - 사용자가 업로드한 커스텀 양식의 상단 제목줄(헤더)은 건드리지 않고, 하단에 위치한 플레이스홀더 병합 셀을 자동 탐색하여 교체.
  - `templates/` 폴더를 통한 양식 영구 저장 및 재사용.
- **폐쇄망 오프라인 빌드 지원 (`vendor/`)**:
  - 외부 인터넷 없이 `go build -mod=vendor`로 즉시 빌드 가능.
- **품질 보증**:
  - DoD v1.2 조건정의서 16개 전 항목 일괄 자동 검증 통과 (`test_dod.go`, `run_dod_test.bat`).

---

### 태그 생성 및 원격 푸시 규칙
```bash
git tag -a v1.2.0 -m "SOP 자동 생성 프로그램 v1.2 정식 릴리스 (CHANGELOG 2026-10-28 항목)"
git push origin v1.2.0
```
