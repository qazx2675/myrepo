# PR_CHECKLIST.md

- [ ] Python 환경 검증 (`py --version` 3.12 이상 확인)
- [ ] 모의 환경 단위 테스트 통과 (`py bot/test_bot_mock.py` 실행 및 OK 확인)
- [ ] 로컬 LLM 구동 검증 (Ollama `qwen3:8b` 프로세스 100% GPU 확인: `ollama ps`)
- [ ] 프롬프트 워밍업 토큰 수 점검 (num_ctx 24576 대비 90% 이하 유지 확인)
- [ ] 60개 자연어 테스트케이스 검증 완료 (`py bot/run_tests.py` 결과 CSV 확인)
- [ ] 미답변/거절 문구 100% 동작 확인 (F1~F10 "문서에 없는 내용입니다." 일치)
- [ ] 응답 시간 목표(이후 질문 30초 이내) 충족 확인
- [ ] .env 및 개인정보(토픽명, 비밀값)가 Git에 커밋되지 않는지 확인
- [ ] CHANGELOG.md 항목 추가 및 버전 태그 일치 확인
