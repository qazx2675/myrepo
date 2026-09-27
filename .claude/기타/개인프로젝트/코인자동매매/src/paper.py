"""실시간 호가 순차소진 체결 엔진 (M5.5).

계획서 1.2 / 2.3: 빗썸엔 모의투자가 없다. 실시간 시세·호가는 실제 API 에서 받고
주문만 이 엔진이 가상 체결한다. 호가 잔량을 순차 소진하며 평균 체결가를 계산하므로
'1억으로 알트코인을 사면 슬리피지가 생긴다'가 숫자로 잡힌다.

- fills.Order 를 그대로 받는다 (백테스터와 같은 인터페이스).
- 수수료(PAPER_FEE_RATE) + 지연(PAPER_LATENCY_MS) 반드시 반영. **끄지 말 것** (CLAUDE.md 4).
- 호가 깊이가 모자라면 부분 체결 (filled_fully=False).
"""

from __future__ import annotations

import time
from dataclasses import dataclass

from config import settings
from src.fills import Order


@dataclass
class PaperFill:
    price: float          # 가중평균 체결 단가
    volume: float         # 체결 수량
    fee: float            # 수수료 (KRW)
    notional: float       # 체결 명목금액 = price * volume
    filled_fully: bool    # 요청 전량 체결됐는지
    levels_used: int      # 소진한 호가 레벨 수


def execute_against_book(order: Order, book: dict, fee_rate: float | None = None) -> PaperFill | None:
    """호가 스냅샷 1개에 대해 체결. 미체결이면 None.

    book: {'orderbook_units': [{ask_price, ask_size, bid_price, bid_size}, ...]}  (best-first)
    """
    fee_rate = settings.PAPER_FEE_RATE if fee_rate is None else fee_rate
    units = book.get("orderbook_units") or []
    if not units:
        return None
    is_limit = order.kind == "limit"

    if order.side == "buy":
        budget = order.krw / (1 + fee_rate)          # 명목 예산 (수수료 별도)
        vol = spent = 0.0
        used = 0
        for u in units:
            px, sz = float(u["ask_price"]), float(u["ask_size"])
            if is_limit and px > order.price:
                break
            remain = budget - spent
            if remain <= 1e-9:
                break
            take_notional = min(remain, px * sz)
            vol += take_notional / px
            spent += take_notional
            used += 1
        if vol <= 0:
            return None
        return PaperFill(spent / vol, vol, spent * fee_rate, spent,
                         filled_fully=spent >= budget - 1e-6, levels_used=used)

    # sell (market / limit / stop-after-trigger)
    want = order.volume
    vol = proceeds = 0.0
    used = 0
    for u in units:
        px, sz = float(u["bid_price"]), float(u["bid_size"])
        if is_limit and px < order.price:
            break
        remain = want - vol
        if remain <= 1e-12:
            break
        take = min(remain, sz)
        vol += take
        proceeds += take * px
        used += 1
    if vol <= 0:
        return None
    return PaperFill(proceeds / vol, vol, proceeds * fee_rate, proceeds,
                     filled_fully=vol >= want - 1e-12, levels_used=used)


def execute_live(order: Order, market: str, client=None,
                 latency_ms: int | None = None) -> PaperFill | None:
    """신호~주문 지연을 반영해 latency 만큼 기다린 뒤 '그때의' 호가로 체결한다."""
    from src.bithumb_client import BithumbClient
    latency_ms = settings.PAPER_LATENCY_MS if latency_ms is None else latency_ms
    if latency_ms > 0:
        time.sleep(latency_ms / 1000)
    book = (client or BithumbClient).orderbook(market)
    if isinstance(book, list):
        book = book[0]
    return execute_against_book(order, book)
