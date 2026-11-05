# 작업 흐름도 — awxkit (Ansible AWX 조작 도구)

설정 파일을 준비하고 `doctor`로 점검한 뒤 **[S1] NodeInfo → [S2] 인벤토리 동기화 → [S3] DHCP 등록 → [S4] PXE 등록** 순서로 진행하는 흐름입니다.
각 단계의 옵션은 [README.md](README.md)를 참고하세요.

```mermaid
flowchart TD
    A["설정 파일 준비<br/>conf/${user}_setting.conf<br/>(./conf → ~/.awxkit → 바이너리 위치/conf 순으로 탐색)"] --> B["doctor.sh<br/>AWX 연결 · 권한 · 템플릿 · ask_variables_on_launch 점검"]
    B --> C{"[X] 항목 있음?"}
    C -- 예 --> C1["해당 항목 해결"] --> B
    C -- 아니오 --> D["(선택) ls.sh / survey.sh<br/>템플릿 · survey 항목 탐색"]
    D --> E["[S1] nodeinfo.sh<br/>${user}.txt hostname 전체를 한 번에 템플릿 실행 → 결과 파일 1개"]
    E --> F["별도 스크립트가 yaml 을 폐쇄망 git 에 업로드"]
    F --> G["[S2] invsync.sh -file yaml<br/>인벤토리 소스 필드 저장 → 소스 동기화 → 호스트 목록 조회"]
    G --> H["[S3] dhcp.sh -infra<br/>s3_template 실행 → 최종 상태 출력"]
    H --> I["[S4] pxe.sh<br/>인프라 · OS 버전 · Boot Mode · Splunk 조합 → s4_template<br/>대상 목록 출력 후 y/N"]
    I --> J["s4_inventory 호스트 수 리포트"]
    J --> K["무작위 대상 몇 대 DHCP/PXE 등록 직접 확인"]
```

<details><summary>SVG 이미지로 보기 (workflow.svg)</summary>

![작업 흐름도](workflow.svg)

</details>
