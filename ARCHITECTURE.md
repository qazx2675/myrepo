# ARCHITECTURE

## 파일별 역할

| 파일 | 역할 |
|---|---|
| `build_bundle.sh` | 관리서버에서 1회 실행. ISO -> 패키지 + 의존성 -> repodata -> `bundles/*.tar.gz` |
| `deploy_repo.sh` | 관리서버에서 실행. 다수 VM에 병렬로 번들 전송, repo 구성, 검증 |
| `bundles/` | 생성된 번들과 sha256 (git 제외) |
| `logs/` | 호스트별 로그 (git 제외) |
| `hosts.txt` | 대상 호스트 목록 (git 제외, `hosts.txt.example` 참고) |

## build_bundle.sh 흐름

`main` -> 인자 파싱(`-8 -9 -o`), root 확인, 작업 디렉터리 생성, `trap cleanup EXIT`

1. `prepare_src` : ISO를 loop 마운트(또는 디렉터리 사용), BaseOS/AppStream repodata 확인, `check_version`으로 버전 문자열 경고
2. `ensure_prereqs` : `have_prereqs` 확인 후 부족하면 `dnf-plugins-core`/`createrepo_c`를 ISO repo에서 설치
3. `build_one` (버전별)
   - `download_pkgs` : 빈 installroot에서 `dnf install --downloadonly`로 전체 클로저 다운로드
   - `verify_pkgs` : 6개 패키지 누락 확인
   - `pack_bundle` : `createrepo_c` -> tar.gz -> `.sha256`
4. `cleanup` : umount, 임시 디렉터리 삭제

보조: `log`, `warn`, `die`, `usage`

## deploy_repo.sh 흐름

`main` -> `parse_args` -> `check_prereq` -> `load_hosts` -> `check_bundles` -> `ask_password` -> `run_all` -> `print_summary`

- `run_all` : `PARALLEL` 개수만큼 `host_job`을 백그라운드로 실행, 로그는 `logs/<host>.log`
- `host_job` / `host_flow` (호스트별)
  1. `/etc/redhat-release`로 버전 감지 (8.10, 9.6 외에는 실패)
  2. `rscp`로 번들 전송 -> `REMOTE_EXTRACT` (`/opt/localrepo/rhel<ver>`에 압축 해제)
  3. `REMOTE_CONFIG` 실행
     - `move_old_repos` -> `write_local_repo` -> `disable_subscription_plugin`(`disable_plugin_file`) -> `fix_nsswitch`
     - 검증: `verify_repolist` -> `verify_resolve`(이미 설치된 패키지 제외 후 `dnf --assumeno`) -> `verify_nsswitch`
  4. 결과를 `logs/.results/<host>`에 `OK|...` 또는 `FAIL|사유`로 기록
- `print_summary` : 성공/실패 수와 실패 사유 출력, 실패가 있으면 종료코드 1

원격 호출 래퍼: `rssh`, `rscp`, `remote_bash` (비밀번호는 `SSHPASS` 환경변수 + `sshpass -e`)
