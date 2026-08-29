"""M6 테스트 — store 왕복 + live_loop 한 틱 (전부 오프라인, 가짜 데이터).

    .venv\\Scripts\\python -m tests.test_m6
"""

from __future__ import annotations

import os
import tempfile
from datetime import datetime

import numpy as np
import pandas as pd

from src import store
from src.live_loop import Bot
from src.paper import execute_against_book


def _tmpdb():
    fd, path = tempfile.mkstemp(suffix=".db")
    os.close(fd)
    os.remove(path)
    return path


def test_store_roundtrip():
    db = _tmpdb()
    store.init(db)
    store.set_cash(1_000_000, db)
    p = store.Position("KRW-BTC", 0.01, 100_000_000, "2026-01-01T00:00:00", 1_000_000, 100_000_000)
    store.upsert_position(p, db)
    got = store.load_positions(db)["KRW-BTC"]
    assert got.volume == 0.01 and got.cost_krw == 1_000_000
    store.record_trade({"market": "KRW-BTC", "entry_ts": "a", "exit_ts": "b",
                        "entry_price": 1, "exit_price": 2, "volume": 1, "pnl_krw": 100,
                        "pnl_pct": 10, "fee_krw": 1, "notional_krw": 200, "exit_kind": "stop"}, db)
    assert len(store.all_trades(db)) == 1
    store.delete_position("KRW-BTC", db)
    assert store.load_positions(db) == {}
    os.remove(db)
    print("  store_roundtrip OK")


# ── 가짜 데이터 소스 ──────────────────────────────────────────────────
class FakeData:
    def __init__(self):
        self.px = {"KRW-AAA": 100.0, "KRW-BBB": 100.0, "KRW-CCC": 100.0}
        self.t = datetime(2026, 1, 1, 10, 0, 0)
        self.events = {"liquidate": set(), "block": set()}
        # AAA 강한 상승 추세 → 스크리너 통과시키기
        self._hist = {m: self._series(m) for m in self.px}

    def _series(self, m):
        n = 60
        if m == "KRW-AAA":
            p = np.linspace(60, 100, n)
        else:
            p = np.full(n, 100.0)
        idx = pd.date_range("2025-11-01", periods=n, freq="D")
        return pd.DataFrame({"open": p, "high": p * 1.01, "low": p * 0.99, "close": p,
                             "volume": 1.0, "value": 5e11}, index=idx)

    def now(self):
        return self.t

    def prices(self, markets):
        return {m: self.px[m] for m in markets if m in self.px}

    def top_by_volume(self, n):
        return list(self.px)[:n]

    def candles(self, market, count=60):
        return self._hist.get(market)

    def fill(self, order, market):
        book = {"orderbook_units": [
            {"ask_price": self.px[market], "ask_size": 1e9,
             "bid_price": self.px[market] * 0.999, "bid_size": 1e9}]}
        return execute_against_book(order, book)

    def risk_events(self):
        # 실제 poll() 처럼 청산 대상은 진입금지에도 포함
        return {"liquidate": set(self.events["liquidate"]),
                "block": self.events["block"] | self.events["liquidate"]}


def test_tick_enters_then_stops_out():
    db = _tmpdb()
    fake = FakeData()
    from config import settings
    old_cap = settings.PAPER_CAPITAL
    settings.PAPER_CAPITAL = 100_000
    try:
        bot = Bot(mode="conservative", data=fake, db=db)
        bot.tick()                                  # 진입 (AAA)
        pos = store.load_positions(db)
        assert "KRW-AAA" in pos, pos
        entry_cash = store.get_cash(db)
        assert entry_cash < 100_000                 # 현금 줄었다

        # AAA -20% 폭락 → 다음 틱에 손절
        fake.px["KRW-AAA"] = 80.0
        fake.t = datetime(2026, 1, 1, 11, 0, 0)
        bot.tick()
        assert "KRW-AAA" not in store.load_positions(db)
        trades = store.all_trades(db)
        assert trades and trades[-1]["exit_kind"] == "stop"
        assert trades[-1]["pnl_krw"] < 0
        print("  tick_enters_then_stops_out OK")
    finally:
        settings.PAPER_CAPITAL = old_cap
        os.remove(db)


def test_tick_liquidates_on_warning():
    db = _tmpdb()
    fake = FakeData()
    from config import settings
    old = settings.PAPER_CAPITAL
    settings.PAPER_CAPITAL = 100_000
    try:
        bot = Bot(mode="conservative", data=fake, db=db)
        bot.tick()
        assert "KRW-AAA" in store.load_positions(db)
        fake.events = {"liquidate": {"KRW-AAA"}, "block": set()}
        fake.t = datetime(2026, 1, 1, 12, 0, 0)
        bot.tick()
        assert "KRW-AAA" not in store.load_positions(db)
        assert store.all_trades(db)[-1]["exit_kind"] == "warning"
        print("  tick_liquidates_on_warning OK")
    finally:
        settings.PAPER_CAPITAL = old
        os.remove(db)


def test_restart_restores_positions():
    db = _tmpdb()
    fake = FakeData()
    from config import settings
    old = settings.PAPER_CAPITAL
    settings.PAPER_CAPITAL = 100_000
    try:
        Bot(mode="conservative", data=fake, db=db).tick()
        held_before = set(store.load_positions(db))
        cash_before = store.get_cash(db)
        # 새 Bot 인스턴스 = 재시작
        bot2 = Bot(mode="conservative", data=fake, db=db)
        assert set(store.load_positions(db)) == held_before
        assert store.get_cash(db) == cash_before
        print("  restart_restores_positions OK")
    finally:
        settings.PAPER_CAPITAL = old
        os.remove(db)


if __name__ == "__main__":
    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
    print("\n전부 통과")
