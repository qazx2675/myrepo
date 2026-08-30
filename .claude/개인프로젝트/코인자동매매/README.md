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
run_backtest.bat  [--holdout]     M3-a 단일종목 백테스터 데모 (MA 크로스 — 파이프라인 확인용)
run_m2a.bat       [--holdout]     M2-a 하락형 필터 A/B + 패턴 탐지빈도 + 차트
run_m2b.bat       [--holdout]     M2-b 진입규칙 A/B/C/D (패턴 가중치)
run_m4.bat        [--holdout]     M4 포트폴리오 백테스터 (리스크 규칙 + 패턴 실측정)
run_m5.bat        [--ntfy]        M5 빗썸 공지/경보 + 뉴스 RSS 실조회 (--ntfy: 테스트 알림)
run_m55.bat                       M5.5 실시간 호가 주문크기별 슬리피지 측정
run_m6.bat  [--once|--reset]      M6 매매 루프 (기본 = 조회 전용 키 + paper 체결)
approve_averaging.bat KRW-XXX     M8 물타기 제안 승인
```

홀드아웃(2025-01~2026-06)은 **1회만**. `data/.holdout_used` 에 기록된다.

## 현재 상태

**M1 ~ M8 코드 전부 완성.** 남은 건 실행/승인 단계 (전부 사용자 몫).

- 부품: `screener` / `risk` / `paper`(실시간 호가체결) / `trader`(실주문 3중 게이트) /
  `notice`·`news`(감시) / `notifier`(ntfy) / `store`(SQLite) / `averaging`(물타기 승인)
- `live_loop.py` 가 엮어서 매매. 재시작 시 `data/trades.db` 복원. 페이퍼/실주문 자동 라우팅.
- **차트 패턴은 진입 게이트로 채택 안 함** (3번 측정 모두 중립). 엔진은 소프트 가산만.

### 남은 단계 (전부 사용자)

| 단계 | 할 일 |
|---|---|
| **튜닝** | 현재 데모 스크리너로는 conservative MDD ~15% (목표 10% 초과). `screener.base_score` 보강 / 손절폭 조정 후 `run_m4` → 홀드아웃 검증 |
| **M6-a** | `.env` 에 `NTFY_TOPIC`. `run_m6.bat --reset` 을 집 PC/VPS 에서 **2~4주** 상시 (`PAPER_CAPITAL=1억`) |
| **M6-b** | `settings.PAPER_CAPITAL = 100_000` 으로 바꿔 `run_m6.bat --reset`, **1주**. 거부된 주문 0건 확인 |
| **M7** | 빗썸 **주문 권한** 키 발급 → `.env` 교체 + `LIVE_TRADING=1` + `DRY_RUN=False`. **M6-b 통과 후에만** |

