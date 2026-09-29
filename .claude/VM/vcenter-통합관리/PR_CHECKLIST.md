# PR_CHECKLIST.md

`vcenter-portal` 을 배포하거나 수정하기 전 확인 목록입니다.

## 빌드 / 정적 분석

- [ ] 폐쇄망 기준 빌드 성공 (Windows `build.ps1` / Linux `./build.sh` — `-mod=vendor`, 둘 다 같은 `dist` 구성), `dist/vc-portal/` 구성이 README 1.2 와 일치
- [ ] `gofmt -l ./cmd ./internal` 출력 없음
- [ ] `go vet -mod=vendor ./...` 통과
- [ ] `go test -mod=vendor ./...` 통과
- [ ] `node --check web/assets/*.js` 통과
- [ ] `GOOS=windows go build -mod=vendor ./cmd/...` 성공
- [ ] 의존성을 추가/변경했다면 `go mod tidy && go mod vendor` 후 `vendor/` 포함
- [ ] `dist` 의 `.ps1`/`.example` 이 UTF-8 BOM + CRLF 인지 확인 (`file dist/vc-portal/*/*.ps1`)

## 동작 검증 (랩 vCenter 기준 최소 1개 시나리오)

- [ ] `run-collector.ps1 -Check` 종료 코드 0, 실제 수집 후 `data\manifest.js`/`index.js`/`<id>.js` 생성
- [ ] 일부 vCenter 를 일부러 실패(잘못된 주소 등)시켰을 때 종료 코드 2, 직전 데이터 유지, 화면 배너 표시
- [ ] 중복 실행 시 종료 코드 4
- [ ] `index.html` 을 탐색기(`file://`)로 열어 트리/검색/Summary 가 실제 데이터로 표시됨
- [ ] "vCenter에서 열기" 로 런처가 자동 로그인하고 대상 화면이 열림 (런처/딥링크를 고쳤다면 필수)
- [ ] 대규모 화면 확인이 필요하면 `node testdata/gen-fake.js` 데이터로 확인

## 문서

- [ ] `CHANGELOG.md` 에 날짜/변경 내용 기록
- [ ] README 의 표(옵션/종료 코드/파라미터/문서별 설명)가 실제 코드와 일치
- [ ] 데이터 형식을 바꿨다면 `docs/DATA_SCHEMA.md` 갱신 (수집기와 웹 UI 를 함께 수정)
- [ ] 구조/규칙을 바꿨다면 `ARCHITECTURE.md` 갱신
- [ ] 리포지토리에 실제 계정/비밀번호/내부 주소가 들어가지 않음 (`vcportal.conf` 는 커밋 금지, `*.example` 만)
