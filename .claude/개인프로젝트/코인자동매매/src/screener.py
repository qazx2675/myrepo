"""스크리너 (M4).

유동성 필터 → 기술적 base_score → 패턴 가산(combine) → 최종 점수.
패턴은 가산 가중치일 뿐, 단독 진입 신호가 아니다 (계획서 7.6).

기술적 지표는 최소한만: 20일 모멘텀 + 20일 이평 상단 여부 + RSI 과매수 감점.
정교한 지표는 M4-b 측정 결과 보고 필요하면 추가한다.
"""

from __future__ import annotations

import pandas as pd

from config import settings
from src.patterns import Detection, detect, pattern_score


def _rsi(close: pd.Series, days: int) -> float:
    d = close.diff().dropna()
    if len(d) < days:
        return 50.0
    up = d.clip(lower=0).rolling(days).mean().iloc[-1]
    dn = (-d.clip(upper=0)).rolling(days).mean().iloc[-1]
    if dn == 0:
        return 100.0
    rs = up / dn
    return 100 - 100 / (1 + rs)


def base_score(history: pd.DataFrame) -> float:
    """[0, 1] 기술적 점수. 0.5 = 중립."""
    n = settings.SCREEN_MOMENTUM_DAYS
    c = history["close"]
    if len(c) < n + 2:
        return 0.0
    ret = c.iloc[-1] / c.iloc[-n] - 1                 # n일 수익률
    above_ma = c.iloc[-1] > c.rolling(n).mean().iloc[-1]
    rsi = _rsi(c, settings.SCREEN_RSI_DAYS)

    score = 0.5
    score += max(-0.3, min(0.3, ret * 2))            # 모멘텀 ±0.3
    score += 0.1 if above_ma else -0.1
    if rsi > settings.SCREEN_RSI_OVERBOUGHT:
        score -= 0.15                                 # 과매수 감점
    return max(0.0, min(1.0, score))


def liquidity_ok(history: pd.DataFrame, min_24h_value_krw: float) -> bool:
    """최근 완결된 봉들의 평균 일 거래대금이 기준 이상인가.

    마지막 봉은 '오늘'이라 진행 중일 수 있어(부분 거래대금) 제외하고 직전 7일 평균을 쓴다.
    """
    if "value" not in history or len(history) < 3:
        return False
    recent = history["value"].iloc[-8:-1]
    if recent.empty:
        recent = history["value"].iloc[:-1]
    return float(recent.mean()) >= min_24h_value_krw


def combine(base: float, det: Detection | None) -> float:
    return base + settings.PATTERN_SCORE_WEIGHT * pattern_score(det)


def score(history: pd.DataFrame, min_24h_value_krw: float,
          use_pattern: bool = True, bearish_veto: bool = True):
    """(최종점수, base, Detection|None). 유동성 미달이면 최종점수 0."""
    if not liquidity_ok(history, min_24h_value_krw):
        return 0.0, 0.0, None
    b = base_score(history)
    det = detect(history) if (use_pattern or bearish_veto) else None
    if not use_pattern and not bearish_veto:
        return b, b, None
    if not use_pattern and bearish_veto:
        # 하락형이면 후보 제외, 아니면 base 그대로
        from src.patterns import is_bearish
        return (0.0 if is_bearish(det) else b), b, det
    return combine(b, det), b, det
