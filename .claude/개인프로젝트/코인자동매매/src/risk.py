"""리스크 / 자금관리 (M4-a) — 이 프로젝트의 핵심.

"잃지 않는 시스템": 매매 로직보다 이 모듈이 우선한다.

- 모드 프로파일 (conservative / aggressive) — 계획서 4장, 10만원 기준
- 진입 사이징 / 손절가(하드+추적) / 익절(부분매도) / 일일 손실 한도
- **최소 주문금액 5,000원 검증** — 분할매도는 전량으로 승격, 신규진입은 스킵
- 실효 수수료율 감시 (쿠폰 만료 감지)
- 유의종목 즉시 청산 판정 (불변 규칙 7)

전부 순수 함수. 백테스터/실시간 루프가 이걸 호출한다.
"""

from __future__ import annotations

from dataclasses import dataclass

from config import settings
from src.fills import Order


@dataclass(frozen=True)
class Mode:
    name: str
    capital: int                 # 운용금 (KRW)
    reserve: int                 # 예비금 (KRW)
    max_positions: int
    per_coin_max: int            # 코인당 최대 투입 (KRW)
    first_entry_frac: float      # 1차 진입 비중 (per_coin_max 대비)
    stop_loss_pct: float         # 손절선 (음수, %)
    take_profit_pct: float       # 익절 발동 수익률 (%)
    take_profit_sell_frac: float # 익절 시 매도 비중
    trailing_stop_pct: float     # 추적 손절 (음수, 고점 대비 %)
    daily_loss_limit_pct: float  # 일일 손실 한도 (음수, %)
    screen_top_n: int
    min_24h_value_krw: int       # 최소 24h 거래대금
    screen_interval_min: int


CONSERVATIVE = Mode(
    name="conservative", capital=50_000, reserve=50_000, max_positions=2,
    per_coin_max=25_000, first_entry_frac=0.60,
    stop_loss_pct=-7.0, take_profit_pct=10.0, take_profit_sell_frac=0.50,
    trailing_stop_pct=-5.0, daily_loss_limit_pct=-3.0,
    screen_top_n=20, min_24h_value_krw=10_000_000_000, screen_interval_min=60,
)

AGGRESSIVE = Mode(
    name="aggressive", capital=100_000, reserve=0, max_positions=2,
    per_coin_max=50_000, first_entry_frac=0.70,
    stop_loss_pct=-12.0, take_profit_pct=20.0, take_profit_sell_frac=1.0 / 3.0,
    trailing_stop_pct=-8.0, daily_loss_limit_pct=-6.0,
    screen_top_n=50, min_24h_value_krw=3_000_000_000, screen_interval_min=15,
)

MODES = {m.name: m for m in (CONSERVATIVE, AGGRESSIVE)}


def get_mode(name: str | None = None) -> Mode:
    return MODES[name or settings.MODE]


# ── 진입 사이징 ─────────────────────────────────────────────────────────
def entry_krw(mode: Mode, cash_available: float, held_count: int) -> int:
    """1차 진입에 쓸 KRW. 진입 불가면 0.

    - 이미 최대 보유 → 0
    - 계산액이 최소 주문금액 미만 → 0 (진입 스킵. 계획서 4.2)
    """
    if held_count >= mode.max_positions:
        return 0
    want = min(mode.per_coin_max * mode.first_entry_frac, cash_available)
    krw = int(want)
    if krw < settings.MIN_ORDER_KRW:
        return 0
    return krw


# ── 손절 / 추적 손절 ───────────────────────────────────────────────────
def stop_price(mode: Mode, entry_price: float, peak_price: float | None = None) -> float:
    """손절 발동가. peak_price 를 주면 추적 손절과 비교해 더 높은(유리한) 쪽."""
    hard = entry_price * (1 + mode.stop_loss_pct / 100)
    if peak_price is None:
        return hard
    trail = peak_price * (1 + mode.trailing_stop_pct / 100)
    return max(hard, trail)


def stop_order(mode: Mode, volume: float, entry_price: float,
               peak_price: float | None = None) -> Order:
    """손절 주문 (시장가). 지정가 손절 금지 — CLAUDE.md."""
    return Order("sell", "stop", volume=volume,
                 price=stop_price(mode, entry_price, peak_price), tag="stop_loss")


# ── 익절 (부분 매도) ───────────────────────────────────────────────────
def take_profit_order(mode: Mode, volume: float, entry_price: float,
                      current_price: float, already_done: bool) -> Order | None:
    """익절 조건 충족 시 부분 매도 주문. 이미 1회 했으면 None.

    부분 매도 금액이 최소 주문금액 미만이면 **전량 매도로 승격** (계획서 4.2).
    """
    if already_done:
        return None
    gain_pct = (current_price - entry_price) / entry_price * 100
    if gain_pct < mode.take_profit_pct:
        return None
    sell_vol = volume * mode.take_profit_sell_frac
    if sell_vol * current_price < settings.MIN_ORDER_KRW:
        sell_vol = volume  # 전량 승격
    return Order("sell", "market", volume=sell_vol, tag="take_profit")


# ── 일일 손실 한도 ─────────────────────────────────────────────────────
def daily_loss_exceeded(mode: Mode, day_start_equity: float, current_equity: float) -> bool:
    if day_start_equity <= 0:
        return False
    dd_pct = (current_equity - day_start_equity) / day_start_equity * 100
    return dd_pct <= mode.daily_loss_limit_pct


# ── 최소 주문금액 검증 ─────────────────────────────────────────────────
def validate_order(order: Order, current_price: float, full_volume: float | None = None):
    """(집행할 order 또는 None, 사유 문자열).

    - 매수가 최소 미만 → (None, "buy_skip")
    - 부분매도가 최소 미만 → full_volume 주면 전량매도로 승격, 아니면 (None, "sell_skip")
    - 그 외 → (order, "ok")
    """
    if order.side == "buy":
        if order.krw < settings.MIN_ORDER_KRW:
            return None, "buy_skip"
        return order, "ok"
    # sell
    notional = order.volume * current_price
    if notional >= settings.MIN_ORDER_KRW:
        return order, "ok"
    if full_volume is not None and full_volume * current_price >= settings.MIN_ORDER_KRW:
        return Order("sell", order.kind, volume=full_volume, price=order.price,
                     tag=order.tag + "+promoted"), "promoted_to_full"
    return None, "sell_skip"


# ── 실효 수수료율 감시 (쿠폰 만료 감지) ────────────────────────────────
def effective_fee_rate(trades: list[dict]) -> float:
    fee = sum(t["fee_krw"] for t in trades)
    notional = sum(t["notional_krw"] for t in trades)
    return fee / notional if notional else 0.0


def fee_coupon_suspect(trades: list[dict], min_trades: int = 5) -> bool:
    """실효 수수료율이 경보선 초과 → 쿠폰 만료 의심. 신규 진입 중단 신호."""
    if len(trades) < min_trades:
        return False
    return effective_fee_rate(trades) > settings.FEE_ALERT_ABOVE


# ── 유의종목 즉시 청산 (불변 규칙 7) ──────────────────────────────────
def must_liquidate(market: str, warning_markets: set[str]) -> bool:
    """빗썸 투자유의/경보 목록에 있으면 모드 무관 즉시 전량 시장가 청산."""
    return market in warning_markets
