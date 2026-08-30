"""페이퍼 매매 메인 루프 (M6).

지금까지의 부품(screener / risk / paper / notice / news / notifier / store)을 엮는다.
M6-a: PAPER_CAPITAL=1억, 2~4주.   M6-b: PAPER_CAPITAL=10만원, 1주.
같은 코드, settings.PAPER_CAPITAL 만 바꾼다.

**조회 전용 API 키만 쓴다.** paper 엔진이 가상 체결하므로 주문 권한이 필요 없다.
재시작해도 data/trades.db 에서 포지션/현금이 복원된다.

    .venv\\Scripts\\python -m src.live_loop --once        # 1회만 (점검용)
    .venv\\Scripts\\python -m src.live_loop                # 계속 (mode.screen_interval_min 주기)
    .venv\\Scripts\\python -m src.live_loop --reset        # DB 초기화 후 시작
"""

from __future__ import annotations

import logging
import os
import sys
import time
from datetime import datetime

from config import settings
from src import averaging, news, notice, notifier, paper, risk, screener, store
from src.fills import Order

log = logging.getLogger("live")


# ── 데이터 소스 (테스트에서 교체 가능) ────────────────────────────────
class LiveData:
    def __init__(self):
        from src.bithumb_client import BithumbClient
        from src import trader
        self._C = BithumbClient
        ok, why = trader.live_enabled()
        self._trader = trader.Trader(BithumbClient) if ok else None
        log.warning("체결 모드: %s (%s)", "실주문" if ok else "페이퍼", why)

    def now(self) -> datetime:
        return datetime.now()

    def prices(self, markets: list[str]) -> dict[str, float]:
        if not markets:
            return {}
        got = self._C.current_price(markets)
        return got if isinstance(got, dict) else {markets[0]: got}

    def top_by_volume(self, n: int) -> list[str]:
        mk = self._C.krw_markets()
        tk = self._C.tickers(mk)
        ranked = sorted(tk.values(), key=lambda t: t.get("acc_trade_price_24h", 0), reverse=True)
        return [t["market"] for t in ranked[:n]]

    def candles(self, market: str, count: int = 60):
        import python_bithumb as pb
        return pb.get_ohlcv(market, interval="day", count=count)

    def fill(self, order: Order, market: str):
        if self._trader is not None:
            return self._trader.execute(order, market)
        return paper.execute_live(order, market, client=self._C)

    def risk_events(self) -> dict:
        n, w = notice.poll(), news.poll()
        return {"liquidate": n["liquidate"] | w["liquidate"],
                "block": n["block"] | w["block"]}


