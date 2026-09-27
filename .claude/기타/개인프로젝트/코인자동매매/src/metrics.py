"""백테스트 성과 지표 (M3-a).

수익률만 보지 않는다. 철학상 중요한 것: MDD, 실효 수수료율, 거래빈도.
"""

from __future__ import annotations

from dataclasses import dataclass

import pandas as pd


@dataclass
class Metrics:
    start: str
    end: str
    bars: int
    total_return_pct: float
    cagr_pct: float
    mdd_pct: float                # 최대 낙폭 (음수)
    n_trades: int
    win_rate_pct: float
    avg_win_pct: float
    avg_loss_pct: float
    profit_factor: float          # 총이익 / 총손실 (절대값)
    total_fee_krw: float
    total_notional_krw: float
    effective_fee_rate: float     # total_fee / total_notional  (계획서 3.3)
    exposure_pct: float           # 포지션 보유 봉 비율
    n_stop_exits: int

    def as_rows(self) -> list[tuple[str, str]]:
        f = lambda x: f"{x:,.2f}"
        return [
            ("기간", f"{self.start} ~ {self.end} ({self.bars}봉)"),
            ("총수익률", f"{f(self.total_return_pct)} %"),
            ("CAGR", f"{f(self.cagr_pct)} %"),
            ("MDD", f"{f(self.mdd_pct)} %"),
            ("거래횟수", str(self.n_trades)),
            ("승률", f"{f(self.win_rate_pct)} %"),
            ("평균이익 / 평균손실", f"{f(self.avg_win_pct)} % / {f(self.avg_loss_pct)} %"),
            ("Profit Factor", f(self.profit_factor)),
            ("총 수수료", f"{f(self.total_fee_krw)} 원"),
            ("실효 수수료율", f"{self.effective_fee_rate * 100:.4f} %"),
            ("포지션 노출", f"{f(self.exposure_pct)} %"),
            ("손절 청산 횟수", str(self.n_stop_exits)),
        ]


def _mdd(equity: pd.Series) -> float:
    peak = equity.cummax()
    dd = (equity - peak) / peak
    return float(dd.min() * 100)


def compute(equity: pd.Series, trades: list[dict], bars_in_market: int) -> Metrics:
    eq0, eq1 = float(equity.iloc[0]), float(equity.iloc[-1])
    total_ret = (eq1 / eq0 - 1) * 100

    days = max((equity.index[-1] - equity.index[0]).days, 1)
    cagr = ((eq1 / eq0) ** (365.0 / days) - 1) * 100

    wins = [t["pnl_pct"] for t in trades if t["pnl_pct"] > 0]
    losses = [t["pnl_pct"] for t in trades if t["pnl_pct"] <= 0]
    gross_win = sum(t["pnl_krw"] for t in trades if t["pnl_krw"] > 0)
    gross_loss = -sum(t["pnl_krw"] for t in trades if t["pnl_krw"] <= 0)

    total_fee = sum(t["fee_krw"] for t in trades)
    total_notional = sum(t["notional_krw"] for t in trades)

    return Metrics(
        start=str(equity.index[0].date()),
        end=str(equity.index[-1].date()),
        bars=len(equity),
        total_return_pct=total_ret,
        cagr_pct=cagr,
        mdd_pct=_mdd(equity),
        n_trades=len(trades),
        win_rate_pct=(len(wins) / len(trades) * 100) if trades else 0.0,
        avg_win_pct=(sum(wins) / len(wins)) if wins else 0.0,
        avg_loss_pct=(sum(losses) / len(losses)) if losses else 0.0,
        profit_factor=(gross_win / gross_loss) if gross_loss else float("inf"),
        total_fee_krw=total_fee,
        total_notional_krw=total_notional,
        effective_fee_rate=(total_fee / total_notional) if total_notional else 0.0,
        exposure_pct=(bars_in_market / len(equity) * 100) if len(equity) else 0.0,
        n_stop_exits=sum(1 for t in trades if t["exit_kind"] == "stop"),
    )
