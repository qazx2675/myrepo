# PR_CHECKLIST.md

- [ ] 빌드 성공 (오프라인 빌드 프로젝트면 vendor 기준으로 확인: `bash setup.sh`)
- [ ] 테스트 통과 (`go vet ./... && go test ./...`, `GOOS=windows go vet ./...`)
- [ ] 실제 환경 또는 시뮬레이터로 최소 1개 시나리오 검증 (가짜 AWX 또는 실제 AWX 왕복)
- [ ] `--snapshot` 전후 비교로 작업 템플릿/인벤토리 소스 변경 0건 확인
- [ ] 전송 후 variables 가 원복되었는지 `--status` 로 확인
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 관련 표/설명 갱신 필요 여부 확인
- [ ] 흐름이 바뀌었으면 WORKFLOW.md / ARCHITECTURE.md 흐름도 갱신
- [ ] `*.conf`, 비밀번호가 커밋에 들어가지 않았는지 확인
