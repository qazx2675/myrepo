# CHANGELOG

`vc_password_update`의 변경 사항을 날짜순(최신이 위)으로 기록합니다.

---

## 2026-09-27 — 최초 작성 (v0.1.0)

- `main.go`: `-dir`/`-vc`/`-id`/`-adminId`/`-timeout` 플래그. `secret_lib.sh`와 동일한
  openssl 파라미터로 대상 계정 비밀번호 복호화, `govmomi/ssoadmin`의
  `ResetPersonPassword`로 admin 강제 재설정.
- `run.sh`(수동 실행 편의) / `cron_wrapper.sh`(crontab 진입점, 85일 경과 체크) /
  `setup.sh`(폐쇄망 오프라인 빌드) 추가.
- 공유 `govendor/govmomi-0.55.1-vc-password-update` 신규 생성(기존 govmomi vendor
  사본에는 `ssoadmin`/`sts` 패키지가 없어 이 프로젝트 전용으로 분리).
- 랩 vCenter(192.168.0.50, 8.0.3)로 실측 검증:
  - **핵심 가정 검증**: 비밀번호 이력 정책(`ProhibitedPreviousPasswordsCount=5`)이 있는
    상태에서 같은 값으로 연속 두 번 admin 재설정해도 둘 다 성공 — "같은 비밀번호로
    admin 재설정 = 만료 타이머만 리셋" 설계 확인.
  - 응답 없는 vCenter를 목록에 섞어도 `-timeout`(기본 30초) 이후 해당 항목만 실패
    처리하고 나머지는 계속 처리됨을 확인 (최초 구현은 타임아웃이 없어 무한 대기했던
    문제를 `context.WithTimeout`으로 수정).
  - `run.sh`, `cron_wrapper.sh`(최초 실행 → 갱신 → 즉시 재실행 시 스킵) end-to-end 확인.
  - `GOPROXY=off` + 빈 모듈 캐시 없이도 vendor 심볼릭 링크 기반 오프라인 빌드 성공.
- 검증 중 랩 vCenter 시계가 실제 시각보다 약 44분 느려 STS 토큰 발급이
  `MessageExpired`로 실패하는 것을 발견 — vCenter 시계를 동기화해 해결(README §5
  "알려진 한계"에 기록. 운영 환경에서도 NTP 동기화 여부를 먼저 확인할 것).
