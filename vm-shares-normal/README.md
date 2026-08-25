# vm-shares-normal

`-f`로 지정한 목록 파일(list.txt)에 있는 VM들의 vCenter CPU/Memory Shares ratio를 **Normal**로 일괄 설정하는 Go 도구입니다.

이 폴더를 통째로 다운로드하면 **폐쇄망(외부 네트워크 접근 불가) 환경에서도 빌드**할 수 있도록 의존 패키지가 `vendor/` 아래에 함께 포함되어 있습니다.

---

## 1. 빌드 및 설치 방법

### 사전 요구사항
- Go 1.21 이상 (`go version`으로 확인)
- vCenter에 접속 가능한 네트워크 (빌드 자체는 오프라인 가능, **실행 시**에는 vCenter 접속이 필요함)

### 1-1. 다운로드
Git 저장소에서 이 폴더(`vm-shares-normal/`) 전체를 내려받습니다. `vendor/` 폴더까지 함께 받아야 폐쇄망 빌드가 가능합니다.

```bash
git clone <저장소 URL>
cd <저장소>/vm-shares-normal
```

또는 이미 저장소를 갖고 있다면 `git pull` 후 이 폴더로 이동합니다.

### 1-2. 빌드

**인터넷이 연결된 환경**(일반 빌드, go.sum 기준 의존성 재검증):
```bash
go build -o vm-shares-normal .
```

**폐쇄망(오프라인) 환경** — 반드시 `vendor/`를 함께 받은 상태에서:
```bash
GOFLAGS=-mod=vendor GOPROXY=off go build -o vm-shares-normal .
```

빌드가 끝나면 현재 폴더에 실행 파일 `vm-shares-normal`(Windows는 `vm-shares-normal.exe`)이 생성됩니다. 별도 설치 스크립트는 없으며, 이 실행 파일을 원하는 위치로 복사해서 사용하면 됩니다.

---

## 2. 사용 방법

### 2-1. 대상 VM 목록 파일 준비 (list.txt)
줄바꿈으로 구분된 VM 이름 목록을 작성합니다. `#`으로 시작하는 줄은 주석으로 무시됩니다.

```
web-vm-01
web-vm-02
# 아래는 점검 제외
# db-vm-01
```

### 2-2. 비밀번호 환경변수 설정
vCenter 비밀번호는 커맨드라인 인자 대신 환경변수로 전달합니다.

```bash
export VC_PASSWORD='vCenter 로그인 비밀번호'
```

### 2-3. 실행

바이너리를 직접 빌드했다면 `vm-shares-normal.sh` 래퍼로 실행하는 것이 편합니다. 바이너리가 없으면 자동으로 빌드(폐쇄망이면 `vendor/` 사용)한 뒤 실행합니다.

```bash
./vm-shares-normal.sh -vc 192.168.0.50 -id administrator@vsphere.local -f list.txt
```

바이너리를 직접 실행해도 동일하게 동작합니다.

```bash
./vm-shares-normal -vc 192.168.0.50 -id administrator@vsphere.local -f list.txt
```

실행 결과는 VM 단위로 아래처럼 출력됩니다.

```
[OK]   web-vm-01: CPU/Memory shares ratio set to normal
[OK]   web-vm-02: CPU/Memory shares ratio set to normal
[SKIP] db-vm-01: not found in vCenter
[FAIL] app-vm-03: <에러 메시지>
```

---

## 3. 옵션별 상세 설명

| 옵션 | 필수 여부 | 설명 |
|---|---|---|
| `-vc` | 필수 | 대상 vCenter 주소 (예: `192.168.0.50`) |
| `-id` | 필수 | vCenter 로그인 계정 (예: `administrator@vsphere.local`) |
| `-f` | 필수 | 대상 VM 목록 파일 경로 (한 줄에 VM 이름 하나, `#` 주석 지원) |
| `-insecure` | 선택 (기본값 `true`) | TLS 인증서 검증을 건너뜁니다. 자체 서명 인증서를 쓰는 vCenter/ESXi 랩 환경 기본값이며, 운영 환경에서 정식 인증서를 쓴다면 `-insecure=false`로 지정하세요. |
| `VC_PASSWORD` (환경변수) | 필수 | vCenter 로그인 비밀번호. 커맨드라인에 평문으로 남지 않도록 환경변수로만 받습니다. |

동작 범위: 지정한 vCenter 전체(모든 데이터센터/폴더)에서 목록에 있는 이름과 일치하는 VM을 찾아 `CpuAllocation.Shares`, `MemoryAllocation.Shares`를 둘 다 `Level=Normal`로 Reconfigure합니다. 그 외 설정(레벨이 아닌 커스텀 shares 값, 리소스 풀, 예약/제한 등)은 건드리지 않습니다.

---

## 4. 문서별 설명

| 파일 | 설명 |
|---|---|
| `main.go` | 실제 로직 (플래그 파싱 → 목록 파일 읽기 → vCenter 접속 → VM 조회 → Shares Reconfigure) |
| `vm-shares-normal.sh` | 실행 편의용 bash 래퍼. 바이너리가 없으면 자동 빌드 후 인자를 그대로 전달해서 실행 |
| `go.mod` / `go.sum` | 의존 패키지 버전 고정 (`github.com/vmware/govmomi`) |
| `vendor/` | 위 의존 패키지의 소스 전체 사본 — 폐쇄망 빌드용. 직접 수정하지 마세요(빌드 시 자동 생성/동기화됨: `go mod vendor`) |
| `README.md` | 이 문서 |

---

## 5. 전역 명령어로 사용하기 (선택 사항)

매번 실행 파일 경로를 입력하기 번거롭다면 PATH가 잡힌 위치로 옮겨서 어디서든 `vm-shares-normal`로 실행할 수 있습니다.

```bash
sudo mv vm-shares-normal /usr/local/bin/
vm-shares-normal -vc 192.168.0.50 -id administrator@vsphere.local -f list.txt
```

---

## 주의사항 (Disclaimer)

- 본 설정 변경 관련 스크립트 및 툴은 100% 신뢰하기보다는 **참고용(보조 도구)**으로 사용하는 것을 권장합니다.
- 설정 변경 스크립트 실행 후에는 반드시 **대상 서버 중 무작위로 몇 대를 골라 vCenter 등에서 실제로 값이 변경되었는지 직접 확인**하시기 바랍니다. 출력 로그의 `[OK]` 표시만으로 변경을 최종 확정하지 마세요.
- 이 도구는 대상 VM의 전원 상태나 다른 VM/그룹과의 설정 동질성을 검증하지 않습니다. 실행 전 대상 목록(list.txt)이 의도한 VM만 포함하는지 반드시 재확인하세요.
