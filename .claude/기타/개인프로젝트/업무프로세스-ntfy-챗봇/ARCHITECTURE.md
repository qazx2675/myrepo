# ARCHITECTURE.md

## 시스템 개요 및 구성도

휴대폰(ntfy 앱)에서 질문을 보내면, 집 PC의 `bot.py` 가 큐에 넣고 로컬 LLM(Ollama, 기본 `qwen3.5:9b-q8_0`)이 근거 문서(`업무프로세스.md` 등)만을 바탕으로 답합니다. 답변은 질문한 사용자의 개인 토픽으로만 발행되며, 👍/👎 피드백도 같은 토픽에서 수집합니다.

```
[사용자 A..G 휴대폰 (ntfy 앱)]  각자 자기 개인 토픽 1개만 구독
   │ ① 질문 발행 (개인 토픽)                              ▲ ⑤ 답변 + [👍 좋음] [👎 나쁨] (같은 개인 토픽)
   ▼                                                      │
[ntfy.sh 공개 서버]                                        │
   │ ② 토픽마다 구독 스레드 (사용자 수만큼, since 커서 state.json 저장)
   ▼                                                      │
[bot.py 수신부] ──③ 큐 적재 (사용자·토픽·맥락 epoch)──▶ [FIFO 큐 · 단일 워커]
   │  /reset 은 큐를 거치지 않고 즉시 처리                    │ 앞에 N건 있으면 "접수되었습니다. 앞에 N건 대기 중입니다"
   │                                                      │ 를 해당 토픽으로 즉시 발행
   │                                                      ▼
   │                         [문서 로더] 질문마다 해시 확인 → 변경 시 재적재 (logs/doc_changes.jsonl)
   │                                     · 예산 가드: 문서 토큰 + ANSWER_RESERVE > NUM_CTX 이면 절 선택 모드
   │                                     · 절 선택: ##/### 단위, 용어표 절은 항상 포함, 키워드 점수순
   │                                     · 용어 사전: 문서의 | 약어 | 정의 | 표 자동 파싱 → 질문에 [용어 참고] 추가
   │                                                      ▼
   │                         [프롬프트 빌더] 시스템 프롬프트(문서 + 제목 목록) + 사용자별 최근 3턴 + 질문
   │                                                      ▼
   │                         [Ollama /api/chat] think / num_ctx / keep_alive 등 옵션 전달
   │                                                      ▼
   │                         [후처리] <think> 제거 · § 조항 표기 제거 · 미답변 판정 · 1,200자 단위 분할
   │                                                      │
   │                    맥락 저장 (접수 이후 /reset 이 있었으면 저장 안 함) · 로그 기록
   │                                                      │ ④ 해당 사용자 토픽으로만 발행
   └─ 피드백: 같은 개인 토픽의 "good|bad <질문ID>" 메시지 ──▶ process_feedback ──▶ logs/feedback.jsonl
```

---

## 처리 흐름 요약

1. **수신**: 사용자별 구독 스레드가 자기 토픽의 메시지만 받습니다. 봇이 보낸 메시지(태그 `bot`, 제목 `🤖`)는 무시하여 무한 루프를 막습니다.
2. **분기**: `good`/`bad` 로 시작하면 피드백으로, `/reset` 이면 맥락 초기화로 처리합니다. 그 밖의 질문은 큐에 넣습니다.
3. **큐**: 전 사용자 공용 FIFO 큐 하나를 워커 스레드 하나가 처리합니다. 답변은 직렬로 나가고, 대기 중인 사람에게는 앞에 몇 건인지 알려 줍니다.
4. **문서**: 질문마다 문서 파일 해시를 비교합니다. 바뀌었으면 다시 읽고 용어 사전을 새로 만듭니다. 토큰 예산을 넘으면 질문과 관련된 절만 고릅니다.
5. **답변**: 맥락 3턴과 질문을 붙여 Ollama `/api/chat` 을 호출합니다. 호출이 실패하면 오류 안내를 발행하고 `errors.jsonl` 에 기록합니다. 단, `think` 옵션을 모델이 거부(HTTP 400)한 경우에만 옵션을 빼고 한 번 더 보냅니다.
6. **발행**: 결과를 해당 사용자 토픽으로 발행합니다. 긴 답변은 나누어 역순으로 보내 앱에서 1번부터 읽히게 합니다.

---

## 폴더 및 파일별 역할 (저장소)

