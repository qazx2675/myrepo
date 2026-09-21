# CHANGELOG

`vm-param-check-usability-improvement`(체크/`-fix` 통합 도구)에 매개변수나 기능이 추가·수정될 때마다 이 파일에 날짜순(최신이 위)으로 기록합니다.

---

## 2026-09-22 — V2: ev01~ev10 그룹, `-specExport`, 스펙 `키=""` 형식

V2(`.claude/VM/V2/vm-param-check-usability-improvement`)는 원본 복사본에서 시작했다. 원본 폴더는 수정하지 않았다.

- **ev01~ev10 그룹**: `-cores/-numa/-cpu/-mem/-disk/-shares-evNN`, `-affinity-evNN` 플래그를 ev02~ev10(affinity는 ev01~ev10)까지 반복문으로 등록한다. 기존 이름은 그대로다. 그룹 판정(`model.ClassifyGroup`)은 VM 이름에 들어 있는 `ev01`~`ev10` 중 첫 매치다. ev02~ev10 규칙은 예전 ev02/ev03과 같다(값이 있으면 체크, 없으면 스킵, 조사 대상이 1대뿐이면 스킵).
  - **동작 변경**: 이름에 `ev04`~`ev10`이 들어간 VM은 예전에는 "미분류"로 보고 기본값(`-cpu` 등)으로 체크했지만, 이제는 해당 그룹으로 보고 그 그룹 값이 있을 때만 체크한다. CSV 정렬 순서는 공통 → 호스트 → ev01 … ev10 → 네트워크.
  - 기대값 구조체는 `EV02/EV03` 필드 대신 `Groups map[그룹]값`을 쓴다(`checker/hardware.go`, `checker/topology.go`). `fixer.GroupOf`, `GroupCountWarning`, CSV `sourceRank`도 같은 목록을 쓴다.
- **`-specExport=<폴더명>`**(신규): vCenter에 연결하지 않고 `-specRoot` 아래 스펙을 VMsetup(`vm_setup.sh`)이 VM 생성에 쓸 수 있게 `이름=값` 줄로 출력한다. 첫 줄 `groups=<ev 개수>`, ev01의 이름 없는 키(`cpu` 등)는 `cpu-ev01`로 정규화, affinity 상대경로는 스펙 폴더 기준 절대경로로 바꾼다. **ev01 필수, 값이 있는 ev는 cpu/mem/disk/shares가 모두 있어야 하고, ev 번호는 연속이어야 한다**(어기면 stderr + 종료코드 1). 체크 동작(`-specRoot`)에는 이 규칙을 적용하지 않는다(기존 스펙이 막히지 않도록).
- **스펙 파일 `키=""`**: vim 수동 입력 템플릿용으로 값의 양끝 큰따옴표를 뗀다. **따옴표째 비워 둔 값(`cpu=""`)은 "지정 안 함"으로 건너뛴다.** 따옴표 없는 `cpu=`는 예전처럼 빈 값 옵션으로 남긴다(`-initFolder` 빈 틀 동작 유지 — 이걸 건너뛰게 했더니 기존 `TestInitFolderBlank`가 깨져서 범위를 좁혔다).
- **`-initFolder` 빈 틀**: ev03 주석 블록을 "ev03~ev10은 ev02와 같은 이름 규칙" 안내로 바꿨다.
- **영향 범위**: `main.go`(플래그 등록/기대값 조립/`evaluateVM` affinity 분기/`specSettableFlags`/`-specExport`), `model/group.go`(신규), `checker/hardware.go`, `checker/topology.go`, `config/spec.go`, `config/export.go`(신규), `config/init.go`, `fixer/plan.go`(`GroupOf`), `fixer/gates.go`(대수 경고 그룹 목록, 안 쓰게 된 `containsFold` 제거), `report/csv.go`, `demo.go`/`scaletest.go`(구조체 리터럴), 테스트 `model/group_test.go`·`config/export_test.go`·`checker/groups_test.go`(신규), `setup.sh`(vendor 링크 경로 `../../govendor/govmomi-0.39.0`).
- **검증(록키 192.168.0.58)**: `go build`/`go vet`/`go test ./...` 통과(기존 테스트 전부 + 신규). **회귀**: 수정 전/후 바이너리의 `-demo`, `-scale 300` 출력(콘솔·상세 CSV 6,121줄·요약 CSV)이 **바이트 단위로 동일**.

