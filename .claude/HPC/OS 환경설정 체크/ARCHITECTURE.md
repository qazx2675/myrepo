# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| `os_check_final_annotated.sh` | 스크립트 본체: gossh `-pm` 접속 분류 → OS 점검 → LDAP/SPLUNK/LACP 리포트 → 환경설정 적용 → 재점검 |
| `README.md` | 사전 준비, 실행 방법, 결과 파일, 함수 목록 |
| `WORKFLOW.md` | 작업 흐름도 |
| `PLAN.md` | 개발/변경 계획 메모 |

스크립트 안의 주석 태그로 수정 범위를 구분합니다.

| 태그 | 의미 |
|---|---|
| `[수정필요]` | 환경에 맞게 값을 채우거나 확인할 부분 (예: `select_user`, 파일명 규칙, FAIL 판정 기준) |
| `[수정금지]` | 기존 로직. 건드리면 안 되는 부분 (예: 접두사 분류, 메시지 케이스 6~9) |
| `[연계]` | 다른 함수와 데이터를 주고받는 부분 (예: `run_post_apply_check` ↔ `report_setting_check_fail`) |

수정 요청별로 볼 곳:

- **"리포트 항목 추가"** → `report_*` 함수
- **"재점검 방식 변경"** → `run_post_apply_check` + `report_setting_check_fail` (함께 수정)
- **"점검 스크립트 대상 변경"** → `run_check_script(target_list, output_file)`

## 작업 흐름도

단계별 설명은 [WORKFLOW.md](WORKFLOW.md)를 참고하세요.

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
