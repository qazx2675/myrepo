# PR_CHECKLIST.md

수정·배포 전에 확인합니다. 앞의 6개는 모든 프로젝트 공통 항목이고, 그 아래는 이 프로젝트(redfish / biostool) 전용입니다.

## 공통

- [ ] 빌드 성공 (오프라인 빌드 프로젝트라 `bash setup.sh` 로 확인 — 이 프로젝트는 외부 의존성이 없어 `vendor/` 가 없음. 의존성을 추가했다면 `go mod vendor` 후 `vendor/` 를 커밋했는지)
- [ ] 테스트 통과 (`go vet ./... && go test ./...`)
- [ ] 실제 환경 또는 시뮬레이터로 최소 1개 시나리오 검증 (mock BMC: `사용법.txt` 의 “mock 연습” 또는 `mockbmc.sh` + `bios_check.sh -dry-run`)
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

### 문서·샘플

- [ ] **합성 testdata 를 갱신했다면** `testdata/README.md` 의 “합성 자료이며 실제 덤프가 아님” 경고와 `Oem.MockData` 표식을 유지했다 (실덤프를 넣을 때는 IP·시리얼·MAC·UUID·자산태그를 지우고 별도 이름으로)
- [ ] README 의 예시 출력·명령이 실제 동작과 같다 (예시 명령을 랩에서 한 번 실행해 확인)
- [ ] 문서에 비밀번호·실제 호스트 이름·키가 없다
- [ ] 새 `.sh` 는 LF 줄바꿈이고(`.gitattributes`), RHEL6 의 bash 4.1 에서 동작한다 (`[[ -v ]]`·연관 배열 등 4.2/4.4 전용 문법 금지). Windows 에서 만든 파일은 실행 권한 비트가 빠질 수 있으니 필요하면 `git update-index --chmod=+x <파일>`
- [ ] 소스·동작이 바뀌었으면 `계획서.md` 의 상태표·검증 현황·단계 진행표 갱신
- [ ] 릴리스라면 CHANGELOG 에 맞춰 태그: `git tag -a vX.Y.Z -m "<요약> (CHANGELOG YYYY-MM-DD 항목)"` 후 `git push origin vX.Y.Z`

## 흐름도 다시 그리기

`workflow*.svg` 는 WORKFLOW.md 의 mermaid 블록 5개를 렌더링한 것입니다. mermaid 를 고쳤으면 다시 렌더링해 같은 이름으로 교체하십시오(폐쇄망 밖의 랩에서 `docker.io/minlag/mermaid-cli` 컨테이너 사용). 설정 `{"flowchart":{"htmlLabels":false,"useMaxWidth":false,"padding":14,"nodeSpacing":40,"rankSpacing":38,"curve":"basis","wrappingWidth":800},"themeVariables":{"fontFamily":"Malgun Gothic, Noto Sans KR, Noto Sans CJK KR, sans-serif","fontSize":"16px"}}` 와 `-b white -I <고유id>` 를 쓰고, 렌더링 결과는 `width`/`height` 를 명시하고 흰 배경 `<rect>` 를 깔아(다크 모드 미리보기에서도 읽히게) 저장한 뒤 `xmllint --noout` 로 유효성을 확인했습니다. 텍스트가 잘리지 않는지 브라우저로 직접 열어 보십시오.
