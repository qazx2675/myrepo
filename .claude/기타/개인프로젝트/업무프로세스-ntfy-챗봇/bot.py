#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
업무프로세스 ntfy 챗봇 (bot.py)
- 휴대폰(ntfy) -> 집 PC 로컬 LLM(Ollama) -> ntfy 답변 + 피드백(👍/👎)
- 사용자별 개인 토픽(users.json), 선입선출 큐, 사용자별 최근 3턴 맥락(/reset)
- Python 표준 라이브러리만 사용 (외부 pip 의존성 없음)
"""

import sys
import os
import re
import time
import json
import uuid
import secrets
import queue
import hashlib
import threading
import subprocess
from collections import OrderedDict
from datetime import datetime, timezone, timedelta
import urllib.request
import urllib.error

# Windows 콘솔 UTF-8 출력 보정
if sys.platform == 'win32':
    try:
        sys.stdout.reconfigure(encoding='utf-8', line_buffering=True)
        sys.stderr.reconfigure(encoding='utf-8', line_buffering=True)
    except Exception:
        pass

# 한국 표준시 (KST) 타임존
KST = timezone(timedelta(hours=9))

def now_iso():
    return datetime.now(KST).isoformat()

# 기본 경로 설정
BASE_DIR = os.path.dirname(os.path.abspath(__file__))
DATA_DIR = os.path.abspath(os.path.join(BASE_DIR, ".."))
if not os.path.exists(os.path.join(DATA_DIR, "logs")) and os.path.exists(r"D:\업무프로세스"):
    DATA_DIR = r"D:\업무프로세스"
if os.environ.get("BOT_DATA_DIR"):  # 테스트 등에서 데이터 폴더를 따로 지정
    DATA_DIR = os.path.abspath(os.environ["BOT_DATA_DIR"])

DOC_PATH = os.path.join(DATA_DIR, "업무프로세스.md")
LOGS_DIR = os.path.join(DATA_DIR, "logs")
TESTS_DIR = os.path.join(DATA_DIR, "tests")
BACKUP_DIR = os.path.join(DATA_DIR, "백업")
STATE_FILE = os.path.join(DATA_DIR, "state.json")
ENV_FILE = os.path.join(DATA_DIR, ".env")
USERS_FILE = os.path.join(DATA_DIR, "users.json")

QA_LOG = os.path.join(LOGS_DIR, "qa.jsonl")
UNANSWERED_LOG = os.path.join(LOGS_DIR, "unanswered.jsonl")
FEEDBACK_LOG = os.path.join(LOGS_DIR, "feedback.jsonl")
ERRORS_LOG = os.path.join(LOGS_DIR, "errors.jsonl")
DOC_CHANGES_LOG = os.path.join(LOGS_DIR, "doc_changes.jsonl")

# 디렉터리 준비
for d in [LOGS_DIR, TESTS_DIR, BACKUP_DIR]:
    os.makedirs(d, exist_ok=True)

# .env 로드 및 기본 설정
def load_or_create_env():
    env_vars = {}
    if os.path.exists(ENV_FILE):
        try:
            with open(ENV_FILE, "r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if line and not line.startswith("#") and "=" in line:
                        k, v = line.split("=", 1)
                        env_vars[k.strip()] = v.strip().strip('"').strip("'")
        except Exception as e:
            print(f"[설정] .env 읽기 경고: {e}")

    updated = False
    if "NTFY_SERVER" not in env_vars:
        env_vars["NTFY_SERVER"] = "https://ntfy.sh"
        updated = True
    # 단일/분리 토픽은 .env 에 지정된 경우에만 사용(하위 호환).
    # 지정이 없으면 사용자별 개인 토픽 모드(users.json)로 동작한다.
    if "NTFY_TOPIC" in env_vars and env_vars["NTFY_TOPIC"]:
        single_t = env_vars["NTFY_TOPIC"]
        env_vars["NTFY_QUESTION_TOPIC"] = single_t
        env_vars["NTFY_ANSWER_TOPIC"] = single_t
        env_vars["NTFY_FEEDBACK_TOPIC"] = single_t
    if "NTFY_TOKEN" not in env_vars:
        env_vars["NTFY_TOKEN"] = ""
        updated = True
    if "OLLAMA_URL" not in env_vars:
        env_vars["OLLAMA_URL"] = "http://127.0.0.1:11434"
        updated = True
    if "MODEL" not in env_vars:
        env_vars["MODEL"] = "qwen3.5:9b-q8_0"
        updated = True
    if "NUM_CTX" not in env_vars:
        env_vars["NUM_CTX"] = "24576"
        updated = True
    if "KEEP_ALIVE" not in env_vars:
        env_vars["KEEP_ALIVE"] = "30m"
        updated = True
    if "TEMPERATURE" not in env_vars:
        env_vars["TEMPERATURE"] = "0.2"
        updated = True
    if "TOP_P" not in env_vars:
        env_vars["TOP_P"] = "0.8"
        updated = True
    if "TOP_K" not in env_vars:
        env_vars["TOP_K"] = "20"
        updated = True
    if "DOC_PATH" not in env_vars:
        env_vars["DOC_PATH"] = DOC_PATH
        updated = True
    if "THINK" not in env_vars:
        env_vars["THINK"] = "false"
        updated = True
    if "CONTEXT_TTL_SEC" not in env_vars:
        env_vars["CONTEXT_TTL_SEC"] = "600"
        updated = True
    if "ANSWER_RESERVE" not in env_vars:
        env_vars["ANSWER_RESERVE"] = "3000"
        updated = True
    if "NO_ANSWER_TEXT" not in env_vars:
        env_vars["NO_ANSWER_TEXT"] = "문서에 없는 내용입니다."
        updated = True

    if updated or not os.path.exists(ENV_FILE):
        try:
            with open(ENV_FILE, "w", encoding="utf-8") as f:
                f.write("# 업무프로세스 ntfy 챗봇 환경설정\n")
                for k, v in env_vars.items():
                    f.write(f"{k}={v}\n")
            print(f"[설정] .env 파일이 갱신되었습니다: {ENV_FILE}")
        except Exception as e:
            print(f"[설정] .env 파일 저장 실패: {e}")

    return env_vars

CONFIG = load_or_create_env()

NTFY_SERVER = CONFIG.get("NTFY_SERVER", "https://ntfy.sh").rstrip("/")
NTFY_TOPIC = CONFIG.get("NTFY_TOPIC", "")
if NTFY_TOPIC:
    NTFY_QUESTION_TOPIC = NTFY_TOPIC
    NTFY_ANSWER_TOPIC = NTFY_TOPIC
    NTFY_FEEDBACK_TOPIC = NTFY_TOPIC
else:
    NTFY_QUESTION_TOPIC = CONFIG.get("NTFY_QUESTION_TOPIC")
    NTFY_ANSWER_TOPIC = CONFIG.get("NTFY_ANSWER_TOPIC")
    NTFY_FEEDBACK_TOPIC = CONFIG.get("NTFY_FEEDBACK_TOPIC")

IS_SINGLE_TOPIC = bool(NTFY_QUESTION_TOPIC) and (NTFY_QUESTION_TOPIC == NTFY_ANSWER_TOPIC)
NTFY_TOKEN = CONFIG.get("NTFY_TOKEN", "")
MODEL = os.environ.get("MODEL") or CONFIG.get("MODEL", "qwen3.5:9b-q8_0")
for idx, arg in enumerate(sys.argv):
    if arg in ("--model", "-m") and idx + 1 < len(sys.argv):
        MODEL = sys.argv[idx + 1]
OLLAMA_URL = CONFIG.get("OLLAMA_URL", "http://127.0.0.1:11434").rstrip("/")
NUM_CTX = int(CONFIG.get("NUM_CTX", 24576))
KEEP_ALIVE = CONFIG.get("KEEP_ALIVE", "30m")
TEMPERATURE = float(CONFIG.get("TEMPERATURE", 0.2))
TOP_P = float(CONFIG.get("TOP_P", 0.8))
TOP_K = int(CONFIG.get("TOP_K", 20))
TARGET_DOC_PATH = CONFIG.get("DOC_PATH", DOC_PATH)
NO_ANSWER_TEXT = CONFIG.get("NO_ANSWER_TEXT", "문서에 없는 내용입니다.")

# think: "true"/"false" 이면 요청에 그대로 전송, 그 외(빈 값 등)는 보내지 않음
_think_raw = str(CONFIG.get("THINK", "false")).strip().lower()
THINK = {"true": True, "false": False}.get(_think_raw)
THINK_DROPPED = False  # 모델이 think 를 지원하지 않아 400 이 오면 True (이후 요청에서 생략)

CONTEXT_TTL_SEC = int(CONFIG.get("CONTEXT_TTL_SEC", 600))
ANSWER_RESERVE = int(CONFIG.get("ANSWER_RESERVE", 3000))
# 조각 발행 간격(초). ntfy 메시지 시각은 1초 단위라 같은 초에 들어가면 앱에서 순서가 뒤바뀔 수 있어 1.1초 이상 유지
CHUNK_INTERVAL = max(float(CONFIG.get("CHUNK_INTERVAL_SEC", 1.2)), 1.1)
HISTORY_TURNS = 3
DOC_PATHS_CFG = CONFIG.get("DOC_PATHS", "")  # 세미콜론 구분 다중 문서
DOC_DIR_CFG = CONFIG.get("DOC_DIR", "")      # 폴더 내 .md 전체
LEGACY_USER = "단일토픽"                      # 단일/분리 토픽 모드의 사용자 라벨

# 최근 질문/답변 캐시 (피드백 연결용)
QA_CACHE_MAX = 500
QA_CACHE = OrderedDict()  # q_id -> {"question": ..., "answer": ..., "user": ...} (최근 QA_CACHE_MAX 건만 유지)

def cache_qa(q_id, record):
    QA_CACHE[q_id] = record
    QA_CACHE.move_to_end(q_id)
    while len(QA_CACHE) > QA_CACHE_MAX:
        QA_CACHE.popitem(last=False)

# JSONL 헬퍼
LOG_LOCK = threading.Lock()

def append_jsonl(filepath, data):
    with LOG_LOCK:
        try:
            with open(filepath, "a", encoding="utf-8") as f:
                f.write(json.dumps(data, ensure_ascii=False) + "\n")
        except Exception as e:
            print(f"[로그 에러] {filepath} 쓰기 실패: {e}")

# 상태 파일 헬퍼 (마지막 처리 메시지 ID)
def load_state():
    if os.path.exists(STATE_FILE):
        try:
            with open(STATE_FILE, "r", encoding="utf-8") as f:
                return json.load(f)
        except Exception:
            pass
    return {"last_q_id": "", "last_fb_id": ""}

def save_state(state):
    tmp = STATE_FILE + ".tmp"
    try:
        # 임시파일에 쓴 뒤 교체: 쓰는 도중 중단돼도 기존 state.json 이 깨지지 않는다
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(state, f, ensure_ascii=False, indent=2)
        os.replace(tmp, STATE_FILE)
    except Exception as e:
        print(f"[상태 저장 실패] {e}")
        try:
            os.remove(tmp)
        except OSError:
            pass

# 사용자 토픽 (users.json: 이름 -> 토픽)
USERS = {}        # 이름 -> 토픽 (사용자별 개인 토픽 모드일 때만 채워짐)
USER_MODE = False

def load_users():
    try:
        with open(USERS_FILE, "r", encoding="utf-8") as f:
            data = json.load(f)
        if isinstance(data, dict):
            return {str(k): str(v) for k, v in data.items() if v}
    except Exception as e:
        print(f"[사용자] users.json 읽기 실패: {e}")
    return {}

def init_users():
    """users.json 이 있거나 USER_NAMES 가 지정되었거나 단일/분리 토픽 설정이 없으면 개인 토픽 모드.
    users.json 이 없으면 USER_NAMES(기본 사용자1..사용자7) 기준으로 자동 생성한다."""
    global USERS, USER_MODE
    USERS, USER_MODE = {}, False
    has_file = os.path.exists(USERS_FILE)
    explicit_names = bool(CONFIG.get("USER_NAMES", "").strip())
    legacy_topic = bool(NTFY_TOPIC) or bool(
        CONFIG.get("NTFY_QUESTION_TOPIC") and CONFIG.get("NTFY_ANSWER_TOPIC"))
    if not (has_file or explicit_names or not legacy_topic):
        return
    if has_file:
        USERS = load_users()
    if not USERS:
        names = [n.strip() for n in CONFIG.get("USER_NAMES", "").split(",") if n.strip()]
        if not names:
            names = [f"사용자{i}" for i in range(1, 8)]
        USERS = {n: "proc-" + secrets.token_urlsafe(16) for n in names}
        try:
            with open(USERS_FILE, "w", encoding="utf-8") as f:
                json.dump(USERS, f, ensure_ascii=False, indent=2)
        except Exception as e:
            print(f"[사용자] users.json 저장 실패: {e}")
    USER_MODE = True

init_users()

# 사용자별 대화 맥락 (최근 3턴, TTL 후 초기화, /reset)
CONTEXTS = {}  # 사용자 -> {"turns": [(질문, 답변)], "ts": 마지막 갱신 시각, "epoch": 초기화 횟수}
CONTEXT_LOCK = threading.Lock()

def _now():
    return time.time()

def _ctx(user):
    return CONTEXTS.setdefault(user, {"turns": [], "ts": 0.0, "epoch": 0})

def get_history(user):
    with CONTEXT_LOCK:
        c = _ctx(user)
        if c["turns"] and _now() - c["ts"] > CONTEXT_TTL_SEC:
            c["turns"] = []
        return list(c["turns"])

def get_epoch(user):
    with CONTEXT_LOCK:
        return _ctx(user)["epoch"]

def add_turn(user, question, answer, epoch):
    with CONTEXT_LOCK:
        c = _ctx(user)
        if c["epoch"] != epoch:  # 접수 이후 /reset 이 있었으면 저장하지 않음
            return
        if c["turns"] and _now() - c["ts"] > CONTEXT_TTL_SEC:
            c["turns"] = []
        c["turns"].append((question, answer))
        c["turns"] = c["turns"][-HISTORY_TURNS:]
        c["ts"] = _now()

def reset_context(user):
    with CONTEXT_LOCK:
        c = _ctx(user)
        c["turns"] = []
        c["epoch"] += 1

# 문서 읽기 (질문마다 다시 읽어 변경사항 즉시 반영)
def read_document_raw(path=None):
    """(텍스트, 바이트 수, sha256) 반환. 읽지 못하면 ("", 0, "")"""
    if path is None:
        path = TARGET_DOC_PATH
    if not os.path.exists(path):
        # 대안으로 .txt 도 시도
        txt_alt = path.replace(".md", ".txt")
        if os.path.exists(txt_alt):
            path = txt_alt

    try:
        with open(path, "rb") as f:
            raw = f.read()
    except Exception as e:
        print(f"[문서 읽기 오류] {path}: {e}")
        return "", 0, ""
    for enc in ["utf-8", "cp949", "euc-kr"]:
        try:
            return raw.decode(enc), len(raw), hashlib.sha256(raw).hexdigest()
        except UnicodeDecodeError:
            continue
    return raw.decode("utf-8", errors="replace"), len(raw), hashlib.sha256(raw).hexdigest()

def read_document(path=None):
    return read_document_raw(path)[0]

def doc_paths():
    """DOC_PATHS(세미콜론) > DOC_DIR(폴더 내 .md) > DOC_PATH(단일) 순으로 문서 경로 목록 결정"""
    if DOC_PATHS_CFG.strip():
        return [p.strip() for p in DOC_PATHS_CFG.split(";") if p.strip()]
    if DOC_DIR_CFG.strip() and os.path.isdir(DOC_DIR_CFG.strip()):
        d = DOC_DIR_CFG.strip()
        return [os.path.join(d, n) for n in sorted(os.listdir(d)) if n.lower().endswith(".md")]
    return [TARGET_DOC_PATH]

def estimate_tokens(text):
    # 대략치: UTF-8 바이트 / 3
    return int(round(len(text.encode("utf-8")) / 3.0))

# '## 제목' / '### 제목' 단위 절 분할 (코드 블록 안은 제목으로 보지 않음)
SECTION_HEADING_RE = re.compile(r"^#{2,3}\s+(.+?)\s*$")

def _is_gloss_header(line):
    s = line.strip()
    if not s.startswith("|"):
        return False
    cells = [re.sub(r"[*`]", "", c).strip() for c in s.strip("|").split("|")]
    return len(cells) >= 2 and cells[0] == "약어" and cells[1] == "정의"

def split_sections(text):
    """[{"title", "text", "glossary"}] - 첫 제목 이전 내용은 title="" 인 도입부 절"""
    sections = [{"title": "", "lines": []}]
    in_code = False
    for line in text.splitlines():
        if line.lstrip().startswith("```"):
            in_code = not in_code
        m = None if in_code else SECTION_HEADING_RE.match(line)
        if m:
            sections.append({"title": m.group(1), "lines": [line]})
        else:
            sections[-1]["lines"].append(line)
    out = []
    for s in sections:
        body = "\n".join(s["lines"]).strip("\n")
        if not body.strip():
            continue
        out.append({"title": s["title"], "text": body,
                    "glossary": any(_is_gloss_header(l) for l in s["lines"])})
    return out

def parse_glossary(doc_text):
    """'| 약어 | 정의 | ... |' 표의 행을 [(약어, 정의, 비고)] 로 반환 (코드에 사전 상수 없음)"""
    def clean(s):
        return re.sub(r"[*`]", "", s).strip()
    rows = []
    lines = doc_text.splitlines()
    i = 0
    while i < len(lines):
        if _is_gloss_header(lines[i]):
            i += 2  # 헤더 + 구분선
            while i < len(lines) and lines[i].strip().startswith("|"):
                c = [clean(x) for x in lines[i].strip().strip("|").split("|")]
                if len(c) >= 2 and c[0]:
                    rows.append((c[0], c[1], c[2] if len(c) > 2 else ""))
                i += 1
            continue
        i += 1
    return rows

def _term_aliases(term):
    al = {term}
    if " " in term:
        first = term.split()[0]
        if len(first) >= 3:
            al.add(first)
    return al

def _term_in_question(alias, q):
    if re.fullmatch(r"[A-Za-z0-9_.\-]+", alias):
        return re.search(r"(?<![A-Za-z0-9])" + re.escape(alias) + r"(?![A-Za-z0-9])", q, re.I) is not None
    return alias.lower() in q.lower()

def gloss_question(question, glossary):
    """질문에 등장한 용어표 약어의 정의를 덧붙인다. 없으면 원문 그대로."""
    found = []
    for term, definition, note in glossary:
        if any(_term_in_question(a, question) for a in _term_aliases(term)):
            d = definition + (f" ({note})" if note else "")
            found.append(f"{term}: {d}")
    if not found:
        return question
    return question + "\n\n[용어 참고] " + " / ".join(found)

# 문서 캐시 (파일 해시가 바뀔 때만 재적재)
DOC_STATE = {"sig": None, "docs": [], "glossary": []}

def _title_lines(titles):
    return "\n".join(f"- {n}: {t}" for n, t in titles)

def _all_titles(docs):
    return [(n, s["title"]) for n, t in docs for s in split_sections(t) if s["title"]]

def _full_tokens(docs):
    """문서 전체를 주입할 때의 프롬프트 토큰 추정 (문서 + 고정 문구 + 제목 목록 + 파일 표지)"""
    base = estimate_tokens(build_system_prompt("", "-", partial=True))
    return (sum(estimate_tokens(t) for _, t in docs) + base
            + estimate_tokens(_title_lines(_all_titles(docs))) + 20 * len(docs))

def refresh_documents():
    """문서를 읽어 해시를 비교하고, 변경 시 재적재 + logs/doc_changes.jsonl 기록.
    반환: ([(파일명, 텍스트)], 용어표)"""
    docs, h, total = [], hashlib.sha256(), 0
    for p in doc_paths():
        text, nbytes, sha = read_document_raw(p)
        if not nbytes:
            continue
        docs.append((os.path.basename(p), text))
        h.update(os.path.basename(p).encode("utf-8") + sha.encode("ascii"))
        total += nbytes
    sig = h.hexdigest()
    if sig != DOC_STATE["sig"]:
        first = DOC_STATE["sig"] is None
        glossary = []
        for _, t in docs:
            glossary.extend(parse_glossary(t))
        DOC_STATE.update({"sig": sig, "docs": docs, "glossary": glossary})
        tokens = sum(estimate_tokens(t) for _, t in docs)
        over = _full_tokens(docs) + ANSWER_RESERVE > NUM_CTX
        append_jsonl(DOC_CHANGES_LOG, {
            "time": now_iso(),
            "event": "load" if first else "change",
            "files": [n for n, _ in docs],
            "bytes": total,
            "tokens_est": tokens,
            "sha256": sig,
            "over_budget": over
        })
        print(f"[문서 {'적재' if first else '변경 감지'}] {len(docs)}개 파일, {total:,}B, 토큰추정 {tokens:,}")
        if over:
            msg = f"문서 토큰추정({tokens:,})이 NUM_CTX({NUM_CTX}) 예산을 초과하여 절 선택 모드로 동작합니다."
            print(f"[경고] {msg}")
            append_jsonl(ERRORS_LOG, {"time": now_iso(), "error": msg})
    return DOC_STATE["docs"], DOC_STATE["glossary"]

def _query_terms(question):
    terms = set()
    for t in re.findall(r"[0-9A-Za-z가-힣]+", question.lower()):
        if len(t) < 2:
            continue
        terms.add(t)
        if len(t) >= 3:
            terms.add(t[:-1])  # 조사 제거 근사
        if len(t) >= 4:
            terms.add(t[:-2])
    return {t for t in terms if len(t) >= 2}

def _section_score(sec, terms):
    title, body = sec["title"].lower(), sec["text"].lower()
    score = 0
    for t in terms:
        if t in title:
            score += 3
        if t in body:
            score += 1
    return score

def _history_tokens(history):
    return sum(estimate_tokens(q) + estimate_tokens(a) for q, a in (history or []))

def assemble_documents(docs, question, history=None):
    """프롬프트에 넣을 문서 구성. 반환 (문서문자열, [(파일명, 제목)], 부분발췌여부)
    question 은 용어 참고가 붙은 최종 질문, history 는 함께 전송될 최근 턴 [(질문, 답변)]"""
    def fmt(name, text):
        return f"[문서 파일: {name}]\n{text}"

    # 문서 외에 같이 전송되는 질문(+용어 참고)·이전 대화 토큰을 모두 차감한다
    extra = estimate_tokens(question) + _history_tokens(history)
    if _full_tokens(docs) + extra + ANSWER_RESERVE <= NUM_CTX:
        return "\n\n".join(fmt(n, t) for n, t in docs), _all_titles(docs), False

    # 절 선택 모드: 용어 사전 표 절은 항상 포함, 나머지는 질문과의 키워드 겹침 점수순
    fixed = estimate_tokens(build_system_prompt("", "-", partial=True)) + 200
    budget = max(NUM_CTX - ANSWER_RESERVE - fixed - extra - 20 * len(docs), 500)
    terms = _query_terms(question)
    cands = []
    for n, t in docs:
        for s in split_sections(t):
            cands.append({"file": n, "order": len(cands), "sec": s,
                          "score": _section_score(s, terms),
                          "tok": estimate_tokens(s["text"]) + 10
                                 + estimate_tokens(_title_lines([(n, s["title"])]))})
    chosen = [c for c in cands if c["sec"]["glossary"]]
    used = sum(c["tok"] for c in chosen)
    ranked = sorted((c for c in cands if not c["sec"]["glossary"] and c["score"] > 0),
                    key=lambda c: (-c["score"], c["order"]))
    picked = False
    for c in ranked:
        if used + c["tok"] <= budget:
            chosen.append(c)
            used += c["tok"]
            picked = True
    if not picked:
        # 점수가 없거나 한 절이 예산보다 크면 첫 후보를 잘라서라도 넣는다
        pool = ranked or [c for c in cands if not c["sec"]["glossary"]]
        if pool:
            c = dict(pool[0])
            remain_chars = max(budget - used, 300)  # 한글은 1자≈1토큰이므로 토큰 예산을 그대로 문자 수로 사용
            c["sec"] = dict(c["sec"], text=c["sec"]["text"][:remain_chars])
            chosen.append(c)
    chosen.sort(key=lambda c: c["order"])
    parts, titles = [], []
    for n, _ in docs:
        mine = [c for c in chosen if c["file"] == n]
        if not mine:
            continue
        parts.append(fmt(n, "\n\n".join(c["sec"]["text"] for c in mine)))
        titles.extend((n, c["sec"]["title"]) for c in mine if c["sec"]["title"])
    return "\n\n".join(parts), titles, True

# 시스템 프롬프트 구성 (서술형. 문서 내용·업무명·고정 예시 답안을 코드에 두지 않는다)
def build_system_prompt(doc_text, titles="", partial=False):
    partial_note = ""
    if partial:
        partial_note = "\n- 지침서는 질문과 관련 있어 보이는 절만 발췌되어 있습니다. 발췌된 내용에서 근거를 찾지 못하면 지침서에서 확인되지 않는다고 답합니다."
    ref_rule = "- 답변 마지막 줄에 '📌 참고: ' 다음에 근거가 된 문서 파일명과 절 제목을 쉼표로 적습니다."
    title_block = ""
    if titles:
        ref_rule += " 제목은 아래 [문서 제목 목록]에 있는 것만 사용합니다."
        title_block = f"\n\n[문서 제목 목록]\n{titles}"
    return f"""당신은 아래 [지침서]를 충분히 이해하고 있는 사내 동료 엔지니어입니다. 동료가 메신저로 물어보면 지침서 내용만을 근거로, 사람이 설명해 주듯 자연스러운 말투로 답해 주세요. 문서를 그대로 복사하지 말고 질문의 의도에 맞게 풀어서 설명합니다.

