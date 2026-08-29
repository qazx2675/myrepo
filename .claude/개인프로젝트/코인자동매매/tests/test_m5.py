"""M5 테스트 — 공지 감시 / 뉴스 필터 / 알림 (전부 오프라인).

    .venv\\Scripts\\python -m tests.test_m5

실제 API 호출 확인은  .venv\\Scripts\\python -m src.run_m5
"""

from __future__ import annotations

import numpy as np
import pandas as pd

from src import news, notice, notifier
from src import portfolio_backtest as pf
from src.news import Headline
from src.risk import CONSERVATIVE

NOTICES = [
    {"categories": ["거래유의"], "title": "아이콘(ICX) 거래유의종목 지정"},
    {"categories": ["거래지원종료"], "title": "신세틱스(SNX) 거래지원 종료"},
    {"categories": ["입출금"], "title": "썬더코어(TT) 출금 중지 안내"},
    {"categories": ["마켓 추가"], "title": "인터폴드(FOLD), 이유알코인(EURC) 원화 마켓 추가"},
    {"categories": ["안내"], "title": "빗썸 API 수수료 무료 이벤트"},
]
WARNINGS = [
    {"market": "KRW-ZIL", "warning_type": "PRICE_SUDDEN_FLUCTUATION", "warning_step": "DANGER"},
    {"market": "KRW-ICX", "warning_type": "TRADING_VOLUME_SUDDEN_FLUCTUATION", "warning_step": "WARNING"},
    {"market": "KRW-ELF", "warning_type": "PRICE_DIFFERENCE_HIGH", "warning_step": "CAUTION"},
]


def test_ticker_extraction():
    assert notice.tickers_in("아이콘(ICX) 거래유의종목 지정") == {"KRW-ICX"}
    assert notice.tickers_in("인터폴드(FOLD), 이유알코인(EURC) 원화 마켓 추가") == {"KRW-FOLD", "KRW-EURC"}
    assert notice.tickers_in("수수료 무료 이벤트") == set()
    print("  ticker_extraction OK")


def test_notice_liquidation():
    assert notice.liquidation_markets(NOTICES) == {"KRW-ICX", "KRW-SNX"}
    print("  notice_liquidation OK")


def test_notice_block():
    b = notice.entry_blocked_markets(NOTICES, WARNINGS)
    assert "KRW-TT" in b and "KRW-FOLD" in b and "KRW-EURC" in b   # 공지
    assert "KRW-ZIL" in b and "KRW-ICX" in b                       # DANGER / WARNING
    assert "KRW-ELF" not in b                                      # CAUTION 은 통과
    print("  notice_block OK")


def test_news_scan():
    hs = [
        Headline("Solana network suffers major exploit, $50M stolen", "", ""),
        Headline("SEC opens investigation into Ripple", "", ""),
        Headline("Bitcoin ETF sees record inflows", "", ""),
    ]
    v = news.scan(hs)
    by = {x.market: x.level for x in v}
    assert by.get("KRW-SOL") == "liquidate"
    assert by.get("KRW-XRP") == "block"
    assert "KRW-BTC" not in by                     # 위험 키워드 없음
    print("  news_scan OK")


def test_notifier_no_topic(monkeypatch=None):
    import os
    old = os.environ.pop("NTFY_TOPIC", None)
    try:
        # .env 에 토픽이 있을 수 있으니 강제로 빈 값
        os.environ["NTFY_TOPIC"] = ""
        assert notifier.notify("test") is False       # 미설정 → 조용히 False
    finally:
        if old is not None:
            os.environ["NTFY_TOPIC"] = old
    print("  notifier_no_topic OK")


def _series(prices):
    idx = pd.date_range("2024-01-01", periods=len(prices), freq="D")
    p = np.array(prices, float)
    return pd.DataFrame({"open": p, "high": p * 1.02, "low": p * 0.98, "close": p,
                         "volume": 1.0, "value": 1e12}, index=idx)


def test_liquidate_provider_forces_exit():
    rise = list(np.linspace(100, 100, 40)) + list(np.linspace(100, 200, 60))
    panel = {"KRW-A": _series(rise), "KRW-B": _series([100] * 100)}
    # 60번째 봉부터 KRW-A 를 유의종목으로 지정
    def prov(date, held):
        return {"KRW-A"} if date >= panel["KRW-A"].index[60] else set()
    r = pf.run(panel, CONSERVATIVE, initial_cash=50_000,
               use_pattern=False, bearish_veto=False, liquidate_provider=prov)
    assert any(t["exit_kind"] == "warning" for t in r.trades), [t["exit_kind"] for t in r.trades]
    print("  liquidate_provider_forces_exit OK")


if __name__ == "__main__":
    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
    print("\n전부 통과")
