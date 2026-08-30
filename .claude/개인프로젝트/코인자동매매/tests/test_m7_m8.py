"""M7(실주문 게이트) + M8(물타기 승인 플로우) 테스트 — 전부 오프라인.

    .venv\\Scripts\\python -m tests.test_m7_m8

실주문 API 는 절대 호출하지 않는다. 게이트가 막는지만 확인.
"""

from __future__ import annotations

import os
import tempfile

import numpy as np
import pandas as pd

from config import settings
from src import averaging, trader
from src.store import Position

# ntfy 실발송 방지
_sent = []
averaging.notifier.notify = lambda *a, **k: _sent.append(("notify", a, k)) or True
averaging.notifier.alert = lambda *a, **k: _sent.append(("alert", a, k)) or True


# ── M7: 실주문 게이트 ─────────────────────────────────────────────────
def test_live_disabled_by_default():
    ok, why = trader.live_enabled()
    assert ok is False and "DRY_RUN" in why
    print("  live_disabled_by_default OK")


def test_trader_init_blocked_when_dry_run():
    try:
        trader.Trader()
        assert False, "DRY_RUN=True 인데 Trader 가 생성됨"
    except RuntimeError as e:
        assert "DRY_RUN" in str(e)
    print("  trader_init_blocked OK")


def test_trader_init_blocked_without_live_flag():
    settings.DRY_RUN = False
    old = os.environ.get("LIVE_TRADING")
    os.environ["LIVE_TRADING"] = "0"
    try:
        try:
            trader.Trader()
            assert False, "LIVE_TRADING != 1 인데 생성됨"
        except RuntimeError as e:
            assert "LIVE_TRADING" in str(e)
    finally:
        settings.DRY_RUN = True
        if old is None:
            os.environ.pop("LIVE_TRADING", None)
        else:
            os.environ["LIVE_TRADING"] = old
    print("  trader_init_blocked_without_flag OK")


# ── M8: 물타기 승인 플로우 ────────────────────────────────────────────
def _iso(y=2026, mo=1, d=1):
    return f"{y:04d}-{mo:02d}-{d:02d}T00:00:00"


def _tmp_avg():
    d = tempfile.mkdtemp()
    settings.AVG_REQUEST_DIR = os.path.join(d, "req")
    settings.AVG_APPROVED_FILE = os.path.join(d, "approved.txt")
    return d


def test_should_propose_and_dedup():
    _tmp_avg()
    from src.risk import CONSERVATIVE as C          # trailing -5 → trigger -3
    p = Position("KRW-AAA", 10, 100.0, _iso(), 1000, 100.0)
    assert averaging.trigger_pct(C) == -3.0
    assert averaging.should_propose(p, 98.0, C) is False     # -2% > trigger -3%
    assert averaging.should_propose(p, 96.0, C) is True      # -4%
    averaging.propose(p, 96.0, "conservative")
    assert averaging.has_pending("KRW-AAA")
    assert averaging.should_propose(p, 90.0, C) is False     # 이미 제안됨 → 중복 안 함
    print("  should_propose_and_dedup OK")


def test_approve_and_consume():
    _tmp_avg()
    averaging.approve("KRW-BBB")
    averaging.approve("KRW-CCC")
    got = averaging.take_approved()
    assert set(got) == {"KRW-BBB", "KRW-CCC"}
    assert averaging.take_approved() == []                   # 1회성 (소비됨)
    print("  approve_and_consume OK")


class _Fake:
    def __init__(self):
        self.px = {"KRW-AAA": 100.0}

    def now(self):
        from datetime import datetime
        return datetime(2026, 1, 1, 10, 0, 0)

    def prices(self, markets):
        return {m: self.px[m] for m in markets if m in self.px}

    def top_by_volume(self, n):
        return []

    def candles(self, m, count=60):
        return None

    def fill(self, order, market):
        from src.paper import execute_against_book
        book = {"orderbook_units": [{"ask_price": self.px[market], "ask_size": 1e9,
                                     "bid_price": self.px[market] * 0.999, "bid_size": 1e9}]}
        return execute_against_book(order, book)

    def risk_events(self):
        return {"liquidate": set(), "block": set()}


def test_live_loop_averaging_after_approval():
    from src import store
    from src.live_loop import Bot
    _tmp_avg()
    fd, db = tempfile.mkstemp(suffix=".db")
    os.close(fd)
    os.remove(db)
    old_cap = settings.PAPER_CAPITAL
    settings.PAPER_CAPITAL = 100_000
    try:
        fake = _Fake()
        bot = Bot(mode="conservative", data=fake, db=db)
        store.set_cash(50_000, db)
        store.upsert_position(Position("KRW-AAA", 150.0, 100.0, _iso(), 15_000, 100.0), db)

        fake.px["KRW-AAA"] = 96.5                 # -3.5% (추적손절 -5% 전, 물타기 트리거 -3% 후)
        bot.tick()
        assert averaging.has_pending("KRW-AAA"), "제안 파일 없음"
        p = store.load_positions(db)["KRW-AAA"]
        assert abs(p.cost_krw - 15_000) < 1, "승인 전인데 물타기됨"

        averaging.approve("KRW-AAA")
        bot.tick()
        p2 = store.load_positions(db)["KRW-AAA"]
        assert p2.cost_krw > 15_000, "승인했는데 물타기 안 됨"
        assert p2.volume > 150.0
        assert not averaging.has_pending("KRW-AAA"), "제안 파일이 안 지워짐"
        print("  live_loop_averaging_after_approval OK")
    finally:
        settings.PAPER_CAPITAL = old_cap
        os.remove(db)


if __name__ == "__main__":
    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
    print("\n전부 통과")
