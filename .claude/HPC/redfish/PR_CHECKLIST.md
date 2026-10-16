# PR_CHECKLIST.md

수정·배포 전에 확인합니다. 앞의 6개는 모든 프로젝트 공통 항목이고, 그 아래는 이 프로젝트(redfish / biostool) 전용입니다.

## 공통

- [ ] 빌드 성공 (오프라인 빌드 프로젝트라 `bash setup.sh` 로 확인 — 이 프로젝트는 외부 의존성이 없어 `vendor/` 가 없음. 의존성을 추가했다면 `go mod vendor` 후 `vendor/` 를 커밋했는지)
- [ ] 테스트 통과 (`go vet ./... && go test ./...`)
- [ ] 실제 환경 또는 시뮬레이터로 최소 1개 시나리오 검증 (mock BMC: `사용법.txt` 의 “mock 연습” 또는 `mockbmc.sh` + `bios_check.sh -dry-run`)
- [ ] 테스트케이스 `testcase/rf_fallback_case.sh` 통과 (`bash testcase/rf_fallback_case.sh`, 종합 PASS)
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 관련 표/설명 갱신 필요 여부 확인 (플래그·conf 키·상태 코드·결과 파일 열이 바뀌면 3절 표)
- [ ] 흐름이 바뀌었으면 WORKFLOW.md / ARCHITECTURE.md 흐름도 갱신 (WORKFLOW.md 의 mermaid 를 고쳤으면 아래 “흐름도 다시 그리기”로 `workflow*.svg` 도 갱신)

## 이 프로젝트 전용

### 바이너리 (회사 환경에는 Go 가 없음)

- [ ] **Go 소스를 바꿨다면 바이너리를 다시 빌드**했다: `bash build.sh`(→ `bin/biostool`, 랩 .58 의 Go) 와 `bash build_os6.sh`(→ `bin/biostool_os6`, **랩 .60 의 Go 1.20.x**)
- [ ] **두 바이너리를 함께 커밋**했다 (`.gitignore` 에서 `bin/` 을 제외하지 않았는지 확인). 소스만 바꾸고 바이너리를 안 바꾸면 회사에서는 옛 동작이 그대로 나감
- [ ] 새 바이너리의 sha256 을 기록했다 (`sha256sum bin/biostool bin/biostool_os6` → CHANGELOG 의 해당 버전 항목)
- [ ] 두 바이너리가 같은 입력(mock)에서 같은 결과를 내는지 확인했다 (os8 ↔ os6 결과 파일 비교, 타임스탬프 제외)
- [ ] Go 1.20 호환을 지켰다: `slices`·`maps`·`cmp` 패키지, `min`/`max`/`clear` 내장함수, `log/slog` 금지. 확신이 없으면 .60(Go 1.20)에서 빌드해 확인

### 통합 점검 (`bios.sh` / TUI / `-auto`)

- [ ] **`-auto`/`-auto-check` 경로에 쓰기가 없다**: 비대화형은 프로파일 점검만 하고 BMC 에 PATCH·POST 를 보내지 않는다(`setplan_test.go` 가 `applySetPlan` 을 TUI 흐름 밖에서 호출하지 못하게 막는지, mock 의 `Writes` 가 0 인지). `-plain`·`-from-dump` 도 쓰기 없음
- [ ] `check.res_<user>` 가 **호출한 폴더**에 만들어지고(`-res-dir`, `bios.sh` 가 호출 폴더를 넘김) `-auto` 는 `_postapply` 도 같은 내용으로 만든다. targets.txt 는 호출한 폴더 기준 경로이고, FAIL 이 있어도 종료코드 0 이다
- [ ] **TUI 에서 Ctrl+C·Ctrl+Z 는 무시**(“종료는 q 를 누르세요”)되고 **Ctrl+X 는 진행 중 작업(접속·조회, 기준 조회, 설정 적용)을 취소**한다. 터미널에서 직접 한 번 확인(종료 후 터미널 상태가 정상으로 돌아오는지 포함)
- [ ] 수정 단계 규칙이 유지된다: 프로파일 `verified=Y` 만, 전체비교는 `allcheck_set=on` 일 때만, `[충돌]` 은 설정하지 않음, 남의 Pending 은 질문(y/n/Ctrl+X), Pending 까지만(재부팅 없음)
- [ ] 태그 3종 묶음(`tagFor`)·`(N/A)` 표기·CSV 4종(UTF-8 BOM)·`check.res_<user>` 형식을 바꿨다면 README 2.0절·3.6절·사용법.txt 와 `계획서_v0.2_TUI.md` 를 함께 갱신했다
- [ ] 옛 래퍼 호환: `bios_check.sh`(옛 전용 옵션이 있으면 옛 방식)·`all_bios_check.sh`(옵션 없이 터미널이면 `bios.sh`)의 분기가 그대로다