## 2026-09-20 — `-fix` 게이트: ev01/ev02 짝(VM 대수)이 안 맞아도 막지 않고 경고만 출력

- **변경**: ev01/ev02/ev03 그룹의 VM 대수가 서로 다르면 교정을 중단하던 조건을 게이트에서 뺐다. 대신 다르면 `[경고] 그룹별 VM 대수가 다릅니다(PASS/FAIL 무관, 조회된 VM 전부 기준): ev01=2대, ev02=1대 — 짝이 맞지 않지만 교정은 그대로 진행합니다`를 출력하고 계속 진행한다. 대수는 PASS/FAIL과 무관하게 조회한 VM 전부로 세고, 접미사가 없는 VM("기타")과 비교할 그룹이 하나뿐인 경우는 경고도 없다.
- **코드**: `fixer/gates.go` — 게이트(`CheckGates`)에서 대수 검사를 제거하고, 같은 로직을 경고 문구를 돌려주는 `GroupCountWarning(vms)`로 분리(`checkHomogeneity`는 그룹 내 스펙 비교만 남음). `main.go`의 `runFix`가 게이트 직전에 이 문구를 출력만 한다. 그룹 내 스펙 비교(코어/소켓·NUMA 제외), 전원 OFF 게이트, 실제 변경 전 y/N 확인은 그대로.
- **영향 범위**: `fixer/gates.go`, `main.go`(경고 출력 4줄), `fixer/gates_test.go`(대수 관련 테스트를 "막지 않고 경고"로 재작성), `fixer/plan_test.go`(`TestGateGroupCount`를 같은 방향으로 수정), 도구 `README.md`("게이트" 설명), `make_update_package.sh`(패키지 이름에 시각 추가: `YYYYMMDD-HHMM`, 같은 날 여러 번 만들어도 겹치지 않게). **이 변경은 Go 코드뿐이라 빌드된 `vm-param-check` 실행파일만 교체하면 반영된다**(`update.sh`로 충분, 스크립트/템플릿/`SPEC_DIR` 변경 없음).
- **검증(록키 192.168.0.58)**: `go build`/`go vet`/`go test ./...` 통과. 같은 테스트 하나를 옛/새 `gates.go`에 돌려 옛 코드는 `[동질성 검증 실패] 그룹별 VM 대수가 다릅니다…ev01=2대, ev02=1대`로 막고 새 코드는 통과함을 확인. **호출부 연결은 vcsim으로 실행 확인**: `aaev01`/`bbev01`/`aaev02`(2:1, 전부 전원 OFF)에 `-fix`(stdin 비움)를 돌려 `[경고] … ev01=2대, ev02=1대 …`가 출력되고 → 동질성·전원 OFF 게이트 통과 → 확인 프롬프트까지 진행됨(설정 변경 없음). **한계**: 실 vCenter(192.168.0.50)에는 ev 그룹 VM이 1:1로 두 대뿐이라 짝이 안 맞는 구성을 실 인벤토리로는 만들 수 없어 vcsim으로 확인했다.

## 2026-09-20 — 폐쇄망용 `update.sh` + 업데이트 패키지 빌더 `make_update_package.sh`

