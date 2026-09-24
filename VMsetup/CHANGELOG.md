# CHANGELOG

`VM_setup` 아래 도구들에 기능이 추가·수정될 때마다 이 파일에 날짜순(최신이 위)으로 기록합니다.

---

## 2026-09-24 — `setup.sh`에 `OS6_hostname` 추가 (호스트 이름으로 OS6 강제 지정)

- **`setup.sh`**: 커널/배포판 자동 판단으로 OS6 여부를 못 잡는 경우를 위해 `OS6_hostname="host1|host2"`(파이프로 여러 대) 환경변수를 추가했다. 이 목록에 현재 호스트 이름(`hostname`)이 있으면 다른 조건과 무관하게 OS6 취급(`bin_os6/*.gz` 설치).
- **영향 범위**: `setup.sh`, `README.md`.
- **검증**: `.58`(hostname=`test-server`)에서 `OS6_hostname="test-server|other-host"`로 판단 로직이 `OS6=1`을 내는 것 확인.

## 2026-09-24 — 실행할 때마다 셸의 비밀번호 export 지우고 시작

- **`vm_setup.sh`**: 시작하자마자 `unset VC_PASSWORD VC_PASS VCENTER_PASS`. 이전 실행(또는 다른 도구 테스트)에서 다른 값으로 export 해 둔 비밀번호가 셸에 남아 있으면, 암호 파일을 건너뛰고 그 값으로 로그인을 시도해 "Login failure"가 나는 사고가 반복돼서 방지책으로 넣었다. 암호는 이제 항상 암호 파일 → 직접 입력 순으로만 받는다(환경변수로 미리 주입하는 경로는 없앰).
- **영향 범위**: `VMsetup/vm_setup.sh`, `VMsetup/README.md`.

## 2026-09-24 — user 메뉴 고정 목록·직접 입력, vswitch_<user>.txt 위치를 VMsetup 으로 이동

- **user 선택(`vm_setup.sh`)**: `0) list`(각 user BM 목록 미리보기) 대신 `0) 직접 선택`(user 이름을 그 자리에서 입력)으로 바뀌었다. 번호 목록(`1~3`)은 이 폴더의 `*.txt` 를 훑는 대신 자주 쓰는 user(`lsh`/`ljh`/`dhk`)를 고정으로 보여준다. `-u <user>` 로 목록에 없는 user(예: 실패 테스트용 `fail_*`)를 쓰는 것은 그대로 된다.
- **`vswitch_<user>.txt` 위치 변경**: `SPEC_DIR/vswitch_<user>.txt` → `VMsetup/vswitch_<user>.txt`(`vm_setup.sh`, `<user>.txt` 와 같은 폴더). `SPEC_DIR` 은 이제 스펙 폴더(`<CAE폴더명>/`)만 담당한다. `vswitch_pgname.sh` 로 IP→포트그룹명 변환할 때도 같은 폴더의 파일을 가리키면 된다.
- **영향 범위**: `VMsetup/vm_setup.sh`(`VSW_FILE` 경로, `select_user()`), `VMsetup/README.md`, `README.md`, `ARCHITECTURE.md`, `PR_CHECKLIST.md`, `VMsetup/user.txt.example`, `SPEC_DIR/vswitch_user.txt.example` → `VMsetup/vswitch_user.txt.example` (파일 이동).
- **검증**: `.58` 검증 환경(vcsim)에서 `bash vm_setup.sh` → 번호 선택 → `VMsetup/vswitch_<user>.txt` 를 읽어 포트그룹 생성부터 VM 생성·affinity·lpage 까지 정상 완료 확인(495대, 성공 495/실패 0).

## 2026-09-24 — user 메뉴, CAE 번호 변경, 색상, 생성 후 스펙 체크, 비밀번호 암호화, OS6 실행파일

