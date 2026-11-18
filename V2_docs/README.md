# V2 문서 안내

- **도구 이름**: V2 (VM 자동화 V2: vm_setup.sh + vm-param-check)
- **하는 일**: BM(ESXi 호스트)마다 VM ev01 ~ ev99 를 만들고, 포트그룹·CPU 토폴로지·affinity·HugePage·PCI 패스스루·전원정책·라이선스·태그를 설정합니다. 그 다음 VM 설정이 스펙과 같은지 체크하고, 다르면 교정합니다.
- **실행**: cd VMsetup && ./vm_setup.sh
- **본문 파일**: [`vm_v2.xhtml`](./vm_v2.xhtml)
- **작성 방식**: ASD-STE100 규칙(짧은 문장, 한 단계에 한 동작, 도구 이름 반복)의 질문-답변 형식. Confluence Intelligence 가 각 절만 읽어도 답할 수 있도록 작성했습니다.

## Confluence 에 붙여 넣는 방법

1. `.xhtml` 파일 전체 내용을 복사합니다. `<root>` 래퍼가 없는 Storage Format 본문 조각입니다.
2. Confluence 페이지를 편집 모드로 열고 **Source Editor (`<>`)** 를 엽니다.
3. 복사한 내용을 붙여 넣고 **Apply** 를 누릅니다.
4. `info`·`warning`·`note`·`code`·`toc` 매크로와 표가 정상인지 확인하고 저장합니다.

## 본문 구성

`vm_v2.xhtml`:

- 0. 도구 요약 (핵심 사실)
- 1. 어떤 일을 하려면 어떤 명령을 실행하나? (명령 찾기)
- 2. 처음 사용하기 전에 무엇을 준비하나?
- 3. vm_setup.sh 는 어떤 순서로 진행하나?
- 4. 스펙(SPEC_DIR)은 어떻게 쓰나?
- 5. vm-param-check 로 체크·교정하려면?
- 6. 하위 도구를 단독으로 실행하려면?
- 7. 문제가 생기면?
- 8. 자주 묻는 질문 (FAQ)
