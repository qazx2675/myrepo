"""M2-b 데모: 엔진 A 상승형을 '가산 가중치'로 썼을 때 효과 측정.

    .venv\\Scripts\\python -m src.run_m2b              # 튜닝 기간
    .venv\\Scripts\\python -m src.run_m2b --holdout    # 홀드아웃 (1회만!)

같은 MA 크로스 트리거를 4가지 진입 규칙으로 비교한다:
  A  무필터           : 크로스업이면 매수
  B  하락형 회피만     : 크로스업 AND not is_bearish            (= M2-a)
  C  약한 가중치       : 크로스업 AND combine(base, det) >= 0.45 (하락형만 실질 차단)
  D  상승형 확인 요구   : 크로스업 AND combine(base, det) >= 0.60 (상승형 패턴 있어야 통과)

패턴은 트리거를 '거르는' 역할만 한다. 패턴만으로 매수하지 않는다 (계획서 7.6).
D 가 MDD 를 크게 낮추면서 수익을 조금만 깎으면 가중치 채택 근거. 거래수도 같이 본다.
"""

from __future__ import annotations

import os
import sys

from config import settings
from src import candles
from src.backtest import Position, run
from src.fills import Order
from src.patterns import detect, is_bearish
from src.screener import combine
from src.run_backtest import DEMO_CASH

COINS = ["KRW-BTC", "KRW-ETH", "KRW-XRP", "KRW-SOL", "KRW-DOGE"]
_MARKER = os.path.join("data", ".holdout_used")
FAST, SLOW, STAKE = 10, 30, 990_000


def _ma_signal(history):
    """(크로스업 여부, base_score[0..1])."""
    if len(history) < SLOW + 1:
        return False, False, 0.0
    c = history["close"]
    fma, sma = c.rolling(FAST).mean(), c.rolling(SLOW).mean()
    up = fma.iloc[-2] <= sma.iloc[-2] and fma.iloc[-1] > sma.iloc[-1]
    dn = fma.iloc[-2] >= sma.iloc[-2] and fma.iloc[-1] < sma.iloc[-1]
    gap = (fma.iloc[-1] - sma.iloc[-1]) / sma.iloc[-1]
    base = max(0.0, min(1.0, 0.5 + gap * 10))     # 크로스 강도 → 0.5 근처
    return (up, dn, base)


def make_strategy(rule: str):
    def strategy(history, position: Position | None):
        up, dn, base = _ma_signal(history)
        if position is not None:
            return Order("sell", "market", volume=position.volume) if dn else None
        if not up:
            return None
        det = detect(history)
        if rule == "A":
            ok = True
        elif rule == "B":
            ok = not is_bearish(det)
        elif rule == "C":
            ok = combine(base, det) >= 0.45
        else:  # D
            ok = combine(base, det) >= 0.60
        return Order("buy", "market", krw=STAKE) if ok else None
    return strategy


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
    rules = ["A", "B", "C", "D"]
    names = {"A": "무필터", "B": "하락회피", "C": "약가중치", "D": "상승확인"}

    print(f"\n===== M2-b  패턴 가중치  ({tag}: {start} ~ {end}) =====")
    agg = {r: {"ret": 0.0, "mdd": 0.0, "tr": 0} for r in rules}
    n_coin = 0
    for coin in COINS:
        df = candles.slice_period(candles.load(coin, "day", 1200), start, end)
        if len(df) < settings.PATTERN_WINDOW + 40:
            continue
        n_coin += 1
        row = [f"{coin:<10}"]
        for r in rules:
            m = run(df, make_strategy(r), initial_cash=DEMO_CASH).metrics
            agg[r]["ret"] += m.total_return_pct
            agg[r]["mdd"] += m.mdd_pct
            agg[r]["tr"] += m.n_trades
            row.append(f"{names[r]} {m.total_return_pct:>7.1f}% mdd{m.mdd_pct:>6.1f}% tr{m.n_trades:>2}")
        print("  " + " | ".join(row))

    if n_coin:
        print("-" * 100)
        print(f"  {'평균':<10}", end="")
        for r in rules:
            a = agg[r]
            print(f" | {names[r]} {a['ret']/n_coin:>7.1f}% mdd{a['mdd']/n_coin:>6.1f}% tr{a['tr']}", end="")
        print()
        base = agg["A"]
        print("\n  A 대비:")
        for r in ["B", "C", "D"]:
            a = agg[r]
            print(f"    {names[r]}: 수익 {(a['ret']-base['ret'])/n_coin:+.1f}%p, "
                  f"MDD {(a['mdd']-base['mdd'])/n_coin:+.1f}%p, 거래 {a['tr']-base['tr']:+d}회")
        print("\n  주의: 표본 작음(5코인×1기간). 방향성 참고만. 홀드아웃으로 재검증.")

    if holdout:
        with open(_MARKER, "a", encoding="utf-8") as fp:
            fp.write(f"M2-b {start}~{end} "
                     + " ".join(f"{names[r]}_ret={agg[r]['ret']/max(n_coin,1):.1f}" for r in rules) + "\n")


if __name__ == "__main__":
    main()