- **user 선택(`vm_setup.sh`)**: `-u` 는 그대로. 없으면 이 폴더의 `<user>.txt` 를 번호로 보여준다(`0) list` = 각 user 의 BM 목록·포트그룹 파일 유무).
- **CAE 번호**: 스펙 매칭은 원래 CAE/LSI 뒤 숫자를 빼고 비교한다(vm-param-check `NormalizeFolderName`) — `SAC-CAE001` 스펙을 `SAC-CAE100` 포트그룹으로 실행해도 같은 스펙(시험으로 확인). 새로 **VM 표 확인 뒤 "CAE 번호를 바꾸시겠습니까? (y/N)"** — y 면 폴더별 새 번호를 받아 이번에 만드는 포트그룹 이름(BM 포트그룹·VM 어댑터 1)에 반영. `# 숫자변경기능` 주석 아래 `ask_cae_number` 한 줄을 주석처리하면 꺼진다.
- **색상**: 머리글(청록)·`[INFO]`(초록)·`[경고]`(노랑)·`[오류]`(빨강)·질문(굵게). 터미널일 때만 켜고 파일/파이프로 돌리면 끈다. `NO_COLOR=1`/`VMSETUP_COLOR=never|always`.
- **BM→스펙 확인 단계 삭제 (동작 변경)**: 표와 y/n 을 없애고 VM 표에 스펙 열을 넣었다("위 스펙·포트그룹 할당이 맞습니까?"). 자동으로 못 정한 BM 만 번호로 고른다. 수동 선택할 VM 이 없으면 VM 표를 한 번만 보여준다.
- **affinity 설정값 출력**: 실행 계획에 스펙별 affinity 파일 내용을 보여준다. 내용이 같으면(이름이 달라도) 한 번만, 쓰는 ev 는 `[ev01~ev99]`, `[ev01,ev03,...]` 로.
- **생성 후 스펙 체크**: 실행이 끝나면 만든 VM 만 실행한 스펙으로 vm-param-check(`-specFolder`, 새 옵션)를 돌려 `[일치]`/`[차이]`와 항목별 개수를 보여준다. 로그·CSV 는 `run_<user>/check_<번호>.*`. 차이가 있어도 중단하지 않고 `-fix` 명령을 안내. ev01 cores/numa 가 없는 스펙은 체크할 수 없어 계획 단계에서 경고하고 건너뛴다.
- **계정·비밀번호**: 기본 계정 `lscsystems@vsphere.local`, `-id <계정>`(기존 `-i` 도 됨). 비밀번호는 `VC_PASSWORD` → `../secret/` 암호 파일 → 입력. `../passwd_update.sh` 로 vCenter/ESXi 비밀번호를 등록·갱신(별도 키 파일 `secret/key`, `openssl enc -aes-256-cbc -md sha256 -salt -a` — OS6 openssl 1.0.1e ↔ OS8 1.1.1k 양방향으로 풀리는 것 확인). 키와 함께 `secret/` 을 복사하면 다른 서버에서도 쓴다.
- **기존 vm-param-check 파일 연동**: SPEC_DIR 은 `-s` → `vm-param-check/SPEC_DIR`(있으면) → `../SPEC_DIR` 순(vm_setting_check_insert.sh 와 같은 순서). affinity 줄이 없는 예전 스펙이면 "지금 ev 별 affinity 를 골라 이 스펙에 추가할까요?" — y 면 골라서 스펙 파일에 추가.
- **OS6**: 실행파일이 없을 때 도구별 setup.sh 대신 V2 의 `setup.sh <이름>` 을 부른다(OS6 면 `bin_os6/` 에서 설치).
- **버그 수정**:
  - `run()` 실패 메시지의 종료코드가 항상 0 으로 찍히던 것(이번 변경 중 생긴 것, 시험에서 발견).
  - `vm_create`: 여유공간 -1 을 "데이터스토어를 못 찾음" 표시로 겸해 써서, 여유공간이 -1 보다 작게 보고되면(vcsim 과할당) **만들 VM 이 없는 재실행도** 모든 호스트가 "데이터스토어 정보를 읽지 못했습니다"로 실패했다 → 찾았는지를 따로 기록. 같은 코드의 `.claude/VM/VM_setup/vm_create-source`, `.claude/VM/vm-setting-go-lang/main_vm_create.go` 에도 적용.
- **영향 범위**: `vm_setup.sh`, `vm_create-source/main.go`, `../setup.sh`, `../build_os6.sh`(새), `../bin_os6/`(새), `../passwd_update.sh`·`../secret_lib.sh`(새), `../.gitignore`, `vm-param-check/main.go`(`-specFolder`), `vm-param-check/vm_setting_check_insert.sh`, 검증 환경(`검증/saccae`, `검증/cmd/vcsimenv` `-username/-password/-noDsHosts`)
- **검증**: `vmsetup_test.sh` 48/48(새 V12~V17: user 메뉴, CAE 번호 무시 매칭·변경·주석처리로 끄기, affinity 출력·생성 후 체크, SPEC_DIR 연동, affinity 추가, `-id`+암호 파일/틀린 비밀번호), `scenarios.sh` 39/39, `go test` 통과. saccae 환경(vcsim): ev99 BM 5대 495대 생성 43초, 실패 케이스 11개(`실패테스트사용법.txt`), 회사 이전 모의(V2 + 예전 vcenter.txt·SPEC_DIR 복원 → affinity 추가 → 생성·체크). CentOS 6.10 컨테이너(bash 4.1.2, openssl 1.0.1e)에서 `setup.sh` 자동 OS6 설치 → 암호 파일 → VM 240대 생성·체크까지 성공(컨테이너라 커널은 4.18 — 실제 2.6.32 커널에서는 돌려보지 못함).

## 2026-09-23 — 호스트당 VM 1~99대 (ev01~ev99)

- **상한 10 → 99 (`vm_create`, `affinity_setting`, `lpage_setting`, `tag_setting`, `vm_setup.sh`, vm-param-check)**: 방식은 그대로이고 개수만 늘었다. `-vmCount`/`-vm_cnt` 1~99, `-ev01Cpu`~`-ev99Share`, `-affinityFile01`~`99`, `-ev01Cores`~`-ev99Numa`. ev01 필수·연속 규칙·값 없는 ev 제외·affinity 파일 필수(같은 파일 공유 가능)도 그대로다.
  - 99가 상한인 이유: VM 이름이 `ev%02d` 두 자리라서. 100번째(`ev100`)는 vm-param-check 그룹 판정에서 `ev10`으로 잘못 잡힌다.
