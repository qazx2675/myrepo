# CHANGELOG

`vm-network-migration` 의 변경 사항을 날짜순(최신이 위)으로 기록합니다.

---

## 2026-09-11 — OS6(RHEL/CentOS 6) 관리서버용 빌드 추가

- **`build_os6.sh` 신규.** vendor 한 govmomi v0.55.1 은 Go 1.21+ 표준 라이브러리
  (`slices`, 빌트인 `min`, `reflect.TypeFor`)를 써서, ip_change/ldap_setting 처럼
  go.mod 만 낮추는 걸로는 Go 1.20(RHEL 6 이 뜨는 마지막 툴체인) 빌드가 안 됩니다.
  이 스크립트는 전체 모듈을 임시 사본으로 복사해 vendor 안 딱 3곳만 Go 1.20 호환
  코드로 치환한 뒤(원본 grep 대조 후 치환 — 대상 문구가 없으면 실패) go.mod/
  vendor/modules.txt 의 go 버전을 낮춰 Go 1.20 으로 nm-* 7종을 빌드합니다.
  결과는 `../../bin_os6/` 에 (통합 스크립트 배치 기준).
- `run.sh` 가 `NM_BIN_DIR` 환경변수로 바이너리 디렉터리를 바꿀 수 있게 함
  (`BIN="${NM_BIN_DIR:-$(pwd)/bin}"`). 통합 스크립트가 관리서버 OS6 판정 시
  자동으로 `bin_os6/` 를 넘겨줍니다(`lib/stages.sh` 의 `_nm_bin_dir`).
- 검증: Go 1.20.14 로 `go build ./...` · `go vet ./...` · `go test ./...` 모두
  통과, 실제 7개 바이너리 생성 확인.

---

## 2026-09-09 — 통합 스크립트(Network_Change_Integration_Script) 연동

- `{user}.txt` 를 `VM이름 변경될IP` 2열로 써도 되도록, `LoadVMList` 가 각 줄의
  **첫 번째 필드만** VM 이름으로 읽습니다. (통합 스크립트가 IP 변경 대상 목록과
  VM 목록을 한 파일로 공유하기 위함. VM 이름만 있는 기존 형식도 그대로 동작)
- **`nm-inventory` 추가** (`run.sh --debug-inventory`). vCenter 가 실제로 보고하는
  VM/ESXi 호스트 이름을 그대로 덤프합니다. "상위폴더가 둘 이상인 환경에서 일부
  VM/호스트를 못 찾는다" 는 증상의 원인(이름 표기 불일치 등)을 확인하는 진단용.
  대상 목록 파일을 읽지 않으므로 `-user` 없이 동작합니다.
- ESXi 호스트 이름 매칭을 **FQDN ↔ short name 양쪽**으로 확장. vswitch 파일은
  FQDN(`esxi01.seccae.com`)으로 적지만 vCenter 인벤토리에는 short name(`esxi01`)
  으로 등록돼 있을 수 있어(상위폴더가 둘 이상인 환경에서 관찰됨), `HostByName`
  과 `config.SameHost` 가 첫 마디만 같아도 같은 호스트로 봅니다. short name 이
  서로 다른 두 호스트와 겹치면 그 별칭은 모호함으로 두어 오매칭을 막습니다.
- `TargetForHost` / `LookupVM` 의 "찾을 수 없음" 오류에 **후보 목록·색인 개수**와
  `nm-inventory` 안내를 덧붙여, 이름 불일치를 화면에서 바로 알 수 있게 함.

---

## 2026-09-02 — 재설계 신규 작성 (v1.0.0)

이전 `vm-network-migration`(v0.2.0, 단일 바이너리)을 제거하고, "VM 인프라 네트워크
마이그레이션 및 롤백 자동화 계획서" 에 맞춰 새로 작성했습니다.

### 구조

- **단계별 독립 바이너리 6종**으로 분리: `nm-backup` / `nm-pgcreate` /
  `nm-disconnect` / `nm-connect` / `nm-verify` / `nm-rollback`.
  모든 바이너리가 같은 플래그 이름과 같은 종료 코드 규약(0/1/2)을 씁니다.