- **문제**: `update_deploy.sh`는 서버가 GitHub에 접속할 수 있다는 전제(`git clone`)로 만들어서, 폐쇄망 서버에서는 애초에 실행할 수 없었다.
- **`update.sh`(신규)**: `bash update.sh "사용중인디렉토리"`. `git`/`go`/인터넷 없이 bash와 기본 명령만으로 동작. 패키지의 `payload/`에 들어 있는 **바뀐 파일만** 반영하고 나머지는 건드리지 않는다. 교체 전에 ① `SHA256SUMS`로 패키지 손상 확인 ② 새 실행파일을 이 서버에서 먼저 시험(`-demo`, vCenter 접속 안 함, 결과는 임시 폴더) — 실행이 안 되면 아무것도 바꾸지 않고 중단 ③ 이전 파일을 `<디렉토리>.update_backup.<시각>/`에 백업. `-n` 미리보기, 이미 최신이면 종료. `01.*`, `vcenter.txt`, `SPEC_DIR/`, `*.csv`, `*.log`는 payload에 같은 이름이 있어도 건너뜀. 공백·한글 경로 지원.
- **`make_update_package.sh`(신규)**: 인터넷 되는 빌드 서버(Go 필요)에서 `vm-param-check/setup.sh`로 **정적 빌드**(`CGO_ENABLED=0`, linux/amd64 — 서버 glibc 버전이 달라도 실행)하고 `dist/vm-param-check-update-YYYYMMDD-HHMM.tar.gz`(update.sh + payload + SHA256SUMS + VERSION.txt)를 만든다. 바뀐 파일이 더 있으면 `payload/`에 같은 상대경로로 넣으면 된다.
- **영향 범위**: `update.sh`, `make_update_package.sh`, `.gitignore`(`dist/`) 신규, `README.md`(폐쇄망 절 신설·디렉토리 트리). 도구 코드와 `update_deploy.sh`(인터넷 되는 서버용)는 변경 없음. 독립 브랜치 `vm-param-check-standalone`에는 이번 항목을 반영하지 않았다(브랜치 재조립 필요 시 `.claude/동질성-게이트-완화-및-배포-갱신-스크립트-개선/작업기록.md` 참고).
- **검증(록키 192.168.0.58, 시나리오 35건 전부 통과)**: **네트워크를 끊은 상태(`unshare -n`)** 에서 실행. ① 공백·한글이 든 경로에서 사용자 파일 다수(`01.vm_setting_check_insert.sh`, `vcenter.txt`, `SPEC_DIR` 5개 파일, csv, log, 템플릿, 소스)의 해시가 실행파일 외 전부 동일 ② 실행파일이 새 것으로 교체(sha256 == payload)·755·동작 ③ 백업에는 옛 실행파일만 ④ 재실행 "이미 최신"·끝에 `/` 붙은 경로 ⑤ `-n` 미리보기는 전체 해시 동일 ⑥ 전송 중 깨진 패키지 → 중단·무변경 ⑦ 이 서버에서 실행 안 되는 실행파일 → 중단·무변경·백업 없음 ⑧ 잘못된 디렉토리/인자 거부 ⑨ payload에 사용자 파일 이름이 섞여도 건너뜀. 정적 빌드 실행파일을 실 vCenter(192.168.0.50)에 실행해 체크·`-fix`(stdin 비움) 결과가 기존과 동일하고 설정이 바뀌지 않음을 확인.

## 2026-09-20 — `update_deploy.sh`를 사용자 파일을 보존하는 제자리 갱신으로 재작성 + 독립 배포 브랜치 `vm-param-check-standalone`

