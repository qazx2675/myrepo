"""M5.5 테스트 — 실시간 호가 순차소진 체결 엔진.

    .venv\\Scripts\\python -m tests.test_m55
"""

from __future__ import annotations

from src.fills import Order
from src.paper import execute_against_book

# 3레벨 호가. ask: 100/101/102, bid: 99/98/97. 각 레벨 10개.
BOOK = {"orderbook_units": [
    {"ask_price": 100, "ask_size": 10, "bid_price": 99, "bid_size": 10},
    {"ask_price": 101, "ask_size": 10, "bid_price": 98, "bid_size": 10},
    {"ask_price": 102, "ask_size": 10, "bid_price": 97, "bid_size": 10},
]}
FEE = 0.0004


def approx(a, b, tol=1e-4):
    return abs(a - b) <= tol * max(1.0, abs(b))


def test_market_buy_single_level():
    # 500원 어치 → 첫 레벨(100 * 10 = 1000원 용량) 안에서 다 체결
    f = execute_against_book(Order("buy", "market", krw=500), BOOK, FEE)
    assert f.levels_used == 1 and approx(f.price, 100)
    assert approx(f.notional + f.fee, 500)          # 총 투입 = 명목 + 수수료
    assert approx(f.volume, f.notional / 100)
    assert f.filled_fully
    print("  market_buy_single_level OK")


def test_market_buy_walks_levels():
    # 2500원 → 레벨1(1000) + 레벨2(1010) + 레벨3 일부. 평균가 > 100
    f = execute_against_book(Order("buy", "market", krw=2500), BOOK, FEE)
    assert f.levels_used == 3
    assert f.price > 100 and f.price < 102
    print(f"  market_buy_walks_levels OK (avg {f.price:.3f}, {f.levels_used} levels)")


def test_market_buy_partial_when_book_thin():
    # 100만원 → 호가 전체(약 100*10+101*10+102*10=3030원)로도 부족 → 부분 체결
    f = execute_against_book(Order("buy", "market", krw=1_000_000), BOOK, FEE)
    assert not f.filled_fully and f.levels_used == 3
    print("  partial_when_book_thin OK")


def test_market_sell_walks_bids():
    f = execute_against_book(Order("sell", "market", volume=15), BOOK, FEE)
    assert f.levels_used == 2                        # 10 @ 99, 5 @ 98
    assert approx(f.price, (10 * 99 + 5 * 98) / 15)
    assert approx(f.fee, f.notional * FEE)
    print("  market_sell_walks_bids OK")


def test_limit_buy_no_fill_below_market():
    assert execute_against_book(Order("buy", "limit", krw=500, price=99), BOOK, FEE) is None
    print("  limit_buy_no_fill OK")


def test_limit_buy_caps_at_limit_price():
    # limit 100.5 → 레벨1(100)만 체결, 레벨2(101)는 제외
    f = execute_against_book(Order("buy", "limit", krw=5000, price=100.5), BOOK, FEE)
    assert f.levels_used == 1 and approx(f.price, 100)
    assert not f.filled_fully
    print("  limit_buy_caps OK")


def test_limit_sell_respects_price():
    f = execute_against_book(Order("sell", "limit", volume=100, price=98.5), BOOK, FEE)
    assert f.levels_used == 1 and approx(f.price, 99)   # 99 >= 98.5 만
    print("  limit_sell_respects_price OK")


def test_empty_book():
    assert execute_against_book(Order("buy", "market", krw=1000), {"orderbook_units": []}, FEE) is None
    print("  empty_book OK")


if __name__ == "__main__":
    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
    print("\n전부 통과")
