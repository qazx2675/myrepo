# 계정 수동 마운트 자동화 (account-mount)

담당자로부터 전달받은 계정 및 스토리지 정보(`info.txt`)를 바탕으로, 관리 노드에서 `gossh`를 통해 여러 대상 호스트들의 `/etc/auto.master`, `/etc/passwd`, `/etc/group`, `/etc/auto.home` 설정을 일괄 등록·갱신하고 `autofs` 서비스를 리로드/재시작하는 도구입니다.

> 폴더 및 파일별 상세 역할은 [ARCHITECTURE.md](ARCHITECTURE.md), 변경 이력은 [CHANGELOG.md](CHANGELOG.md), 배포 전 점검 항목은 [PR_CHECKLIST.md](PR_CHECKLIST.md)를 참고하십시오.

- **폐쇄망(오프라인) 완벽 지원**: 외부 라이브러리를 일절 사용하지 않고 Go 표준 라이브러리만으로 작성되어 인터넷 연결 없이 즉시 빌드됩니다. 또한 Go 1.20 기반 `CGO_ENABLED=0` 정적 바이너리(`bin/account-mount-engine`)가 함께 포함되어 있어 Go 컴파일러가 없는 폐쇄망이나 구형 OS(RHEL6 등)에서도 곧바로 실행할 수 있습니다.
- **대상 노드 무의존성**: 대상 노드에 Go나 별도 패키지가 필요 없으며, 기본 `bash`와 `awk`만으로 동작합니다.
- **수술적 갱신 및 멱등성**: 파일을 통째로 덮어쓰지 않고 대상 계정 줄만 추가하거나 갱신합니다. 이미 동일한 설정이 반영되어 있으면 `NOCHANGE`로 판정하여 불필요한 백업 생성이나 `autofs` 재시작을 하지 않습니다.

---

## 1. 빌드 및 설치 방법

### 1.1 사전 준비

| 항목 | 필요 위치 | 비고 |
|---|---|---|
| `bin/account-mount-engine` | 관리 노드 | 저장소에 정적 빌드된 바이너리 기본 포함 (소스 수정 시 `setup.sh`로 재빌드) |
| Go 툴체인 (1.20 이상) | 관리 노드 (선택) | 소스를 직접 재빌드할 때만 필요 |
| `gossh` | 관리 노드 | 병렬 SSH 실행 도구 (`PATH`에 등록되어 있거나 `-gossh` 옵션으로 경로 지정) |
| `bash`, `awk`, `grep` | 대상 노드 | 리눅스 기본 내장 도구 |

### 1.2 다운로드 및 오프라인 빌드

저장소(또는 브랜치 폴더)를 다운로드한 뒤 아래 명령으로 즉시 빌드 및 권한 설정을 수행할 수 있습니다.

```bash
git clone -b 계정생성 https://github.com/qazx2675/myrepo.git account-mount
cd account-mount

# 오프라인 정적 빌드 (Go가 설치된 환경)
chmod +x setup.sh run.sh
./setup.sh
```

수동 빌드 명령어:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=mod -o bin/account-mount-engine ./cmd/account-mount-engine
```

> **참고**: 폐쇄망 서버에 Go 컴파일러가 없더라도 저장소에 커밋된 `bin/account-mount-engine` 정적 바이너리를 그대로 사용할 수 있습니다 (`chmod +x bin/account-mount-engine run.sh`).

### 1.3 입력 파일 준비 (`info.txt`, `hosts.txt`)

```bash
cp info.txt.example  info.txt
cp hosts.txt.example hosts.txt
```

1. **`info.txt`** — 담당자가 전달해준 계정 정보를 줄바꿈 단위로 저장합니다.
   - 양식: `계정 GID/UID(둘다값이같음) 스토리지`
   ```text
   asdf 1234 qwer12345:/asdf/asdf
   ```
2. **`hosts.txt`** — 작업을 적용할 대상 서버(hostname 또는 IP)를 한 줄에 하나씩 작성합니다.
   ```text
   testserver0001
   testserver0002
   ```

---

## 2. 사용 방법

### 2.1 적용 규칙 요약

`info.txt`에 `asdf 1234 qwer12345:/asdf/asdf`가 입력되면 각 대상 서버의 4개 파일에 다음과 같이 적용됩니다.

1. **`/etc/auto.master`**
   - `/user /etc/auto.home` 항목이 공백(스페이스/탭) 개수와 무관하게 이미 존재하면 건너뛰고, 없으면 **가장 아랫줄에 추가**합니다.
   ```text
   /user /etc/auto.home
   ```
2. **`/etc/passwd`**
   - `계정:x:UID:GID:r계정:/user/계정:/bin/csh` 형식으로 등록합니다. (기존 동일 계정이 있으면 해당 줄 갱신, 없으면 맨 아래 추가)
   ```text
   asdf:x:1234:1234:rasdf:/user/asdf:/bin/csh
   ```
3. **`/etc/group`**
   - `계정:x:UID:` 형식으로 등록합니다. (기존 동일 그룹이 있으면 해당 줄 갱신, 없으면 맨 아래 추가)
   ```text
   asdf:x:1234:
   ```
4. **`/etc/auto.home`**
   - `계정 -rw,hard,actimeo=1,proto=tcp,vers=3 스토리지/계정` 형식으로 등록합니다. (기존 동일 계정 키가 있으면 해당 줄 갱신, 없으면 맨 아래 추가)
   ```text
   asdf -rw,hard,actimeo=1,proto=tcp,vers=3 qwer12345:/asdf/asdf/asdf
   ```
5. **백업 및 `autofs` 서비스 반영**
   - 변경되는 파일은 수정 직전 `<파일명>.bak.<YYYYMMDDHHMMSS>` 형식으로 원본을 백업합니다.
   - `/etc/auto.master` 또는 `/etc/auto.home`이 실제로 변경된 경우에만 `autofs` 서비스를 자동 리로드/재시작합니다.

### 2.2 기본 실행 예시

```bash
# 1) 변경 예정 내용(diff) 먼저 확인 (DRY-RUN, 실제 파일 수정 및 서비스 재시작 없음)
./run.sh -dry-run

