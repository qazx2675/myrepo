#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
experiment.py - ntfy 업무프로세스 챗봇 모델 x 프롬프트 변형 실험 하네스 (S3)

표준 라이브러리만 사용. bot.py / run_tests.py 는 수정하지 않는다.

사용 예
  python experiment.py run --models qwen3.5:9b-q8_0 --variants P1_nothink --cases v2 --limit 2 --out 실험\\_smoke
  python experiment.py summarize --out 실험\\_smoke

서브커맨드
  run        모델 x 변형 x 문항 호출, 결과를 <out>\\runs.jsonl 에 즉시 append (resume 지원)
  summarize  runs.jsonl -> 게이트 판정 + 비교표.md / 비교표.csv / 게이트요약.md
  variants   정의된 변형 목록 출력
"""

import argparse
import ast
import csv
import hashlib
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone, timedelta

if sys.platform == "win32":
    try:
        sys.stdout.reconfigure(encoding="utf-8", line_buffering=True)
        sys.stderr.reconfigure(encoding="utf-8", line_buffering=True)
    except Exception:
        pass

KST = timezone(timedelta(hours=9))
HERE = os.path.dirname(os.path.abspath(__file__))

DEFAULT_DOC = r"D:\업무프로세스\업무프로세스.md"
DEFAULT_OLLAMA = "http://127.0.0.1:11434"
CASES_OLD = os.path.join(HERE, "tests", "테스트케이스.txt")
CASES_V2 = os.path.join(HERE, "tests", "테스트케이스_v2.txt")
EXPECT_V2 = os.path.join(HERE, "tests", "테스트케이스_v2_기대.md")
BOT_PY = os.path.join(HERE, "bot.py")

CALL_TIMEOUT = 120          # 호출당 timeout(초)
HISTORY_TURNS = 3           # 후속 질문 세트에서 전달할 최근 턴 수

# 생성 옵션 기본값 (운영 .env 와 동일한 값. CLI 로 덮어쓸 수 있음)
GEN_DEFAULTS = {"temperature": 0.2, "top_p": 0.8, "top_k": 20}

# ---------------------------------------------------------------------------
# 변형(variant) 정의
#   prompt     : P0(bot.py 현행) | P1(서술형) | P2(P1 + 질문 정규화)
#   think      : None(미지정) | True | False   (미지원 모델이면 자동 제거)
#   num_predict: int | None(미지정)
#   normalize  : True 면 질문에 문서 용어표 정의를 덧붙임 (P2 는 항상 True)
#   num_ctx    : int
#   history    : True 면 K 세트에서 직전 최대 3턴을 전달
# ---------------------------------------------------------------------------
NUM_CTX_OPS = 24576
VARIANTS = {
    "P0_think":          {"prompt": "P0", "think": None,  "num_predict": None, "normalize": False, "num_ctx": NUM_CTX_OPS, "history": False},
    "P0_nothink":        {"prompt": "P0", "think": False, "num_predict": None, "normalize": False, "num_ctx": NUM_CTX_OPS, "history": False},
    "P1_nothink":        {"prompt": "P1", "think": False, "num_predict": None, "normalize": False, "num_ctx": NUM_CTX_OPS, "history": False},
    "P1_nothink_np400":  {"prompt": "P1", "think": False, "num_predict": 400,  "normalize": False, "num_ctx": NUM_CTX_OPS, "history": False},
    "P1_nothink_hist":   {"prompt": "P1", "think": False, "num_predict": None, "normalize": False, "num_ctx": NUM_CTX_OPS, "history": True},
    "P1_nothink_ctx16k": {"prompt": "P1", "think": False, "num_predict": None, "normalize": False, "num_ctx": 16384,       "history": False},
    "P2_nothink":        {"prompt": "P2", "think": False, "num_predict": None, "normalize": True,  "num_ctx": NUM_CTX_OPS, "history": False},
    "P2_nothink_np400":  {"prompt": "P2", "think": False, "num_predict": 400,  "normalize": True,  "num_ctx": NUM_CTX_OPS, "history": False},
    "P2_nothink_hist":   {"prompt": "P2", "think": False, "num_predict": None, "normalize": True,  "num_ctx": NUM_CTX_OPS, "history": True},
    "P1_think_np1500":   {"prompt": "P1", "think": True,  "num_predict": 1500, "normalize": False, "num_ctx": NUM_CTX_OPS, "history": False},
}

# ---------------------------------------------------------------------------
# 거절 판정 패턴 (자동 게이트용. 품질 판정이 아니라 '거절 문구 포함 여부'만 본다)
# ---------------------------------------------------------------------------
REJECT_PATTERNS = [
    r"문서에\s*없는\s*내용", r"문서에\s*(?:는\s*)?(?:명시|규정|기재|언급|정의|포함|근거)[^\n.]{0,12}(?:없|않|못)",
    r"문서에\s*없", r"명시되어\s*있지\s*않", r"규정(?:되어|이)?\s*있지\s*않", r"규정이\s*없", r"내용이\s*없",
    r"관련(?:이|된\s*내용이|된\s*정보가)?\s*없", r"관련이\s*적", r"범위(?:를|에서|는)?\s*벗어", r"범위\s*밖", r"범위\s*외",
    r"답변(?:드리기|하기|해\s*드리기)\s*(?:어렵|곤란|힘들)", r"답변(?:드릴|할)\s*수\s*없", r"도와드리기\s*어렵",
    r"다루(?:지|고\s*있지)\s*않", r"찾을\s*수\s*없", r"확인(?:되지|할\s*수\s*없)", r"정보(?:가|는)\s*없", r"알\s*수\s*없",
    r"제공(?:하지|할\s*수)\s*(?:않|없)", r"포함(?:되어\s*)?있지\s*않", r"포함되지\s*않", r"벗어난", r"나와\s*있지\s*않",
]
REJECT_RE = re.compile("|".join(REJECT_PATTERNS))
FULL_REFUSAL_MAX_CHARS = 160   # 참고 줄 제외 본문이 이보다 짧고 거절 문구가 있으면 '전체 거절'
BASELINE_UNANSWERED = 22 / 60  # 계획서 §S3: 기존 미답변 22/60 이하

SPEED_AVG_MAX = 10.0
SPEED_MAX_MAX = 20.0

# 미답변율 대상(문서 내 질문) / 문서외 거절 대상
IN_DOC_GROUPS = ("J", "G", "H", "I")
OUT_DOC_GROUPS = ("L", "F")


def now_iso():
    return datetime.now(KST).isoformat(timespec="seconds")


def log(msg):
    print(msg, flush=True)


# ---------------------------------------------------------------------------
# 문서 처리 (모두 문서에서 동적 추출. 코드에 문서 내용 상수 없음)
# ---------------------------------------------------------------------------
def read_doc(path):
    raw = open(path, "rb").read()
    for enc in ("utf-8", "cp949"):
        try:
            return raw.decode(enc), hashlib.sha256(raw).hexdigest(), len(raw)
        except UnicodeDecodeError:
            continue
    return raw.decode("utf-8", errors="replace"), hashlib.sha256(raw).hexdigest(), len(raw)


def estimate_tokens(nbytes):
    # bot 계획서 실측(25,873B ~ 8.5k 토큰)에 맞춘 대략치: UTF-8 바이트/3
    return int(round(nbytes / 3.0))


HEADING_RE = re.compile(r"^(#{2,3})\s+(\d+(?:\.\d+)?)\.?\s+(.+?)\s*$")


def parse_headings(doc):
    """[(번호, 제목)] - '## 1. 제목', '### 2.1 제목' 형식"""
    out = []
    in_code = False
    for line in doc.splitlines():
        if line.lstrip().startswith("```"):
            in_code = not in_code
            continue
        if in_code:
            continue
        m = HEADING_RE.match(line)
        if m:
            out.append((m.group(2), m.group(3)))
    return out


def _clean_cell(s):
    s = s.strip()
    s = re.sub(r"[*`]", "", s)
    return s.strip()


def parse_glossary(doc):
    """'| 약어 | 정의 | ... |' 헤더를 가진 표의 행을 [(약어, 정의, 비고)] 로 반환"""
    rows = []
    lines = doc.splitlines()
    i = 0
    while i < len(lines):
        line = lines[i]
        if line.strip().startswith("|"):
            cells = [c.strip() for c in line.strip().strip("|").split("|")]
            if len(cells) >= 2 and _clean_cell(cells[0]) == "약어" and _clean_cell(cells[1]) == "정의":
                i += 2  # 헤더 + 구분선
                while i < len(lines) and lines[i].strip().startswith("|"):
                    c = [_clean_cell(x) for x in lines[i].strip().strip("|").split("|")]
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
        return question, []
    return question + "\n\n[용어 참고] " + " / ".join(found), found


# ---------------------------------------------------------------------------
# 프롬프트
# ---------------------------------------------------------------------------
def load_p0_builder():
    """bot.py 를 import 하면 .env 생성·운영 폴더 접근 등 부작용이 있으므로,
    AST 로 build_system_prompt 함수만 꺼내 그대로 실행 가능하게 만든다(내용 무변경)."""
    src = open(BOT_PY, "r", encoding="utf-8").read()
    tree = ast.parse(src)
    for node in tree.body:
        if isinstance(node, ast.FunctionDef) and node.name == "build_system_prompt":
            mod = ast.Module(body=[node], type_ignores=[])
            ns = {}
            exec(compile(mod, BOT_PY, "exec"), ns)
            return ns["build_system_prompt"]
    raise RuntimeError("bot.py 에서 build_system_prompt 를 찾지 못했습니다")


def build_p1_prompt(doc, headings):
    titles = "\n".join(f"- {n} {t}" for n, t in headings)
    return f"""당신은 아래 [지침서]를 충분히 이해하고 있는 사내 동료 엔지니어입니다. 동료가 메신저로 물어보면 지침서 내용만을 근거로, 사람이 설명해 주듯 자연스러운 말투로 답해 주세요. 문서를 그대로 복사하지 말고 질문의 의도에 맞게 풀어서 설명합니다.

