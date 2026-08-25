# AWX 조작 툴 (awxkit)

이 도구는 Ansible AWX 서버를 조작하고 상태를 점검하기 위해 작성된 Go 언어 기반의 도구입니다. 폐쇄망 환경에서도 `vendor/` 폴더를 통해 독립적으로 빌드 및 실행이 가능하도록 구성되어 있습니다.

단일 바이너리가 아니라 **단계별로 독립된 바이너리**(`awxkit-doctor`, `awxkit-nodeinfo` 등)로 나뉘어 있고, 각 바이너리는 같은 이름의 bash 스크립트(`doctor.sh`, `nodeinfo.sh` 등)로 감싸져 있어 그 스크립트만 실행하면 됩니다.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

### 1.1 저장소 가져오기 및 압축
인터넷이 연결된 환경에서 코드를 다운로드한 뒤, 의존성이 포함된 해당 폴더 전체를 폐쇄망으로 이동시킵니다.
```bash
# 인터넷이 되는 환경에서 저장소 다운로드 후 압축
tar czf awxkit.tar.gz ".claude/HPC/awxkit"
```

### 1.2 폐쇄망 환경에서 빌드
폐쇄망 서버로 압축 파일을 옮긴 후, 아래 명령어로 압축을 풀고 빌드합니다.
```bash
# 압축 해제 및 폴더 이동
tar xzf awxkit.tar.gz
cd ".claude/HPC/awxkit"

# 빌드 스크립트 실행 (내부적으로 GOFLAGS=-mod=vendor 사용)
bash setup.sh
```
성공적으로 완료되면 단계별 바이너리(`awxkit-doctor`, `awxkit-ls`, `awxkit-survey`, `awxkit-nodeinfo`, `awxkit-invsync`, `awxkit-dhcp`, `awxkit-pxe`) 7개가 한 번에 생성됩니다.

### 1.3 전역 명령어로 사용하기 (선택 사항)
빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 이동하면 어디서든 명령어처럼 사용할 수 있습니다.
```bash
cp awxkit-* /usr/local/bin/
# 이후 어디서든 awxkit-doctor, awxkit-nodeinfo 등으로 실행 가능
```

### 1.4 매번 직접 빌드하지 않고 바로 실행하기 (`*.sh`)
바이너리 존재 여부를 신경 쓰지 않고 곧바로 쓰고 싶다면 각 단계의 bash 스크립트를 실행하면 됩니다.
해당 바이너리가 없으면 자동으로 `setup.sh`를 실행해 7개를 전부 빌드한 뒤, 넘긴 인자를 그대로 바이너리에 전달합니다.
```bash
bash doctor.sh
bash nodeinfo.sh
```

## 2. 사용 방법

### 2.1 설정 파일 준비
`conf/sample_setting.conf`를 복사해 `conf/${user}_setting.conf`로 만들고 값을 채웁니다.
`${user}`는 아래 순서로 결정됩니다.

1. `config.CurrentUser()` (소스에서 현장 로직을 직접 구현하는 함수, 기본은 빈 문자열)
2. `-user <이름>` 플래그
3. `AWXKIT_USER` 환경변수
4. `$USER` (Linux) / `%USERNAME%` (Windows)

```bash
cp conf/sample_setting.conf conf/hong_setting.conf
vi conf/hong_setting.conf   # awx_url / username / password 등 채우기
chmod 600 conf/hong_setting.conf   # 평문 비밀번호가 들어있으므로 권장
```

### 2.2 설정 파일 탐색 순서
`-conf <경로>`를 지정하지 않으면 아래 순서로 `${user}_setting.conf`를 찾습니다.

1. `./conf/${user}_setting.conf`
2. `~/.awxkit/${user}_setting.conf`
3. `<바이너리 위치>/conf/${user}_setting.conf`

### 2.3 doctor로 최초 점검
설정을 채운 뒤 가장 먼저 `doctor.sh`를 실행해 연결·권한·파라미터 설정을 확인합니다.
```
$ bash doctor.sh
[i] 설정 파일: conf/hong_setting.conf
[✔] AWX 연결 성공 (버전: 23.0.0)
[✔] 조회 가능한 템플릿 12개
[✔] S1 (NodeInfo): 템플릿(nodeinfo, ID=7) 실행 가능
[!] S3 (DHCP): 템플릿(dhcp-register)의 'ask_variables_on_launch'가 꺼져 있습니다. extra_vars를 보내도 조용히 무시됩니다.
...
점검 완료.
```
`[X]`가 있으면 그 항목을 해결하기 전까지 이후 단계가 정상 동작하지 않습니다.

