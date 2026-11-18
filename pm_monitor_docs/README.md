# pm_monitor 문서 안내

- **도구 이름**: pm_monitor (프로그램 install_mn, 버전 v0.5.0)
- **하는 일**: PM(물리 서버) 의 OS 설치 진행, yum update 후 커널 변경, 서버 설정을 1분마다 감시해 화면(TUI)으로 보여 줍니다.
- **실행**: bash install_mn.sh <호스트파일> [oklist.txt] (root 로 실행)
- **본문 파일**: [`pm_monitor.xhtml`](./pm_monitor.xhtml)
- **작성 방식**: ASD-STE100 규칙(짧은 문장, 한 단계에 한 동작, 도구 이름 반복)의 질문-답변 형식. Confluence Intelligence 가 각 절만 읽어도 답할 수 있도록 작성했습니다.

## Confluence 에 붙여 넣는 방법

1. `.xhtml` 파일 전체 내용을 복사합니다. `<root>` 래퍼가 없는 Storage Format 본문 조각입니다.
2. Confluence 페이지를 편집 모드로 열고 **Source Editor (`<>`)** 를 엽니다.
3. 복사한 내용을 붙여 넣고 **Apply** 를 누릅니다.
4. `info`·`warning`·`note`·`code`·`toc` 매크로와 표가 정상인지 확인하고 저장합니다.

## 본문 구성

`pm_monitor.xhtml`:

- 0. 도구 요약 (핵심 사실)
- 1. 어떤 일을 하려면 어떤 명령이나 키를 쓰나? (명령 찾기)
- 2. 처음 사용하기 전에 무엇을 준비하나?
- 3. OS 설치 상태는 어떻게 판정하나?
- 4. 화면 키는 무엇인가?
- 5. 백그라운드 수집은 어떻게 동작하나?
- 6. 서버 설정 조사(l), 기준표(oklist), res 파일은 어떻게 쓰나? (v0.5.0)
- 7. yum update 모드는 어떻게 쓰나? (v0.4.0)
- 8. 설정 파일(pm_monitor.conf)의 키는 무엇인가?
- 9. 문제가 생기면?
- 10. 자주 묻는 질문 (FAQ)
