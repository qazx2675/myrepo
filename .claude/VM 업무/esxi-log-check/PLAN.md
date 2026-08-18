# esxi-log-check (esxi-log-check) - 계획서

## 목적

다중 ESXi 호스트에서 `gossh`로 수집한(또는 도구가 직접 수집하는) `vobd.log` / `vmkernel.log` / `ipmi_sel` 로그를 패턴 레지스트리(`esxi_critical_patterns.yaml`)로 매칭하여, 호스트별 CRITICAL/HIGH 하드웨어 장애 이벤트를 자동으로 추출한다.

## 배경

- 수십~수백 대 규모 ESXi 호스트의 로그를 사람이 직접 눈으로 확인하는 것은 비효율적이고 누락 위험이 크다.
- 단순 키워드 grep만으로는 "빈도가 쌓여야 심각한 것"(aggregate)이나 "연쇄적으로 발생해야 의미 있는 것"(correlation chain, 예: 무증상 리부팅)을 놓치기 쉽다.
- 패턴이 코드에 하드코딩되어 있으면 새로운 장애 케이스가 발견될 때마다 재빌드가 필요해 대응이 늦어진다 → 패턴을 YAML로 외부화.

## 아키텍처 개요

| 컴포넌트 | 파일 | 역할 |
|---|---|---|
| 진입점 | `main.go` | 플래그 파싱, 전체 흐름 제어 |
| 원샷 수집 | `collect.go` | `-w` 모드에서 내부적으로 gossh를 서브프로세스로 호출해 3종 로그 자동 수집 |
| 로그 파서 | `internal/gossh/parser.go` | gossh 출력(호스트별 prefix 포함)을 호스트 단위로 분리 |
| 패턴 레지스트리 | `internal/registry/registry.go` | `esxi_critical_patterns.yaml` 로드 및 검증 |
| 매칭 엔진 | `internal/match/matcher.go` | 로그 라인 ↔ 패턴 매칭, aggregate(빈도 기반 격상) |
| 상관분석 | `internal/correlate/correlate.go` | correlation_chains(연쇄 이벤트) + 무증상 리부팅 탐지(`CHAIN_SILENT_REBOOT`) |
| 리포트 | `internal/report/report.go` | text/json 리포트 생성 |
| 패턴 정의 | `esxi_critical_patterns.yaml` | 40+ 하드웨어 장애 패턴 (재빌드 없이 수정 가능) |

## 마일스톤 진행 현황

| 마일스톤 | 내용 |
|---|---|
| M0 | 패턴 리서치 — `esxi_critical_patterns.yaml`(40+ 패턴) 작성 |
| M1 | SSH Executor(파싱 전용) + Pattern Registry 골격. 실제 gossh 출력으로 검증 완료 |
| M2 | 다중 호스트 처리는 gossh 자체 워커풀에 위임(설계상 이미 충족)로 판단, Severity 리포트 강화(`[0]` 전체집계, 심각도 정렬, `-onlyProblems`) |
| M3 | `aggregate`(빈도 기반 격상), `correlation_chains`(연쇄 이벤트 상관분석 4종), 무증상 리부팅 탐지(`CHAIN_SILENT_REBOOT`), IPMI SEL 전용 타임스탬프 파서 |
| M4 (최신) | `-w` 원샷 모드: 내부적으로 gossh 서브프로세스를 호출해 수집부터 리포트까지 한 번에 처리 |

각 옵션(`-w`, `-input`, `-format`, `-onlyProblems`, `-silentRebootGap`, `-noCorrelate` 등)의 상세 사용법은 같은 디렉토리의 [README.md](./README.md)를 참고.

## 수정 방법

### 1. 장애 패턴 추가/수정 (가장 흔한 수정)

`esxi_critical_patterns.yaml`만 수정하면 되고, **재빌드 불필요**. 각 패턴 항목은 다음 필드를 가진다:

- `source`: 매칭 대상 로그 (`vobd.log` / `vmkernel.log` / `ipmi_sel` 중 하나 — `internal/gossh/parser.go`가 인식하는 소스명과 정확히 일치해야 함)
- 매칭 정규식, 심각도(CRITICAL/HIGH/MEDIUM/LOW), `requires_prev_line_suffix`(직전 라인 조건부 확정 여부) 등

새 소스(`hostd.log` 등)를 추가하려면 `-input <source>=<file>` 수동 모드로 우선 검증한 뒤, `-w` 원샷 모드에 자동 수집시키려면 `collect.go`의 수집 대상 목록에 추가해야 한다.

### 2. correlation_chains(연쇄 이벤트) 추가

`condition`/`then` 타입 체인(`CHAIN_FABRIC_WIDE_APD`, `CHAIN_SILENT_REBOOT`)은 YAML 스키마로 일반화되어 있지 않고 `internal/correlate/correlate.go`에 ID별로 Go 코드로 특별 처리되어 있다. 새 체인 타입을 추가하려면 이 파일에 코드를 추가하고 재빌드해야 한다.

### 3. 무증상 리부팅 탐지 민감도 조정

빌드 없이 `-silentRebootGap <duration>` 플래그로 조정 가능 (기본 30분). 환경별 로그 밀도에 따라 오탐/누락이 있으면 이 값만 조정하면 된다.

### 4. 빌드

```bash
cd ".claude/esxi-log-check"
go mod tidy   # gopkg.in/yaml.v3 등 의존성 다운로드 (인터넷 필요 — air-gapped면 vendor/ 미리 준비)
go build -o esxi-log-check .
```

## 알려진 한계 / 확인 필요

- gossh 실제 출력 포맷은 ESXi 8.0.3 호스트로 검증했으나, SSH 실패 시 정확한 에러 문구는 환경마다 다를 수 있음.
- `aggregate`는 "조건 만족 시점의 특정 발생"이 아니라 "해당 패턴의 findings 전체"를 격상하는 단순화된 방식.
- 무증상 리부팅의 부팅 로그 판별 정규식은 VMware 공개 자료 기준 추정치 — 실제 재부팅 로그로 미검증.
- IPMI SEL 파서(`MM/DD/YYYY HH:MM:SS`)는 실 BMC 하드웨어로 미검증 — 컬럼 위치 비의존 방식으로 리스크는 완화했으나 재검증 권장.
- `-w` 모드에서 특정 소스 하나만 수집 실패하는 세부 케이스(예: vobd.log는 되고 ipmi_sel만 실패)는 코드 구조상 독립 처리되지만 실측 재현 테스트는 아직 안 됨.

## 제외한 항목

- `esxi-log-check` (컴파일된 바이너리) — 소스에서 재빌드 가능하므로 저장소에 포함하지 않음
- `esxi-log-check-test/` 하위 테스트 픽스처(vobd/vmkernel/ipmi_sel 샘플 로그 등) — 로컬 검증용 결과물이라 제외
