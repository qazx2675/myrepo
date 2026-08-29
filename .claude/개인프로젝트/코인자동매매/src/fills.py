"""캔들 기반 체결 모델 (M3-a).

과거 호가 스냅샷은 빗썸이 제공하지 않는다. 백테스트에서는 봉(open/high/low)으로
체결을 근사한다. 실시간 호가 순차소진 체결은 M6 준비 단계의 몫이다.

체결 규칙 (order 가 실행되는 봉 = 신호 다음 봉):
  매수 시장가  : open * (1 + slip)
  매수 지정가 P: low <= P 이면 체결, 체결가 min(P, open)
  매도 시장가  : open * (1 - slip)
  매도 지정가 P: high >= P 이면 체결, 체결가 max(P, open)
  손절(stop) S : low <= S 이면 체결. 갭다운이면 open 에, 아니면 S 에. 그 뒤 slip 불리하게.

수수료는 양방향 모두 명목금액 * fee_rate. 매수는 "총 투입 KRW = 명목 + 수수료".
"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass
class Order:
    side: str                 # "buy" | "sell"
    kind: str                 # "market" | "limit" | "stop"
    krw: float = 0.0          # 매수: 총 투입 KRW (수수료 포함)
    volume: float = 0.0       # 매도: 매도 수량
    price: float = 0.0        # limit / stop 기준가
    tag: str = ""             # 기록용 (전략이 남기는 메모)


@dataclass
class Fill:
    price: float              # 체결 단가
    volume: float             # 체결 수량
    fee: float                # 수수료 (KRW)
    notional: float           # 명목금액 = price * volume (수수료 제외)
    kind: str                 # 체결된 주문 종류


def execute(order: Order, candle, fee_rate: float, slip_bps: float) -> Fill | None:
    """order 를 candle 에 대해 체결 시도. 미체결이면 None."""
    slip = slip_bps / 10_000.0
    o = float(candle.open)
    h = float(candle.high)
    lo = float(candle.low)

    if order.side == "buy":
        if order.kind == "market":
            px = o * (1 + slip)
        elif order.kind == "limit":
            if lo > order.price:
                return None
            px = min(order.price, o)
        else:
            raise ValueError(f"매수에 지원 안 되는 kind: {order.kind}")
        notional = order.krw / (1 + fee_rate)     # notional + notional*rate = krw
        fee = notional * fee_rate
        return Fill(px, notional / px, fee, notional, order.kind)

    if order.side == "sell":
        if order.kind == "market":
            px = o * (1 - slip)
        elif order.kind == "limit":
            if h < order.price:
                return None
            px = max(order.price, o)
        elif order.kind == "stop":
            if lo > order.price:
                return None
            px = min(o, order.price) * (1 - slip)
        else:
            raise ValueError(f"매도에 지원 안 되는 kind: {order.kind}")
        notional = order.volume * px
        fee = notional * fee_rate
        return Fill(px, order.volume, fee, notional, order.kind)

    raise ValueError(f"알 수 없는 side: {order.side}")
