# AWX 조작 툴 (awxkit)

이 도구는 Ansible AWX 서버를 조작하고 상태를 점검하기 위해 작성된 Go 언어 기반의 도구입니다. 폐쇄망 환경에서도 `vendor/` 폴더를 통해 독립적으로 빌드 및 실행이 가능하도록 구성되어 있습니다.

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
성공적으로 완료되면 `awxkit` 바이너리 파일이 생성됩니다.

### 1.3 전역 명령어로 사용하기 (선택 사항)
빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 이동하면 어디서든 명령어처럼 사용할 수 있습니다.
```bash
cp awxkit /usr/local/bin/
# 이후 어디서든 awxkit 명령어로 실행 가능
```

### 1.4 매번 직접 빌드하지 않고 바로 실행하기 (`run.sh`)
빌드 여부를 신경 쓰지 않고 곧바로 쓰고 싶다면 `run.sh`를 쓰면 됩니다.
바이너리가 없으면 자동으로 `setup.sh`를 실행해 빌드한 뒤, 넘긴 인자를 그대로 `awxkit`에 전달합니다.
```bash
bash run.sh doctor
bash run.sh              # 인자 없이 실행하면 대화형 메뉴
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
3. `<awxkit 실행 파일 위치>/conf/${user}_setting.conf`

### 2.3 실행
```bash
# 인자 없이 실행하면 번호 선택 메뉴가 뜹니다
./awxkit

# 또는 명령을 바로 지정 (스크립트에서 호출하기 좋음)
./awxkit doctor
AWXKIT_USER=hong ./awxkit doctor
./awxkit -conf /path/to/other.conf -user hong doctor
```

### 2.4 doctor로 최초 점검
설정을 채운 뒤 가장 먼저 `doctor`를 실행해 연결·권한·파라미터 설정을 확인합니다.
```
$ ./awxkit doctor
[i] 설정 파일: conf/hong_setting.conf
[✔] AWX 연결 성공 (버전: 23.0.0)
[✔] 조회 가능한 템플릿 12개
[✔] S1 (NodeInfo): 템플릿(nodeinfo, ID=7) 실행 가능
[!] S3 (DHCP): 템플릿(dhcp-register)의 'ask_variables_on_launch'가 꺼져 있습니다. extra_vars를 보내도 조용히 무시됩니다.
...
점검 완료.
```
`[X]`가 있으면 그 항목을 해결하기 전까지 이후 명령이 정상 동작하지 않습니다.

### 2.5 템플릿 탐색 (`ls` / `survey`)
현장 정보 수집 양식을 손으로 채우기 전에, 실제 AWX에서 템플릿과 변수명을 먼저 조회할 수 있습니다.
```bash
./awxkit ls
# 총 12개 템플릿
#   ID: 7     | nodeinfo                       | extra_vars 허용    | -
#   ID: 24    | pxe-register                   | extra_vars 허용    | survey 있음

./awxkit survey pxe-register     # 또는 survey 24
# [✔] pxe-register (ID: 24) — survey 4개 문항
#   1) 인프라        var=pxe_infra       (필수)
#      선택지: [seoul | daejeon | busan]
#   ...
# 위 var= 값을 conf의 관련 key(s3_infra_key, s4_osver_key 등)에 그대로 사용하세요.
```
`survey`에 인자를 주지 않으면(`./awxkit survey`, 또는 메뉴에서 선택) 템플릿 ID/이름을 물어봅니다.

### 2.6 [S1] NodeInfo 실행 (`nodeinfo`)
NodeInfo 템플릿은 hostname을 텍스트로 한 번에 받아 처리하는 구조입니다. hostname마다 따로 실행하지 않고,
**`${user}.txt`에 나열된 hostname 전체를 줄바꿈으로 이어붙여 템플릿을 한 번만 실행**하고, 결과도 파일 하나로 받습니다.
탐색 순서는 conf 파일과 동일합니다: `./conf/${user}.txt` → `~/.awxkit/${user}.txt` → `<awxkit 실행 파일 위치>/conf/${user}.txt`.

```bash
# conf/hong.txt
web01
web02
db01
```
```bash
./awxkit nodeinfo
# [i] 호스트 목록: conf/hong.txt (3개)
# [i] nodeinfo 실행 중... (3개 hostname을 한 번에 전달)
#     [job 1234] running
#     [job 1234] successful
# [✔] 완료 (job 1234) — 결과 저장: output/hong_nodeinfo.yaml
```
- `s1_hostname_key`에 hostname 전체(줄바꿈으로 이어붙인 텍스트)를 담아 `s1_template`을 한 번 실행하고, 결과를 `s1_output_dir/${user}_nodeinfo.yaml` 하나로 저장합니다.
- 결과 취득 방식(`s1_fetch`)에 따라: `artifacts`(기본, `s1_artifact_key`로 값을 지정하지 않으면 전체 artifacts를 저장), `stdout`(표준출력 그대로 저장), `remote`(API로 받을 수 없어 원격 경로 안내만 출력).
- 실패하면 stdout 마지막 30줄을 보여주고 종료 코드 1로 끝납니다.
- 모든 실행은 `history_file`에 한 줄씩 기록됩니다.
- `${user}.txt` 대신 다른 파일을 쓰려면 `-hosts <경로>`로 직접 지정할 수 있습니다.
```bash
./awxkit -hosts ./retry_list.txt nodeinfo
```

## 3. 옵션별 상세 설명

### 3.1 전역 플래그
| 플래그 | 설명 |
|---|---|
| `-conf <경로>` | 설정 파일 경로를 직접 지정 (탐색 순서 무시) |
| `-user <이름>` | 사용자 식별자를 직접 지정 (`AWXKIT_USER` 환경변수보다 우선순위 낮음) |
| `-hosts <경로>` | (`nodeinfo` 전용) `${user}.txt` 대신 사용할 호스트 목록 파일 |

### 3.2 명령
| 명령 | 상태 | 설명 |
|---|---|---|
| `doctor` | **구현됨** | conf 로딩, 파일 권한, AWX 연결/버전, 템플릿 존재·실행 권한·`ask_variables_on_launch` 점검 |
| `ls` | **구현됨** | 템플릿 목록(ID·이름·extra_vars 허용 여부·survey 유무) 조회 |
| `survey <ID\|이름>` | **구현됨** | 템플릿의 survey 문항(질문명·변수명·선택지·기본값·필수여부) 조회. 인자를 생략하면 대화형으로 물어봄 |
| `nodeinfo` | **구현됨** | [S1] `${user}.txt`의 hostname 전체를 한 번에 넣어 NodeInfo 템플릿 실행 및 결과 파일 저장 |
| `invsync` | 예정 | [S2] 인벤토리 동기화 |
| `dhcp` | 예정 | [S3] DHCP 등록 |
| `pxe` | 예정 | [S4] PXE 등록 및 호스트 수 리포트 |

### 3.3 설정 파일(`${user}_setting.conf`) 키
`key = value` 형식, `#` 이후는 주석. 전체 항목은 [`conf/sample_setting.conf`](./conf/sample_setting.conf) 참고.

