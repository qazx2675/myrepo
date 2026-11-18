# vm-ip-change 문서 안내

- **도구 이름**: vm-ip-change (vCenter API IP 자동변경)
- **하는 일**: SSH 로 접속할 수 없는 RHEL 8.10 VM 의 IP 를 바꿉니다. 네트워크 대신 vCenter → ESXi → VMware Tools(Guest Operations API)를 거쳐 VM 안에서 nmcli 를 실행합니다.
- **실행**: ./bin/vm-ip-change -vcenter vcenter.txt -list list.txt
- **본문 파일**: [`vm_ip_change.xhtml`](./vm_ip_change.xhtml)
- **작성 방식**: ASD-STE100 규칙(짧은 문장, 한 단계에 한 동작, 도구 이름 반복)의 질문-답변 형식. Confluence Intelligence 가 각 절만 읽어도 답할 수 있도록 작성했습니다.

## Confluence 에 붙여 넣는 방법

1. `.xhtml` 파일 전체 내용을 복사합니다. `<root>` 래퍼가 없는 Storage Format 본문 조각입니다.
2. Confluence 페이지를 편집 모드로 열고 **Source Editor (`<>`)** 를 엽니다.
3. 복사한 내용을 붙여 넣고 **Apply** 를 누릅니다.
4. `info`·`warning`·`note`·`code`·`toc` 매크로와 표가 정상인지 확인하고 저장합니다.

## 본문 구성

`vm_ip_change.xhtml`:

- 0. 도구 요약 (핵심 사실)
- 1. 어떤 일을 하려면 어떤 명령을 실행하나? (명령 찾기)
- 2. 처음 사용하기 전에 무엇을 준비하나?
- 3. 안전하게 실행하는 순서는?
- 4. VM 안에서 무엇을 바꾸나?
- 5. IP 를 이전 값으로 되돌리려면?
- 6. 실행 뒤에 무엇을 확인하나?
- 7. 문제가 생기면?
- 8. 자주 묻는 질문 (FAQ)
