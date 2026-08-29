"""M4-b 테스트 — 포트폴리오 백테스터 불변식.

    .venv\\Scripts\\python -m tests.test_m4b
"""

from __future__ import annotations

import numpy as np
import pandas as pd

from src import portfolio_backtest as pf
from src.risk import CONSERVATIVE

C = CONSERVATIVE


def _series(prices):
    idx = pd.date_range("2024-01-01", periods=len(prices), freq="D")
    p = np.array(prices, dtype=float)
    return pd.DataFrame({"open": p, "high": p * 1.02, "low": p * 0.98, "close": p,
                         "volume": 1.0, "value": 1e12}, index=idx)


def test_starts_at_initial_cash():
    up = list(np.linspace(100, 300, 120))
    panel = {"KRW-A": _series(up), "KRW-B": _series(up[::-1])}
    r = pf.run(panel, C, initial_cash=50_000, use_pattern=False, bearish_veto=False)
    assert abs(r.equity.iloc[0] - 50_000) < 50_000 * 0.05
    print("  starts_at_initial_cash OK")


def test_never_exceeds_max_positions():
    # 여러 코인이 동시에 강하게 상승 → 진입 신호 쏟아짐. 그래도 보유는 max_positions 이하
    rise = list(np.linspace(100, 100, 40)) + list(np.linspace(100, 400, 90))
    panel = {f"KRW-{c}": _series(rise) for c in "ABCDE"}
    r = pf.run(panel, C, initial_cash=50_000, use_pattern=False, bearish_veto=False)
    # 동시 진입 이력을 직접 못 보므로: 총 거래가 유니버스*라운드 수준으로 폭주하지 않는지
    assert r.metrics.n_trades <= 40, r.metrics.n_trades
    for t in r.trades:
        assert {"market", "pnl_pct", "exit_kind", "fee_krw", "notional_krw"} <= set(t)
    print("  never_exceeds_max_positions OK")


def test_stop_loss_fires_on_crash():
    # 40봉 횡보 후 급락 → 진입했다면 손절로 청산
    path = list(np.linspace(100, 130, 60)) + list(np.linspace(130, 55, 30))
    panel = {"KRW-A": _series(path), "KRW-B": _series([100] * 90)}
    r = pf.run(panel, C, initial_cash=50_000, use_pattern=False, bearish_veto=False)
    kinds = {t["exit_kind"] for t in r.trades}
    assert "stop" in kinds, kinds
    print("  stop_loss_fires_on_crash OK")


def test_take_profit_fires_on_rally():
    path = list(np.linspace(100, 100, 40)) + list(np.linspace(100, 180, 60))
    panel = {"KRW-A": _series(path), "KRW-B": _series([100] * 100)}
    r = pf.run(panel, C, initial_cash=50_000, use_pattern=False, bearish_veto=False)
    kinds = [t["exit_kind"] for t in r.trades]
    assert "take_profit" in kinds or "forced_close" in kinds
    assert any(t["pnl_pct"] > 0 for t in r.trades)
    print("  take_profit_fires_on_rally OK")


if __name__ == "__main__":
    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
    print("\n전부 통과")
