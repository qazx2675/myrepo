"""보유 현황 대시보드 (터미널 출력).

live_loop 이 매 틱 끝에 호출. 단독 실행도 가능:
    .venv\\Scripts\\python -m src.dashboard      # 지금 상태 1회 출력
"""

from __future__ import annotations

from datetime import datetime

from config import settings
from src import store, trader


def _fmt(n: float) -> str:
    return f"{n:,.0f}"


def _px(n: float) -> str:
    if n >= 100:
        return f"{n:,.0f}"
    if n >= 1:
        return f"{n:,.2f}"
    return f"{n:.6f}".rstrip("0")


def render(db: str = store.DB_PATH, prices: dict[str, float] | None = None,
           now: datetime | None = None) -> str:
    positions = store.load_positions(db)
    cash = store.get_cash(db)
    mode = store.get_meta("mode", settings.MODE, db) or settings.MODE
    start = float(store.get_meta("start_equity", repr(cash), db))
    trades = store.all_trades(db)
    live, _ = trader.live_enabled()

    if prices is None:
        prices = {}
        if positions:
            try:
                from src.bithumb_client import BithumbClient
                got = BithumbClient.current_price(list(positions))
                prices = got if isinstance(got, dict) else {list(positions)[0]: got}
            except Exception:
                prices = {}

    ts = (now or datetime.now()).strftime("%Y-%m-%d %H:%M")
    W = 74
    L = ["", "═" * W,
         f" {ts}   모드 {mode}   {'★실주문★' if live else '페이퍼'}",
         "═" * W,
         f" 현금            {_fmt(cash):>18} 원"]

    pos_value = 0.0
    if positions:
        L.append("")
        L.append(f" {'코인':<10}{'수량':>15}{'평단':>11}{'현재가':>11}{'투입':>13}{'평가액':>13}{'손익':>8}")
        L.append(" " + "─" * (W - 2))
        for m, p in sorted(positions.items()):
            px = prices.get(m, p.entry_price)
            val = p.volume * px
            pos_value += val
            pnl_pct = (val - p.cost_krw) / p.cost_krw * 100 if p.cost_krw else 0.0
            vol = f"{p.volume:,.4f}".rstrip("0").rstrip(".")
            L.append(f" {m:<10}{vol:>15}{_px(p.entry_price):>11}{_px(px):>11}"
                     f"{_fmt(p.cost_krw):>13}{_fmt(val):>13}{pnl_pct:>+7.1f}%")
        L.append(" " + "─" * (W - 2))
    else:
        L.append("")
        L.append("  (보유 없음)")

    total = cash + pos_value
    L += ["",
          f" 평가액          {_fmt(pos_value):>18} 원",
          f" 총 자산         {_fmt(total):>18} 원   시작 대비 {total / start - 1:+.2%}",
          f" 보유 {len(positions)}/{_mode_max(mode)}   ·   누적 거래 {len(trades)}건",
          "═" * W, ""]
    return "\n".join(L)


def _mode_max(mode_name: str) -> int:
    from src.risk import MODES
    m = MODES.get(mode_name)
    return m.max_positions if m else 2


def main():
    print(render())


if __name__ == "__main__":
    main()
