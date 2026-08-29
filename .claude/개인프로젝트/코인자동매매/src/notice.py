"""빗썸 공지 / 투자경보 감시 (M5) — 암호화폐판 DART.

계획서 8장:
  거래유의종목 지정 / 거래지원 종료  → 모드 무관 즉시 전량 시장가 청산 (불변 규칙 7)
  입출금 중단 / 신규 상장            → 신규 진입 금지
  virtual_asset_warning (WARNING↑)  → 신규 진입 금지 (청산까지는 아님. 단기 경보라 end_date 있음)

결정론적 규칙이다. LLM 판단 아님.
"""

from __future__ import annotations

import logging
import re

import requests

from config import settings

log = logging.getLogger("notice")

_TICKER = re.compile(r"\(([A-Z0-9]{2,15})\)")


def _to_market(ticker: str) -> str:
    return ticker if ticker.startswith("KRW-") else f"KRW-{ticker}"


def tickers_in(title: str) -> set[str]:
    """공지 제목의 (ABC) 들을 KRW-ABC 로. '아이콘(ICX) 거래유의종목 지정' → {'KRW-ICX'}."""
    return {_to_market(m) for m in _TICKER.findall(title)}


def fetch_notices(count: int | None = None) -> list[dict]:
    count = settings.NOTICE_POLL_COUNT if count is None else count
    r = requests.get(settings.NOTICE_URL, params={"count": count}, timeout=10)
    r.raise_for_status()
    data = r.json()
    return data if isinstance(data, list) else data.get("data", [])


def fetch_warnings() -> list[dict]:
    r = requests.get(settings.WARNING_URL, timeout=10)
    r.raise_for_status()
    data = r.json()
    return data if isinstance(data, list) else data.get("data", [])


def _cat_hit(notice: dict, cats: tuple[str, ...]) -> bool:
    got = notice.get("categories") or []
    if isinstance(got, str):
        got = [got]
    return any(c in g for g in got for c in cats)


def liquidation_markets(notices: list[dict] | None = None) -> set[str]:
    """거래유의 / 거래지원종료 공지에 걸린 마켓 → 즉시 청산 대상."""
    notices = fetch_notices() if notices is None else notices
    out: set[str] = set()
    for n in notices:
        if _cat_hit(n, settings.NOTICE_LIQUIDATE_CATS):
            out |= tickers_in(n.get("title", ""))
    return out


def entry_blocked_markets(notices: list[dict] | None = None,
                          warnings: list[dict] | None = None) -> set[str]:
    """입출금중단 / 신규상장 공지 + WARNING 이상 투자경보 → 신규 진입 금지."""
    notices = fetch_notices() if notices is None else notices
    warnings = fetch_warnings() if warnings is None else warnings
    out: set[str] = set()
    for n in notices:
        if _cat_hit(n, settings.NOTICE_BLOCK_CATS):
            out |= tickers_in(n.get("title", ""))
    for w in warnings:
        if w.get("warning_step") in settings.WARNING_BLOCK_STEPS:
            out.add(_to_market(w.get("market", "")))
    out.discard("KRW-")
    return out


def poll() -> dict:
    """한 번 조사. 실패하면 빈 결과 (매매를 막지 않는다)."""
    try:
        notices = fetch_notices()
        warnings = fetch_warnings()
    except Exception as e:                        # noqa: BLE001
        log.error("공지/경보 조회 실패 (%s): %s", type(e).__name__, e)
        return {"liquidate": set(), "block": set(), "ok": False}
    return {
        "liquidate": liquidation_markets(notices),
        "block": entry_blocked_markets(notices, warnings),
        "ok": True,
    }
