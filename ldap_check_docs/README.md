# ldap_check 문서 안내

- **도구 이름**: ldap_check (실행 파일 ldap_check.sh)
- **하는 일**: 서버의 LDAP · DNS · NTP · autofs 설정 파일이 기준값(ldap_config.conf)과 같은지 점검합니다. 서버가 어느 인프라·사이트에 속하는지 판별합니다.
- **실행**: 한 서버: bash /user/asdf/ldap_check.sh여러 서버: gossh -script -w hosts.txt "bash /user/asdf/ldap_check.sh"
- **본문 파일**: [`ldap_check.xhtml`](./ldap_check.xhtml)
- **작성 방식**: ASD-STE100 규칙(짧은 문장, 한 단계에 한 동작, 도구 이름 반복)의 질문-답변 형식. Confluence Intelligence 가 각 절만 읽어도 답할 수 있도록 작성했습니다.

## Confluence 에 붙여 넣는 방법

1. `.xhtml` 파일 전체 내용을 복사합니다. `<root>` 래퍼가 없는 Storage Format 본문 조각입니다.
2. Confluence 페이지를 편집 모드로 열고 **Source Editor (`<>`)** 를 엽니다.
3. 복사한 내용을 붙여 넣고 **Apply** 를 누릅니다.
4. `info`·`warning`·`note`·`code`·`toc` 매크로와 표가 정상인지 확인하고 저장합니다.

## 본문 구성

`ldap_check.xhtml`:

- 0. 도구 요약 (핵심 사실)
- 1. 어떤 일을 하려면 어떤 명령을 실행하나? (명령 찾기)
- 2. 처음 사용하기 전에 무엇을 준비하나?
- 3. ldap_check 는 무엇을 어떻게 점검하나?
- 4. 결과는 어떻게 읽나?
- 5. 기준 파일(ldap_config.conf)의 키는 무엇인가?
- 6. 문제가 생기면?
- 7. 자주 묻는 질문 (FAQ)
