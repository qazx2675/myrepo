# PR_CHECKLIST.md — copy (closed-network-copy)

- [ ] ETX 오프라인 빌드 성공 (`./setup.sh`)
- [ ] Windows 빌드 성공 (`build.bat`, 또는 `go build ./cmd/copy-widget`)
- [ ] 단위 테스트 통과 (`go test ./...`)
- [ ] `go vet ./cmd/copy-send/... ./internal/...` 통과 (widget의 클립보드 코드는
      Windows에서 vet 시 알려진 오탐이 하나 남음 — ARCHITECTURE.md 5번 참고, 새 오류가
      아닌지만 확인)
- [ ] `bash -n setup.sh send.sh` 통과
- [ ] `copy-send`로 공백/탭/여러 줄이 섞인 텍스트를 실제 전송하고, `copy-widget`으로
      받아 클립보드에 `Ctrl+V` 했을 때 원문과 100% 일치하는지 확인
- [ ] `max_lines` 초과 시 `copy-send`가 실제로 전송을 거부하는지 확인
- [ ] AWX에 이전 미수신 데이터가 남아 있을 때 `copy-send`가 기본적으로 거부하고,
      `-force` 를 주면 덮어쓰는지 확인
- [ ] `copy-widget` 클릭 후 AWX의 description이 실제로 비워지는지(cleanup) 확인
- [ ] 위젯이 항상 위로 유지되는지, 드래그로 이동되는지, 오른쪽 클릭으로 정상
      종료되는지 확인
- [ ] `conf/copy_setting.conf` 실물(비밀번호 포함)이 커밋에 들어가지 않았는지 확인
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 옵션 표 갱신 필요 여부 확인