| 파일/폴더 | 역할 | 비고 |
|---|---|---|
| `bot.py` | 설정 로드, 사용자 토픽·맥락 관리, 문서 로더, 프롬프트 빌더, Ollama 호출, 큐 워커, ntfy 구독·발행, 피드백 기록 | 핵심 실행 파일. 표준 라이브러리만 사용 |
| `run_tests.py` | 60문항(`tests/테스트케이스.txt`) 일괄 실행, 결과 CSV 저장 | 결과 위치는 `TESTS_OUT_DIR` 로 변경 가능 |
| `experiment.py` | 모델 × 프롬프트 변형 매트릭스 실험, v2 42문항, 안전 게이트 요약, 비교표 생성 | 실험 결과는 `실험/` 아래 |
| `test_bot_mock.py` | 모의 객체 기반 단위 테스트 (문서·프롬프트·큐·사용자 분리·맥락·TTL·/reset·문서 교체/예산/다중 문서) | 데이터는 임시 폴더(`BOT_DATA_DIR`) 사용 |
| `run.bat` | UTF-8 코드페이지 설정 후 `py -u bot\bot.py` 실행 | 실행 위치에 따라 경로 확인 필요 |
| `[1]~[5] *.bat`, `scheduler/manage_service.py` | Ollama 시작·중지, 봇 시작·중지, 상태 확인 | 운영 폴더 `D:\업무프로세스` 에서 실행 |
| `.env.example` | 설정 템플릿 (ntfy 서버·토픽 모드, 모델, 문서 경로, 맥락·예산 옵션) | 예시 값만 담습니다 |
| `tests/` | `테스트케이스.txt` (60문항), `테스트케이스_v2.txt` (42문항), `테스트케이스_v2_기대.md`, `결과_20261010.csv` | |
| `실험/` | 실험 기록: 현황, 문서 분석, 후보 적재 검증, 임시 선정, 사용자 판정 가이드, 단계별 결과 | |
| `계획서.md`, `계획서_v2_답변품질개선.md` | 1차 설계 문서, v2 계획서 | |
| `README.md` | 설치, 실행, 옵션, 면책, 전역 등록 가이드 | 표준 문서 |
| `PR_CHECKLIST.md` | 배포·커밋 전 체크리스트 | 표준 문서 |
| `CHANGELOG.md` | 버전별 변경 이력 | 표준 문서 |
| `.gitignore` | `users.json` 제외 | 현재 이 한 줄만 있음 |

---

## 데이터 저장소 구성 (`D:\업무프로세스`)

| 디렉터리/파일 | 역할 |
|---|---|
| `업무프로세스.md` | 기본 근거 문서 (`DOC_PATH`). 여러 문서는 `DOC_PATHS` 또는 `DOC_DIR` |
| `.env` | 설정 값. 토픽은 직접 적지 않아도 됩니다 (`users.json` 이 관리) |
| `users.json` | 사용자 이름 → 개인 토픽. 최초 실행 시 자동 생성. 토픽은 비밀번호 역할 |
| `state.json` | 사용자별 마지막 처리 메시지 ID (재시작 시 이어받기) |
| `logs/qa.jsonl` | 질문·답변 기록 (ID, 시각, 사용자 이름, 질문, 답변, 토큰 수, 소요 시간, 맥락 턴 수, 문서 모드) |
| `logs/unanswered.jsonl` | 미답변(거절) 질문 |
| `logs/feedback.jsonl` | 👍/👎 피드백 (질문 ID로 원본 질문·답변 연결) |
| `logs/errors.jsonl` | LLM 호출 실패, 문서 예산 초과 경고, 워커 오류 |
| `logs/doc_changes.jsonl` | 문서 적재·변경 이력 (시각, 파일명, 바이트, 토큰 추정, sha256, 예산 초과 여부) |
| `tests/` | 테스트케이스와 결과 CSV |
| `백업/` | 자동 생성 디렉터리 |

---

## 모듈 경계 (bot.py 내부 함수 그룹)

| 그룹 | 주요 함수 |
|---|---|
| 설정 | `load_or_create_env`, `init_users`, `load_users` |
| 사용자·맥락 | `get_history`, `add_turn`, `reset_context`, `get_epoch` |
| 문서 로더 | `doc_paths`, `read_document_raw`, `refresh_documents`, `split_sections`, `parse_glossary`, `gloss_question` |
| 예산·절 선택 | `_full_tokens`, `assemble_documents`, `_section_score`, `_query_terms` |
| 프롬프트 | `build_system_prompt`, `strip_clause_refs` |
| LLM | `query_llm` |
| 큐·처리 | `enqueue_question`, `_worker_loop`, `process_question`, `accept_question` |
| ntfy 입출력 | `subscribe_stream`, `publish_ntfy`, `_send_ntfy_payload`, `make_user_handler` |
| 피드백 | `process_feedback` |

단일 파일을 유지하고 외부 패키지는 추가하지 않습니다.

---

## 수정 팁
1. 답변 규칙이나 거절 조건을 바꾸려면 `bot.py` 의 `build_system_prompt()` 를 수정합니다. 절 번호나 업무명은 넣지 않습니다 (`test_no_hardcoded_business_terms` 가 검사합니다).
2. 문서를 바꾸거나 여러 개로 늘리려면 `.env` 의 `DOC_PATH`, `DOC_PATHS`, `DOC_DIR` 을 수정합니다. 내용만 바꾸면 재시작이 필요 없습니다.
3. 메시지 분할 기준(1,200자)이나 피드백 버튼 형식을 바꾸려면 `publish_ntfy()` 를 수정합니다.
4. 사용자 추가·교체는 `users.json` 을 편집합니다. 자세한 절차는 README 2.2를 봅니다.
5. 모델을 바꾸려면 `.env` 의 `MODEL=` 한 줄을 수정합니다 (README 3.1).