```bash
# -conf / -user 로 직접 지정하고 싶을 때
AWXKIT_USER=hong bash doctor.sh
bash doctor.sh -conf /path/to/other.conf -user hong
```

### 2.4 템플릿 탐색 (`ls.sh` / `survey.sh`)
현장 정보 수집 양식을 손으로 채우기 전에, 실제 AWX에서 템플릿과 변수명을 먼저 조회할 수 있습니다.
```bash
bash ls.sh
# 총 12개 템플릿
#   ID: 7     | nodeinfo                       | extra_vars 허용    | -
#   ID: 24    | pxe-register                   | extra_vars 허용    | survey 있음

bash survey.sh pxe-register     # 또는 survey.sh 24
# [✔] pxe-register (ID: 24) — survey 4개 문항
#   1) 인프라        var=pxe_infra       (필수)
#      선택지: [seoul | daejeon | busan]
#   ...
# 위 var= 값을 conf의 관련 key(s3_infra_key, s4_osver_key 등)에 그대로 사용하세요.
```
`survey.sh`에 인자를 주지 않으면 템플릿 ID/이름을 물어봅니다.

### 2.5 [S1] NodeInfo 실행 (`nodeinfo.sh`)
NodeInfo 템플릿은 hostname을 텍스트로 한 번에 받아 처리하는 구조입니다. hostname마다 따로 실행하지 않고,
**`${user}.txt`에 나열된 hostname 전체를 줄바꿈으로 이어붙여 템플릿을 한 번만 실행**하고, 결과도 파일 하나로 받습니다.
탐색 순서는 conf 파일과 동일합니다: `./conf/${user}.txt` → `~/.awxkit/${user}.txt` → `<바이너리 위치>/conf/${user}.txt`.

```bash
# conf/hong.txt
web01
web02
db01
```
```bash
bash nodeinfo.sh -os 8.10
# [i] 호스트 목록: conf/hong.txt (3개)
# [i] nodeinfo 실행 중... (3개 hostname을 한 번에 전달)
#     [job 1234] running
#     [job 1234] successful
# [✔] 다운로드 완료 (job 1234) — 결과 저장: output/hong_nodeinfo.yaml
# [?] 다른 터미널에서 양식 변환 스크립트를 실행해 위 파일을 변환하세요.
# 변환이 완료되었으면 Y를 입력하세요 (Y/N): y
# [✔] 양식 변환 확인 완료.
```
- `s1_hostname_key`에 hostname 전체(줄바꿈으로 이어붙인 텍스트)를 담아 `s1_template`을 한 번 실행하고, 결과를 `s1_output_dir/${user}_nodeinfo.yaml` 하나로 저장합니다.
- OS 버전은 `-os` 플래그로 받습니다(예: `-os 8.10`). `s1_osver_key`가 설정된 경우에만 사용되며, 그 변수명으로 launch 시 전달됩니다. `s1_osver_choices`를 채우면 목록에 없는 값은 오류, 비우면 검증 없이 그대로 사용합니다.
- 템플릿에 `s1_hostname_key`/`s1_osver_key` 외의 필수 survey 항목이 더 있다면(`variables_needed_to_start` 오류), `s1_extra_vars`에 `"key=value, key2=value2"` 형태로 채우면 launch 시 함께 전달됩니다. `survey.sh`로 조회한 변수명=값을 그대로 넣으면 됩니다.
- 결과 취득 방식(`s1_fetch`)에 따라: `artifacts`(기본, `s1_artifact_key`로 값을 지정하지 않으면 전체 artifacts를 저장), `stdout`(표준출력 그대로 저장), `remote`(API로 받을 수 없어 원격 경로 안내만 출력).
- **양식 변환 확인**: 다운로드된 결과 파일을 정해진 양식으로 바꾸는 스크립트는 awxkit이 실행하지 않습니다. 사용자가 다른 터미널에서 그 스크립트를 직접 실행한 뒤, awxkit이 물어보는 `Y/N`에 `Y`로 답해야 실행이 완료됩니다. `N`이거나 다른 입력이면 `downloaded_unconfirmed` 상태로 종료 코드 1을 반환합니다 — 다운로드는 됐지만 변환이 확인되지 않았다는 뜻입니다.
- 실패하면 stdout 마지막 30줄을 보여주고 종료 코드 1로 끝납니다.
- 모든 실행은 `history_file`에 한 줄씩 기록됩니다 (`status=successful` 또는 `status=downloaded_unconfirmed` 등).
- `${user}.txt` 대신 다른 파일을 쓰려면 `-hosts <경로>`로 직접 지정할 수 있습니다.
```bash
bash nodeinfo.sh -hosts ./retry_list.txt
```

