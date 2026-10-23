# CHANGELOG

버전 태그는 이 파일의 항목과 연결합니다: `git tag -a vm-power-onoff-v0.1.0 -m "<요약> (CHANGELOG 2026-10-23 항목)"`

## 0.1.0 — 2026-10-23

최초 구현. 설계 결정 전체는 [계획서.md](계획서.md).

- 기능 4종: Power Off(Guest 종료 요청을 대상 전체에 한꺼번에 → 미종료 VM 은 `YeS` 확인 후 강제), Power On(순차: lpage BM 의 VM 은 **ev 단계별** — 모든 ev01 동시 PowerOn → 전부 부팅 대기 → 모든 ev02 동시 PowerOn …, 미사용 BM 은 안내 후 한 번에), 일괄 Power On(OS 설치용), 재부팅(Off 전체 동시 → 완료 확인 → On 전체 동시).
- **병렬 규칙**: 작업 집합 단위로 명령을 한꺼번에 보낸다(VM 하나씩 보내고 기다리지 않음). Power Off · 일괄 Power On · 재부팅은 대상 전체 동시(`concurrency` 기본 200은 상한). 순차 부팅은 2번 기능에만 있고, 그 안에서도 VM 단위가 아니라 **ev 단계(같은 ev 번호 전체) 단위**로 순차이며 단계 안은 병렬이다. 부팅 지연 질문은 단계당 한 번(c/s/q).
- 안전장치 5단계: 6자리 코드 → 대상 목록·총 대수 → Enter 대기 → 60초 VM 이름 입력(구분자 무관·순서 무관·중복/불일치 즉시 취소) → `YeS` → 10초 가로 카운트다운. 모든 단계 Ctrl+C 즉시 중단, 취소 사유 메시지, exit 1.
- VWP 검사: VM 의 모든 상위 폴더 경로·NIC 포트그룹 이름에 `vwp`(대소문자 무시) 포함 시 경고 배너 + 별도 6자리 확인 코드.
- 실행 제한: `/etc/vcetner_pwon_off/pwonoff.conf`(root 소유·group/other 쓰기 금지)의 `allowed_dir` 에서만 실행, `on_off_<user>.txt` 이외 파일 불가(심볼릭 링크 거부).
- vCenter 지정·인증은 VMsetup 방식: 번호 선택(0=직접, Enter=이전), `secret_lib.sh` 암호 파일 → 직접 입력, `VC_PASSWORD` 환경변수는 읽지 않음.
- 부팅 완료 판정: Tools 실행 + heartbeat green + IPv4 → 22 포트 직접 → 막힌 대역은 `os6_host` 경유 ssh(`/dev/tcp`).
- 감사 로그 `allowed_dir/run_<user>/`(비밀번호·확인 코드 미기록).
- 빌드: vendor 포함 오프라인 빌드(`setup.sh`), OS6(Go 1.20) 정적 빌드 `bin_os6/vm_power_onoff.gz`, `install.sh`.
- 수정: 비밀번호를 공용 stdin 리더로 읽도록 해 비밀번호 줄이 다음 메뉴 입력으로 새는 문제를 방지(E2E `user/vCenter/기능 번호 선택 + 비밀번호 유출 없음`).
- 검증: 단위 테스트(race 포함), vcsim 통합 테스트, 안전장치 시나리오(`--demo` 파이프 입력), vcsim 위 E2E 14 시나리오, OS6 바이너리 `--demo` 실행. 빌드 서버 192.168.0.58(Rocky 8, Go 1.26.5), OS6 빌드 192.168.0.60(Go 1.20.14).
- 한계: 실제 vCenter 에서는 아직 확인하지 못했다(vCenter 사용 불가). vcsim 은 Tools/heartbeat/IP 를 채우지 않아 부팅 완료 판정의 vCenter 쪽 값과 22 포트 경유는 가짜 구현·단위 테스트로만 검증했다.
