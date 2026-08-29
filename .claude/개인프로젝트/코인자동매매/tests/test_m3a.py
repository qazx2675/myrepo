"""M3-a 단위 테스트 (체결 모델 + 백테스터).

    .venv\\Scripts\\python -m tests.test_m3a

pytest 없이 assert 로 돌린다 (test_m1.py 와 동일 방식).
가장 신경 쓴 부분: fills.execute 의 지정가/손절/갭 처리.
"""

from __future__ import annotations

from dataclasses import dataclass

import pandas as pd

from src import fills, metrics
from src.backtest import run
from src.fills import Order

FEE = 0.0004
SLIP = 5  # bps


@dataclass
class Bar:
    open: float
    high: float
    low: float
    close: float
    volume: float = 1.0
    value: float = 1.0


def approx(a, b, tol=1e-6):
    return abs(a - b) <= tol * max(1.0, abs(b))


def test_market_buy():
    bar = Bar(open=100, high=110, low=95, close=105)
    f = fills.execute(Order("buy", "market", krw=1_000_000), bar, FEE, SLIP)
    assert approx(f.price, 100 * 1.0005)                 # 편도 슬리피지
    assert approx(f.notional + f.fee, 1_000_000)         # 총 투입 = 명목 + 수수료
    assert approx(f.fee, f.notional * FEE)
    assert approx(f.volume, f.notional / f.price)
    print("  market_buy OK")


def test_limit_buy_fill_and_nofill():
    # low(95) <= price(97) -> 체결, 체결가 min(97, open=100) = 97
    f = fills.execute(Order("buy", "limit", krw=500_000, price=97), Bar(100, 110, 95, 105), FEE, SLIP)
    assert f is not None and approx(f.price, 97)
    # low(95) > price(90) -> 미체결
    assert fills.execute(Order("buy", "limit", krw=500_000, price=90), Bar(100, 110, 95, 105), FEE, SLIP) is None
    # open(88) < price(97) -> 유리하게 open 에 체결
    f2 = fills.execute(Order("buy", "limit", krw=500_000, price=97), Bar(88, 99, 85, 90), FEE, SLIP)
    assert approx(f2.price, 88)
    print("  limit_buy OK")


def test_market_sell():
    f = fills.execute(Order("sell", "market", volume=2.0), Bar(100, 110, 95, 105), FEE, SLIP)
    assert approx(f.price, 100 * 0.9995)
    assert approx(f.notional, 2.0 * f.price)
    assert approx(f.fee, f.notional * FEE)
    print("  market_sell OK")


def test_stop():
    # low(96) > stop(95) -> 미발동
    assert fills.execute(Order("sell", "stop", volume=1.0, price=95), Bar(100, 105, 96, 101), FEE, SLIP) is None
    # low(90) <= stop(95), open(100) > stop -> stop 가에 체결 후 슬리피지
    f = fills.execute(Order("sell", "stop", volume=1.0, price=95), Bar(100, 102, 90, 92), FEE, SLIP)
    assert approx(f.price, 95 * 0.9995)
    # 갭다운: open(80) < stop(95) -> open 에 체결 (더 불리)
    f2 = fills.execute(Order("sell", "stop", volume=1.0, price=95), Bar(80, 82, 70, 75), FEE, SLIP)
    assert approx(f2.price, 80 * 0.9995)
    print("  stop OK")


def _df(prices):
    idx = pd.date_range("2024-01-01", periods=len(prices), freq="D")
    return pd.DataFrame({"open": prices, "high": [p * 1.01 for p in prices],
                         "low": [p * 0.99 for p in prices], "close": prices,
                         "volume": 1.0, "value": 1.0}, index=idx)


def test_execution_delay():
    """신호는 다음 봉 시가에 체결된다 (delay_bars=1)."""
    df = _df([100, 100, 200, 200, 200])
    calls = []

    def strat(history, position):
        calls.append(len(history))
        if len(history) == 2 and position is None:      # 2번째 봉 종가에 매수 신호
            return Order("buy", "market", krw=100_000)
        return None

    res = run(df, strat, initial_cash=100_000, fee_rate=0, slippage_bps=0, delay_bars=1)
    # 3번째 봉(index2) 시가 200 에 체결. 이후 강제청산도 200 -> 손익 0 근처
    assert res.trades[0]["entry_price"] == 200
    print("  execution_delay OK")


def test_round_trip_pnl():
    df = _df([100, 100, 100, 150, 150])

    def strat(history, position):
        if len(history) == 1 and position is None:
            return Order("buy", "market", krw=100_000)
        if len(history) == 4 and position is not None:
            return Order("sell", "market", volume=position.volume)
        return None

    res = run(df, strat, initial_cash=100_000, fee_rate=0, slippage_bps=0, delay_bars=1)
    t = res.trades[0]
    assert approx(t["entry_price"], 100) and approx(t["exit_price"], 150)
    assert approx(t["pnl_pct"], 50.0, tol=1e-3)
    assert not res.unclosed
    print("  round_trip_pnl OK")


def test_mdd():
    eq = pd.Series([100, 120, 90, 110, 60, 80],
                   index=pd.date_range("2024-01-01", periods=6, freq="D"))
    m = metrics.compute(eq, trades=[], bars_in_market=3)
    assert approx(m.mdd_pct, -50.0, tol=1e-3)            # 120 -> 60
    print("  mdd OK")


if __name__ == "__main__":
    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
    print("\n전부 통과")