- **문제 1 — 사용자 파일이 사라짐(`update_deploy.sh`)**: 기존 스크립트는 배포 폴더를 통째로 `<경로>.bak.<시각>`으로 옮기고 새로 복사했다. 그래서 배포 경로에 같이 놓여 있던 `01.vm_setting_check_insert.sh`, `vcenter.txt`, `SPEC_DIR/`, 대상 목록 `*.txt`, 결과 `*.csv`가 배포 경로에서 전부 사라졌다(백업 폴더에만 남음).
- **문제 2 — 갱신 후 빌드 실패(vendor 공유화의 부작용)**: 9/18 커밋(`f8a2e7e`)으로 이 도구의 `vendor/`가 저장소에서 빠지고 `.claude/공통/govendor/govmomi-0.39.0`을 가리키는 심볼릭 링크(`setup.sh`가 생성)로 바뀌었다. 평평한 배포 경로에는 그 상대경로가 없어서 기존 `update_deploy.sh`의 `go build -mod=vendor`가 `inconsistent vendoring`으로 실패하고, 1번 때문에 이미 옮겨진 뒤라 **실행파일조차 없는 상태**로 끝났다. 록키(192.168.0.58)에서 사용자 파일을 심어둔 배포 경로로 그대로 재현 확인.
- **스크립트(`update_deploy.sh`, 전면 재작성)**:
  - 새 버전에 있는 파일만 그 자리에서 덮어쓰고, 배포 경로에만 있는 파일은 옮기지도 지우지도 않는다.
  - 임시 폴더에서 **먼저 빌드**하고 성공했을 때만 배포 경로를 바꾼다. 실패하면 아무것도 안 바뀐 채 중단.
  - 덮어쓰는 파일과 이전 실행파일은 `<배포경로>.update_backup.<시각>/`에 백업.
  - `01.*`, `vcenter.txt`, `SPEC_DIR/`, `*.csv`, `*.log`는 저장소에 같은 이름이 생겨도 덮어쓰지 않음. `vm_setting_check_insert.sh`/`folder_setup.sh`/`testfiles/*`는 배포본과 다르면 덮어쓰지 않고 `<이름>.new`로 옆에 저장. `vendor`가 심볼릭 링크인 배포본은 링크 너머를 건드리지 않음.
  - 도구가 없는 비어 있지 않은 폴더를 배포 경로로 잘못 지정하면 거부. `-n/--dry-run`, `REPO_URL`/`REPO_BRANCH` 환경변수 지원. 바뀔 게 없으면 "이미 최신"으로 종료.
- **독립 브랜치(`vm-param-check-standalone`)**: 루트가 이 프로젝트 폴더(`vm-param-check-usability-improvement/`)인 독립 히스토리 브랜치(기존 `gossh-standalone`과 같은 방식). master와 달리 `vm-param-check/vendor/`를 **실제 파일로** 포함하고(공유 govendor와 git 트리 해시가 동일함을 확인한 그 내용), `setup.sh`는 링크를 걸지 않는 버전, `.gitignore`는 `vendor` 줄을 뺀 버전이다. 폴더만 떼어가도 오프라인 빌드가 된다. `update_deploy.sh`가 소스를 여기서 받는다.
- **영향 범위**: `update_deploy.sh`, `README.md`(프로젝트 — 배포 갱신 절 신설, 폐쇄망 안내에 master는 폴더만 떼어가면 빌드 불가 주의 추가, 디렉토리 트리 설명), 신규 브랜치 `vm-param-check-standalone`. 도구 코드(`vm-param-check/`)는 이 항목에서 바뀐 것 없음.
- **검증(록키 192.168.0.58, 시나리오 36건 + 이름 충돌 1건 전부 통과)**: ① 배포 경로 없음 → 신규 설치·`-demo` 동작 ② 사용자 파일 6종이 있는 옛 배포본 갱신 → 사용자 파일 sha256 전부 동일, 편집한 템플릿은 `.new`로 분리, 소스 갱신·백업·새 실행파일 동작 ③ 즉시 재실행 → "이미 최신" ④ `--dry-run` → 배포 트리 해시 동일·백업 없음 ⑤ 새 버전 빌드 실패 → 종료코드≠0, 배포 트리 해시 동일 ⑥ 도구 없는 폴더 지정 → 거부 ⑦ vendor 링크 → 링크 너머 내용 그대로 ⑧ 브랜치를 받아 `setup.sh`로 오프라인 빌드(`GOPROXY=off`) ⑨ 저장소에 `01.vm_setting_check_insert.sh`와 같은 이름의 파일이 생긴 최악의 경우에도 사용자 파일 그대로.

