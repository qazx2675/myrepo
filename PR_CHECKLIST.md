# PR_CHECKLIST.md — 패스워드변경자동화 (pwreset)

- [ ] `go build ./...` 성공
- [ ] `go vet ./...` 통과
- [ ] `bash -n run.sh` 통과
- [ ] `-encrypt` 로 암호화한 `newpass.enc` 를 기본 모드로 정상 복호화해
      새 비밀번호가 원본과 일치하는지 확인 (왕복 테스트)
- [ ] `promptfsm.go` 의 정규식을 고쳤다면 실제 서버 프롬프트 문구와 다시
      대조했는지
- [ ] `list.txt.example`/`old_password.txt.example` 을 고쳤다면
      `targets.go` 의 파싱 로직도 같이 고쳤는지
- [ ] 새 비밀번호 평문이 로그·CSV·에러 메시지 어디에도 남지 않는지 확인
- [ ] 실제 서버(또는 재현 가능한 테스트 대상) 최소 1대로 검증 — **미완료
      (아직 실서버 검증 전)**
- [ ] `list.txt`, `old_password.txt`, `newpass.enc`, `key.bin`, `result.csv`
      실물이 커밋에 들어가지 않았는지 확인 (`.gitignore` 확인)
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 옵션 표/사전 조건 갱신 필요 여부 확인
