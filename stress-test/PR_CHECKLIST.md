# PR_CHECKLIST.md

- [ ] 빌드 성공 — `bash build.sh` (os8). os6 빌드는 하지 않음
- [ ] **소스(`*.go`, `go.mod`)를 바꿨다면 `stress-test` 바이너리를 os8(랩 .58) 에서 재빌드해 함께 커밋** (회사에 Go 없음, `.gitignore` 로 `stress-test` 제외 금지)
- [ ] `gofmt -l .` 출력이 비어 있음
- [ ] `go vet ./...` 통과 (가짜 AWX 모듈은 `cd test/fake_awx && go vet ./...`)
- [ ] `go test ./...` 통과
- [ ] Go 1.20 호환 유지 — `go.mod` 가 `go 1.20`, 그보다 새로운 문법·표준 라이브러리 API 미사용
- [ ] 랩 호스트(.58)에서 `bash test/e2e.sh` 통과 (시나리오 A 정상 / B 미실행·재실행·`-retry 0` 종료코드 1 / D job 실패 / E 오류 종료코드 2), TTY 동작을 건드렸다면 `bash test/e2e_c.sh` 도 확인
- [ ] 실제 gossh 로 랩 VM(.58/.60 등) 접속체크·loadavg 감시를 확인 (실제 AWX 템플릿·실제 `custom_inventory.sh`·AMD 장비는 확인 못 했다면 보고에 명시)
- [ ] 결과표 형식 확인 — 각 줄 5필드(`awk -F'|' 'NF!=5'` 결과 없음), 첫 줄 제목 줄 `hostname|CPU벤더|cpu|stress-ng|비고`, 입력 순서 유지, 단계 값은 `실행/미실행/실패/접속불가/DNS실패/-` 만 사용
- [ ] 종료코드 규약 유지 — 0 전부 실행, 1 미실행/실패 있음, 2 설정·입력 오류 (setup_check 등 호출자가 의존)
- [ ] 비대화형 호출(`-batch`)에서 프롬프트·stdin 읽기가 없음을 확인
- [ ] `bash -n stress_run.sh build.sh test/*.sh` 통과, `shellcheck -S warning stress_run.sh build.sh setup.sh` 경고 없음, conf 키를 추가했으면 `setup.sh` 로 새 키가 질문되는지 확인
- [ ] 래퍼와 바이너리 이름 일치 확인 (`stress_run.sh` ↔ `stress-test`), 실행 권한(`100755`) 유지
- [ ] conf 키를 추가·변경했다면 `conf/sample_setting.conf`, README 설정 키 표(3.3), `config.go` 기본값·`validate` 를 함께 갱신
- [ ] 옵션·결과·종료코드·한계가 바뀌었으면 README.md 해당 표/설명 갱신
- [ ] 실행 흐름이 바뀌었으면 ARCHITECTURE.md 흐름도와 계획서.md 갱신
- [ ] 랩 점검 후 정리 — 대상 서버의 busy loop(`while :; do :; done`) 프로세스 종료, 시험용 conf·output·임시 파일이 커밋에 섞이지 않았는지 확인
