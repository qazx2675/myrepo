# ARCHITECTURE.md

표준 라이브러리만 사용하는 단일 Go 바이너리 + bash 래퍼.

| 폴더/파일 | 역할 |
|---|---|
| `cmd/survey/main.go` | CLI 진입점. conf→표1→설정값/인프라망 수집→ESXi 판별→SDC 분리→결과파일 A/B/SDC 저장. `splitSDC` |
| `cmd/survey/config.go` | `conf/conf.toml` 하위집합 파서. `Config`, `MountRule` 정의 |
| `cmd/survey/asset.go` | 표1 텍스트 파서. 조사 대상 hostname 순서 목록 + `hostname -> (상태, 위치)` |
| `cmd/survey/collect.go` | `runGossh`(임시 hostfile + gossh 1회 배치 + `hostname: 결과` 파싱). `Collect`(설정값, `-c` 붙음), `CollectInfra`(인프라망 + `infra_regex` 실패 시 `infra_fallback_cmd` 재조사), 설정값 추출, 접속 실패 감지(`DNS 미등록` 분리) |
| `cmd/survey/rule.go` | `applyInfraRegex`(gossh 출력값에 `infra_regex` 적용), `ApplStatus`(mountpoint→정상위치→표1위치 비교로 O/X) |
| `cmd/survey/vm.go` | `DetectESXi`(설정값 없음 호스트에 `uname`→VMkernel), `dnsResolvable`(`net.LookupHost` 로 존재 확인), `SurveyVMs`(`<esxi>ev01~03` 생성→DNS 해석되는 것만 조사, 타임아웃 1회 재조사), `vmName` |
| `cmd/survey/output.go` | TSV 헤더/행 조립, 필드 정규화(탭·개행 제거), 결과 파일 저장 |
| `cmd/survey/survey_test.go` | 순수 함수 단위 테스트 (파싱·판정) |
| `run_survey.sh` | 실행 래퍼: 바이너리/conf 확인, 폴더 이동 후 실행 |
| `update.sh` | 폐쇄망 증분 업데이트(변경분만 복사, conf·result·asset 보존, 오래된 *.go 제거, 멱등) |
| `scripts/infra_survey.sh` | 인프라망 판별 **샘플** 스크립트. 사내 규칙에 맞게 교체 |
| `conf/conf.toml` | 실행 설정 (유일한 설정 파일, 실행 파일 옆 `conf/` 에 위치) |
| `.github/workflows/ci.yml` | push/PR 마다 `go build/vet/test` |

## 수정 요청 → 어디를 보나

| 요청 | 관련 파일 |
|---|---|
| "설정값 수집 명령 바꿔줘" | `conf/conf.toml` 의 `[scripts].config_value` (코드 수정 불필요) |
| "인프라망 판별 로직 바꿔줘" | `scripts/infra_survey.sh` (또는 `conf/conf.toml` 의 `infra_net` 경로) |
| "인프라망 스크립트 출력 형식이 달라" | `conf/conf.toml` 의 `[scripts].infra_regex` / `infra_fallback_cmd` / `infra_fallback_regex` (코드 수정 불필요) |
| "SDC 분리 기준(문자열) 바꿔줘" | `cmd/survey/config.go` — `sdcInfraValue` 상수 |
| "gossh 출력 형식이 달라" | `cmd/survey/collect.go` — `parsePdshLine`, `detectError` |
| "gossh 동시 실행 수 바꿔줘" | `conf/conf.toml` 의 `[gossh].concurrency` (코드 수정 불필요) |
| "O/X 판정 기준 바꿔줘" | `cmd/survey/rule.go` — `ApplStatus` |
| "설정값 없음 판정 기준" | `cmd/survey/collect.go` — `extractConfigValue`(`/` 시작 행만), `Collect` |
| "ESXi 판별 방법 바꿔줘" | `cmd/survey/vm.go` — `DetectESXi` (현재 `uname` → `VMkernel`) |
| "VM 이름 규칙 / 개수 바꿔줘" | `cmd/survey/vm.go` — `vmName`, `vmPerEsxi` |
| "출력 컬럼 추가/순서 변경" | `cmd/survey/output.go` — `tsvHeader`, `cmd/survey/main.go` 의 행 조립 |
| "표1 양식이 달라 (구분자/컬럼)" | `cmd/survey/asset.go` — `LoadAsset` (현재 탭 구분) |