# 2) 실제 일괄 적용 (hosts.txt 의 모든 호스트에 info.txt 적용)
./run.sh

# 3) 특정 호스트 1대에만 선행 적용
./run.sh -host testserver0001

# 4) gossh 없이 현재 로컬 서버에 직접 적용할 때
./run.sh -local
```

실행 결과 출력 예시:

```text
대상 호스트=2대  등록 계정=1건  모드=적용
적용 계정 목록 (info.txt):
  - 계정=asdf           UID/GID=1234     스토리지=qwer12345:/asdf/asdf/asdf
----------------------------------------------------------------------
  192.168.0.58             OK (accounts=1 changed:/etc/auto.master,/etc/passwd,/etc/group,/etc/auto.home)
  192.168.0.60             OK (accounts=1 changed:/etc/auto.master,/etc/passwd,/etc/group,/etc/auto.home)
----------------------------------------------------------------------
OK           2대
```

동일한 내용으로 다시 실행하면 이미 적용되어 있으므로 `NOCHANGE`가 출력됩니다:

```text
  192.168.0.58             NOCHANGE (accounts=1)
  192.168.0.60             NOCHANGE (accounts=1)
----------------------------------------------------------------------
NOCHANGE     2대
```

### 2.3 되돌리기 (Rollback)

적용 시 생성된 `<파일>.bak.<STAMP>` 백업을 이용해 변경 전 상태로 안전하게 복원할 수 있습니다.

```bash
# 1) 각 노드에 남아 있는 백업 시점(STAMP) 목록 조회
./run.sh -list-backups

# 2) 가장 최근 백업으로 복원할 때 무엇이 바뀌는지 확인 (DRY-RUN)
./run.sh -rollback -dry-run

# 3) 가장 최근 백업 시점으로 실제 복원
./run.sh -rollback

