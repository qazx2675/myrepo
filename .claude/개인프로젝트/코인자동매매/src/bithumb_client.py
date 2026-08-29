"""빗썸 API 연동 (M1).

- 공개 조회: 시세 / 캔들 / 호가 / 마켓목록 / 투자경보
- 인증 조회: 잔고 / 주문가능정보
- probe_readonly(): 발급한 키가 정말 '조회 전용'인지 확인 (주문 API 가 거부되는지)

인증(JWT HS256)은 python_bithumb 가 처리한다. 이 모듈은 얇은 래퍼일 뿐이다.
"""

from __future__ import annotations

import logging
import os
import uuid

import python_bithumb as pb
from dotenv import load_dotenv

log = logging.getLogger("bithumb")

_FAKE_UUID = "00000000-0000-0000-0000-000000000000"


def _mask(s: str) -> str:
    if not s:
        return "(빈값)"
    return s[:4] + "…" + s[-2:] if len(s) > 8 else "…"


def load_keys() -> tuple[str, str]:
    """.env 에서 빗썸 키를 읽는다. 없으면 즉시 에러."""
    load_dotenv()
    ak = os.getenv("BITHUMB_ACCESS_KEY", "").strip()
    sk = os.getenv("BITHUMB_SECRET_KEY", "").strip()
    if not ak or not sk:
        raise RuntimeError(
            "BITHUMB_ACCESS_KEY / BITHUMB_SECRET_KEY 가 .env 에 없습니다. "
            ".env.example 을 .env 로 복사한 뒤 채우세요."
        )
    log.info("빗썸 키 로드: access=%s", _mask(ak))
    return ak, sk


class BithumbClient:
    def __init__(self, access_key: str | None = None, secret_key: str | None = None):
        if access_key is None or secret_key is None:
            access_key, secret_key = load_keys()
        self._priv = pb.Bithumb(access_key, secret_key)

    # ── 공개 조회 ────────────────────────────────────────────────────────
    @staticmethod
    def current_price(market: str) -> float:
        return pb.get_current_price(market)

    @staticmethod
    def ohlcv(market: str, interval: str = "day", count: int = 200):
        return pb.get_ohlcv(market, interval=interval, count=count)

    @staticmethod
    def orderbook(market: str):
        return pb.get_orderbook(market)

    @staticmethod
    def market_all():
        return pb.get_market_all()

    @staticmethod
    def krw_markets() -> list[str]:
        return [m["market"] for m in pb.get_market_all() if m["market"].startswith("KRW-")]

    @staticmethod
    def tickers(markets: list[str]) -> dict[str, dict]:
        """마켓별 ticker (acc_trade_price_24h 등). 한 번에 조회."""
        import requests
        out: dict[str, dict] = {}
        for i in range(0, len(markets), 100):
            chunk = markets[i:i + 100]
            r = requests.get("https://api.bithumb.com/v1/ticker",
                             params={"markets": ",".join(chunk)}, timeout=10)
            r.raise_for_status()
            for t in r.json():
                out[t["market"]] = t
        return out

    @staticmethod
    def virtual_asset_warning():
        """투자경보/유의 마켓 목록. 계획서 8장 — 유의종목 지정 시 즉시 청산 근거."""
        return pb.get_virtual_asset_warning()

    # ── 인증 조회 ────────────────────────────────────────────────────────
    def balances(self):
        return self._priv.get_balances()

    def krw_balance(self) -> float:
        return self._priv.get_balance("KRW")

    def order_chance(self, market: str):
        return self._priv.get_order_chance(market)

    # ── 키 권한 검증 ─────────────────────────────────────────────────────
    def probe_readonly(self) -> dict:
        """존재하지 않는 주문을 취소 시도한다 (아무것도 바꾸지 않음).

        - 성공  -> 있을 수 없음. 키에 주문권한 있음 -> FAIL
        - 'out_of_scope' / 401·403 권한 오류 -> 조회 전용 확인 -> OK
        - 'order_not_found' 류 -> 키에 주문권한 있음 -> FAIL
        - 그 외 -> 사람 판단 필요 (INCONCLUSIVE)
        """
        try:
            self._priv.cancel_order(_FAKE_UUID)
            return {"result": "FAIL", "detail": "취소 요청이 통과함 — 키에 주문 권한이 있습니다."}
        except pb.BithumbAPIException as e:
            name = (e.error_msg or "").lower()
            code = e.status_code
            if "out_of_scope" in name or code in (401, 403):
                return {"result": "OK", "detail": f"주문 API 거부됨 (HTTP {code}: {e.error_msg}). 조회 전용 키 확인."}
            if "not_found" in name or "order_not_found" in name or code == 404:
                return {"result": "FAIL", "detail": f"주문 조회는 통과함 ({e.error_msg}) — 키에 주문 권한이 있습니다."}
            return {"result": "INCONCLUSIVE", "detail": f"HTTP {code}: {e.error_msg}"}
