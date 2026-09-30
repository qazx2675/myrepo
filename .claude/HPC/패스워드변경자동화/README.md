# 패스워드변경자동화 (pwreset)

비밀번호 롤테이션 스크립트가 누락되어 SSH 접속 시 `Current password` 변경
프롬프트가 뜨는 Rocky Linux 서버 소수를, **알고 있는 과거 비밀번호 후보
목록**으로 자동 로그인한 뒤 **지정된 새 비밀번호**로 맞춰주는 소규모 복구
자동화 도구입니다. 기존 expect 기반 스크립트를 Go 로 재작성했습니다.

> 폴더·파일별 역할은 [ARCHITECTURE.md](ARCHITECTURE.md) 를 참고하세요.

- 저장소를 통째로 내려받으면 **폐쇄망에서 그대로 빌드**됩니다. 외부
  네트워크 접속 없이 `go.sum` 에 고정된 `golang.org/x/crypto` 모듈만 씁니다
  (Go 모듈 캐시 또는 vendor 가 준비돼 있어야 합니다).
- 대상 서버에는 아무것도 설치하지 않습니다. 접속은 순수 SSH 프로토콜(`golang.org/x/crypto/ssh`)만 사용합니다.
- 새 비밀번호는 평문으로 디스크에 남기지 않습니다. AES-256-GCM 으로 암호화한
  `newpass.enc` 와 별도의 키 파일(`key.bin`)로 나눠 보관합니다.
- 대상별로 goroutine 을 띄워 병렬 처리합니다.

## 사전 조건 (반드시 읽으십시오)

**본인이 소유·관리하는 서버, 최대 3대의 소규모 랩 환경 전용 도구입니다.**
비밀번호 롤테이션 정책이 없거나 일시적으로 누락되어, SSH 로그인 시
비밀번호 변경 프롬프트가 뜨는 것을 알고 있고, 그 서버의 과거 비밀번호
후보를 알고 있는 상황에서만 사용하십시오.

- **`list.txt` 에 명시된 서버 외에는 절대 사용하지 마십시오.** 임의의
  서버를 대상으로 비밀번호 후보를 무차별 대입하는 용도가 아닙니다.
- 계정은 `root` 로 고정되어 있습니다 (소스: `cmd/pwreset/sshclient.go`).
- 계정 잠금 정책(실패 횟수 제한)이 없는 랩 환경을 전제로 합니다. 실서버에
  계정 잠금 정책이 걸려 있다면, 후보 비밀번호 시도만으로도 계정이 잠길 수
  있으니 사용하지 마십시오.
- 본 도구는 **아직 실서버 검증 전**입니다. 아래 "주의사항" 을 반드시 읽고
  소수 대상으로 먼저 시험하십시오.

## 1. 빌드 및 실행 방법

### 1.1 사전 준비

| 항목 | 필요 위치 | 비고 |
|---|---|---|
| Go 툴체인 | 관리 PC | `go.mod` 기준 1.26.5. 빌드할 때만 필요 |
| SSH(22번 포트) 접속 가능 | 관리 PC → 대상 서버 | 호스트 키 검증은 하지 않습니다(랩 환경 전제) |

### 1.2 빌드 및 실행

```bash
cd ".claude/HPC/패스워드변경자동화"
chmod +x run.sh
./run.sh -h
```

수동 빌드:

```bash
go build -o bin/pwreset ./cmd/pwreset
```

### 1.3 입력 파일 준비

```bash
cp list.txt.example         list.txt
cp old_password.txt.example old_password.txt
```

- `list.txt` — 대상 서버 IP (줄바꿈으로 나열, 계정은 root 고정). 최대 3대
  전제입니다.
- `old_password.txt` — 과거 비밀번호 후보. **최신(가장 최근에 쓰였을 것으로
  추정)을 맨 위**에, 한 줄에 하나씩. `pwreset` 은 이 순서대로 시도합니다.

두 파일 모두 실제 값이 들어가므로 `.gitignore` 로 커밋을 막아 두었습니다.

### 1.4 새 비밀번호 암호화

새 비밀번호는 평문 그대로 넘기지 않고, 먼저 암호화해 파일로 저장합니다.

```bash
echo -n '새비밀번호' > /tmp/plain.txt   # 임시 평문 파일 (사용 후 직접 삭제)
./run.sh -encrypt -in /tmp/plain.txt -out newpass.enc -key key.bin
shred -u /tmp/plain.txt                # 또는 rm
```

`key.bin` 이 없으면 이때 새로 생성됩니다(0600 권한). 이후 `-key key.bin` 을
그대로 재사용하면 됩니다.

### 1.5 실행

```bash
./run.sh -list list.txt -oldpw old_password.txt -newpw newpass.enc -key key.bin -out result.csv
```

