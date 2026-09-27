"""M3-a 데모 실행기.

    .venv\\Scripts\\python -m src.run_backtest              # 튜닝 기간
    .venv\\Scripts\\python -m src.run_backtest --holdout    # 홀드아웃 (1회만!)

여기 쓰인 MA 크로스 전략은 **백테스터 파이프라인이 도는지 확인하는 데모**다.
실전 전략이 아니다. 실전 전략(엔진 A/B)은 M2 에서 이 백테스터를 재사용해 만든다.

홀드아웃 가드: --holdout 을 쓰면 data/.holdout_used 에 기록하고 경고한다.
계획서 7.7 — 홀드아웃 기간에서 파라미터를 만지작거리면 검증이 무의미해진다.
"""

from __future__ import annotations

import logging
import os
import sys

from config import settings
from src import candles
from src.backtest import Position, run
from src.fills import Order

logging.basicConfig(level=logging.INFO, format="%(levelname)s: %(message)s")
log = logging.getLogger("backtest")

MARKET = "KRW-BTC"
_MARKER = os.path.join("data", ".holdout_used")


DEMO_CASH = 1_000_000     # 데모는 소액 전액투입으로 파이프라인만 본다 (PAPER_CAPITAL 아님)


def demo_ma_cross(fast: int = 10, slow: int = 30, stake: float = 990_000):
    """단기 MA 가 장기 MA 를 상향 돌파하면 전액 매수, 하향 돌파하면 전량 매도."""
    def strategy(history, position: Position | None):
        if len(history) < slow + 1:
            return None
        c = history["close"]
        fma, sma = c.rolling(fast).mean(), c.rolling(slow).mean()
        f_now, f_prev = fma.iloc[-1], fma.iloc[-2]
        s_now, s_prev = sma.iloc[-1], sma.iloc[-2]
        cross_up = f_prev <= s_prev and f_now > s_now
        cross_dn = f_prev >= s_prev and f_now < s_now
        if position is None and cross_up:
            return Order("buy", "market", krw=stake, tag="ma_cross_up")
        if position is not None and cross_dn:
            return Order("sell", "market", volume=position.volume, tag="ma_cross_dn")
        return None
    return strategy


def _print(title, res):
    print(f"\n===== {title} =====")
    for k, v in res.metrics.as_rows():
        print(f"  {k:<22} {v}")
    if res.unclosed:
        print("  (주의: 마지막 봉에 강제 청산됨)")
    if res.metrics.effective_fee_rate > settings.FEE_ALERT_ABOVE:
        print(f"  ⚠ 실효 수수료율이 FEE_ALERT_ABOVE({settings.FEE_ALERT_ABOVE}) 초과")


def main():
    holdout = "--holdout" in sys.argv
    df = candles.load(MARKET, "day", count=1200)

    if holdout:
        os.makedirs("data", exist_ok=True)
        prev = open(_MARKER).read().strip() if os.path.exists(_MARKER) else ""
        log.warning("홀드아웃 실행. 이 결과를 보고 파라미터를 바꾸면 검증이 무의미해진다.")
        if prev:
            log.warning("이미 홀드아웃을 돌린 기록이 있다:\n%s", prev)
        start, end, tag = settings.HOLDOUT_START, settings.HOLDOUT_END, "HOLDOUT"
    else:
        start, end, tag = settings.TUNE_START, settings.TUNE_END, "튜닝"

    seg = candles.slice_period(df, start, end)
    res = run(seg, demo_ma_cross(), initial_cash=DEMO_CASH)
    _print(f"{tag}  {MARKET}  MA(10/30) 데모", res)

    # 참고용: 수수료 쿠폰 미적용(0.25%) 시나리오
    res_nofee = run(seg, demo_ma_cross(), initial_cash=DEMO_CASH, fee_rate=0.0025)
    print(f"\n  [쿠폰 미적용 0.25%] 총수익률 {res_nofee.metrics.total_return_pct:,.2f} % "
          f"(쿠폰 적용 대비 {res_nofee.metrics.total_return_pct - res.metrics.total_return_pct:+.2f}%p)")

    if holdout:
        with open(_MARKER, "a", encoding="utf-8") as fp:
            fp.write(f"{start}~{end} {MARKET} return={res.metrics.total_return_pct:.2f}% "
                     f"mdd={res.metrics.mdd_pct:.2f}%\n")


if __name__ == "__main__":
    main()
