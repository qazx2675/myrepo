# PR_CHECKLIST.md — ldap_setting

- [ ] 오프라인 빌드 성공 (`./setup.sh`)
- [ ] 단위 테스트 통과 (`go test ./...`)
- [ ] `go vet ./...` 통과
- [ ] `bash -n setup.sh scripts/deploy_ldap.sh test_all.sh` 통과
- [ ] `./test_all.sh` 왕복 테스트 통과 (PASS 14 / FAIL 0)
- [ ] `apply_body.sh` 를 고쳤다면 `../ldap_check/ldap_check.sh` 의 대응 검사도 같이 고쳤는지
- [ ] `conf/ldap_config.conf.sample` 을 고쳤다면 `../ldap_check/ldap_config.conf.sample` 도 같이 고쳤는지
- [ ] 멱등성 확인 (같은 스크립트를 두 번 돌렸을 때 두 번째는 `NOCHANGE`)
- [ ] `-dry-run` 출력이 실제 변경과 일치하는지 확인
- [ ] 실제 노드 또는 `-root` fixture 로 최소 1개 시나리오 검증
- [ ] 설정 변경을 적용했다면 **무작위 서버 몇 대를 직접 확인한 결과**를 PR 에 기재
- [ ] `conf/ldap_config.conf` / `conf/assets.txt` 실물(bindpw 포함)이 커밋에 들어가지 않았는지 확인
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 옵션 표 갱신 필요 여부 확인