- **제어 계층 `run.sh`** 가 단계 순서와 종료 코드 분기, 자동 롤백을 담당합니다.
- 전 과정 병렬 처리 — 세마포어 기반 워커 풀(`internal/pool`)로 동시 실행 개수를
  `-concurrency` 로 제한해 vCenter API 부하를 조절합니다.
- 다중 vCenter 지원. VM 이 어느 vCenter 에 있는지 이름/UUID 색인으로 자동 판별하고,
  vCenter 한 대라도 조회에 실패하면 VM 을 건드리지 않고 중단합니다.
- 자격증명은 `VC_USER` / `VC_PASSWORD` 환경변수로만 받습니다.
- `vendor/` 포함 — 폴더를 통째로 받으면 폐쇄망에서 그대로 빌드됩니다.

### 계획서 대비 의도적으로 다르게 한 부분

- **실행 순서 재배치**: `[백업] → Step 2 생성 → Step 1 해제 → Step 3 연결 → Step 4 검증`.
  계획서의 단계 번호와 롤백 설계(3-Undo → 1-Undo)는 그대로 두되, 포트그룹 생성을
  연결 해제보다 먼저 돌려 **해제~연결 사이의 네트워크 단절 구간을 줄였습니다.**
- **Step 2 를 프로젝트에 내장**했습니다. 계획서는 외부 `vswitch_setting` 호출을
  예로 들었지만, 외부 바이너리에 의존하면 "폴더만 받아서 폐쇄망 빌드" 요건이
  깨지므로 같은 동작을 내장했습니다.
- **`vswitch_{user}.txt` 1번 컬럼은 BM(ESXi 호스트) 이름**입니다. 포트그룹은 호스트
  vSwitch 위에 만들어지므로 생성 단위가 호스트이고, 어떤 VM 이 어느 포트그룹으로
  갈지는 그 VM 이 올라가 있는 호스트(`vm.runtime.host`)로 결정됩니다.
  이관 대상 VM 이 있는 호스트에 항목이 여러 줄이면 대상을 특정할 수 없어
  백업 단계에서 실패 처리합니다.
- **부분 실패 시 나머지 VM 으로 계속 진행**합니다. 계획서의 "실패한 VM 만 선택적
  롤백" 을 따르되, 거기서 전체를 멈추면 이미 Step 1 에 성공해 연결이 끊긴 VM 들이
  신규 포트그룹에 붙지 못한 채 **네트워크가 죽은 상태로 남는** 문제가 있어,
  실패분만 원복·제외하고 남은 VM 으로 이어서 진행하도록 했습니다(`nm-rollback -prune`).

### 안전장치

- 백업이 한 건이라도 실패하면 상태 파일을 아예 쓰지 않고 중단합니다.
  절반만 백업된 상태로 변경을 시작하면 나머지는 롤백할 수 없기 때문입니다.
- 기존 `state_{user}.json` 은 `-force` 없이 덮어쓰지 않습니다. `run.sh` 는 재실행 시
  `--resume`(이어서) / `--force-backup`(새로 백업) 중 무엇을 할지 물어봅니다.
- dry-run 은 별도 임시 상태 파일(`state_{user}.dryrun.json`)을 쓰고 종료 시 지웁니다.
  실제 원본 기록을 오염시키지 않기 위해서입니다. dry-run 실패 시에는 변경한 것이
  없으므로 롤백을 호출하지 않습니다.
- 모든 단계가 멱등입니다. 이미 원하는 상태면 `Reconfigure` Task 를 띄우지 않습니다.
- 롤백은 원본 포트그룹뿐 아니라 `connected` / `startConnected` 까지 복원합니다.
  원래 꺼져 있던 NIC 을 작업 때문에 켜 버리면 이관이 아니라 설정 변경이 됩니다.
- 생성한 포트그룹은 롤백해도 지우지 않습니다. 다른 VM 이 쓰고 있을 수 있고,
  빈 포트그룹이 남는 것은 무해합니다.

### Step 4 검증 강화

