# Changelog

## [0.1.1] - 2026-11-03

### 변경
- `deploy_repo.sh` 검증: VM에 이미 설치된 패키지(`rpm -q`)는 제외하고 미설치 패키지만 `dnf --assumeno`로 의존성 검증. 6개 모두 설치되어 있으면 검증을 건너뜀(repo 구성은 동일하게 진행).

## [0.1.0] - 2026-11-03

### 추가
- 최초 작성
- `build_bundle.sh`: RHEL 8.10 / 9.6 ISO(또는 압축 해제 디렉터리)에서 `autofs nscd rpcbind sssd tcsh nfs-utils`와 전체 의존성을 받아 repodata를 만들고 `localrepo-rhel<ver>.tar.gz` + `.sha256` 생성. ISO loop 마운트, 필수 도구(`dnf-plugins-core`, `createrepo_c`) 자동 설치, 종료 시 umount/임시 디렉터리 정리.
- `deploy_repo.sh`: 호스트 목록의 다수 VM에 병렬 배포(버전 감지, 번들 전송/해제, 기존 .repo 이동, local.repo 생성, subscription-manager 플러그인 비활성화, nsswitch.conf `sudoers: ldap files` 마지막 줄 설정), `dnf --assumeno` 검증, 호스트별 로그와 성공/실패 요약. 재실행 안전.
- 문서: README, ARCHITECTURE, PR_CHECKLIST, 사용법.txt, hosts.txt.example, CI(shellcheck, `bash -n`).

### 수정
- 의존성 해결을 `dnf download --resolve`에서 빈 installroot 기준 `dnf install --downloadonly`로 변경. 전자는 sssd-common의 조건부 의존성(`libsss_autofs`)을 놓침. weak deps는 제외(`install_weak_deps=0`).