- **도움말 정리**: ev02~ev99 옵션은 번호만 다르므로 `-h`에서 `-ev02~99Cpu`, `-affinityFile02~99`처럼 한 줄씩만 보인다(vm-param-check `-h`도 `-cpu-ev02~99` 등). 실제로 쓰는 옵션 이름은 바뀌지 않았다.
- **`vm_setup.sh` vim 스펙 템플릿**: 빈 틀은 예전처럼 ev10까지만 두고(99개를 다 넣으면 수백 줄), ev11~ev99는 같은 규칙으로 줄을 직접 추가하라는 안내를 넣었다. 저장 시에는 ev99까지 읽는다.
- **검증 (vcsim)**: `scenarios.sh` 39/39 PASS — 새 S6: 호스트 2대 × 99대 = 198대 생성, affinity(ev01~ev99 같은 파일)/lpage ev99 적용, 100은 `vm_create`/`affinity_setting`/`tag_setting` 모두 거부, ev01+ev99만 주면 연속 규칙 오류, `-h` 묶음 표시, vm-param-check `-specExport` `groups=99`. `vmsetup_test.sh` 36/36 PASS — 새 V11: 템플릿 밖 ev11/ev12를 넣은 스펙을 `vm_setup.sh`로 실행해 24대 생성 + ev12 affinity/lpage 적용.
- **실환경(home-test, vCenter 192.168.0.50 / ESXi 192.168.0.59)**: `-vmCount=99` → 기존 `192ev01`/`192ev02`는 건너뛰고 `192ev03`~`192ev99` **97대를 22초**에 생성(rc=0). `affinity_setting -vm_cnt=99`(ev01~ev99 같은 파일) 99대 성공·재조회 일치 **7초**, `lpage_setting` ev03~ev99 97대 **5초**. 97대 모두 cpu/Shares/affinity/1GB 페이지 확인. 기존 두 VM은 이미 가진 affinity 값과 같은 파일을 써서 실행 전후 덤프 차이 없음. `vm-param-check`가 `192ev57`/`192ev99`를 ev57/ev99 그룹으로 판정(`-cpu-ev57=4`는 의도대로 FAIL, ev99 항목은 OK). 매핑 파일을 비워서 어댑터는 없음(`portgroup` 설정없음).

## 2026-09-23 — FQDN/짧은 이름 교차 매칭, 실패 시 종료코드, affinity 자동 계산 삭제, vCenter 번호 선택

- **호스트 이름 매칭(`vm_create`, `vswitch_setting`)**: 파일의 BM 이름과 vCenter 등록 이름이 각각 FQDN이든 짧은 이름이든 찾는다 — 정확히 같은 이름이 있으면 그것, 없으면 첫 `.` 앞부분끼리 비교해서 **하나뿐일 때만** 쓴다(여러 대면 오류). 예전에는 `vm_create`만 "vCenter가 FQDN이고 파일이 짧은 이름"을 찾았고, `vswitch_setting`은 정확히 같은 이름만 찾아서 짧은 이름으로 적으면 **포트그룹만 안 만들어지고 VM은 만들어지는** 어긋남이 있었다.
- **실패하면 종료코드 1 (동작 변경)**:
  - `vm_create`: 호스트를 못 찾음/같은 이름 여러 대/데이터스토어 없음을 `[오류]`로 출력하고(예전에는 **메시지 없이** 건너뜀), 이런 호스트나 VM 생성 실패가 있으면 끝에 요약 후 종료코드 1.
  - `vswitch_setting`: 호스트를 못 찾음/포트그룹 생성 실패가 있으면 종료코드 1.
  - **이미 있는 VM/포트그룹은 정상**(종료코드 0). vcsim은 이미 있는 포트그룹을 `DuplicateName`으로 돌려줘서 `AlreadyExists`와 함께 "이미 존재"로 처리한다.
  - 그래서 `vm_setup.sh`가 실패한 단계에서 멈춘다(예전에는 계속 진행해 `[완료]`까지 출력). 원인을 고치고 다시 실행하면 이미 만든 것은 건너뛴다.
- **affinity 자동 계산 삭제 (동작 변경)**: `affinity_setting`은 `-vm_cnt` 범위의 ev마다 `-affinityFileNN`이 **필수**다. 예전에는 파일이 없으면 `-ht`로 CPU 0번부터 1:1 계산해서 **ev01~ev10이 같은 물리 CPU에 겹쳐 고정**됐다. 파일 내용 `AUTO`도 오류. `-ht`는 예전 명령줄이 깨지지 않도록 받아서 안내 후 무시한다.
  - **여러 ev에 같은 파일 지정 가능**(`-affinityFile01=affinity_ev01.txt -affinityFile02=affinity_ev01.txt ...`, 스펙에서는 `affinity-ev02=affinity_ev01.txt`).
  - `vm_setup.sh`: 스펙에 `affinity-evNN`이 없는 ev가 있으면 그 스펙은 자동 할당하지 않고 이유를 알린다. vim 스펙 입력 후 ev별 affinity는 **1) 직전 ev와 같은 파일(ev02부터, Enter) / 2) 기존 파일 / 3) vim 입력**. 2번에서 같은 스펙 폴더의 파일을 고르면 복사하지 않고 그대로 가리킨다.
  - 예시 스펙(`SPEC_DIR/TST-CAE001-SAMP48c-QRST`)에 `affinity-ev02`와 `affinity_ev02.txt`를 추가했다.
  - vm-param-check의 체크(ev01 affinity 파일이 없을 때 `-ht` 기반 기대값 계산)는 체크 기능이라 그대로 두었다.
