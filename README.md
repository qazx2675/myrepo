# RHEL 오프라인 로컬 repo 구성 도구

RHEL 8.10 / 9.6 VM 여러 대에 오프라인 로컬 yum(dnf) repo를 구성합니다. 10GB급 ISO 대신, 아래 6개 패키지와 전체 의존성만 담은 경량 번들(tar.gz)을 전송합니다.

```
yum install -y autofs nscd rpcbind sssd tcsh nfs-utils
```

> **주의 (참고용 도구)**
> 본 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.
> 이 도구는 설정을 변경하는 스크립트입니다. 적용 후에는 대상 서버 중 몇 대를 임의로 골라 설정(repolist, nsswitch.conf 마지막 줄, subscription-manager.conf)이 실제로 반영됐는지 직접 확인하십시오.

## 1. 빌드 및 설치 방법

### 코드 받기

```bash
# git으로 받기
git clone -b rhel-localrepo --single-branch https://github.com/qazx2675/myrepo.git rhel-localrepo
cd rhel-localrepo
```

또는 이 폴더 전체를 관리서버로 복사해도 됩니다. (파일은 모두 브랜치 루트에 있습니다.)

### 관리서버 요구사항

| 항목 | 내용 |
|---|---|
| OS / 권한 | RHEL 9.x, root |
| 필수 명령 | `sshpass`, `ssh`, `scp`, `sha256sum` (bash 4.3 이상) |
| ISO | RHEL 8.10 ISO, RHEL 9.6 ISO (BaseOS/AppStream 포함. 압축 해제한 디렉터리도 가능) |
| 기타 | `dnf download`(dnf-plugins-core), `createrepo_c`가 없으면 `build_bundle.sh`가 ISO repo에서 자동 설치 |

### 번들 생성

```bash
chmod +x build_bundle.sh deploy_repo.sh
./build_bundle.sh -8 /path/rhel-8.10-x86_64-dvd.iso -9 /path/rhel-9.6-x86_64-dvd.iso
ls -l bundles/
```

`bundles/` 아래에 `localrepo-rhel8.10.tar.gz`, `localrepo-rhel9.6.tar.gz`와 각각의 `.sha256` 파일이 생성됩니다. (tar 안의 최상위 디렉터리는 `rhel8.10`, `rhel9.6`)

## 2. 사용 방법

1. `hosts.txt.example`을 복사해 `hosts.txt`를 작성합니다. 한 줄에 IP 또는 hostname 하나이며, 예시 줄은 본인 환경 값으로 바꿉니다.
   ```bash
   cp hosts.txt.example hosts.txt
   vi hosts.txt
   ```
2. 배포를 실행합니다.
   ```bash
   ./deploy_repo.sh
   ```
3. 프롬프트에 대상 VM 공통 root SSH 비밀번호를 한 번 입력합니다.
4. 끝나면 출력되는 실행 요약(성공/실패 수, 실패 호스트와 사유)과 `logs/<host>.log`를 확인합니다.
5. 성공한 VM에서는 다른 스크립트가 `yum install -y autofs nscd rpcbind sssd tcsh nfs-utils`를 실행해도 로컬 repo만으로 의존성까지 설치됩니다.

## 3. 옵션별 상세 설명

### build_bundle.sh

| 옵션 | 설명 | 기본값 |
|---|---|---|
| `-8 <경로>` | RHEL 8.10 ISO 또는 압축 해제 디렉터리 | (없음) |
| `-9 <경로>` | RHEL 9.6 ISO 또는 압축 해제 디렉터리 | (없음) |
| `-o <디렉터리>` | 번들 출력 디렉터리 | `./bundles` |
| `-h` | 사용법 출력 | - |

- `-8`, `-9` 중 최소 하나는 필요하며 지정한 버전만 빌드합니다. (단, `deploy_repo.sh`는 두 번들이 모두 필요)
- 스크립트 내부 값(환경변수로 덮어쓰지 않음): 패키지 `autofs nscd rpcbind sssd tcsh nfs-utils`, 버전 `8.10`/`9.6`, GPG 키 `/etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-release`.

### deploy_repo.sh

| 옵션 | 대응 환경변수 | 설명 | 기본값 |
|---|---|---|---|
| `-f <파일>` | `HOSTS_FILE` | 대상 호스트 목록 파일 | `./hosts.txt` |
| `-b <디렉터리>` | `BUNDLE_DIR` | 번들 디렉터리 | `./bundles` |
| `-j <수>` | `PARALLEL` | 동시 처리 호스트 수 (1 이상 정수) | `10` |
| `-p <포트>` | `SSH_PORT` | SSH 포트 (숫자) | `8022` |
| `-h` | - | 사용법 출력 | - |

