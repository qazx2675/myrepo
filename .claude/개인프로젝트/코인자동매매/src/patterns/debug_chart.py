"""패턴 탐지 결과를 PNG 로 저장 (M2-a).

"왜 이걸 하락삼각으로 봤나" 를 눈으로 확인 못 하면 파라미터 튜닝이 불가능하다.
(계획서 7.8)
"""

from __future__ import annotations

import os

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt

# 한글 라벨용. Windows 기본 폰트(맑은 고딕) → 없으면 DejaVu 로 폴백.
plt.rcParams["font.family"] = ["Malgun Gothic", "DejaVu Sans"]
plt.rcParams["axes.unicode_minus"] = False

from config import settings
from src.patterns.engine_a import Detection


def save(det: Detection, path: str | None = None, title_extra: str = "") -> str:
    w = det.window
    x = range(len(w))
    if path is None:
        os.makedirs(settings.PATTERN_DEBUG_DIR, exist_ok=True)
        stamp = str(w.index[-1].date())
        path = os.path.join(settings.PATTERN_DEBUG_DIR, f"{stamp}_{det.pattern}.png")

    fig, ax = plt.subplots(figsize=(10, 5))
    ax.plot(x, w["close"].values, color="#888", lw=1, label="close")
    ax.fill_between(x, w["low"].values, w["high"].values, color="#ccc", alpha=0.4)

    ax.scatter(det.highs_idx, w["high"].values[det.highs_idx], color="crimson", s=30, zorder=5)
    ax.scatter(det.lows_idx, w["low"].values[det.lows_idx], color="royalblue", s=30, zorder=5)

    xs = [0, len(w) - 1]
    ax.plot(xs, [det.upper.at(v) for v in xs], "--", color="crimson",
            label=f"upper (slope {det.upper.slope:,.0f}, R² {det.upper.r2:.2f})")
    ax.plot(xs, [det.lower.at(v) for v in xs], "--", color="royalblue",
            label=f"lower (slope {det.lower.slope:,.0f}, R² {det.lower.r2:.2f})")

    ax.set_title(f"{det.pattern} [{det.direction}]  conf={det.confidence:.2f}  {title_extra}")
    ax.legend(fontsize=8)
    fig.tight_layout()
    fig.savefig(path, dpi=90)
    plt.close(fig)
    return path
