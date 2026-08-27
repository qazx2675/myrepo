# CHANGELOG

## [v0.3.0] - 2026-08-27

### Changed
- **표1 파싱을 탭(`\t`) 구분으로 변경.** 상태·위치에 공백이 들어가도 정상 파싱
  (탭이 있으면 탭으로만 분리, 없으면 공백 폴백). 샘플도 탭 구분으로 교체.
- **인프라망 조사를 gossh 배치로 변경.** 기존: 로컬에서 `bash <script> <hostname>` 을
  호스트마다 실행. 변경: `gossh -w <hosts> -script "bash <infra_net>"` 1회 실행 후
  각 호스트 출력값(`hostname: 뒤`)에 `infra_regex` 적용. `-c` 는 붙지 않음.
  → `infra_net` 은 이제 **조사 대상 호스트에 배포된** 스크립트/명령이다.
- **"설정값 없음" 판정 정정.** 해당 호스트의 gossh 출력이 아예 없으면(또는 `/appl`
  매칭 행이 없으면) 설정값·appl설정유무 = `없음`. 접속 실패(ssh 에러)일 때만 공란 + `접속불가`.
- `extractConfigValue` 는 `/` 로 시작하는 줄만 후보로 삼는다
  (gossh/grep 에러 라인이 `ERROR: ...` 형태로 설정값에 섞여 `X` 로 나오던 문제 해결).

## [v0.2.0] - 2026-08-27

### Changed
- 설정값이 없을 때(응답은 받았으나 auto.appl 매칭 행 없음): 설정값 = `없음`,
  appl설정유무 = `없음` (기존: 공란 + `X` + 특이사항 "설정값 없음"). 특이사항에는 표기하지 않음.
- 접속불가 등으로 조사 자체가 안 된 경우: 설정값·appl설정유무 공란, 사유는 특이사항.
- 인프라망 조사는 gossh 접속 여부와 무관하게 항상 수행됨을 문서에 명시.

### Added
- `[scripts].infra_regex` — 인프라망 스크립트 출력에서 값을 뽑는 정규식(캡처 그룹 1).
  예: `'^\S+:\s+INFO\s+(.+)$'` → `web01: INFO ldap infra site` 에서 `ldap infra site`.
  비우면 첫 줄 전체 사용, 매칭 실패 시 공란(미조사).
- conf 파서가 작은따옴표(`'...'`) 리터럴 문자열도 허용(정규식 입력 편의).
- 인프라 스크립트가 0이 아닌 종료코드여도 stdout 이 있으면 사용.
- 인프라 스크립트는 `bash <경로> <hostname>` 으로 실행 (실행권한/셔뱅 불필요).

## [v0.1.0] - 2026-08-27

### Added
- 초기 구현 (계획서: `.claude/HPC/조사/계획서.md`)
  - `cmd/survey` Go 도구: 표1 파싱 → gossh 병렬 수집 → TSV 결과 파일
  - 표1 hostname 열 전체를 조사 대상으로 자동 사용 (중간 목록 파일 없음)
  - 설정값(auto.appl 마지막 필드) 추출, mountpoint→정상위치→표1위치 비교로 appl설정유무 O/X
  - 설정값 조사: `gossh -c <concurrency> -w file -script` 1회 배치 (기본 동시 4000, `conf` 조정 가능)
  - 인프라망 조사: gossh 와 별개로 로컬 스크립트를 호스트마다 실행
  - 인프라망은 conf 지정 스크립트(`scripts/infra_survey.sh` 샘플)에 위임
  - pdsh 형식(`hostname: 결과`) 출력 파싱 및 접속 실패 감지 → 특이사항
  - 탭 구분 결과 파일 `result_YYYYMMDD_HHMM.tsv` (필드 내 탭·개행 스페이스 치환)
- `run_survey.sh` 실행 래퍼 (인자 없음)
- 설정은 실행 파일 옆 `conf/conf.toml` 하나만 사용 (옵션 지정 없음)
- 문서: `README.md`, `ARCHITECTURE.md`, `PR_CHECKLIST.md`
- `.github/workflows/ci.yml` (build/vet/test)
- 단위 테스트 `cmd/survey/survey_test.go`

### Verified
- Rocky Linux (go 1.26.5) 에서 `go build ./...` / `go vet ./...` / `go test ./...` 통과.
- 가짜 gossh(pdsh 형식 출력) + 접속불가 host 1대 섞은 스모크 테스트:
  3행 모두 출력, 탭 구분, 접속불가 host 는 특이사항만 채워짐, O/X 정상.

### Notes
- 외부 모듈 의존성 없음(표준 라이브러리만) → 폐쇄망에서 `go build` 만으로 빌드.
  이 때문에 계획서의 "TOML 라이브러리 + go mod vendor" 대신 conf 하위집합 자체 파서를 사용.
- 미확정 항목(리스크): 실제 `gossh` 출력 형식(pdsh 가정), 인프라망 판별 실제 규칙(샘플 제공).
