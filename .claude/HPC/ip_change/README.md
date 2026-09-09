# ip_change

Red Hat Enterprise Linux 대상 노드들의 **IP·게이트웨이 설정을 일괄 변경**하는 도구입니다.

관리 노드에서 Go 바이너리를 실행하면, 대상 전체를 담은 apply 스크립트 하나를 만들어
`gossh` 로 대상 노드에 밀어 넣고 실행합니다. 네트워크 서비스는 재시작하지 않습니다
(설정 파일 변경에만 집중).

> **본 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을
> 권장합니다.** 설정 변경 스크립트이므로, 적용 후 대상 서버 중 무작위로 몇 대를
> 직접 접속해 설정이 실제로 반영됐는지 반드시 확인하십시오.

- 저장소를 통째로 내려받으면 **폐쇄망에서 그대로 빌드**됩니다. 외부 의존성이 없습니다(Go 표준 라이브러리만 사용).
- `CGO_ENABLED=0` 정적 빌드라 구형 배포판에서도 glibc 버전과 무관하게 동작합니다.
- 대상 노드에 Go 도 필요 없습니다. 노드에서 도는 것은 bash 뿐입니다.
- ifcfg 파일을 **통째로 덮어쓰지 않고 `IPADDR`/`GATEWAY` 두 키만 갱신**합니다.
- 변경 전 원본 파일을 `.bak.<시각>` 으로 백업합니다.

---

## 1. 빌드 및 설치 방법

### 1.1 사전 준비

| 항목 | 필요 위치 | 비고 |
|---|---|---|
| Go 툴체인 | 관리 노드 | `go.mod` 기준 1.26.5. 빌드할 때만 필요 |
| `gossh` | 관리 노드 | 저장소의 [`.claude/공통/gossh/v2`](../../공통/gossh/v2/) |
| bash / awk / sed / getent | 대상 노드 | 기본 설치분으로 충분 |

> **`gossh` 는 v2 를 쓰십시오.** 구버전에는 명령 앞뒤 따옴표를 잘라내는 버그가 있어
> 원격 주입이 깨집니다. `gossh -h` 에 `-cf` 와 `-b` 가 보이면 v2 입니다.

### 1.2 내려받아 빌드하기

```bash
git clone https://github.com/qazx2675/myrepo.git
cd "myrepo/.claude/HPC/ip_change"
./setup.sh
```

`setup.sh` 는 `GOPROXY=off` 로 인터넷 접속을 시도하지 않고 `bin/ip-change-engine` 을 만듭니다.

수동 빌드:

```bash
CGO_ENABLED=0 go build -o bin/ip-change-engine ./cmd/ip-change-engine
```

### 1.3 설정 파일 채우기

```bash
cp conf/ip_change.conf.sample  conf/ip_change.conf       # 선택 사항(없어도 기본값으로 동작)
cp conf/testuser.txt.sample    conf/<계정명>.txt          # 작업 대상 목록
```

- `conf/ip_change.conf` — ifcfg 를 찾을 경로(`network_scripts_dir`), RHEL 9+ 대비 경로(`rhel9_path`). 없어도 기본값(`/etc/sysconfig/network-scripts`)으로 동작합니다.
- `conf/<계정명>.txt` — 작업 대상. `hostname 변경될ip` 공백 구분, 한 줄에 하나.

```
hostname1 3.3.3.3
hostname2 2.2.2.2
```

- `scripts/run_ip_change.sh` 의 `select_user_context()` — 비워둔 자리입니다. 계정 선택 로직을
  채우고 `RUN_USER` 변수에 계정명을 대입하십시오. 그 값으로 `conf/${RUN_USER}.txt` 를 로드합니다.

### 1.4 설치 확인

```bash
go test ./...
```

---

## 2. 사용 방법

```bash
cd scripts
./run_ip_change.sh
```

또는 엔진을 직접 호출:

```bash
./bin/ip-change-engine -targets conf/testuser.txt -u root -i ~/.ssh/id_rsa
```

### 실행 흐름