# 4) 특정 백업 시점(14자리 STAMP)을 지정해 복원
./run.sh -rollback-to 20261106154500
```

---

## 3. 옵션별 상세 설명

`./run.sh`는 모든 명령행 옵션을 내부 Go 엔진(`bin/account-mount-engine`)에 그대로 전달합니다.

| 옵션 | 기본값 | 필수 여부 | 상세 설명 |
|---|---|---|---|
| `-info <경로>` | `./info.txt` | 선택 | 계정 마운트 정보 파일 경로 (`계정 UID/GID 스토리지` 줄바꿈 단위). 환경변수 `INFO_FILE`로도 지정 가능 |
| `-hosts <경로>` | `./hosts.txt` | 선택 | 대상 호스트 목록 파일 경로 (한 줄에 hostname 또는 IP 1개). 환경변수 `HOSTS_FILE`로도 지정 가능 |
| `-host <호스트>` | *(없음)* | 선택 | 단일 대상 호스트만 지정하여 실행 (`-hosts` 파일 대신 이 호스트 1대만 처리) |
| `-local` | `false` | 선택 | `gossh`를 거치지 않고 현재 실행 중인 로컬 서버에 직접 적용 |
| `-dry-run` | `false` | 선택 | 파일을 실제로 변경하거나 서비스를 재시작하지 않고 바뀔 내용(`diff`)만 출력 |
| `-print-script` | `false` | 선택 | 대상 노드에서 실행될 bash 스크립트 전문을 표준출력으로 확인하고 종료 |
| `-root <경로>` | *(빈 값 = 실제 `/etc`)* | 선택 | 테스트용 루트 디렉터리 지정 (지정 시 실제 `/etc` 대신 `<root>/etc` 하위에 기록하며 서비스 재시작 건너뜀) |
| `-list-backups` | `false` | 선택 | 각 대상 노드에 남아 있는 `.bak.<STAMP>` 백업 시점 및 파일 목록 조회 |
| `-rollback` | `false` | 선택 | 가장 최근 백업 시점으로 파일 복원 및 필요 시 `autofs` 리로드 |
| `-rollback-to <STAMP>` | *(없음)* | 선택 | 지정한 14자리 백업 시점(`YYYYMMDDHHMMSS`)으로 복원 |
| `-gossh <경로>` | `gossh` | 선택 | `gossh` 실행 파일 경로 |
| `-u <계정>` | `root` | 선택 | SSH 접속 계정 |
| `-p <비밀번호>` | *(없음)* | 선택 | SSH 접속 비밀번호 |
| `-i <키경로>` | *(없음)* | 선택 | SSH 개인키 파일 경로 |
| `-P <포트>` | `22` | 선택 | SSH 접속 포트 |
| `-c <정수>` | `0` (`gossh` 기본값) | 선택 | `gossh` 동시 접속 수 |
| `-t <초>` | `0` (`gossh` 기본값) | 선택 | `gossh` 접속 타임아웃(초) |
| `-remote-path <경로>` | `/root/account_mount_apply.sh` | 선택 | 원격 노드에 임시로 생성 후 삭제할 스크립트 경로 |
| `-h`, `--help` | - | 선택 | 도움말 출력 |

---

## 4. 문서별 설명 및 주의사항 (Disclaimer)

### 문서 구성

| 파일명 | 설명 |
|---|---|
| `README.md` | 빌드/설치, 사용법, 옵션, 주의사항, 전역 명령어 등록 가이드 (본 문서) |
| `ARCHITECTURE.md` | 폴더 및 파일별 역할 표, 수정 가이드, 전체 처리 흐름도(Mermaid) |
| `CHANGELOG.md` | 버전별 변경 이력 및 Git 태그 연결 규칙 |
| `PR_CHECKLIST.md` | 코드 수정 및 배포 전 필수 점검 체크리스트 |

### 주의사항 (Disclaimer)

> **주의사항 (Disclaimer)**: 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.

> **주의사항**: 설정 변경 스크립트의 경우, 설정 변경 완료 후 반드시 랜덤한 서버 몇 대를 직접 점검하여 변경 사항이 의도대로 정상 반영되었는지 교차 검증하십시오.

- 본 도구는 `/etc/passwd`, `/etc/group`, `/etc/auto.master`, `/etc/auto.home` 등 **시스템 계정 및 마운트 핵심 파일**을 수정합니다.
- 대규모 서버에 일괄 적용하기 전 반드시 `./run.sh -dry-run`으로 변경 예정 내역을 확인하고, `./run.sh -host <샘플호스트>`로 1~2대에 먼저 적용한 뒤 `id <계정>`, `getent passwd <계정>`, `ls -ld /user/<계정>`으로 정상 동작을 확인하십시오.
- 적용이 새로 생성한 파일(예: 기존에 `/etc/auto.home` 파일 자체가 없었던 노드에 새로 생성된 경우)은 이전 원본 백업(`.bak.<STAMP>`)이 존재하지 않으므로 롤백 시 임의로 삭제하지 않습니다.

---

## 5. 전역 명령어로 사용하기 (선택 사항)

어느 디렉터리에서나 `account-mount-engine` 명령을 바로 실행하려면 `/usr/local/bin`에 심볼릭 링크를 생성하거나 바이너리를 복사하십시오.

```bash
# 심볼릭 링크 등록
sudo ln -sf "$(pwd)/bin/account-mount-engine" /usr/local/bin/account-mount-engine

# 또는 바이너리 직접 복사
sudo cp bin/account-mount-engine /usr/local/bin/account-mount-engine
```

전역 등록 후 실행 예시:

```bash
account-mount-engine -info /path/to/info.txt -hosts /path/to/hosts.txt -dry-run
account-mount-engine -info /path/to/info.txt -hosts /path/to/hosts.txt
```
