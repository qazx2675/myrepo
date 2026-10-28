# PR_CHECKLIST.md

- [x] 빌드 성공 (오프라인 폐쇄망 환경 vendor 기준 확인: `go build -mod=vendor -ldflags="-H windowsgui -s -w" -o excel-test-runner.exe .`)
- [x] 조건정의서(DoD v1.2) 전 항목 테스트 통과 (`go run -mod=vendor test_dod.go` 16개 항목 전체 PASS)
- [x] 폐쇄망 제약사항 검증 (target 및 템플릿 파일 불변 확인, 신규 엑셀 정상 생성)
- [x] 작업자 이름 영구 기억 및 상시 변경 UI/API 검증
- [x] 시트별 H2 병합 셀 및 이미지/도형 세로 적재 순서 조합 검증
- [x] 상단 헤더 보존 및 하단 플레이스홀더 병합 셀 자동 교체 검증
- [x] CHANGELOG.md 항목 추가 및 버전 태그 규칙 명시
- [x] README.md 표준 규격(1번 빌드 -> 2번 사용법 -> 3번 옵션 -> 4번 주의사항 -> 5번 전역 명령어) 준수
