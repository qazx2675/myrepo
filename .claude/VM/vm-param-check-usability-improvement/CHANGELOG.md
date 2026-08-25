# CHANGELOG

`vm-param-check-usability-improvement`(체크/`-fix` 통합 도구)에 매개변수나 기능이 추가·수정될 때마다 이 파일에 날짜순(최신이 위)으로 기록합니다.

---

## 2026-08-23 — NUMA 노드당 코어수(`config.numaInfo.coresPerNumaNode`) 체크가 Auto 모드를 무시하던 버그 수정

- **문제**: `autoCoresPerNumaNode=true`(vCenter UI: "NUMA 노드 - 전원을 켤 때 할당됨")인 VM은 `coresPerNumaNode`에 지난 전원 켜짐 시점의 값이 그대로 남아있을 뿐이라 무시해야 하는데, 체크가 이 값을 그대로 기대값과 비교해서 **우연히 값이 같으면 OK로 잘못 판정**하고 있었다.
- **조회(`vcenter/client.go`)**: `vm.Config.NumaInfo.AutoCoresPerNumaNode`를 함께 조회하도록 추가. `model.VMInfo`에 `NumaAutoCoresPerNode *bool` 필드 신설(governomi 타입 주석 근거를 코드 주석에 명시).
- **체크(`checker/topology.go`)**: `CheckTopology`에서 `NumaAutoCoresPerNode`가 true면 `NumaCoresPerNode` 값 비교 없이 **"설정없음"으로 처리**하도록 분기 추가(실제값은 "자동(전원을 켤 때 할당됨)"으로 표시, Note에 사유 명시).
- **영향 범위**: 이 폴더의 `vm-param-check`뿐 아니라, 레거시 체크 전용 도구 `vm-param-setting-check`의 `checker/topology.go`/`model/types.go`/`vcenter/client.go`에도 동일한 수정을 함께 적용(같은 버그가 양쪽에 존재).
- **검증**: `go build`/`go vet`/`go test` 전부 통과.

- **파싱(`main.go`)**: 세 플래그 모두 기존엔 ev01만 "ratio 숫자 하나 또는 normal 하나"를 받고 ev02/ev03는 정수 하나만 받았는데, 이제 `-disk`처럼 쉼표로 여러 값을 나열할 수 있고 ratio 숫자와 `normal`을 섞어도 된다(예: `-shares-ev01=4000,normal`). 새 헬퍼 `parseSharesListFlag`가 파싱을 전담(`parseIntListFlag`와 동일한 패턴).
- **체크(`checker/hardware.go`)**: `SharesExpect`를 `EV01 int / EV01Normal bool / EV02,EV03 *int` 구조에서 `EV01,EV02,EV03 []SharesItem`(각 항목은 `{Ratio int, Normal bool}`)으로 재설계. `checkShares`는 이제 허용값 목록 중 **하나라도 실제값과 맞으면 OK**로 판정한다 — CPU/메모리는 서로 독립적으로 판정하므로 "CPU는 ratio로, 메모리는 normal로" 맞는 경우도 둘 다 OK가 된다. 실제값 표시도 `level=custom (ratio=4000)` / `level=normal`처럼 상태를 명확히 보여주도록 통일.
- **영향 범위**: `main.go`(파싱/헬퍼), `checker/hardware.go`(구조체+판정 로직), `demo.go`/`scaletest.go`(고정 데모/스케일값을 새 `checker.RatioShares()` 헬퍼로 감싸도록만 수정 — 실제 판정 로직 변경 없음), `checker/hardware_test.go`(신규 구조체 반영 + 혼합 목록 판정 테스트 추가). ev01이 그룹 미분류("")에서는 체크되지 않는 기존 동작, ev02/ev03 옵션 미지정 시 스킵되는 기존 동작은 그대로 유지.
- **검증**: `go build`/`go vet`/`go test` 전부 통과. vcsim(포트 54322 임시 인스턴스)에 CPU shares=custom/ratio=4000, Memory shares=level normal로 설정한 VM을 만들어 `-shares-ev01=4000,normal`로 체크 — CPU는 ratio 매칭으로 OK, 메모리는 normal 매칭으로 OK, 두 항목 모두 `기대값=4000 또는 normal`로 정확히 표시됨을 확인.

## 2026-08-21 — `numa.vcpu.preferHT` 매개변수 체크/자동교정 추가

- **체크(`checker/preferht.go`, 신규 파일)**: `numa.vcpu.preferHT`를 체크하는 `CheckPreferHT` 함수 추가. 모든 VM에 공통 적용되는 단일 항목이라 그룹(ev01/ev02/ev03)별 옵션이 따로 없다.
  - `-preferHT` 플래그(또는 SPEC_DIR 스펙 파일의 `preferHT=` 옵션)로 값이 **실제로 주어졌을 때만** 체크한다. 주어지지 않으면 이 항목은 콘솔/CSV 어디에도 출력되지 않는다(다른 항목처럼 "설정없음"으로 나오지 않음).
  - 값이 주어졌는데 VM의 ExtraConfig에 키가 없는 경우도 "설정없음"이 아니라 **FAIL**로 처리한다(단순 TRUE/FALSE 토글이라 값이 없으면 곧 기대값이 아니라는 뜻).
- **플래그(`main.go`)**: `-preferHT <값>` 플래그 추가, `expectSet.PreferHT` 필드 추가, `evaluateVM()`에서 `CheckPreferHT` 호출 추가, `specSettableFlags`에 `preferHT` 등록(SPEC_DIR 스펙 파일에서 지정 가능).
- **자동교정(`fixer/plan.go`)**: `advancedOrder`에 `numa.vcpu.preferHT` 추가 — 기존 고급설정 교정 로직(`fixable`/`BuildPlan`)을 그대로 재사용하므로 이 한 줄 추가만으로 `-fix` 실행 시 FAIL 항목이 자동교정 계획에 포함되고, 실제 적용까지 이어진다.
- **검증**: vcsim(포트 54321)에 대해 ① `-preferHT` 미지정 시 출력 없음 ② `-preferHT TRUE` 지정 + 값 없음 → FAIL ③ `-fix` dry-run 계획에 `numa.vcpu.preferHT: (설정없음) -> TRUE` 1건만 정확히 잡힘 ④ 적용 후 재검증에서 OK로 전환 — 4가지 시나리오 모두 실제 vCenter API로 확인함.
- **범위**: 이번 변경은 위 파일들(`checker/preferht.go` 신규, `main.go`/`fixer/plan.go` 각 수 줄)에 한정되며, 기존 체크/교정 로직은 전혀 건드리지 않음.
- **참고**: 이 매개변수를 독립적으로 vCenter에 일괄 적용하는 별도 병렬 스크립트는 `VM_setup/numa_preferht_setting-source/`에 별도로 추가됨(이 도구의 체크/자동교정 로직과는 무관한 독립 스크립트 — 변경 이력은 그 폴더의 README.md 참고).
