# PR_CHECKLIST.md

- [ ] `data/manual.md` 를 복사한 상태에서 `./build.sh` 성공 (vet + test + 정적 빌드)
- [ ] `go test ./...` 전부 통과 (holdout 은 기본 경고만, 엄격 확인은 `HPCBOT_STRICT=1`)
- [ ] 실제 시나리오 1건 이상 실행: `dist/hpcbot "gpu 드라이버 설치"`, 후보(`설치`)·미매칭(`점심 메뉴`) 종료 코드 2/1
- [ ] seed/kb 를 바꿨다면 `HPCBOT_REPORT=1 go test -run TestAccuracy .` 로 test_report.md 갱신 (holdout 은 튜닝에 쓰지 않음)
- [ ] CHANGELOG.md 갱신
- [ ] README 의 정확도 표·사용법.txt 명령 확인
- [ ] 커밋에 `data/manual.md`, `dist/`, Jev 키가 없는지 확인 (`git status`, `git grep`)