[지침서]
===
{doc}
===

[답변 원칙]
- 지침서에 없는 내용은 지어내지 않습니다. 일반 상식이나 외부 지식으로 빈틈을 메우지 마세요.
- 질문의 일부만 지침서에 근거가 있으면, 확인된 부분은 그대로 답하고 "문서에 규정이 없는 부분:" 으로 시작하는 한 줄로 어떤 부분이 없는지 밝힙니다.
- 지침서와 전혀 관련 없는 질문은 정중히 거절합니다. 거절할 때는 "문서에 없는 내용입니다"를 포함해 한두 문장으로 짧게 답합니다.
- 질문이 여러 개이면 하나씩 나누어 빠짐없이 답합니다. 짧은 질문, 약어, 오타는 지침서 맥락에서 가장 자연스러운 뜻으로 이해합니다. 앞선 대화가 있으면 이어서 답합니다.
- 조항 기호(§)와 '§2.1' 같은 표기는 본문에 쓰지 않습니다.
- 간결하게, 핵심 위주로 답하고 순서가 있는 절차는 번호를 붙입니다.
- 답변 마지막 줄에 '📌 참고: ' 다음에 근거가 된 절의 제목을 쉼표로 적습니다. 아래 [문서 제목 목록]에 있는 제목만 사용합니다.