- **`vm_setup.sh` vCenter 선택**: `-v`가 없으면 `vcenter.txt`(V2 폴더 → 없으면 vm-param-check 폴더) 목록을 번호로 보여주고 고른다(`0` 직접 입력). user별 마지막 실행 vCenter를 `run_<user>/last_vcenter`에 기억해서 시작할 때 출력하고 목록에 `<- 이전 실행`으로 표시, Enter = 이전 실행. `-n`은 vCenter를 묻지 않는다. `vcenter.txt.example` 추가, V2 루트 `vcenter.txt`는 git 제외.
- **`vswitch_pgname.sh`**: 폴더명을 물을 때 Enter = 직전에 입력한 폴더명.
- **검증(록키, vcsim)**: `검증/scenarios.sh` 32건 PASS — 회귀 비교(수정 전/후 바이너리, ev01~ev03에 **같은 affinity 파일** 지정)에서 VM 12대 덤프·출력 차이 0건. 추가 S5: 파일 없는 ev/`AUTO` 파일 오류 종료, `bm1`→`bm1.example.com`·`DC0_H0.example.com`→`DC0_H0` 교차 매칭(vswitch/vm_create), 없는 호스트·짧은 이름 중복(`dup.a.com`/`dup.b.com`) 오류 + 종료코드 1, 재실행(이미 있음) 종료코드 0. `검증/vmsetup_test.sh` 34건 PASS — 추가: 재실행 정상 완료, 짧은 이름 파일 + FQDN 호스트 전 과정, vCenter 번호 선택/기억/Enter/`-n`/대체 경로, affinity 2번(다른 폴더 복사·같은 폴더 참조), affinity 없는 스펙 경고, 없는 호스트에서 중단. 실환경(home-test)에서는 이번 변경을 돌리지 않았다.

## 2026-09-22 — `vswitch_pgname.sh` 신설 (IP → `-cae-a-b-c-0` 포트그룹명 변환)

- **`VMsetup/vswitch_pgname.sh`(신규)**: `vswitch_${user}.txt`의 2번째 컬럼이 이미 `<폴더명>-cae-a-b-c-d` 형식이면 그대로 두고, IP(`a.b.c.d`)면 폴더명을 입력받아 `<폴더명>-cae-<a>-<b>-<c>-0`으로 바꾼다(**항상 `/24` 가정, 마지막 옥텟은 0 고정**). 둘 다 아니면 경고만 내고 그대로 둔다. 파일을 그 자리에서 바꾸고 원본은 `<파일>.bak`로 남긴다. 인라인 주석이 있던 줄을 변환하면 그 주석은 사라진다(문서화된 동작).
- `vm_setup.sh` 자체 로직은 건드리지 않았다 — 변환은 `vm_setup.sh` 실행 전 별도 단계다.
- **검증**: CAE 형식 줄(변경 없음), 유효 IP 2건(폴더명 입력 → 변환), CAE 형식도 IP도 아닌 줄(경고 후 유지), 빈 줄/주석 줄(유지), 인자 없음(사용법 출력 후 종료코드 2), 폴더명 빈 입력(경고 후 원본 유지) — 전부 기대대로 동작. `bash -n` 통과.

## 2026-09-22 — V2: 루트 `setup.sh` 신설 (전체 오프라인 빌드)

- **`V2/setup.sh`(신규)**: 11개 도구의 `setup.sh`를 한 번에 돌려 빌드하고 성공/실패를 요약한다(`bash setup.sh`, 도구 이름으로 일부만, `-l` 목록, `-c` 정리). `GOPROXY=off`, `GOFLAGS=-mod=vendor`, `GOTOOLCHAIN=local`, `GOSUMDB=off`를 강제해서 모듈·새 Go 툴체인을 내려받지 않는다. Go 없음/버전 낮음(1.26.5 미만)/`govendor` 없음은 빌드 전에 원인을 알리고 멈춘다. 각 도구의 `setup.sh`는 그대로라 도구 하나만 직접 빌드해도 된다.
- **검증(록키, 새로 받은 V2 브랜치)**: `unshare -rn`으로 네트워크를 끊은 상태에서 11개 전부 성공, 도구 지정/`-l`/`-c` 동작, 알 수 없는 도구·Go 없음·Go 1.20·govendor 없음은 원인 메시지와 종료코드 1, 소스를 일부러 깨뜨리면 그 도구만 실패로 요약하고 나머지는 계속 빌드.

## 2026-09-22 — V2: `vm_setup.sh` 신설 (스펙·포트그룹 자동 할당 → y/n → 수동 선택 → vim) + home-test 실환경 검증

