"""포트폴리오 백테스터 (M4) — 여러 코인 유니버스에서 리스크 규칙을 검증한다.

단일종목 backtest.py 로는 '동시 보유 2', '일일 손실 한도', '스크리너 순위 진입',
'추적 손절', '익절 부분매도' 같은 M4 규칙을 검증할 수 없다.

봉(일봉)마다:
  1) 자산 평가, 그날 시작 자산 기록
  2) 보유 포지션 청산 점검: 유의종목 → 손절(하드/추적) → 익절
  3) 일일 손실 한도 미도달 & 여유 슬롯 있으면 → 스크리너 상위 후보 1개 진입
  신호는 PF_EXEC_DELAY_BARS 봉 뒤 시가에 체결.

단순화 (명시):
  - 일봉 전용, 한 봉에 신규 진입 최대 1건
  - 물타기 없음 (M8)
  - 유의종목: 과거 데이터가 없어 backtest 에선 hook 만. 실제는 live 에서 notice.py.
"""

from __future__ import annotations

from dataclasses import dataclass, field

import pandas as pd

from config import settings
from src import fills, metrics, risk, screener
from src.fills import Order
from src.risk import Mode


@dataclass
class Pos:
    market: str
    volume: float
    entry_price: float
    entry_i: int
    cost_krw: float
    peak_price: float
    tp_done: bool = False


@dataclass
class PFResult:
    equity: pd.Series
    trades: list[dict]
    metrics: metrics.Metrics
    exits: dict = field(default_factory=dict)


def _rows_for(panel: dict[str, pd.DataFrame], i: int) -> dict[str, pd.Series]:
    return {m: df.iloc[i] for m, df in panel.items() if i < len(df)}


