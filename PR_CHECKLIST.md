# PR_CHECKLIST.md

- [ ] 빌드 성공 — `bash build.sh` (os8). os6 빌드는 2026-11-04 부터 하지 않음
- [ ] **소스(`*.go`, `go.mod`)를 바꿨다면 `bin/new_setup`(os8) 만 재빌드해 함께 커밋** (`bin/new_setup_os6` 는 옛 버전 그대로 두고 재빌드하지 않음, 회사에 Go 없음, `.gitignore` 로 `bin/` 제외 금지)
- [ ] `gofmt -l .` 출력이 비어 있음
- [ ] `go vet ./...` 통과
- [ ] `go test ./...` 통과
- [ ] Go 1.20 호환 유지 — `go.mod` 가 `go 1.20`, 그보다 새로운 문법·표준 라이브러리 API 미사용
- [ ] 랩 호스트(.58)에서 `bash test/run_tests.sh` 통과 (fake gossh 로 완료/접속불가/디스크미인식/설치정지/OS설치중, 이전 실행 `_os_install` 잔존 무시 확인)
- [ ] 출력 형식 확인 — 각 줄이 `|` 로 8필드 (`awk -F'|' 'NF!=8'` 결과 없음), 첫 줄은 제목 줄(`hostname|상태|설정체크|CPU테스트|stress-ng|H/T|OS|비고`), 이후 입력 순서 유지, stdout 에 안내문 없음
- [ ] 실제 gossh 로 랩 VM 몇 대에 실행해 `완료`/`접속불가` 확인 (설치중 계열은 fake 로만 확인했다면 보고에 명시)
- [ ] `shellcheck new_setup.sh build.sh test/*.sh` 경고 없음
- [ ] 래퍼와 바이너리 이름 일치 확인 (`new_setup.sh` ↔ `bin/new_setup`, 옛 `bin/new_setup_os6`), 실행 권한(`100755`) 유지
- [ ] README.md 관련 표/설명 갱신 필요 여부 확인 (상태값·설정 키·종료코드·한계)
- [ ] 판정 흐름이 바뀌었으면 ARCHITECTURE.md 흐름도와 계획서.md 갱신
- [ ] 원격 명령(1차 정보수집·2차 anaconda.log)이 csh/tcsh 로그인 셸에서도 동작 — 변수 대입·`{ }`·`exit`·`!`·글롭 없이 `;` `&&` `||` 와 작은따옴표만 사용, 랩 .60(csh)에서 실제 gossh 로 확인
- [ ] 설정체크: config_check.sh 가 `-auto-check` 를 지원하는지 확인 (미지원 구버전이면 실행하지 않고 `-` + 경고), 실제 config_check.sh 를 랩에 두고 `run.sh` 대역으로 `-auto-check` 경로 확인 (운영 run.sh 로는 확인 불가 — 보고에 명시)
- [ ] 설정 키를 추가·변경했다면 `new_setup.conf.sample`, README 설정 키 표, 계획서.md 를 함께 갱신