### 2.6 [S2] 인벤토리 동기화 (`invsync.sh`)
nodeinfo 이후 별도 스크립트가 폐쇄망 git에 이미 업로드해 둔 yaml 파일명을 `-file`로 받아,
(1) 인벤토리 소스의 `s2_source_field`(기본 `source_path`)에 그 파일명을 저장 → (2) 소스 동기화 →
(3) `s2_inventory`(대상 인벤토리 ID)가 설정되어 있으면 등록된 호스트 전체를 나열합니다.
```bash
bash invsync.sh -file hong_nodeinfo.yaml
# [i] 소스가 2개 있어 첫 번째(main-source, ID=5)를 사용합니다.
# [i] 인벤토리 소스(5)의 source_path를 "hong_nodeinfo.yaml"로 저장 중...
# [i] 인벤토리 소스(5) 동기화 시작...
#     [inventory_update 400] running
#     [inventory_update 400] successful
# [✔] 동기화 완료 (inventory_update 400)
# [✔] 인벤토리(3)에 총 3대 등록됨
#   - web01 (enabled)
#   - web02 (enabled)
#   - db01 (disabled)
```
- `-file`은 필수입니다. yaml 파일 자체를 git에 올리는 건 이 스크립트가 하지 않고, 이미 올라가 있다는 전제로 파일명만 넘깁니다.
- 사용할 소스는 `s2_inventory_source`가 채워져 있으면 그 ID를 고정으로 쓰고, 비어 있으면 `s2_inventory` 아래 소스 목록 중 첫 번째(ID 오름차순)를 자동 선택합니다.
- `s2_source_field`가 실제 AWX 필드명과 다르면(기본 추정값은 `source_path`) 저장이 무의미해질 수 있으니, 최초 1회는 AWX 웹 UI에서 값이 실제로 바뀌었는지 확인하는 걸 권장합니다.
- `s2_inventory`를 비워두면 동기화만 하고 호스트 목록 조회는 건너뜁니다. 필드 저장 실패, 동기화가 `successful`이 아닌 경우 등은 종료 코드 1로 끝납니다.

### 2.7 [S3] DHCP 등록 (`dhcp.sh`)
인프라를 선택해 `s3_template`을 실행하고, 최종 상태를 즉시 보여줍니다. 설정 변경 작업이므로 완료 후 검증 권고 문구가 항상 함께 출력됩니다.
```bash
# 번호로 지정 (s3_infra_choices의 순서 기준)
bash dhcp.sh -infra 1

# 값으로 직접 지정
bash dhcp.sh -infra daejeon

# 생략하면 s3_infra_choices가 있을 때 번호 선택 메뉴, 없으면 자유 입력을 받음
bash dhcp.sh
# 인프라 선택:
#   1) seoul
#   2) daejeon
#   3) busan
# 번호 선택: 2
# [i] dhcp-register 실행 중... (infra_type=daejeon)
#     [job 501] successful
# [✔] 성공 (job 501)
# [!] 설정 변경 작업입니다. 완료 후 랜덤하게 서버 몇 대를 직접 확인해 실제로 변경되었는지 검증하세요.
```
`s3_infra_choices`가 설정된 상태에서 목록에 없는 값을 주면 오류로 종료합니다. 실패하면 stdout 마지막 30줄을 보여주고 종료 코드 1로 끝납니다.
템플릿에 `-infra` 외 다른 필수 survey 항목이 있다면 `s3_extra_vars`에 `"key=value, key2=value2"` 형태로 채우면 launch 시 함께 전달됩니다 ([S1]의 `s1_extra_vars`와 동일한 방식).