def run(panel: dict[str, pd.DataFrame], mode: Mode, initial_cash: float,
        use_pattern: bool = True, bearish_veto: bool = True,
        fee_rate: float | None = None, slippage_bps: float | None = None,
        liquidate_provider=None) -> PFResult:
    """liquidate_provider(date, held_markets) -> set[str]: 유의종목 등 즉시 청산 대상.
    실시간 루프에선 notice.poll()/news.poll() 결과를 넘긴다. 백테스트 기본값은 없음."""
    fee_rate = settings.BACKTEST_FEE_RATE if fee_rate is None else fee_rate
    slippage_bps = settings.FILL_SLIPPAGE_BPS if slippage_bps is None else slippage_bps
    delay = settings.PF_EXEC_DELAY_BARS

    idx = next(iter(panel.values())).index
    for df in panel.values():
        if not df.index.equals(idx):
            raise ValueError("모든 코인의 캔들 인덱스가 같아야 한다 (align 먼저)")
    n = len(idx)

    cash = float(initial_cash)
    positions: dict[str, Pos] = {}
    pending: list[tuple[int, str, Order]] = []
    trades: list[dict] = []
    equity_vals: list[float] = []
    exits = {"stop": 0, "take_profit": 0, "warning": 0, "forced_close": 0}

    def equity_at(i):
        e = cash
        for m, p in positions.items():
            if i < len(panel[m]):
                e += p.volume * float(panel[m].iloc[i].close)
        return e

    for i in range(n):
        day_start_equity = equity_at(i)

        # 1) 예약 주문 체결
        due = [(m, o) for (j, m, o) in pending if j == i]
        pending = [(j, m, o) for (j, m, o) in pending if j != i]
        for m, order in due:
            bar = panel[m].iloc[i]
            fill = fills.execute(order, bar, fee_rate, slippage_bps)
            if fill is None:
                continue
            if order.side == "buy":
                if m in positions or len(positions) >= mode.max_positions:
                    continue
                cost = fill.notional + fill.fee
                if cost > cash:
                    continue
                cash -= cost
                positions[m] = Pos(m, fill.volume, fill.price, i, cost, fill.price)
            else:
                p = positions.get(m)
                if p is None:
                    continue
                proceeds = fill.notional - fill.fee
                cash += proceeds
                sold = min(fill.volume, p.volume)
                frac = sold / p.volume
                cost_part = p.cost_krw * frac
                pnl = proceeds - cost_part
                trades.append({
                    "market": m, "entry_time": idx[p.entry_i], "exit_time": idx[i],
                    "entry_price": p.entry_price, "exit_price": fill.price, "volume": sold,
                    "pnl_krw": pnl, "pnl_pct": pnl / cost_part * 100 if cost_part else 0.0,
                    "fee_krw": fill.fee, "notional_krw": fill.notional,
                    "exit_kind": order.tag.split("+")[0], "hold_bars": i - p.entry_i,
                })
                exits[order.tag.split("+")[0]] = exits.get(order.tag.split("+")[0], 0) + 1
                p.volume -= sold
                p.cost_krw -= cost_part
                if p.volume <= 1e-12:
                    del positions[m]
                elif order.tag.startswith("take_profit"):
                    p.tp_done = True

        # 2) 청산 점검 (다음 봉 체결 예약)
        exec_i = i + delay
        liquidate = set()
        if liquidate_provider is not None:
            liquidate = liquidate_provider(idx[i], set(positions)) or set()
        for m, p in list(positions.items()):
            if i >= len(panel[m]):
                continue
            px = float(panel[m].iloc[i].close)
            p.peak_price = max(p.peak_price, px)
            if any(mm == m and oo.side == "sell" for (_, mm, oo) in pending):
                continue  # 이미 매도 예약됨
            if exec_i >= n:
                continue
            # 유의종목 즉시 청산 (불변 규칙 7) — 손절보다 우선
            if m in liquidate:
                pending.append((exec_i, m, Order("sell", "market", volume=p.volume, tag="warning")))
                continue
            # 손절
            sp = risk.stop_price(mode, p.entry_price, p.peak_price)
            if px <= sp:
                pending.append((exec_i, m, Order("sell", "market", volume=p.volume, tag="stop")))
                continue
            # 익절
            tp = risk.take_profit_order(mode, p.volume, p.entry_price, px, p.tp_done)
            if tp is not None:
                o2, _ = risk.validate_order(tp, px, full_volume=p.volume)
                if o2 is not None:
                    pending.append((exec_i, m, o2))

        # 3) 신규 진입
        cur_equity = equity_at(i)
        room = len(positions) < mode.max_positions
        if room and not risk.daily_loss_exceeded(mode, day_start_equity, cur_equity) \
                and not risk.fee_coupon_suspect(trades):
            best_m, best_s = None, settings.SCREEN_ENTRY_THRESHOLD
            for m, df in panel.items():
                if m in positions or i >= len(df):
                    continue
                if any(mm == m for (_, mm, _) in pending):
                    continue
                hist = df.iloc[: i + 1]
                s, _, _ = screener.score(hist, mode.min_24h_value_krw, use_pattern, bearish_veto)
                if s > best_s:
                    best_m, best_s = m, s
            if best_m and exec_i < n:
                krw = risk.entry_krw(mode, cash, len(positions))
                if krw > 0:
                    pending.append((exec_i, best_m, Order("buy", "market", krw=krw, tag="entry")))

        equity_vals.append(equity_at(i))

    # 강제 청산
    for m, p in list(positions.items()):
        bar = panel[m].iloc[-1]
        f = fills.execute(Order("sell", "market", volume=p.volume, tag="forced_close"),
                          bar, fee_rate, slippage_bps)
        proceeds = f.notional - f.fee
        cash += proceeds
        pnl = proceeds - p.cost_krw
        trades.append({
            "market": m, "entry_time": idx[p.entry_i], "exit_time": idx[-1],
            "entry_price": p.entry_price, "exit_price": f.price, "volume": p.volume,
            "pnl_krw": pnl, "pnl_pct": pnl / p.cost_krw * 100 if p.cost_krw else 0.0,
            "fee_krw": f.fee, "notional_krw": f.notional,
            "exit_kind": "forced_close", "hold_bars": n - 1 - p.entry_i,
        })
        exits["forced_close"] += 1
    if positions:
        equity_vals[-1] = cash
    positions.clear()

    # 매수 수수료 근사 반영
    for t in trades:
        bn = t["entry_price"] * t["volume"]
        t["fee_krw"] += bn * fee_rate
        t["notional_krw"] += bn

    equity = pd.Series(equity_vals, index=idx)
    bars_in_market = sum(1 for v in equity_vals if v)  # 근사: 항상 참 → 노출은 별도 계산 생략
    m = metrics.compute(equity, trades, bars_in_market=0)
    return PFResult(equity=equity, trades=trades, metrics=m, exits=exits)
