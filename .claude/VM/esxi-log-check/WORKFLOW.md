# 작업 흐름도 — esxi-log-check (ESXi 치명 로그 수집·분석)

ESXi 호스트 로그를 모아 패턴 레지스트리로 분류하고 리포트를 만드는 흐름입니다.
메뉴·옵션은 [README.md](README.md)를 참고하세요.

```mermaid
flowchart TD
    A["./run_analyzer.sh (대화형 메뉴)<br/>또는 esxi-log-check 직접 실행"] --> B{"메뉴 / 모드"}
    B -- "1 · 2 실제 서버 (-w 호스트 목록)" --> C["gossh 서브프로세스 호출<br/>vobd.log · vmkernel.log tail(-tailLines) · ipmi_sel"]
    B -- "3 모의 테스트" --> D["esxi_mock_logger<br/>32개 카테고리 · 100여 개 상황 주입"]
    B -- "-input source=파일" --> E["이미 받아 둔 gossh 출력 사용"]
    C --> F["gossh 출력 파싱 (호스트: 줄)"]
    D --> F
    E --> F
    F --> G["패턴 레지스트리 로딩<br/>esxi_critical_patterns.yaml"]
    G --> H["정규표현식 매칭 → 카테고리 · 심각도 · 담당(엔지니어/서버운영)"]
    H --> I["상관관계 분석<br/>이벤트 연쇄 · 무증상 재부팅 후보 (-noCorrelate 로 끔)"]
    I --> J["무응답 호스트 계산 (-hostlist)"]
    J --> K{"출력 방식"}
    K -- text / json --> L["터미널 또는 -out 파일"]
    K -- html --> M["HTML 리포트"]
    K -- "-server / 메뉴 4" --> N["내장 웹 서버 대시보드 (기본 :12345)"]
    L --> O["CRITICAL/HIGH 항목 → 담당자 조치"]
    M --> O
    N --> O
```

<details><summary>SVG 이미지로 보기 (workflow.svg)</summary>

![작업 흐름도](workflow.svg)

</details>