vCenter 는 **존재하지 않는 포트그룹 이름도** NIC 백킹의 `DeviceName` 에 그대로
받아줍니다(특히 전원이 꺼진 VM). 실제로 랩 검증 중 없는 포트그룹으로 연결이
"성공" 처리되는 것을 확인했습니다. 그래서 `nm-verify` 는 이름 일치뿐 아니라
**그 포트그룹이 VM 이 올라가 있는 호스트에 실제로 존재하는지**까지 확인합니다.

### 검증

Rocky Linux 8 / go1.26.5 / vCenter 8 / govmomi v0.55.1, 실제 랩(vCenter 192.168.0.50,
ESXi 192.168.0.59, 대상 VM 2대)에서 확인:

- 빈 모듈 캐시 + `GOPROXY=off` 로 폐쇄망 오프라인 빌드 성공 (캐시가 비어 있는 채로 유지됨)
- `gofmt` / `go vet` / `go build` 통과
- 백업 → 생성 → 해제 → 연결 → 검증 전 과정 성공, `run.sh` 통합 실행 성공
- 재실행 시 전 단계 멱등(스킵) 동작 확인
- 전체 롤백 / 특정 VM 선택 롤백 후, 포트그룹·`connected`·`startConnected` 가 백업값과
  정확히 일치함을 별도 조회 프로그램으로 독립 확인
- 부분 실패 주입 시 실패분만 원복 → 상태 파일에서 제외 → 남은 VM 으로 계속 진행,
  종료 코드 1 과 경고 출력 확인
- 롤백까지 실패하는 경우 종료 코드 3 + 수동 확인 안내 확인
- 자격증명 / `-user` 누락 시 vCenter 접속 전 종료 코드 2 로 중단 확인
- dry-run 이 실제 상태 파일을 만들지 않고 임시 파일도 남기지 않음을 확인
- 검증 후 랩은 원래 상태로 복구했고, 테스트용 포트그룹도 삭제했습니다.

## 2026-09-02 — 미사용 플래그 제거 (동작 변경 없음)

쓰이지 않는 옵션을 걷어냈습니다. **기본 동작은 이전과 완전히 동일합니다.**

- `-vm-file` / `-worklist-file` / `-failed-file` 제거. 세 경로 모두 `-user` 에서
  파생되고 `run.sh` 도 재정의한 적이 없어, 열어 둘 이유가 없는 설정이었습니다.
  `-state-file` 만 남겼습니다 — dry-run 이 실제 상태 파일을 건드리지 않도록
  `run.sh` 가 임시 경로로 돌려야 하기 때문입니다.
- `-timeout` 제거. 값을 바꿔 쓴 적이 없어 `internal/cli` 의 30분 상수로 고정했습니다.
- `-version` 및 `cli.Version` 상수 제거. 버전은 CHANGELOG 와 git 태그로 관리합니다.

공통 플래그가 11개에서 6개(`-user`, `-vcenter-file`, `-state-file`, `-nic-index`,
`-concurrency`, `-dry-run`)로 줄었습니다.

**검증**: gofmt / `go vet` / `go test` 통과, 폐쇄망 빌드 성공. 실 랩에서 dry-run →
전체 실행 → 롤백까지 이전과 동일하게 동작하고, 롤백 후 원본 상태로 복구됨을 확인.

## 2026-09-02 — vCenter 계정 지정 방식을 `-id` 플래그로 변경

VM_setup 아래 다른 도구들(`vswitch_setting-source` 등)과 계정 지정 관례를 맞췄습니다.

- **`-id` 플래그 신설** (`run.sh` 는 `--id`). 기본값은 **`lscsystems@vsphere.local`**
  이고, 다른 계정을 쓰려면 `-id=administrator@vsphere.local` 처럼 지정합니다.
- **`VC_USER` / `VCENTER_USER` 환경변수는 더 이상 쓰지 않습니다.** 계정 ID 가 플래그로
  넘어오므로 환경변수와 플래그 중 무엇이 이기는지 헷갈릴 여지를 없앴습니다.
  기존에 `VC_USER` 로 계정을 넘기던 절차가 있다면 `-id` 로 바꿔야 합니다.
