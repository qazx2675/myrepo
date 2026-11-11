# RHEL 8.10 필수 패키지 오프라인 저장 및 폐쇄망 설치 도구

RHEL 8.10 설치 이미지(`/mnt/rhel810`)로부터 필수 시스템 패키지(`nscd`, `autofs`, `rpcbind`, `nfs-utils`, `tcsh`)와 하위 의존성 전체를 추출하여 번들로 저장하고, 외부 네트워크가 차단된 폐쇄망 환경의 다른 RHEL 8 서버에 안전하게 일괄 설치하는 2종 스크립트 모음입니다.

---

## 📌 주요 특징

- **호환성 및 의존성 수집 (`save_packages.sh`)**:
  - 시스템에 이미 등록된 `RHEL810-base`, `RHEL810-app` 레포지토리 및 `/mnt/rhel810` 마운트 미디어를 완벽하게 감지합니다.
  - 임시 설정 파일이나 환경 간섭 없이 `repotrack`, `dnf download --resolve --alldeps`, 또는 마운트 미디어 직접 수집을 통해 **최소 설치(Minimal Install) 상태의 대상 서버에서도 설치 가능한 완전한 의존성 RPM 목록**을 자동으로 수집합니다.
- **안전한 폐쇄망 오프라인 설치 (`install_packages.sh`)**:
  - 외부 저장소나 Red Hat Subscription Manager가 차단된 폐쇄망 서버에서 발생할 수 있는 네트워크 타임아웃을 방지하기 위해 `--disablerepo='*'` 플래그를 기본 적용합니다.
  - SHA256 체크섬 무결성 검증, 패키지 설치 여부 전수 검증, 서비스 활성화(`systemctl enable --now`) 및 요약 상태표 출력을 지원합니다.
- **포터블 번들링**:
  - `save_packages.sh` 실행 시 저장 디렉터리 내에 `install_packages.sh`, `checksums.sha256`, `package_manifest.txt`가 함께 복사되므로 해당 디렉터리(또는 `.tar.gz`) 하나만 대상 서버로 복사하면 즉시 설치 가능합니다.

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

RHEL 8.10 ISO 이미지가 마운트된 서버(기본 경로 `/mnt/rhel810`)에서 `save_packages.sh`를 실행합니다.

```bash
# 1. 실행 권한 부여
chmod +x save_packages.sh install_packages.sh

# 2. 패키지 수집 실행 (기본 마운트: /mnt/rhel810, 출력: ./rhel810_packages)
sudo ./save_packages.sh

# 옵션 사용 예시 (마운트 경로 및 출력 경로 지정, 압축 tar.gz 번들 동시 생성)
sudo ./save_packages.sh -m /mnt/rhel810 -o ./my_rhel8_bundle -a
```

#### 생성되는 산출물 구조
```text
rhel810_packages/
├── rpms/                       # 다운로드된 모든 RPM 패키지 파일 (의존성 포함)
│   ├── autofs-*.rpm
│   ├── nscd-*.rpm
│   ├── rpcbind-*.rpm
│   ├── nfs-utils-*.rpm
│   ├── tcsh-*.rpm
│   └── (기타 libtirpc, hesiod, gssproxy 등 의존성 RPMs)
├── checksums.sha256            # RPM 파일 무결성 검증용 SHA256 체크섬
├── package_manifest.txt        # 패키지 이름, 버전, 아키텍처 매니페스트
└── install_packages.sh         # 대상 서버에서 즉시 실행할 수 있는 설치 스크립트
```

---

### 2단계: 대상 서버(폐쇄망)로 번들 전송

저장된 폴더(또는 `-a` 옵션으로 생성된 `.tar.gz` 파일)를 대상 서버로 전송합니다.

```bash
# SCP로 폴더 전송 예시:
scp -r ./rhel810_packages root@<대상_서버_IP>:/tmp/

# 또는 tar.gz 압축본 전송 예시:
scp ./rhel810_packages.tar.gz root@<대상_서버_IP>:/tmp/
```

---

### 3단계: 대상 서버에서 오프라인 설치 실행

외부 네트워크가 차단된 대상 서버에서 `install_packages.sh`를 실행합니다.

```bash
# tar.gz 압축본으로 전달받은 경우 압축 해제
cd /tmp
tar -xzf rhel810_packages.tar.gz
cd rhel810_packages

# 패키지 설치 및 서비스(rpcbind, autofs, nscd) 자동 시작
sudo ./install_packages.sh -s
```

실행 시 다음 작업이 자동으로 수행됩니다:
1. `checksums.sha256` 기반의 RPM 무결성 검증
2. `dnf --disablerepo='*' -y install rpms/*.rpm` 실행을 통한 로컬 오프라인 설치
3. 5개 패키지 정상 설치 여부 전수 검증 (`rpm -q`)
4. `tcsh` 쉘 실행 검증
5. `-s` 지정 시 `rpcbind`, `autofs`, `nscd` 서비스 자동 등록 및 시작
6. 최종 설치 버전 및 서비스 상태 요약표 출력

---

## ⚙️ 상세 스크립트 옵션 안내

### `save_packages.sh`
```text
사용법: ./save_packages.sh [옵션]

옵션:
  -m, --mount <경로>      RHEL 8.10 ISO 마운트 디렉터리 (기본값: /mnt/rhel810)
  -o, --output <디렉터리>  패키지 저장 출력 디렉터리 (기본값: ./rhel810_packages)
  -a, --archive           저장 완료 후 .tar.gz 압축 파일 및 .sha256 체크섬 생성
  -h, --help              도움말 화면 출력
```

### `install_packages.sh`
```text
사용법: ./install_packages.sh [옵션]

옵션:
  -d, --dir <경로>         RPM 파일들이 위치한 디렉터리 경로
                          (지정하지 않을 경우 ./rpms 또는 현재 디렉터리 자동 탐색)
  -s, --start-services    설치 완료 후 rpcbind, autofs, nscd 서비스를 즉시 enable 및 start
  --skip-check            SHA256 체크섬 무결성 검증 건너뛰기
  -h, --help              도움말 화면 출력
```

---

## 🔍 수동 검증 및 점검 명령어

설치 완료 후 시스템 상태를 수동으로 점검하려면 아래 명령어를 사용할 수 있습니다:

```bash
# 1. 패키지 설치 확인
rpm -q nscd autofs rpcbind nfs-utils tcsh

# 2. 서비스 실행 상태 확인
systemctl status rpcbind autofs nscd

# 3. tcsh 쉘 동작 확인
tcsh -c "echo 'tcsh shell test OK'"

# 4. NFS 마운트 도구 확인
which mount.nfs
```

