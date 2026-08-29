"""코인 뉴스 RSS 키워드 필터 (M5).

계획서 13.1: LLM 호출 없음. 결정론적 키워드 매칭.
  청산 키워드(해킹/상장폐지…) + 코인 언급 → 해당 코인 보유 시 즉시 청산
  보류 키워드(소송/규제…)  + 코인 언급 → 해당 코인 신규 진입 24h 금지
오탐이 나도 손해는 기회비용뿐이다 (틀린 회피 < 틀린 진입).
"""

from __future__ import annotations

import logging
import re
import xml.etree.ElementTree as ET
from dataclasses import dataclass

import requests

from config import settings

log = logging.getLogger("news")

# 유니버스 코인 별칭 (소문자 비교).
#   strong = 오탐이 적은 정식 명칭. 청산(파괴적 행동) 판정엔 strong + 제목 매칭만 쓴다.
#   weak   = 티커 등 짧은 별칭. 진입보류(회피) 판정엔 같이 쓴다.
STRONG_ALIASES: dict[str, tuple[str, ...]] = {
    "KRW-BTC": ("bitcoin", "비트코인"),
    "KRW-ETH": ("ethereum", "이더리움"),
    "KRW-XRP": ("ripple", "리플", "xrp"),
    "KRW-SOL": ("solana", "솔라나"),
    "KRW-DOGE": ("dogecoin", "도지코인"),
    "KRW-ADA": ("cardano", "카르다노"),
    "KRW-DOT": ("polkadot", "폴카닷"),
    "KRW-LINK": ("chainlink", "체인링크"),
}
WEAK_ALIASES: dict[str, tuple[str, ...]] = {
    "KRW-BTC": ("btc",), "KRW-ETH": ("eth", "이더"), "KRW-XRP": (),
    "KRW-SOL": ("sol",), "KRW-DOGE": ("doge", "도지"), "KRW-ADA": ("ada", "에이다"),
    "KRW-DOT": ("dot",), "KRW-LINK": ("link",),
}
ALIASES = {m: STRONG_ALIASES[m] + WEAK_ALIASES.get(m, ()) for m in STRONG_ALIASES}


@dataclass
class Headline:
    title: str
    summary: str
    link: str


@dataclass
class Verdict:
    market: str
    level: str          # "liquidate" | "block"
    keyword: str
    title: str


def fetch_headlines(feeds: tuple[str, ...] | None = None) -> list[Headline]:
    feeds = settings.NEWS_RSS_FEEDS if feeds is None else feeds
    out: list[Headline] = []
    for url in feeds:
        try:
            r = requests.get(url, timeout=settings.NEWS_TIMEOUT,
                             headers={"User-Agent": "Mozilla/5.0"})
            r.raise_for_status()
            root = ET.fromstring(r.content)
            for item in root.iter("item"):
                t = (item.findtext("title") or "").strip()
                d = (item.findtext("description") or "").strip()
                link = (item.findtext("link") or "").strip()
                if t:
                    out.append(Headline(t, d, link))
        except Exception as e:                    # noqa: BLE001 — 뉴스 실패가 매매를 막지 않음
            log.error("RSS 실패 %s (%s)", url, type(e).__name__)
    return out


def _first_keyword(text: str, keywords) -> str | None:
    low = text.lower()
    for kw in keywords:
        if kw.lower() in low:
            return kw
    return None


_ASCII = re.compile(r"^[a-z0-9]+$")


def _alias_in(alias: str, text_low: str) -> bool:
    """짧은 영문 별칭(btc, ada…)은 단어 경계로 매칭 (Canada 의 'ada' 오탐 방지).
    한글/긴 별칭은 단순 부분매칭."""
    a = alias.lower()
    if _ASCII.match(a) and len(a) <= 4:
        return re.search(rf"\b{re.escape(a)}\b", text_low) is not None
    return a in text_low


def scan(headlines: list[Headline] | None = None,
         markets: tuple[str, ...] | None = None) -> list[Verdict]:
    """코인별 청산/보류 판정.

    청산(파괴적) : **제목**에 정식명칭(strong alias) + 청산 키워드가 함께.
    보류(회피)   : 제목+요약에 아무 별칭 + (청산/보류) 키워드가 함께.
    같은 코인에 둘 다 걸리면 청산 우선.
    """
    headlines = fetch_headlines() if headlines is None else headlines
    markets = tuple(STRONG_ALIASES) if markets is None else markets
    hit: dict[str, Verdict] = {}
    for h in headlines:
        title_low = h.title.lower()
        full_low = f"{h.title} {h.summary}".lower()
        lk_title = _first_keyword(h.title, settings.NEWS_LIQUIDATE_KEYWORDS)
        any_kw = _first_keyword(full_low, settings.NEWS_LIQUIDATE_KEYWORDS) \
            or _first_keyword(full_low, settings.NEWS_BLOCK_KEYWORDS)
        for m in markets:
            strong_in_title = any(_alias_in(a, title_low) for a in STRONG_ALIASES.get(m, ()))
            any_alias = any(_alias_in(a, full_low) for a in ALIASES.get(m, ()))
            if lk_title and strong_in_title:
                hit[m] = Verdict(m, "liquidate", lk_title, h.title)
            elif any_kw and any_alias and m not in hit:
                hit[m] = Verdict(m, "block", any_kw, h.title)
    return list(hit.values())


def poll(markets: tuple[str, ...] | None = None) -> dict:
    """(liquidate set, block set). 실패하면 빈 결과."""
    v = scan(markets=markets)
    liq = {x.market for x in v if x.level == "liquidate"}
    return {
        "liquidate": liq,
        "block": {x.market for x in v if x.level == "block"} | liq,  # 청산 대상은 재진입도 금지
        "verdicts": v,
    }
