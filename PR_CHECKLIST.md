# PR_CHECKLIST.md

- [ ] 빌드 성공 (오프라인 빌드 확인: `./setup.sh` 또는 `CGO_ENABLED=0 go build ./...`)
- [ ] 정적 바이너리(`bin/account-mount-engine`) 최신 상태 갱신 및 커밋 포함 확인
- [ ] 테스트 통과 (`go test ./...` 및 `go vet ./...`)
- [ ] 셸 스크립트 문법 검사 통과 (`bash -n setup.sh run.sh`)
- [ ] 실제 환경(`home-test`) 또는 시뮬레이터(`-root`)로 최소 1개 시나리오 검증 (신규 추가, 멱등성 재실행, 기존 계정 갱신, 롤백)
- [ ] `info.txt`, `hosts.txt` 실물 데이터가 커밋에 포함되지 않았는지 확인 (`.gitignore` 확인)
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 관련 표/설명 갱신 필요 여부 확인

