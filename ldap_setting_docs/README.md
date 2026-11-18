# ldap_setting 문서 안내

- **도구 이름**: ldap_setting (엔진 ldap-config-engine)
- **하는 일**: 여러 서버의 LDAP · DNS · NTP · autofs 설정 파일 7종을 인프라·사이트 기준값으로 바꿉니다. 파일 전체를 덮어쓰지 않습니다. 필요한 키만 바꿉니다.
- **본문 파일**: [`ldap_setting.xhtml`](./ldap_setting.xhtml)
- **작성 방식**: ASD-STE100 규칙(짧은 문장, 한 단계에 한 동작, 도구 이름 반복)의 질문-답변 형식. Confluence Intelligence 가 각 절만 읽어도 답할 수 있도록 작성했습니다.

## Confluence 에 붙여 넣는 방법

1. `.xhtml` 파일 전체 내용을 복사합니다. `<root>` 래퍼가 없는 Storage Format 본문 조각입니다.
2. Confluence 페이지를 편집 모드로 열고 **Source Editor (`<>`)** 를 엽니다.
3. 복사한 내용을 붙여 넣고 **Apply** 를 누릅니다.
4. `info`·`warning`·`note`·`code`·`toc` 매크로와 표가 정상인지 확인하고 저장합니다.

## 본문 구성

`ldap_setting.xhtml`:

- 0. 도구 요약 (핵심 사실)
- 1. 어떤 일을 하려면 어떤 명령을 실행하나? (명령 찾기)
- 2. 처음 사용하기 전에 무엇을 준비하나?
- 3. ldap_setting 은 어떤 파일을 바꾸나?
- 4. auto.appl 은 왜 바로 바뀌지 않나? (대기와 원복)
- 5. 표준 적용 절차는 무엇인가? (안전 순서)
- 6. 잘못 적용했으면 어떻게 되돌리나? (롤백)
- 7. 옵션·설정 키 참고표
- 8. 문제가 생기면?
- 9. 자주 묻는 질문 (FAQ)
