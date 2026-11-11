# RHEL 8.10 필수 패키지 오프라인 저장 및 폐쇄망 자동 설치 도구

RHEL 8.10 설치 이미지(`/mnt/rhel810`) 및 로컬 yum 레포지토리(`RHEL810-base`, `RHEL810-app`)로부터 필수 시스템 패키지(`nscd`, `autofs`, `rpcbind`, `nfs-utils`, `tcsh`)와 하위 의존성 전체를 추출하여 저장하고, 외부 네트워크 및 GPG 키가 없는 폐쇄망 환경의 다른 RHEL 8 서버에 안전하게 일괄 설치하는 2종 스크립트 모음입니다.

---

## 📌 주요 설계 특징

- **포괄적인 사전 수집 (`save_packages.sh`)**:
  - 특정 패키지를 임의로 제외하지 않고, 마운트 미디어 및 레포지토리에서 대상 패키지와 전체 하위 의존성(glibc, libxml2, nfs 라이브러리 등)을 빠짐없이 온전히 수집합니다.
  - 별도의 설정 파일이나 간섭 없이 표준 DNF/repotrack 명령어로 동작합니다.

- **지능형 설치 필터링 (`install_packages.sh`)**:
  - 대상 서버의 환경을 실시간으로 분석하여 폐쇄망에서 자주 발생하는 3대 의존성 충돌을 **스크립트가 스스로 감지하고 자동 해결**합니다:
    1. **32비트(i686) RPM 자동 배제**: 64비트 시스템에 32비트 RPM이 혼입되어 발생하는 `libc.so.6(GLIBC_2.28)` 오류를 원천 차단합니다.
    2. **호스트 기존 패키지 보존**: 호스트에 이미 설치된 최신 라이브러리(예: `python3-libxml2`가 요구하는 최신 `libxml2`, 최신 `glibc`)가 있을 경우, 번들 내의 구버전 RPM을 자동으로 제외하여 다운그레이드 충돌을 방지합니다.
    3. **`nscd` GLIBC 릴리즈 락 자동 해결**: Red Hat의 `nscd`가 요구하는 엄격한 `glibc = %{version}-%{release}` 불일치 감지 시, `--nodeps` 옵션으로 안전하게 단독 설치(바이너리 ABI 100% 호환)하여 트랜잭션 실패를 방지합니다.

- **완전 폐쇄망 최적화**:
  - 외부 레포지토리 접속 차단(`--disablerepo='*'`) 및 GPG 서명 검증 비활성화(`--nogpgcheck`, `--nosignature`)로 인터넷이 차단된 환경에서도 문제없이 실행됩니다.

---

## 📦 대상 패키지 및 서비스

| 패키지명 | 설명 | 관련 시스템 서비스 |
|:---|:---|:---|
| `nscd` | Name Service Cache Daemon (DNS, passwd, group 등 네임서비스 캐싱) | `nscd.service` |
| `autofs` | 자동 마운트 데몬 (NFS/디렉터리 접근 시 자동 마운트 처리) | `autofs.service` |
| `rpcbind` | RPC 포트 매핑 데몬 (NFS v3/v4 등 연동 필수) | `rpcbind.service` |
| `nfs-utils` | NFS 클라이언트 및 마운트 유틸리티 (`mount.nfs` 등) | `nfs-client.target` |
| `tcsh` | C Shell 기반 확장 쉘 | - |

---

## 🚀 사용 방법

### 1단계: 패키지 추출 및 저장 (마운트 서버에서 실행)

RHEL 8.10 이미지가 마운트되어 있고 `RHEL810-base`, `RHEL810-app` 레포지토리가 구성된 서버에서 실행합니다.

```bash
# 1. 실행 권한 부여
chmod +x save_packages.sh install_packages.sh

# 2. 패키지 수집 및 tar.gz 압축본 생성
sudo ./save_packages.sh -a
```

#### 생성되는 산출물 구조
```text
rhel810_packages/
├── rpms/                       # 수집된 전체 RPM 패키지 파일 (의존성 포함)
│   ├── autofs-*.rpm
│   ├── nscd-*.rpm
│   ├── rpcbind-*.rpm
│   ├── nfs-utils-*.rpm
│   ├── tcsh-*.rpm
│   └── (기타 glibc, libxml2, libtirpc, hesiod, gssproxy 등 전체 의존성 RPMs)
├── package_manifest.txt        # 패키지 이름, 버전, 아키텍처 목록
└── install_packages.sh         # 대상 서버에서 즉시 실행할 수 있는 자동 필터링 설치 스크립트
```