## 2026-09-20 — `-fix` 동질성 게이트: 코어/소켓·NUMA 비교 제외, 그룹 대수는 PASS 포함 전체 기준

- **문제 1 — 고치려는 불일치 때문에 교정이 막힘**: 게이트가 같은 그룹 안의 VM끼리 코어/소켓(`hardware.numCoresPerSocket`), NUMA(`numa.vcpu.maxPerVirtualNode`) 등이 같은지 비교했는데, 이 둘은 `-fix`가 기대값으로 직접 고치는 항목이다. FAIL인 VM은 값이 다를 수밖에 없어서 `동질성 검증 실패: ev01 그룹 내 스펙 불일치 … 코어/소켓 X≠Y`로 정상적인 교정이 중단됐다. (참고: 코어/소켓은 NUMA에서 파생되는 값이 아니라 vCenter의 별도 설정이며, 둘 다 고칠 수 있는 항목이라는 점에서 같은 부류다.)
- **문제 2 — PASS VM이 있으면 그룹 대수가 틀리게 계산됨**: 그룹 간 VM 대수를 교정 대상(`targets`)으로 세서, PASS인 VM(교정 대상에서 빠짐)이 있으면 실제 구성과 무관하게 대수가 달라 보였다. 예) ev01 PASS 1 + FAIL 1 / ev02 FAIL 2 = 실제 2:2인데 ev01=1대, ev02=2대로 계산돼 `그룹별 VM 대수가 다릅니다`로 중단.
- **게이트(`fixer/gates.go`)**:
  - `diffSpec`에서 코어/소켓과 NUMA 비교를 제거. vCPU·메모리·디스크·CPU Shares·HT는 계속 비교(메모리/디스크/Shares는 이 도구가 못 고치는 수동조치 항목이라 다르면 진짜로 다른 스펙). 근거를 함수 주석에 명시.
  - `checkHomogeneity`가 그룹 대수는 `CheckGates`에 이미 넘어오던 조회 VM 전부(`allVMs`, PASS 포함)로 세고, 그룹 내 스펙 비교만 교정 대상끼리 한다. 에러 문구에 `(PASS/FAIL 무관, 조회된 VM 전부 기준)` 표기.
  - `describeSpec`은 실제로 비교하는 값만 보여주도록 코어/소켓·NUMA를 뺐다(비교하지 않는 값이 에러 문구에 나오면 그게 원인처럼 오해됨).
- **영향 범위**: `fixer/gates.go`(위 3곳), `fixer/gates_test.go`(신규), `vm-param-check/README.md`("게이트" 설명). 전원 OFF 게이트, `BuildPlan`/`fixable` 분류, 체크 로직은 변경 없음. `-fix`의 확인 프롬프트(실제 변경 전 y/N)도 그대로다. 다른 도구 폴더(`vm-param-setting-check`, `integrated-vm-param-check-test-tool`, `VM_setup/vm-param-fix`)의 동일 이름 `gates.go`는 이번에 건드리지 않았다.
- **의도적으로 남긴 것**: NUMA·코어/소켓 외에 vCPU와 HT도 `-fix`가 고치는 항목이라 같은 논리가 적용될 수 있으나, 요청 범위(코어/소켓+NUMA)를 넘지 않도록 비교를 유지했다. 이 둘이 FAIL인 VM이 섞여 게이트에 막히면 같은 방식으로 뺄 수 있다.
- **검증(록키 192.168.0.58, Go 1.26.5, 오프라인)**: `go build`/`go vet`/`go test ./...` 통과. 신규 테스트 11건(새 규칙 5 + 기존 동작 유지 6: 여전히 비교하는 항목 5, 그룹이 하나뿐이면 대수 비교 생략 1)을 **옛 `gates.go`에 돌리면 새 규칙 5개가 정확히 FAIL**로 잡히는 것 확인(2:2 PASS 혼합 구성, 코어/소켓·NUMA만 다른 경우, 전체 기준 2:3 불일치, 대상이 한 그룹뿐인 경우, 에러 문구 등). 기존 동작 유지 6건은 옛 코드에서도 통과한다. 실 vCenter(192.168.0.50)의 `192ev01`(PASS)/`192ev02`(FAIL)에 `-fix`(stdin 비움 → 설정 변경 없음)를 옛/새 바이너리로 돌려 게이트 흐름이 동일하게 동작하고 아무것도 바뀌지 않음을 확인. **한계**: 실 vCenter에는 ev 그룹 VM이 1:1로 2대뿐이라, PASS 혼합 2:2 구성의 옛 버그 재현은 실 인벤토리가 아니라 단위 테스트로 확인했다.