인자를 생략하면 위 기본 파일명을 그대로 씁니다(1.3~1.4 에서 만든 파일과
이름이 같다면 인자 없이 `./run.sh` 만 실행해도 됩니다).

### 1.6 결과 확인

`result.csv` 에 대상별 처리 결과가 남습니다.

| 컬럼 | 의미 |
|---|---|
| `ip` | 대상 IP |
| `found_password_index` | 성공한 후보의 `old_password.txt` 내 인덱스(0부터). 접속 실패 시 빈 값 |
| `status` | `성공` / `접속불가` (모든 후보로 시도해도 로그인 실패) |
| `applied_new_password` | 새 비밀번호 적용 여부(`예`/`아니오`) |

`접속불가` 로 남은 대상은 `old_password.txt` 의 후보로 로그인 자체가 안 된
것이므로, 후보 목록을 다시 확인하거나 직접 접속해 확인하십시오.

---

## 2. 옵션별 상세 설명

### `bin/pwreset`

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-encrypt` | `false` | 서브커맨드. 새 비밀번호 평문을 암호화해 `-out` 에 저장 |
| `-in` | (없음) | `-encrypt` 전용. 평문 비밀번호 파일 경로 |
| `-list` | `list.txt` | 대상 IP 목록 파일 (줄바꿈으로 IP 나열, 계정 root 고정) |
| `-oldpw` | `old_password.txt` | 과거 비밀번호 후보 목록 (최신이 맨 위) |
| `-newpw` | `newpass.enc` | 암호화된 새 비밀번호 파일 |
| `-key` | `key.bin` | AES-256 키 파일 (없으면 `-encrypt` 시 새로 생성) |
| `-out` | `result.csv` | 결과 CSV 경로 (`-encrypt` 시에는 암호문 출력 경로) |

### `run.sh`

빌드 후 입력 파일(list/oldpw/newpw/key, `-encrypt` 시에는 in) 존재 여부를
검증하고 `bin/pwreset` 을 실행하는 얇은 래퍼입니다. 인자는 그대로
`pwreset` 에 전달됩니다. `-h`/`--help` 또는 인자 누락 시 사용법을 출력합니다.

---

## 3. 문서별 설명

| 파일 | 역할 |
|---|---|
| `run.sh` | 빌드 + 입력 파일 검증 + 실행 래퍼 |
| `cmd/pwreset/main.go` | CLI 진입점. 플래그 파싱 → `-encrypt`/기본 모드 분기 → 병렬 처리 → CSV 저장 |
| `cmd/pwreset/sshclient.go` | 대상 한 대 접속. SSH 인증 단계(keyboard-interactive)와 pty 셸 단계 두 경로 처리 |
| `cmd/pwreset/promptfsm.go` | 비밀번호 변경 프롬프트 상태머신 (두 경로가 공유) |
| `cmd/pwreset/cryptutil.go` | AES-256-GCM 암복호화, 키 파일 로드/생성 |
| `cmd/pwreset/targets.go` | `list.txt`/`old_password.txt` 파싱 |
| `cmd/pwreset/report.go` | 결과 CSV 기록 |
| `list.txt.example`, `old_password.txt.example` | 입력 파일 형식 예시 (실제 값 없음) |
| `.gitignore` | 실제 대상 목록·비밀번호·키·결과 파일 커밋 차단 |

자세한 처리 흐름과 상태머신은 [ARCHITECTURE.md](ARCHITECTURE.md) 를 보십시오.

---

## 4. 주의사항 (Disclaimer)

본 도구의 실행 결과는 100% 신뢰하기보다 **참고용(보조 도구)** 으로 사용하는
것을 권장합니다.

- **`list.txt` 에 적은 서버 외에는 사용하지 마십시오.** 본인이 소유·관리하지
  않는 서버에 비밀번호 후보를 시도하는 것은 이 도구의 의도된 용도가
  아닙니다.
- **아직 실서버 검증 전입니다.** 소수 대상(1대)으로 먼저 실행해 `result.csv`
  결과와 실제 로그인 가능 여부를 직접 확인한 뒤 나머지 대상에 사용하십시오.
- 적용 후에는 **새 비밀번호로 직접 SSH 접속해 실제로 반영됐는지 확인**하십시오.
- `old_password.txt`, `key.bin`, `newpass.enc`, `result.csv` 는 비밀번호와
  직결된 파일입니다. 커밋 금지(`.gitignore` 등록됨)이며, 사용 후 불필요하면
  직접 삭제하십시오.
- 계정 잠금 정책이 있는 서버에는 사용하지 마십시오. 후보를 순서대로
  시도하는 과정에서 계정이 잠길 수 있습니다.
