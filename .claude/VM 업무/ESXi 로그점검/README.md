# ESXi 로그점검 (ESXi Log Check) 툴 및 모의 환경

이 프로젝트는 ESXi 장비에서 발생하는 각종 치명적인 로그(MCE, PSOD, APD/PDL, vSAN ESA, NVMeoF 단절 등)를 수집하여 분석하는 도구(`esxi-log-check`)와, 이를 테스트할 수 있는 **ESXi 8.0 이상 모의 환경(Mock Generator)**을 포함합니다.

## 디렉토리 구조
- `main.go`, `collect.go`, `parse.go`: 실제 ESXi 장비(혹은 모의 환경)에 SSH로 접속하여 로그를 긁어오고 분석하는 핵심 Go 애플리케이션입니다.
- `esxi_critical_patterns.yaml`: ESXi 치명적 오류를 판별하는 정규표현식 정의 파일입니다.
- `internal/mock/`: ESXi가 없는 환경(Rocky Linux 등)에서 ESXi 8.0 이상의 로그를 방대하게 쏟아내고 CLI를 흉내 내는 가짜 환경 스크립트들이 들어 있습니다.
  - `esxi_mock_logger.go`: 300종 이상의 치명적 에러를 뿜어내는 모의 로그 생성기 (Go 기반).
  - `localcli_mock.sh`, `esxcli_mock.sh`: ESXi의 하드웨어 정보를 리턴하는 CLI 래퍼.
- `vendor/`, `go.mod`, `go.sum`: 폐쇄망 오프라인 빌드를 위한 의존성 패키징 폴더.

---

## 1. 폐쇄망 환경 오프라인 빌드 가이드 (`esxi-log-check`)

본 폴더에는 필요한 모든 Go 의존성(`yaml.v3`, `crypto/ssh` 등)이 `vendor/` 디렉토리에 함께 포함되어 있습니다. 따라서 인터넷(외부망) 연결 없이도 빌드가 가능합니다.

1. **Go 설치 확인**: Linux 시스템에 Go가 설치되어 있어야 합니다. (Rocky Linux의 경우 `dnf install golang`)
2. **프로젝트 다운로드**: Git 등에서 이 폴더를 통째로 압축하여 폐쇄망 서버(Rocky Linux 등)로 옮긴 후 압축을 풉니다.
3. **빌드 명령어 실행**: 해당 폴더 내부로 이동하여 **`-mod=vendor`** 플래그를 주고 빌드합니다.
   ```bash
   cd "ESXi 로그점검"
   go build -mod=vendor -o esxi-log-check main.go collect.go parse.go
   ```
4. **실행**:
   ```bash
   ./esxi-log-check -host 192.168.0.58 -user root -pass "password" -mode all
   ```

---

## 2. 모의 환경 (Mock Environment) 셋업 가이드

ESXi 실장비 없이 `192.168.0.58` (Rocky Linux) 서버를 ESXi인 것처럼 속여 방대한 로그 수집/분석 테스트를 진행할 수 있습니다.

### 2.1 Mock Logger (랜덤 로그 발생기) 빌드 및 배포
`internal/mock/esxi_mock_logger.go`를 빌드하여 실행하면 `/var/run/log/` 경로에 가짜 ESXi 로그가 무작위로 생성됩니다.

```bash
cd internal/mock/
# 의존성이 필요 없는 단일 파일이므로 바로 빌드 가능합니다.
go build -o esxi_mock_logger esxi_mock_logger.go

# 시스템 바이너리 경로로 이동
sudo mv esxi_mock_logger /usr/local/bin/
sudo chmod +x /usr/local/bin/esxi_mock_logger
```

### 2.2 ESXi CLI 래퍼 (localcli, esxcli) 배포
ESXi의 고유 CLI 명령어를 흉내 내어 가짜 결과를 출력하는 스크립트를 배포합니다.

```bash
sudo cp localcli_mock.sh /usr/local/bin/localcli
sudo cp esxcli_mock.sh /usr/local/bin/esxcli
sudo chmod +x /usr/local/bin/localcli /usr/local/bin/esxcli
```

### 2.3 모의 테스트 진행 방법
1. Mock Logger를 실행하여 가짜 에러를 발생시킵니다. `-count` 인자로 발생시킬 복합 시나리오의 갯수를 지정할 수 있습니다. (1~10개 동시 다발적 생성 추천)
   ```bash
   # 5개의 서로 다른 장애 상황을 무작위로 혼합하여 발생시킵니다.
   esxi_mock_logger -count 5
   ```
2. 이제 `esxi-log-check` 분석 도구를 해당 Rocky Linux IP로 향하게 하여 실행합니다.
   ```bash
   ./esxi-log-check -host 192.168.0.58 -user root -pass "password" -mode all
   ```
3. 분석 툴이 `/var/run/log/vmkernel.log` 등을 긁어와 `esxi_critical_patterns.yaml` 룰셋에 기반하여 정확하게 장애를 탐지하고 리포팅하는지 확인합니다.
