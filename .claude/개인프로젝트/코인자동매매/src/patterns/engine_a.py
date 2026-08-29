"""엔진 A — 추세선 2개 선형회귀 패턴 분류 (M2-a).

고점 스윙들에 회귀선 1개, 저점 스윙들에 회귀선 1개.
두 선의 (기울기 부호, 수렴/발산/평행) 조합으로 패턴을 판정한다.

M2-a 는 이 중 **하락형만** 회피 필터로 쓴다: 하락삼각 / 상승쐐기 / 베어깃발.
상승형·중립형도 판정은 하지만(엔진 재사용), M2-a 진입 로직에는 넣지 않는다.

한계: 헤드앤숄더·더블탑 같은 스윙 시퀀스 패턴은 추세선으로 안 잡힌다 → 엔진 B (M3-b).
"""

from __future__ import annotations

from dataclasses import dataclass

import numpy as np
import pandas as pd

from config import settings

BEARISH = {"하락삼각", "상승쐐기", "베어깃발"}
BULLISH = {"상승삼각", "하락쐐기", "불깃발"}
NEUTRAL = {"대칭삼각", "메가폰", "박스권"}


@dataclass
class Line:
    slope: float          # 봉당 가격 변화
    intercept: float      # x=0(윈도우 첫 봉) 에서의 값
    r2: float
    n: int                # 사용된 스윙 개수

    def at(self, x: float) -> float:
        return self.slope * x + self.intercept


@dataclass
class Detection:
    pattern: str
    direction: str        # "bearish" | "bullish" | "neutral"
    confidence: float     # min(R²_상단, R²_하단)
    upper: Line
    lower: Line
    highs_idx: list[int]  # 윈도우 내 스윙 고점 위치
    lows_idx: list[int]
    window: pd.DataFrame  # 분석에 쓴 구간 (디버그 차트용)


def _swings(values: np.ndarray, k: int, kind: str) -> list[int]:
    """프랙탈 스윙 위치. kind='high' 면 좌우 k봉보다 큰 지점, 'low' 면 작은 지점."""
    out = []
    n = len(values)
    for i in range(k, n - k):
        seg = values[i - k : i + k + 1]
        if kind == "high" and values[i] == seg.max() and (seg.argmax() == k):
            out.append(i)
        elif kind == "low" and values[i] == seg.min() and (seg.argmin() == k):
            out.append(i)
    return out


def _fit(xs: list[int], ys: np.ndarray) -> Line:
    x = np.array(xs, dtype=float)
    y = np.array(ys, dtype=float)
    slope, intercept = np.polyfit(x, y, 1)
    pred = slope * x + intercept
    ss_res = float(np.sum((y - pred) ** 2))
    ss_tot = float(np.sum((y - y.mean()) ** 2))
    r2 = 1.0 - ss_res / ss_tot if ss_tot > 0 else 1.0
    return Line(float(slope), float(intercept), max(0.0, min(1.0, r2)), len(xs))


def _prior_trend(df_full: pd.DataFrame, win_start_pos: int, lookback: int) -> str:
    """윈도우 직전 lookback 봉의 추세. 'up' | 'down' | 'flat'."""
    a = max(0, win_start_pos - lookback)
    seg = df_full["close"].iloc[a:win_start_pos]
    if len(seg) < 3:
        return "flat"
    x = np.arange(len(seg), dtype=float)
    slope = np.polyfit(x, seg.values, 1)[0] / seg.mean()
    if slope > settings.PATTERN_FLAT_SLOPE:
        return "up"
    if slope < -settings.PATTERN_FLAT_SLOPE:
        return "down"
    return "flat"


def _classify(u: Line, lo: Line, n_bars: int, mean_price: float, prior: str) -> tuple[str, str] | None:
    flat = settings.PATTERN_FLAT_SLOPE
    su, sl = u.slope / mean_price, lo.slope / mean_price      # 정규화 기울기

    up_u, dn_u = su > flat, su < -flat
    up_l, dn_l = sl > flat, sl < -flat
    flat_u = not up_u and not dn_u
    flat_l = not up_l and not dn_l

    w0 = (u.at(0) - lo.at(0)) / mean_price
    w1 = (u.at(n_bars - 1) - lo.at(n_bars - 1)) / mean_price
    if w0 <= 0:
        return None
    converging = w1 < w0 * settings.PATTERN_CONVERGE_RATIO
    diverging = w1 > w0 / settings.PATTERN_CONVERGE_RATIO
    parallel = abs(w1 - w0) / w0 <= settings.PATTERN_CHANNEL_TOL

    if converging:
        if dn_u and flat_l:
            return "하락삼각", "bearish"
        if flat_u and up_l:
            return "상승삼각", "bullish"
        if dn_u and up_l:
            return "대칭삼각", "neutral"
        if up_u and up_l:
            return "상승쐐기", "bearish"
        if dn_u and dn_l:
            return "하락쐐기", "bullish"
        return None
    if diverging and up_u and dn_l:
        return "메가폰", "neutral"
    if parallel:
        if flat_u and flat_l:
            return "박스권", "neutral"
        if up_u and up_l:
            return ("베어깃발", "bearish") if prior == "down" else ("상승채널", "neutral")
        if dn_u and dn_l:
            return ("불깃발", "bullish") if prior == "up" else ("하락채널", "neutral")
    return None


def detect(df: pd.DataFrame, window: int | None = None) -> Detection | None:
    """df 의 마지막 `window` 봉에서 패턴 1개를 판정. 없으면 None."""
    window = settings.PATTERN_WINDOW if window is None else window
    k = settings.PATTERN_SWING_K
    if len(df) < window:
        return None

    win_start_pos = len(df) - window
    w = df.iloc[win_start_pos:]
    highs = _swings(w["high"].values, k, "high")
    lows = _swings(w["low"].values, k, "low")
    if len(highs) < settings.PATTERN_MIN_SWINGS or len(lows) < settings.PATTERN_MIN_SWINGS:
        return None

    u = _fit(highs, w["high"].values[highs])
    lo = _fit(lows, w["low"].values[lows])
    conf = min(u.r2, lo.r2)
    if conf < settings.PATTERN_MIN_CONFIDENCE:
        return None

    mean_price = float(w["close"].mean())
    prior = _prior_trend(df, win_start_pos, settings.PATTERN_FLAG_LOOKBACK)
    res = _classify(u, lo, window, mean_price, prior)
    if res is None:
        return None
    pattern, direction = res
    return Detection(pattern, direction, conf, u, lo, highs, lows, w)


def is_bearish(det: Detection | None) -> bool:
    """M2-a 회피 필터: 이 패턴이 잡히면 진입 후보에서 제외한다."""
    return det is not None and det.pattern in BEARISH


def is_bullish(det: Detection | None) -> bool:
    return det is not None and det.pattern in BULLISH


def pattern_score(det: Detection | None) -> float:
    """패턴을 [-1, 1] 점수로. 상승형 +, 하락형 -, 중립/미탐지 0. 신뢰도로 스케일.

    M2-b 에서 스크리너 점수에 '가산'된다. 이 값만으로 진입하지 않는다.
    """
    if det is None:
        return 0.0
    if det.pattern in BULLISH:
        return det.confidence
    if det.pattern in BEARISH:
        return -1.0 if settings.PATTERN_BEARISH_VETO else -det.confidence
    return 0.0
