"""M5.5 실제 확인 — 실시간 호가로 주문 크기별 슬리피지를 본다.

    .venv\\Scripts\\python -m src.run_m55

'1억으로 알트코인 사면 슬리피지 생긴다'(계획서 2.3)를 숫자로 확인한다.
페이퍼 1억(M6-a) 이 이걸 무시하지 않는지 보는 용도.
"""

from __future__ import annotations

import python_bithumb as pb

from src.fills import Order
from src.paper import execute_against_book

MARKETS = ["KRW-BTC", "KRW-XRP", "KRW-DOGE"]
SIZES = [15_000, 1_000_000, 10_000_000, 100_000_000]


def main():
    for market in MARKETS:
        book = pb.get_orderbook(market)
        if isinstance(book, list):
            book = book[0]
        units = book["orderbook_units"]
        best_ask = float(units[0]["ask_price"])
        depth = sum(float(u["ask_price"]) * float(u["ask_size"]) for u in units)
        print(f"\n=== {market}  최우선 매도호가 {best_ask:,.4g}  (호가창 매도측 총 {depth:,.0f}원) ===")
        print(f"  {'주문':>14}  {'평균체결가':>14}  {'슬리피지':>9}  레벨  전량체결")
        for krw in SIZES:
            f = execute_against_book(Order("buy", "market", krw=krw), book)
            if f is None:
                print(f"  {krw:>14,}  체결 불가")
                continue
            slip_bps = (f.price / best_ask - 1) * 10_000
            print(f"  {krw:>14,}  {f.price:>14,.4g}  {slip_bps:>7.1f}bp  {f.levels_used:>4}  "
                  f"{'예' if f.filled_fully else '아니오(부분)'}")

    print("\n해석:")
    print("  - 상위권 코인은 호가창이 깊어 1억 매수도 슬리피지 1bp 미만 (BTC 는 레벨은 여러 개 소진).")
    print("  - 즉 1억 페이퍼의 진짜 함정은 '슬리피지'가 아니라 '최소주문 5,000원이 안 보이는 것'.")
    print("    → M6-a(1억) 만으로 실전 전환 금지. M6-b(10만원) 필수. (계획서 2.3)")
    print("  - 스크리닝 하위권(aggressive 상위 50위권) 알트는 이보다 슬리피지 큼. 그 코인 담을 땐 주의.")


if __name__ == "__main__":
    main()