1. `select_user_context()` 로 작업 계정을 선택 → `conf/${RUN_USER}.txt` 로드
2. 대상 전체에 대한 apply 스크립트 하나를 만들어 gossh 로 일괄 전송
3. 각 노드에서:
   - `getent hosts $(hostname)` 로 **현재 서비스 IP** 확인
   - `network_scripts_dir`(RHEL 9+ 이고 `rhel9_path` 가 설정돼 있으면 그 경로) 안에서
     `IPADDR=<현재 서비스 IP>` 인 ifcfg 파일 탐색
   - 원본을 `.bak.<시각>` 백업 후 `IPADDR`/`GATEWAY` 두 줄만 갱신
   - 게이트웨이는 변경 IP의 마지막 옥텟을 `1` 로 바꾼 값 (예: `3.3.3.3` → `3.3.3.1`)
   - 갱신 결과를 다시 읽어 검증
   - 네트워크 서비스는 재시작하지 않음
4. 결과를 한 줄씩 출력 (성공은 초록, 실패는 굵은 빨강)

```
hostname1 3.3.3.3 3.3.3.1
hostname2 2.2.2.2 2.2.2.1
```

---

## 3. 옵션별 상세 설명

### `bin/ip-change-engine`

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-config` | `./ip_change.conf` | 설정 파일 경로 (없어도 기본값으로 동작) |
| `-targets` | (필수) | 대상 목록 파일 경로 (`hostname 변경될ip`) |
| `-gossh` | `gossh` | gossh 실행 파일 경로 |
| `-u` | `root` | SSH 접속 계정 |
| `-p` | (없음) | SSH 접속 비밀번호 |
| `-i` | (없음) | SSH 키 파일 경로 |
| `-P` | `22` | SSH 포트 |
| `-c` | `0`(gossh 기본값) | gossh 동시 접속 수 |
| `-t` | `0`(gossh 기본값) | gossh 접속 타임아웃(초) |
| `-remote-path` | `/root/ip_change_apply.sh` | 원격에 떨어뜨릴 스크립트 경로 (실행 후 삭제됨) |

### `conf/ip_change.conf`

| 키 | 기본값 | 설명 |
|---|---|---|
| `network_scripts_dir` | `/etc/sysconfig/network-scripts` | ifcfg-* 를 찾을 경로 |
| `rhel9_path` | (비움) | 채우면, 노드 OS 메이저 버전이 9 이상일 때만 이 경로를 대신 씁니다. 이 경로 아래에서도 기존 ifcfg(KEY=VALUE) 형식을 찾는다고 가정합니다 — NetworkManager keyfile(.nmconnection) 형식은 지원하지 않습니다 |

### `scripts/run_ip_change.sh`

| 옵션 | 설명 |
|---|---|
| `-config <경로>` | 설정 파일 지정 (기본 `../conf/ip_change.conf`) |
| 환경변수 `GOSSH_ARGS` | 엔진에 그대로 전달할 gossh 관련 플래그 (예: `GOSSH_ARGS="-i ~/.ssh/id_rsa"`) |

---

## 4. 문서별 설명

| 폴더/파일 | 역할 |
|---|---|
| `setup.sh` | 폐쇄망 오프라인 빌드 |
| `cmd/ip-change-engine/main.go` | CLI 진입점. 대상 로드 → 스크립트 생성 → gossh 호출 → 결과 출력 |
| `internal/target/` | `{user}.txt`(`hostname 변경될ip`) 파싱, 게이트웨이(`.1`) 계산 |
| `internal/config/` | `ip_change.conf`(평문 key=value) 파싱 |
| `internal/render/` | apply 스크립트 조립 (`apply_body.sh` 를 `go:embed`) |
| `internal/render/apply_body.sh` | **실제 IP/GATEWAY 를 바꾸는 본체.** 대상 판정, ifcfg 탐색, 백업, 갱신, 검증 |
| `internal/remote/` | gossh 호출. base64 로 인코딩해 원격 주입하고 `<host>: <내용>` 출력을 파싱 |
| `internal/color/` | 관리 노드 출력에만 쓰는 ANSI 색상(성공=녹색, 실패=굵은 빨강). 대상 노드로 전송되는 스크립트 출력에는 적용하지 않음 |
| `scripts/run_ip_change.sh` | 관리자용 래퍼. 계정 선택(`select_user_context`, 빈 자리) → 엔진 실행 |
| `conf/*.sample` | 설정·대상 목록 예시. 실제 값 파일은 `.gitignore` 로 커밋 차단 |

자세한 파일별 역할과 수정 요청별 참고 위치는 [ARCHITECTURE.md](ARCHITECTURE.md) 를 보십시오.
