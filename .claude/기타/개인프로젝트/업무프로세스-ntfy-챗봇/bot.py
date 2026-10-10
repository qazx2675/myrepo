#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
업무프로세스 ntfy 챗봇 (bot.py)
- 휴대폰(ntfy) -> 집 PC 로컬 LLM(qwen3:8b) -> ntfy 답변 + 피드백(👍/👎)
- Python 표준 라이브러리만 사용 (외부 pip 의존성 없음)
"""

import sys
import os
import time
import json
import uuid
import secrets
import threading
import subprocess
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

DOC_PATH = os.path.join(DATA_DIR, "업무프로세스.md")
LOGS_DIR = os.path.join(DATA_DIR, "logs")
TESTS_DIR = os.path.join(DATA_DIR, "tests")
BACKUP_DIR = os.path.join(DATA_DIR, "백업")
STATE_FILE = os.path.join(DATA_DIR, "state.json")
ENV_FILE = os.path.join(DATA_DIR, ".env")

QA_LOG = os.path.join(LOGS_DIR, "qa.jsonl")
UNANSWERED_LOG = os.path.join(LOGS_DIR, "unanswered.jsonl")
FEEDBACK_LOG = os.path.join(LOGS_DIR, "feedback.jsonl")
ERRORS_LOG = os.path.join(LOGS_DIR, "errors.jsonl")

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
    if "NTFY_TOPIC" not in env_vars and "NTFY_QUESTION_TOPIC" not in env_vars:
        # 기본값으로 가장 편리한 단일 토픽(1개 구독으로 질의응답) 생성
        env_vars["NTFY_TOPIC"] = f"proc-chat-{secrets.token_hex(8)}"
        updated = True

    if "NTFY_TOPIC" in env_vars and env_vars["NTFY_TOPIC"]:
        single_t = env_vars["NTFY_TOPIC"]
        env_vars["NTFY_QUESTION_TOPIC"] = single_t
        env_vars["NTFY_ANSWER_TOPIC"] = single_t
        env_vars["NTFY_FEEDBACK_TOPIC"] = single_t

    if "NTFY_QUESTION_TOPIC" not in env_vars:
        env_vars["NTFY_QUESTION_TOPIC"] = f"proc-q-{secrets.token_hex(8)}"
        updated = True
    if "NTFY_ANSWER_TOPIC" not in env_vars:
        env_vars["NTFY_ANSWER_TOPIC"] = f"proc-a-{secrets.token_hex(8)}"
        updated = True
    if "NTFY_FEEDBACK_TOPIC" not in env_vars:
        env_vars["NTFY_FEEDBACK_TOPIC"] = f"proc-fb-{secrets.token_hex(8)}"
        updated = True
    if "NTFY_TOKEN" not in env_vars:
        env_vars["NTFY_TOKEN"] = ""
        updated = True
    if "OLLAMA_URL" not in env_vars:
        env_vars["OLLAMA_URL"] = "http://127.0.0.1:11434"
        updated = True
    if "MODEL" not in env_vars:
        env_vars["MODEL"] = "qwen3:8b"
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
    if "DOC_PATH" not in env_vars:
        env_vars["DOC_PATH"] = DOC_PATH
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

IS_SINGLE_TOPIC = (NTFY_QUESTION_TOPIC == NTFY_ANSWER_TOPIC)
NTFY_TOKEN = CONFIG.get("NTFY_TOKEN", "")
OLLAMA_URL = CONFIG.get("OLLAMA_URL", "http://127.0.0.1:11434").rstrip("/")
MODEL = CONFIG.get("MODEL", "qwen3:8b")
NUM_CTX = int(CONFIG.get("NUM_CTX", 24576))
KEEP_ALIVE = CONFIG.get("KEEP_ALIVE", "30m")
TEMPERATURE = float(CONFIG.get("TEMPERATURE", 0.2))
TARGET_DOC_PATH = CONFIG.get("DOC_PATH", DOC_PATH)
NO_ANSWER_TEXT = CONFIG.get("NO_ANSWER_TEXT", "문서에 없는 내용입니다.")

# 최근 질문/답변 캐시 (피드백 연결용)
QA_CACHE = {}  # q_id -> {"question": ..., "answer": ...}

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
    try:
        with open(STATE_FILE, "w", encoding="utf-8") as f:
            json.dump(state, f, ensure_ascii=False, indent=2)
    except Exception as e:
        print(f"[상태 저장 실패] {e}")

# 문서 읽기 (질문마다 다시 읽어 변경사항 즉시 반영)
def read_document(path=TARGET_DOC_PATH):
    if not os.path.exists(path):
        # 대안으로 .txt 도 시도
        txt_alt = path.replace(".md", ".txt")
        if os.path.exists(txt_alt):
            path = txt_alt

    for enc in ["utf-8", "cp949", "euc-kr"]:
        try:
            with open(path, "r", encoding=enc) as f:
                return f.read()
        except UnicodeDecodeError:
            continue
        except Exception as e:
            print(f"[문서 읽기 오류] {path}: {e}")
            break
    return ""

# 시스템 프롬프트 구성
def build_system_prompt(doc_text):
    return f"""다음은 참고할 업무프로세스 문서입니다:
