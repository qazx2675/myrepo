# awx_script 문서 안내

- **도구 이름**: awx_script (AWX OS 설치 등록 스크립트 묶음)
- **하는 일**: HPC 서버를 OS 설치(AWX)에 등록합니다. 노드정보 수집 → 정리 → MAC 보정 → DNS 점검 → 사양별 yml 분할 → AWX 인벤토리 동기화 → DHCP·PXE 등록 → auto_setup 감시 전달까지 합니다.
- **실행**: bash 01.AWX_nodeinfo_V2.sh (대화형). 01 이 02.source_dhcp_pxe.sh 를 자동으로 실행합니다.
- **본문 파일**: [`awx_script.xhtml`](./awx_script.xhtml)
- **작성 방식**: ASD-STE100 규칙(짧은 문장, 한 단계에 한 동작, 도구 이름 반복)의 질문-답변 형식. Confluence Intelligence 가 각 절만 읽어도 답할 수 있도록 작성했습니다.

## Confluence 에 붙여 넣는 방법

1. `.xhtml` 파일 전체 내용을 복사합니다. `<root>` 래퍼가 없는 Storage Format 본문 조각입니다.
2. Confluence 페이지를 편집 모드로 열고 **Source Editor (`<>`)** 를 엽니다.
3. 복사한 내용을 붙여 넣고 **Apply** 를 누릅니다.
4. `info`·`warning`·`note`·`code`·`toc` 매크로와 표가 정상인지 확인하고 저장합니다.

## 본문 구성

`awx_script.xhtml`:

- 0. 도구 요약 (핵심 사실)
- 1. 어떤 일을 하려면 어떤 명령을 실행하나? (명령 찾기)
- 2. 처음 사용하기 전에 무엇을 준비하나?
- 3. 01.AWX_nodeinfo_V2.sh 는 어떤 순서로 진행하나?
- 4. 02.source_dhcp_pxe.sh 는 무엇을 하나?
- 5. yml 은 어떻게 나누고 splunk 값은 어떻게 정하나?
- 6. 환경변수 참고표
- 7. 문제가 생기면?
- 8. 자주 묻는 질문 (FAQ)
