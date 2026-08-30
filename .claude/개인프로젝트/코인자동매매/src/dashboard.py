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


def _vol(n: float) -> str:
    if n >= 1:
        s = f"{n:,.4f}"
    elif n >= 0.0001:
        s = f"{n:.8f}"
    else:
        return f"{n:.4e}"
    return s.rstrip("0").rstrip(".")


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
    W = 86
    L = ["", "═" * W,
         f" {ts}   모드 {mode}   {'★실주문★' if live else '페이퍼'}",
         "═" * W,
         f" 현금            {_fmt(cash):>18} 원"]

    pos_value = 0.0
    if positions:
        L.append("")
        L.append(f" {'코인':<11}{'수량':>16}{'평단':>13}{'현재가':>13}{'투입':>12}{'평가액':>12}{'손익':>8}")
        L.append(" " + "─" * (W - 2))
        for m, p in sorted(positions.items()):
            px = prices.get(m, p.entry_price)
            val = p.volume * px
            pos_value += val
            pnl_pct = (val - p.cost_krw) / p.cost_krw * 100 if p.cost_krw else 0.0
            L.append(f" {m:<11}{_vol(p.volume):>16}{_px(p.entry_price):>13}{_px(px):>13}"
                     f"{_fmt(p.cost_krw):>12}{_fmt(val):>12}{pnl_pct:>+7.1f}%")
        L.append(" " + "─" * (W - 2))
    else:
        L.append("")
        L.append("  (보유 없음)")

    total = cash + pos_value
    invested = sum(p.cost_krw for p in positions.values())
    realized = sum(t["pnl_krw"] for t in trades)
    L += ["",
          f" 평가액          {_fmt(pos_value):>18} 원",
          f" 총 자산         {_fmt(total):>18} 원   시작 대비 {total / start - 1:+.2%}",
          f" 실현손익 누적   {realized:>+18,.0f} 원   ({len(trades)}건)"]
    if invested > 0:
        L.append(f" 투입 대비 손익  {(pos_value - invested):>+18,.0f} 원   "
                 f"({(pos_value / invested - 1):+.2%}, 투입 {_fmt(invested)}원)")
    L.append(f" 보유 {len(positions)}/{_max_pos(mode, db)}")
    cd = _cooldown(db, now or datetime.now())
    if cd:
        L.append(f" 재진입 쿨다운   {', '.join(sorted(cd))}")
    L += ["═" * W, ""]
    return "\n".join(L)


def _cooldown(db: str, now: datetime) -> set[str]:
    hrs = settings.REENTRY_COOLDOWN_HOURS
    if hrs <= 0:
        return set()
    latest = {}
    for t in store.all_trades(db):
        latest[t["market"]] = t.get("exit_ts")
    out = set()
    for m, ts in latest.items():
        try:
            when = datetime.fromisoformat(ts)
        except (ValueError, TypeError):
            continue
        if (now - when).total_seconds() < hrs * 3600:
            out.add(m)
    return out


def _max_pos(mode_name: str, db: str) -> int:
    v = store.get_meta("max_positions", None, db)
    if v is not None:
        return int(v)
    from src.risk import MODES
    m = MODES.get(mode_name)
    return m.max_positions if m else 2


def main():
    print(render())


if __name__ == "__main__":
    main()