---

### 2단계: 대상 서버(폐쇄망)로 번들 전송

저장된 압축본(`rhel810_packages.tar.gz`) 또는 폴더를 대상 서버로 전송합니다.

```bash
scp rhel810_packages.tar.gz root@<대상_서버_IP>:/tmp/
```

---

### 3단계: 대상 서버에서 오프라인 설치 실행

외부 네트워크 및 GPG 키가 없는 대상 서버에서 `install_packages.sh`를 실행합니다.

```bash
cd /tmp
tar -xzf rhel810_packages.tar.gz
cd rhel810_packages

# 패키지 자동 필터링 설치 및 서비스(rpcbind, autofs, nscd) 자동 시작
sudo ./install_packages.sh -s
```

실행 시 다음 작업이 자동으로 수행됩니다:
1. 번들 내 RPM 분석 및 호스트 충돌 유발 패키지 자동 필터링 (i686 제외, libxml2/glibc 호스트 버전 보존)
2. `nscd` 버전 잠금 감지 시 `--nodeps` 단독 선행 설치
3. `dnf --disablerepo='*' --nogpgcheck -y install` 로컬 오프라인 트랜잭션 설치
4. 5개 패키지 정상 설치 여부 전수 검증 (`rpm -q`)
5. `tcsh` 쉘 실행 검증
6. `-s` 지정 시 `rpcbind`, `autofs`, `nscd` 서비스 자동 등록 및 시작
7. 최종 설치 버전 및 서비스 상태 요약표 출력

---

## ⚙️ 상세 스크립트 옵션 안내

### `save_packages.sh`
```text
사용법: ./save_packages.sh [옵션]

옵션:
  -m, --mount <경로>      RHEL 8.10 ISO 마운트 디렉터리 (기본값: /mnt/rhel810)
  -o, --output <디렉터리>  패키지 저장 출력 디렉터리 (기본값: ./rhel810_packages)
  -a, --archive           저장 완료 후 .tar.gz 압축 파일 생성
  -h, --help              도움말 화면 출력
```

### `install_packages.sh` (또는 `install.sh`)
```text
사용법: ./install_packages.sh [옵션]

옵션:
  -d, --dir <경로>         RPM 파일들이 위치한 디렉터리 경로
                          (지정하지 않을 경우 ./rpms 또는 현재 디렉터리 자동 탐색)
  -s, --start-services    설치 완료 후 rpcbind, autofs, nscd 서비스를 즉시 enable 및 start
  -h, --help              도움말 화면 출력
```

---

## 📡 4. 기존 배포 스크립트 연동 및 원격 자동 배포 (`deploy_packages.sh`)

기존에 사용하시던 배포 스크립트에 포함하여 **대상 서버로 RPM 디렉터리와 install.sh를 scp 전송 후 원격 설치**할 수 있도록 전용 함수 `deploy_and_install_packages` 및 단독 배포 스크립트를 제공합니다.

* **SSH 포트**: `8022`
* **인증 방식**: `sshpass` (평문 비밀번호 변수 사용)

### 기존 스크립트에 함수로 삽입하는 방법
기존 배포 스크립트 상단에 아래 설정과 함수를 그대로 복사해 넣고, 배포 단계 직전에 호출하시면 됩니다:

```bash
# [설정]
SSH_PORT="8022"
SSH_USER="root"
SSH_PASS="대상서버비밀번호"  # 평문 저장

# [함수 호출 예시]
deploy_and_install_packages "192.168.1.50"
```

### 단독 실행 스크립트로 사용하는 방법
```bash
# 비밀번호 수정: deploy_packages.sh 내 SSH_PASS 변수 수정
vi deploy_packages.sh

# 단일 호스트 실행
chmod +x deploy_packages.sh
./deploy_packages.sh 192.168.1.10

# 호스트 목록 파일(hosts.txt)로 일괄 실행
./deploy_packages.sh -f hosts.txt
```
