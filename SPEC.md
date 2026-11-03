# RHEL 오프라인 로컬 repo 구성 — 요구사항 명세 (인터뷰 확정본)

## 목표
대상 VM에 로컬 repo를 구성하여, 다른 스크립트가 아래 명령을 실행하면 의존성까지 모두 설치되도록 한다.
`yum install -y autofs nscd rpcbind sssd tcsh nfs-utils`
- 이 작업 범위에 실제 설치(yum install)는 포함하지 않는다. 검증은 `dnf --assumeno`로 의존성 해결 여부만 확인한다.
- 10GB ISO를 통째로 전송하지 않는다. 6개 패키지와 전체 의존성만 담은 경량 번들(tar)을 전송한다.

## 환경
| 항목 | 값 |
|---|---|
| 관리서버 | RHEL 9.x. 8.10 ISO와 9.6 ISO를 보유. createrepo_c는 없거나 설치 여부를 모름 |
| 대상 VM | RHEL 8.10 또는 RHEL 9.6. 마이너 버전은 이 둘로만 통일되어 있음 |
| 접속 | root 계정으로 직접 접속, SSH 포트 8022, sshpass 사용 가능 |
| 패스워드 | 실행할 때 `read -s -p`로 1회 입력받음. 모든 VM에 공통으로 쓰며, `sshpass -e`로 넘겨 ps 출력에 노출되지 않게 함 |
| 아키텍처 | x86_64로 가정 |

## 스크립트 구성 (bash)
### 1) build_bundle.sh — 관리서버에서 1회 실행
- ISO 경로는 변수나 인자로 지정한다. 예: `ISO_RHEL8=...`, `ISO_RHEL9=...`
- ISO를 loop 마운트한 뒤 BaseOS와 AppStream을 `--repofrompath`로 등록한다.
- createrepo_c가 없으면 9.6 ISO의 repo에서 자동으로 설치한다.
- 버전별로 빈 installroot와 `--releasever`를 지정하고 `dnf download --resolve --alldeps` 6개 패키지를 받는다. 이렇게 하면 전체 의존성 클로저가 생성된다.
- `createrepo_c`로 repodata를 생성한 뒤 `localrepo-rhel8.10.tar.gz`와 `localrepo-rhel9.6.tar.gz`를 만든다.
- 마지막에 ISO를 umount하고 임시 디렉터리를 정리한다.

### 2) deploy_repo.sh — 관리서버에서 실행, 다수 VM을 대상으로 함
- 대상 목록 파일에는 한 줄에 IP 또는 hostname 하나를 적는다. `#`로 시작하는 주석 줄과 빈 줄은 무시한다.
- 여러 VM을 **병렬**로 처리한다. 동시 실행 개수는 변수로 조정할 수 있게 한다.
- 호스트별 로그를 `logs/<host>.log`에 남기고, 끝나면 성공/실패 **요약**을 출력한다.
- **멱등성**: 같은 VM에 다시 실행해도 파일이 중복되거나 설정이 깨지지 않아야 한다.
- 각 대상 VM에서 수행하는 단계는 다음과 같다.
  1. `/etc/redhat-release`를 읽어 8.10인지 9.6인지 감지한다. 둘 다 아니면 해당 호스트는 실패로 처리한다.
  2. 버전에 맞는 번들을 scp(-P 8022)로 전송한 뒤 `/opt/localrepo/rhel<ver>`에 압축을 푼다.
  3. `/etc/yum.repos.d/*.repo`를 모두 `/root`로 옮긴다. `/root`에 같은 이름의 파일이 있으면 기존 파일에 `.YYYYmmddHHMMSS`를 붙여 백업한다. 우리가 만든 local.repo는 이동 대상에서 제외한다.
  4. `/etc/yum.repos.d/local.repo`를 생성한다. baseurl은 `file:///opt/localrepo/rhel<ver>`, gpgcheck=1, gpgkey는 RPM-GPG-KEY-redhat-release로 한다.
  5. subscription-manager 플러그인을 비활성화한다(`enabled=0`).
     - RHEL 9: `/etc/dnf/plugins/subscription-manager.conf`
     - RHEL 8: `/etc/yum/pluginconf.d/subscription-manager.conf`. 이 경로가 `/etc/dnf/plugins`와 symlink일 수 있으므로 실제로 존재하는 경로를 처리한다.
  6. `/etc/nsswitch.conf`: 수정 전에 백업한다. 기존 `sudoers:` 줄을 모두 지우고 **맨 마지막 줄**에 `sudoers: ldap files`를 넣는다.
  7. 검증한다.
     - `dnf clean all` 실행 후 `dnf repolist`에 local repo만 나오는지 확인한다.
     - `dnf --assumeno install autofs nscd rpcbind sssd tcsh nfs-utils`로 의존성이 모두 해결되는지 확인한다.
     - nsswitch.conf의 마지막 줄을 확인한다.

## 참고/위험
- RHEL 8/9에서는 authselect가 nsswitch.conf를 다시 생성할 수 있다. 이후 다른 스크립트가 `authselect select ...`를 실행하면 sudoers 줄이 사라질 수 있다. authselect 프로파일에 반영하는 작업은 이번 범위에서 제외한다(사용자 결정).
- 번들 크기는 버전별로 수백 MB 수준으로 예상한다(전체 클로저).
