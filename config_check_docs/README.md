# config_check 문서 안내

- **도구 이름**: config_check (실행 파일 config_check.sh, Bash)
- **하는 일**: 여러 HPC 서버의 OS 설정을 점검합니다. 사용자가 고르면 설정을 적용합니다(y 또는 set). 적용 뒤 다시 점검하고 결과 리포트(벤더별 코멘트 포함)를 출력합니다.
- **본문 파일**: [`config_check.xhtml`](./config_check.xhtml)
- **작성 방식**: ASD-STE100 규칙(짧은 문장, 한 단계에 한 동작, 도구 이름 반복)의 질문-답변 형식. Confluence Intelligence 가 각 절만 읽어도 답할 수 있도록 작성했습니다.

## Confluence 에 붙여 넣는 방법

1. `.xhtml` 파일 전체 내용을 복사합니다. `<root>` 래퍼가 없는 Storage Format 본문 조각입니다.
2. Confluence 페이지를 편집 모드로 열고 **Source Editor (`<>`)** 를 엽니다.
3. 복사한 내용을 붙여 넣고 **Apply** 를 누릅니다.
4. `info`·`warning`·`note`·`code`·`toc` 매크로와 표가 정상인지 확인하고 저장합니다.

## 본문 구성

`config_check.xhtml`:

- 0. 도구 요약 (핵심 사실)
- 1. 어떤 일을 하려면 어떤 명령을 실행하나? (명령 찾기)
- 2. 처음 사용하기 전에 무엇을 준비하나?
- 3. 실행 모드 3가지는 어떻게 다른가?
- 4. 표준 작업 절차는 무엇인가? (안전 순서)
- 5. 결과 리포트는 어떻게 읽나?
- 6. auto_setup·setup_check 와는 어떻게 연결하나?
- 7. 설정·파일 참고표
- 8. 문제가 생기면? (롤백 포함)
- 9. 자주 묻는 질문 (FAQ)
