# PR_CHECKLIST.md

- [ ] Python 환경 검증 (`py --version` 3.12 이상 확인)
- [ ] 단위 테스트 통과 (`py test_bot_mock.py` 실행, OK 확인. 데이터는 임시 폴더를 쓰는지 확인)
- [ ] 로컬 LLM 구동 검증 (Ollama `qwen3.5:9b-q8_0` 프로세스 100% GPU 확인: `ollama ps`, `.env` 의 MODEL 과 일치)
- [ ] 프롬프트 워밍업 토큰 수 점검 (워밍업 `prompt` 토큰이 NUM_CTX 의 90% 이하, `[경고]` 로그 없음)
- [ ] 60개 테스트케이스 검증 완료 (`py run_tests.py` 결과 CSV 확인). 모델·프롬프트를 바꾼 경우 v2 42문항은 `experiment.py` 로 확인
- [ ] 문서 밖 거절 문구 확인 (F 시리즈 질문에 `문서에 없는 내용입니다.` 포함)
- [ ] 응답 시간 확인: 답변 1건 10초 이내 목표, 3명 동시 질문 시 3번째 응답 30초 이내 목표 (실측값을 PR 설명에 적기)
- [ ] 문서 하드코딩 금지: `bot.py` 에 절 번호·업무명·고정 답변 문구가 없어야 함 (`test_no_hardcoded_business_terms` 통과)
- [ ] 커밋 제외 확인 (`git status`): `.env`, `users.json`, `state.json`, `logs/`, `백업/`, `업무프로세스.md` 는 커밋하지 않음. 현재 `.gitignore` 에는 `users.json` 만 있으므로 나머지는 직접 확인
- [ ] 실험 산출물 확인: `tests/결과*.csv`, `실험/**/비교표.*` 의 답변에 업무 문서 발췌가 들어 있으므로 커밋 전 검토
- [ ] 개인정보 확인: 토픽명·토큰이 README, `.env.example`, 로그, 커밋 메시지에 없는지 확인 (`.env.example` 은 예시 문자열만)
- [ ] README · ARCHITECTURE · `.env.example` 의 기본값(MODEL, NUM_CTX, THINK, CONTEXT_TTL_SEC, ANSWER_RESERVE 등)이 `bot.py` 의 기본값과 일치
- [ ] CHANGELOG.md 항목 추가 및 버전 태그 일치 확인
