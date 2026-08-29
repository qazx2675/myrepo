"""M5 실제 확인 — 빗썸 공지/경보 + 뉴스 RSS 를 지금 조회해 본다.

    .venv\\Scripts\\python -m src.run_m5           # 조회만
    .venv\\Scripts\\python -m src.run_m5 --ntfy    # .env 의 NTFY_TOPIC 으로 테스트 알림 1건
"""

from __future__ import annotations

import sys

from src import news, notice, notifier


def main():
    print("=== 빗썸 공지 (최근) ===")
    try:
        for n in notice.fetch_notices(12):
            cats = ",".join(n.get("categories") or [])
            print(f"  [{cats}] {n.get('title', '')}")
    except Exception as e:
        print(f"  조회 실패: {e}")

    p = notice.poll()
    print(f"\n  → 즉시 청산 대상: {sorted(p['liquidate']) or '없음'}")
    print(f"  → 신규 진입 금지: {sorted(p['block']) or '없음'}")

    print("\n=== 뉴스 RSS 키워드 스캔 (유니버스 코인) ===")
    hs = news.fetch_headlines()
    print(f"  헤드라인 {len(hs)}건 수집")
    v = news.scan(hs)
    if not v:
        print("  위험 키워드 매칭 없음")
    for x in v:
        print(f"  [{x.level}] {x.market} ← '{x.keyword}' : {x.title[:70]}")

    if "--ntfy" in sys.argv:
        ok = notifier.notify("M5 테스트 알림입니다.", title="clipSend 코인봇",
                             priority="default", tags="test_tube")
        print(f"\nntfy 발송: {'성공' if ok else '실패 (NTFY_TOPIC 확인)'}")


if __name__ == "__main__":
    main()
