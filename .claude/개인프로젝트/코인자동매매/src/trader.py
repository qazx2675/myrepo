"""실주문 실행 (M7) — 빗썸에 진짜 주문을 낸다.

⚠️ 이 모듈이 실제로 주문을 내려면 **3중 게이트를 전부** 통과해야 한다:
   1. config.settings.DRY_RUN == False
   2. .env 의 LIVE_TRADING=1
   3. 발급된 API 키에 주문 권한이 있음 (get_order_chance 성공)

하나라도 아니면 RuntimeError. 기본 상태(DRY_RUN=True)에서는 절대 주문이 안 나간다.
M6-b 페이퍼 검증을 통과하고, 사용자가 명시적으로 전환할 때만 쓴다 (불변 규칙 4).

체결 결과는 paper.PaperFill 과 같은 필드로 돌려줘서 live_loop 이 갈아끼울 수 있게 한다.
"""

from __future__ import annotations

import logging
import os
import time

from dotenv import load_dotenv

from config import settings
from src.fills import Order
from src.paper import PaperFill

log = logging.getLogger("trader")


def live_enabled() -> tuple[bool, str]:
    """(가능여부, 사유). live_loop 이 시작 시 호출해 상태를 로그로 남긴다."""
    if settings.DRY_RUN:
        return False, "DRY_RUN=True (기본값)"
    load_dotenv()
    if os.getenv("LIVE_TRADING", "").strip() != "1":
        return False, ".env LIVE_TRADING != 1"
    return True, "실주문 활성"


class Trader:
    def __init__(self, client=None):
        ok, why = live_enabled()
        if not ok:
            raise RuntimeError(f"실주문 비활성: {why}. paper 엔진을 쓰세요.")
        from src.bithumb_client import BithumbClient
        self._priv = (client or BithumbClient)()  # 인스턴스 필요 (인증 조회)
        # 키에 주문 권한이 있는지 확인 (없으면 여기서 실패)
        self._priv.order_chance("KRW-BTC")
        log.warning("★ 실주문 모드 활성 — 진짜 돈이 움직입니다 ★")

    def execute(self, order: Order, market: str) -> PaperFill | None:
        """시장가 주문만 지원 (전략이 시장가만 씀). 체결 후 조회해서 평균가 확인."""
        if order.kind != "market":
            raise ValueError("실주문은 시장가만 (지정가 손절 금지 — CLAUDE.md)")
        log.warning("실주문 %s %s %s", order.side, market,
                    f"{order.krw:,.0f}원" if order.side == "buy" else f"{order.volume}개")
        if order.side == "buy":
            res = self._priv._priv.buy_market_order(market, order.krw)
        else:
            res = self._priv._priv.sell_market_order(market, order.volume)

        uuid = res.get("uuid")
        if not uuid:
            log.error("주문 응답에 uuid 없음: %s", res)
            return None
        time.sleep(1.0)                       # 체결 반영 대기
        info = self._priv.get_order(uuid) if hasattr(self._priv, "get_order") else res
        return _to_fill(order, info)


def _to_fill(order: Order, info: dict) -> PaperFill | None:
    """빗썸 주문 조회 결과 → PaperFill. 필드명은 빗썸 2.0 기준, 없으면 보수적으로."""
    try:
        vol = float(info.get("executed_volume") or info.get("volume") or 0)
        paid_fee = float(info.get("paid_fee") or 0)
        # 체결 총액: trades 합 or price*vol
        funds = info.get("executed_funds") or info.get("trades_funds")
        notional = float(funds) if funds else vol * float(info.get("price") or 0)
        if vol <= 0 or notional <= 0:
            return None
        return PaperFill(notional / vol, vol, paid_fee, notional, filled_fully=True, levels_used=0)
    except (TypeError, ValueError) as e:
        log.error("체결 파싱 실패: %s (%s)", e, info)
        return None
