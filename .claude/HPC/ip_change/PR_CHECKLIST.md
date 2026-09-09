# PR_CHECKLIST.md — ip_change

- [ ] 오프라인 빌드 성공 (`./setup.sh`)
- [ ] 단위 테스트 통과 (`go test ./...`, bash 가 있는 빌드 서버에서 왕복(end-to-end) 테스트까지 포함)
- [ ] `go vet ./...` 통과
- [ ] `bash -n setup.sh scripts/run_ip_change.sh` 통과
- [ ] `apply_body.sh` 를 고쳤다면 `internal/render/render_test.go` 의 왕복 테스트도 같이 갱신했는지
- [ ] `conf/ip_change.conf.sample` 을 고쳤다면 `internal/config/config.go` 검증 로직도 같이 고쳤는지
- [ ] 멱등성 확인 (같은 대상에 같은 스크립트를 두 번 돌렸을 때 두 번째도 정상 종료하는지 — 이미 바뀐 IP 라 "현재 서비스 IP" 를 못 찾는 것이 정상 동작임을 함께 기재)
- [ ] 백업이 파일당 정확히 한 번만 생성되는지 (`.bak.<시각>` 이 1개)
- [ ] 실제 노드 또는 fixture(`ROOT`)로 최소 1개 시나리오 검증 (성공 1건, 실패 1건 이상)
- [ ] 게이트웨이 계산이 요구한 `.1` 규칙과 일치하는지
- [ ] 서비스 재시작이 어디에도 없는지(요구사항)
- [ ] 출력 형식이 `hostname ip gateway` 한 줄인지 (성공 시)
- [ ] **적용을 실제로 수행했다면 무작위 서버 몇 대를 직접 접속해 확인한 결과를 PR 에 기재**
- [ ] `conf/*.txt`, `conf/ip_change.conf` 실물(실제 호스트/IP)이 커밋에 들어가지 않았는지 확인
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 옵션 표 갱신 필요 여부 확인
