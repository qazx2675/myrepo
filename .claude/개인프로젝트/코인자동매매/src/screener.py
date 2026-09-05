"""스크리너 (M4 + 2026-09 전략 재검증).

유동성 필터 → 기술적 base_score → 패턴 가산(combine) → 최종 점수.
+ BTC 200일선 레짐 필터 (신규 진입 전면 게이트).

진입 신호(`SCREEN_STRATEGY`):
  - "momentum" : 20일 모멘텀. **검증 결과 엣지 없음** (홀드아웃 -4.2%). 남겨둠(비교용).
  - "meanrev"  : 상승추세 눌림목 매수. 튜닝에서 훨씬 나쁨(승률 35%). 미사용.
  - "breakout" : 20일 신고가 돌파 + 거래대금 확대. **채택** — 홀드아웃 통과
                 (conservative +18.5% / MDD -5.9% / 승률 65%, 2025-01~2026-06).
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


def _momentum_score(history: pd.DataFrame) -> float:
    """모멘텀: 20일간 오르고 20일선 위인 코인. (M4 원본. 검증 결과 엣지 없음)"""
    n = settings.SCREEN_MOMENTUM_DAYS
    c = history["close"]
    if len(c) < n + 2:
        return 0.0
    ret = c.iloc[-1] / c.iloc[-n] - 1
    above_ma = c.iloc[-1] > c.rolling(n).mean().iloc[-1]
    rsi = _rsi(c, settings.SCREEN_RSI_DAYS)
    score = 0.5
    score += max(-0.3, min(0.3, ret * 2))
    score += 0.1 if above_ma else -0.1
    if rsi > settings.SCREEN_RSI_OVERBOUGHT:
        score -= 0.15
    return max(0.0, min(1.0, score))


def _meanrev_score(history: pd.DataFrame) -> float:
    """평균회귀: 상승 추세(50일선 위·상승) 안에서 20일선 아래로 눌린 코인을 매수.

    - 레짐: 가격이 50일선 위 + 50일선이 10봉 전보다 상승 (하락장에선 진입 안 함)
    - 눌림: 가격이 20일선보다 2~15% 아래 (그보다 얕으면 눌림 아님, 깊으면 붕괴)
    - 확인: RSI 낮을수록 가점
    """
    c = history["close"]
    if len(c) < 52:
        return 0.0
    ma20 = c.rolling(20).mean().iloc[-1]
    ma50 = c.rolling(50).mean()
    px = float(c.iloc[-1])

    if px <= ma50.iloc[-1] or ma50.iloc[-1] <= ma50.iloc[-10]:
        return 0.0                                    # 상승 추세 아님

    dip = px / ma20 - 1                                # 음수 = 20일선 아래
    if dip > -0.02 or dip < -0.15:
        return 0.0

    rsi = _rsi(c, settings.SCREEN_RSI_DAYS)
    score = 0.5
    score += min(0.25, -dip * 2.5)                     # 깊을수록 가점 (-15%까지)
    score += 0.20 if rsi < 40 else (0.10 if rsi < 50 else -0.10)
    return max(0.0, min(1.0, score))


def _breakout_score(history: pd.DataFrame) -> float:
    """돈치안 돌파: 20일 신고가를 오늘 갱신 + 거래대금 확대 → 움직임 초입."""
    n = 20
    c = history["close"]
    v = history["value"]
    if len(c) < n + 6:
        return 0.0
    prior_high = float(c.iloc[-(n + 1):-1].max())     # 어제까지의 20일 신고가
    px = float(c.iloc[-1])
    if px <= prior_high:
        return 0.0                                    # 돌파 아님
    brk = px / prior_high - 1                          # 돌파 강도
    vol_ratio = float(v.iloc[-1]) / float(v.iloc[-6:-1].mean() + 1e-9)

    score = 0.5
    score += min(0.25, brk * 10)                       # 살짝 돌파 ~ 강하게
    score += 0.15 if vol_ratio > 1.3 else (0.0 if vol_ratio > 0.8 else -0.15)
    rsi = _rsi(c, settings.SCREEN_RSI_DAYS)
    if rsi > 85:
        score -= 0.15                                  # 너무 과열
    return max(0.0, min(1.0, score))


_STRATS = {"momentum": _momentum_score, "meanrev": _meanrev_score, "breakout": _breakout_score}


def base_score(history: pd.DataFrame) -> float:
    """[0, 1] 기술적 점수. SCREEN_STRATEGY 로 방식 선택."""
    return _STRATS.get(settings.SCREEN_STRATEGY, _momentum_score)(history)


def btc_regime_ok(btc_history: pd.DataFrame | None) -> bool:
    """BTC 가 200일선 위인가. SCREEN_BTC_REGIME 이 켜져 있을 때 신규 진입 게이트."""
    if not settings.SCREEN_BTC_REGIME:
        return True
    if btc_history is None or len(btc_history) < 200:
        return True                                   # 데이터 부족 시 막지 않음
    c = btc_history["close"]
    return float(c.iloc[-1]) > float(c.rolling(200).mean().iloc[-1])


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