| 키 | 설명 |
|---|---|
| `awx_url` / `username` / `password` | AWX 접속 정보 |
| `insecure_tls` | 사설 인증서 환경에서 `true` |
| `s1_*` | [S1] NodeInfo 템플릿/파라미터/결과 취득 방식(`s1_fetch`: artifacts\|stdout\|remote, `s1_artifact_key`)/저장 경로(`s1_output_dir`) |
| `s2_*` | [S2] 인벤토리 소스·대상 인벤토리 ID |
| `s3_*` | [S3] DHCP 템플릿/인프라 선택 변수명·옵션 |
| `s4_*` | [S4] PXE 템플릿/인프라·OS·Boot Mode·Splunk 변수명, 결과 집계용 인벤토리 ID |
| `poll_interval` | Job 상태 폴링 간격(초) |
| `history_file` | 실행 이력 기록 파일 |

## 4. 문서별 고유 설명
- 상세 설계·API 매핑·단계별 계획: [`PLAN.md`](./PLAN.md)
- 작업 이력: [`WORKLOG.md`](./WORKLOG.md)
- 현재 3단계(설정 로딩 + `doctor` + `ls`/`survey` + `nodeinfo`)까지 구현 완료. 나머지 명령은 `PLAN.md`의 단계별 마일스톤에 따라 순차 구현됩니다.

### 4.1 디렉토리 구조

```
awxkit/
├── README.md          # 이 문서
├── PLAN.md             # 설계·API 매핑·단계별 계획
├── WORKLOG.md          # 작업 이력
├── go.mod              # Go 모듈 정의 파일 (module awxkit)
├── .gitattributes       # *.sh/*.go/go.mod를 LF로 강제 (Windows 체크아웃 시 CRLF로 깨지는 것 방지)
├── main.go             # 진입점 — 전역 플래그, 대화형 메뉴, 명령 디스패치
├── common.go            # conf 로딩 + AWX 클라이언트 생성 + Job 폴링 등 공용 헬퍼
├── doctor.go           # doctor 명령 — conf/연결/권한/파라미터 점검
├── catalog.go           # ls / survey 명령 — 템플릿·survey 정의 조회
├── nodeinfo.go           # nodeinfo 명령 — [S1] hostname 전체를 한 번에 넣어 NodeInfo 실행 및 결과 저장
├── setup.sh            # vendor 패키지를 사용해 폐쇄망에서도 빌드하는 스크립트 (go build -o awxkit .)
├── run.sh              # 바이너리가 없으면 자동 빌드 후 실행하는 래퍼 스크립트
├── config/              # 설정 파일 로더(config.go) + 사용자 식별 훅(user.go) + 호스트 목록 로더
├── awx/                 # AWX REST API(/api/v2) 클라이언트
├── conf/
│   └── sample_setting.conf   # ${user}_setting.conf 작성용 샘플
└── vendor/              # 빌드에 필요한 Go 의존성 패키지 모음 (현재 외부 의존성 없음)
```
