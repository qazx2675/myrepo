# PR_CHECKLIST.md

- [ ] 빌드 성공 (`bash setup.sh`, vendor 기준 오프라인)
- [ ] 테스트 통과 (`go vet -mod=vendor ./...`, `go test -mod=vendor ./...`)
- [ ] 실제 환경 또는 mock BMC 로 최소 1개 시나리오 검증
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 관련 표/설명 갱신 필요 여부 확인
- [ ] 흐름이 바뀌었으면 WORKFLOW.md / ARCHITECTURE.md 흐름도 갱신
