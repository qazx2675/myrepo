# worklog-tracker

여러 사용자가 공유하는 bash 스크립트에 **함수 몇 줄만 추가**하면 되는
작업 로그 자동 기록 시스템입니다.

- user는 자동 지정(기존 select 로직 그대로 사용), incident 번호만 입력받아 검증
- 여러 명이 동시에 실행해도 락 없이 안전 (파일 분리 + append 원자성)
- **"완료 시각"은 스크립트가 추정하지 않고 사람이 명시적으로 처리** — 미완료 건은
  다음 실행 때 자동으로 안내되어 누락되기 어려운 구조
- 완료 시 `vswitch_{user}.txt`, `spec_list_{user}` 산출물 자동 수집
- (선택) `script(1)` 기반 터미널 전체 로그 녹화 지원

자세한 설계 배경은 [`계획서.md`](./계획서.md) 참고.

## 빠른 시작

```bash
git clone <이 저장소> worklog-tracker
```

### 1) 기존 스크립트에 붙이기

```bash
# 기존 스크립트 최상단
source "/path/to/worklog-tracker/lib/worklog.sh"

# 기존 user 선택(select) 로직 바로 다음 줄
worklog_start "$SELECTED_USER"

# ── 기존 작업 로직은 전혀 수정할 필요 없음 ──

# (선택) 스크립트 끝에서 바로 완료 처리 여부를 물어보고 싶다면
worklog_finish_prompt "$SELECTED_USER"
```

전체 예시: [`docs/integration-example.sh`](./docs/integration-example.sh)

### 2) 미완료 작업 완료 처리 (독립 실행, 언제든 가능)

```bash
./bin/worklog-finish.sh
```

### 3) 미완료 작업 현황 조회

```bash
./bin/worklog-status.sh          # 전체
./bin/worklog-status.sh kim      # 특정 user만
```

## 동작 방식 요약

| 상황 | 동작 |
|---|---|
| 스크립트 실행 (`worklog_start`) | 즉시 START 기록. incident 번호 형식(`REQ`+숫자) 검증 후 입력받음 |
| 같은 user의 미완료 건이 있을 때 | 목록으로 안내, 완료 처리할 건 하나 선택 가능(강제 아님), 스킵하면 계속 미완료로 유지 |
| 완료 처리 | 완료 시각 직접 입력(기본값 현재시각), 소요시간 자동 계산, 산출물 파일 자동 수집 |
| 동시 실행 | user/incident별 파일 분리 + `all.log`는 한 줄 append로 원자적 기록 → 락 불필요 |

## 디렉토리 구조

```
worklog-tracker/
├── lib/worklog.sh              # 핵심 함수 라이브러리
├── bin/worklog-finish.sh       # 완료 처리 독립 명령
├── bin/worklog-status.sh       # 상태 조회 독립 명령
├── docs/integration-example.sh # 통합 예시
├── 계획서.md
└── logs/                       # 실행 시 자동 생성 (기본적으로 git 추적 제외)
```

## 설정

`lib/worklog.sh` 상단에서 아래 항목을 환경에 맞게 조정하세요.

- `WORKLOG_ARTIFACT_PATTERNS` — 완료 시 수집할 산출물 파일명 패턴
- `WORKLOG_ARTIFACT_SEARCH_DIRS` — 산출물 파일을 찾을 디렉토리 목록
- `WORKLOG_INCIDENT_REGEX` — 인시던트 번호 형식 (자릿수 확정되면 수정)

`bin/worklog-finish.sh`의 `USERS=(...)` 배열도 실제 사용자 목록으로 교체하세요.

## 라이선스

내부 운영용 스크립트 — 필요에 맞게 자유롭게 수정해서 사용하세요.
