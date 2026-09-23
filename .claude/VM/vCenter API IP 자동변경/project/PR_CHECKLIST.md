# PR 체크리스트

- [ ] `./setup.sh` 오프라인 빌드 성공 (`GOPROXY=off`, `-mod=vendor`)
- [ ] `GOFLAGS=-mod=vendor GOPROXY=off go vet ./...` / `go test ./...` 통과
- [ ] `./e2e-test.sh` → `PASS`
- [ ] 화면/Ctrl+C 를 바꿨다면 `사용법.txt` 의 수동 시험(1000대)을 실제 터미널에서 확인
- [ ] 게스트 스크립트(vsphere.go)를 바꿨다면 실제 VM 몇 대로 확인 + `cmd/fake-vcenter` 의 `stageOf` 확인
- [ ] `CHANGELOG.md` 에 날짜별 항목 추가
- [ ] `README.md` 표/설명(옵션, 진행 화면, 시험 환경) 갱신 필요 여부 확인
