# 작업 흐름도 — vm-ip-change (vCenter Guest Operations 로 IP 재설정)

네트워크로 접속할 수 없는 VM 의 IP 를 vCenter → ESXi → VMware Tools 경로로 바꾸는 흐름입니다.
자세한 설명은 [README.md](README.md) 3장 "실행 흐름" 을 참고하십시오.

```mermaid
flowchart TD
    A["입력<br/>vcenter.txt · list.txt (VM표시이름 새IP)<br/>VC_USER/VC_PASSWORD · GUEST_USER/GUEST_PASSWORD"] --> B["대상 목록 출력<br/>호스트네임 → 새 IP (게이트웨이 = 마지막 옥텟 1)"]
    B --> C["vcenter.txt 순서대로 접속"]
    C --> D["아직 처리 못 한 호스트네임과 같은 VM 검색"]
    D --> E{"전원 ON + VMware Tools 실행 중?"}
    E -- 아니오 --> F["[FAIL] 기록 후 건너뜀"]
    E -- 예 --> G["최대 16대 동시 처리 (타임아웃 없음)"]
    D --> C2{"남은 vCenter 있음?"}
    C2 -- 예 --> C
    C2 -- 아니오 --> H["어디서도 못 찾은 호스트네임 → [FAIL]"]
    G --> G1["① 연결 확인<br/>nmcli con show --active 첫 연결(lo 제외), 원래 설정 캡처"]
    G1 --> G2["② IP 설정 적용<br/>ipv4.addresses/gateway manual, /24 고정"]
    G2 --> G3["③ 연결 재기동<br/>nmcli con up (그 연결만)"]
    G3 --> I["[OK]"]
    I --> J["진행 화면 / 결과 요약"]
    F --> J
    H --> J
    J --> K{"실패 있음?"}
    K -- 예 --> L["종료 코드 1"]
    K -- 아니오 --> M["종료 코드 0"]
    J -.->|Ctrl+C 3회| N["즉시 중단, 종료 코드 130"]
    L --> O["콘솔로 무작위 VM 몇 대 접속해 IP 확인"]
    M --> O
```

<details><summary>SVG 이미지로 보기 (workflow.svg)</summary>

![작업 흐름도](workflow.svg)

</details>

## 시험 흐름 (실제 vCenter 없이)

```mermaid
flowchart LR
    A["./setup.sh<br/>vm-ip-change + fake-vcenter 빌드"] --> B{"시험 방식"}
    B -- 자동 --> C["./e2e-test.sh<br/>VM 20대, OK 수 = 실제 변경 수 확인 → PASS/FAIL"]
    B -- 수동 --> D["터미널 A: ./fake-vcenter.sh (-vms -slow -fail ...)"]
    D --> E["터미널 B: 출력된 명령으로 vm-ip-change 실행"]
    E --> F["터미널 A: Ctrl+C → testrun/report.txt"]
```

<details><summary>SVG 이미지로 보기 (workflow-test.svg)</summary>

![작업 흐름도](workflow-test.svg)

</details>
