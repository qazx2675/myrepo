"""M2-a 엔진 A 테스트.

    .venv\\Scripts\\python -m tests.test_m2a

합성 캔들로 각 패턴이 의도대로 분류되는지 본다.
"""

from __future__ import annotations

import numpy as np
import pandas as pd

from src.patterns import detect, is_bearish

N = 40                       # 윈도우 크기와 동일
PEAKS = [3, 11, 19, 27, 35]
TROUGHS = [7, 15, 23, 31, 39]
PRE = 18                      # 윈도우 직전 추세용 여유봉


def _build(upper_fn, lower_fn, prior_slope=0.0) -> pd.DataFrame:
    """PEAKS 에서만 상단선에, TROUGHS 에서만 하단선에 닿는다. 나머지 봉은 밴드 안쪽."""
    high = np.empty(N)
    low = np.empty(N)
    close = np.empty(N)
    for i in range(N):
        u, l = float(upper_fn(i)), float(lower_fn(i))
        band = u - l
        mid = (u + l) / 2
        if i in PEAKS:
            high[i], low[i] = u, mid - 0.10 * band
        elif i in TROUGHS:
            high[i], low[i] = mid + 0.10 * band, l
        else:
            high[i], low[i] = mid + 0.15 * band, mid - 0.15 * band
        close[i] = (high[i] + low[i]) / 2

    base = close[0]
    pre_close = np.array([base * (1 + prior_slope * (j - PRE)) for j in range(PRE)])
    pc = np.concatenate([pre_close, close])
    ph = np.concatenate([pre_close * 1.002, high])
    pl = np.concatenate([pre_close * 0.998, low])
    po = np.concatenate([[pc[0]], pc[:-1]])
    idx = pd.date_range("2024-01-01", periods=len(pc), freq="D")
    return pd.DataFrame({"open": po, "high": ph, "low": pl, "close": pc,
                         "volume": 1.0, "value": 1.0}, index=idx)


def test_descending_triangle():
    df = _build(lambda i: 1000 - 4 * i, lambda i: 800)
    d = detect(df)
    assert d is not None and d.pattern == "하락삼각", d and d.pattern
    assert d.direction == "bearish" and is_bearish(d)
    print("  하락삼각 OK")


def test_ascending_triangle():
    df = _build(lambda i: 1000, lambda i: 800 + 4 * i)
    d = detect(df)
    assert d is not None and d.pattern == "상승삼각", d and d.pattern
    assert d.direction == "bullish" and not is_bearish(d)
    print("  상승삼각 OK")


def test_rising_wedge():
    df = _build(lambda i: 900 + 3 * i, lambda i: 780 + 6 * i)   # 둘 다 상승, 하단이 더 가파름 → 수렴
    d = detect(df)
    assert d is not None and d.pattern == "상승쐐기", d and d.pattern
    assert is_bearish(d)
    print("  상승쐐기 OK")


def test_symmetrical_triangle():
    df = _build(lambda i: 1000 - 2 * i, lambda i: 800 + 2 * i)
    d = detect(df)
    assert d is not None and d.pattern == "대칭삼각", d and d.pattern
    assert d.direction == "neutral" and not is_bearish(d)
    print("  대칭삼각 OK")


def test_box_range():
    df = _build(lambda i: 1000, lambda i: 800)
    d = detect(df)
    assert d is not None and d.pattern == "박스권", d and d.pattern
    assert not is_bearish(d)
    print("  박스권 OK")


def test_bear_flag():
    # 평행 상승 채널 + 직전 하락추세 → 베어깃발
    df = _build(lambda i: 1000 + 4 * i, lambda i: 850 + 4 * i, prior_slope=-0.015)
    d = detect(df)
    assert d is not None and d.pattern == "베어깃발", d and d.pattern
    assert is_bearish(d)
    print("  베어깃발 OK")


def test_no_pattern_on_short_input():
    df = _build(lambda i: 1000, lambda i: 800).iloc[:20]
    assert detect(df) is None
    print("  짧은입력 None OK")


if __name__ == "__main__":
    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
    print("\n전부 통과")