- [ ] **Cisco XML 혼합 대상(`xmlhost.go`·`classifyTargets`·프로파일 `# [xml]` 구역·`mockucsm`)을 바꿨다면**: XML 호출이 `aaaLogin`·`configResolveClass`·`aaaLogout` 뿐이고 쓰기 경로가 생기지 않았다 (`internal/ucsmxml` 관문 시험), `AUTH_FAIL` 은 Redfish 로 재시도하지 않는다, XML 호스트가 전체비교(`XML_READONLY`)·설정 계획에서 빠진다, `xmlhost_test.go`·`ucsm_test.go`·`tui_flow_test.go` 와 README 2.0절·3.3절·3.4절·사용법.txt·`testcase/README.md` 를 맞췄다. mock 소스를 고쳤다면 `bin/mockucsm`·`bin/mockucsm_os6` 도 재빌드해 함께 커밋

### 랩 검증 (Windows 에서 Go 를 실행하지 않음)

- [ ] 랩 .58(Go 1.25)에서 `go vet ./... && go test ./...` 통과
- [ ] 랩 .60(Go 1.20, `export PATH=/opt/go1.20/bin:$PATH CGO_ENABLED=0`)에서 `go vet ./... && go build ./cmd/biostool && go test ./...` 통과 — **.60 빌드 실패는 곧 RHEL6 호환 실패**
- [ ] 랩에서 `bash setup.sh` 성공 (go1.25, go1.20 둘 다)
- [ ] 실제 BMC 에 접속하지 않았다 (모든 시험은 mock·`-from-dump`)

### 안전 규칙 (허용목록·쓰기 경로)

- [ ] **허용목록(`redfish.go` 의 `checkAllowed`·`checkBody`·`reDangerAttr`)을 바꿨다면** `redfish_security_test.go`·`set_security_test.go` 에 시험을 추가·갱신하고 통과시켰다 (로그 조회·ClearLog·Reset·남의 세션/Job 삭제가 여전히 전송 전에 막히는지, mock 의 `LogHits`=0·`Writes` 가 읽기 전용 경로에서 0 인지)
- [ ] 쓰기(PATCH·Job POST)는 `set.go` 의 경로에서만 일어난다 (다른 파일이 `net/http` 를 직접 쓰지 않음: `TestNoRawHTTPOutsideRedfish`)
- [ ] 호스트당 세션 생성 1·삭제 1 이 유지된다 (mock 집계 `sessions_created`/`sessions_deleted`)
- [ ] 재부팅·즉시 적용(`ApplyTime=Immediate`)·ComputerSystem.Reset 을 보내는 경로가 생기지 않았다
- [ ] `verified=Y` 규칙을 지켰다: `*` 행에는 `verified=Y` 불가(로드 오류), 설정은 모델을 지정한 행만
- [ ] 비밀번호·토큰이 출력·로그·TSV·결과 파일·오류 상세에 나오지 않는다 (마스킹 시험 유지)
- [ ] `profiles/*.tsv` 의 `verified=Y` 승격은 **실장비 dry-run 본문을 사람이 확인한 뒤에만** ([FIRST_RUN.md](FIRST_RUN.md))

### 계정 생성·변경