[지침서]
===
{doc_text}
===

[답변 원칙]
- 지침서에 없는 내용은 지어내지 않습니다. 일반 상식이나 외부 지식으로 빈틈을 메우지 마세요.
- 질문의 일부만 지침서에 근거가 있으면, 확인된 부분은 그대로 답하고 "문서에 규정이 없는 부분:" 으로 시작하는 한 줄로 어떤 부분이 없는지 밝힙니다.
- 지침서와 전혀 관련 없는 질문은 정중히 거절합니다. 거절할 때는 "문서에 없는 내용입니다"를 포함해 한두 문장으로 짧게 답합니다.
- 질문이 여러 개이면 하나씩 나누어 빠짐없이 답합니다. 짧은 질문, 약어, 오타는 지침서 맥락에서 가장 자연스러운 뜻으로 이해합니다. 앞선 대화가 있으면 이어서 답합니다.
- 질문 끝에 [용어 참고]가 붙어 있으면 지침서 용어표에서 가져온 뜻이므로 질문을 이해하는 데 사용합니다.
- 조항 기호(§)와 '§2.1' 같은 표기는 본문에 쓰지 않습니다.
- 간결하게, 핵심 위주로 답하고 순서가 있는 절차는 번호를 붙입니다.
{ref_rule}{partial_note}{title_block}"""

CLAUSE_REF_RE = re.compile(r"§\s*\d+(?:\.\d+)*")

def _is_pure_citation(inner):
    """§번호(들)와 제목/구분자뿐이면 True. 설명 문장이 섞여 있으면 False."""
    rest = re.sub(r"\s*[,;·/~]?\s*" + CLAUSE_REF_RE.pattern, "", inner).strip()
    if len(rest) > 40 or re.search(r"[.,:;!?]|(?:다|요)$", rest):
        return False
    return True

# 조항 기호 인용 제거: '§' 로 시작하는 순수 인용 괄호는 전체를, 설명이 섞인 괄호는 § 번호만 지운다 (다른 괄호는 유지)
def strip_clause_refs(text):
    if "§" not in text:
        return text
    pairs = {"(": ")", "（": "）", "[": "]"}
    out = []
    i, n = 0, len(text)
    while i < n:
        c = text[i]
        if c in pairs:
            j = i + 1
            while j < n and text[j] in " \t":
                j += 1
            if j < n and text[j] == "§":
                close, depth, k = pairs[c], 1, i + 1
                while k < n and depth:
                    if text[k] == c:
                        depth += 1
                    elif text[k] == close:
                        depth -= 1
                    elif text[k] == "\n":
                        break
                    k += 1
                if depth == 0:
                    if _is_pure_citation(text[i + 1:k - 1]):
                        while out and out[-1] in " \t":
                            out.pop()
                        i = k
                        continue
                    # 괄호 안에 인용 외 설명이 더 있으면 괄호와 설명은 두고 § 번호만 제거
                    out.append(c)
                    i = j
                    while True:
                        m = CLAUSE_REF_RE.match(text, i)
                        if not m:
                            break
                        i = m.end()
                        while i < n and text[i] in " \t,;":
                            i += 1
                    continue
        out.append(c)
        i += 1
    s = "".join(out)
    s = re.sub(r"[ \t]*§\s*\d+(?:\.\d+)*", "", s)  # 괄호 밖 '§2.1' 표기
    s = s.replace("§", "")
    s = re.sub(r"[ \t,]+\)", ")", s)
    s = re.sub(r"\(\s*,\s*", "(", s)
    return s

THINK_TAG_RE = re.compile(r"<think>.*?</think>", re.S)

# Ollama LLM 호출
def query_llm(question, doc_text=None, history=None):
    global THINK_DROPPED
    doc_mode = "full"
    llm_question = question
    if doc_text is None:
        docs, glossary = refresh_documents()
        llm_question = gloss_question(question, glossary)
        assembled, titles, partial = assemble_documents(docs, llm_question, history)
        system_prompt = build_system_prompt(assembled, _title_lines(titles), partial)
        doc_mode = "sections" if partial else "full"
    else:
        system_prompt = build_system_prompt(doc_text)

    messages = [{"role": "system", "content": system_prompt}]
    for hq, ha in (history or []):
        messages.append({"role": "user", "content": hq})
        messages.append({"role": "assistant", "content": ha})
    messages.append({"role": "user", "content": llm_question})

    req_body = {
        "model": MODEL,
        "messages": messages,
        "options": {
            "num_ctx": NUM_CTX,
            "temperature": TEMPERATURE,
            "top_p": TOP_P,
            "top_k": TOP_K
        },
        "keep_alive": KEEP_ALIVE,
        "stream": False
    }
    if THINK is not None and not THINK_DROPPED:
        req_body["think"] = THINK

    t0 = time.time()
    data = None
    err = None
    for _attempt in range(2):
        req = urllib.request.Request(
            f"{OLLAMA_URL}/api/chat",
            data=json.dumps(req_body).encode("utf-8"),
            headers={"Content-Type": "application/json; charset=utf-8"}
        )
        try:
            with urllib.request.urlopen(req, timeout=180) as resp:
                data = json.loads(resp.read().decode("utf-8"))
            break
        except urllib.error.HTTPError as e:
            if e.code == 400 and "think" in req_body:
                # 모델이 think 를 지원하지 않음 -> 제거 후 재시도, 이후 요청에도 생략
                req_body.pop("think", None)
                THINK_DROPPED = True
                print("[알림] 모델이 think 옵션을 지원하지 않아 제거하고 재시도합니다.")
                continue
            err = e
            break
        except Exception as e:
            err = e
            break

    elapsed = time.time() - t0
    if data is None:
        return {
            "success": False,
            "error": str(err),
            "elapsed_sec": elapsed,
            "answer": "",
            "prompt_eval_count": 0,
            "eval_count": 0,
            "unanswered": True,
            "doc_mode": doc_mode
        }

    msg = data.get("message", {}).get("content") or ""
    msg = THINK_TAG_RE.sub("", msg).strip()
    prompt_tokens = data.get("prompt_eval_count", 0)
    eval_tokens = data.get("eval_count", 0)

    # 본문 내 § 조항 인용 자동 제거 (2중 안전장치)
    msg = strip_clause_refs(msg)

    # 90% num_ctx 토큰 초과 경고
    if prompt_tokens > int(NUM_CTX * 0.9):
        print(f"[경고] 프롬프트 토큰 수({prompt_tokens})가 num_ctx({NUM_CTX})의 90%를 초과했습니다!")

    unanswered = False
    rejection_phrases = [NO_ANSWER_TEXT, "문서에 없는 내용", "명시되어 있지 않습니다", "내용이 없습니다", "문서에 없습니다"]
    if any(p in msg for p in rejection_phrases):
        unanswered = True

    return {
        "success": True,
        "answer": msg,
        "prompt_eval_count": prompt_tokens,
        "eval_count": eval_tokens,
        "elapsed_sec": elapsed,
        "unanswered": unanswered,
        "doc_mode": doc_mode
    }

def _send_ntfy_payload(payload, idx, total_chunks, label=""):
    headers = {"Content-Type": "application/json; charset=utf-8"}
    if NTFY_TOKEN:
        headers["Authorization"] = f"Bearer {NTFY_TOKEN}"

    try:
        req = urllib.request.Request(
            NTFY_SERVER,
            data=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
            headers=headers,
            method="POST"
        )
        with urllib.request.urlopen(req, timeout=15) as resp:
            _ = resp.read()
        print(f"[ntfy 발행 성공] 조각 ({idx}/{total_chunks}) -> {label} | 제목: {payload.get('title', '')}")
    except Exception as e:
        print(f"[ntfy 발행 실패] 조각 ({idx}/{total_chunks}): {e}")

# ntfy 발행 (메시지 분할 및 역순 발행)
# topic: 발행할 토픽(사용자 개인 토픽). 생략하면 단일/분리 토픽 모드의 답변 토픽.
# label: 로그에 남길 사용자 이름 (토픽은 로그에 남기지 않음)
# title_fn: (idx, total) -> 조각 제목. 지정하면 title 대신 사용
# header: 첫 조각 맨 앞에만 붙일 안내문 (조각 경계에서 잘리거나 단독 조각이 되지 않음)
def publish_ntfy(answer_text, q_id, title="🤖 답변", topic=None, label=None, feedback=True,
                 title_fn=None, header=""):
    answer_topic = topic or NTFY_ANSWER_TOPIC
    feedback_topic = topic or NTFY_FEEDBACK_TOPIC
    label = label or LEGACY_USER
    # 4096바이트 / 한글 약 1300자 분할 처리
    max_chunk_chars = 1200
    chunks = []

    if len(header) + len(answer_text) <= max_chunk_chars:
        chunks = [answer_text]
    else:
        lines = answer_text.split("\n")
        cur_chunk = ""
        for line in lines:
            cap = max_chunk_chars - (len(header) if not chunks else 0)  # 첫 조각은 안내문 길이만큼 덜 채움
            if len(cur_chunk) + len(line) + 1 > cap:
                if cur_chunk:
                    chunks.append(cur_chunk.strip())
                cur_chunk = line + "\n"
            else:
                cur_chunk += line + "\n"
        if cur_chunk.strip():
            chunks.append(cur_chunk.strip())
    if header:
        chunks[0] = header + chunks[0]

    total_chunks = len(chunks)

    def feedback_actions():
        return [
            {
                "action": "http",
                "label": "👍 좋음",
                "url": f"{NTFY_SERVER}/{feedback_topic}",
                "body": f"good {q_id}"
            },
            {
                "action": "http",
                "label": "👎 나쁨",
                "url": f"{NTFY_SERVER}/{feedback_topic}",
                "body": f"bad {q_id}"
            }
        ]

    if total_chunks == 1:
        payload = {
            "topic": answer_topic,
            "title": title_fn(1, 1) if title_fn else title,
            "message": chunks[0],
            "tags": ["bot"]
        }
        if feedback:
            payload["actions"] = feedback_actions()
        _send_ntfy_payload(payload, 1, 1, label)
    else:
        # 모바일 앱 피드는 최신 메시지가 상단에 위치하므로,
        # 역순 (마지막 조각 -> 1번 조각)으로 전송하여
        # 사용자가 위에서 아래로(1 -> 2 -> 3) 자연스럽게 읽을 수 있도록 배치
        indexed_chunks = list(enumerate(chunks, 1))
        for idx, chunk in reversed(indexed_chunks):
            chunk_title = title_fn(idx, total_chunks) if title_fn else f"{title} ({idx}/{total_chunks})"
            payload = {
                "topic": answer_topic,
                "title": chunk_title,
                "message": chunk,
                "tags": ["bot"]
            }
            if idx == total_chunks and feedback:
                payload["actions"] = feedback_actions()
            _send_ntfy_payload(payload, idx, total_chunks, label)
            time.sleep(CHUNK_INTERVAL)

# 사용자에게 보내는 오류 문구 (상세 원인은 errors.jsonl 에만 기록)
ERROR_REPLY_TEXT = "처리 중 오류가 발생했습니다. 잠시 후 다시 시도해 주세요"

def _first_line(text):
    """공백 제거 후 비어 있지 않은 첫 줄 (없으면 "")"""
    return next((l.strip() for l in (text or "").splitlines() if l.strip()), "")

# 답변 조각 제목: '답변 (i/N) - 질문 첫 줄(최대 30자)'
def build_answer_title(question, idx, total):
    q = _first_line(question)
    if len(q) > 30:
        q = q[:30] + "…"
    head = "답변" if total == 1 else f"답변 ({idx}/{total})"
    return f"{head} - {q}" if q else head

# 봇 답변 제목 형식 (tags 외 2차 자기 메시지 판별용)
ANSWER_TITLE_RE = re.compile(r"^답변(?: \(\d+/\d+\))?(?: - |$)")

# 질문 처리 메인 로직
def process_question(msg_id, question_text, user=None, topic=None, epoch=None):
    user = user or LEGACY_USER
    q_id = msg_id if msg_id else str(uuid.uuid4())[:8]
    if epoch is None:
        epoch = get_epoch(user)
    print(f"\n[질문 처리 시작] ID: {q_id} | 사용자: {user} | 질문: {_first_line(question_text)}")

    history = get_history(user)
    res = query_llm(question_text, history=history)
    if not res["success"]:
        print(f"[LLM 에러] ID: {q_id} | {res['error']}")
        append_jsonl(ERRORS_LOG, {
            "id": q_id,
            "user": user,
            "time": now_iso(),
            "question": question_text,
            "error": res["error"]
        })
        publish_ntfy(ERROR_REPLY_TEXT, q_id, title="오류 발생",
                     topic=topic, label=user)
        return

    ans = res["answer"]
    elapsed = res["elapsed_sec"]
    unanswered = res["unanswered"]
    p_tokens = res["prompt_eval_count"]
    e_tokens = res["eval_count"]

    print(f"[답변 생성 완료] 소요시간: {elapsed:.2f}s | 토큰: {p_tokens}+{e_tokens} | 미답변: {unanswered}")
    print(f"[답변 내용]\n{ans}\n")

    # 캐시 저장 (피드백 연계용)
    cache_qa(q_id, {"question": question_text, "answer": ans, "user": user})
    add_turn(user, question_text, ans, epoch)

    # 로그 기록
    append_jsonl(QA_LOG, {
        "id": q_id,
        "user": user,
        "time": now_iso(),
        "question": question_text,
        "answer": ans,
        "unanswered": unanswered,
        "prompt_eval_count": p_tokens,
        "eval_count": e_tokens,
        "elapsed_sec": round(elapsed, 2),
        "history_turns": len(history),
        "doc_mode": res.get("doc_mode", "full")
    })

    if unanswered:
        append_jsonl(UNANSWERED_LOG, {
            "id": q_id,
            "user": user,
            "time": now_iso(),
            "question": question_text
        })

    # 답변 윗줄에 주의사항 헤더 추가
    disclaimer = (
        "**AI 답변은 100% 정확하지 않습니다.**\n"
        f"**같은 대화의 최근 {HISTORY_TURNS}턴까지 맥락이 이어집니다. 새 주제는 /reset 으로 초기화하세요"
        f" ({max(CONTEXT_TTL_SEC // 60, 1)}분 이상 질문이 없으면 자동 초기화).**\n\n"
    )
    # ntfy 로 전송 (해당 사용자 토픽으로만). 안내문은 첫 조각에만 붙는다
    publish_ntfy(ans, q_id, topic=topic, label=user, header=disclaimer,
                 title_fn=lambda i, n: build_answer_title(question_text, i, n))

# 선입선출 큐 (전 사용자 공용, 단일 워커)
JOB_QUEUE = queue.Queue()
QUEUE_LOCK = threading.Lock()
OUTSTANDING = 0   # 처리 중 + 대기 중 건수
_worker_thread = None

def _worker_loop():
    global OUTSTANDING
    while True:
        job = JOB_QUEUE.get()
        try:
            process_question(*job)
        except Exception as e:
            print(f"[큐 워커 에러] {e}")
            append_jsonl(ERRORS_LOG, {"time": now_iso(), "user": job[2], "error": f"worker: {e}"})
            try:  # 사용자에게는 고정 문구만 (예외 원문 미포함, 발행 실패는 무시)
                publish_ntfy(ERROR_REPLY_TEXT, job[0] or "", title="오류 발생",
                             topic=job[3], label=job[2], feedback=False)
            except Exception:
                pass
        finally:
            with QUEUE_LOCK:
                OUTSTANDING -= 1
            JOB_QUEUE.task_done()

def ensure_worker():
    global _worker_thread
    with QUEUE_LOCK:
        if _worker_thread is None or not _worker_thread.is_alive():
            _worker_thread = threading.Thread(target=_worker_loop, daemon=True)
            _worker_thread.start()

def enqueue_question(msg_id, text, user, topic):
    """질문을 큐에 넣고, 앞에 대기가 있으면 해당 사용자 토픽으로 즉시 안내한다."""
    global OUTSTANDING
    ensure_worker()
    epoch = get_epoch(user)
    with QUEUE_LOCK:
        ahead = OUTSTANDING
        OUTSTANDING += 1
        JOB_QUEUE.put((msg_id, text, user, topic, epoch))
    if ahead > 0:
        print(f"[접수] 사용자: {user} | 앞에 {ahead}건 대기")
        publish_ntfy(f"접수되었습니다. 앞에 {ahead}건 대기 중입니다", msg_id or "", title="접수 안내",
                     topic=topic, label=user, feedback=False)

# 피드백 대상 질문 조회: 캐시 우선, 없으면 qa.jsonl 역순 탐색. 없으면 None
def lookup_qa(q_id):
    if not q_id:
        return None
    if q_id in QA_CACHE:
        return QA_CACHE[q_id]
    if os.path.exists(QA_LOG):
        try:
            with open(QA_LOG, "r", encoding="utf-8") as f:
                lines = f.readlines()
            for line in reversed(lines):
                try:
                    record = json.loads(line)
                    if record.get("id") == q_id:
                        return record
                except Exception:
                    continue
        except Exception:
            pass
    return None

def is_own_feedback(msg_text, user):
    """'good|bad <질문ID>' 형태이고 해당 사용자의 답변 ID 일 때만 피드백으로 본다."""
    parts = msg_text.strip().split()
    if len(parts) != 2 or parts[0].lower() not in ("good", "bad"):
        return False
    rec = lookup_qa(parts[1])
    return rec is not None and rec.get("user") == user

# 피드백 처리 메인 로직
def process_feedback(msg_id, fb_text, user=None):
    parts = fb_text.strip().split()
    if not parts:
        return
    user = user or LEGACY_USER
    rating = parts[0].lower()  # "good" or "bad"
    target_q_id = parts[1] if len(parts) > 1 else ""

    question = ""
    answer = ""
    rec = lookup_qa(target_q_id)
    if rec is not None:
        if rec.get("user") != user:
            # 다른 사용자의 질문ID 에 대한 평가는 기록하지 않는다 (내용은 로그에도 남기지 않음)
            print(f"[피드백 무시] 질문ID: {target_q_id} | 사용자: {user} | 사용자 불일치")
            return
        question = rec.get("question", "")
        answer = rec.get("answer", "")

    print(f"[피드백 수신] 질문ID: {target_q_id} | 사용자: {user} | 평가: {rating}")
    append_jsonl(FEEDBACK_LOG, {
        "id": target_q_id,
        "user": user,
        "time": now_iso(),
        "rating": rating,
        "question": question,
        "answer": answer
    })

# 이벤트 디스패처
def accept_question(msg_id, msg_text, user, topic):
    """/reset 은 즉시 처리하고, 그 외 질문은 큐에 넣는다."""
    if msg_text.strip().lower() == "/reset":
        reset_context(user)
        print(f"[맥락 초기화] 사용자: {user}")
        publish_ntfy("대화 맥락을 초기화했습니다", msg_id or "", title="초기화",
                     topic=topic, label=user, feedback=False)
        return
    lines = msg_text.strip().splitlines()
    shown = "\n".join([lines[0]] + ["    " + l for l in lines[1:]])  # 여러 줄이면 둘째 줄부터 들여쓰기
    print(f"\n[질문 수신] 사용자: {user} | {datetime.now(KST).strftime('%H:%M:%S')} | 질문: {shown}")
    enqueue_question(msg_id, msg_text, user, topic)

def handle_single_topic_event(msg_id, msg_text, event, user=None, topic=None):
    """
    단일 토픽/개인 토픽 모드용 통합 핸들러:
    - 봇이 보낸 답변은 무시 (무한 루프 방지)
    - good/bad 피드백 메시지는 process_feedback으로 전달
    - 사용자 일반 질문은 큐(accept_question)로 전달
    """
    user = user or LEGACY_USER
    tags = event.get("tags") or []
    title = event.get("title") or ""

    # 1. 봇 자신이 보낸 메시지인지 확인
    if ("bot" in tags or title.startswith("🤖") or title in ["오류 발생", "워밍업"]
            or ANSWER_TITLE_RE.match(title)):
        return

    # 2. 피드백 메시지인지 확인 (👍/👎 버튼: 'good|bad <내 답변 ID>' 일 때만. 그 외는 일반 질문)
    if is_own_feedback(msg_text, user):
        process_feedback(msg_id, msg_text, user)
        return

    # 3. 빈 메시지 무시
    if not msg_text.strip():
        return

    # 4. 사용자 질문 처리
    accept_question(msg_id, msg_text, user, topic)

def handle_question_only(msg_id, msg_text, event):
    """분리 토픽 모드: 질문 처리 (봇 자체 발행 메시지 제외)"""
    tags = event.get("tags") or []
    if "bot" in tags:
        return
    if not msg_text.strip():
        return
    accept_question(msg_id, msg_text, LEGACY_USER, None)

def handle_feedback_only(msg_id, msg_text, event):
    """분리 토픽 모드: 피드백 처리"""
    if not msg_text.strip():
        return
    process_feedback(msg_id, msg_text)

def make_user_handler(user, topic):
    def handler(msg_id, msg_text, event):
        handle_single_topic_event(msg_id, msg_text, event, user=user, topic=topic)
    return handler

# ntfy 스트림 구독 워커 (질문 토픽 & 피드백 토픽)
STATE_LOCK = threading.Lock()

def subscribe_stream(topic, on_message, last_id_key, label=None):
    label = label or "구독"
    state = load_state()
    last_id = state.get(last_id_key, "")

    backoff = 5
    while True:
        url = f"{NTFY_SERVER}/{topic}/json"
        if last_id:
            url += f"?since={last_id}"

        print(f"[{label} 구독 시작]")
        headers = {}
        if NTFY_TOKEN:
            headers["Authorization"] = f"Bearer {NTFY_TOKEN}"

        try:
            req = urllib.request.Request(url, headers=headers)
            with urllib.request.urlopen(req, timeout=120) as resp:
                backoff = 5
                for raw_line in resp:
                    if not raw_line:
                        continue
                    line_str = raw_line.decode("utf-8").strip()
                    if not line_str:
                        continue
                    try:
                        event = json.loads(line_str)
                    except json.JSONDecodeError:
                        continue

                    if event.get("event") == "message":
                        msg_id = event.get("id", "")
                        msg_text = event.get("message", "")

                        # 중복 처리 방지
                        if msg_id and msg_id == last_id:
                            continue

                        if msg_id:
                            last_id = msg_id
                            with STATE_LOCK:
                                st = load_state()
                                st[last_id_key] = msg_id
                                save_state(st)

                        on_message(msg_id, msg_text, event)
        except urllib.error.HTTPError as e:
            if e.code == 400 and last_id:
                print(f"[{label} 알림] 이전 메시지 ID({last_id})가 만료되었거나 유효하지 않아 400 오류 발생. 상태를 초기화하고 최신 스트림으로 자동 전환합니다.")
                last_id = ""
                with STATE_LOCK:
                    st = load_state()
                    st[last_id_key] = ""
                    save_state(st)
                time.sleep(1)
                continue
            print(f"[{label} HTTP 에러] {e}. {backoff}초 후 재연결...")
            time.sleep(backoff)
            backoff = min(backoff * 2, 300)
        except Exception as e:
            print(f"[{label} 연결 종료 또는 에러] {e}. {backoff}초 후 재연결...")
            time.sleep(backoff)
            backoff = min(backoff * 2, 300)

def _topic_text(topic):
    """콘솔에는 토픽을 마스킹해 출력한다 (SHOW_TOPICS=true 일 때만 전체 출력)."""
    show = str(os.environ.get("SHOW_TOPICS") or CONFIG.get("SHOW_TOPICS", "")).strip().lower() == "true"
    if show or not topic:
        return topic
    return topic[:8] + "…"

# 시작 시 워밍업
def startup_warmup():
    print("=" * 60)
    print(" [업무프로세스 ntfy 챗봇 시작 - 워밍업] ")
    print(f" 문서 경로: {'; '.join(doc_paths())}")
    print(f" 모델: {MODEL} | 컨텍스트: {NUM_CTX} | 온도: {TEMPERATURE} | think: {THINK}")
    print(f" ntfy 서버: {NTFY_SERVER}")
    if USER_MODE:
        print(f" [운영 모드] 사용자별 개인 토픽 모드 ({len(USERS)}명)")
        print(f" 각 사용자의 구독 토픽은 users.json 파일에서 확인하세요 (타인에게 알려주지 마세요).")
        print(f" (콘솔에 전체 토픽을 보려면 SHOW_TOPICS=true 로 실행)")
        for name, t in USERS.items():
            print(f"    {name}  ->  {_topic_text(t)}")
    elif IS_SINGLE_TOPIC:
        print(f" [운영 모드] 🌟 단일 토픽 대화 모드 (1개 방 구독으로 통합)")
        print(f" 👉 구독할 단일 토픽: {_topic_text(NTFY_QUESTION_TOPIC)} (전체는 .env 또는 SHOW_TOPICS=true)")
        print(f"    (휴대폰 ntfy 앱에서 위 토픽 1개만 구독하시면 질문/답변이 한 방에서 이뤄집니다)")
    else:
        print(f" [운영 모드] 분리 토픽 모드 (3개 토픽)")
        print(f" 질문 토픽: {_topic_text(NTFY_QUESTION_TOPIC)}")
        print(f" 답변 토픽: {_topic_text(NTFY_ANSWER_TOPIC)}")
        print(f" 피드백 토픽: {_topic_text(NTFY_FEEDBACK_TOPIC)}")
    print("=" * 60)

    docs, _ = refresh_documents()
    if not docs:
        print("[경고] 업무프로세스 문서를 읽을 수 없습니다! 경로를 확인하십시오.")
    else:
        for name, text in docs:
            print(f"문서 읽기 성공: {name} {len(text):,} 자, {len(text.splitlines()):,} 줄")

    print("\n워밍업 요청 전송 중 (Ollama 모델 로드 및 프롬프트 캐싱)...")
    res = query_llm("워밍업 테스트 질문입니다.")
    if res["success"]:
        print(f"워밍업 완료: 소요시간 {res['elapsed_sec']:.2f}초 | 프롬프트 토큰: {res['prompt_eval_count']}")
    else:
        print(f"워밍업 실패: {res['error']}")

    # ollama ps 확인
    ollama_bin = r"C:\Users\qazx2\AppData\Local\Programs\Ollama\ollama.exe"
    if not os.path.exists(ollama_bin):
        ollama_bin = "ollama"
    try:
        ps_out = subprocess.run([ollama_bin, "ps"], capture_output=True, text=True, check=False)
        print("\n[현재 Ollama 프로세스 상태]")
        print(ps_out.stdout.strip())
    except Exception as e:
        print(f"ollama ps 실행 실패: {e}")
    print("=" * 60 + "\n")

def main():
    startup_warmup()
    ensure_worker()

    if USER_MODE:
        print(f"[개인 토픽 모드 가동] 사용자 {len(USERS)}명 토픽 구독 대기 중")
        for name, t in USERS.items():
            threading.Thread(
                target=subscribe_stream,
                args=(t, make_user_handler(name, t), f"last_user_{name}", name),
                daemon=True
            ).start()
    elif IS_SINGLE_TOPIC:
        print(f"[단일 토픽 모드 가동] 토픽 1개로 질문/답변/피드백 통합 대기 중")
        t_single = threading.Thread(
            target=subscribe_stream,
            args=(NTFY_QUESTION_TOPIC, handle_single_topic_event, "last_single_id", LEGACY_USER),
            daemon=True
        )
        t_single.start()
    else:
        print(f"[분리 토픽 모드 가동] 질문 / 피드백 토픽 대기 중")
        # 질문 구독 스레드
        t_q = threading.Thread(
            target=subscribe_stream,
            args=(NTFY_QUESTION_TOPIC, handle_question_only, "last_q_id", "질문"),
            daemon=True
        )
        t_q.start()

        # 피드백 구독 스레드
        t_fb = threading.Thread(
            target=subscribe_stream,
            args=(NTFY_FEEDBACK_TOPIC, handle_feedback_only, "last_fb_id", "피드백"),
            daemon=True
        )
        t_fb.start()

    print("\n[챗봇 준비 완료] 휴대폰에서 질문을 전송하면 실시간으로 응답합니다. (종료: Ctrl+C)")
    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        print("\n[챗봇 종료]")

if __name__ == "__main__":
    main()