## 2026-08-25 — `-shares-ev01/02/03` 쉼표 다중값 + `normal` 혼합 지원

- **파싱(`main.go`)**: 세 플래그 모두 기존엔 ev01만 "ratio 숫자 하나 또는 normal 하나"를 받고 ev02/ev03는 정수 하나만 받았는데, 이제 `-disk`처럼 쉼표로 여러 값을 나열할 수 있고 ratio 숫자와 `normal`을 섞어도 된다(예: `-shares-ev01=4000,normal`). 새 헬퍼 `parseSharesListFlag`가 파싱을 전담(`parseIntListFlag`와 동일한 패턴).
- **체크(`checker/hardware.go`)**: `SharesExpect`를 `EV01 int / EV01Normal bool / EV02,EV03 *int` 구조에서 `EV01,EV02,EV03 []SharesItem`(각 항목은 `{Ratio int, Normal bool}`)으로 재설계. `checkShares`는 이제 허용값 목록 중 **하나라도 실제값과 맞으면 OK**로 판정한다 — CPU/메모리는 서로 독립적으로 판정하므로 "CPU는 ratio로, 메모리는 normal로" 맞는 경우도 둘 다 OK가 된다. 실제값 표시도 `level=custom (ratio=4000)` / `level=normal`처럼 상태를 명확히 보여주도록 통일.
- **영향 범위**: `main.go`(파싱/헬퍼), `checker/hardware.go`(구조체+판정 로직), `demo.go`/`scaletest.go`(고정 데모/스케일값을 새 `checker.RatioShares()` 헬퍼로 감싸도록만 수정 — 실제 판정 로직 변경 없음), `checker/hardware_test.go`(신규 구조체 반영 + 혼합 목록 판정 테스트 추가). ev01이 그룹 미분류("")에서는 체크되지 않는 기존 동작, ev02/ev03 옵션 미지정 시 스킵되는 기존 동작은 그대로 유지.
- **검증**: `go build`/`go vet`/`go test` 전부 통과. vcsim(포트 54322 임시 인스턴스)에 CPU shares=custom/ratio=4000, Memory shares=level normal로 설정한 VM을 만들어 `-shares-ev01=4000,normal`로 체크 — CPU는 ratio 매칭으로 OK, 메모리는 normal 매칭으로 OK, 두 항목 모두 `기대값=4000 또는 normal`로 정확히 표시됨을 확인.

## 2026-08-23 — NUMA 노드당 코어수(`config.numaInfo.coresPerNumaNode`) 체크가 Auto 모드를 무시하던 버그 수정

