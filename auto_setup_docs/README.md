# auto_setup 문서 안내

- **도구 이름**: auto_setup (데몬 + CLI + TUI 상태판)
- **하는 일**: AWX 로 OS 를 설치한 서버를 자동으로 감시합니다. 서버가 READY 가 되면 LDAP 설정을 복원하고 config_check.sh(설정체크 + 설정수정)를 자동으로 실행합니다. 결과를 4자리 code 로 저장합니다.
- **본문 파일**: [`auto_setup.xhtml`](./auto_setup.xhtml)
- **작성 방식**: ASD-STE100 규칙(짧은 문장, 한 단계에 한 동작, 도구 이름 반복)의 질문-답변 형식. Confluence Intelligence 가 각 절만 읽어도 답할 수 있도록 작성했습니다.

## Confluence 에 붙여 넣는 방법

1. `.xhtml` 파일 전체 내용을 복사합니다. `<root>` 래퍼가 없는 Storage Format 본문 조각입니다.
2. Confluence 페이지를 편집 모드로 열고 **Source Editor (`<>`)** 를 엽니다.
3. 복사한 내용을 붙여 넣고 **Apply** 를 누릅니다.
4. `info`·`warning`·`note`·`code`·`toc` 매크로와 표가 정상인지 확인하고 저장합니다.

## 본문 구성

`auto_setup.xhtml`:

- 0. 도구 요약 (핵심 사실)
- 1. 어떤 일을 하려면 어떤 명령이나 키를 쓰나? (명령 찾기)
- 2. 처음 사용하기 전에 무엇을 준비하나?
- 3. auto_setup 은 어떤 순서로 자동 진행하나?
- 4. "보류" 서버는 무엇이고 어떻게 처리하나? (늦은 READY 보류)
- 5. TUI 상태판은 어떻게 사용하나?
- 6. 추가 기능은 어떻게 사용하나?
- 7. os8_mgmt 와 os6_mgmt 는 어떻게 다르게 동작하나?
- 8. 설정 키(auto_setup.conf)는 무엇이 있나?
- 9. 주요 시간 기준은 무엇인가?
- 10. 문제가 생기면?
- 11. 자주 묻는 질문 (FAQ)