- [ ] **계정 쓰기(`account*.go`·`ModeAccount`·`ucsmxml` 계정 모드)를 바꿨다면**: 허용 경로·본문 검사 시험(`TestCheckAllowedAccountPaths`·`TestCheckAccountBodies`·`TestAccountClientNeedsRegistration`·`ucsmxml_account_test.go`)이 통과하고, BIOS 점검·설정 모드에서 계정 경로가 계속 거부되며, 롤백이 **이번 실행이 만든 계정만** 지우는지(`TestCreateVerifyFailRollsBackOnlyOurs`), 비밀번호가 출력·결과·오류에 없는지 확인했다
- [ ] `bash testcase/rf_fallback_case.sh` 와 `RF_FORCE_OS6=1 …` 가 모두 PASS(시나리오 1~11)이고, `bin/biostool account change` 를 **터미널에서 직접** 한 번 실행해 `*` 표시·붙여넣기 거부·결과를 확인했다
### 문서·샘플

- [ ] **합성 testdata 를 갱신했다면** `testdata/README.md` 의 “합성 자료이며 실제 덤프가 아님” 경고와 `Oem.MockData` 표식을 유지했다 (실덤프를 넣을 때는 IP·시리얼·MAC·UUID·자산태그를 지우고 별도 이름으로)
- [ ] README 의 예시 출력·명령이 실제 동작과 같다 (예시 명령을 랩에서 한 번 실행해 확인)
- [ ] 문서에 비밀번호·실제 호스트 이름·키가 없다
- [ ] 새 `.sh` 는 LF 줄바꿈이고(`.gitattributes`), RHEL6 의 bash 4.1 에서 동작한다 (`[[ -v ]]`·연관 배열 등 4.2/4.4 전용 문법 금지). Windows 에서 만든 파일은 실행 권한 비트가 빠질 수 있으니 필요하면 `git update-index --chmod=+x <파일>`
- [ ] 소스·동작이 바뀌었으면 `계획서.md` 의 상태표·검증 현황·단계 진행표 갱신
- [ ] 릴리스라면 CHANGELOG 에 맞춰 태그: `git tag -a vX.Y.Z -m "<요약> (CHANGELOG YYYY-MM-DD 항목)"` 후 `git push origin vX.Y.Z`

## 흐름도 다시 그리기

`workflow*.svg` 는 WORKFLOW.md 의 mermaid 블록 8개를 렌더링한 것입니다(블록 순서 = workflow, _set, _dump, _allcheck, _retry, _account, _bios, _auto). `workflow_bios.svg`·`workflow_auto.svg` 와 `workflow.svg`·`workflow_allcheck.svg` 는 아래 절차 대신 `npx -y @mermaid-js/mermaid-cli@11 -i in.mmd -o out.svg -c cfg.json`(`cfg.json` = `{"htmlLabels":false,"flowchart":{"htmlLabels":false}}` + 위와 같은 `fontFamily`·간격 설정)로 렌더링한 뒤, `width`/`height` 정수 명시·`viewBox="-8 -8 W H"`·흰 배경 `<rect>`·`font-size:16px` 로 후처리했습니다. 어느 방법이든 `<foreignObject>` 가 0개이고 한글이 `<text>` 로 들어 있는지 확인합니다. mermaid 를 고쳤으면 다시 렌더링해 같은 이름으로 교체하십시오. GitHub 에서 SVG 가 잘려 보이던 원인은 글자가 `foreignObject`(고정 크기 상자)에 들어 있어 뷰어 글꼴이 렌더링 때보다 넓으면 잘린 것이므로, **글자를 `<text>` 로 내는 `htmlLabels:false` 로 렌더링**합니다. `mermaid@10.9.3` 의 `mermaid.render` 를 브라우저에서 호출하고 설정은 `{"htmlLabels":false,"useMaxWidth":false,"padding":24,"nodeSpacing":45,"rankSpacing":45,"curve":"basis","wrappingWidth":800}`, `fontFamily` 는 `Malgun Gothic, Noto Sans KR, Noto Sans CJK KR, sans-serif`, **측정용 `fontSize` 는 17px** 로 하고 저장 전에 SVG 안의 `font-size:17px` 를 `16px` 로 바꿉니다(박스에 약 6% 여유). 그 뒤 `width`/`height` 를 올림한 정수로 명시하고 `viewBox="-8 -8 W H"` 에 맞춘 흰 배경 `<rect>` 를 깔아(다크 모드 미리보기에서도 읽히게) 저장하며, XML 유효성과 `<foreignObject>` 가 0개인지 확인합니다. 텍스트가 잘리지 않는지 브라우저로 직접 열어 보십시오.
