"""M1 수동 점검 스크립트.

    .venv\\Scripts\\python -m tests.test_m1              # 공개 조회만
    .venv\\Scripts\\python -m tests.test_m1 --private    # .env 키로 인증 조회 + 권한 검증

완료 기준 (계획서 9장 / 진행상황.md 5.3):
  [1] 캔들/호가/현재가 조회 성공
  [2] 잔고 조회 성공
  [3] 주문 API 가 권한 오류로 거부됨  <- 핵심
  [4] .env 없이 --private 실행하면 명확한 에러
  [5] 로그에 키 원문 노출 안 됨
"""

import logging
import sys

logging.basicConfig(level=logging.INFO, format="%(levelname)s %(name)s: %(message)s")

from src.bithumb_client import BithumbClient  # noqa: E402

MARKET = "KRW-BTC"


def check_public():
    print("\n[공개 조회]")
    price = BithumbClient.current_price(MARKET)
    print(f"  현재가 {MARKET}: {price:,}")

    df = BithumbClient.ohlcv(MARKET, interval="day", count=5)
    print(f"  일봉 {len(df)}개, 마지막 종가: {df['close'].iloc[-1]:,}")

    ob = BithumbClient.orderbook(MARKET)
    print(f"  호가 조회 OK (keys: {list(ob)[:3]}…)")

    warn = BithumbClient.virtual_asset_warning()
    print(f"  투자경보 마켓 {len(warn)}건")

    markets = BithumbClient.market_all()
    print(f"  전체 마켓 {len(markets)}개")
    print("  -> [1] 통과")


def check_private():
    print("\n[인증 조회]")
    cli = BithumbClient()  # .env 없으면 여기서 RuntimeError

    krw = cli.krw_balance()
    print(f"  KRW 잔고: {krw:,}")
    bal = cli.balances()
    print(f"  보유 자산 {len(bal)}종  -> [2] 통과")

    print("\n[키 권한 검증]")
    r = cli.probe_readonly()
    print(f"  {r['result']}: {r['detail']}")
    if r["result"] == "OK":
        print("  -> [3] 통과 — 조회 전용 키 확인됨")
    elif r["result"] == "FAIL":
        print("  -> [3] 실패 — 이 키는 주문이 가능합니다. 조회 전용 키를 새로 발급하세요.")
        sys.exit(1)
    else:
        print("  -> [3] 불확실 — 위 에러 메시지를 보고 직접 판단하세요.")
        sys.exit(2)


if __name__ == "__main__":
    check_public()
    if "--private" in sys.argv:
        check_private()
    else:
        print("\n(--private 붙이면 .env 키로 잔고 조회 + 권한 검증까지 수행)")