- **비밀번호는 그대로 환경변수로만** 받습니다(`VC_PASSWORD`, 대체 `VC_PASS` /
  `VCENTER_PASS`). 비밀번호를 명령행에 적으면 셸 히스토리와 `ps` 에 남기 때문에
  플래그로 열지 않았습니다.
- `config.Credentials()` → `config.Password()` 로 정리, `-id` 가 빈 값이면 종료 코드 2.
- `run.sh` 배너에 접속 계정을 표시해 어떤 계정으로 도는지 바로 보이게 했습니다.

**검증**: gofmt / `go vet` / `go test` 통과, 폐쇄망 빌드 성공. 실 랩에서
① `-id` 미지정 시 `lscsystems@vsphere.local` 로 접속을 시도해 권한 오류로 중단
(기본값이 실제로 쓰이는지 확인) ② `-id=administrator@vsphere.local` 지정 시 백업 →
전체 실행 → 롤백까지 정상 동작 ③ `VC_PASSWORD` 누락 시 종료 코드 2 ④ `-id=""` 거부 —
네 가지 모두 확인. 검증 후 랩은 원상복구했습니다.

## 2026-09-02 — Go 바이너리 출력에 색상 추가 (동작 변경 없음)

`nm-*` 바이너리 실행 결과를 읽기 쉽게 ANSI 색을 입혔습니다. 이모지는 쓰지 않았습니다.
`run.sh` 는 건드리지 않았습니다(사용자가 별도로 손댄 상태).

- `internal/color` 패키지 신설. 성공=녹색, 스킵=노랑, 실패=굵은 빨강, 예정(dry-run)=
  청록, `[Step N]`/`[롤백]` 제목=굵은 청록, `[INFO]`=청록, `[경고]`/`[중단]`/`오류:`=
  빨강 계열로 통일했습니다.
- 표준 출력이 터미널이 아니면(파이프·파일 리다이렉트) 자동으로 색을 끕니다.
  `NO_COLOR` 환경변수로도 강제로 끌 수 있습니다.
- 종료 코드, 메시지 문구, 로직은 전혀 바뀌지 않았습니다 — 순수 출력 서식만 추가.

**검증**: gofmt / `go vet` / `go test` 통과, 폐쇄망 빌드 성공. 실 랩에서 `script` 로
의사 터미널(pty)을 만들어 실제 ANSI 이스케이스(`\x1b[32m` 등)가 성공/스킵/실패/
Step 제목에 정확히 찍히는지, `NO_COLOR=1` 로는 꺼지는지 확인. 파이프로 연결한
일반 실행에서는 색이 나오지 않는 것도 확인. 백업→생성→해제→연결→검증→롤백 전
과정을 색상 적용 버전으로 다시 돌려 이전과 동일하게 동작함을 확인. 랩은
원상복구하고 테스트 포트그룹도 삭제했습니다.

## 2026-09-03 — 폐쇄망 로컬 확장판(`portgroup_change.sh`) 존재를 문서에 기록

이 저장소의 `run.sh` 자체는 바꾸지 않았습니다(사용자가 별도로 관리하는 파일이라
손대지 말라는 지침에 따름). 폐쇄망 배포 환경에서 `run.sh` 를 `portgroup_change.sh`
로 이름을 바꾸고 아래 두 편의 기능을 로컬에 추가해 쓴다는 것을 README §2.8 에
기록만 해 두었습니다.

- 이름 변경 외 실행 방식(대화형 사용자 선택, `{user}.txt`/`vswitch_${user}.txt`
  참조, 단계 순서, 롤백 등)은 `run.sh` 와 동일
- **VLAN 자동 0 처리**: BM(ESXi 호스트)과 VM 이 같은 네트워크 대역이면 VLAN 을
  자동으로 `0` 으로 설정
- **입력 형식 자동 변환**: `<BM호스트명> <VM_IP> <VLAN>` 으로 입력해도
  `vswitch_${user}.txt` 가 요구하는 `<BM호스트명> <포트그룹명> <VLAN>` 형식으로
  자동 변환

