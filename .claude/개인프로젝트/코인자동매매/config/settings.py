"""전역 설정 상수. 계획서 5.1 참조."""

DRY_RUN         = True            # 기본값. False 전환은 사용자 명시 요청 시에만
                                  # 실주문은 이것 + .env LIVE_TRADING=1 + 주문권한 키, 3중 게이트
PAPER_CAPITAL   = 100_000_000     # 실전 전 100_000 으로 재검증 필수
MODE            = "conservative"
ENTRY_HOURS     = None            # 24시간 진입 (제한 없음)

NOTIFIER        = "ntfy"         # ntfy 푸시. 토픽/토큰은 .env
NTFY_SERVER     = "https://ntfy.sh"   # 자가호스팅 시 교체
HEARTBEAT_HOUR  = 9               # 일 1회 생존 신호

FEE_EXPECTED    = 0.0004          # 쿠폰 적용 기준
FEE_ALERT_ABOVE = 0.0005          # 실효 수수료율 초과 시 경고+진입중단

MIN_ORDER_KRW   = 5000            # 빗썸 KRW 마켓 최소 주문금액

# 페이퍼 엔진 현실성 (끄지 말 것)
PAPER_USE_ORDERBOOK = True        # 호가 잔량 순차 소진 방식 체결 (M6 준비 단계에서 구현)
PAPER_FEE_RATE      = 0.0004
PAPER_LATENCY_MS    = 300         # 신호~주문 지연 반영

# ── 백테스터 (M3-a) ────────────────────────────────────────────────────
BACKTEST_FEE_RATE       = 0.0004  # 쿠폰 적용 기준. 실전 검증은 미적용(0.0025)도 한 번 돌려볼 것
FILL_SLIPPAGE_BPS       = 5       # 시장가 체결 편도 슬리피지(bp). 캔들 백테스트 상수.
                                  # 주문금액 대비 슬리피지는 M6 실시간 호가 엔진의 몫.
BACKTEST_EXEC_DELAY_BARS = 1      # 신호는 다음 봉 시가에 체결 (PAPER_LATENCY_MS 의 봉단위 반영)
CANDLE_CACHE_DIR        = "data/candles"

# 튜닝 / 홀드아웃 기간 분리 (계획서 7.7). 홀드아웃은 한 번만 돌린다.
TUNE_START    = "2023-05-19"
TUNE_END      = "2024-12-31"
HOLDOUT_START = "2025-01-01"
HOLDOUT_END   = "2026-06-30"

# ── 차트 패턴 엔진 A (M2-a) ────────────────────────────────────────────
# 고점/저점 스윙에 각각 추세선 1개씩 피팅 → 기울기 부호 + 수렴/발산으로 분류.
PATTERN_WINDOW          = 40      # 분석 봉수
PATTERN_SWING_K         = 3       # 프랙탈 스윙: 좌우 K봉보다 높아야(낮아야) 스윙
PATTERN_MIN_SWINGS      = 3       # 추세선 한쪽당 최소 스윙 개수
PATTERN_FLAT_SLOPE      = 0.0010  # |정규화 기울기| 가 이 값 이하면 '수평' (분율/봉)
PATTERN_CONVERGE_RATIO  = 0.65    # 끝폭 < 시작폭 * 이 값  이면 수렴
PATTERN_CHANNEL_TOL     = 0.30    # |끝폭 - 시작폭| / 시작폭 이 이 값 이하면 평행채널
PATTERN_MIN_CONFIDENCE  = 0.55    # 두 추세선 적합도(min R²) 하한. 미만이면 미탐지
PATTERN_FLAG_LOOKBACK   = 15      # 깃발 방향 판정용: 윈도우 직전 추세 확인 봉수
PATTERN_DEBUG_DIR       = "data/pattern_charts"

# ── 차트 패턴 → 스크리너 점수 (M2-b) ──────────────────────────────────
# 패턴은 '가산 가중치'다. 단독 진입 신호가 아니다 (계획서 7.6).
PATTERN_SCORE_WEIGHT    = 0.20    # 최종 점수 = base_score + WEIGHT * pattern_score([-1,1])
PATTERN_BEARISH_VETO    = True    # 하락형이면 pattern_score 를 강하게 음수로 (M2-a 회피와 연결)

# ── 스크리너 base_score (M4) ──────────────────────────────────────────
SCREEN_MOMENTUM_DAYS    = 20      # 모멘텀/이평 기준 봉수
SCREEN_RSI_DAYS         = 14
SCREEN_RSI_OVERBOUGHT   = 75      # 이 위면 base_score 감점
SCREEN_ENTRY_THRESHOLD  = 0.55    # 최종 점수가 이 값 이상이어야 진입 후보

# ── 포트폴리오 백테스터 (M4) ─────────────────────────────────────────
PF_EXEC_DELAY_BARS      = 1

# ── 물타기 (M8) — 자동 실행 금지. 사람 승인만 (불변 규칙 2) ───────────
# 제안 트리거는 '추적 손절보다 2%p 덜 빠진 지점'. 손절 전에 사람이 승인할 창을 준다.
AVG_TRIGGER_MARGIN_PCT = 2.0   # trailing_stop_pct + 이 값 지점에서 제안
AVG_ADD_FRAC      = 0.5        # 승인 시 (per_coin_max - 현재투입) 의 이 비율만큼 추가 매수
AVG_REQUEST_DIR   = "data/averaging"
AVG_APPROVED_FILE = "data/averaging_approved.txt"

# ── 공지 감시 / 뉴스 (M5) ────────────────────────────────────────────
NOTICE_URL   = "https://api.bithumb.com/v1/notices"
WARNING_URL  = "https://api.bithumb.com/v1/market/virtual_asset_warning"
NOTICE_POLL_COUNT = 30

# 공지 categories → 대응 (계획서 8장)
NOTICE_LIQUIDATE_CATS = ("거래유의", "거래지원종료")          # 즉시 전량 시장가 청산
NOTICE_BLOCK_CATS     = ("입출금", "마켓 추가", "마켓추가")   # 신규 진입 금지 (마켓추가=신규상장 변동성)
# virtual_asset_warning: 이 단계 이상이면 신규 진입 금지 (청산까지는 아님)
WARNING_BLOCK_STEPS   = ("WARNING", "DANGER")

# 뉴스 RSS (제목/요약 키워드 매칭. LLM 미사용 — 계획서 13.1)
NEWS_RSS_FEEDS = (
    "https://cointelegraph.com/rss",
    "https://www.coindesk.com/arc/outboundfeeds/rss/",
    "https://www.theblock.co/rss.xml",
)
NEWS_TIMEOUT = 8
NEWS_LIQUIDATE_KEYWORDS = ("해킹", "탈취", "상장폐지", "거래중단", "횡령", "파산",
                           "도난", "지급불능", "러그풀", "rug pull", "hack", "exploit", "stolen")
NEWS_BLOCK_KEYWORDS     = ("소송", "규제", "조사", "압수수색", "제재", "논란",
                           "lawsuit", "sec ", "investigation", "sanction")
