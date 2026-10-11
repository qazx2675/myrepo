#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""업무프로세스 ntfy 챗봇 수동 테스트 도우미 (표준 라이브러리만 사용).

  python 수동테스트.py            절차와 기대 결과 체크리스트 출력 (= show)
  python 수동테스트.py show       위와 같음
  python 수동테스트.py list       테스트 케이스 목록
  python 수동테스트.py send <이름> <케이스> [--yes]
                                  users.json 에서 <이름> 의 토픽을 읽어 테스트 질문을 ntfy 로 전송
                                  (사용자가 직접 실행합니다. 실행하면 실제로 ntfy 서버로 POST 합니다)

토픽은 출력에 앞 8자만 보여 줍니다.
"""
import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request

if hasattr(sys.stdout, "reconfigure"):  # Windows 콘솔/리다이렉트에서도 UTF-8 로 출력
    try:
        sys.stdout.reconfigure(encoding="utf-8")
        sys.stderr.reconfigure(encoding="utf-8")
    except Exception:
        pass

DEFAULT_USERS_FILE = r"D:\업무프로세스\users.json"
DEFAULT_NTFY_SERVER = "https://ntfy.sh"
BURST_GAP_SEC = 0.3

# 케이스: 이름 -> (설명, [전송할 메시지들])
CASES = {
    "long": (
        "긴 답변 유도 (3~5조각 예상, 조각당 약 1,000바이트). 포괄 질문 1건",
        ["OS 설치부터 환경설정 체크, 망변경까지 절차와 주의사항을 전부 자세히 정리해줘"],
    ),
    "short": (
        "짧게 끝나는 질문 (조각 1개 예상)",
        ["업무프로세스 문서는 어떤 내용을 다루는지 한 줄로 알려줘"],
    ),
    "reset": (
        "대화 맥락 초기화 명령 /reset",
        ["/reset"],
    ),
    "burst": (
        "질문 3개 연속 전송 (대기 안내 확인)",
        [
            "OS 설치 절차를 간단히 알려줘",
            "환경설정 체크 항목을 간단히 알려줘",
            "망변경 시 주의사항을 간단히 알려줘",
        ],
    ),
    "noanswer": (
        "문서 밖 질문 (거절 문구 예상)",
        ["내일 서울 날씨와 오늘 저녁 메뉴 추천해줘"],
    ),
    "feedback-guard": (
        "q_id 가 없는 good/bad 문장이 질문으로 처리되는지",
        ["good test"],
    ),
}

CHECKLIST = """\
============================================================
 업무프로세스 ntfy 챗봇 수동 테스트 절차
============================================================
준비
  [ ] 1. [1] 로컬_LLM_시작.bat 또는 [3] 챗봇_시작.bat 으로 Ollama 와 봇을 시작한다.
  [ ] 2. 봇 터미널에 설정 요약과 사용자별 토픽이 출력되고 대기 상태가 된다.
  [ ] 3. 휴대폰 ntfy 앱에서 테스트할 사용자의 토픽 1개만 구독한다.
  [ ] 4. 아래 케이스는 PC 에서 `python 수동테스트.py send <이름> <케이스>` 로 보내거나,
        휴대폰 앱에서 같은 문장을 직접 입력해도 된다. (`list` 로 문장 확인)

[A] long - 긴 답변 분할 표시
  보내기: python 수동테스트.py send <이름> long
  기대 결과 (봇 터미널)
  [ ] 질문이 들어오면 즉시 `[질문 수신] 사용자: … | 시각 | 질문: …` 가 출력된다.
        (LLM 답변이 끝나기 전에 나와야 한다)
  [ ] 답변 발행 때 `[ntfy 발행 성공] 조각 (i/N)` 이 N 번 출력된다.
  기대 결과 (휴대폰 앱, 위에서 아래로)
  [ ] 제목이 `답변 (1/N) - 질문 첫 줄`, `답변 (2/N) - 질문 첫 줄` ... 순서로 보인다.
  [ ] `AI 답변은 100% 정확하지 않습니다` 안내문이 (1/N) 에만 있고 (2/N) 이후에는 없다.
  [ ] 👍 좋음 / 👎 나쁨 버튼이 마지막 (N/N) 에만 있다. (1/N) 등에는 없다.
  [ ] 조각 간 발행 간격이 1.2초 이상이다 (봇 터미널 발행 로그 시각 또는 앱 도착 시각).
  [ ] 조각 내용이 순서대로 이어 읽힌다 (문장이 중간에서 뒤섞이지 않는다).

