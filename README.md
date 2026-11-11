# RHEL 8.10 필수 패키지 오프라인 저장 및 폐쇄망 설치 도구

RHEL 8.10 설치 이미지(`/mnt/rhel810`) 및 로컬 yum 레포지토리(`RHEL810-base`, `RHEL810-app`)로부터 필수 시스템 패키지(`nscd`, `autofs`, `rpcbind`, `nfs-utils`, `tcsh`)와 하위 의존성 전체를 추출하여 번들로 저장하고, 외부 네트워크 및 GPG 키가 없는 폐쇄망 환경의 다른 RHEL 8 서버에 안전하게 일괄 설치하는 2종 스크립트 모음입니다.

---

## 📌 주요 특징

- **폐쇄망 최적화 (GPG 검증 비활성화)**:
  - 폐쇄망 환경에서는 Red Hat GPG 공개키가 없거나 갱신되지 않는 경우가 대부분이므로, 모든 수집 및 설치 과정에서 `--nogpgcheck` 및 `gpgcheck=0`을 기본 적용하여 서명 관련 오류를 원천 차단합니다.
  - 불필요한 체크섬 생성/검증 단계를 제거하여 폐쇄망 작업 흐름을 간소화했습니다.
- **기존 동작 환경 직결 연동 (`save_packages.sh`)**:
  - 현재 시스템에서 `yum install`이 정상 동작하는 `RHEL810-base`, `RHEL810-app` 레포지토리 및 `/mnt/rhel810` 마운트 미디어를 직접 연결하여 패키지를 수집합니다.
  - `repotrack`, `dnf download --resolve --alldeps`, 또는 마운트 미디어 직접 복사를 통해 **최소 설치(Minimal Install) 대상 서버에서도 설치 가능한 의존성 RPM 목록**을 자동으로 확보합니다.
- **안전한 폐쇄망 오프라인 설치 (`install_packages.sh`)**:
  - 외부 레포지토리 연결을 차단(`--disablerepo='*'`)하고 오직 전달받은 로컬 RPM만을 사용하여 트랜잭션을 해결합니다.
  - 5개 패키지 설치 여부 전수 검증 및 서비스(`rpcbind`, `autofs`, `nscd`) 자동 활성화/실행을 지원합니다.

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
├── rpms/                       # 수집된 모든 RPM 패키지 파일 (의존성 포함)
│   ├── autofs-*.rpm
│   ├── nscd-*.rpm
│   ├── rpcbind-*.rpm
│   ├── nfs-utils-*.rpm
│   ├── tcsh-*.rpm
│   └── (기타 libtirpc, hesiod, gssproxy 등 의존성 RPMs)
├── package_manifest.txt        # 패키지 이름, 버전, 아키텍처 목록
└── install_packages.sh         # 대상 서버에서 즉시 실행할 수 있는 설치 스크립트
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

# 패키지 무결격 설치 및 서비스(rpcbind, autofs, nscd) 자동 시작
sudo ./install_packages.sh -s
```

실행 시 다음 작업이 자동으로 수행됩니다:
1. `dnf --disablerepo='*' --nogpgcheck -y install rpms/*.rpm` 로컬 오프라인 설치
2. 5개 패키지 정상 설치 여부 전수 검증 (`rpm -q`)
3. `tcsh` 쉘 실행 검증
4. `-s` 지정 시 `rpcbind`, `autofs`, `nscd` 서비스 자동 등록 및 시작
5. 최종 설치 버전 및 서비스 상태 요약표 출력

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

### `install_packages.sh`
```text
사용법: ./install_packages.sh [옵션]

옵션:
  -d, --dir <경로>         RPM 파일들이 위치한 디렉터리 경로
                          (지정하지 않을 경우 ./rpms 또는 현재 디렉터리 자동 탐색)
  -s, --start-services    설치 완료 후 rpcbind, autofs, nscd 서비스를 즉시 enable 및 start
  -h, --help              도움말 화면 출력
```
