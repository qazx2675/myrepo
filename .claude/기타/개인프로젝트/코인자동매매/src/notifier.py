"""ntfy 푸시 알림 (M5).

계획서 6장. OAuth·토큰 갱신 없음 — POST 한 번.
**알림 실패가 매매를 막지 않는다.** 예외는 삼키고 로그만.
"""

from __future__ import annotations

import logging
import os

import requests
from dotenv import load_dotenv

from config import settings

log = logging.getLogger("notifier")

_PRIO = {"min": 1, "low": 2, "default": 3, "high": 4, "urgent": 5}


def _config() -> tuple[str, str, str] | None:
    load_dotenv()
    topic = os.getenv("NTFY_TOPIC", "").strip()
    if not topic:
        return None
    token = os.getenv("NTFY_TOKEN", "").strip()
    return settings.NTFY_SERVER.rstrip("/"), topic, token


def notify(message: str, title: str | None = None, priority: str = "default",
           tags: str | None = None, actions: list[dict] | None = None) -> bool:
    """ntfy 로 발송. 성공 여부 반환 (실패해도 예외 안 냄).

    JSON 발행 방식을 쓴다. 헤더 방식은 한글 제목이 latin-1 로 안 넘어간다.
    actions: ntfy 액션 버튼 리스트 (http/view 등). 그대로 전달.
    """
    cfg = _config()
    if cfg is None:
        log.warning("NTFY_TOPIC 미설정 — 알림 생략: %s", title or message[:40])
        return False
    server, topic, token = cfg
    payload = {"topic": topic, "message": message, "priority": _PRIO.get(priority, 3)}
    if title:
        payload["title"] = title
    if tags:
        payload["tags"] = [t.strip() for t in tags.split(",") if t.strip()]
    if actions:
        payload["actions"] = actions
    headers = {"Authorization": f"Bearer {token}"} if token else {}
    try:
        r = requests.post(server, json=payload, headers=headers, timeout=5)
        r.raise_for_status()
        return True
    except Exception as e:                       # noqa: BLE001 — 매매를 막으면 안 됨
        log.error("ntfy 발송 실패 (%s): %s", type(e).__name__, e)
        return False


def alert(message: str, title: str = "⚠ 경보") -> bool:
    """손절/청산/유의종목 등 즉시 확인 필요."""
    return notify(message, title=title, priority="urgent", tags="warning")


def heartbeat(summary: str) -> bool:
    """일 1회 생존 신호. 안 오면 그 자체가 장애 신호."""
    return notify(summary, title="일일 상태", priority="low", tags="green_heart")
