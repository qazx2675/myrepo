# PR 체크리스트

- [ ] `shellcheck build_bundle.sh deploy_repo.sh` 통과
- [ ] `bash -n` 문법 검사 통과 (두 스크립트)
- [ ] 번들 생성 확인: `bundles/*.tar.gz`, `sha256sum -c`, tar 최상위 디렉터리(`rhel8.10`, `rhel9.6`)
- [ ] 랩/시뮬레이션 배포 시나리오 1회 이상 (성공 호스트 + 실패 호스트 모두 포함, 요약 출력 확인)
- [ ] 재실행 멱등성 확인 (같은 호스트에 다시 실행해도 동일 결과, 파일 중복 없음)
- [ ] `CHANGELOG.md` 갱신
- [ ] README 표(옵션, 환경변수, 문서 목록) 갱신 필요 여부 확인
