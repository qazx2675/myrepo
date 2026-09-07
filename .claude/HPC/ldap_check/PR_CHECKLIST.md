# PR_CHECKLIST.md — ldap_check

- [ ] `bash -n ldap_check.sh test_check.sh` 통과
- [ ] `./test_check.sh` 전 시나리오 통과 (PASS 10 / FAIL 0)
- [ ] `../ldap_setting/test_all.sh` 왕복 테스트도 통과 (적용 쪽과 짝이 맞는지)
- [ ] `ldap_config.conf.sample` 을 고쳤다면 `../ldap_setting/conf/ldap_config.conf.sample` 도 같이 고쳤는지
- [ ] 스크립트가 여전히 **읽기 전용**인지 (파일을 쓰는 코드가 들어가지 않았는지)
- [ ] jq 의존성이 새로 들어가지 않았는지
- [ ] 실제 노드 최소 1대에서 실행해 OK/FAIL 이 상식과 맞는지 확인
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 옵션·키 표 갱신 필요 여부 확인
