# CHANGELOG.md — 패스워드변경자동화 (pwreset)

## 2026-09-29

- `cmd/pwreset` 초기 구현. 비밀번호 롤테이션 누락으로 SSH 접속 시
  `Current password` 변경 프롬프트가 뜨는 서버(최대 3대, 랩 환경)를 과거
  비밀번호 후보로 자동 로그인 후 새 비밀번호로 맞추는 도구(기존 expect
  기반을 Go 로 재작성).
  - SSH 인증 단계(keyboard-interactive)와 로그인 후 pty 셸 단계, 두 경로
    모두에서 프롬프트를 처리 (`promptfsm.go` 상태머신을 공유)
  - 새 비밀번호는 AES-256-GCM 으로 암호화해 `newpass.enc`(+`key.bin`)로
    보관, 평문은 메모리에서만 다룸
  - 대상 서버마다 goroutine 을 띄워 병렬 처리, 결과는 `result.csv` 로 기록
  - `run.sh`(빌드 + 입력 파일 검증 + 실행 래퍼), `list.txt.example`,
    `old_password.txt.example`, `.gitignore`, README/ARCHITECTURE/PR_CHECKLIST
    문서 추가
  - **실서버 검증 예정** — 아직 실제 대상 서버로 시험하지 않았습니다.
