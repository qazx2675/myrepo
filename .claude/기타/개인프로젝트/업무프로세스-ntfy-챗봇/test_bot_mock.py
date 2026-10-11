#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
단위 테스트 및 모의 환경 검증 (test_bot_mock.py)
- bot.py 의 핵심 모듈(문서 읽기, 프롬프트 생성, 분할 전송, 피드백 연결, 상태 저장)과
  개인 토픽·FIFO 큐·대화 맥락·문서 처리를 단위 검증한다.
- 모든 테스트는 임시 폴더를 DATA_DIR(BOT_DATA_DIR) 로 사용하고 Ollama/ntfy 는 mock 이다.
  (운영 데이터 폴더·로그를 건드리지 않는다)
"""

import sys
import os
import io
import json
import shutil
import tempfile
import threading
import unittest
import importlib.util
import contextlib
import urllib.error
from unittest import mock

BOT_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)), "bot.py")
_counter = [0]

SAMPLE_DOC = """# 샘플 지침서

## 1. 개요
이 문서는 샘플입니다.

## 2. 약어 정의
| 약어 | 정의 | 비고 |
|---|---|---|
| ABC | 알파 베타 센터 | 테스트 |
| XYZ | 엑스 와이 존 | |

## 3. 장비 점검 절차
점검 장비는 먼저 전원을 확인합니다.