- **`vm_setup.sh`(신규)**: `VMsetup/<user>.txt`(BM 목록) + `SPEC_DIR/vswitch_<user>.txt`(BM 포트그룹 VLAN)로 스펙 결정 → 포트그룹 결정 → `vswitch_setting` → 스펙별 `vm_create` → `affinity_setting` → `lpage_setting`을 한 번에 실행한다. 사용법은 README "2. 사용 방법" 참고.
  - **스펙 자동 할당**: 포트그룹 이름 `<폴더명>-cae-a-b-c-d`에서 폴더명을 뽑아 `vm-param-check -specExport`로 SPEC_DIR 스펙을 찾는다(차수만 다른 폴더는 같은 스펙). BM→스펙 표를 보여주고 `(y/n)`. `n`이거나 자동으로 못 정한 BM은 목록에서 번호 선택(Enter=직전 선택), 목록에 없으면 `0`으로 vim.
  - **vim 스펙 입력**: 모든 항목이 `키=""` + 주석 설명. 저장하면 `SPEC_DIR/<folder>/`가 새로 생긴다(`folder`가 비었거나 CAE 규칙 위반이거나 같은 스펙이 이미 있으면 오류 안내 후 다시 편집, 검증은 `-initFolder`/`-specExport`를 그대로 재사용). 이어서 값이 있는 ev마다 **affinity를 자동 / 기존 파일 선택 / vim 입력** 중에서 고른다(vim 템플릿은 vCPU 수만큼 `sched.vcpuN.affinity=""` 줄).
  - **포트그룹(네트워크 어댑터 1) 할당**: BM에 포트그룹이 1개면 그 BM의 모든 VM, 여러 개면 스펙 폴더명과 이름이 맞는 것이 하나일 때 자동. VM→포트그룹 표를 `(y/n)`, 자동으로 못 정한 VM/`n`이면 번호 선택(**`a<번호>` = 그 BM의 VM 전체에 적용**), 목록에 없으면 vim(`hostname=""`, `portgroup=""`). 포트그룹 신규 생성(VLAN 입력)은 지원하지 않는다.
  - 실제 vCenter를 바꾸는 단계 직전에 실행 계획을 보여주고 한 번 더 확인한다. `-n`이면 계획까지만 보이고 종료(vCenter 접속 없음).
  - 스펙 값 → 도구 옵션: `cpu/mem/disk/shares-evNN` → `vm_create`(disk·shares 쉼표 목록은 첫 값), `ht` → `affinity_setting -ht`, `affinity-evNN` → `-affinityFileNN`, `cores`(소켓당)/`numa`(노드당 vCPU) → `lpage_setting` 총 코어(=cpu)/소켓 수/NUMA 노드 수.
  - **`affinity_setting`/`lpage_setting`에는 짧은 이름 목록(`vmbase_<k>.txt`)을 넘긴다**: 두 도구는 worklist 문자열을 그대로 VM 이름 접두어로 쓰고, `vm_create`는 BM 이름의 `.` 앞부분만 쓴다. `esxi-node-001.domain` 같은 BM이면 VM 이름은 `esxi-node-001ev01`인데 두 도구가 `esxi-node-001.domainev01`을 찾아 VM을 못 찾는다(실환경 `192.168.0.59` → `192ev03`에서 발견, vcsim에 도메인 붙은 호스트를 추가해 회귀 시험 추가).
- **`tag_setting`은 포함하지 않았다**: 스펙에 DEPT_NAME/PURPOSE/VM_TYPE 값이 없어서 단독 실행한다.
- **검증 — vcsim(록키)**: `검증/vmsetup_test.sh` 21건 PASS — 자동 할당 실행(4대 생성/포트그룹/affinity/lpage/shares), 스펙 후보 2개 모호 → 수동 선택 + 포트그룹 2개는 스펙 폴더명으로 자동 선택, vim 경로(잘못된 폴더명 → 다시 편집 → 저장, ev01 자동/ev02 vim affinity, 포트그룹 수동/vim), `n` 전체 수동, `a<번호>`, 도메인이 붙은 BM(`bm1.example.com` → `bm1ev01`), `-n`이 vCenter를 바꾸지 않음. vim은 가짜 편집기(`VM_SETUP_EDITOR`)로 템플릿을 채워 시험했고 **실제 vim 화면은 확인하지 않았다**.
- **검증 — home-test 실환경(vCenter 192.168.0.50, ESXi 192.168.0.59)**: 테스트용 데이터센터 `V2TEST-DC`를 하나 더 만들어 데이터센터 2개 상태에서 진행했고, 끝나고 VM·포트그룹·데이터센터를 전부 지워 원래 인벤토리와 같음을 확인했다(기존 `192ev01`/`192ev02`도 테스트 전후 설정 덤프 동일).
  - **수정 전 도구 재현**: `vm_create`는 `데이터센터가 2개 존재하여 자동 선택이 불가합니다`로 종료, `lpage_setting`은 `please specify a datacenter`로 실패.
  - `vswitch_setting`: 같은 BM에 포트그룹 2개(VLAN 3901/3902) 생성.
  - `vm_create`(`-datacenter` 없이, ev01~ev10): 이미 있는 `192ev01`/`192ev02`는 건너뛰고 ev03~ev10 **8대를 3.7초**에 생성, `-mapFile` VM 키로 ev05만 다른 포트그룹.
  - `lpage_setting`: ev03~ev10 8대 성공(기존 두 VM은 대상에서 제외).
  - `nic_assign`: 전원 꺼진 VM은 `conn=false/start=true`(전원 켤 때 연결 체크), **전원을 켠 VM(`192ev04`)에서 포트그룹을 바꿔도 `conn=true/start=true`**, 재실행은 "이미 적용됨".
  - `vm-param-check -specRoot`: VM 폴더가 CAE 규칙이 아니라서 포트그룹 이름으로 스펙 폴더를 유추해 ev03~ev10 스펙(`-cpu-ev10` 등)이 적용되고 핵심 항목이 전부 OK.
  - **실환경에서는 `vm_setup.sh`를 끝까지 돌리지 않았다**: 랩 호스트 이름이 `192.168.0.59`라 VM 이름 접두어가 `192`가 되어, 이미 있는 `192ev01`/`192ev02`에 `affinity_setting`(ev01부터 적용)이 적용되기 때문이다. 대신 `vm_setup.sh -n`으로 실행 계획을 뽑아 그 계획의 도구·옵션을 그대로 실행했다. `vm_setup.sh`의 실행 흐름 자체는 vcsim에서 시험했다.

