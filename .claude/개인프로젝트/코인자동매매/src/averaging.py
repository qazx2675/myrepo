"""물타기 (M8) — 절대 자동 실행 안 함. 사람 승인만 (불변 규칙 2).

흐름:
  1. 보유 포지션이 진입가 대비 AVG_TRIGGER_PCT 이하로 빠짐
     → propose(): 요청 파일 생성 + ntfy 알림 ("승인하려면 approve_averaging.bat KRW-XXX")
  2. 사용자가 approve_averaging.bat 실행 → data/averaging_approved.txt 에 마켓 추가
  3. 다음 틱에 take_approved() 가 그 마켓을 읽어 live_loop 이 추가 매수 실행
     (per_coin_max 한도 내에서만, 최소주문 검증 통과 시에만)

요청 파일이 이미 있으면 중복 제안하지 않는다. 승인은 1회성(읽으면 소비).
"""

from __future__ import annotations

import os

from config import settings
from src import notifier
from src.store import Position


def _req_path(market: str) -> str:
    return os.path.join(settings.AVG_REQUEST_DIR, market.replace("/", "_") + ".req")


def has_pending(market: str) -> bool:
    return os.path.exists(_req_path(market))


def clear_pending(market: str) -> None:
    p = _req_path(market)
    if os.path.exists(p):
        os.remove(p)


def trigger_pct(mode) -> float:
    """추적 손절보다 AVG_TRIGGER_MARGIN_PCT 만큼 덜 빠진 지점 (음수)."""
    return mode.trailing_stop_pct + settings.AVG_TRIGGER_MARGIN_PCT


def should_propose(p: Position, price: float, mode) -> bool:
    if has_pending(p.market):
        return False
    loss_pct = (price - p.entry_price) / p.entry_price * 100
    return loss_pct <= trigger_pct(mode)


def propose(p: Position, price: float, mode_name: str) -> None:
    os.makedirs(settings.AVG_REQUEST_DIR, exist_ok=True)
    loss_pct = (price - p.entry_price) / p.entry_price * 100
    with open(_req_path(p.market), "w", encoding="utf-8") as f:
        f.write(f"{p.market} entry={p.entry_price} now={price} loss={loss_pct:.1f}%\n")
    notifier.notify(
        f"{p.market} {loss_pct:.1f}% (진입 {p.entry_price:,.4g} → {price:,.4g})\n"
        f"물타기 승인하려면:  approve_averaging.bat {p.market}",
        title=f"물타기 제안 [{mode_name}]", priority="high", tags="question")


def take_approved() -> list[str]:
    """승인된 마켓 목록을 읽고 파일을 비운다 (소비)."""
    f = settings.AVG_APPROVED_FILE
    if not os.path.exists(f):
        return []
    with open(f, encoding="utf-8") as fp:
        markets = [ln.strip() for ln in fp if ln.strip()]
    open(f, "w", encoding="utf-8").close()
    return markets


def approve(market: str) -> None:
    """approve_averaging.bat 이 호출. 승인 파일에 추가."""
    os.makedirs(os.path.dirname(settings.AVG_APPROVED_FILE), exist_ok=True)
    with open(settings.AVG_APPROVED_FILE, "a", encoding="utf-8") as fp:
        fp.write(market.strip() + "\n")
    print(f"승인 기록됨: {market}. 다음 루프 틱에 반영됩니다.")


if __name__ == "__main__":
    import sys
    if len(sys.argv) != 2:
        print("사용법: python -m src.averaging KRW-XXX")
        sys.exit(1)
    approve(sys.argv[1])