- **문제**: `autoCoresPerNumaNode=true`(vCenter UI: "NUMA 노드 - 전원을 켤 때 할당됨")인 VM은 `coresPerNumaNode`에 지난 전원 켜짐 시점의 값이 그대로 남아있을 뿐이라 무시해야 하는데, 체크가 이 값을 그대로 기대값과 비교해서 **우연히 값이 같으면 OK로 잘못 판정**하고 있었다.
- **조회(`vcenter/client.go`)**: `vm.Config.NumaInfo.AutoCoresPerNumaNode`를 함께 조회하도록 추가. `model.VMInfo`에 `NumaAutoCoresPerNode *bool` 필드 신설(governomi 타입 주석 근거를 코드 주석에 명시).
- **체크(`checker/topology.go`)**: `CheckTopology`에서 `NumaAutoCoresPerNode`가 true면 `NumaCoresPerNode` 값 비교 없이 **"설정없음"으로 처리**하도록 분기 추가(실제값은 "자동(전원을 켤 때 할당됨)"으로 표시, Note에 사유 명시).
- **영향 범위**: 이 폴더의 `vm-param-check`뿐 아니라, 레거시 체크 전용 도구 `vm-param-setting-check`의 `checker/topology.go`/`model/types.go`/`vcenter/client.go`에도 동일한 수정을 함께 적용(같은 버그가 양쪽에 존재).
- **검증**: `go build`/`go vet`/`go test` 전부 통과.

## 2026-08-21 — `numa.vcpu.preferHT` 매개변수 체크/자동교정 추가

- **체크(`checker/preferht.go`, 신규 파일)**: `numa.vcpu.preferHT`를 체크하는 `CheckPreferHT` 함수 추가. 모든 VM에 공통 적용되는 단일 항목이라 그룹(ev01/ev02/ev03)별 옵션이 따로 없다.
  - `-preferHT` 플래그(또는 SPEC_DIR 스펙 파일의 `preferHT=` 옵션)로 값이 **실제로 주어졌을 때만** 체크한다. 주어지지 않으면 이 항목은 콘솔/CSV 어디에도 출력되지 않는다(다른 항목처럼 "설정없음"으로 나오지 않음).
  - 값이 주어졌는데 VM의 ExtraConfig에 키가 없는 경우도 "설정없음"이 아니라 **FAIL**로 처리한다(단순 TRUE/FALSE 토글이라 값이 없으면 곧 기대값이 아니라는 뜻).
- **플래그(`main.go`)**: `-preferHT <값>` 플래그 추가, `expectSet.PreferHT` 필드 추가, `evaluateVM()`에서 `CheckPreferHT` 호출 추가, `specSettableFlags`에 `preferHT` 등록(SPEC_DIR 스펙 파일에서 지정 가능).
- **자동교정(`fixer/plan.go`)**: `advancedOrder`에 `numa.vcpu.preferHT` 추가 — 기존 고급설정 교정 로직(`fixable`/`BuildPlan`)을 그대로 재사용하므로 이 한 줄 추가만으로 `-fix` 실행 시 FAIL 항목이 자동교정 계획에 포함되고, 실제 적용까지 이어진다.
- **검증**: vcsim(포트 54321)에 대해 ① `-preferHT` 미지정 시 출력 없음 ② `-preferHT TRUE` 지정 + 값 없음 → FAIL ③ `-fix` dry-run 계획에 `numa.vcpu.preferHT: (설정없음) -> TRUE` 1건만 정확히 잡힘 ④ 적용 후 재검증에서 OK로 전환 — 4가지 시나리오 모두 실제 vCenter API로 확인함.
- **범위**: 이번 변경은 위 파일들(`checker/preferht.go` 신규, `main.go`/`fixer/plan.go` 각 수 줄)에 한정되며, 기존 체크/교정 로직은 전혀 건드리지 않음.
- **참고**: 이 매개변수를 독립적으로 vCenter에 일괄 적용하는 별도 병렬 스크립트는 `legacy-vm-param-fix-external-orchestration/numa_preferht_setting-source/`에 별도로 추가됨(이 도구의 체크/자동교정 로직과는 무관한 독립 스크립트 — 변경 이력은 그 폴더의 README.md 참고).
