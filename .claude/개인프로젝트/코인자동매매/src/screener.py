"""스크리너 점수 조합 (M2-b 골격).

지금은 '패턴 점수를 base 점수에 가산' 하는 얇은 함수 하나뿐이다.
유동성 필터·기술적 지표·순위 매기기는 M4 에서 붙인다.

핵심 규칙 (계획서 7.6): 패턴은 **가산 가중치**다. 패턴 하나로 진입하지 않는다.
"""

from __future__ import annotations

import pandas as pd

from config import settings
from src.patterns import Detection, detect, pattern_score


def combine(base_score: float, det: Detection | None) -> float:
    """base_score(전략/지표가 준 점수) 에 패턴 점수를 가산한다."""
    return base_score + settings.PATTERN_SCORE_WEIGHT * pattern_score(det)


def score(history: pd.DataFrame, base_score: float) -> tuple[float, Detection | None]:
    """history 로 패턴을 판정하고 combine 한 최종 점수와 탐지 결과를 돌려준다."""
    det = detect(history)
    return combine(base_score, det), det
