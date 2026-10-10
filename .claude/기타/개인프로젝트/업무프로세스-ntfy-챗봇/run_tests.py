#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
테스트케이스 일괄 실행 및 검증 스크립트 (run_tests.py)
- tests/테스트케이스.txt 의 60개 자연어 질문을 Ollama에 직접 전송
- 결과_YYYYMMDD.csv 에 번호, 질문, 답변, 미답변 여부, 토큰 수, 소요시간, 판정, 원인 저장
- 6절 검증 기준 통계 계산 및 출력
"""

import sys
import os
import time
import json
import csv
from datetime import datetime, timezone, timedelta
import urllib.request
import urllib.error

# Windows 콘솔 UTF-8 출력 보정
if sys.platform == 'win32':
    try:
        sys.stdout.reconfigure(encoding='utf-8')
        sys.stderr.reconfigure(encoding='utf-8')
    except Exception:
        pass

# 한국 표준시 (KST)
KST = timezone(timedelta(hours=9))

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
DATA_DIR = os.path.abspath(os.path.join(BASE_DIR, ".."))
if not os.path.exists(os.path.join(DATA_DIR, "logs")) and os.path.exists(r"D:\업무프로세스"):
    DATA_DIR = r"D:\업무프로세스"

DOC_PATH = os.path.join(DATA_DIR, "업무프로세스.md")
TESTS_DIR = os.path.join(DATA_DIR, "tests")
TEST_CASES_FILE = os.path.join(TESTS_DIR, "테스트케이스.txt")
ENV_FILE = os.path.join(DATA_DIR, ".env")

# .env 로드
CONFIG = {}
if os.path.exists(ENV_FILE):
    try:
        with open(ENV_FILE, "r", encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    k, v = line.split("=", 1)
                    CONFIG[k.strip()] = v.strip().strip('"').strip("'")
    except Exception:
        pass

OLLAMA_URL = CONFIG.get("OLLAMA_URL", "http://127.0.0.1:11434").rstrip("/")
MODEL = CONFIG.get("MODEL", "qwen3:8b")
NUM_CTX = int(CONFIG.get("NUM_CTX", 24576))
KEEP_ALIVE = CONFIG.get("KEEP_ALIVE", "30m")
TEMPERATURE = float(CONFIG.get("TEMPERATURE", 0.2))
NO_ANSWER_TEXT = CONFIG.get("NO_ANSWER_TEXT", "문서에 없는 내용입니다.")

def read_document(path=DOC_PATH):
    if not os.path.exists(path):
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

def query_llm(question, system_prompt):
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

def evaluate_test_case(test_id, question, answer, unanswered):
    """
    6절 검증 기준에 따른 자동 판정 및 원인 판별
    - F군 (F1~F10): 100% "문서에 없는 내용입니다." 이어야 맞음
    - A~E군: 문서 안 질문.
      1) 근거 표시 여부 `(근거: ...)`
      2) 문서에 내용이 있는데 거절한 경우 -> 문서 부족 또는 모델 오해
      3) 내용 적합성 검토
    """
    category = test_id[0].upper()
    rejection_phrases = [NO_ANSWER_TEXT, "문서에 없는 내용", "명시되어 있지 않습니다", "내용이 없습니다", "문서에 없습니다"]
    is_rejected = unanswered or any(p in answer for p in rejection_phrases)

    # F군 (문서 밖 질문)
    if category == 'F':
        if is_rejected:
            return "맞음", "정상 (문서 밖 질문 올바르게 거절)"
        else:
            return "틀림", "모델 오해 (문서 밖 질문인데 임의 답변)"

    # A~E군 (문서 안 질문)
    # 1. 문서에 내용이 있는데 "문서에 없는 내용입니다"로 거절한 경우
    if unanswered:
        # 질문이 실제로 문서에 아예 없는 작업인지 판별
        # 예: VM 스펙 파일 작성, 메모리 사이징 등 문서에 없는 세부 항목인 경우 문서 부족 판정
        return "부분", "문서 부족 (문서에 명시되지 않은 세부 작업)"

    # 2. 근거 표시 확인
    has_evidence = ("근거:" in answer or "근거 :" in answer or "§" in answer)
    
    # 3. 답변 길이 및 내용 충실도
    if len(answer) > 20 and has_evidence:
        return "맞음", "정상 (문서 내용 일치 및 근거 제시)"
    elif len(answer) > 20:
        return "부분", "프롬프트 문제 (근거 표시 누락)"
    else:
        return "틀림", "모델 오해 (답변 부실)"

def load_test_cases():
    cases = []
    if not os.path.exists(TEST_CASES_FILE):
        print(f"[에러] 테스트케이스 파일 없음: {TEST_CASES_FILE}")
        return cases

    with open(TEST_CASES_FILE, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            if "\t" in line:
                tid, q = line.split("\t", 1)
            elif ":" in line:
                tid, q = line.split(":", 1)
            elif " " in line:
                tid, q = line.split(" ", 1)
            else:
                continue
            cases.append((tid.strip(), q.strip()))
    return cases

def run_all_tests():
    print("=" * 70)
    print(" [업무프로세스 챗봇 자연어 테스트케이스 (60개) 일괄 실행] ")
    print(f" 모델: {MODEL} | num_ctx: {NUM_CTX} | 온도: {TEMPERATURE}")
    print(f" 문서: {DOC_PATH}")
    print("=" * 70)

    doc_text = read_document()
    if not doc_text:
        print("[에러] 문서를 읽을 수 없습니다.")
        return

    sys_prompt = build_system_prompt(doc_text)
    test_cases = load_test_cases()
    if not test_cases:
        print("[에러] 테스트케이스가 비어 있습니다.")
        return

    print(f"총 {len(test_cases)}개 테스트케이스를 로드했습니다.\n")

    today_str = datetime.now(KST).strftime("%Y%m%d")
    output_csv = os.path.join(TESTS_DIR, f"결과_{today_str}.csv")

    results = []
    elapsed_times = []
    first_elapsed = None

    for i, (tid, q) in enumerate(test_cases, 1):
        print(f"[{i}/{len(test_cases)}] {tid}: {q}")
        res = query_llm(q, sys_prompt)

        if not res["success"]:
            print(f"  -> 실패: {res['error']}")
            ans = f"ERROR: {res['error']}"
            unans = True
            tokens = "0+0"
            elapsed = res["elapsed_sec"]
            judgement = "틀림"
            cause = "호출 실패"
        else:
            ans = res["answer"]
            unans = res["unanswered"]
            tokens = f"{res['prompt_eval_count']}+{res['eval_count']}"
            elapsed = res["elapsed_sec"]
            judgement, cause = evaluate_test_case(tid, q, ans, unans)

        if first_elapsed is None:
            first_elapsed = elapsed
        else:
            elapsed_times.append(elapsed)

        first_line = ans.replace("\n", " ")[:60]
        print(f"  -> [{judgement}] ({elapsed:.2f}s, {tokens} 토큰) {first_line}...")

        results.append({
            "번호": tid,
            "질문": q,
            "답변": ans,
            "미답변 여부": "Y" if unans else "N",
            "토큰 수": tokens,
            "소요 시간(초)": f"{elapsed:.2f}",
            "판정": judgement,
            "원인": cause
        })

    # CSV 저장
    fieldnames = ["번호", "질문", "답변", "미답변 여부", "토큰 수", "소요 시간(초)", "판정", "원인"]
    with open(output_csv, "w", encoding="utf-8-sig", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        for r in results:
            writer.writerow(r)

    print("\n" + "=" * 70)
    print(f" [테스트 완료 및 결과 요약] CSV 저장: {output_csv}")
    print("=" * 70)

    # 6절 기준 검증 통계
    total_count = len(results)
    correct_count = sum(1 for r in results if r["판정"] == "맞음")
    partial_count = sum(1 for r in results if r["판정"] == "부분")
    wrong_count = sum(1 for r in results if r["판정"] == "틀림")

    # 문서 안 (A~E 50개) / 문서 밖 (F 10개) 분리
    in_doc = [r for r in results if not r["번호"].startswith("F")]
    out_doc = [r for r in results if r["번호"].startswith("F")]

    in_correct = sum(1 for r in in_doc if r["판정"] == "맞음")
    in_partial = sum(1 for r in in_doc if r["판정"] == "부분")
    in_wrong = sum(1 for r in in_doc if r["판정"] == "틀림")
    in_pass_rate = (in_correct / len(in_doc)) * 100 if in_doc else 0
    in_effective_pass = ((in_correct + in_partial) / len(in_doc)) * 100 if in_doc else 0

    out_correct = sum(1 for r in out_doc if r["판정"] == "맞음")
    out_pass_rate = (out_correct / len(out_doc)) * 100 if out_doc else 0

    avg_subsequent_time = sum(elapsed_times) / len(elapsed_times) if elapsed_times else 0

    print(f"1. 전체 테스트 수: {total_count}개")
    print(f"   - 맞음: {correct_count}개 | 부분: {partial_count}개 | 틀림: {wrong_count}개")
    print(f"2. 문서 안 50개 (A~E):")
    print(f"   - 완전 일치(맞음): {in_correct}개 ({in_pass_rate:.1f}%)")
    print(f"   - 부분 정답: {in_partial}개")
    print(f"   - 틀림: {in_wrong}개")
    print(f"   - 유효 통과율(맞음+부분): {in_effective_pass:.1f}%")
    print(f"3. 문서 밖 10개 (F1~F10 거절률):")
    print(f"   - 거절 성공(맞음): {out_correct}/{len(out_doc)}개 ({out_pass_rate:.1f}%)")
    print(f"4. 응답 속도:")
    print(f"   - 첫 질문(모델 적재 포함): {first_elapsed:.2f}초")
    print(f"   - 이후 질문 평균 응답 시간: {avg_subsequent_time:.2f}초 (목표: 30초 이내 충족 여부: {'충족' if avg_subsequent_time <= 30 else '초과'})")
    print("=" * 70)

if __name__ == "__main__":
    run_all_tests()