[문서 제목 목록]
{titles}"""


# ---------------------------------------------------------------------------
# 테스트케이스 / 기대.md
# ---------------------------------------------------------------------------
def read_cases_file(path):
    raw = open(path, "rb").read()
    try:
        text = raw.decode("utf-8-sig")
    except UnicodeDecodeError:
        text = raw.decode("cp949")
    out = []
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if "\t" in line:
            cid, q = line.split("\t", 1)
        else:
            parts = line.split(None, 1)
            if len(parts) < 2:
                continue
            cid, q = parts
        out.append((cid.strip(), q.strip()))
    return out


def select_cases(spec):
    old = read_cases_file(CASES_OLD) if os.path.exists(CASES_OLD) else []
    v2 = read_cases_file(CASES_V2) if os.path.exists(CASES_V2) else []
    s = spec.strip()
    if s.lower() == "v2":
        return v2
    if s.lower() == "old":
        return old
    if s.lower() == "all":
        return old + v2
    wanted = [x.strip() for x in re.split(r"[,\s]+", s) if x.strip()]
    pool = {cid: q for cid, q in old + v2}
    missing = [w for w in wanted if w not in pool]
    if missing:
        raise SystemExit(f"알 수 없는 문항 ID: {', '.join(missing)}")
    return [(w, pool[w]) for w in wanted]


NUM_RE = re.compile(r"\d+(?:\.\d+)?")


def parse_expect(path):
    """기대.md 표에서 '근거 절' 열의 절 번호를 {ID: [번호...]} 로 추출"""
    res = {}
    if not os.path.exists(path):
        return res
    col = None
    for line in open(path, "r", encoding="utf-8").read().splitlines():
        if not line.strip().startswith("|"):
            col = None
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if "ID" in cells and any("근거" in c for c in cells):
            col = next(i for i, c in enumerate(cells) if "근거" in c)
            continue
        if col is None or re.fullmatch(r"[-: ]+", cells[0] or ""):
            continue
        if len(cells) > col and re.match(r"^[A-Z]+\d", cells[0]):
            res[cells[0]] = NUM_RE.findall(cells[col])
    return res


def check_expect_warnings(headings, expect):
    have = {n for n, _ in headings}
    warns = []
    for cid, nums in expect.items():
        lost = [n for n in nums if n not in have]
        if lost:
            warns.append(f"[경고][E5] {cid}: 기대.md 근거 절 {', '.join(lost)} 가 현재 문서에 없음 (근거 절 소실)")
    return warns


# ---------------------------------------------------------------------------
# Ollama 호출
# ---------------------------------------------------------------------------
def http_json(url, body=None, timeout=CALL_TIMEOUT):
    data = json.dumps(body).encode("utf-8") if body is not None else None
    req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json; charset=utf-8"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read().decode("utf-8"))


def http_error_text(e):
    try:
        return e.read().decode("utf-8", "replace")
    except Exception:
        return ""


def gpu_vram_used_mb():
    try:
        out = subprocess.run(
            ["nvidia-smi", "--query-gpu=memory.used,memory.total", "--format=csv,noheader,nounits"],
            capture_output=True, text=True, timeout=10)
        if out.returncode == 0:
            used, total = [int(x.strip()) for x in out.stdout.strip().splitlines()[0].split(",")]
            return used, total
    except Exception:
        pass
    return None, None


def unload_model(base, model):
    try:
        http_json(f"{base}/api/generate", {"model": model, "keep_alive": 0}, timeout=60)
    except Exception as e:
        log(f"  [언로드 경고] {model}: {e}")


def load_model(base, model, num_ctx, loads_path):
    """워밍업 로드 후 /api/ps 로 GPU 적재율을 loads.jsonl 에 기록"""
    t0 = time.time()
    err = ""
    try:
        http_json(f"{base}/api/generate",
                  {"model": model, "keep_alive": "30m", "options": {"num_ctx": num_ctx}}, timeout=300)
    except Exception as e:
        err = str(e)
    load_sec = round(time.time() - t0, 2)
    rec = {"ts": now_iso(), "model": model, "num_ctx": num_ctx, "load_sec": load_sec, "error": err}
    try:
        ps = http_json(f"{base}/api/ps", timeout=15)
        ent = next((m for m in ps.get("models", [])
                    if m.get("name") == model or m.get("model") == model), None)
        if ent:
            size, vram = ent.get("size", 0), ent.get("size_vram", 0)
            rec.update({"size": size, "size_vram": vram,
                        "gpu_pct": round(100.0 * vram / size, 1) if size else None,
                        "context_length": ent.get("context_length")})
        else:
            rec["error"] = (err + " " if err else "") + "/api/ps 에 모델 없음"
    except Exception as e:
        rec["error"] = (err + " " if err else "") + f"/api/ps 실패: {e}"
    used, total = gpu_vram_used_mb()
    rec["vram_used_mb"], rec["vram_total_mb"] = used, total
    with open(loads_path, "a", encoding="utf-8") as f:
        f.write(json.dumps(rec, ensure_ascii=False) + "\n")
    return rec


THINK_TAG_RE = re.compile(r"<think>(.*?)</think>", re.S)


def chat_once(base, model, messages, variant, gen, no_think_models):
    """Ollama /api/chat 1회 호출. think 미지원이면 제거 후 재시도. 반환 dict"""
    opts = {"num_ctx": variant["num_ctx"], "temperature": gen["temperature"],
            "top_p": gen["top_p"], "top_k": gen["top_k"]}
    if variant.get("num_predict"):
        opts["num_predict"] = variant["num_predict"]
    body = {"model": model, "messages": messages, "options": opts, "keep_alive": "30m", "stream": False}
    want_think = variant.get("think")
    send_think = want_think is not None and model not in no_think_models
    if send_think:
        body["think"] = want_think
    think_dropped = want_think is not None and not send_think

    t0 = time.time()
    data, err = None, ""
    for attempt in range(2):
        try:
            data = http_json(f"{base}/api/chat", body, timeout=CALL_TIMEOUT)
            err = ""
            break
        except urllib.error.HTTPError as e:
            text = http_error_text(e)
            if e.code == 400 and "think" in body:
                body.pop("think", None)
                no_think_models.add(model)
                think_dropped = True
                continue
            err = f"HTTP {e.code}: {text[:200]}"
            break
        except Exception as e:
            err = f"{type(e).__name__}: {e}"
            break
    elapsed = round(time.time() - t0, 2)
    if data is None:
        return {"answer": "", "thinking_len": 0, "elapsed": elapsed, "prompt_tokens": 0,
                "eval_tokens": 0, "error": err, "think_dropped": think_dropped, "done_reason": ""}
    msg = data.get("message", {}) or {}
    content = (msg.get("content") or "")
    thinking = msg.get("thinking") or ""
    m = THINK_TAG_RE.search(content)
    if m:
        thinking += m.group(1)
        content = THINK_TAG_RE.sub("", content)
    return {"answer": content.strip(), "thinking_len": len(thinking), "elapsed": elapsed,
            "prompt_tokens": data.get("prompt_eval_count", 0), "eval_tokens": data.get("eval_count", 0),
            "error": "", "think_dropped": think_dropped, "done_reason": data.get("done_reason", "")}


# ---------------------------------------------------------------------------
# run
# ---------------------------------------------------------------------------
K_RE = re.compile(r"^([A-Za-z]+\d+)-(\d+)$")


def read_jsonl(path):
    out = []
    if os.path.exists(path):
        with open(path, "r", encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if line:
                    try:
                        out.append(json.loads(line))
                    except Exception:
                        pass
    return out


def cmd_run(args):
    models = [m.strip() for m in args.models.split(",") if m.strip()]
    vnames = [v.strip() for v in args.variants.split(",") if v.strip()]
    bad = [v for v in vnames if v not in VARIANTS]
    if bad:
        raise SystemExit(f"정의되지 않은 변형: {', '.join(bad)} (정의됨: {', '.join(VARIANTS)})")
    cases = select_cases(args.cases)
    if args.limit:
        cases = cases[:args.limit]
    if not cases:
        raise SystemExit("실행할 문항이 없습니다")

    doc, sha, nbytes = read_doc(args.doc)
    headings = parse_headings(doc)
    glossary = parse_glossary(doc)
    log(f"[문서] {args.doc}  {nbytes}B  sha256={sha[:16]}...  토큰추정 약 {estimate_tokens(nbytes)}  "
        f"절 {len(headings)}개  용어 {len(glossary)}개")
    for w in check_expect_warnings(headings, parse_expect(EXPECT_V2)):
        log(w)

    p0_builder = load_p0_builder()
    prompts = {"P0": p0_builder(doc), "P1": build_p1_prompt(doc, headings)}
    prompts["P2"] = prompts["P1"]

    out_dir = os.path.abspath(args.out)
    os.makedirs(out_dir, exist_ok=True)
    runs_path = os.path.join(out_dir, "runs.jsonl")
    loads_path = os.path.join(out_dir, "loads.jsonl")
    base = args.ollama.rstrip("/")
    try:
        http_json(f"{base}/api/version", timeout=10)
    except Exception as e:
        raise SystemExit(f"Ollama 에 연결할 수 없습니다 ({base}): {e}")

    existing = {}
    for r in read_jsonl(runs_path):
        existing[(r.get("model"), r.get("variant"), r.get("id"))] = r
    gen = {"temperature": args.temperature, "top_p": args.top_p, "top_k": args.top_k}

    total = len(models) * len(vnames) * len(cases)
    done = 0
    no_think_models = set()
    loaded_key = None
    prev_model = None
    t_start = time.time()
    counts = {"run": 0, "skip": 0, "err": 0}

    for model in models:
        for vname in vnames:
            variant = VARIANTS[vname]
            key = (model, variant["num_ctx"])
            if loaded_key != key:
                if prev_model and prev_model != model:
                    unload_model(base, prev_model)
                rec = load_model(base, model, variant["num_ctx"], loads_path)
                log(f"[적재] {model} ctx={variant['num_ctx']} {rec.get('load_sec')}s "
                    f"GPU={rec.get('gpu_pct')}% vram={rec.get('vram_used_mb')}MB"
                    + (f" 오류={rec['error']}" if rec.get("error") else ""))
                loaded_key, prev_model = key, model
            use_norm = variant["normalize"] or variant["prompt"] == "P2"
            history = []   # [(질문, 답변)]
            cur_set = None
            for cid, q in cases:
                done += 1
                mk = K_RE.match(cid)
                set_id = mk.group(1) if mk else None
                if set_id != cur_set:
                    history, cur_set = [], set_id
                ek = (model, vname, cid)
                if ek in existing and not (args.retry_errors and existing[ek].get("error")):
                    counts["skip"] += 1
                    if set_id:
                        history.append((q, existing[ek].get("answer", "")))
                    log(f"[{done}/{total}] {model} | {vname} | {cid}  건너뜀(기존 결과)")
                    continue
                user_q, glossed = (gloss_question(q, glossary) if use_norm else (q, []))
                msgs = [{"role": "system", "content": prompts[variant["prompt"]]}]
                hist_used = 0
                if variant["history"] and set_id:
                    for hq, ha in history[-HISTORY_TURNS:]:
                        msgs.append({"role": "user", "content": hq})
                        msgs.append({"role": "assistant", "content": ha})
                        hist_used += 1
                msgs.append({"role": "user", "content": user_q})
                res = chat_once(base, model, msgs, variant, gen, no_think_models)
                rec = {"ts": now_iso(), "model": model, "variant": vname, "id": cid, "question": q,
                       "answer": res["answer"], "thinking_len": res["thinking_len"],
                       "elapsed_sec": res["elapsed"], "prompt_tokens": res["prompt_tokens"],
                       "eval_tokens": res["eval_tokens"], "error": res["error"],
                       "doc_sha256": sha, "num_ctx": variant["num_ctx"], "prompt": variant["prompt"],
                       "think": variant["think"], "think_dropped": res["think_dropped"],
                       "num_predict": variant["num_predict"], "history_turns": hist_used,
                       "glossed_terms": len(glossed), "done_reason": res["done_reason"]}
                with open(runs_path, "a", encoding="utf-8") as f:
                    f.write(json.dumps(rec, ensure_ascii=False) + "\n")
                existing[ek] = rec
                counts["run"] += 1
                if res["error"]:
                    counts["err"] += 1
                if set_id:
                    history.append((q, res["answer"]))
                log(f"[{done}/{total}] {model} | {vname} | {cid}  {res['elapsed']}s  "
                    f"prompt={res['prompt_tokens']} eval={res['eval_tokens']} think_len={res['thinking_len']} "
                    f"{'ERR ' + res['error'] if res['error'] else 'OK'}"
                    f"{' (think 제거)' if res['think_dropped'] else ''}")

    if prev_model and not args.keep_loaded:
        unload_model(base, prev_model)
    log(f"[완료] 실행 {counts['run']} / 건너뜀 {counts['skip']} / 오류 {counts['err']}  "
        f"총 {round(time.time() - t_start, 1)}s  -> {runs_path}")


# ---------------------------------------------------------------------------
# summarize
# ---------------------------------------------------------------------------
def group_of(cid):
    m = re.match(r"^([A-Za-z]+)", cid)
    return m.group(1).upper() if m else ""


def body_of(answer):
    lines = [l for l in answer.splitlines() if not l.strip().startswith("📌")]
    return "\n".join(lines).strip()


def has_reject(answer):
    return REJECT_RE.search(answer) is not None


def is_full_refusal(answer):
    if not answer.strip():
        return True   # 빈 답변은 미답변으로 센다
    b = body_of(answer)
    return has_reject(b) and len(b) <= FULL_REFUSAL_MAX_CHARS


def pct(a, b):
    return "n/a" if not b else f"{a}/{b} ({100.0 * a / b:.0f}%)"


def md_cell(s):
    return str(s).replace("|", "\\|").replace("\n", " ")


def cmd_summarize(args):
    out_dir = os.path.abspath(args.out)
    runs_all = read_jsonl(os.path.join(out_dir, "runs.jsonl"))
    if not runs_all:
        raise SystemExit(f"runs.jsonl 이 없거나 비어 있습니다: {out_dir}")
    loads = read_jsonl(os.path.join(out_dir, "loads.jsonl"))

    latest = {}
    for r in runs_all:
        latest[(r["model"], r["variant"], r["id"])] = r
    runs = list(latest.values())
    if args.models:
        ms = {m.strip() for m in args.models.split(",")}
        runs = [r for r in runs if r["model"] in ms]
    if args.variants:
        vs = {v.strip() for v in args.variants.split(",")}
        runs = [r for r in runs if r["variant"] in vs]

    doc_hashes = sorted({r.get("doc_sha256", "") for r in runs})
    doc_path = args.doc
    sha_now, tok_est, nbytes = "", None, None
    headings = []
    if os.path.exists(doc_path):
        doc, sha_now, nbytes = read_doc(doc_path)
        tok_est = estimate_tokens(nbytes)
        headings = parse_headings(doc)
    warns = check_expect_warnings(headings, parse_expect(EXPECT_V2)) if headings else []
    if sha_now and any(h and h != sha_now for h in doc_hashes):
        warns.append("[경고] runs.jsonl 의 문서 해시와 현재 문서 해시가 다릅니다 (결과가 다른 문서 버전 기준)")

    # 후보 순서: 등장 순서 유지
    cand_order = []
    for r in runs_all:
        k = (r["model"], r["variant"])
        if k not in cand_order and any((x["model"], x["variant"]) == k for x in runs):
            cand_order.append(k)

    def load_for(model, num_ctx):
        c = [l for l in loads if l.get("model") == model and l.get("num_ctx") == num_ctx]
        if not c:
            c = [l for l in loads if l.get("model") == model]
        return c[-1] if c else None

    header_lines = [
        f"문서 SHA256: {sha_now or ','.join(doc_hashes)}",
        f"문서 크기/토큰 추정: {nbytes if nbytes is not None else '?'}B / 약 {tok_est if tok_est else '?'} 토큰 (UTF-8 바이트/3 추정)"
        + (f", 실측 prompt 토큰(최대) {max(r.get('prompt_tokens', 0) for r in runs)}" if runs else ""),
        f"생성: {now_iso()}  결과 폴더: {out_dir}",
    ]

    rows = []
    for model, vname in cand_order:
        rs = [r for r in runs if r["model"] == model and r["variant"] == vname]
        ok = [r for r in rs if not r.get("error")]
        errs = len(rs) - len(ok)
        times = [r["elapsed_sec"] for r in ok]
        avg_t = sum(times) / len(times) if times else None
        max_t = max(times) if times else None
        ld = load_for(model, rs[0].get("num_ctx"))
        gpu = ld.get("gpu_pct") if ld else None
        vram = ld.get("vram_used_mb") if ld else None

        out_rs = [r for r in ok if group_of(r["id"]) in OUT_DOC_GROUPS]
        out_rej = sum(1 for r in out_rs if has_reject(r["answer"]))
        in_rs = [r for r in rs if group_of(r["id"]) in IN_DOC_GROUPS]
        in_unans = sum(1 for r in in_rs if r.get("error") or is_full_refusal(r["answer"]))
        sect = sum(1 for r in ok if "§" in r["answer"])
        empty = sum(1 for r in ok if not r["answer"].strip())

        g_speed = None if avg_t is None else (avg_t <= SPEED_AVG_MAX and max_t <= SPEED_MAX_MAX)
        g_load = (gpu == 100.0) and errs == 0 if gpu is not None else None
        g_out = None if not out_rs else out_rej == len(out_rs)
        g_sect = sect == 0
        g_unans = None if not in_rs else (in_unans / len(in_rs) <= BASELINE_UNANSWERED)
        gates = [g_speed, g_load, g_out, g_sect, g_unans]
        overall = "PASS" if all(g is True for g in gates) else ("FAIL" if any(g is False for g in gates) else "N/A")
        rows.append({"model": model, "variant": vname, "n": len(rs), "avg": avg_t, "max": max_t, "errs": errs,
                     "gpu": gpu, "vram": vram, "out": (out_rej, len(out_rs)), "unans": (in_unans, len(in_rs)),
                     "sect": sect, "empty": empty, "gates": gates, "overall": overall,
                     "think_dropped": any(r.get("think_dropped") for r in rs),
                     "avg_think": (sum(r.get("thinking_len", 0) for r in ok) / len(ok)) if ok else 0})

    def fmt(v, suffix="", nd=1):
        return "-" if v is None else f"{v:.{nd}f}{suffix}"

    def mark(g):
        return "-" if g is None else ("PASS" if g else "FAIL")

    # 게이트 요약 md
    md = ["# 안전 게이트 요약 (자동 판정 - 답변 품질 점수 아님)", ""]
    md += [f"- {l}" for l in header_lines]
    md += [f"- {w}" for w in warns]
    md += ["", "기준: 속도 평균<=%.0fs & 최대<=%.0fs / 적재 100%% GPU & 오류0 / 문서외 거절 L·F 전부 거절 문구 포함 / "
           "금지패턴 § 0건 / 미답변율(G·H·I·J 전체 거절) <= 22/60(36.7%%)" % (SPEED_AVG_MAX, SPEED_MAX_MAX), ""]
    md += ["| 모델 | 변형 | 문항 | 평균s | 최대s | 오류 | GPU% | VRAM MB | 평균 thinking자 | 속도 | 적재 | 문서외거절 | §금지 | 미답변율 | 종합 |",
           "|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|"]
    for r in rows:
        md.append("| " + " | ".join([
            r["model"], r["variant"], str(r["n"]), fmt(r["avg"]), fmt(r["max"]), str(r["errs"]),
            "-" if r["gpu"] is None else f"{r['gpu']:.0f}", "-" if r["vram"] is None else str(r["vram"]),
            f"{r['avg_think']:.0f}",
            mark(r["gates"][0]), mark(r["gates"][1]),
            f"{mark(r['gates'][2])} {pct(*r['out'])}", f"{mark(r['gates'][3])} ({r['sect']})",
            f"{mark(r['gates'][4])} {pct(*r['unans'])}", r["overall"]]) + " |")
    notes = [r for r in rows if r["think_dropped"]]
    if notes:
        md += ["", "참고: 다음 후보는 think 파라미터가 미지원이라 제거되어 호출됨: "
               + ", ".join(f"{r['model']}/{r['variant']}" for r in notes)]
    empties = [r for r in rows if r["empty"]]
    if empties:
        md += ["참고: 빈 답변(thinking 이 예산을 소진했을 가능성) 발생: "
               + ", ".join(f"{r['model']}/{r['variant']}={r['empty']}건" for r in empties)]
    with open(os.path.join(out_dir, "게이트요약.md"), "w", encoding="utf-8") as f:
        f.write("\n".join(md) + "\n")

    # 비교표 (질문별 나란히)
    ids = []
    for r in runs_all:
        if r["id"] not in ids:
            ids.append(r["id"])
    qtext = {r["id"]: r["question"] for r in runs_all}
    cm = ["# 비교표 (판정/메모는 사용자가 기입)", ""]
    cm += [f"- {l}" for l in header_lines]
    cm += [f"- {w}" for w in warns]
    cm += ["", "판정 기호: ◎ 매끄러움 / ○ 정확하나 딱딱 / △ 일부 오류 / × 오답·지어냄", ""]
    for cid in ids:
        cands = [(m, v) for (m, v) in cand_order if (m, v, cid) in latest and latest[(m, v, cid)] in runs]
        if not cands:
            continue
        cm += [f"## {cid}", "", f"**질문**: {qtext[cid]}", ""]
        for m, v in cands:
            r = latest[(m, v, cid)]
            status = f"오류: {r['error']}" if r.get("error") else f"{r['elapsed_sec']}s"
            cm += [f"**{m} / {v}** ({status})", ""]
            ans = r["answer"] if r["answer"].strip() else "(빈 답변)"
            cm += ["> " + l if l.strip() else ">" for l in ans.splitlines()]
            cm += ["", "- 판정: ", "- 메모: ", ""]
    with open(os.path.join(out_dir, "비교표.md"), "w", encoding="utf-8") as f:
        f.write("\n".join(cm) + "\n")

    with open(os.path.join(out_dir, "비교표.csv"), "w", encoding="utf-8-sig", newline="") as f:
        w = csv.writer(f)
        for l in header_lines + warns:
            w.writerow(["# " + l])
        w.writerow(["ID", "질문", "모델", "변형", "답변", "소요초", "오류", "판정", "메모"])
        for cid in ids:
            for m, v in cand_order:
                r = latest.get((m, v, cid))
                if r is None or r not in runs:
                    continue
                w.writerow([cid, r["question"], m, v, r["answer"], r["elapsed_sec"], r.get("error", ""), "", ""])

    # stdout
    for l in header_lines:
        log(l)
    for w_ in warns:
        log(w_)
    log("")
    log(f"{'모델':<32}{'변형':<20}{'n':>3}{'평균s':>7}{'최대s':>7}{'오류':>5}{'GPU%':>6}{'VRAM':>7}  "
        f"{'속도':<5}{'적재':<5}{'문서외':<14}{'§':<6}{'미답변율':<16}종합")
    for r in rows:
        log(f"{r['model']:<32}{r['variant']:<20}{r['n']:>3}{fmt(r['avg']):>7}{fmt(r['max']):>7}{r['errs']:>5}"
            f"{('-' if r['gpu'] is None else format(r['gpu'], '.0f')):>6}"
            f"{('-' if r['vram'] is None else r['vram']):>7}  "
            f"{mark(r['gates'][0]):<5}{mark(r['gates'][1]):<5}"
            f"{mark(r['gates'][2]) + ' ' + pct(*r['out']):<14}{mark(r['gates'][3]) + str(r['sect']):<6}"
            f"{mark(r['gates'][4]) + ' ' + pct(*r['unans']):<16}{r['overall']}")
    log("")
    log(f"[저장] {os.path.join(out_dir, '게이트요약.md')}, 비교표.md, 비교표.csv")


# ---------------------------------------------------------------------------
def main():
    ap = argparse.ArgumentParser(description="ntfy 챗봇 모델x프롬프트 실험 하네스")
    sub = ap.add_subparsers(dest="cmd", required=True)

    r = sub.add_parser("run", help="실험 실행")
    r.add_argument("--models", required=True, help="쉼표 구분 모델 태그")
    r.add_argument("--variants", required=True, help="쉼표 구분 변형 이름 (variants 서브커맨드로 확인)")
    r.add_argument("--cases", default="v2", help="v2 | old | all | ID 쉼표목록 (기본 v2)")
    r.add_argument("--limit", type=int, default=0, help="앞에서 N문항만")
    r.add_argument("--out", required=True, help="결과 폴더")
    r.add_argument("--doc", default=DEFAULT_DOC)
    r.add_argument("--ollama", default=DEFAULT_OLLAMA)
    r.add_argument("--temperature", type=float, default=GEN_DEFAULTS["temperature"])
    r.add_argument("--top-p", dest="top_p", type=float, default=GEN_DEFAULTS["top_p"])
    r.add_argument("--top-k", dest="top_k", type=int, default=GEN_DEFAULTS["top_k"])
    r.add_argument("--retry-errors", action="store_true", help="resume 시 오류였던 항목도 다시 실행")
    r.add_argument("--keep-loaded", action="store_true", help="종료 시 모델을 언로드하지 않음")
    r.set_defaults(fn=cmd_run)

    s = sub.add_parser("summarize", help="게이트 판정 + 비교표 생성")
    s.add_argument("--out", required=True)
    s.add_argument("--doc", default=DEFAULT_DOC)
    s.add_argument("--models", default="")
    s.add_argument("--variants", default="")
    s.set_defaults(fn=cmd_summarize)

    v = sub.add_parser("variants", help="변형 목록 출력")
    v.set_defaults(fn=lambda a: [log(f"{n:<20} {json.dumps(c, ensure_ascii=False)}") for n, c in VARIANTS.items()])

    args = ap.parse_args()
    args.fn(args)


if __name__ == "__main__":
    main()