### 3.1 점검 후 보고
점검 후 담당자에게 보고합니다.
"""


def load_bot(env=None, users=None, doc=None):
    """임시 DATA_DIR 로 bot.py 를 새 모듈 인스턴스로 로드한다. (module, data_dir) 반환"""
    data_dir = tempfile.mkdtemp(prefix="bot_test_")
    if env is not None:
        with open(os.path.join(data_dir, ".env"), "w", encoding="utf-8") as f:
            f.write("\n".join(f"{k}={v}" for k, v in env.items()) + "\n")
    if users is not None:
        with open(os.path.join(data_dir, "users.json"), "w", encoding="utf-8") as f:
            json.dump(users, f, ensure_ascii=False)
    if doc is not None:
        with open(os.path.join(data_dir, "업무프로세스.md"), "w", encoding="utf-8") as f:
            f.write(doc)
    old = os.environ.get("BOT_DATA_DIR")
    old_model = os.environ.pop("MODEL", None)
    os.environ["BOT_DATA_DIR"] = data_dir
    _counter[0] += 1
    try:
        spec = importlib.util.spec_from_file_location(f"bot_under_test_{_counter[0]}", BOT_PATH)
        mod = importlib.util.module_from_spec(spec)
        with contextlib.redirect_stdout(io.StringIO()):
            spec.loader.exec_module(mod)
    finally:
        if old is None:
            os.environ.pop("BOT_DATA_DIR", None)
        else:
            os.environ["BOT_DATA_DIR"] = old
        if old_model is not None:
            os.environ["MODEL"] = old_model
    return mod, data_dir


class FakeResp:
    def __init__(self, content="답변입니다.", **extra):
        body = {"message": {"role": "assistant", "content": content},
                "prompt_eval_count": 100, "eval_count": 20}
        body.update(extra)
        self._raw = json.dumps(body).encode("utf-8")

    def read(self):
        return self._raw

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False


def http_400():
    return urllib.error.HTTPError("http://x/api/chat", 400, "think not supported", {}, io.BytesIO(b"{}"))


def read_jsonl(path):
    if not os.path.exists(path):
        return []
    with open(path, "r", encoding="utf-8") as f:
        return [json.loads(l) for l in f if l.strip()]


class BotTestBase(unittest.TestCase):
    def setUp(self):
        self.dirs = []

    def tearDown(self):
        for d in self.dirs:
            shutil.rmtree(d, ignore_errors=True)

    def make_bot(self, **kw):
        m, d = load_bot(**kw)
        self.dirs.append(d)
        return m, d

    def make_users_bot(self, users=None, doc=SAMPLE_DOC):
        users = users or {"가": "topic-가-aaaa", "나": "topic-나-bbbb"}
        m, d = self.make_bot(env={"DOC_PATH": "", "THINK": "false"}, users=users, doc=doc)
        m.TARGET_DOC_PATH = os.path.join(d, "업무프로세스.md")
        m.DOC_PATHS_CFG = ""
        m.DOC_DIR_CFG = ""
        return m


class TestBotComponents(BotTestBase):
    def test_document_reading(self):
        m, d = self.make_bot(doc=SAMPLE_DOC)
        doc = m.read_document(os.path.join(d, "업무프로세스.md"))
        self.assertIn("샘플 지침서", doc)
        self.assertEqual(m.read_document(os.path.join(d, "없는파일.md")), "")

    def test_prompt_construction(self):
        m, _ = self.make_bot()
        prompt = m.build_system_prompt(SAMPLE_DOC, "- 샘플.md: 1. 개요")
        self.assertIn(SAMPLE_DOC, prompt)
        self.assertIn("📌 참고:", prompt)
        self.assertIn("[문서 제목 목록]", prompt)
        self.assertIn("- 샘플.md: 1. 개요", prompt)
        self.assertNotIn("발췌", prompt)
        self.assertIn("발췌", m.build_system_prompt(SAMPLE_DOC, "- a", partial=True))
        # 제목 목록 없이도 호출 가능 (하위 호환)
        self.assertIn(SAMPLE_DOC, m.build_system_prompt(SAMPLE_DOC))

    def test_no_hardcoded_business_terms(self):
        """프롬프트/코드에 특정 절 번호·업무명·고정 예시 답안이 없어야 한다"""
        with open(BOT_PATH, "r", encoding="utf-8") as f:
            src = f.read()
        for word in ["7.3", "limit", "nofile", "dd 명령어", "ITOM", "AWX", "카카오톡", "nodeinfo",
                     "PXE", "DHCP", "모범 답변"]:
            self.assertNotIn(word, src, f"bot.py 에 특정 업무 문구 하드코딩: {word}")
        m, _ = self.make_bot()
        prompt = m.build_system_prompt("본문", "- a: b")
        for word in ["7.3", "limit", "nofile", "dd 명령어", "ITOM", "AWX"]:
            self.assertNotIn(word, prompt)

    def test_rejection_detection(self):
        m, _ = self.make_bot()
        res_rejected = "문서에 없는 내용입니다."
        self.assertTrue(m.NO_ANSWER_TEXT in res_rejected)

    def test_chunking_and_actions(self):
        m, _ = self.make_bot()
        sent = []
        m._send_ntfy_payload = lambda payload, idx, total, label="": sent.append(payload)
        with mock.patch.object(m.time, "sleep", lambda s: None):
            m.publish_ntfy("이것은 긴 답변 문장입니다.\n" * 100, "q1", topic="T1", label="가")
        self.assertTrue(len(sent) > 1, "1200자 초과 텍스트는 여러 조각으로 분할되어야 함")
        self.assertTrue(all(p["topic"] == "T1" for p in sent))
        with_actions = [p for p in sent if "actions" in p]
        self.assertEqual(len(with_actions), 1, "피드백 버튼은 마지막 조각에만")
        self.assertTrue(all(a["url"].endswith("/T1") for a in with_actions[0]["actions"]))

    def test_feedback_lookup(self):
        m, _ = self.make_bot()
        test_qid = "test_q_123"
        m.QA_CACHE[test_qid] = {"question": "OS 설치 어떻게 해?", "answer": "1. 승인 확인...", "user": "가"}
        m.process_feedback("fb_1", f"good {test_qid}", user="가")
        recs = [r for r in read_jsonl(m.FEEDBACK_LOG) if r.get("id") == test_qid]
        self.assertEqual(len(recs), 1)
        self.assertEqual(recs[0]["rating"], "good")
        self.assertEqual(recs[0]["question"], "OS 설치 어떻게 해?")
        self.assertEqual(recs[0]["user"], "가")

    def test_state_saving_and_loading(self):
        m, _ = self.make_bot()
        st = m.load_state()
        st["last_q_id"] = "test_temp_id"
        m.save_state(st)
        self.assertEqual(m.load_state().get("last_q_id"), "test_temp_id")

    def test_state_save_is_atomic(self):
        m, d = self.make_bot()
        m.save_state({"last_q_id": "A"})
        self.assertFalse(os.path.exists(m.STATE_FILE + ".tmp"))
        with mock.patch.object(m.os, "replace", side_effect=OSError("disk")):
            m.save_state({"last_q_id": "B"})   # 교체 실패해도 예외 없이 기존 파일 유지
        self.assertEqual(m.load_state().get("last_q_id"), "A")
        self.assertFalse(os.path.exists(m.STATE_FILE + ".tmp"))

    def test_qa_cache_limited_to_500_recent(self):
        m, _ = self.make_bot()
        for i in range(m.QA_CACHE_MAX + 20):
            m.cache_qa(f"q{i}", {"question": "q", "answer": "a", "user": "가"})
        self.assertEqual(len(m.QA_CACHE), 500)
        self.assertNotIn("q0", m.QA_CACHE)
        self.assertIn(f"q{m.QA_CACHE_MAX + 19}", m.QA_CACHE)

    def test_feedback_of_other_user_is_ignored_without_leak(self):
        m, _ = self.make_bot()
        m.QA_CACHE["q9"] = {"question": "비밀질문내용", "answer": "비밀답변내용", "user": "나"}
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            m.process_feedback("fb", "good q9", user="가")
        self.assertEqual(read_jsonl(m.FEEDBACK_LOG), [])
        self.assertNotIn("비밀질문내용", buf.getvalue())
        self.assertNotIn("비밀답변내용", buf.getvalue())

    def test_feedback_other_user_in_qa_jsonl_is_ignored(self):
        m, _ = self.make_bot()
        m.append_jsonl(m.QA_LOG, {"id": "q8", "user": "나", "question": "질문X", "answer": "답X"})
        m.process_feedback("fb", "bad q8", user="가")
        self.assertEqual(read_jsonl(m.FEEDBACK_LOG), [])
        m.process_feedback("fb", "bad q8", user="나")
        recs = read_jsonl(m.FEEDBACK_LOG)
        self.assertEqual(len(recs), 1)
        self.assertEqual(recs[0]["question"], "질문X")

    def test_temp_data_dir_isolated(self):
        m, d = self.make_bot()
        self.assertEqual(os.path.normcase(m.DATA_DIR), os.path.normcase(os.path.abspath(d)))
        self.assertNotIn("업무프로세스", os.path.basename(m.LOGS_DIR))


class TestClauseRefs(BotTestBase):
    def setUp(self):
        super().setUp()
        self.m, _ = self.make_bot()

    def test_paren_citation_removed_entirely(self):
        s = self.m.strip_clause_refs("아래 순서로 진행하세요 (§2.2 승인 및 협의 프로세스).")
        self.assertEqual(s, "아래 순서로 진행하세요.")

    def test_multiple_refs_in_one_paren(self):
        s = self.m.strip_clause_refs("확인합니다 (§2.1, §3.2) 후 진행합니다.")
        self.assertEqual(s, "확인합니다 후 진행합니다.")

    def test_nested_parens_inside_citation(self):
        s = self.m.strip_clause_refs("진행합니다 (§2.2 승인 (협의) 절차) 이후 보고합니다.")
        self.assertEqual(s, "진행합니다 이후 보고합니다.")

    def test_mid_sentence_parens_preserved(self):
        src = "서버(자산 시스템)에서 확인(예: 5분 후)하세요. 끝 (§4 완료 보고)\n2. 다음 (참고) 단계"
        s = self.m.strip_clause_refs(src)
        self.assertEqual(s, "서버(자산 시스템)에서 확인(예: 5분 후)하세요. 끝\n2. 다음 (참고) 단계")

    def test_no_marker_unchanged(self):
        src = "일반 문장 (괄호) 그대로 [대괄호]"
        self.assertEqual(self.m.strip_clause_refs(src), src)

    def test_bare_marker_and_trailing_in_paren(self):
        s = self.m.strip_clause_refs("규정 §2.2 에 따릅니다 (예: 5분 §2.1)")
        self.assertNotIn("§", s)
        self.assertIn("(예: 5분)", s)
        self.assertIn("에 따릅니다", s)

    def test_bracket_and_fullwidth(self):
        s = self.m.strip_clause_refs("진행합니다 [§3.1 설치] 그리고（§5 망 변경）끝")
        self.assertEqual(s, "진행합니다 그리고끝")

    def test_paren_with_explanation_keeps_text_removes_only_ref(self):
        s = self.m.strip_clause_refs("작업합니다 (§2.2 에 따라 5분 이내에 처리합니다) 이후 보고.")
        self.assertEqual(s, "작업합니다 (에 따라 5분 이내에 처리합니다) 이후 보고.")

    def test_paren_with_colon_explanation_preserved(self):
        s = self.m.strip_clause_refs("확인 (§3.1: 승인자는 팀장이어야 함) 하세요")
        self.assertNotIn("§", s)
        self.assertIn("승인자는 팀장이어야 함", s)

    def test_pure_multi_ref_with_title_removed(self):
        s = self.m.strip_clause_refs("진행 (§2.1, §3.2 승인 절차) 합니다")
        self.assertEqual(s, "진행 합니다")

    def test_unclosed_paren_does_not_swallow_text(self):
        s = self.m.strip_clause_refs("앞 (§2.2 닫히지 않음\n다음 줄은 그대로")
        self.assertIn("다음 줄은 그대로", s)
        self.assertNotIn("§", s)


class TestLLMCall(BotTestBase):
    def _capture(self, m, responses):
        """urlopen mock: responses 는 순서대로 반환/raise 할 값 목록. 요청 본문을 bodies 에 기록"""
        bodies = []
        it = iter(responses)

        def fake_urlopen(req, timeout=None):
            bodies.append(json.loads(req.data.decode("utf-8")))
            r = next(it)
            if isinstance(r, Exception):
                raise r
            return r
        return bodies, fake_urlopen

    def test_think_false_sent_and_history_inserted(self):
        m = self.make_users_bot()
        bodies, fake = self._capture(m, [FakeResp("응답")])
        with mock.patch.object(m.urllib.request, "urlopen", fake):
            res = m.query_llm("질문3", history=[("질문1", "답1"), ("질문2", "답2")])
        self.assertTrue(res["success"])
        b = bodies[0]
        self.assertIs(b["think"], False)
        roles = [x["role"] for x in b["messages"]]
        self.assertEqual(roles, ["system", "user", "assistant", "user", "assistant", "user"])
        self.assertEqual(b["messages"][1]["content"], "질문1")
        self.assertEqual(b["messages"][4]["content"], "답2")
        self.assertEqual(b["messages"][-1]["content"], "질문3")
        self.assertEqual(b["model"], "qwen3.5:9b-q8_0")
        self.assertEqual(b["options"]["num_ctx"], 24576)

    def test_think_dropped_on_400_and_persists(self):
        m = self.make_users_bot()
        bodies, fake = self._capture(m, [http_400(), FakeResp("재시도 성공"), FakeResp("다음 요청")])
        with mock.patch.object(m.urllib.request, "urlopen", fake):
            r1 = m.query_llm("첫 질문")
            r2 = m.query_llm("두 번째 질문")
        self.assertTrue(r1["success"])
        self.assertEqual(r1["answer"], "재시도 성공")
        self.assertIn("think", bodies[0])
        self.assertNotIn("think", bodies[1])
        self.assertNotIn("think", bodies[2], "한 번 제거한 뒤에는 이후 요청에도 생략")
        self.assertTrue(r2["success"])

    def test_think_unset_when_config_blank(self):
        m, d = self.make_bot(env={"THINK": "none"})
        self.assertIsNone(m.THINK)

    def test_http_error_other_than_think_is_failure(self):
        m = self.make_users_bot()
        err = urllib.error.HTTPError("http://x", 500, "boom", {}, io.BytesIO(b""))
        bodies, fake = self._capture(m, [err])
        with mock.patch.object(m.urllib.request, "urlopen", fake):
            res = m.query_llm("질문")
        self.assertFalse(res["success"])
        self.assertTrue(res["unanswered"])

    def test_answer_strips_clause_refs_and_think_tags(self):
        m = self.make_users_bot()
        bodies, fake = self._capture(m, [FakeResp("<think>속생각</think>1. 진행하세요 (§2.2 승인 및 협의 프로세스).")])
        with mock.patch.object(m.urllib.request, "urlopen", fake):
            res = m.query_llm("질문")
        self.assertEqual(res["answer"], "1. 진행하세요.")


class TestUsers(BotTestBase):
    def test_users_json_autogenerated_default_names(self):
        m, d = self.make_bot(env={"DOC_PATH": ""})
        self.assertTrue(m.USER_MODE)
        path = os.path.join(d, "users.json")
        self.assertTrue(os.path.exists(path))
        with open(path, encoding="utf-8") as f:
            users = json.load(f)
        self.assertEqual(list(users), [f"사용자{i}" for i in range(1, 8)])
        topics = list(users.values())
        self.assertEqual(len(set(topics)), 7)
        for t in topics:
            self.assertTrue(t.startswith("proc-"))
            self.assertGreaterEqual(len(t), len("proc-") + 20)  # token_urlsafe(16) -> 22자

    def test_users_json_from_user_names_env(self):
        m, d = self.make_bot(env={"USER_NAMES": "철수, 영희", "NTFY_TOPIC": "legacy-topic"})
        self.assertTrue(m.USER_MODE)
        self.assertEqual(list(m.USERS), ["철수", "영희"])

    def test_existing_users_json_is_kept(self):
        users = {"가": "topic-가-aaaa"}
        m, d = self.make_bot(users=users)
        self.assertEqual(m.USERS, users)

    def test_legacy_single_topic_without_users_json(self):
        m, d = self.make_bot(env={"NTFY_TOPIC": "legacy-topic"})
        self.assertFalse(m.USER_MODE)
        self.assertTrue(m.IS_SINGLE_TOPIC)
        self.assertEqual(m.NTFY_ANSWER_TOPIC, "legacy-topic")
        self.assertFalse(os.path.exists(os.path.join(d, "users.json")))

    def test_gitignore_has_users_json(self):
        gi = os.path.join(os.path.dirname(BOT_PATH), ".gitignore")
        with open(gi, encoding="utf-8") as f:
            self.assertIn("users.json", f.read().split())

    def test_startup_prints_topics_but_not_token(self):
        m, d = self.make_bot(env={"NTFY_TOKEN": "SECRET_TOKEN_VALUE", "DOC_PATH": ""},
                             users={"가": "topic-가-aaaa"}, doc=SAMPLE_DOC)
        m.TARGET_DOC_PATH = os.path.join(d, "업무프로세스.md")
        self.assertEqual(m.NTFY_TOKEN, "SECRET_TOKEN_VALUE")
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf), \
                mock.patch.object(m, "query_llm", lambda q, **k: {"success": True, "elapsed_sec": 0.1,
                                                                 "prompt_eval_count": 1}), \
                mock.patch.object(m.subprocess, "run", lambda *a, **k: mock.Mock(stdout="")):
            m.startup_warmup()
        out = buf.getvalue()
        self.assertIn("가", out)
        self.assertNotIn("topic-가-aaaa", out, "토픽은 마스킹되어야 함")
        self.assertIn("topic-가-" + "…", out)
        self.assertIn("users.json", out)
        self.assertNotIn("SECRET_TOKEN_VALUE", out)

    def test_startup_shows_full_topics_when_show_topics(self):
        m, d = self.make_bot(env={"DOC_PATH": "", "SHOW_TOPICS": "true"},
                             users={"가": "topic-가-aaaa"}, doc=SAMPLE_DOC)
        m.TARGET_DOC_PATH = os.path.join(d, "업무프로세스.md")
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf), \
                mock.patch.object(m, "query_llm", lambda q, **k: {"success": True, "elapsed_sec": 0.1,
                                                                 "prompt_eval_count": 1}), \
                mock.patch.object(m.subprocess, "run", lambda *a, **k: mock.Mock(stdout="")):
            m.startup_warmup()
        self.assertIn("topic-가-aaaa", buf.getvalue())

    def test_subscribe_stream_default_label_is_not_topic(self):
        m = self.make_users_bot()

        class Stop(BaseException):
            pass

        def boom(*a, **k):
            raise Stop()
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf), mock.patch.object(m.urllib.request, "urlopen", boom):
            with self.assertRaises(Stop):
                m.subscribe_stream("secret-topic-xyz", lambda *a: None, "k")
        self.assertNotIn("secret-topic-xyz", buf.getvalue())
        self.assertIn("구독 시작", buf.getvalue())

    def test_handlers_route_to_own_topic_only(self):
        m = self.make_users_bot()
        calls = []
        m.enqueue_question = lambda msg_id, text, user, topic: calls.append((user, topic, text))
        h_a = m.make_user_handler("가", "topic-가-aaaa")
        h_b = m.make_user_handler("나", "topic-나-bbbb")
        h_a("1", "질문A", {})
        h_b("2", "질문B", {})
        h_a("3", "봇 메시지", {"tags": ["bot"]})   # 봇 자신의 발행은 무시
        h_a("4", "   ", {})                         # 빈 메시지 무시
        self.assertEqual(calls, [("가", "topic-가-aaaa", "질문A"), ("나", "topic-나-bbbb", "질문B")])


class TestFeedbackRouting(BotTestBase):
    def setUp(self):
        super().setUp()
        self.m = self.make_users_bot()
        self.fb, self.q = [], []
        self.m.process_feedback = lambda mid, text, user=None: self.fb.append((text, user))
        self.m.accept_question = lambda mid, text, user, topic: self.q.append((text, user))
        self.m.QA_CACHE["abc123"] = {"question": "q", "answer": "a", "user": "가"}

    def _ev(self, text, user="가"):
        self.m.handle_single_topic_event("1", text, {}, user=user, topic="t")

    def test_real_feedback_is_feedback(self):
        self._ev("good abc123")
        self._ev("BAD abc123")
        self.assertEqual(self.fb, [("good abc123", "가"), ("BAD abc123", "가")])
        self.assertEqual(self.q, [])

    def test_general_question_starting_with_good_is_question(self):
        self._ev("good morning")
        self._ev("bad 서버 상태 확인 방법 알려줘")
        self._ev("good")
        self._ev("good abc123 감사합니다")
        self.assertEqual(self.fb, [])
        self.assertEqual(len(self.q), 4)

    def test_other_users_id_is_not_feedback(self):
        self._ev("good abc123", user="나")
        self.assertEqual(self.fb, [])
        self.assertEqual(self.q, [("good abc123", "나")])

    def test_id_found_only_in_qa_jsonl(self):
        self.m.append_jsonl(self.m.QA_LOG, {"id": "zz9", "user": "나", "question": "q", "answer": "a"})
        self._ev("good zz9", user="나")
        self.assertEqual(self.fb, [("good zz9", "나")])


class TestQueueAndContext(BotTestBase):
    def setUp(self):
        super().setUp()
        self.m = self.make_users_bot()
        self.sent = []
        self.m._send_ntfy_payload = lambda payload, idx, total, label="": self.sent.append(payload)
        self.order = []
        self.histories = []

    def _fake_llm(self, gate=None):
        def fake(question, doc_text=None, history=None):
            self.order.append(question)
            self.histories.append(list(history or []))
            if gate is not None and len(self.order) == 1:
                gate.wait(5)
            return {"success": True, "answer": f"답:{question}", "prompt_eval_count": 1,
                    "eval_count": 1, "elapsed_sec": 0.01, "unanswered": False, "doc_mode": "full"}
        return fake

    def _ask(self, user, topic, text, msg_id=None):
        self.m.accept_question(msg_id or f"id-{len(self.order)}-{text}", text, user, topic)

    def test_fifo_order_and_wait_notice_and_separation(self):
        m = self.m
        gate = threading.Event()
        m.query_llm = self._fake_llm(gate)
        ta, tb = "topic-가-aaaa", "topic-나-bbbb"
        self._ask("가", ta, "Q1", "m1")
        self._ask("나", tb, "Q2", "m2")
        self._ask("가", ta, "Q3", "m3")   # 같은 사용자의 중복 연타도 큐에 모두 들어간다
        self._ask("가", ta, "Q3", "m4")
        gate.set()
        m.JOB_QUEUE.join()

        self.assertEqual(self.order, ["Q1", "Q2", "Q3", "Q3"])
        notices = [p for p in self.sent if "접수되었습니다" in p["message"]]
        self.assertEqual([n["message"] for n in notices],
                         ["접수되었습니다. 앞에 1건 대기 중입니다",
                          "접수되었습니다. 앞에 2건 대기 중입니다",
                          "접수되었습니다. 앞에 3건 대기 중입니다"])
        self.assertEqual([n["topic"] for n in notices], [tb, ta, ta])
        for n in notices:
            self.assertIn("bot", n["tags"])
            self.assertNotIn("actions", n)

        # 답변은 각자의 토픽으로만, 사용자 분리
        answers = [p for p in self.sent if p["message"].endswith(tuple(f"답:{q}" for q in ("Q1", "Q2", "Q3")))]
        by_topic = {}
        for p in answers:
            by_topic.setdefault(p["topic"], []).append(p["message"].rsplit("답:", 1)[1])
        self.assertEqual(by_topic[ta], ["Q1", "Q3", "Q3"])
        self.assertEqual(by_topic[tb], ["Q2"])
        self.assertEqual({p["topic"] for p in self.sent}, {ta, tb})
        # 피드백 버튼도 해당 토픽
        for p in answers:
            for a in p.get("actions", []):
                self.assertTrue(a["url"].endswith("/" + p["topic"]))
        self.assertEqual(m.OUTSTANDING, 0)

    def test_no_notice_when_idle(self):
        m = self.m
        m.query_llm = self._fake_llm()
        self._ask("가", "topic-가-aaaa", "Q1", "m1")
        m.JOB_QUEUE.join()
        self.assertEqual([p for p in self.sent if "접수되었습니다" in p["message"]], [])

    def test_log_uses_user_name_not_topic(self):
        m = self.m
        m.query_llm = self._fake_llm()
        self._ask("가", "topic-가-aaaa", "Q1", "m1")
        m.JOB_QUEUE.join()
        recs = read_jsonl(m.QA_LOG)
        self.assertEqual(recs[-1]["user"], "가")
        with open(m.QA_LOG, encoding="utf-8") as f:
            self.assertNotIn("topic-가-aaaa", f.read())

    def test_three_turn_context_and_fourth_drops_first(self):
        m = self.m
        m.query_llm = self._fake_llm()
        ta = "topic-가-aaaa"
        for i in range(1, 6):
            self._ask("가", ta, f"Q{i}", f"m{i}")
            m.JOB_QUEUE.join()
        self.assertEqual(self.histories[0], [])
        self.assertEqual(self.histories[2], [("Q1", "답:Q1"), ("Q2", "답:Q2")])
        self.assertEqual([q for q, _ in self.histories[3]], ["Q1", "Q2", "Q3"])
        self.assertEqual([q for q, _ in self.histories[4]], ["Q2", "Q3", "Q4"])   # 첫 턴 탈락
        self.assertEqual([q for q, _ in m.get_history("가")], ["Q3", "Q4", "Q5"])

    def test_context_is_per_user(self):
        m = self.m
        m.query_llm = self._fake_llm()
        self._ask("가", "topic-가-aaaa", "QA", "m1")
        m.JOB_QUEUE.join()
        self._ask("나", "topic-나-bbbb", "QB", "m2")
        m.JOB_QUEUE.join()
        self.assertEqual(self.histories[1], [])
        self.assertEqual(m.get_history("나"), [("QB", "답:QB")])

    def test_context_ttl_expiry(self):
        m = self.m
        m.query_llm = self._fake_llm()
        clock = [1000.0]
        m._now = lambda: clock[0]
        self._ask("가", "topic-가-aaaa", "Q1", "m1")
        m.JOB_QUEUE.join()
        clock[0] += m.CONTEXT_TTL_SEC - 1
        self.assertEqual(len(m.get_history("가")), 1)
        clock[0] += 2   # TTL 초과
        self._ask("가", "topic-가-aaaa", "Q2", "m2")
        m.JOB_QUEUE.join()
        self.assertEqual(self.histories[1], [])

    def test_reset_command(self):
        m = self.m
        m.query_llm = self._fake_llm()
        ta = "topic-가-aaaa"
        self._ask("가", ta, "Q1", "m1")
        m.JOB_QUEUE.join()
        self.assertEqual(len(m.get_history("가")), 1)
        self._ask("가", ta, "/reset", "m2")
        self.assertEqual(m.get_history("가"), [])
        resets = [p for p in self.sent if p["message"] == "대화 맥락을 초기화했습니다"]
        self.assertEqual(len(resets), 1)
        self.assertEqual(resets[0]["topic"], ta)
        self.assertEqual(self.order, ["Q1"], "/reset 은 LLM 으로 보내지 않는다")
        self._ask("가", ta, "Q2", "m3")
        m.JOB_QUEUE.join()
        self.assertEqual(self.histories[1], [])

    def test_disclaimer_mentions_three_turns(self):
        m = self.m
        m.query_llm = self._fake_llm()
        self._ask("가", "topic-가-aaaa", "Q1", "m1")
        m.JOB_QUEUE.join()
        msg = [p for p in self.sent if "답:Q1" in p["message"]][0]["message"]
        self.assertIn("3턴", msg)
        self.assertNotIn("맥락은 이어지지", msg)

    def test_llm_error_goes_to_own_topic(self):
        m = self.m
        m.query_llm = lambda q, doc_text=None, history=None: {
            "success": False, "error": "boom", "elapsed_sec": 0, "answer": "",
            "prompt_eval_count": 0, "eval_count": 0, "unanswered": True}
        self._ask("나", "topic-나-bbbb", "Q1", "m1")
        m.JOB_QUEUE.join()
        errs = [p for p in self.sent if p["title"] == "오류 발생"]
        self.assertEqual([p["topic"] for p in errs], ["topic-나-bbbb"])
        self.assertEqual(m.get_history("나"), [])
        self.assertEqual(errs[0]["message"], m.ERROR_REPLY_TEXT)
        self.assertNotIn("boom", errs[0]["message"], "예외 원문은 사용자에게 보내지 않는다")
        self.assertEqual(read_jsonl(m.ERRORS_LOG)[-1]["error"], "boom", "상세는 errors.jsonl 에만")

    def test_worker_exception_notifies_user_with_fixed_text(self):
        m = self.m

        def bad(*a, **k):
            raise RuntimeError("secret-detail")
        m.query_llm = bad
        self._ask("나", "topic-나-bbbb", "Q1", "m1")
        m.JOB_QUEUE.join()
        errs = [p for p in self.sent if p["title"] == "오류 발생"]
        self.assertEqual(len(errs), 1)
        self.assertEqual(errs[0]["topic"], "topic-나-bbbb")
        self.assertEqual(errs[0]["message"], m.ERROR_REPLY_TEXT)
        self.assertNotIn("actions", errs[0])
        self.assertNotIn("secret-detail", json.dumps(self.sent, ensure_ascii=False))
        self.assertTrue(any("secret-detail" in r.get("error", "") for r in read_jsonl(m.ERRORS_LOG)))
        self.assertEqual(m.OUTSTANDING, 0)

    def test_worker_exception_publish_failure_is_ignored(self):
        m = self.m
        m.query_llm = lambda *a, **k: (_ for _ in ()).throw(RuntimeError("x"))

        def pub_fail(*a, **k):
            raise OSError("ntfy down")
        m.publish_ntfy = pub_fail
        self._ask("가", "topic-가-aaaa", "Q1", "m1")
        m.JOB_QUEUE.join()
        self.assertEqual(m.OUTSTANDING, 0)
        self.assertTrue(m._worker_thread.is_alive())


class TestDocuments(BotTestBase):
    def test_glossary_parsed_from_document(self):
        m = self.make_users_bot()
        g = m.parse_glossary(SAMPLE_DOC)
        self.assertEqual([(a, b) for a, b, _ in g], [("ABC", "알파 베타 센터"), ("XYZ", "엑스 와이 존")])
        q = m.gloss_question("ABC 점검은?", g)
        self.assertIn("ABC: 알파 베타 센터", q)
        self.assertNotIn("XYZ", q)
        self.assertEqual(m.gloss_question("관련 없음", g), "관련 없음")

    def test_document_replacement_reflected_without_restart(self):
        m = self.make_users_bot(doc="# 문서\n\n## 1. 첫 절\n원래 내용 알파\n")
        path = m.TARGET_DOC_PATH
        bodies = []

        def fake(req, timeout=None):
            bodies.append(json.loads(req.data.decode("utf-8")))
            return FakeResp("ok")
        with mock.patch.object(m.urllib.request, "urlopen", fake):
            m.query_llm("질문")
            with open(path, "w", encoding="utf-8") as f:
                f.write("# 문서\n\n## 1. 첫 절\n바뀐 내용 베타\n")
            m.query_llm("질문")
            m.query_llm("질문")   # 변경 없음 -> 재적재 기록 없음
        self.assertIn("원래 내용 알파", bodies[0]["messages"][0]["content"])
        self.assertIn("바뀐 내용 베타", bodies[1]["messages"][0]["content"])
        self.assertNotIn("원래 내용 알파", bodies[1]["messages"][0]["content"])
        recs = read_jsonl(m.DOC_CHANGES_LOG)
        self.assertEqual([r["event"] for r in recs], ["load", "change"])
        for r in recs:
            self.assertIn("time", r)
            self.assertGreater(r["bytes"], 0)
            self.assertGreater(r["tokens_est"], 0)
        self.assertEqual(recs[0]["tokens_est"], round(recs[0]["bytes"] / 3.0))

    def _big_doc(self, sections=40, filler=60):
        parts = ["# 큰 문서\n", "## 용어\n| 약어 | 정의 | 비고 |\n|---|---|---|\n| QQ | 큐 큐 | |\n"]
        for i in range(sections):
            parts.append(f"## {i + 1}. 항목{i}\n" + (f"일반 내용 {i} 채움말 " * filler) + "\n")
        parts.append("## 99. 희귀주제 처리\n희귀키워드절차는 특별한 순서로 진행합니다.\n")
        return "\n".join(parts)

    def test_budget_exceeded_switches_to_section_selection(self):
        m = self.make_users_bot(doc=self._big_doc())
        m.NUM_CTX = 3000
        m.ANSWER_RESERVE = 500
        docs, glossary = m.refresh_documents()
        text, titles, partial = m.assemble_documents(docs, "희귀키워드절차 알려줘")
        self.assertTrue(partial)
        self.assertIn("희귀키워드절차는 특별한 순서", text)
        self.assertIn("| QQ | 큐 큐 |", text, "용어 사전 표 절은 항상 포함")
        self.assertNotIn("일반 내용 7 ", text)
        self.assertLess(m.estimate_tokens(text) + m.ANSWER_RESERVE, m.NUM_CTX)
        self.assertTrue(any("희귀주제" in t for _, t in titles))
        # 초과 경고 로그
        self.assertTrue(any("절 선택 모드" in r.get("error", "") for r in read_jsonl(m.ERRORS_LOG)))
        self.assertTrue(read_jsonl(m.DOC_CHANGES_LOG)[0]["over_budget"])

    def test_truncated_first_candidate_respects_token_budget(self):
        """키워드 매칭이 없고 한 절이 예산보다 클 때: 한글 1자≈1토큰 기준으로 잘라야 한다"""
        m = self.make_users_bot(doc="## 1. 거대한 절\n" + "가" * 20000 + "\n")
        m.NUM_CTX = 3000
        m.ANSWER_RESERVE = 500
        docs, _ = m.refresh_documents()
        text, titles, partial = m.assemble_documents(docs, "무관한질문")
        self.assertTrue(partial)
        fixed = m.estimate_tokens(m.build_system_prompt("", "-", partial=True))
        self.assertLessEqual(m.estimate_tokens(text) + fixed + m.ANSWER_RESERVE, m.NUM_CTX)
        self.assertLess(len(text), 3000)

    def test_history_tokens_flip_full_mode_decision(self):
        m = self.make_users_bot(doc=SAMPLE_DOC)
        docs, _ = m.refresh_documents()
        q = "점검 절차 알려줘"
        m.ANSWER_RESERVE = 0
        m.NUM_CTX = m._full_tokens(docs) + m.estimate_tokens(q) + 5
        self.assertFalse(m.assemble_documents(docs, q)[2])
        hist = [("이전 질문 " * 20, "이전 답변 " * 50)]
        self.assertTrue(m.assemble_documents(docs, q, hist)[2], "history 가 예산을 넘기면 절 선택 모드")

    def test_history_reduces_section_budget(self):
        m = self.make_users_bot(doc=self._big_doc())
        m.NUM_CTX = 3000
        m.ANSWER_RESERVE = 500
        docs, _ = m.refresh_documents()
        q = "일반 내용 채움말 항목"
        base = m.assemble_documents(docs, q)[0]
        hist = [("이전 질문입니다 " * 30, "이전 답변입니다 " * 60)]
        with_hist = m.assemble_documents(docs, q, hist)[0]
        self.assertLess(m.estimate_tokens(with_hist), m.estimate_tokens(base))
        self.assertLessEqual(m.estimate_tokens(with_hist) + m._history_tokens(hist)
                             + m.estimate_tokens(m.build_system_prompt("", "-", partial=True))
                             + m.ANSWER_RESERVE, m.NUM_CTX)

    def test_query_llm_passes_history_to_assemble(self):
        m = self.make_users_bot()
        seen = {}
        orig = m.assemble_documents

        def spy(docs, question, history=None):
            seen["history"] = history
            return orig(docs, question, history)
        m.assemble_documents = spy
        with mock.patch.object(m.urllib.request, "urlopen", lambda req, timeout=None: FakeResp("ok")):
            m.query_llm("질문", history=[("a", "b")])
        self.assertEqual(seen["history"], [("a", "b")])

    def test_under_budget_injects_full_document(self):
        m = self.make_users_bot(doc=SAMPLE_DOC)
        docs, _ = m.refresh_documents()
        text, titles, partial = m.assemble_documents(docs, "아무 질문")
        self.assertFalse(partial)
        self.assertIn("점검 후 담당자에게 보고합니다.", text)
        self.assertIn("1. 개요", [t for _, t in titles])

    def test_section_mode_end_to_end_prompt(self):
        m = self.make_users_bot(doc=self._big_doc())
        m.NUM_CTX = 3000
        m.ANSWER_RESERVE = 500
        bodies = []

        def fake(req, timeout=None):
            bodies.append(json.loads(req.data.decode("utf-8")))
            return FakeResp("ok", prompt_eval_count=500)
        with mock.patch.object(m.urllib.request, "urlopen", fake):
            res = m.query_llm("희귀키워드절차 알려줘")
        self.assertEqual(res["doc_mode"], "sections")
        sysmsg = bodies[0]["messages"][0]["content"]
        self.assertIn("발췌", sysmsg)
        self.assertIn("희귀키워드절차는 특별한 순서", sysmsg)

    def test_multiple_documents_doc_paths(self):
        m = self.make_users_bot()
        d = os.path.dirname(m.TARGET_DOC_PATH)
        p1, p2 = os.path.join(d, "문서A.md"), os.path.join(d, "문서B.md")
        with open(p1, "w", encoding="utf-8") as f:
            f.write("## 1. 에이 절\n에이 내용\n\n| 약어 | 정의 |\n|---|---|\n| AA | 에이 정의 |\n")
        with open(p2, "w", encoding="utf-8") as f:
            f.write("## 1. 비이 절\n비이 내용\n")
        m.DOC_PATHS_CFG = f"{p1};{p2}"
        bodies = []

        def fake(req, timeout=None):
            bodies.append(json.loads(req.data.decode("utf-8")))
            return FakeResp("ok")
        with mock.patch.object(m.urllib.request, "urlopen", fake):
            m.query_llm("AA 는?")
        sysmsg = bodies[0]["messages"][0]["content"]
        self.assertIn("[문서 파일: 문서A.md]", sysmsg)
        self.assertIn("[문서 파일: 문서B.md]", sysmsg)
        self.assertIn("에이 내용", sysmsg)
        self.assertIn("비이 내용", sysmsg)
        self.assertIn("- 문서B.md: 1. 비이 절", sysmsg)
        self.assertIn("AA: 에이 정의", bodies[0]["messages"][-1]["content"])

    def test_multiple_documents_doc_dir(self):
        m = self.make_users_bot()
        d = os.path.join(os.path.dirname(m.TARGET_DOC_PATH), "docs")
        os.makedirs(d)
        for n, body in [("b.md", "## 1. 비\n비 내용\n"), ("a.md", "## 1. 에이\n에이 내용\n"), ("c.txt", "무시")]:
            with open(os.path.join(d, n), "w", encoding="utf-8") as f:
                f.write(body)
        m.DOC_DIR_CFG = d
        names = [os.path.basename(p) for p in m.doc_paths()]
        self.assertEqual(names, ["a.md", "b.md"])
        docs, _ = m.refresh_documents()
        self.assertEqual([n for n, _ in docs], ["a.md", "b.md"])

    def test_single_doc_path_still_works(self):
        m = self.make_users_bot()
        self.assertEqual(m.doc_paths(), [m.TARGET_DOC_PATH])
        docs, _ = m.refresh_documents()
        self.assertEqual([n for n, _ in docs], ["업무프로세스.md"])


class TestAnswerFormat(BotTestBase):
    """답변 제목·조각 순서·안내문·버튼·발행 간격·수신 출력·자기 메시지 무시"""

    def setUp(self):
        super().setUp()
        self.m = self.make_users_bot()
        self.sent, self.sleeps = [], []
        self.real_send = self.m._send_ntfy_payload
        self.m._send_ntfy_payload = lambda payload, idx, total, label="": self.sent.append(payload)

    def _answer(self, n_lines):
        return "\n".join(f"줄{i:02d} " + "가" * 95 for i in range(n_lines))

    def _process(self, question, answer):
        m = self.m
        m.query_llm = lambda q, doc_text=None, history=None: {
            "success": True, "answer": answer, "prompt_eval_count": 1, "eval_count": 1,
            "elapsed_sec": 0.01, "unanswered": False, "doc_mode": "full"}
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf), mock.patch.object(m.time, "sleep", self.sleeps.append):
            m.process_question("q1", question, user="가", topic="topic-가-aaaa")
        return buf.getvalue()

    # ---- build_answer_title ----
    def test_title_single_and_multi(self):
        t = self.m.build_answer_title
        self.assertEqual(t("서버 점검 방법", 1, 1), "답변 - 서버 점검 방법")
        self.assertEqual(t("서버 점검 방법", 2, 3), "답변 (2/3) - 서버 점검 방법")

    def test_title_empty_question(self):
        t = self.m.build_answer_title
        self.assertEqual(t("", 1, 1), "답변")
        self.assertEqual(t("  \n \n", 1, 1), "답변")
        self.assertEqual(t(None, 1, 2), "답변 (1/2)")

    def test_title_truncates_over_30_chars(self):
        t = self.m.build_answer_title
        q30 = "가" * 30
        self.assertEqual(t(q30, 1, 1), "답변 - " + q30)
        self.assertEqual(t(q30 + "나", 1, 1), "답변 - " + q30 + "…")

    def test_title_uses_first_nonblank_line(self):
        t = self.m.build_answer_title
        self.assertEqual(t("\n   첫 줄 질문  \n둘째 줄\n셋째", 3, 3), "답변 (3/3) - 첫 줄 질문")

    # ---- 발행 간격 설정 ----
    def test_chunk_interval_default_and_clamp(self):
        self.assertEqual(self.m.CHUNK_INTERVAL, 1.2)
        m_low, _ = self.make_bot(env={"CHUNK_INTERVAL_SEC": "0.5"})
        self.assertEqual(m_low.CHUNK_INTERVAL, 1.1)
        m_hi, _ = self.make_bot(env={"CHUNK_INTERVAL_SEC": "2.0"})
        self.assertEqual(m_hi.CHUNK_INTERVAL, 2.0)

    # ---- 2~3 조각 발행 ----
    def _check_multi(self, n_lines, expect_chunks):
        q = "긴 답변 질문입니다\n둘째 줄"
        self._process(q, self._answer(n_lines))
        n = len(self.sent)
        self.assertEqual(n, expect_chunks)
        # (a) 발행 순서 N -> 1, 제목 (i/N)
        self.assertEqual([p["title"] for p in self.sent],
                         [f"답변 ({i}/{n}) - 긴 답변 질문입니다" for i in range(n, 0, -1)])
        first = self.sent[-1]   # 마지막에 발행된 것이 1/N
        # (b) 안내문은 첫 조각에만
        self.assertTrue(first["message"].startswith("**AI 답변은 100% 정확하지 않습니다.**"))
        self.assertIn("줄00", first["message"])
        for p in self.sent[:-1]:
            self.assertNotIn("100% 정확하지", p["message"])
            self.assertNotIn("/reset", p["message"])
        # 답변 내용은 순서대로 빠짐없이 이어짐
        body = "\n".join(p["message"] for p in reversed(self.sent))
        self.assertEqual([f"줄{i:02d}" for i in range(n_lines)],
                         [l[:3] for l in body.splitlines() if l.startswith("줄")])
        # (c) 버튼은 마지막 조각(N/N)에만
        self.assertIn("actions", self.sent[0])
        self.assertTrue(self.sent[0]["title"].startswith(f"답변 ({n}/{n})"))
        for p in self.sent[1:]:
            self.assertNotIn("actions", p)
        # 태그 bot 유지
        self.assertTrue(all("bot" in p["tags"] for p in self.sent))
        # (d) 조각 사이 sleep 은 CHUNK_INTERVAL(>=1.1) 이상
        self.assertGreaterEqual(len(self.sleeps), n - 1)
        for s in self.sleeps:
            self.assertGreaterEqual(s, self.m.CHUNK_INTERVAL)
            self.assertGreaterEqual(s, 1.1)

    def test_two_chunks(self):
        self._check_multi(15, 2)

    def test_three_chunks(self):
        self._check_multi(30, 3)

    def test_single_chunk_title_disclaimer_and_buttons(self):
        self._process("짧은 질문", "짧은 답변")
        self.assertEqual(len(self.sent), 1)
        p = self.sent[0]
        self.assertEqual(p["title"], "답변 - 짧은 질문")
        self.assertIn("100% 정확하지", p["message"])
        self.assertTrue(p["message"].endswith("짧은 답변"))
        self.assertIn("actions", p)
        self.assertIn("bot", p["tags"])

    def test_disclaimer_never_alone_or_split(self):
        """첫 줄이 매우 길어도 안내문만 단독 조각이 되거나 중간에서 잘리지 않는다"""
        for first_len in (1100, 1000):
            self.sent.clear()
            self._process("질문", "나" * first_len + "\n" + "다" * 300)
            self.assertEqual(len(self.sent), 2)
            first = self.sent[-1]["message"]
            disc_lines = [l for l in first.splitlines() if l.startswith("**")]
            self.assertEqual(len(disc_lines), 2, "안내문 두 줄이 첫 조각에 온전히")
            self.assertTrue(disc_lines[0].endswith("**") and disc_lines[1].endswith("**"))
            self.assertIn("나" * first_len, first, "안내문과 답변 첫 줄이 같은 조각")
            self.assertNotIn("**", self.sent[0]["message"])
        # 첫 줄이 한도 안이면 안내문을 포함한 첫 조각도 1200자 이내
        for p in self.sent:
            self.assertLessEqual(len(p["message"]), 1200)

    def test_other_titles_unchanged(self):
        m = self.m
        m.query_llm = lambda *a, **k: {"success": False, "error": "x", "elapsed_sec": 0, "answer": "",
                                       "prompt_eval_count": 0, "eval_count": 0, "unanswered": True}
        with contextlib.redirect_stdout(io.StringIO()):
            m.process_question("q1", "질문", user="가", topic="t")
        self.assertEqual(self.sent[-1]["title"], "오류 발생")
        self.assertNotIn("100% 정확하지", self.sent[-1]["message"])
        m.publish_ntfy("접수되었습니다. 앞에 1건 대기 중입니다", "q2", title="접수 안내", topic="t", feedback=False)
        self.assertEqual(self.sent[-1]["title"], "접수 안내")
        self.assertNotIn("actions", self.sent[-1])
        self.assertIn("bot", self.sent[-1]["tags"])

    # ---- (e) 터미널 출력 ----
    def test_receive_print_before_enqueue(self):
        m = self.m
        buf = io.StringIO()
        seen = []
        m.enqueue_question = lambda mid, text, user, topic: seen.append(buf.getvalue())
        with contextlib.redirect_stdout(buf):
            m.handle_single_topic_event("1", "첫 줄 질문\n둘째 줄", {}, user="가", topic="topic-가-aaaa")
        out = buf.getvalue()
        self.assertRegex(out, r"\[질문 수신\] 사용자: 가 \| \d{2}:\d{2}:\d{2} \| 질문: 첫 줄 질문\n    둘째 줄")
        self.assertIn("[질문 수신]", seen[0], "큐에 넣기 전에 출력")
        self.assertNotIn("topic-가-aaaa", out)

    def test_process_prints_start_not_receive_and_publish_title(self):
        m = self.m
        m._send_ntfy_payload = self.real_send   # 실제 _send_ntfy_payload 로 로그 확인 (urlopen 은 mock)
        m.NTFY_TOKEN = "SECRET_TOKEN_VALUE"
        with mock.patch.object(m.urllib.request, "urlopen", lambda req, timeout=None: FakeResp()):
            out = self._process("처리 질문\n둘째", self._answer(15))
        self.assertIn("[질문 처리 시작] ID: q1 | 사용자: 가 | 질문: 처리 질문\n", out)
        self.assertNotIn("[질문 수신]", out)
        self.assertIn("[ntfy 발행 성공] 조각 (2/2) -> 가 | 제목: 답변 (2/2) - 처리 질문", out)
        self.assertIn("[ntfy 발행 성공] 조각 (1/2) -> 가 | 제목: 답변 (1/2) - 처리 질문", out)
        self.assertLess(out.index("조각 (2/2)"), out.index("조각 (1/2)"))
        self.assertNotIn("topic-가-aaaa", out)
        self.assertNotIn("SECRET_TOKEN_VALUE", out)

    # ---- (f) 자기 답변 무시 ----
    def test_own_answers_are_not_requeued(self):
        m = self.m
        self._process("긴 질문", self._answer(30))
        self._process("짧은 질문", "짧은 답변")
        calls = []
        m.enqueue_question = lambda *a: calls.append(a)
        m.process_feedback = lambda *a, **k: calls.append(a)
        with contextlib.redirect_stdout(io.StringIO()):
            for p in self.sent:
                ev = {"tags": p["tags"], "title": p["title"]}
                m.handle_single_topic_event("x", p["message"], ev, user="가", topic="topic-가-aaaa")
                m.handle_question_only("x", p["message"], ev)
                # tags 가 빠져도 답변 제목 형식이면 무시 (2차 판별)
                m.handle_single_topic_event("x", p["message"], {"title": p["title"]},
                                            user="가", topic="topic-가-aaaa")
            for title in ["답변", "답변 (1/3)", "답변 - 질문", "답변 (2/2) - 질문"]:
                m.handle_single_topic_event("x", "본문", {"title": title}, user="가", topic="t")
        self.assertEqual(calls, [])
        # 일반 질문(제목 없음, '답변'으로 시작하는 본문/제목 아님)은 그대로 처리
        with contextlib.redirect_stdout(io.StringIO()):
            m.handle_single_topic_event("y", "답변 형식이 궁금해요", {}, user="가", topic="t")
            m.handle_single_topic_event("z", "질문", {"title": "답변해주세요"}, user="가", topic="t")
        self.assertEqual(len(calls), 2)


if __name__ == "__main__":
    unittest.main(verbosity=2)
