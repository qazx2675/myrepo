# CHANGELOG

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