## 2026-09-22 — V2: 호스트당 VM 1~10대, 데이터센터 여러 개 대응, vswitch 병렬화, nic_assign 신설

V2(`.claude/VM/V2/VMsetup`)는 `.claude/VM/VM_setup` 복사본에서 시작했다. 원본 폴더는 수정하지 않았다(`vm-param-fix`는 V2에서 제외).

- **VM 1~10대(`vm_create`, `affinity_setting`, `lpage_setting`, `tag_setting`)**: ev01~ev03 고정이던 플래그를 반복문으로 ev01~ev10까지 등록한다. 기존 이름(`-ev01Cpu`, `-affinityFile02`, `-ev03Cores` 등)은 그대로다.
  - `vm_create`: `-vmCount` 1~10. **ev01 필수**, ev 번호는 **ev01부터 연속**이어야 한다(예: ev02 없이 ev03 → 에러로 중단). **값(`-evNNCpu`)이 없는 ev는 만들지 않는다** — `-vmCount`가 값이 있는 ev 수보다 크면 경고 후 있는 만큼만 만든다.
    - **동작 변경 1**: 예전 ev03 기본값(1 CPU/1GB/20GB/Share 1000)을 없앴다. `-vmCount=3`에 ev03 값을 안 주면 예전에는 기본값으로 ev03을 만들었지만 이제는 만들지 않는다.
    - **동작 변경 2(완화)**: 예전에는 `-vmCount=1`이어도 `-ev02Cpu`가 없으면 종료했다. 이제는 ev01만 있으면 된다.
  - `affinity_setting`: `-vm_cnt` 1~10, `-affinityFile01`~`-affinityFile10`.
  - `lpage_setting`: `-ev01Cores/Sockets/Numa`~`-ev10...`.
  - `tag_setting`: `-vmCount` 상한 10 검사만 추가(예전에는 상한 없음). 이미 병렬 + vCenter 전체 조회 구조였다.
- **데이터센터 2개 이상 / 폴더 여러 단계**:
  - `vm_create`: 예전에는 데이터센터가 2개 이상이면 `-datacenter` 없이 종료했다. 이제는 데이터센터별 goroutine으로 VM/호스트를 병렬 배치 조회해서, 호스트가 속한 데이터센터의 VM 폴더에 만든다. VM 이름 중복 검사는 모든 데이터센터 대상. 같은 이름의 호스트가 여러 데이터센터에 있으면 그 호스트만 건너뛴다. `-datacenter`를 주면 예전처럼 그 데이터센터로 한정한다.
  - `lpage_setting`: `finder.DefaultDatacenter()`(에러 무시) → vCenter 최상위부터 `ContainerView`로 VM 이름을 1회 조회. 예전에는 다중 DC에서 `please specify a datacenter`로 실패했다(vcsim 재현).
  - `mac_info`, `main_conn`: 데이터센터를 전부 돌며 같은 방식으로 찾는다(데이터센터 1개면 예전과 같은 조회·같은 출력 순서).
  - `affinity_setting`, `numa_preferht_setting`, `tag_setting`, `vswitch_setting`, `license_assign`은 원래 다중 DC에서 동작하는 구조라 이 부분은 변경 없음.
