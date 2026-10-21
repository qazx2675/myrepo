# CHANGELOG.md — 패스워드변경자동화 (pwreset)

## 2026-10-21 (3차) — 키 인증 우선 (PasswordAuthentication no 대응)

- **SSH 키 인증을 1순위로** 추가(gossh 와 동일 방식). `-i <키>` 또는 `~/.ssh`
  의 `id_ed25519`·`id_ecdsa`·`id_rsa` 자동 탐색. 후보 비밀번호는 폴백.
  - 대상 서버가 `PasswordAuthentication no` 로 비밀번호 인증을 막아둔 경우
    기존(비밀번호 전용)엔 `접속불가` 로 실패하던 것을, 키로 접속해 pty
    세션에서 강제변경을 처리하도록 함. 강제변경의 `Current password` 응답엔
    `old_password.txt` 후보를 그대로 사용.
  - 성공 기준을 서버의 `all authentication tokens updated successfully`
    출력으로 삼음(password 인증이 꺼진 서버에선 새 비번 재접속 검증이 불가).
  - 결과 status 에 `미적용`(접속은 됐으나 변경 안 됨) 추가. 접속만 된 후보는
    다음 후보까지 계속 시도.
  - 랩 검증: Rocky 에서 `PasswordAuthentication no`+만료계정+키 구성으로
    비밀번호 전용 실패 → 키 인증 접속·강제변경·만료해제 확인.

## 2026-10-21 (2차) — 변경 유실 버그 수정 + 재접속 검증

- **거짓 성공/변경 유실 버그 수정.** `runShell` 이 `Retype` 를 보내는 즉시
  성공으로 간주해 반환 → 호출부가 서버의 변경 처리 완료 전에 연결을 닫아
  변경이 유실되던 문제. 이제 `all authentication tokens updated
  successfully`(또는 셸 프롬프트)까지 기다린 뒤 닫습니다.
- **새 비밀번호 재접속 검증 추가.** 변경 시도 후 새 비밀번호로 실제 로그인이
  되는지 확인해(`verifyLogin`) 그 결과를 성공 기준으로 삼습니다. 변경 직후
  세션을 끊는 서버에서도 올바르게 판정됩니다.
- **결과 CSV 에 `detail` 컬럼 추가.** `verified` /
  `connected-but-not-applied` / 접속 오류 문자열로 실패 사유를 남깁니다.
- 랩 검증: Rocky(만료 계정)에서 `kbdinteractive` on/off 두 구성 모두 변경·검증
  확인.

## 2026-10-21

- CLI 사용성 개선 및 배포 방식 변경.
  - `-encrypt -in <file> -out <out> -key <key>` 조합을 `-pass <평문파일>`
    하나로 대체. `key.bin`/`newpass.enc` 경로는 고정, 없으면 `key.bin` 자동
    생성
  - 비밀번호 변경 실행 시 `-oldpw`/`-newpw`/`-key` 플래그 제거.
    `old_password.txt`/`newpass.enc`/`key.bin` 을 코드 상수로 고정 — `-list
    <파일>` 만으로 바로 시작
  - `list.txt` 대상 검증에서 IPv4 형식 체크 제거. 호스트명으로도 SSH 접속이
    가능하므로 IP/호스트명 모두 그대로 허용
  - `run.sh` 에서 `go build` 단계 제거. 바이너리(`bin/pwreset`)를 미리
    빌드해 저장소에 커밋하는 방식으로 배포하며, `run.sh` 는 그 바이너리를
    바로 실행하는 래퍼로 축소

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
