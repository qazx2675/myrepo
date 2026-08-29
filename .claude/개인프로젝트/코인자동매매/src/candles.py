"""과거 캔들 로더 + 로컬 캐시 (M3-a).

백테스트를 돌릴 때마다 빗썸 API 를 때리지 않도록 CSV 로 캐시한다.
캐시가 요청 개수보다 짧을 때만 다시 받는다 (증분 아님 — 통째로. 단순함 우선).
"""

from __future__ import annotations

import os

import pandas as pd
import python_bithumb as pb

from config import settings

_CACHE = settings.CANDLE_CACHE_DIR
_COLS = ["open", "high", "low", "close", "volume", "value"]


def _path(market: str, interval: str) -> str:
    return os.path.join(_CACHE, f"{market}_{interval}.csv")


def load(market: str, interval: str = "day", count: int = 1200, refresh: bool = False) -> pd.DataFrame:
    """캔들 DataFrame. 컬럼: open/high/low/close/volume/value, index=KST datetime(오름차순)."""
    os.makedirs(_CACHE, exist_ok=True)
    p = _path(market, interval)

    if not refresh and os.path.exists(p):
        df = pd.read_csv(p, index_col=0, parse_dates=True)
        if len(df) >= count:
            return df.iloc[-count:]

    df = pb.get_ohlcv(market, interval=interval, count=count)
    df = df[_COLS].astype(float)
    df.to_csv(p)
    return df


def slice_period(df: pd.DataFrame, start: str, end: str) -> pd.DataFrame:
    """[start, end] 양끝 포함으로 자른다. 'YYYY-MM-DD'."""
    return df.loc[start:end]


def align(panel: dict[str, pd.DataFrame]) -> dict[str, pd.DataFrame]:
    """여러 코인 DataFrame 을 공통 날짜(교집합)로 맞춘다."""
    common = None
    for df in panel.values():
        common = df.index if common is None else common.intersection(df.index)
    return {m: df.loc[common] for m, df in panel.items()}
