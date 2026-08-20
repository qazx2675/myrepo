# CHANGELOG

`vm-param-check-usability-improvement`(체크/`-fix` 통합 도구)에 매개변수나 기능이 추가·수정될 때마다 이 파일에 날짜순(최신이 위)으로 기록합니다.

---

## 2026-08-21 — `numa.vcpu.preferHT` 매개변수 체크/자동교정 추가

- **체크(`checker/preferht.go`, 신규 파일)**: `numa.vcpu.preferHT`를 체크하는 `CheckPreferHT` 함수 추가. 모든 VM에 공통 적용되는 단일 항목이라 그룹(ev01/ev02/ev03)별 옵션이 따로 없다.
  - `-preferHT` 플래그(또는 SPEC_DIR 스펙 파일의 `preferHT=` 옵션)로 값이 **실제로 주어졌을 때만** 체크한다. 주어지지 않으면 이 항목은 콘솔/CSV 어디에도 출력되지 않는다(다른 항목처럼 "설정없음"으로 나오지 않음).
  - 값이 주어졌는데 VM의 ExtraConfig에 키가 없는 경우도 "설정없음"이 아니라 **FAIL**로 처리한다(단순 TRUE/FALSE 토글이라 값이 없으면 곧 기대값이 아니라는 뜻).
- **플래그(`main.go`)**: `-preferHT <값>` 플래그 추가, `expectSet.PreferHT` 필드 추가, `evaluateVM()`에서 `CheckPreferHT` 호출 추가, `specSettableFlags`에 `preferHT` 등록(SPEC_DIR 스펙 파일에서 지정 가능).
- **자동교정(`fixer/plan.go`)**: `advancedOrder`에 `numa.vcpu.preferHT` 추가 — 기존 고급설정 교정 로직(`fixable`/`BuildPlan`)을 그대로 재사용하므로 이 한 줄 추가만으로 `-fix` 실행 시 FAIL 항목이 자동교정 계획에 포함되고, 실제 적용까지 이어진다.
- **검증**: vcsim(포트 54321)에 대해 ① `-preferHT` 미지정 시 출력 없음 ② `-preferHT TRUE` 지정 + 값 없음 → FAIL ③ `-fix` dry-run 계획에 `numa.vcpu.preferHT: (설정없음) -> TRUE` 1건만 정확히 잡힘 ④ 적용 후 재검증에서 OK로 전환 — 4가지 시나리오 모두 실제 vCenter API로 확인함.
- **범위**: 이번 변경은 위 파일들(`checker/preferht.go` 신규, `main.go`/`fixer/plan.go` 각 수 줄)에 한정되며, 기존 체크/교정 로직은 전혀 건드리지 않음.
- **참고**: 이 매개변수를 독립적으로 vCenter에 일괄 적용하는 별도 병렬 스크립트(`numa_preferht_setting-source`, `legacy-vm-param-fix-external-orchestration/`에 추가 예정)는 이 변경에 포함되지 않음 — 별도로 작업 진행.
