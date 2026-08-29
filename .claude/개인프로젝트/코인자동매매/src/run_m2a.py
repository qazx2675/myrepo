"""M2-a 데모: 하락형 회피 필터가 성과를 개선하는지 측정한다.

    .venv\\Scripts\\python -m src.run_m2a              # 튜닝 기간
    .venv\\Scripts\\python -m src.run_m2a --holdout    # 홀드아웃 (1회만!)

같은 MA 크로스 데모 전략을 두 번 돌린다:
  (A) 필터 없음
  (B) 매수 직전 engine_a 로 하락형 패턴(하락삼각/상승쐐기/베어깃발) 감지 시 매수 스킵

'틀린 회피는 기회비용, 틀린 진입은 실손실' — 필터가 MDD 를 낮추면 채택 근거가 된다.
탐지된 하락형은 data/pattern_charts/ 에 PNG 로 저장 (육안 검증용, 계획서 7.8).

주의: 코인 5종 × 기간 1개 = 표본이 매우 작다. 이 결과로 확신하지 말 것.
M2-b / M3-b 이후 홀드아웃으로 재검증한다.
"""

from __future__ import annotations

import os
import sys

from config import settings
from src import candles
from src.backtest import Position, run
from src.fills import Order
from src.patterns import debug_chart, detect, is_bearish
from src.run_backtest import DEMO_CASH, demo_ma_cross

COINS = ["KRW-BTC", "KRW-ETH", "KRW-XRP", "KRW-SOL", "KRW-DOGE"]
_MARKER = os.path.join("data", ".holdout_used")


def with_filter(base_strategy, chart_budget: list[int]):
    """base_strategy 의 매수 신호를 하락형 패턴이면 취소한다."""
    def strategy(history, position: Position | None):
        order = base_strategy(history, position)
        if order is None or order.side != "buy":
            return order
        det = detect(history)
        if is_bearish(det):
            if chart_budget[0] > 0:
                try:
                    debug_chart.save(det, title_extra=str(history.index[-1].date()))
                    chart_budget[0] -= 1
                except Exception as e:            # 차트 실패가 백테스트를 막지 않음
                    print(f"  (차트 저장 실패: {e})")
            return None
        return order
    return strategy


def _period(holdout: bool):
    if holdout:
        os.makedirs("data", exist_ok=True)
        if os.path.exists(_MARKER):
            print("⚠ 이미 홀드아웃 실행 기록 있음:", open(_MARKER).read().strip())
        print("⚠ 홀드아웃 실행. 이 결과 보고 파라미터 만지면 검증 무의미.")
        return settings.HOLDOUT_START, settings.HOLDOUT_END, "HOLDOUT"
    return settings.TUNE_START, settings.TUNE_END, "튜닝"


def main():
    holdout = "--holdout" in sys.argv
    start, end, tag = _period(holdout)
    chart_budget = [8]

    print(f"\n===== M2-a  하락형 회피 필터  ({tag}: {start} ~ {end}) =====")
    print(f"{'코인':<10}{'수익률(무필터)':>16}{'수익률(필터)':>16}{'MDD(무필터)':>14}{'MDD(필터)':>14}{'거래(무/필)':>12}")

    agg = {"ret_a": 0.0, "ret_b": 0.0, "mdd_a": 0.0, "mdd_b": 0.0, "n": 0}
    for coin in COINS:
        df = candles.slice_period(candles.load(coin, "day", count=1200), start, end)
        if len(df) < settings.PATTERN_WINDOW + 40:
            print(f"{coin:<10}  (캔들 부족, 건너뜀)")
            continue
        a = run(df, demo_ma_cross(), initial_cash=DEMO_CASH)
        b = run(df, with_filter(demo_ma_cross(), chart_budget), initial_cash=DEMO_CASH)
        ma, mb = a.metrics, b.metrics
        print(f"{coin:<10}{ma.total_return_pct:>15.1f}%{mb.total_return_pct:>15.1f}%"
              f"{ma.mdd_pct:>13.1f}%{mb.mdd_pct:>13.1f}%{ma.n_trades:>7}/{mb.n_trades:<4}")
        agg["ret_a"] += ma.total_return_pct; agg["ret_b"] += mb.total_return_pct
        agg["mdd_a"] += ma.mdd_pct; agg["mdd_b"] += mb.mdd_pct; agg["n"] += 1

    d_mdd = d_ret = 0.0
    if agg["n"]:
        n = agg["n"]
        print("-" * 82)
        print(f"{'평균':<10}{agg['ret_a']/n:>15.1f}%{agg['ret_b']/n:>15.1f}%"
              f"{agg['mdd_a']/n:>13.1f}%{agg['mdd_b']/n:>13.1f}%")
        d_mdd = agg["mdd_b"]/n - agg["mdd_a"]/n
        d_ret = agg["ret_b"]/n - agg["ret_a"]/n
        print(f"\n필터 효과 (데모 전략 기준): MDD {d_mdd:+.1f}%p, 수익률 {d_ret:+.1f}%p")

    # 필터가 데모전략의 매수를 거의 못 막으므로, 엔진이 실제로 무엇을 얼마나 잡는지 별도 스캔
    print("\n[참고] 전 기간 패턴 탐지 빈도 (매수 시점 무관, 봉마다 스캔):")
    for coin in COINS:
        df = candles.slice_period(candles.load(coin, "day", 1200), start, end)
        if len(df) < settings.PATTERN_WINDOW + 5:
            continue
        bear = other = 0
        for i in range(settings.PATTERN_WINDOW, len(df)):
            d = detect(df.iloc[:i])
            if d is None:
                continue
            if is_bearish(d):
                bear += 1
                if chart_budget[0] > 0:
                    try:
                        debug_chart.save(d, title_extra=f"{coin} {df.index[i-1].date()}")
                        chart_budget[0] -= 1
                    except Exception:
                        pass
            else:
                other += 1
        print(f"  {coin:<10} 하락형 {bear:>3}봉 / 그 외 {other:>3}봉")

    print("\n해석: 데모(MA크로스)는 상승추세에서만 매수해서 하락형 패턴과 거의 겹치지 않는다.")
    print("      → 필터의 실제 가치는 M4 이후 '진짜 진입 로직' 위에서 재측정한다.")
    saved = 8 - chart_budget[0]
    if saved:
        print(f"하락형 패턴 차트 {saved}장: {settings.PATTERN_DEBUG_DIR}/  (육안 검증)")

    if holdout:
        with open(_MARKER, "a", encoding="utf-8") as fp:
            fp.write(f"M2-a {start}~{end} d_mdd={d_mdd:+.1f} d_ret={d_ret:+.1f}\n")


if __name__ == "__main__":
    main()
