# PR_CHECKLIST.md

## 빌드 / 테스트

- [ ] 빌드 성공 — 오프라인(vendor) 기준: `bash setup.sh` (또는 `GOPROXY=off go build -mod=vendor ./...`)
- [ ] `go vet -mod=vendor ./...` 무경고, `gofmt -l .` 비어 있음
- [ ] 테스트 통과 — 빌드 서버(192.168.0.58, root)에서 `bash test/run_tests.sh` → `FAIL=0`
- [ ] 실제 환경 또는 시뮬레이터로 최소 1개 시나리오 검증 — vcsim E2E(`test/e2e_vcsim.sh`) 전부 PASS
- [ ] 의존성을 바꿨다면 `vendor/` 를 갱신(`go mod vendor`, 오프라인은 빌드 서버 모듈 캐시 사용)하고 `bash build_os6.sh` 로 OS6 바이너리도 다시 빌드
- [ ] **cross_check 를 건드렸다면**(별도 모듈, 표준 라이브러리만 — vendor 없음): `cd cross_check && go vet ./... && go test ./... && bash test/e2e.sh <bin>` 통과(`<bin>` = `cross_check/bin/cross_check`, Linux). 전체 화면(TUI)을 건드렸다면 `bash test/pty_e2e.sh <bin>` 도
- [ ] cross_check 코드가 바뀌었으면 `bash build_os6.sh --cross-check-only`(Go 1.20 서버)로 `bin_os6/cross_check.gz` 를 다시 만들었다(못 했다면 사유를 CHANGELOG 에 적음)

## 안전장치 / 보안 (이 도구는 전원을 직접 조작한다)

- [ ] 안전장치 1~5단계 취소 사유와 exit 1 이 그대로다(`test/guard_scenarios.sh` PASS) — 특히 `YeS` 대소문자, 3단계 중복/누락/초과 취소
- [ ] 허용 경로(`allowed_dir`) 밖 실행 불가, conf 권한(root 소유, group/other 쓰기 금지) 검사가 유지된다
- [ ] 대상 파일은 `on_off_<user>.txt` 하나만 읽는다(다른 이름·심볼릭 링크 거부)
- [ ] 비밀번호·확인 코드가 화면 에코, 로그, 감사 로그, 오류 메시지에 남지 않는다(비밀번호는 `LineReader` 로만 읽음)
- [ ] Off/일괄 On/재부팅은 대상 전체 동시 전송, **순차 부팅은 2번 기능(lpage)만이며 ev 단계 단위**(단계 안은 병렬) — 이 규칙이 바뀌었으면 README·계획서·WORKFLOW 갱신
- [ ] 실제 vCenter(home-test)에서 최소 1회 확인했거나, 못 했다면 사유를 CHANGELOG 에 적었다(Tools/heartbeat/IP 와 22 포트 판정은 vcsim 으로 재현 불가)

## 교차검증(cross_check)

- [ ] 필수 정책 문구("승인 완료 후 2분 내에 본 작업을 진행하지 않으면 자동 취소")가 `cross_check/README.md` 최상단에 그대로 있고, 2분은 **기본값**(`approval_ttl_sec`, 10~3600)이라는 설명이 함께 있다
- [ ] **`approval_ttl_sec`·`work_timeout_sec`·auth(`auth`/`users`/`target_dir`) conf 를 바꿨다면 → `cross_check/README.md` 갱신**(설정 표·승인 유효시간 바꾸기·문제 해결), `install.sh` 의 conf 템플릿·`pwonoff.conf.example` 확인
- [ ] 확인자 화면에 작업 대상이 보이지 않고(개수도 안 줌), 대상 불일치·패스워드 오류 메시지에 대상 이름이 들어가지 않는다 (붙여넣기 승인 규칙이 바뀌었으면 README·INTERVIEW 갱신)
- [ ] 상태·단계(`대기`/`승인`/`중단`, `승인/작업중`·`승인/작업완료`·`승인/작업중단`)와 중단 사유 문구를 바꿨다면 README 표·화면·`--finish` 규칙·e2e 가 모두 맞는다

## 문서 / 기록

- [ ] CHANGELOG.md 항목 추가 (날짜, 영향 범위, 검증)
- [ ] README.md 관련 표/설명 갱신 필요 여부 확인 (옵션 표, conf 키, 사용 순서, 주의사항)
- [ ] 흐름이 바뀌었으면 WORKFLOW.md / workflow.svg / ARCHITECTURE.md 갱신
- [ ] conf 키가 바뀌었으면 `pwonoff.conf.example` 과 README 의 conf 표 갱신
- [ ] 사용자가 입력하는 명령이 바뀌었으면 `사용법.txt`(각 명령 위 설명 주석) 갱신

## 저장소

- [ ] 사용자 파일(`on_off_*.txt` 실제 목록, `run_*/`)과 빌드 산출 실행파일(`vm_power_onoff`)이 커밋에 없다 (`bin_os6/*.gz` 는 포함)
- [ ] 배포 시점에 태그를 남겼다: `git tag -a vm-power-onoff-vX.Y.Z -m "<변경 요약> (CHANGELOG YYYY-MM-DD 항목)"` 후 `git push origin vm-power-onoff-vX.Y.Z` (저장소에 다른 프로젝트 태그 v0.1.0 등이 있어 접두어 필수)
