# PR_CHECKLIST.md — Network_Change_Integration_Script

배포/수정 전 점검. 이 스크립트는 하위 3개 프로젝트를 함께 건드리므로,
그쪽 체크리스트도 같이 봅니다.

## 이 프로젝트

- [ ] `./setup.sh` 성공 (오프라인: `GOMODCACHE=$(mktemp -d) GOPROXY=off` 로 확인)
- [ ] `bash -n change.sh setup.sh lib/*.sh` 통과
- [ ] `bash tests/test_preprocess.sh` 통과 (전처리 Case 1/2/3 회귀)
- [ ] `./change.sh --help` 정상 출력
- [ ] `./change.sh --dry-run <계정>` 전 구간 (IP 는 대상표만, LDAP/포트그룹은 엔진 dry-run)
- [ ] `--only C` / `--only D` / `--only E` / `--from D` 각각 확인
- [ ] `--debug-inventory` 로 vCenter 인벤토리 덤프 확인
- [ ] 실기 검증: **1대 → 소수(3~5대) → 전체** 순서로 IP 단계부터
- [ ] 결과 파일 5종(`on_off` / `ip_ok` / `ldap_ok` / `*.failed` / `failed_*`) 내용 확인
- [ ] `--retry ip` / `--retry ldap` 로 실패분만 다시 도는지 확인
- [ ] 인시던트 저장 → `./change.sh port <인시던트>` 재개 확인
- [ ] `./change.sh rollback <인시던트>` — 포트그룹·LDAP 자동 원복, IP 는 안내만
- [ ] 엔진 출력 파싱 계약: `ip-change-engine` / `ldap-config-engine` 출력 형식이
      바뀌지 않았는지 (CI "엔진 출력 계약 확인" 통과)
- [ ] `README.md` 에러코드 대응표에 새 코드 반영
- [ ] `CHANGELOG.md` 항목 추가

## 하위 프로젝트 (이 통합으로 인해 바뀐 부분)

- [ ] `ldap_setting`: `go test ./...` (자산현황 중복/`default_site`/`ev` fallback)
- [ ] `vm-network-migration`: `go test ./...` (`LoadVMList`/`SameHost`),
      `nm-inventory` 라이브 동작
- [ ] 두 프로젝트 `CHANGELOG.md` 갱신, 원본에 반영됐는지 확인

## 배포 태그

```bash
git tag -a nci-v0.1.0 -m "<변경 요약> (CHANGELOG 2026-09-09 항목)"
git push origin nci-v0.1.0
```
