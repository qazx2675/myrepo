# PR_CHECKLIST.md (v2.0)

## 빌드 / 정적 검사
- [x] Windows 대상 교차 컴파일 성공 (Linux 환경, vendor 기준: `GOOS=windows GOARCH=amd64 GOFLAGS=-mod=vendor go build -o /dev/null .`)
- [x] Windows 대상 `go vet` 통과 (`.` 및 `test_dod.go`)
- [ ] Windows 실기 빌드 성공 (`go build -mod=vendor -ldflags="-H windowsgui -s -w" -o sop-generator.exe .`, `rsrc` 리소스 반영 포함)
- [ ] GitHub Actions CI (`windows-latest`) 빌드 및 vet 통과

## DoD v2.0 E2E 검증 (Windows + Excel 필요, `run_dod_test.bat` 또는 `go run -mod=vendor test_dod.go`)
- [ ] 샘플 target.xlsx 생성 및 열기, 샘플 전제조건(3개 시트, 3섹션, 시트별 상이한 행 위치, 14pt) 확인 (DoD-0.1)
- [ ] 결과 파일이 지정한 이름으로 실행 파일 폴더에 생성 (DoD-1.2)
- [ ] 원본 target.xlsx 불변 확인 (DoD-1.3)
- [ ] 양식 복제: 데이터 블록 병합 구조, 열 너비, 데이터 셀 14pt, 섹션 제목 글꼴 유지 (DoD-1.4 ~ 1.7)
- [ ] 대분류/중분류/상세내용/기본양식 관련 코드·문자열 및 `templates/` 폴더 완전 삭제 확인 (DoD-2.1, 2.2)
- [ ] `config.json`에 `worker_name` 영구 저장, `verifier_name` / `target_object` 키 존재 (DoD-3.1)
- [ ] `web/index.html`에 작업대상(`targetObject`), 검증자(`verifierName`) 입력란 존재 (DoD-3.2)
- [ ] 작업대상/작업자/검증자가 3개 섹션 모두에 정확히 기입 (DoD-3.3)
- [ ] 결과 시트에서 3개 섹션 / 8개 헤더를 고정 좌표 없이 동적 탐색 (DoD-4.1)
- [ ] Step 값 사전작업=1, 본작업=2, 사후작업=3 (DoD-4.2)
- [ ] 선택 순서대로 `1. [시트이름]` 헤더 적재 및 원본 텍스트 포함 (DoD-5.1, 5.2)
- [ ] 이미지/도형: Excel COM 검증, 병합 영역 내 가로 배치, 열별 세로 비겹침, 열 헤더 아래 위치, 행 높이 충족 (DoD-5.4 ~ 5.8)
- [ ] 전 항목 단일 실행으로 일괄 PASS (DoD-0)

## 문서 및 기타
- [x] CHANGELOG.md 에 v2.0.0 항목 추가 (v1.2 이력 유지)
- [x] README.md 를 v2.0 기준으로 갱신 (빌드 -> 사용법 -> 옵션 -> 주의사항 -> 전역 명령어 순서 유지)
- [x] ARCHITECTURE.md 파일 역할 표 갱신
- [x] 폐쇄망 제약 확인: `vendor/` 기준 빌드 가능, 신규 외부 의존성 없음
