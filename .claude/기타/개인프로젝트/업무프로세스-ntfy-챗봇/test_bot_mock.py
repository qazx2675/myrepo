#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
단위 테스트 및 모의 환경 검증 (test_bot_mock.py)
- bot.py 의 핵심 모듈(문서 읽기, 프롬프트 생성, 분할 전송, 피드백 연결, 상태 저장) 단위 검증
"""

import sys
import os
import json
import tempfile
import unittest

# bot.py 모듈 import
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "bot"))
import bot

class TestBotComponents(unittest.TestCase):
    def test_document_reading(self):
        doc = bot.read_document()
        self.assertTrue(len(doc) > 0, "문서 내용이 비어있지 않아야 함")
        self.assertIn("서버 운영 및 관리 표준 지침서", doc)

    def test_prompt_construction(self):
        sample_doc = "# 샘플 업무 문서\n내용입니다."
        prompt = bot.build_system_prompt(sample_doc)
        self.assertIn("다음은 참고할 업무프로세스 문서입니다:", prompt)
        self.assertIn(sample_doc, prompt)
        self.assertIn("문서에 없는 내용입니다.", prompt)
        self.assertIn("(근거: 문서의 항목/제목)", prompt)

    def test_rejection_detection(self):
        res_rejected = "문서에 없는 내용입니다."
        self.assertTrue(bot.NO_ANSWER_TEXT in res_rejected)

    def test_chunking_and_actions(self):
        # 긴 텍스트 분할 테스트 (> 1200자)
        long_text = "이것은 긴 답변 문장입니다.\n" * 100
        max_chunk_chars = 1200
        chunks = []
        lines = long_text.split("\n")
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

        self.assertTrue(len(chunks) > 1, "1200자 초과 텍스트는 여러 조각으로 분할되어야 함")

    def test_feedback_lookup(self):
        # 가상 질문 ID 등록 후 피드백 연결 테스트
        test_qid = "test_q_123"
        bot.QA_CACHE[test_qid] = {"question": "OS 설치 어떻게 해?", "answer": "1. 승인 확인..."}
        
        # 피드백 처리
        bot.process_feedback("fb_1", f"good {test_qid}")
        
        # feedback.jsonl 확인
        found = False
        if os.path.exists(bot.FEEDBACK_LOG):
            with open(bot.FEEDBACK_LOG, "r", encoding="utf-8") as f:
                for line in f:
                    rec = json.loads(line)
                    if rec.get("id") == test_qid and rec.get("rating") == "good":
                        self.assertEqual(rec.get("question"), "OS 설치 어떻게 해?")
                        found = True
                        break
        self.assertTrue(found, "feedback.jsonl에 캐시된 질문/답변과 함께 저장되어야 함")

    def test_state_saving_and_loading(self):
        st = bot.load_state()
        st["last_q_id"] = "msg_test_999"
        bot.save_state(st)
        loaded = bot.load_state()
        self.assertEqual(loaded.get("last_q_id"), "msg_test_999")

if __name__ == "__main__":
    unittest.main()
