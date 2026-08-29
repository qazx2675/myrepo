"""단일 종목 백테스터 (M3-a).

전략은 콜러블 하나다:

    strategy(history, position) -> Order | None

  history  : 현재 봉까지 포함한 캔들 DataFrame (마지막 행 = '오늘', 종가 시점 판단)
  position : 보유 중이면 Position, 아니면 None
  반환      : fills.Order 또는 None

신호는 BACKTEST_EXEC_DELAY_BARS 봉 뒤 시가에 체결된다 (PAPER_LATENCY_MS 의 봉단위 근사).
룩어헤드 방지: 전략은 df.iloc[:i+1] 만 본다.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Callable, Optional

import pandas as pd

from config import settings
from src import fills, metrics
from src.fills import Order

Strategy = Callable[[pd.DataFrame, Optional["Position"]], Optional[Order]]


@dataclass
class Position:
    volume: float
    entry_price: float          # 명목 기준 평균 매입 단가
    entry_time: pd.Timestamp
    entry_i: int                # 진입 봉 index (보유 봉수 계산용)
    cost_krw: float             # 매수 시 총 투입 KRW (수수료 포함)


@dataclass
class Result:
    equity: pd.Series
    trades: list[dict]
    metrics: metrics.Metrics
    unclosed: bool = False      # 마지막 봉에 강제 청산했는지
    _bars_in_market: int = field(default=0, repr=False)


def run(
    df: pd.DataFrame,
    strategy: Strategy,
    initial_cash: float,
    fee_rate: float | None = None,
    slippage_bps: float | None = None,
    delay_bars: int | None = None,
) -> Result:
    fee_rate = settings.BACKTEST_FEE_RATE if fee_rate is None else fee_rate
    slippage_bps = settings.FILL_SLIPPAGE_BPS if slippage_bps is None else slippage_bps
    delay = settings.BACKTEST_EXEC_DELAY_BARS if delay_bars is None else delay_bars

    if len(df) < delay + 2:
        raise ValueError("캔들이 너무 적습니다")

    cash = float(initial_cash)
    pos: Optional[Position] = None
    pending: list[tuple[int, Order]] = []     # (실행봉 index, order)
    trades: list[dict] = []
    equity_vals: list[float] = []
    bars_in_market = 0

    for i in range(len(df)):
        bar = df.iloc[i]

        # 1) 이번 봉에 실행 예정인 주문 체결
        due = [o for (idx, o) in pending if idx == i]
        pending = [(idx, o) for (idx, o) in pending if idx != i]
        for order in due:
            fill = fills.execute(order, bar, fee_rate, slippage_bps)
            if fill is None:
                continue
            if order.side == "buy":
                if pos is not None:
                    # M3-a 는 물타기 없음 (M4/M8). 이미 보유 중이면 매수 무시.
                    continue
                cash -= (fill.notional + fill.fee)
                pos = Position(fill.volume, fill.price, df.index[i], i, fill.notional + fill.fee)
            else:  # sell
                if pos is None:
                    continue
                proceeds = fill.notional - fill.fee
                cash += proceeds
                pnl_krw = proceeds - pos.cost_krw
                trades.append({
                    "entry_time": pos.entry_time, "exit_time": df.index[i],
                    "entry_price": pos.entry_price, "exit_price": fill.price,
                    "volume": pos.volume,
                    "pnl_krw": pnl_krw,
                    "pnl_pct": pnl_krw / pos.cost_krw * 100,
                    "fee_krw": fill.fee,                     # 매도 수수료만 (매수분은 아래서 보정)
                    "notional_krw": fill.notional,
                    "exit_kind": fill.kind,
                    "hold_bars": i - pos.entry_i,
                })
                pos = None

        # 2) 전략 판단 (현재 봉 종가 시점, 미래 안 봄)
        exec_idx = i + delay
        if exec_idx < len(df):
            order = strategy(df.iloc[: i + 1], pos)
            if order is not None:
                pending.append((exec_idx, order))

        # 3) 자산 평가 (현재 봉 종가)
        mark = cash + (pos.volume * float(bar.close) if pos else 0.0)
        equity_vals.append(mark)
        if pos:
            bars_in_market += 1

    # 마지막에 열려 있으면 종가로 강제 청산 (지표 정리용)
    unclosed = pos is not None
    if pos is not None:
        last = df.iloc[-1]
        f = fills.execute(Order("sell", "market", volume=pos.volume, tag="forced_close"),
                          last, fee_rate, slippage_bps)
        proceeds = f.notional - f.fee
        cash += proceeds
        pnl_krw = proceeds - pos.cost_krw
        trades.append({
            "entry_time": pos.entry_time, "exit_time": df.index[-1],
            "entry_price": pos.entry_price, "exit_price": f.price, "volume": pos.volume,
            "pnl_krw": pnl_krw, "pnl_pct": pnl_krw / pos.cost_krw * 100,
            "fee_krw": f.fee, "notional_krw": f.notional, "exit_kind": "forced_close",
            "hold_bars": len(df) - 1 - pos.entry_i,
        })
        equity_vals[-1] = cash

    # 매수 수수료를 각 트레이드에 반영 (매수 명목 * fee_rate 근사)
    for t in trades:
        buy_notional = t["entry_price"] * t["volume"]
        buy_fee = buy_notional * fee_rate
        t["fee_krw"] += buy_fee
        t["notional_krw"] += buy_notional

    equity = pd.Series(equity_vals, index=df.index)
    m = metrics.compute(equity, trades, bars_in_market)
    return Result(equity=equity, trades=trades, metrics=m, unclosed=unclosed,
                  _bars_in_market=bars_in_market)
