# setup_check 문서 안내

- **도구 이름**: setup_check (프로그램 new_setup, 바이너리 bin/new_setup, RHEL 6 용 bin/new_setup_os6)
- **하는 일**: 새로 OS 를 설치한 서버의 설치 상태를 판정합니다. 결과를 8열 표(hostname|상태|설정체크|CPU테스트|stress-ng|H/T|OS|비고)로 출력합니다.
- **본문 파일**: [`setup_check.xhtml`](./setup_check.xhtml)
- **작성 방식**: ASD-STE100 규칙(짧은 문장, 한 단계에 한 동작, 도구 이름 반복)의 질문-답변 형식. Confluence Intelligence 가 각 절만 읽어도 답할 수 있도록 작성했습니다.

## Confluence 에 붙여 넣는 방법

1. `.xhtml` 파일 전체 내용을 복사합니다. `<root>` 래퍼가 없는 Storage Format 본문 조각입니다.
2. Confluence 페이지를 편집 모드로 열고 **Source Editor (`<>`)** 를 엽니다.
3. 복사한 내용을 붙여 넣고 **Apply** 를 누릅니다.
4. `info`·`warning`·`note`·`code`·`toc` 매크로와 표가 정상인지 확인하고 저장합니다.

## 본문 구성

`setup_check.xhtml`:

- 0. 도구 요약 (핵심 사실)
- 1. 어떤 일을 하려면 어떤 명령을 실행하나? (명령 찾기)
- 2. 처음 사용하기 전에 무엇을 준비하나?
- 3. 기본 판정은 어떻게 하나?
- 4. -run 은 어떤 순서로 진행하나?
- 5. 설정 파일(new_setup.conf)의 키는 무엇인가?
- 6. 결과 파일과 종료 코드는 무엇인가?
- 7. 문제가 생기면?
- 8. 자주 묻는 질문 (FAQ)