- **`vswitch_setting` 병렬화**: 호스트를 하나씩 순차 처리하던 것을 호스트 단위 워커풀(`-concurrency`, 기본 20)로 바꿨다. 같은 호스트의 포트그룹 여러 개는 호스트 안에서 순서대로 만든다. 출력 문구는 같고, 호스트별로 묶어서 완료순으로 찍는다. **한 BM에 포트그룹을 2개 이상 적는 것은 예전 코드도 지원했다**(수정 전 바이너리로 vcsim에서 2개 생성 확인).
- **`vm_create -mapFile` VM 이름 키**: 매핑 파일에 `bm001ev02 PG-B`처럼 VM 이름으로 적은 줄이 있으면 그 VM만 해당 포트그룹을 쓴다. 없으면 예전처럼 BM 이름 줄을 따른다(한 BM에 포트그룹이 여러 개일 때 VM별로 다르게 붙이기 위함).
- **`nic_assign-source` 신설**: 이미 만들어진 VM의 네트워크 어댑터 1을 할당표(`VM이름 포트그룹`)대로 바꾸고 "연결됨"과 "전원을 켤 때 연결"을 체크한다. `vm-network-migration` Step 3(`nm-connect`)의 `SetPortgroup` → `EnsureConnectState` 방식(반영 후 API로 다시 읽어 확인, 아니면 연결 상태만 재전송, 최대 5회)을 따른다. 전원이 꺼진 VM은 "전원을 켤 때 연결"만 확인한다. VM끼리는 워커풀 병렬, 이미 원하는 상태면 건드리지 않는다(멱등).
- **빌드**: 의존성을 `V2/govendor/`(govmomi 0.55.1-standard, 0.39.0)에 두고 각 `setup.sh`가 `../../govendor/...`를 `vendor`로 링크한다 → **V2 폴더만 받아도 폐쇄망 빌드 가능**. `license_assign-source`는 원래처럼 자체 `vendor/`를 쓴다.
- **영향 범위**: `vm_create-source/main.go`, `affinity_setting-source/main.go`, `lpage_setting-source/main.go`, `tag_setting-source/main.go`, `vswitch_setting-source/main.go`, `mac_info-source/main.go`, `main_conn-source/main.go`, `nic_assign-source/`(신규), 각 `setup.sh`(vendor 링크 경로).
- **검증(록키 192.168.0.58, vcsim)**: 10개 모듈 `go build`/`go vet` 통과, `nic_assign` 단위 테스트 통과. vcsim 시나리오 23건 전부 PASS:
  - **회귀**: 수정 전/후 바이너리를 같은 인벤토리(DC 1개, 호스트 4대)에 돌려 `vm_create`(ev01~ev03 12대)·`affinity_setting`·`lpage_setting` 결과를 VM 설정 덤프(numCPU/메모리/예약/Shares/extraConfig/부트순서/디스크/NIC)로 비교 — **차이 0건**, 출력 로그도 동일.
  - **10대**: 호스트 4대 × 10대 = 40대 생성, ev10 Shares=normal, affinity/lpage ev10까지 적용.
  - **규칙**: ev02 없이 ev03 → 에러, ev03 값 없음 → 2대만 생성+경고, `-vmCount=1`+ev02 없음 → 정상.
  - **데이터센터 3개 + 호스트/VM 폴더 3단계**: vm_create/lpage/affinity/vswitch/mac_info 정상. 같은 환경에서 수정 전 vm_create는 종료, 수정 전 lpage는 실패함을 확인.
  - **포트그룹 2개**: 같은 BM에 PG-A/PG-B 생성 → `-mapFile`에서 BM 키=PG-A, VM 키(ev02)=PG-B로 생성 → `nic_assign`으로 ev01/ev03을 PG-B로 변경(전원 켤 때 연결 체크), 재실행 시 "이미 적용됨".
  - **속도**: 데이터센터 1개·호스트 20대에서 `vm_create` 조회 경로 10회 평균 수정 전 0.0275s / 수정 후 0.0275s. 데이터센터 3개·폴더 3단계·호스트 60대는 0.0534s.
  - **미검증**: 전원이 켜진 VM의 "연결됨" 체크는 vcsim이 런타임 연결을 흉내내지 않아 실환경(home-test)에서 확인 예정.

## 2026-09-02 — `vswitch_setting-source` 코드 교체 (HostGroup 매핑 → vSwitch 포트그룹 생성)

- 메일(`go lang 모음 V2`)의 `main_vs.txt`로 `main.go` 전체를 교체했다. 기존 코드는 폴더명과 달리 클러스터 HostGroup(DRS 그룹)을 매핑하는 도구였는데, 새 코드는 폴더명 그대로 **`worklist.txt`의 (호스트, 포트그룹, VLAN)을 읽어 각 호스트 `vSwitch0`에 포트그룹을 일괄 생성**한다(`HostNetworkSystem.AddPortGroup`).
- 플래그 변경: `-url`/`-cluster`/`-concurrency` → `-vcTargetIP`(필수), `-id`, `-worklistFile`, `-targetVSwitch`. 비밀번호는 환경변수 `VC_PASSWORD`로 받는다.
- 오래된 빌드 산출물 `vswitch_setting`(옛 로직 바이너리)을 삭제했다. 폐쇄망에서 `bash setup.sh`로 재빌드해야 한다.
- `README.md`를 새 동작/플래그에 맞게 다시 작성했다.
- `go.mod`/`go.sum`/`vendor/`는 그대로 두었다. 새 코드가 쓰는 패키지(`object`, `view`, `mo`, `types`)는 모두 기존 `vendor/`에 포함돼 있다.
- **검증**: 이 환경에 Go 툴체인이 없어 빌드/실행 검증은 하지 못했다. 소스 교체와 import 대상이 vendor에 존재하는지만 확인.

## 2026-08-25 — `vm_create-source` 게스트 OS 지정 옵션(`-guestId`) 추가

- **`-guestId` 플래그 신설 (`main.go`)**: 기존에는 `rhel8_64Guest`가 소스에 하드코딩돼 있어 다른 OS로 만들려면 소스를 고쳐야 했다. 이제 `-guestId=rhel9_64Guest`처럼 인자로 지정할 수 있고, **아무것도 주지 않으면 종전과 동일하게 `rhel8_64Guest`가 기본값**으로 쓰인다.
  - 게스트 OS 식별자 목록은 vSphere 버전마다 달라지므로 도구에서 화이트리스트로 막지 않는다(막으면 새 OS가 나올 때마다 소스를 고쳐야 함). 빈 값만 즉시 거르고, 실제 유효성은 vCenter가 `CreateVM` 단계에서 판정한다.
  - 시작 시 `[INFO] 게스트 OS: <값> / 펌웨어: <값>`을 출력해서 어떤 값으로 만들어지는지 바로 확인할 수 있게 했다.