### 2.8 [S4] PXE 등록 (`pxe.sh`)
인프라·OS 버전·Boot Mode·Splunk 설치 여부 4개 옵션을 조합해 `s4_template`을 실행하고, 완료 후 `s4_inventory`의 전체 호스트 수를 리포트합니다. 각 옵션은 `dhcp.sh`의 `-infra`와 같은 방식(번호/값/생략)으로 동작합니다.
```bash
bash pxe.sh -infra 1 -os rocky-9.2 -boot uefi -splunk On-premise
# [i] pxe-register 실행 중... (pxe_infra=seoul, os_version=rocky-9.2, boot_mode=uefi, install_splunk=On-premise)
#     [job 600] successful
# [✔] 성공 (job 600)
# 총 42대의 호스트가 등록 완료되었습니다.
```
`-boot`는 `legacy` 또는 `uefi`, `-splunk`는 `On-premise` / `Cloud` / `no` 중 하나를 받습니다 (`s4_bootmode_choices`/`s4_splunk_choices`로 선택지 강제 가능).
각 옵션의 선택지(`s4_infra_choices` 등)는 선택 사항 — 비워두면 해당 옵션은 자유 입력을 받습니다. `s4_inventory`를 비워두면 호스트 수 집계는 건너뜁니다. 실패하면 stdout 마지막 30줄을 보여주고 종료 코드 1로 끝납니다.
템플릿에 위 4개 옵션 외 다른 필수 survey 항목이 있다면 `s4_extra_vars`에 `"key=value, key2=value2"` 형태로 채우면 launch 시 함께 전달됩니다 ([S1]의 `s1_extra_vars`와 동일한 방식).

## 3. 옵션별 상세 설명

### 3.1 스크립트별 전용 플래그
모든 스크립트가 `-conf <경로>` / `-user <이름>`을 공용으로 받습니다.

| 스크립트 | 전용 플래그 |
|---|---|
| `doctor.sh` | (없음) |
| `ls.sh` | (없음) |
| `survey.sh` | `<ID\|이름>` (위치 인자, 생략 시 대화형 입력) |
| `nodeinfo.sh` | `-hosts <경로>` — `${user}.txt` 대신 사용할 호스트 목록 파일 / `-os <값>` — OS 버전(`s1_osver_key` 설정 시) |
| `invsync.sh` | `-file <파일명>` (필수) — git에 이미 업로드된 인벤토리 yaml 파일명 |
| `dhcp.sh` | `-infra <번호\|값>` — 인프라 선택지 번호 또는 값 |
| `pxe.sh` | `-infra` / `-os` / `-boot` / `-splunk` <번호\|값> |

### 3.2 단계별 바이너리·스크립트
| 스크립트 | 바이너리 | 설명 |
|---|---|---|
| `doctor.sh` | `awxkit-doctor` | conf 로딩, 파일 권한, AWX 연결/버전, 템플릿 존재·실행 권한·`ask_variables_on_launch` 점검 |
| `ls.sh` | `awxkit-ls` | 템플릿 목록(ID·이름·extra_vars 허용 여부·survey 유무) 조회 |
| `survey.sh` | `awxkit-survey` | 템플릿의 survey 문항(질문명·변수명·선택지·기본값·필수여부) 조회 |
| `nodeinfo.sh` | `awxkit-nodeinfo` | [S1] `${user}.txt`의 hostname 전체를 한 번에 넣어 NodeInfo 템플릿 실행 및 결과 파일 저장 |
| `invsync.sh` | `awxkit-invsync` | [S2] `-file`로 받은 yaml 파일명을 소스의 `s2_source_field`에 저장 → 동기화 트리거·완료 대기 → `s2_inventory`의 등록 호스트 목록 조회 |
| `dhcp.sh` | `awxkit-dhcp` | [S3] 인프라 선택 후 DHCP 등록 템플릿 실행, 최종 상태 즉시 출력, 설정 변경 검증 권고 문구 출력 |
| `pxe.sh` | `awxkit-pxe` | [S4] 인프라·OS 버전·Boot Mode·Splunk 설치 여부 조합 실행 후 등록 완료 호스트 수 리포트 |

