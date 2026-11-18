# stress-test 문서 안내

- **도구 이름**: stress-test (부하테스트 실행·판정 도구)
- **하는 일**: AWX 에 서버를 등록하고 부하테스트 템플릿(a) cpu, b) stress-ng)을 실행합니다. 그다음 gossh 로 서버의 1분 로드(/proc/loadavg)를 감시합니다. 부하가 실제로 걸렸는지 실행/미실행 으로 판정합니다.
- **본문 파일**: [`stress_test.xhtml`](./stress_test.xhtml)
- **작성 방식**: ASD-STE100 규칙(짧은 문장, 한 단계에 한 동작, 도구 이름 반복)의 질문-답변 형식. Confluence Intelligence 가 각 절만 읽어도 답할 수 있도록 작성했습니다.

## Confluence 에 붙여 넣는 방법

1. `.xhtml` 파일 전체 내용을 복사합니다. `<root>` 래퍼가 없는 Storage Format 본문 조각입니다.
2. Confluence 페이지를 편집 모드로 열고 **Source Editor (`<>`)** 를 엽니다.
3. 복사한 내용을 붙여 넣고 **Apply** 를 누릅니다.
4. `info`·`warning`·`note`·`code`·`toc` 매크로와 표가 정상인지 확인하고 저장합니다.

## 본문 구성

`stress_test.xhtml`:

- 0. 도구 요약 (핵심 사실)
- 1. 어떤 일을 하려면 어떤 명령을 실행하나? (명령 찾기)
- 2. 처음 사용하기 전에 무엇을 준비하나?
- 3. stress-test 는 어떤 순서로 동작하나?
- 4. 대화형 실행(stress_run.sh)은 어떻게 하나?
- 5. 결과는 어떻게 읽나?
- 6. setup_check 와는 어떻게 연결되나?
- 7. 옵션·설정 참고표
- 8. 문제가 생기면?
- 9. 자주 묻는 질문 (FAQ)