옵션 없이 환경변수로만 바꿀 수 있는 변수:

| 환경변수 | 설명 | 기본값 |
|---|---|---|
| `SSH_USER` | SSH 접속 계정 | `root` |
| `REPO_BASE` | VM에서 번들을 푸는 위치 (`$REPO_BASE/rhel<ver>`) | `/opt/localrepo` |
| `LOG_DIR` | 호스트별 로그 디렉터리 | `./logs` |
| `GPGKEY` | local.repo의 gpgkey 값 | `file:///etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-release` |

예: `PARALLEL=20 SSH_PORT=22 ./deploy_repo.sh -f mylist.txt` (옵션은 환경변수보다 우선)

## 4. 문서별 설명

| 파일 | 역할 |
|---|---|
| `build_bundle.sh` | ISO에서 패키지와 전체 의존성을 받아 repodata를 만들고 번들(tar.gz + sha256) 생성 |
| `deploy_repo.sh` | 번들을 VM에 전송하고 repo/설정을 구성한 뒤 검증, 요약 출력 |
| `SPEC.md` | 요구사항 명세 (인터뷰 확정본) |
| `README.md` | 이 문서 |
| `ARCHITECTURE.md` | 파일 역할 표와 스크립트 함수 흐름 |
| `PR_CHECKLIST.md` | 변경 시 확인할 체크리스트 |
| `CHANGELOG.md` | 변경 이력 |
| `사용법.txt` | 수동 실행용 명령 모음 |
| `hosts.txt.example` | 대상 호스트 목록 예시 |
| `.gitignore` | 번들, 로그, hosts.txt, ISO 제외 |
| `.github/workflows/ci.yml` | shellcheck, `bash -n` CI |

## 동작 상세 / 주의사항

### 각 VM에서 변경되는 것

- `/etc/yum.repos.d/*.repo`를 `/root`로 이동합니다. `/root`에 같은 이름이 있으면 기존 파일을 `.YYYYmmddHHMMSS`를 붙여 백업합니다. (`local.repo`는 제외)
- `/etc/yum.repos.d/local.repo` 생성 (baseurl `file:///opt/localrepo/rhel<ver>`, gpgcheck=1).
- subscription-manager 플러그인 `enabled=0` (RHEL 9: `/etc/dnf/plugins/subscription-manager.conf`, RHEL 8: `/etc/yum/pluginconf.d/subscription-manager.conf`). 변경이 있을 때만 `.bak.<시각>` 백업.
- `/etc/nsswitch.conf`: 기존 `sudoers:` 줄을 지우고 마지막 줄에 `sudoers: ldap files` 추가 (변경 시 `.bak.<시각>` 백업).
- `/opt/localrepo/rhel<ver>`에 번들 압축 해제.
- 재실행해도 안전합니다(멱등).

### 의존성 해결

빈 installroot 기준으로 `dnf install --downloadonly`를 실행해 전체 클로저를 받습니다. `dnf download --resolve`는 sssd-common의 조건부 의존성을 놓치므로 사용하지 않습니다. weak deps는 제외합니다(`install_weak_deps=0`).

### 중요한 전제

대상 VM이 같은 ISO(8.10/9.6 GA)로 설치되었고 그 이후 별도 업데이트가 없어야 합니다. VM의 glibc 등이 ISO보다 새 버전이면 nscd(glibc와 정확히 같은 버전 요구) 등이 충돌해 설치가 실패할 수 있습니다. 이 경우 deploy의 검증 단계(`dnf --assumeno`)에서 FAIL로 드러납니다.

### 기타 주의

- `authselect select`를 나중에 실행하면 nsswitch.conf가 재생성되어 `sudoers` 줄이 사라질 수 있습니다.
- `deploy_repo.sh`는 8.10/9.6 번들이 둘 다 `bundles/`에 있어야 시작합니다.
- 비밀번호는 실행 시 1회 입력받아 `sshpass -e`(환경변수)로 전달합니다.

### 검증 상태

랩(Rocky 8.10/9.6 컨테이너, 가짜 ISO)에서 다음을 확인했습니다: 번들 생성(EL9 컨테이너에서, ISO 루프 마운트와 필수도구 자동설치 포함) -> 4대 배포 성공 / 가짜 호스트 실패 보고 -> 재실행 동일 결과 -> 네트워크 차단 상태에서 실제 `yum install` 성공. 실제 RHEL ISO(모듈 메타데이터, redhat-release 키)로는 미검증입니다.
