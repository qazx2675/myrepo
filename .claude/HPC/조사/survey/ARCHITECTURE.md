# ARCHITECTURE.md

표준 라이브러리만 사용하는 단일 Go 바이너리 + bash 래퍼.

| 폴더/파일 | 역할 |
|---|---|
| `cmd/survey/main.go` | CLI 진입점. conf 로드 → 표1 로드 → gossh 수집 → 행별 조립 → TSV 저장 흐름 조립 |
| `cmd/survey/config.go` | `conf/conf.toml` 하위집합 파서. `Config`, `MountRule` 정의 |
| `cmd/survey/asset.go` | 표1 텍스트 파서. 조사 대상 hostname 순서 목록 + `hostname -> (상태, 위치)` |
| `cmd/survey/collect.go` | 임시 hostfile 작성, `gossh -c <concurrency> -w file -script` **1회 배치 실행**, `hostname: 결과` 파싱, 설정값 추출, 접속 실패 감지 |
| `cmd/survey/rule.go` | `InfraNet`(인프라망 스크립트를 **호스트마다** 실행, `infra_regex` 로 값 추출, gossh 아님), `ApplStatus`(mountpoint→정상위치→표1위치 비교로 O/X) |
| `cmd/survey/output.go` | TSV 헤더/행 조립, 필드 정규화(탭·개행 제거), 결과 파일 저장 |
| `cmd/survey/survey_test.go` | 순수 함수 단위 테스트 (파싱·판정) |
| `run_survey.sh` | 실행 래퍼: 바이너리/conf 확인, 폴더 이동 후 실행 |
| `scripts/infra_survey.sh` | 인프라망 판별 **샘플** 스크립트. 사내 규칙에 맞게 교체 |
| `conf/conf.toml` | 실행 설정 (유일한 설정 파일, 실행 파일 옆 `conf/` 에 위치) |
| `.github/workflows/ci.yml` | push/PR 마다 `go build/vet/test` |

## 수정 요청 → 어디를 보나

| 요청 | 관련 파일 |
|---|---|
| "설정값 수집 명령 바꿔줘" | `conf/conf.toml` 의 `[scripts].config_value` (코드 수정 불필요) |
| "인프라망 판별 로직 바꿔줘" | `scripts/infra_survey.sh` (또는 `conf/conf.toml` 의 `infra_net` 경로) |
| "인프라망 스크립트 출력 형식이 달라" | `conf/conf.toml` 의 `[scripts].infra_regex` (코드 수정 불필요) |
| "gossh 출력 형식이 달라" | `cmd/survey/collect.go` — `parsePdshLine`, `detectError` |
| "gossh 동시 실행 수 바꿔줘" | `conf/conf.toml` 의 `[gossh].concurrency` (코드 수정 불필요) |
| "O/X 판정 기준 바꿔줘" | `cmd/survey/rule.go` — `ApplStatus` |
| "출력 컬럼 추가/순서 변경" | `cmd/survey/output.go` — `tsvHeader`, `cmd/survey/main.go` 의 행 조립 |
| "표1 양식이 달라" | `cmd/survey/asset.go` — `LoadAsset` |
