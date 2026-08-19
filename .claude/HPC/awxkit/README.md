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

## 3. 옵션별 상세 설명

### 3.1 전역 플래그
| 플래그 | 설명 |
|---|---|
| `-conf <경로>` | 설정 파일 경로를 직접 지정 (탐색 순서 무시) |
| `-user <이름>` | 사용자 식별자를 직접 지정 (`AWXKIT_USER` 환경변수보다 우선순위 낮음) |

### 3.2 명령
| 명령 | 상태 | 설명 |
|---|---|---|
| `doctor` | **구현됨** | conf 로딩, 파일 권한, AWX 연결/버전, 템플릿 존재·실행 권한·`ask_variables_on_launch` 점검 |
| `ls` | 예정 | 템플릿 목록 조회 |
| `survey` | 예정 | 템플릿의 survey 정의(변수명·선택지) 조회 → conf 스니펫 출력 |
| `nodeinfo` | 예정 | [S1] NodeInfo 템플릿 실행 및 결과 파일 저장 |
| `invsync` | 예정 | [S2] 인벤토리 동기화 |
| `dhcp` | 예정 | [S3] DHCP 등록 |
| `pxe` | 예정 | [S4] PXE 등록 및 호스트 수 리포트 |

### 3.3 설정 파일(`${user}_setting.conf`) 키
`key = value` 형식, `#` 이후는 주석. 전체 항목은 [`conf/sample_setting.conf`](./conf/sample_setting.conf) 참고.

| 키 | 설명 |
|---|---|
| `awx_url` / `username` / `password` | AWX 접속 정보 |
| `insecure_tls` | 사설 인증서 환경에서 `true` |
| `s1_*` | [S1] NodeInfo 템플릿/파라미터/결과 취득 방식(`s1_fetch`: artifacts\|stdout\|remote)/저장 경로 |
| `s2_*` | [S2] 인벤토리 소스·대상 인벤토리 ID |
| `s3_*` | [S3] DHCP 템플릿/인프라 선택 변수명·옵션 |
| `s4_*` | [S4] PXE 템플릿/인프라·OS·Boot Mode·Splunk 변수명, 결과 집계용 인벤토리 ID |
| `poll_interval` | Job 상태 폴링 간격(초) |
| `history_file` | 실행 이력 기록 파일 |

## 4. 문서별 고유 설명
- 상세 설계·API 매핑·단계별 계획: [`PLAN.md`](./PLAN.md)
- 작업 이력: [`WORKLOG.md`](./WORKLOG.md)
- 현재 1단계(설정 로딩 + `doctor`)까지 구현 완료. 나머지 명령은 `PLAN.md`의 단계별 마일스톤에 따라 순차 구현됩니다.