두 기능의 정확한 판정/생성 규칙(대역 비교 기준, 포트그룹명 생성 규칙)은 폐쇄망
환경에만 있는 로컬 코드이고 이 저장소로 가져올 수 없어 세부 사항은 기록하지
않았습니다. 이 저장소의 `run.sh`/`vswitch_{user}.txt` 형식(3컬럼: BM호스트/
포트그룹명/VLAN)은 이전과 동일합니다 — 동작 변경 없음, 문서만 갱신.

## 2026-09-09 — BOM 이 파일 첫 줄을 깨는 버그 수정

`{user}.txt` / `vswitch_{user}.txt` / `vcenter.txt` 를 메모장 등에서 "UTF-8"로
저장하면 파일 맨 앞에 BOM(U+FEFF, 3바이트 `EF BB BF`)이 붙는데, 이를 제거하지
않고 있었습니다. 그 결과 **파일 첫 줄의 첫 항목에만** `\ufeffhostname` 처럼 보이지
않는 문자가 붙어 조회에 실패했습니다 — 실제 증상: `{user}.txt` 1번째 줄의 VM 은
"VM 을 찾을 수 없습니다", `vswitch_{user}.txt` 1번째 줄의 BM 호스트는 "항목이
worklist 에 없습니다"로 각각 실패. 파일 내용을 정상으로 봐도(눈에 안 보이는 문자라)
원인을 알기 어려웠던 문제입니다.

- `internal/config.LoadLines` 에서 매 줄 `strings.TrimSpace` 전에
  `strings.TrimPrefix(line, "\ufeff")` 를 추가했습니다. BOM 은 파일 맨 앞에만
  올 수 있으므로 사실상 첫 줄에만 영향을 주지만, 매 줄에 걸어도 나머지 줄에는
  아무 영향이 없어 별도의 "첫 줄 판별" 로직 없이 간단하게 처리했습니다.
- `vcenter.txt` / `{user}.txt` / `vswitch_{user}.txt` 모두 `LoadLines` 를 거치므로
  세 파일 전부 한 번에 고쳐집니다.

**검증**: 회귀 테스트 `TestLoadLinesStripsBOM` 추가. gofmt / `go vet` / `go test`
통과. 실 랩에서 BOM + CRLF 를 실제로 붙인 `{user}.txt`/`vswitch_{user}.txt` 로
백업 → 생성 → 해제 → 연결 → 검증 → 롤백 전 과정이 정상 동작함을 확인(수정 전
이었다면 1번째 줄 VM/호스트에서 각각 실패했을 조건). 랩은 원상복구, 테스트
포트그룹도 삭제했습니다.

## 2026-09-09 — vCenter 접속 idle 커넥션 수를 -concurrency 에 맞춤

`-concurrency` 를 올려도 실제 처리량이 기대만큼 늘지 않는 문제를 점검하다가
발견했습니다. Go 의 `http.Transport` 는 호스트당 idle(재사용 가능) 커넥션을
기본 2개만 유지합니다. `-concurrency` 로 같은 vCenter 에 여러 요청을 동시에
보내도 idle 커넥션이 2개뿐이면 나머지는 매번 TLS 핸드셰이크를 새로 하게 되어
병렬화 효과가 줄어듭니다.

- `internal/vsphere.Connect` 에 `concurrency` 인자를 추가하고, 접속 직후
  `c.Client.DefaultTransport().MaxIdleConnsPerHost = concurrency` 로
  설정했습니다. govmomi 벤더 코드는 건드리지 않았습니다(`DefaultTransport()` 가
  이미 export 되어 있어 우리 쪽에서 값만 지정하면 됩니다).
- `ConnectFleet` 의 호출부만 갱신했고, 그 외 동작/플래그 변경은 없습니다.

**검증**: gofmt / `go vet` / `go test` 통과, 6개 바이너리 오프라인 벤더
빌드(`GOFLAGS=-mod=vendor GOPROXY=off`) 성공(Rocky Linux 빌드 호스트).