# ── 봇 ────────────────────────────────────────────────────────────────
class Bot:
    def __init__(self, mode: str | None = None, data=None, db: str = store.DB_PATH):
        self.mode = risk.get_mode(mode)
        self.data = data or LiveData()
        self.db = db
        store.init(db)
        if store.get_meta("cash", None, db) is None:
            store.set_cash(settings.PAPER_CAPITAL, db)
            store.set_meta("mode", self.mode.name, db)
            store.set_meta("start_equity", repr(float(settings.PAPER_CAPITAL)), db)
            store.set_meta("started_at", self.data.now().isoformat(), db)

    # -- 청산 점검 --
    def _exits(self, ev: dict):
        cash = store.get_cash(self.db)
        positions = store.load_positions(self.db)
        prices = self.data.prices(list(positions))
        for m, p in list(positions.items()):
            px = prices.get(m)
            if px is None:
                continue
            p.peak_price = max(p.peak_price, px)
            order = reason = None
            if m in ev["liquidate"]:
                order, reason = Order("sell", "market", volume=p.volume, tag="warning"), "유의종목"
            elif px <= risk.stop_price(self.mode, p.entry_price, p.peak_price):
                order, reason = Order("sell", "market", volume=p.volume, tag="stop"), "손절"
            else:
                tp = risk.take_profit_order(self.mode, p.volume, p.entry_price, px, p.tp_done)
                if tp is not None:
                    order, _ = risk.validate_order(tp, px, full_volume=p.volume)
                    reason = "익절"
            if order is None:
                store.upsert_position(p, self.db)
                continue
            fill = self.data.fill(order, m)
            if fill is None:
                store.upsert_position(p, self.db)
                continue
            proceeds = fill.notional - fill.fee
            cash += proceeds
            sold = min(fill.volume, p.volume)
            frac = sold / p.volume
            cost_part = p.cost_krw * frac
            pnl = proceeds - cost_part
            store.record_trade({
                "market": m, "entry_ts": p.entry_ts, "exit_ts": self.data.now().isoformat(),
                "entry_price": p.entry_price, "exit_price": fill.price, "volume": sold,
                "pnl_krw": pnl, "pnl_pct": pnl / cost_part * 100 if cost_part else 0.0,
                "fee_krw": fill.fee, "notional_krw": fill.notional,
                "exit_kind": order.tag.split("+")[0],
            }, self.db)
            notifier.alert(f"{reason} {m}  {pnl:+,.0f}원 ({pnl / cost_part * 100:+.1f}%)"
                           if cost_part else f"{reason} {m}",
                           title=f"코인봇 {self.mode.name}")
            p.volume -= sold
            p.cost_krw -= cost_part
            if p.volume <= 1e-9:
                store.delete_position(m, self.db)
            else:
                if order.tag.startswith("take_profit"):
                    p.tp_done = True
                store.upsert_position(p, self.db)
        store.set_cash(cash, self.db)

    # -- 스크리닝 --
    def _pick(self, positions: dict, block: set[str]) -> str | None:
        best_m, best_s = None, settings.SCREEN_ENTRY_THRESHOLD
        for m in self.data.top_by_volume(self.mode.screen_top_n):
            if m in positions or m in block:
                continue
            hist = self.data.candles(m)
            if hist is None or len(hist) < settings.SCREEN_MOMENTUM_DAYS + 2:
                continue
            s, _, _ = screener.score(hist, self.mode.min_24h_value_krw)
            if s > best_s:
                best_m, best_s = m, s
        return best_m

    # -- 신규 진입 --
    def _entry(self, ev: dict):
        positions = store.load_positions(self.db)
        if len(positions) >= self.mode.max_positions:
            return
        cash = store.get_cash(self.db)
        prices = self.data.prices(list(positions))
        equity_now = cash + sum(p.volume * prices.get(m, p.entry_price)
                                for m, p in positions.items())
        day_start = float(store.get_meta("day_start_equity", repr(equity_now), self.db))
        if risk.daily_loss_exceeded(self.mode, day_start, equity_now):
            return
        if risk.fee_coupon_suspect(store.all_trades(self.db)):
            log.warning("실효 수수료율 경보 — 신규 진입 중단")
            return
        cand = self._pick(positions, ev["block"])
        if not cand:
            return
        krw = risk.entry_krw(self.mode, cash, len(positions))
        if krw <= 0:
            return
        fill = self.data.fill(Order("buy", "market", krw=krw, tag="entry"), cand)
        if fill is None:
            return
        cost = fill.notional + fill.fee
        store.set_cash(cash - cost, self.db)
        store.upsert_position(store.Position(cand, fill.volume, fill.price,
                                             self.data.now().isoformat(), cost, fill.price), self.db)
        notifier.alert(f"진입 {cand}  {cost:,.0f}원 @ {fill.price:,.4g}",
                       title=f"코인봇 {self.mode.name}")

    # -- 스냅샷 / 일일 롤오버 / 하트비트 --
    def _bookkeeping(self):
        now = self.data.now()
        positions = store.load_positions(self.db)
        prices = self.data.prices(list(positions))
        pv = sum(p.volume * prices.get(m, p.entry_price) for m, p in positions.items())
        cash = store.get_cash(self.db)
        store.record_equity(now.isoformat(), cash, pv, self.db)

        today = now.date().isoformat()
        if store.get_meta("day", None, self.db) != today:
            store.set_meta("day", today, self.db)
            store.set_meta("day_start_equity", repr(cash + pv), self.db)
        if now.hour >= settings.HEARTBEAT_HOUR \
                and store.get_meta("last_heartbeat_date", None, self.db) != today:
            store.set_meta("last_heartbeat_date", today, self.db)
            start = float(store.get_meta("start_equity", repr(cash + pv), self.db))
            notifier.heartbeat(
                f"[{self.mode.name}] 자산 {cash + pv:,.0f}원 "
                f"(시작 대비 {(cash + pv) / start - 1:+.2%}) / 보유 {len(positions)}종 "
                f"{sorted(positions) or '-'}")

    # -- 물타기 (M8): 제안만 자동, 실행은 사용자 승인 후 --
    def _averaging(self):
        positions = store.load_positions(self.db)
        prices = self.data.prices(list(positions))
        for m, p in positions.items():
            px = prices.get(m)
            if px is not None and averaging.should_propose(p, px, self.mode):
                averaging.propose(p, px, self.mode.name)

        approved = set(averaging.take_approved())
        if not approved:
            return
        cash = store.get_cash(self.db)
        positions = store.load_positions(self.db)
        for m in approved:
            p = positions.get(m)
            if p is None:
                averaging.clear_pending(m)
                continue
            room = self.mode.per_coin_max - p.cost_krw
            add = int(min(settings.AVG_ADD_FRAC * max(0.0, room), cash))
            if add < settings.MIN_ORDER_KRW:
                notifier.notify(f"{m} 물타기 승인됐지만 한도/현금 부족 (추가 {add:,}원)",
                                title=f"물타기 취소 [{self.mode.name}]", priority="default")
                averaging.clear_pending(m)
                continue
            fill = self.data.fill(Order("buy", "market", krw=add, tag="averaging"), m)
            if fill is None:
                continue
            cost = fill.notional + fill.fee
            new_vol = p.volume + fill.volume
            p.entry_price = (p.entry_price * p.volume + fill.price * fill.volume) / new_vol
            p.volume = new_vol
            p.cost_krw += cost
            p.peak_price = max(p.peak_price, fill.price)
            store.upsert_position(p, self.db)
            store.set_cash(cash - cost, self.db)
            cash -= cost
            averaging.clear_pending(m)
            notifier.alert(f"물타기 실행 {m}  +{cost:,.0f}원 @ {fill.price:,.4g}  "
                           f"평단 {p.entry_price:,.4g}", title=f"코인봇 {self.mode.name}")

    def tick(self):
        ev = self.data.risk_events()
        self._exits(ev)
        self._averaging()
        self._entry(ev)
        self._bookkeeping()

    def run(self, interval_sec: float, iterations: int | None = None):
        i = 0
        while iterations is None or i < iterations:
            try:
                self.tick()
            except Exception as e:                # noqa: BLE001 — 루프는 죽으면 안 됨
                log.exception("tick 실패: %s", e)
            i += 1
            if iterations is not None and i >= iterations:
                break
            time.sleep(interval_sec)


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    if "--reset" in sys.argv and os.path.exists(store.DB_PATH):
        os.remove(store.DB_PATH)
        print("DB 초기화됨")
    bot = Bot()
    print(f"모드 {bot.mode.name} / 자본 {store.get_cash():,.0f}원 / "
          f"보유 {sorted(store.load_positions())}")
    if "--once" in sys.argv:
        bot.tick()
        print("1회 실행 완료.")
    else:
        interval = bot.mode.screen_interval_min * 60
        print(f"{interval}s 주기로 계속 실행 (Ctrl+C 종료)")
        bot.run(interval)


if __name__ == "__main__":
    main()