[B] short - 단일 메시지
  보내기: python 수동테스트.py send <이름> short
  [ ] 제목이 `답변 - 질문 첫 줄` 이다 (조각 번호 `(1/N)` 없음).
  [ ] 안내문(`AI 답변은 100% 정확하지 않습니다`)이 있고 👍/👎 버튼도 있다.

[C] 피드백 버튼
  [ ] [A] 또는 [B] 의 마지막 메시지에서 👍 좋음 을 누른다.
  [ ] 봇 터미널에 `[피드백 수신] 질문ID: … | 평가: good` 이 출력된다.
  [ ] logs\\feedback.jsonl 마지막 줄에 해당 질문ID 와 good 이 기록된다.
  [ ] 다른 답변에 👎 나쁨 을 눌러 bad 도 확인한다.

[D] burst - 연속 질문과 대기 안내
  보내기: python 수동테스트.py send <이름> burst
  [ ] 봇 터미널에 `[질문 수신]` 이 3건 즉시(답변을 기다리지 않고) 출력된다.
  [ ] 앱에 `접수되었습니다. 앞에 N건 대기 중입니다` 안내가 나온다.
  [ ] 답변은 도착 순서대로 한 건씩 나온다 (선입선출).

[E] reset - 맥락 초기화
  준비: 먼저 `OS 설치 절차 알려줘` 같은 질문을 하고, 이어서 `방금 말한 것 중 첫 단계만 다시 말해줘` 로 맥락이 이어지는지 본다.
  보내기: python 수동테스트.py send <이름> reset
  [ ] 대기 없이 바로 `대화 맥락을 초기화했습니다` 가 온다 (버튼 없음).
  [ ] 다시 `방금 말한 것 중 첫 단계만 다시 말해줘` 를 보내면 이전 맥락을 모른다는 취지로 답한다.

[F] noanswer - 문서 밖 질문
  보내기: python 수동테스트.py send <이름> noanswer
  [ ] `문서에 없는 내용입니다` 로 짧게 거절한다 (지어낸 답이 없다).
  [ ] logs\\unanswered.jsonl 에 기록된다.

[G] feedback-guard - good/bad 문장 오인 방지
  보내기: python 수동테스트.py send <이름> feedback-guard
  [ ] `good test` 는 피드백으로 처리되지 않고 일반 질문으로 접수된다
        (`[질문 수신]` 출력, 피드백 수신 로그 없음).
  [ ] 문서 밖 내용이므로 거절 또는 안내성 답변이 온다.

마무리
  [ ] logs\\errors.jsonl 에 새 오류 줄이 없다 (사용법.txt 의 로그 확인 명령).
  [ ] 이상이 있으면 해당 항목 번호와 봇 터미널 출력, 앱 화면을 함께 메모한다.
