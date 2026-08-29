# 빗썸 코인 자동매매

철학: **"돈을 버는 시스템"이 아니라 "잃지 않는 시스템".**

문서 (이어서 작업할 때 이 3개를 읽는다):

| 파일 | 역할 |
|---|---|
| [계획서.md](계획서.md) | 전체 설계 |
| [CLAUDE.md](CLAUDE.md) | 작업 규칙 (하지 말 것) |
| [진행상황.md](진행상황.md) | 현재 위치 / 다음 작업 / 재발 실수 방지 |

## 셋업 (Windows)

`setup.bat` 을 더블클릭하거나 명령창에서 실행한다. venv 생성 + 의존성 설치 +
`.env` 생성까지 한 번에 한다. 그 뒤 `.env` 를 열어 빗썸 **조회 전용** 키를 채운다.

수동으로 하려면:

```
cd .claude\개인프로젝트\코인자동매매
py -m venv .venv
.venv\Scripts\python -m pip install -r requirements.txt
copy .env.example .env
```

## M1 점검 (Windows)

```
run_m1.bat              공개 조회만 (키 불필요)
run_m1.bat --private    .env 키로 잔고 조회 + 권한 검증
```

수동: `.venv\Scripts\python -m tests.test_m1 [--private]`

`[3] OK — 조회 전용 키 확인됨` 이 나오면 M1 완료.
`[3] FAIL` 이면 그 키는 주문 가능 → 폐기 후 조회 전용으로 재발급.

## 백테스트 / 패턴

```
run_backtest.bat  [--holdout]     M3-a 백테스터 데모 (MA 크로스 — 파이프라인 확인용, 실전 아님)
run_m2a.bat       [--holdout]     M2-a 하락형 회피 필터 A/B + 패턴 탐지빈도 + 차트
run_m2b.bat       [--holdout]     M2-b 진입규칙 A/B/C/D (패턴 가중치)
```

홀드아웃(2025-01~2026-06)은 **1회만**. `data/.holdout_used` 에 기록된다.

## 현재 상태

M1 + M3-a + M2-a + M2-b 완료. M1 인증부만 키 대기.
패턴 엔진/스코어링 메커니즘은 됐고, **필터·가중치 채택 여부는 M4**(진짜 스크리너 진입 로직) 에서
홀드아웃으로 재측정한다. 다음: **M4 리스크/자금관리**.

