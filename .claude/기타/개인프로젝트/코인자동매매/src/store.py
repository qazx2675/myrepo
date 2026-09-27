"""SQLite 상태 저장 (M6) — 재시작해도 포지션/현금/손익이 복원되도록.

data/trades.db 한 파일. 표:
  meta       key/value   (cash, mode, started_at, last_heartbeat_date …)
  positions  보유 포지션  (market PK)
  trades     체결 완료된 왕복
  equity     자산 스냅샷 (ts PK)
"""

from __future__ import annotations

import os
import sqlite3
from contextlib import contextmanager
from dataclasses import dataclass

DB_PATH = os.path.join("data", "trades.db")


@dataclass
class Position:
    market: str
    volume: float
    entry_price: float
    entry_ts: str
    cost_krw: float
    peak_price: float
    tp_done: bool = False


@contextmanager
def _conn(path: str = DB_PATH):
    """트랜잭션 커밋 + 연결 닫기까지 (Windows 파일 잠금 방지)."""
    d = os.path.dirname(path)
    if d:
        os.makedirs(d, exist_ok=True)
    c = sqlite3.connect(path)
    c.row_factory = sqlite3.Row
    try:
        yield c
        c.commit()
    finally:
        c.close()


def init(path: str = DB_PATH) -> None:
    with _conn(path) as c:
        c.executescript("""
        CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT);
        CREATE TABLE IF NOT EXISTS positions(
            market TEXT PRIMARY KEY, volume REAL, entry_price REAL, entry_ts TEXT,
            cost_krw REAL, peak_price REAL, tp_done INTEGER DEFAULT 0);
        CREATE TABLE IF NOT EXISTS trades(
            id INTEGER PRIMARY KEY AUTOINCREMENT, market TEXT,
            entry_ts TEXT, exit_ts TEXT, entry_price REAL, exit_price REAL, volume REAL,
            pnl_krw REAL, pnl_pct REAL, fee_krw REAL, notional_krw REAL, exit_kind TEXT);
        CREATE TABLE IF NOT EXISTS equity(ts TEXT PRIMARY KEY, cash REAL, pos_value REAL, total REAL);
        """)


# ── meta ──────────────────────────────────────────────────────────────
def get_meta(key: str, default=None, path: str = DB_PATH):
    with _conn(path) as c:
        row = c.execute("SELECT value FROM meta WHERE key=?", (key,)).fetchone()
    return row["value"] if row else default


def set_meta(key: str, value, path: str = DB_PATH) -> None:
    with _conn(path) as c:
        c.execute("INSERT INTO meta(key,value) VALUES(?,?) "
                  "ON CONFLICT(key) DO UPDATE SET value=excluded.value", (key, str(value)))


def get_cash(path: str = DB_PATH) -> float:
    v = get_meta("cash", None, path)
    return float(v) if v is not None else 0.0


def set_cash(amount: float, path: str = DB_PATH) -> None:
    set_meta("cash", repr(float(amount)), path)


# ── positions ─────────────────────────────────────────────────────────
def load_positions(path: str = DB_PATH) -> dict[str, Position]:
    with _conn(path) as c:
        rows = c.execute("SELECT * FROM positions").fetchall()
    return {r["market"]: Position(r["market"], r["volume"], r["entry_price"], r["entry_ts"],
                                  r["cost_krw"], r["peak_price"], bool(r["tp_done"]))
            for r in rows}


def upsert_position(p: Position, path: str = DB_PATH) -> None:
    with _conn(path) as c:
        c.execute("""INSERT INTO positions(market,volume,entry_price,entry_ts,cost_krw,peak_price,tp_done)
                     VALUES(?,?,?,?,?,?,?)
                     ON CONFLICT(market) DO UPDATE SET
                       volume=excluded.volume, entry_price=excluded.entry_price,
                       entry_ts=excluded.entry_ts, cost_krw=excluded.cost_krw,
                       peak_price=excluded.peak_price, tp_done=excluded.tp_done""",
                  (p.market, p.volume, p.entry_price, p.entry_ts, p.cost_krw,
                   p.peak_price, int(p.tp_done)))


def delete_position(market: str, path: str = DB_PATH) -> None:
    with _conn(path) as c:
        c.execute("DELETE FROM positions WHERE market=?", (market,))


# ── trades / equity ───────────────────────────────────────────────────
def record_trade(t: dict, path: str = DB_PATH) -> None:
    with _conn(path) as c:
        c.execute("""INSERT INTO trades(market,entry_ts,exit_ts,entry_price,exit_price,volume,
                     pnl_krw,pnl_pct,fee_krw,notional_krw,exit_kind)
                     VALUES(:market,:entry_ts,:exit_ts,:entry_price,:exit_price,:volume,
                     :pnl_krw,:pnl_pct,:fee_krw,:notional_krw,:exit_kind)""", t)


def record_equity(ts: str, cash: float, pos_value: float, path: str = DB_PATH) -> None:
    with _conn(path) as c:
        c.execute("INSERT INTO equity(ts,cash,pos_value,total) VALUES(?,?,?,?) "
                  "ON CONFLICT(ts) DO UPDATE SET cash=excluded.cash, pos_value=excluded.pos_value, "
                  "total=excluded.total", (ts, cash, pos_value, cash + pos_value))


def all_trades(path: str = DB_PATH) -> list[dict]:
    with _conn(path) as c:
        return [dict(r) for r in c.execute("SELECT * FROM trades ORDER BY id").fetchall()]
