# PR_CHECKLIST.md

- [ ] 빌드 성공 — 랩에서 `go build`/`go vet`/`go test` 성공, 소스가 바뀌었으면 `gossh_os6`(Go 1.20)·`gossh_os8` 재빌드 후 함께 커밋 (`vendor/` 없음)
- [ ] 테스트 통과 (`go test ./...` 또는 해당 테스트 스크립트, 테스트가 있는 경우)
- [ ] 실제 환경 또는 시뮬레이터로 최소 1개 시나리오 검증 — 테스트 호스트 목록으로 `-w` 실행 + gossh를 부르는 도구 1개 이상 동작 확인
- [ ] 설정 변경 도구라면 적용 후 무작위 대상 몇 대를 직접 확인
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 관련 표/설명 갱신 필요 여부 확인
- [ ] 흐름이 바뀌었으면 WORKFLOW.md / ARCHITECTURE.md 흐름도 갱신