### 3.3 설정 파일(`${user}_setting.conf`) 키
`key = value` 형식, `#` 이후는 주석. 전체 항목은 [`conf/sample_setting.conf`](./conf/sample_setting.conf) 참고.

| 키 | 설명 |
|---|---|
| `awx_url` / `username` / `password` | AWX 접속 정보 |
| `insecure_tls` | 사설 인증서 환경에서 `true` |
| `s1_*` | [S1] NodeInfo 템플릿/파라미터(`s1_osver_key`+`-os` 플래그로 OS 버전, `s1_extra_vars`로 그 외 필수 survey 항목 전달)/결과 취득 방식(`s1_fetch`: artifacts\|stdout\|remote, `s1_artifact_key`)/저장 경로(`s1_output_dir`) |
| `s2_*` | [S2] 소스 ID(`s2_inventory_source`, 비우면 `s2_inventory` 밑에서 자동 탐색)·대상 인벤토리 ID(`s2_inventory`)·yaml 파일명 저장 필드(`s2_source_field`, 기본 `source_path`) |
| `s3_*` | [S3] DHCP 템플릿/인프라 선택 변수명·옵션(`s3_extra_vars`로 추가 필수 survey 항목 전달) |
| `s4_*` | [S4] PXE 템플릿/인프라·OS·Boot Mode·Splunk 변수명과 각각의 선택지(`*_choices`, 선택 사항), 결과 집계용 인벤토리 ID, 추가 필수 survey 항목(`s4_extra_vars`) |
| `poll_interval` | Job 상태 폴링 간격(초) |
| `history_file` | 실행 이력 기록 파일 |

## 4. 문서별 고유 설명
- 상세 설계·API 매핑·단계별 계획: [`PLAN.md`](./PLAN.md)
- 작업 이력: [`WORKLOG.md`](./WORKLOG.md)
- 4대 시나리오(NodeInfo/인벤토리 동기화/DHCP/PXE)를 포함한 6단계까지 전부 구현 완료. 단계별 독립 바이너리 구조로 전환됨. 남은 것은 7단계(폐쇄망 패키징 정비)뿐입니다.

### 4.1 디렉토리 구조

```
awxkit/
├── README.md              # 이 문서
├── PLAN.md                 # 설계·API 매핑·단계별 계획
├── WORKLOG.md               # 작업 이력
├── go.mod                    # Go 모듈 정의 파일 (module awxkit)
├── .gitattributes             # *.sh/*.go/go.mod를 LF로 강제 (Windows 체크아웃 시 CRLF로 깨지는 것 방지)
├── setup.sh                   # cmd/ 아래 7개 하위 명령을 각각 awxkit-<이름> 바이너리로 빌드
├── doctor.sh / ls.sh / survey.sh / nodeinfo.sh / invsync.sh / dhcp.sh / pxe.sh
│                                # 각 바이너리가 없으면 자동 빌드 후 실행하는 래퍼 스크립트
├── cli/                        # 7개 바이너리가 공유하는 로직
│   ├── client.go                 # conf 로딩 + AWX 클라이언트 생성, 실행 이력 기록
│   ├── poll.go                    # Job/인벤토리 동기화 상태 폴링, 실패 시 stdout tail
│   ├── choice.go                   # 번호/값/생략 선택지 처리 (dhcp, pxe가 공유)
│   └── prompt.go                    # 표준입력 프롬프트 헬퍼
├── cmd/                        # 단계별 진입점(각각 독립된 main 패키지)
│   ├── doctor/main.go
│   ├── ls/main.go
│   ├── survey/main.go
│   ├── nodeinfo/main.go
│   ├── invsync/main.go
│   ├── dhcp/main.go
│   └── pxe/main.go
├── config/                     # 설정 파일 로더(config.go) + 사용자 식별 훅(user.go) + 호스트 목록 로더
├── awx/                        # AWX REST API(/api/v2) 클라이언트
├── conf/
│   ├── sample_setting.conf       # ${user}_setting.conf 작성용 샘플
│   └── sample.txt                 # ${user}.txt(호스트 목록) 작성용 샘플
└── vendor/                     # 빌드에 필요한 Go 의존성 패키지 모음 (현재 외부 의존성 없음)
```