- **VM 생성 실패가 조용히 무시되던 문제 보완 (`main.go`)**: 예전에는 `CreateVM`/Task 실패 시 아무 출력 없이 넘어가서, 예컨대 `-guestId`에 오타가 있으면 `생성 대상 VM 12대`라고 찍은 뒤 아무것도 만들어지지 않은 채 `새로 생성할 VM이 없거나...`로 끝나 원인을 알 수 없었다. 이제 실패 사유를 출력하고(대수가 많을 때 로그가 넘치지 않도록 **처음 5건까지만** 상세 출력), 마지막에 **실패 총 건수**와 `-guestId` 확인 안내를 요약해 준다. 성공 경로의 동작·출력은 이전과 동일하다.
- **검증**: vcsim 기준 ① 옵션 미지정 → `[INFO] 게스트 OS: rhel8_64Guest`, 생성된 VM의 실제 `guestId`도 `rhel8_64Guest` ② `-guestId rhel9_64Guest` → 실제 `guestId`가 `rhel9_64Guest`로 반영됨(독립 호스트/클러스터 호스트 양쪽에서 확인) ③ 빈 값/공백만 준 경우 즉시 종료 — 3가지 모두 확인.
  - 잘못된 식별자에 대한 실패 처리는 vcsim이 `guestId`를 검증하지 않아 시뮬레이터로는 재현되지 않으므로, 소스 사본에 생성 실패를 강제 주입해 별도로 검증함(12건 실패 시 상세 5건 + `실패 12건 (전체 12건 중)` 요약이 정상 출력됨을 확인).

## 2026-08-25 — `vm_create-source` 속도 개선 (동작 변경 없음)

대상 호스트가 많을수록 vCenter 왕복(round-trip) 횟수가 선형으로 늘어나던 구간들을 전부 배치 조회로 바꾸고, VM당 Task 수를 줄였습니다. **생성되는 VM의 최종 설정값은 이전과 완전히 동일합니다.**

- **VM당 Task 2회 → 1회 (`main.go`)**: 예전에는 `CreateVM`으로 만든 뒤 별도 `Reconfigure` Task로 메모리 예약·CPU/메모리 Shares·`sched.mem.*` extraConfig·Secure Boot 해제를 넣었다. 이 값들은 생성 시점에 이미 확정돼 있고 device key에 의존하지 않으므로 **생성 스펙에 함께 담아** Task 1회로 처리하도록 바꿨다.
  - 단, **부팅 순서(BootOrder)만은 생성 이후에 남겨뒀다** — 실제 device key는 VM이 만들어진 뒤에야 확정되므로, 생성 스펙에 임시 음수 key로 넣으면 동작이 달라질 위험이 있어 의도적으로 합치지 않았다.
- **인벤토리 재귀 탐색 제거 (`main.go`)**: 설정 단계에서 VM마다 `finder.VirtualMachine()`으로 인벤토리를 다시 뒤지던 것을, 생성 Task가 돌려주는 MoRef(`task.WaitForResult()`)를 그대로 쓰도록 바꿨다. VM 대수가 많을수록 이 탐색이 급격히 느려지던 구간이 통째로 사라진다.
- **디바이스 목록 배치 조회 (`main.go`)**: 부팅 순서를 정하려고 VM마다 `Properties()`를 호출하던 것을, 생성된 VM 전체에 대해 1회 배치 조회로 바꿨다.
- **데이터스토어 배치 조회 (`main.go`)**: 사전조사 goroutine 안에서 호스트마다 `pc.Retrieve(datastore)`를 부르던 것을, 전체 호스트의 데이터스토어를 중복 제거해 1회 배치 조회하도록 바꿨다. 선택 로직은 그대로 각 호스트 자신의 데이터스토어 안에서만 최대 여유공간을 고르므로 결과는 동일하다.
- **리소스풀 배치 조회 (`main.go`)**: `HostSystem.ResourcePool()`은 내부적으로 `parent` 조회 + `ComputeResource` 조회로 **호스트당 2회** 왕복이 발생한다. 여러 호스트가 같은 클러스터를 공유하므로 부모를 중복 제거한 뒤 `ComputeResource`/`ClusterComputeResource` 타입별로 한 번씩만 조회하도록 바꿨다(govmomi 원본과 동일한 타입 분기).
  - 그 결과 **사전조사 goroutine 안에서는 vCenter 왕복이 아예 발생하지 않는다**(전부 맵 조회).
- **검증**: 변경 전(git HEAD) 바이너리와 변경 후 바이너리를 각각 별도 vcsim 인스턴스에 돌려 결과를 비교함. 독립 호스트(`ComputeResource`)와 클러스터 호스트(`ClusterComputeResource`)를 모두 포함한 4개 호스트 × `-vmCount=3` = **12대 생성**, 커스텀 ratio(`4000`)와 `nomal` 두 Share 모드를 모두 사용.
  - VM별 `numCPU`/`memoryMB`/`firmware`/`guestId`/`memoryReservationLockedToMax`/메모리 예약/CPU·메모리 Shares(level+ratio)/`sched.*` extraConfig/Secure Boot/부트 순서를 덤프해 비교 — **차이 0건**.
  - 디바이스(ParaVirtual SCSI, 디스크 용량, vmxnet3 NIC 포트그룹)도 12대 전부 비교 — **차이 0건**.
  - 재실행 시 이미 존재하는 VM을 건너뛰는 멱등성도 그대로 유지됨을 확인.
- **출력 문구 변경**: 2단계 진행 메시지가 `리소스 설정 대상 VM N대` → `부팅 순서 설정 대상 VM N대`로 바뀌었다(리소스 설정이 생성 단계로 옮겨갔으므로). 그 외 출력은 동일.