===
{doc_text}
===

규칙:
1. 위 문서에 근거가 없거나 문서만으로 판단할 수 없는 질문은 부가 설명이나 변형 없이 반드시 정확히 "문서에 없는 내용입니다." 라고만 답하십시오.
2. 일반 상식이나 외부 지식으로 보충하지 마십시오.
3. 간결하게 답하고, 절차나 순서는 번호 목록으로 작성하십시오.
4. 답변의 마지막 줄에는 반드시 "(근거: 문서의 항목/제목)" 형식으로 근거가 된 문서의 섹션/항목 제목을 적으십시오.
5. 1000자 이내로 작성하십시오.
6. 여러 작업을 함께 묻는 질문은 작업별로 나누어 답하고, 문서에 있는 선후관계 및 주의사항을 먼저 설명하십시오."""

# Ollama LLM 호출
def query_llm(question, doc_text=None):
    if doc_text is None:
        doc_text = read_document()

    system_prompt = build_system_prompt(doc_text)
    req_body = {
        "model": MODEL,
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": question}
        ],
        "options": {
            "num_ctx": NUM_CTX,
            "temperature": TEMPERATURE
        },
        "keep_alive": KEEP_ALIVE,
        "stream": False
    }

    req_json = json.dumps(req_body).encode("utf-8")
    req = urllib.request.Request(
        f"{OLLAMA_URL}/api/chat",
        data=req_json,
        headers={"Content-Type": "application/json; charset=utf-8"}
    )

    t0 = time.time()
    try:
        with urllib.request.urlopen(req, timeout=180) as resp:
            data = json.loads(resp.read().decode("utf-8"))
    except Exception as e:
        elapsed = time.time() - t0
        return {
            "success": False,
            "error": str(e),
            "elapsed_sec": elapsed,
            "answer": "",
            "prompt_eval_count": 0,
            "eval_count": 0,
            "unanswered": True
        }

    elapsed = time.time() - t0
    msg = data.get("message", {}).get("content", "").strip()
    prompt_tokens = data.get("prompt_eval_count", 0)
    eval_tokens = data.get("eval_count", 0)

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
        "unanswered": unanswered
    }

# ntfy 발행 (메시지 분할 및 피드백 버튼 첨부)
def publish_ntfy(answer_text, q_id, title="🤖 답변"):
    # 4096바이트 / 한글 약 1300자 분할 처리
    max_chunk_chars = 1200
    chunks = []
    
    if len(answer_text) <= max_chunk_chars:
        chunks = [answer_text]
    else:
        # 분할
        lines = answer_text.split("\n")
        cur_chunk = ""
        for line in lines:
            if len(cur_chunk) + len(line) + 1 > max_chunk_chars:
                if cur_chunk:
                    chunks.append(cur_chunk.strip())
                cur_chunk = line + "\n"
            else:
                cur_chunk += line + "\n"
        if cur_chunk.strip():
            chunks.append(cur_chunk.strip())

    total_chunks = len(chunks)
    
    for idx, chunk in enumerate(chunks, 1):
        chunk_title = title
        if total_chunks > 1:
            chunk_title = f"{title} ({idx}/{total_chunks})"
        
        is_last = (idx == total_chunks)
        payload = {
            "topic": NTFY_ANSWER_TOPIC,
            "title": chunk_title,
            "message": chunk,
            "tags": ["bot"]
        }

        # 마지막 조각에만 👍/👎 버튼 부착 (Section 3.5)
        if is_last:
            payload["actions"] = [
                {
                    "action": "http",
                    "label": "👍 좋음",
                    "url": f"{NTFY_SERVER}/{NTFY_FEEDBACK_TOPIC}",
                    "body": f"good {q_id}"
                },
                {
                    "action": "http",
                    "label": "👎 나쁨",
                    "url": f"{NTFY_SERVER}/{NTFY_FEEDBACK_TOPIC}",
                    "body": f"bad {q_id}"
                }
            ]

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
            print(f"[ntfy 발행 성공] 조각 {idx}/{total_chunks} -> {NTFY_ANSWER_TOPIC}")
        except Exception as e:
            print(f"[ntfy 발행 실패] 조각 {idx}/{total_chunks}: {e}")

# 질문 처리 메인 로직
def process_question(msg_id, question_text):
    q_id = msg_id if msg_id else str(uuid.uuid4())[:8]
    print(f"\n[질문 수신] ID: {q_id} | 질문: {question_text}")

    res = query_llm(question_text)
    if not res["success"]:
        print(f"[LLM 에러] ID: {q_id} | {res['error']}")
        append_jsonl(ERRORS_LOG, {
            "id": q_id,
            "time": now_iso(),
            "question": question_text,
            "error": res["error"]
        })
        publish_ntfy(f"답변 생성 중 오류가 발생했습니다: {res['error']}", q_id, title="오류 발생")
        return

    ans = res["answer"]
    elapsed = res["elapsed_sec"]
    unanswered = res["unanswered"]
    p_tokens = res["prompt_eval_count"]
    e_tokens = res["eval_count"]

    print(f"[답변 생성 완료] 소요시간: {elapsed:.2f}s | 토큰: {p_tokens}+{e_tokens} | 미답변: {unanswered}")
    print(f"[답변 내용]\n{ans}\n")

    # 캐시 저장 (피드백 연계용)
    QA_CACHE[q_id] = {"question": question_text, "answer": ans}

    # 로그 기록
    append_jsonl(QA_LOG, {
        "id": q_id,
        "time": now_iso(),
        "question": question_text,
        "answer": ans,
        "unanswered": unanswered,
        "prompt_eval_count": p_tokens,
        "eval_count": e_tokens,
        "elapsed_sec": round(elapsed, 2)
    })

    if unanswered:
        append_jsonl(UNANSWERED_LOG, {
            "id": q_id,
            "time": now_iso(),
            "question": question_text
        })

    # ntfy 로 전송
    publish_ntfy(ans, q_id)

# 피드백 처리 메인 로직
def process_feedback(msg_id, fb_text):
    parts = fb_text.strip().split()
    if not parts:
        return
    rating = parts[0].lower()  # "good" or "bad"
    target_q_id = parts[1] if len(parts) > 1 else ""

    question = ""
    answer = ""
    if target_q_id in QA_CACHE:
        question = QA_CACHE[target_q_id]["question"]
        answer = QA_CACHE[target_q_id]["answer"]
    elif os.path.exists(QA_LOG):
        # qa.jsonl 에서 역순 탐색
        try:
            with open(QA_LOG, "r", encoding="utf-8") as f:
                lines = f.readlines()
                for line in reversed(lines):
                    try:
                        record = json.loads(line)
                        if record.get("id") == target_q_id:
                            question = record.get("question", "")
                            answer = record.get("answer", "")
                            break
                    except Exception:
                        continue
        except Exception:
            pass

    print(f"[피드백 수신] 질문ID: {target_q_id} | 평가: {rating}")
    append_jsonl(FEEDBACK_LOG, {
        "id": target_q_id,
        "time": now_iso(),
        "rating": rating,
        "question": question,
        "answer": answer
    })

# 이벤트 디스패처
def handle_single_topic_event(msg_id, msg_text, event):
    """
    단일 토픽 모드용 통합 핸들러:
    - 봇이 보낸 답변은 무시 (무한 루프 방지)
    - good/bad 피드백 메시지는 process_feedback으로 전달
    - 사용자 일반 질문은 process_question으로 전달
    """
    tags = event.get("tags") or []
    title = event.get("title") or ""
    
    # 1. 봇 자신이 보낸 메시지인지 확인
    if "bot" in tags or title.startswith("🤖") or title in ["오류 발생", "워밍업"]:
        return

    # 2. 피드백 메시지인지 확인 (👍/👎 버튼 클릭 시 발행된 텍스트)
    parts = msg_text.strip().split()
    if parts and parts[0].lower() in ["good", "bad"]:
        process_feedback(msg_id, msg_text)
        return

    # 3. 빈 메시지 무시
    if not msg_text.strip():
        return

    # 4. 사용자 질문 처리
    process_question(msg_id, msg_text)

def handle_question_only(msg_id, msg_text, event):
    """분리 토픽 모드: 질문 처리 (봇 자체 발행 메시지 제외)"""
    tags = event.get("tags") or []
    if "bot" in tags:
        return
    if not msg_text.strip():
        return
    process_question(msg_id, msg_text)

def handle_feedback_only(msg_id, msg_text, event):
    """분리 토픽 모드: 피드백 처리"""
    if not msg_text.strip():
        return
    process_feedback(msg_id, msg_text)

# ntfy 스트림 구독 워커 (질문 토픽 & 피드백 토픽)
def subscribe_stream(topic, on_message, last_id_key):
    state = load_state()
    last_id = state.get(last_id_key, "")

    backoff = 5
    while True:
        url = f"{NTFY_SERVER}/{topic}/json"
        if last_id:
            url += f"?since={last_id}"

        print(f"[{topic} 구독 시작] URL: {url}")
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
                            st = load_state()
                            st[last_id_key] = msg_id
                            save_state(st)

                        on_message(msg_id, msg_text, event)
        except urllib.error.HTTPError as e:
            if e.code == 400 and last_id:
                print(f"[{topic} 알림] 이전 메시지 ID({last_id})가 만료되었거나 유효하지 않아 400 오류 발생. 상태를 초기화하고 최신 스트림으로 자동 전환합니다.")
                last_id = ""
                st = load_state()
                st[last_id_key] = ""
                save_state(st)
                time.sleep(1)
                continue
            print(f"[{topic} HTTP 에러] {e}. {backoff}초 후 재연결...")
            time.sleep(backoff)
            backoff = min(backoff * 2, 300)
        except Exception as e:
            print(f"[{topic} 연결 종료 또는 에러] {e}. {backoff}초 후 재연결...")
            time.sleep(backoff)
            backoff = min(backoff * 2, 300)

# 시작 시 워밍업
def startup_warmup():
    print("=" * 60)
    print(" [업무프로세스 ntfy 챗봇 시작 - 워밍업] ")
    print(f" 문서 경로: {TARGET_DOC_PATH}")
    print(f" 모델: {MODEL} | 컨텍스트: {NUM_CTX} | 온도: {TEMPERATURE}")
    print(f" ntfy 서버: {NTFY_SERVER}")
    if IS_SINGLE_TOPIC:
        print(f" [운영 모드] 🌟 단일 토픽 대화 모드 (1개 방 구독으로 통합)")
        print(f" 👉 구독할 단일 토픽: {NTFY_QUESTION_TOPIC}")
        print(f"    (휴대폰 ntfy 앱에서 위 토픽 1개만 구독하시면 질문/답변이 한 방에서 이뤄집니다)")
    else:
        print(f" [운영 모드] 분리 토픽 모드 (3개 토픽)")
        print(f" 질문 토픽: {NTFY_QUESTION_TOPIC}")
        print(f" 답변 토픽: {NTFY_ANSWER_TOPIC}")
        print(f" 피드백 토픽: {NTFY_FEEDBACK_TOPIC}")
    print("=" * 60)

    doc_text = read_document()
    if not doc_text:
        print("[경고] 업무프로세스 문서를 읽을 수 없습니다! 경로를 확인하십시오.")
    else:
        print(f"문서 읽기 성공: {len(doc_text):,} 자, {len(doc_text.splitlines()):,} 줄")

    print("\n워밍업 요청 전송 중 (Ollama 모델 로드 및 프롬프트 캐싱)...")
    res = query_llm("워밍업 테스트 질문입니다.", doc_text=doc_text)
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

    if IS_SINGLE_TOPIC:
        print(f"[단일 토픽 모드 가동] '{NTFY_QUESTION_TOPIC}' 토픽 1개로 질문/답변/피드백 통합 대기 중")
        t_single = threading.Thread(
            target=subscribe_stream,
            args=(NTFY_QUESTION_TOPIC, handle_single_topic_event, "last_single_id"),
            daemon=True
        )
        t_single.start()
    else:
        print(f"[분리 토픽 모드 가동] 질문({NTFY_QUESTION_TOPIC}) / 피드백({NTFY_FEEDBACK_TOPIC}) 대기 중")
        # 질문 구독 스레드
        t_q = threading.Thread(
            target=subscribe_stream,
            args=(NTFY_QUESTION_TOPIC, handle_question_only, "last_q_id"),
            daemon=True
        )
        t_q.start()

        # 피드백 구독 스레드
        t_fb = threading.Thread(
            target=subscribe_stream,
            args=(NTFY_FEEDBACK_TOPIC, handle_feedback_only, "last_fb_id"),
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
