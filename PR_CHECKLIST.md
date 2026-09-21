# PR_CHECKLIST.md

V2(VMsetup + vm-param-check + SPEC_DIR)를 배포하거나 수정하기 전 확인 목록입니다.

## 빌드 / 정적 분석

- [ ] 폐쇄망 기준 빌드 성공 — V2 루트 `bash setup.sh` (11개 전부 OK), V2 폴더만 복사한 곳에서도 확인 (`unshare -rn bash setup.sh` 로 네트워크를 끊고도 성공)
- [ ] `gofmt -l` 출력 없음 (변경한 Go 파일)
- [ ] `go vet -mod=vendor ./...` 통과
- [ ] 테스트 통과 — `vm-param-check`: `go test -mod=vendor ./...`, `nic_assign-source`: `go test -mod=vendor .`
- [ ] `bash -n VMsetup/vm_setup.sh` 통과
- [ ] 의존성을 추가/변경했다면 `govendor/`를 갱신하고 사용하는 모든 도구를 다시 빌드

## 동작 검증 (최소 1개 시나리오)

- [ ] vcsim으로 **수정 전/후 결과 비교**: 같은 인벤토리에서 ev01~ev03 VM 설정 덤프 차이 0건 (기존 동작 불변)
- [ ] ev 규칙: ev01 필수, 번호 누락(ev02 없이 ev03)은 에러, 값 없는 ev는 만들지 않음
- [ ] `vm_setup.sh` 시나리오(자동 할당 / 수동 선택 / vim 입력) — `검증/vmsetup_test.sh`
- [ ] 데이터센터 2개 이상 + 폴더 여러 단계에서 동작 (`검증/scenarios.sh`)
- [ ] 실제 vCenter(home-test)에서 최소 1회 — 특히 **전원 켜진 VM의 "연결됨" 체크**(vcsim으로 재현 불가)
- [ ] 재실행 시 멱등 (`nic_assign`은 "이미 적용됨", `vm_create`는 기존 VM 스킵)

## 문서 / 기록

- [ ] `VMsetup/CHANGELOG.md` 또는 `vm-param-check-usability-improvement/CHANGELOG.md` 항목 추가 (날짜, 영향 범위, 검증)
- [ ] README.md 관련 표/설명 갱신 필요 여부 확인 (옵션 표, 사용 순서, 알려진 한계)
- [ ] 동작이 바뀌는 곳(예: 값 없는 ev는 만들지 않음)은 CHANGELOG에 "동작 변경"으로 명시
- [ ] 작업기록을 `.claude/VM-10대-확장-및-SPEC_DIR-공유/`(또는 새 작업 폴더)에 남김

## 저장소

- [ ] 사용자 파일(`SPEC_DIR/vswitch_*.txt`, 실제 스펙 폴더, `<user>.txt`, `run_*/`)과 빌드 산출 실행파일이 커밋에 없다
- [ ] `master`의 `.claude/VM/V2` 변경을 `V2` 브랜치에도 반영했다 (`git subtree split --prefix=.claude/VM/V2 -b V2` 후 push)
- [ ] 배포 시점에 태그를 남겼다: `git tag -a v2.x.y -m "<변경 요약> (CHANGELOG YYYY-MM-DD 항목)"`
