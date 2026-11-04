# PR_CHECKLIST.md

- [ ] 빌드 성공 — `bash build.sh` (os8) 와 `bash build.sh os6` (os6, Go 1.20 이 있는 빌드 서버) 모두 성공
- [ ] **소스(`*.go`, `go.mod`)를 바꿨다면 `bin/new_setup` 과 `bin/new_setup_os6` 를 둘 다 재빌드해 함께 커밋** (회사에 Go 없음, `.gitignore` 로 `bin/` 제외 금지)
- [ ] `gofmt -l .` 출력이 비어 있음
- [ ] `go vet ./...` 통과
- [ ] `go test ./...` 통과
- [ ] Go 1.20 호환 유지 — `go.mod` 가 `go 1.20`, 그보다 새로운 문법·표준 라이브러리 API 미사용
- [ ] 랩 호스트(.58)에서 `bash test/run_tests.sh` 통과 (fake gossh 로 완료/접속불가/디스크미인식/설치정지/OS설치중, 이전 실행 `_os_install` 잔존 무시 확인)
- [ ] 출력 형식 확인 — 각 줄이 탭 1개로 2필드 (`awk -F'\t' 'NF!=2'` 결과 없음), 입력 순서 유지, 헤더 없음, stdout 에 안내문 없음
- [ ] 실제 gossh 로 랩 VM 몇 대에 실행해 `완료`/`접속불가` 확인 (설치중 계열은 fake 로만 확인했다면 보고에 명시)
- [ ] os6 바이너리를 os6 빌드 서버에서 실제로 실행해 확인
- [ ] `shellcheck new_setup.sh build.sh test/*.sh` 경고 없음
- [ ] 래퍼와 바이너리 이름 일치 확인 (`new_setup.sh` ↔ `bin/new_setup`, `bin/new_setup_os6`), 실행 권한(`100755`) 유지
- [ ] README.md 관련 표/설명 갱신 필요 여부 확인 (상태값·설정 키·종료코드·한계)
- [ ] 판정 흐름이 바뀌었으면 ARCHITECTURE.md 흐름도와 계획서.md 갱신
