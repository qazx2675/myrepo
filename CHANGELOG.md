# CHANGELOG.md — 계정 수동 마운트 자동화 (account-mount)

> **버전 태그 및 원격 푸시 규칙**:
> ```bash
> git tag -a v0.1.0 -m "계정 수동 마운트 및 autofs 일괄 설정 도구 초기 배포 (CHANGELOG 2026-11-06 항목)"
> git push origin v0.1.0
> ```

## 2026-11-06 (v0.1.0)

- 계정 수동 마운트 자동화 도구(`account-mount-engine` 및 `run.sh`) 초기 구현.
  - `info.txt` (`계정 GID/UID 스토리지`, 줄바꿈 단위) 및 `hosts.txt` (대상 호스트 목록) 파싱 및 유효성 검증.
  - `/etc/auto.master`: `/user /etc/auto.home` 항목이 공백/탭과 무관하게 이미 존재하면 제외하고, 없으면 가장 아랫줄에 추가.
  - `/etc/passwd`: `계정:x:UID:GID:r계정:/user/계정:/bin/csh` 형식으로 기존 계정이 있으면 갱신, 없으면 맨 아래에 추가.
  - `/etc/group`: `계정:x:UID:` 형식으로 기존 그룹이 있으면 갱신, 없으면 맨 아래에 추가.
  - `/etc/auto.home`: `계정 -rw,hard,actimeo=1,proto=tcp,vers=3 스토리지/계정` 형식으로 기존 항목이 있으면 갱신, 없으면 맨 아래에 추가.
  - 변경 발생 시 `<파일>.bak.<STAMP>` 자동 백업 생성 및 `-list-backups` / `-rollback` / `-rollback-to` 복원 기능 지원.
  - `/etc/auto.master` 또는 `/etc/auto.home` 변경 시에만 `autofs` 서비스 자동 리로드/재시작 수행.
  - `home-test` 랩 환경(`192.168.0.58`, `192.168.0.60`)에서 `gossh` 원격 일괄 적용, 멱등성(NOCHANGE), 롤백 검증 완료.
