"""M4-a 테스트 — 리스크/자금관리.

    .venv\\Scripts\\python -m tests.test_m4a

특히: 최소 주문금액 5,000원 경계 (10만원 예산에서만 터지는 버그).
"""

from __future__ import annotations

from config import settings
from src import risk
from src.risk import AGGRESSIVE, CONSERVATIVE

C = CONSERVATIVE


def approx(a, b, tol=1e-6):
    return abs(a - b) <= tol * max(1.0, abs(b))


def test_mode_profiles():
    assert C.capital == 50_000 and C.reserve == 50_000
    assert C.per_coin_max == 25_000 and C.max_positions == 2
    assert AGGRESSIVE.capital == 100_000 and AGGRESSIVE.stop_loss_pct == -12.0
    print("  mode_profiles OK")


def test_entry_krw():
    # conservative 1차 = 25,000 * 0.60 = 15,000
    assert risk.entry_krw(C, cash_available=50_000, held_count=0) == 15_000
    # 이미 2개 보유 → 0
    assert risk.entry_krw(C, cash_available=50_000, held_count=2) == 0
    # 현금 부족 → 현금만큼, 단 최소 미만이면 0
    assert risk.entry_krw(C, cash_available=4_000, held_count=0) == 0
    assert risk.entry_krw(C, cash_available=6_000, held_count=0) == 6_000
    print("  entry_krw OK")


def test_stop_price():
    # 하드 손절만
    assert approx(risk.stop_price(C, 100.0), 93.0)            # -7%
    # 추적 손절이 더 높으면 그쪽 (고점 100 → -5% = 95 > 93)
    assert approx(risk.stop_price(C, 100.0, peak_price=100.0), 95.0)
    # 고점이 낮아 추적이 하드보다 아래면 하드 유지
    assert approx(risk.stop_price(C, 100.0, peak_price=96.0), 93.0)
    print("  stop_price OK")


def test_take_profit_partial_vs_full():
    # +10% 도달, 절반 매도. 보유 3.0개 @ 진입 100 → 현재 110, 절반=1.5개*110=165원 < 5000
    #   → 전량 매도로 승격
    o = risk.take_profit_order(C, volume=3.0, entry_price=100.0, current_price=110.0, already_done=False)
    assert o is not None and approx(o.volume, 3.0), "최소금액 미달 → 전량 승격돼야 함"
    # 큰 포지션이면 절반만
    o2 = risk.take_profit_order(C, volume=1000.0, entry_price=100.0, current_price=110.0, already_done=False)
    assert approx(o2.volume, 500.0)
    # 아직 익절선 전 → None
    assert risk.take_profit_order(C, 1000.0, 100.0, 105.0, False) is None
    # 이미 1회 → None
    assert risk.take_profit_order(C, 1000.0, 100.0, 120.0, True) is None
    print("  take_profit OK")


def test_daily_loss_limit():
    assert risk.daily_loss_exceeded(C, 50_000, 48_400)          # -3.2% <= -3%
    assert not risk.daily_loss_exceeded(C, 50_000, 49_000)      # -2%
    assert risk.daily_loss_exceeded(AGGRESSIVE, 100_000, 93_000)  # -7% <= -6%
    print("  daily_loss_limit OK")


def test_validate_order():
    from src.fills import Order
    # 매수 최소 미만 → 스킵
    assert risk.validate_order(Order("buy", "market", krw=3000), 100.0)[0] is None
    # 부분매도 최소 미만 + full_volume 주면 전량 승격
    o, why = risk.validate_order(Order("sell", "market", volume=10.0), current_price=100.0,
                                 full_volume=80.0)
    assert why == "promoted_to_full" and approx(o.volume, 80.0)
    # 부분매도 최소 미만 + 전량도 미만 → 스킵
    assert risk.validate_order(Order("sell", "market", volume=10.0), 100.0, full_volume=40.0)[0] is None
    # 정상
    assert risk.validate_order(Order("sell", "market", volume=100.0), 100.0)[1] == "ok"
    print("  validate_order OK")


def test_fee_monitor():
    good = [{"fee_krw": 4, "notional_krw": 10_000} for _ in range(6)]     # 0.04%
    bad = [{"fee_krw": 25, "notional_krw": 10_000} for _ in range(6)]     # 0.25%
    assert not risk.fee_coupon_suspect(good)
    assert risk.fee_coupon_suspect(bad)
    assert not risk.fee_coupon_suspect(bad[:3])                            # 표본 부족
    print("  fee_monitor OK")


def test_must_liquidate():
    assert risk.must_liquidate("KRW-XYZ", {"KRW-XYZ", "KRW-ABC"})
    assert not risk.must_liquidate("KRW-BTC", {"KRW-XYZ"})
    print("  must_liquidate OK")


if __name__ == "__main__":
    for name, fn in list(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
    print("\n전부 통과")
