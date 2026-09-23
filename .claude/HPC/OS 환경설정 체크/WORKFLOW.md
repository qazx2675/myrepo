# 작업 흐름도 — OS 점검 및 환경설정 자동화 (os_check_final_annotated.sh)

OS 배포 후 **점검 → 리포트 → 환경설정 적용 → 적용 대상 재점검**을 한 번에 처리하는 흐름입니다.
함수별 설명은 [README.md](README.md)의 "4.6 함수 목록"을 참고하세요.

```mermaid
flowchart TD
    A["사전 준비<br/>스크립트 상단 [수정필요] 값 채우기 · ${user}.txt 대상 목록"] --> B["gossh -pm 으로 접속 가능 여부 분류<br/>접두사 분류 · 메시지 케이스"]
    B --> C["접속 가능 서버에 OS 점검 스크립트(run.sh) 실행<br/>run_check_script → check.res_${user}"]
    C --> D["report_ldap_info<br/>값 1종 → 1줄 / 2종 이상 → ldap_case{N}_${user}"]
    C --> E["report_splunk_info<br/>값 1종 → 1줄 / 2종 이상 → 값: N대"]
    C --> F["check_lacp<br/>bond_mode=802.3ad 호스트만 코멘트"]
    D --> G{"환경설정 적용 필요?"}
    E --> G
    F --> G
    G -- 아니오 --> Z["끝"]
    G -- 예 --> H["기본 / 추가 환경설정 적용<br/>대상 → SETTING_TARGET_LIST"]
    H --> I["run_post_apply_check<br/>적용 대상만 재점검 → check.res_${user}_postapply"]
    I --> J["report_setting_check_fail<br/>재점검 결과에서 FAIL 만 출력"]
    J --> K["무작위 서버 몇 대 직접 확인"]
```

<details><summary>SVG 이미지로 보기 (workflow.svg)</summary>

![작업 흐름도](workflow.svg)

</details>
