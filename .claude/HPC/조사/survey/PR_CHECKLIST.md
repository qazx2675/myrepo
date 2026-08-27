# PR_CHECKLIST.md

- [ ] 빌드 성공 (`cd .claude/HPC/조사/survey && go build ./...` — stdlib only, 네트워크 불필요)
- [ ] 정적 분석 통과 (`go vet ./...`)
- [ ] 테스트 통과 (`go test ./...`)
- [ ] 실제 환경 또는 시뮬레이터로 최소 1개 시나리오 검증
      (정상 host 1대 + 접속불가 host 1대 섞어서 결과 행 수·특이사항 확인)
- [ ] 결과 파일을 엑셀에 붙여넣어 셀 분리 정상 확인
- [ ] `conf/conf.toml` 경로/값이 배포 환경 기준으로 맞는지 확인
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 관련 표/설명 갱신 필요 여부 확인
- [ ] 배포 시 버전 태그: `git tag -a vX.Y.Z -m "<요약> (CHANGELOG YYYY-MM-DD 항목)" && git push origin vX.Y.Z`