============================================================
"""


def find_users_file(explicit):
    if explicit:
        return explicit
    if os.path.isfile(DEFAULT_USERS_FILE):
        return DEFAULT_USERS_FILE
    here = os.path.dirname(os.path.abspath(__file__))
    for d in (here, os.path.dirname(here)):
        p = os.path.join(d, "users.json")
        if os.path.isfile(p):
            return p
    return DEFAULT_USERS_FILE  # 없으면 오류 메시지에 기본 경로를 보여 준다


def mask(topic):
    return topic[:8] + "…"


def cmd_show(_args):
    print(CHECKLIST)
    return 0


def cmd_list(_args):
    print("테스트 케이스 (python 수동테스트.py send <이름> <케이스>)")
    print("-" * 60)
    for name, (desc, msgs) in CASES.items():
        print(f" {name:<15} {desc}")
        for m in msgs:
            print(f"{'':<17}> {m}")
    return 0


def cmd_send(args):
    case = CASES.get(args.case)
    if not case:
        print(f"알 수 없는 케이스: {args.case} (list 로 목록 확인)")
        return 2
    path = find_users_file(args.users_file)
    try:
        with open(path, "r", encoding="utf-8") as f:
            users = json.load(f)
    except FileNotFoundError:
        print(f"users.json 을 찾을 수 없습니다: {path} (--users-file 로 지정)")
        return 2
    except Exception as e:
        print(f"users.json 읽기 실패: {e}")
        return 2
    if not isinstance(users, dict) or args.name not in users or not users[args.name]:
        names = ", ".join(users.keys()) if isinstance(users, dict) else "-"
        print(f"사용자 '{args.name}' 이(가) users.json 에 없습니다. 사용 가능한 이름: {names}")
        return 2
    topic = str(users[args.name])
    server = args.ntfy_server.rstrip("/")
    desc, msgs = case

    print(f"서버: {server}")
    print(f"사용자: {args.name} | 토픽: {mask(topic)} | 케이스: {args.case} ({desc})")
    print(f"전송 메시지 {len(msgs)}건:")
    for m in msgs:
        print(f"  > {m}")
    if not args.yes:
        ans = input("실제로 ntfy 로 전송합니다. 계속할까요? [y/N] ").strip().lower()
        if ans not in ("y", "yes"):
            print("취소했습니다.")
            return 1

    url = f"{server}/{topic}"
    for i, m in enumerate(msgs, 1):
        req = urllib.request.Request(url, data=m.encode("utf-8"), method="POST",
                                     headers={"Content-Type": "text/plain; charset=utf-8"})
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                resp.read()
            print(f"[전송 성공] ({i}/{len(msgs)}) {m}")
        except urllib.error.HTTPError as e:
            print(f"[전송 실패] ({i}/{len(msgs)}) HTTP {e.code}")
            return 3
        except Exception as e:
            print(f"[전송 실패] ({i}/{len(msgs)}) {type(e).__name__}")
            return 3
        if i < len(msgs):
            time.sleep(BURST_GAP_SEC)
    print("전송 완료. 봇 터미널과 휴대폰 앱에서 결과를 확인하세요 (show 의 해당 항목).")
    return 0


def main():
    p = argparse.ArgumentParser(description="업무프로세스 ntfy 챗봇 수동 테스트 도우미")
    p.add_argument("--users-file", help=f"users.json 경로 (기본 {DEFAULT_USERS_FILE}, 없으면 스크립트 폴더/상위 폴더)")
    p.add_argument("--ntfy-server", default=DEFAULT_NTFY_SERVER, help=f"ntfy 서버 (기본 {DEFAULT_NTFY_SERVER})")
    sub = p.add_subparsers(dest="cmd")
    sub.add_parser("show", help="수동 테스트 절차와 기대 결과 출력 (기본)")
    sub.add_parser("list", help="케이스 목록")
    sp = sub.add_parser("send", help="테스트 질문을 ntfy 로 전송 (실제 전송)")
    sp.add_argument("name", help="users.json 의 사용자 이름")
    sp.add_argument("case", help="케이스: " + ", ".join(CASES))
    sp.add_argument("--yes", "-y", action="store_true", help="전송 확인 프롬프트 생략")
    # 공통 옵션을 서브커맨드 뒤에 써도 받도록 허용
    for s in sub.choices.values():
        s.add_argument("--users-file", dest="users_file", default=argparse.SUPPRESS, help=argparse.SUPPRESS)
        s.add_argument("--ntfy-server", dest="ntfy_server", default=argparse.SUPPRESS, help=argparse.SUPPRESS)
    args = p.parse_args()
    cmd = args.cmd or "show"
    return {"show": cmd_show, "list": cmd_list, "send": cmd_send}[cmd](args)


if __name__ == "__main__":
    sys.exit(main())
