# Claude Code Guidelines

LLM 코딩 실수를 줄이기 위한 행동 지침입니다.

## 1. Think Before Coding

- 가정을 명시적으로 표현하고 불확실하면 질문하기
- 여러 해석이 있으면 제시하되 조용히 선택하지 않기
- "더 간단한 방법이 있다면 언급하고 필요시 반박하기"

## 2. Simplicity First

- 요청된 것만 구현하며 추측적 기능 제외
- 단일 사용 코드에 추상화 없음
- 요청되지 않은 유연성이나 구성 불가
- 불가능한 시나리오에 대한 에러 처리 제외

## 3. Surgical Changes

- 기존 코드 수정 시 인접한 코드를 '개선'하지 말기
- 깨지지 않은 것은 리팩토링하지 않기
- 기존 스타일에 맞추기
- 사용자 요청과 직접 연결된 줄만 변경하기

## 4. Goal-Driven Execution

- 검증 가능한 성공 기준 정의
- 다단계 작업은 간단한 계획 제시
- 명확한 기준이 독립적 반복을 가능하게 함

---

이 지침들은 불필요한 변경 감소, 과도한 복잡성 재작성 방지, 실수 전 질문을 통해 효과를 발휘합니다.

## 5. Jev API (TypeSafe System One)

- 키는 환경변수가 아니라 `~/.jev-claude.env`(없으면 `~/.jev-router.env`)의 `JEV_API_KEY`. 값은 출력·커밋 금지
- `POST https://api.typesafe.ai/v1/systemone` (`model: jev-latest`, `state`, `questions` noul/choice) — 상세는 메모리 `reference_jev_api.md`
- Windows `python3`는 스토어 스텁 → `C:/Users/qazx2/AppData/Local/Programs/Python/Python311/python.exe` 사용
- 저장소 판정·분류는 현재 브랜치가 아니라 `origin/master` 기준으로 읽는다
