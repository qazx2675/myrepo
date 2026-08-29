"""M4 데모: 포트폴리오 백테스터로 리스크 규칙 + 패턴 필터를 실측정한다.

    .venv\\Scripts\\python -m src.run_m4              # 튜닝 기간
    .venv\\Scripts\\python -m src.run_m4 --holdout    # 홀드아웃 (1회만!)

- conservative / aggressive 모드
- 패턴 사용: none / bearish-veto(M2-a) / soft-weight(M2-b)
  → 진짜 스크리너 진입 로직 위에서 M2-a/M2-b 필터가 실제로 도움 되는지 판정

주의: 5코인 유니버스, 1기간. 표본 작음. 방향성 참고. 홀드아웃으로 재검증.
"""

from __future__ import annotations

import os
import sys

from config import settings
from src import candles
from src.portfolio_backtest import run
from src.risk import AGGRESSIVE, CONSERVATIVE

UNIVERSE = ["KRW-BTC", "KRW-ETH", "KRW-XRP", "KRW-SOL", "KRW-DOGE",
           "KRW-ADA", "KRW-DOT", "KRW-LINK"]
_MARKER = os.path.join("data", ".holdout_used")

PATTERN_MODES = [
    ("패턴없음", dict(use_pattern=False, bearish_veto=False)),
    ("하락veto", dict(use_pattern=False, bearish_veto=True)),
    ("가중치",   dict(use_pattern=True, bearish_veto=True)),
]


def _period(holdout):
    if holdout:
        os.makedirs("data", exist_ok=True)
        if os.path.exists(_MARKER):
            print("⚠ 이미 홀드아웃 기록:", open(_MARKER).read().strip())
        print("⚠ 홀드아웃 실행. 결과 보고 파라미터 만지면 검증 무의미.")
        return settings.HOLDOUT_START, settings.HOLDOUT_END, "HOLDOUT"
    return settings.TUNE_START, settings.TUNE_END, "튜닝"


def main():
    holdout = "--holdout" in sys.argv
    start, end, tag = _period(holdout)

    panel = {}
    for m in UNIVERSE:
        df = candles.slice_period(candles.load(m, "day", 1200), start, end)
        if len(df) > settings.SCREEN_MOMENTUM_DAYS + settings.PATTERN_WINDOW:
            panel[m] = df
    panel = candles.align(panel)
    print(f"\n===== M4 포트폴리오 백테스트  ({tag}: {start} ~ {end}, {len(panel)}코인) =====")

    logline = []
    for mode in (CONSERVATIVE, AGGRESSIVE):
        print(f"\n--- {mode.name}  (운용금 {mode.capital:,} / 손절 {mode.stop_loss_pct}% / "
              f"일일한도 {mode.daily_loss_limit_pct}%) ---")
        print(f"  {'패턴':<10}{'수익률':>10}{'MDD':>10}{'거래':>6}{'승률':>8}   청산내역")
        for pname, opt in PATTERN_MODES:
            r = run(panel, mode, initial_cash=mode.capital, **opt)
            mm = r.metrics
            ex = ", ".join(f"{k}:{v}" for k, v in r.exits.items() if v)
            print(f"  {pname:<10}{mm.total_return_pct:>9.1f}%{mm.mdd_pct:>9.1f}%"
                  f"{mm.n_trades:>6}{mm.win_rate_pct:>7.0f}%   {ex}")
            logline.append(f"{mode.name}/{pname}={mm.total_return_pct:.0f}%(mdd{mm.mdd_pct:.0f})")

    print("\n해석:")
    print("  - '하락veto'·'가중치' 가 '패턴없음' 대비 MDD 를 낮추면서 수익 훼손이 작으면 패턴 채택.")
    print("  - 손절/익절/추적손절 청산이 골고루 나오는지 = 리스크 규칙이 실제로 도는지 확인.")
    print("  - conservative 가 aggressive 보다 MDD 가 낮아야 정상 (안 그러면 파라미터 재검토).")

    if holdout:
        with open(_MARKER, "a", encoding="utf-8") as fp:
            fp.write("M4 " + start + "~" + end + " " + " ".join(logline) + "\n")


if __name__ == "__main__":
    main()
