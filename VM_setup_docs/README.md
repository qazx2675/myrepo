# VM_setup 문서 안내

- **도구 이름**: VM_setup (VM 자동화 V1, 레거시 CLI 도구 모음)
- **하는 일**: ESXi 호스트를 vCenter 에 등록하고, 라이선스·vSwitch 를 설정하고, VM ev01 ~ ev03 을 만들고, affinity·HugePage·NUMA·태그를 설정하고, MAC 목록을 만듭니다. vm-param-fix 로 체크 CSV 를 읽어 설정을 보정합니다.
- **실행**: 통합 실행 스크립트가 없습니다. 하위 도구를 순서대로 하나씩 실행합니다.
- **본문 파일**: [`vm_setup.xhtml`](./vm_setup.xhtml)
- **작성 방식**: ASD-STE100 규칙(짧은 문장, 한 단계에 한 동작, 도구 이름 반복)의 질문-답변 형식. Confluence Intelligence 가 각 절만 읽어도 답할 수 있도록 작성했습니다.

## Confluence 에 붙여 넣는 방법

1. `.xhtml` 파일 전체 내용을 복사합니다. `<root>` 래퍼가 없는 Storage Format 본문 조각입니다.
2. Confluence 페이지를 편집 모드로 열고 **Source Editor (`<>`)** 를 엽니다.
3. 복사한 내용을 붙여 넣고 **Apply** 를 누릅니다.
4. `info`·`warning`·`note`·`code`·`toc` 매크로와 표가 정상인지 확인하고 저장합니다.

## 본문 구성

`vm_setup.xhtml`:

- 0. 도구 요약 (핵심 사실)
- 1. 어떤 일을 하려면 어떤 도구를 실행하나? (명령 찾기)
- 2. 처음 사용하기 전에 무엇을 준비하나?
- 3. 어떤 순서로 실행하나?
- 4. vm-param-fix 는 어떻게 보정하나?
- 5. 하위 도구의 옵션은 무엇인가?
- 6. VM_setup(V1) 과 V2 는 무엇이 다른가?
- 7. 문제가 생기면?
- 8. 자주 묻는 질문 (FAQ)
