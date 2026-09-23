# 작업 흐름도 — ldap_setting (LDAP · DNS · NTP · autofs 일괄 적용)

자산현황을 읽어 사이트별 적용 스크립트를 만들고 `gossh`로 배포한 뒤 `ldap_check`로 검증하는 흐름입니다.
옵션은 [README.md](README.md), 파일별 역할은 [ARCHITECTURE.md](ARCHITECTURE.md)를 참고하세요.

```mermaid
flowchart TD
    A["준비<br/>conf/ldap_config.conf · conf/assets.txt (hostname TAB site)"] --> B["1) deploy_ldap.sh -infra X -dry-run<br/>무엇이 바뀔지 확인 (파일 변경 없음)"]
    B --> C["2) ldap-config-engine -host 소수노드<br/>로그인 · id · ls /appl 확인"]
    C --> D{"문제 없음?"}
    D -- 아니오 --> R["rollback (.bak.시각 복원) · 원인 수정"] --> B
    D -- 예 --> E["3) deploy_ldap.sh -infra X<br/>전체 적용 + 검증"]
    E --> F["엔진: 자산현황을 사이트별로 묶음"]
    F --> G["사이트별 적용 스크립트 1개 생성<br/>(노드별 아님 → gossh 호출은 사이트 수만큼)"]
    G --> H["base64 파이프로 gossh 전송 · 실행"]
    H --> I["노드 현장 판정<br/>RHEL 버전 · hostname s4 예외"]
    I --> J["7개 파일 키 단위 갱신<br/>ldap.conf · autofs_ldap_auth.conf · autofs.conf<br/>nslcd/sssd · resolv.conf · ntp/chrony · auto.appl"]
    J --> K["변경 전 원본 .bak.시각 백업<br/>바뀐 게 없으면 NOCHANGE"]
    K --> L["바뀐 파일에 대응하는 서비스만 재시작<br/>nslcd · sssd · autofs · ntpd · chronyd"]
    L --> M["ldap_check.sh 로 3중 교차 검증<br/>(4) 나중에 -check-only 로 재검증)"]
    M --> N["무작위 노드 몇 대 직접 확인"]
```

<details><summary>SVG 이미지로 보기 (workflow.svg)</summary>

![작업 흐름도](workflow.svg)

</details>
