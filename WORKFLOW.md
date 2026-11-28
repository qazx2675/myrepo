# 작업 흐름도 — ip_change (RHEL 노드 IP · 게이트웨이 일괄 변경)

관리 노드에서 apply 스크립트 하나를 만들어 `gossh`로 대상 노드에 보내고, 노드에서 설정 파일(ifcfg·keyfile)의 필요한 줄만 바꾸는 흐름입니다. 재시작·재부팅은 하지 않으며 새 IP 는 다음 재부팅 때 적용됩니다.
자세한 설명은 [README.md](README.md)의 "실행 흐름"을 참고하세요.

```mermaid
flowchart TD
    A["scripts/run_ip_change.sh<br/>(또는 bin/ip-change-engine 직접)"] --> B["select_user_context<br/>작업 계정 선택 → conf/${RUN_USER}.txt"]
    B --> C["대상 로드 (hostname 변경될ip)<br/>게이트웨이 = 새 IP 마지막 옥텟 1"]
    C --> D["작업 대상 목록 출력"]
    D --> E["대상 전체용 apply 스크립트 1개 조립<br/>(apply_body.sh go:embed)"]
    E --> F["base64 로 gossh 일괄 전송 · 실행<br/>(키 인증, 실패 노드는 -p 로 재시도)"]
    F --> G["노드: 장치 선택<br/>SSH_CONNECTION 의 서버 IP → 없으면 default route"]
    G --> H["설정 파일 탐색 (장치 이름 기준)<br/>nmcli 읽기 → ifcfg → keyfile"]
    H --> I["변경 또는 새 프로필 생성<br/>static · DHCP→static · new-file · already"]
    I --> J["백업 /var/lib/ip_change/backup/STAMP/ + manifest<br/>(-check 이면 쓰지 않고 CHECK)"]
    J --> K["다시 읽어 검증<br/>(재시작·재부팅·nmcli 쓰기 없음)"]
    K --> L["OK 점검: ONBOOT · 경쟁 프로필 · 링크 · 예전 게이트웨이<br/>RESULT 줄 (OK/WARN/FAIL/CHECK)"]
    L --> M["관리 노드: 결과 표 · ip_change_result_시각.txt<br/>예전 게이트웨이 질문 [y/N]"]
    M --> N["무작위 노드 몇 대 직접 확인 → 재부팅 때 새 IP 적용"]
```

<details><summary>SVG 이미지로 보기 (workflow.svg, 이전 흐름 기준이므로 위 도표를 우선하십시오)</summary>

![작업 흐름도](workflow.svg)

</details>
