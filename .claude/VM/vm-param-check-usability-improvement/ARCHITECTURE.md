# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| `README.md` | 프로젝트 소개, 빠른 시작, 갱신 방법 |
| `WORKFLOW.md` | 체크 → 교정 → 재검증 흐름도, 배포·갱신 흐름도 |
| `CHANGELOG.md` / `계획서.md` | 변경 이력 / 설계 배경·검증 근거 |
| `update.sh` | [폐쇄망] 업데이트 패키지의 실행 파일만 교체, 사용자 파일 보존 |
| `make_update_package.sh` | 폐쇄망용 업데이트 패키지(정적 빌드 + SHA256SUMS) 생성 |
| `update_deploy.sh` | [인터넷 되는 서버] standalone 브랜치로 제자리 갱신 + 재빌드 |
| `vm-param-check/main.go` | CLI 진입점: 옵션 파싱, `-specRoot` 병합, 전체 흐름 |
| `vm-param-check/config/` | 스펙 자동매칭(`spec.go`), `-initFolder`(`init.go`), Task 폴더 예외(`portgroup.go`), 대상 목록(`targets.go`) |
| `vm-param-check/vcenter/client.go` | vCenter 접속, 2단계 조회(`FetchVMs`), 폴더/포트그룹 조회 |
| `vm-param-check/checker/` | 항목별 체크: 하드웨어·토폴로지·affinity·전원정책·preferHT·고정값 |
| `vm-param-check/fixer/` | `-fix` 계획(`plan.go`), 게이트(`gates.go`), dry-run 출력(`describe.go`), 병렬 적용(`apply.go`) |
| `vm-param-check/report/` | 콘솔 / CSV / 요약 출력 |
| `vm-param-check/model/types.go` | 공용 데이터 구조체 |
| `vm-param-check/setup.sh` | 오프라인 빌드 (`../../../공통/govendor/govmomi-0.39.0`을 `vendor`로 링크) |
| `vm-param-check/folder_setup.sh`, `vm_setting_check_insert.sh` | 스펙 생성 / 체크·`-fix` 실행 대화형 래퍼 |

수정 요청별로 볼 곳:

- **"체크 항목 추가"** → `checker/` + `report/`(출력 컬럼) + 필요하면 `fixer/plan.go`(자동교정 대상 여부)
- **"스펙 키 추가"** → `config/spec.go`(파싱) + `main.go`(옵션 병합)
- **"조회가 느리다"** → `vcenter/client.go`의 2단계 조회
- **"교정 안전장치 변경"** → `fixer/gates.go`
- 같은 코드의 사본이 `../V2/…`, `../integrated-vm-param-check-test-tool/vm-param-check`, `../vm-param-setting-check`에도 있으므로 공유 코드 버그는 사본에도 똑같이 반영하세요.

## 작업 흐름도

단계별 설명은 [WORKFLOW.md](WORKFLOW.md)를 참고하세요.

```mermaid
flowchart TD
    A["실행<br/>VC_USER / VC_PASS, -vcenterList, -f 대상 목록"] --> B{"기대값을 어떻게 정하나?"}
    B -- "옵션 직접 지정" --> C["-ht -cores -numa -cpu -mem -disk -shares-ev01 ..."]
    B -- "-specRoot" --> D["VM이 속한 vCenter 폴더명 조회"]
    D --> D1{"정식 CAE 폴더?"}
    D1 -- 예 --> D2["폴더명 정규화 → SPEC_DIR/폴더/_spec.txt 매칭"]
    D1 -- "아니오 (Task 폴더)" --> D3["포트그룹 이름으로 원래 폴더명 추정<br/>실패하면 대화형 입력"]
    D3 --> D2
    D2 --> D4["매칭 결과 확인 (-yes 면 생략)"]
    C --> E
    D4 --> E["2단계 조회<br/>① 전체 VM 이름만 가볍게 → ② 대상 VM만 무거운 속성"]
    E --> E1{"못 찾은 대상 / 접속 실패 vCenter?"}
    E1 -- 예 --> E2["경고 출력 (시작·끝 양쪽)"] --> F
    E1 -- 아니오 --> F["항목별 체크<br/>하드웨어 · 토폴로지 · affinity · 전원정책 · preferHT"]
    F --> G["콘솔 요약 + CSV 2종<br/>상세 / _summary (-user 접미사)"]
    G --> H{"-fix ?"}
    H -- 아니오 --> Z["끝"]
    H -- 예 --> I{"게이트<br/>그룹 동질성 + 대상 전원 OFF"}
    I -- 실패 --> Z2["아무것도 바꾸지 않고 중단"]
    I -- 통과 --> J["dry-run: VM별 변경 내역 출력<br/>수동조치 항목은 개수만"]
    J --> K{"y/N 확인<br/>(-yes 여도 -fix 확인은 항상 물음)"}
    K -- N --> Z3["취소 (변경 없음)"]
    K -- y --> L["VM당 Reconfigure 1회로 적용<br/>워커풀 병렬 (-fixConcurrency)"]
    L --> M["교정된 VM만 재조회 → 같은 판정으로 재검증<br/>_recheck_시각.csv"]
    M --> N["무작위 VM 몇 대 직접 확인"]
```
