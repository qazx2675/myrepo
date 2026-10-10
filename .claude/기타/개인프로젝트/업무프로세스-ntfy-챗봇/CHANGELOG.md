# CHANGELOG.md

모든 주목할 만한 변경 사항은 이 문서에 기록됩니다.
버전 태그 생성 시 `git tag -a v0.1.0 -m "업무프로세스 ntfy 챗봇 1차 구현 및 60개 테스트 완료 (CHANGELOG 2026-10-10 항목)"` 형식을 따릅니다.

## [v0.1.0] - 2026-10-10

### 추가 (Added)
- **bot.py**:
  - Python 표준 라이브러리(`urllib`, `json`)만을 사용한 ntfy 실시간 양방향 스트리밍 챗봇 구현
  - Ollama `qwen3:8b` 로컬 LLM 연동 (컨텍스트 24576 토큰, 온도 0.2, 프롬프트 캐싱 최적화)
  - `D:\업무프로세스\업무프로세스.md` 실시간 리로드 및 시스템 프롬프트 주입
  - ntfy 메시지 한도(4096B) 대응 메시지 분할 전송 (1200자 기준)
  - 최종 답변 조각에 👍/👎 http 액션 피드백 버튼 부착
  - 봇 재시작 시 `state.json`을 통한 메시지 이어받기 복구 지원
  - 4종 JSONL 로깅 시스템 (`qa.jsonl`, `unanswered.jsonl`, `feedback.jsonl`, `errors.jsonl`)
- **run_tests.py**:
  - 제7절 자연어 테스트케이스 60개(A~F군) 일괄 실행 및 검증기
  - `결과_YYYYMMDD.csv` 자동 생성 (질문, 답변, 미답변 여부, 토큰 수, 소요시간, 판정, 원인)
  - 제6절 기준 통계 자동 산출 (맞음/부분/틀림 비율, F군 거절률 100%, 평균 응답 시간)
- **test_bot_mock.py**:
  - 모의 환경 단위 테스트 슈트 (문서 로드, 프롬프트 생성, 분할 처리, 피드백 연계 등 6개 항목)
- **run.bat**:
  - Windows 환경 UTF-8 인코딩 및 `py` 런처 기반 실행기
- **산출물**:
  - `ARCHITECTURE.md`, `PR_CHECKLIST.md`, `README.md`, `.env.example`, `.github/workflows/ntfy-chatbot-ci.yml`
