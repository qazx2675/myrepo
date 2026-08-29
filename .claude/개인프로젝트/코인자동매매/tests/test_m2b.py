"""M2-b 테스트 — 패턴 점수 + 스크리너 조합.

    .venv\\Scripts\\python -m tests.test_m2b
"""

from __future__ import annotations

from config import settings
from src.patterns import is_bullish, pattern_score
from src.patterns.engine_a import Detection, Line
from src.screener import combine
from tests.test_m2a import _build
from src.patterns import detect

_L = Line(0.0, 0.0, 1.0, 3)


def _det(pattern):
    return Detection(pattern, "x", 0.8, _L, _L, [], [], None)


def test_score_sign():
    assert pattern_score(None) == 0.0
    assert pattern_score(_det("상승삼각")) > 0
    assert pattern_score(_det("박스권")) == 0.0
    assert pattern_score(_det("하락삼각")) < 0
    print("  score_sign OK")


def test_bearish_veto():
    settings.PATTERN_BEARISH_VETO = True
    assert pattern_score(_det("상승쐐기")) == -1.0
    settings.PATTERN_BEARISH_VETO = False
    assert -1.0 < pattern_score(_det("상승쐐기")) < 0
    settings.PATTERN_BEARISH_VETO = True     # 원복
    print("  bearish_veto OK")


def test_combine():
    w = settings.PATTERN_SCORE_WEIGHT
    assert abs(combine(0.5, None) - 0.5) < 1e-9
    assert abs(combine(0.5, _det("상승삼각")) - (0.5 + w * 0.8)) < 1e-9
    assert combine(0.5, _det("하락삼각")) == 0.5 - w        # veto -> -1
    print("  combine OK")


def test_bullish_detection_flows_through():
    # 상승삼각 합성 → is_bullish, score 양수, combine 이 base 를 올린다
    df = _build(lambda i: 1000, lambda i: 800 + 4 * i)
    d = detect(df)
    assert is_bullish(d) and pattern_score(d) > 0
    assert combine(0.5, d) > 0.5
    print("  bullish_flow OK")


if __name__ == "__main__":
    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
    print("\n전부 통과")
