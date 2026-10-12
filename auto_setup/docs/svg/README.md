# docs/svg — 명령·키별 흐름도

`auto_setup` 의 CLI 명령과 TUI 키(메뉴 항목)마다 흐름도 SVG 를 하나씩 둡니다. 각 그림은 **코드에서 확인한 실제 단계**이며 맨 아래에 구현 위치(파일·함수)가 적혀 있습니다. 브라우저로 열면 라이트/다크 설정을 따릅니다 (`prefers-color-scheme`, 양식은 `../../workflow.svg` 와 같음).

- 전체 흐름: [`../../workflow.svg`](../../workflow.svg) · [`../../WORKFLOW.md`](../../WORKFLOW.md)
- 사용법·옵션: [`../../README.md`](../../README.md) · 코드 구조: [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md)

**색 범례** (그림 맨 아래에도 있음): 파랑 = auto_setup 동작 · 주황 = 판정·키 입력·확인 · 보라 = 외부 호출(gossh·ssh·01·엔진·os_check) · 초록 = 기록·결과 · 회색 = 시작·종료·참고(점선).

## CLI 명령

| 파일 | 명령 | 설명 |
|---|---|---|
| [`cli_report.svg`](cli_report.svg) | `auto_setup` · `--plain` · `--admin` · `-user <이름>` | 옵션 해석 → 데이터 원본 선택 → tty 면 TUI, 아니면 텍스트 표 |
| [`cli_daemon_ctl.svg`](cli_daemon_ctl.svg) | `--start` · `--stop` · `--restart` (`start`·`stop`·`restart`) | os8_mgmt 에서만. pid 로 확인 → 종료(SIGTERM 10초) / 기동(setsid, 5초 확인) |
| [`cli_status.svg`](cli_status.svg) | `status` | jobs 를 읽어 작업별 단계 대수·남은 호스트 출력 (원격이면 중계) |
| [`cli_snapshot.svg`](cli_snapshot.svg) | `snapshot` | 상태 스냅샷 JSON 출력 (TUI·원격 클라이언트가 쓰는 내용) |
| [`cli_code.svg`](cli_code.svg) | `code NNNN` | `codes/NNNN.txt` 결과 코멘트 출력 |
| [`cli_cancel.svg`](cli_cancel.svg) | `cancel <jobid>` | `jobs/<id>.json` 을 `jobs/done/` 으로 옮겨 감시 종료 |
| [`cli_done.svg`](cli_done.svg) | `done <host...>` · `done --job <jobid> [--yml <그룹>]` | `done/<host>` 에 manual 기록 → 데몬이 5초 안에 수거해 완료 처리 |
| [`cli_request_run.svg`](cli_request_run.svg) | `request manual-run <jobid> <yml> [check]` | 수동 OS 체크 요청 → 데몬 수락·실행 (check 는 설정체크만) |
| [`cli_request_recheck.svg`](cli_request_recheck.svg) | `request recheck <jobid> <yml\|*>` | os8 → os6_mgmt 순으로 직접 재확인 (TUI `g`) |
| [`cli_request_other.svg`](cli_request_other.svg) | `request cancel <jobid>` · `request refresh` | 요청 파일만 남기고 데몬이 5초 안에 처리 |
| [`cli_newhost.svg`](cli_newhost.svg) | `newhost [-user <이름>]` | 신규 설치 양식 5단계 → `awx_dir/<user>.txt` 저장 → AWX 실행? |
| [`cli_s.svg`](cli_s.svg) | `s [-user <이름>]` | gossh 작업 기록 화면 (TUI `s` 와 같음) |
| [`cli_probe.svg`](cli_probe.svg) | `probe [-i 10s]` | os6_mgmt 쪽 ping 감시 (데몬이 ssh 장기 세션으로 호출) |
| [`cli_ensure_daemon.svg`](cli_ensure_daemon.svg) | `ensure` · `daemon` | cron 매분 ensure → flock → 데몬 기동, 데몬의 5초 step 루프 |

내부용 `work-relay`(os6_mgmt 의 gossh 작업 기록을 os8_mgmt 에 저장·조회)는 사용자가 직접 부르지 않으므로 [`cli_s.svg`](cli_s.svg) 에 한 줄로만 나옵니다.

## TUI 키

| 파일 | 키 | 화면 | 설명 |
|---|---|---|---|
| [`tui_r.svg`](tui_r.svg) | `r` | 공통 | 새로고침 + 데몬에 즉시 ping·준비확인 요청 (5초 연타 방지) |
| [`tui_f.svg`](tui_f.svg) | `f` | 2 | 정체·실패 호스트만 보기 (토글) |
| [`tui_a.svg`](tui_a.svg) | `a` | 1·2 | 한 job 의 모든 그룹을 한 표로 / 전체 보기에서 화면 1 로 |
| [`tui_pgupdn.svg`](tui_pgupdn.svg) | `PgUp` `PgDn` | 공통 | 한 화면(높이−8행)씩 이동·스크롤 |
| [`tui_help.svg`](tui_help.svg) | `?` | 공통 | 도움말 (아무 키나 누르면 닫힘) |
| [`tui_h_menu.svg`](tui_h_menu.svg) | `h` | 2 | 호스트 작업 인라인 메뉴 (c t g d v, 번호 1~5) |
| [`tui_h_c.svg`](tui_h_c.svg) | `h` → `c` (또는 `c`) | 2 | 체크+수정: 설정체크 + 설정수정 수동 실행 |
| [`tui_h_t.svg`](tui_h_t.svg) | `h` → `t` (또는 `t`) | 2 | 체크만: 설정체크만 (환경설정 수정 안 함) |
| [`tui_h_g.svg`](tui_h_g.svg) | `h` → `g` (또는 `g`) | 2 | 재확인: os8 → os6_mgmt → 접속불가, 진행 창 |
| [`tui_h_d.svg`](tui_h_d.svg) | `h` → `d` (또는 `d`) | 2 | 수동 완료 (호스트 / 그룹 미완료 전체, y 확인) |
| [`tui_h_v.svg`](tui_h_v.svg) | `h` → `v` (또는 `v`) | 2 | 이 그룹의 가장 최근 수동 실행 결과 다시 보기 |
| [`tui_w_menu.svg`](tui_w_menu.svg) | `w` | 공통 | AWX 인라인 메뉴 (1 AWX 실행 · 2 신규 설치 양식) |
| [`tui_w_1.svg`](tui_w_1.svg) | `w` → `1` (또는 `ww` · `w` Enter) | 공통 | AWX 실행: 프로파일 → user → 대상 목록 → 01 실행 |
| [`tui_w_2.svg`](tui_w_2.svg) | `w` → `2` | 공통 | 신규 설치 양식 (CLI `newhost` 와 같은 화면) |
| [`tui_m.svg`](tui_m.svg) | `m` | 공통 | 변경 작업 (IP·LDAP 변경·원복·auto.appl 원복) |
| [`tui_s.svg`](tui_s.svg) | `s` | 공통 | gossh 작업 기록 (메뉴 없이 바로 열림) |
| [`tui_x.svg`](tui_x.svg) | `x` | 공통 | 체크스크립트 단독 실행 (기록 안 남김) |

`c` `t` `g` `v` `d` `x` 는 예전 단축키 그대로 눌러도 동작하는 **숨은 단축키**입니다 (`h` 메뉴는 같은 핸들러를 호출). 화면 1 의 `Enter`(그룹 → 호스트표)·`←` `→`(작업·날짜 이동)·`↑` `↓`·`q` 는 단순 이동이라 그림이 없습니다.

## 고칠 때

- 그림은 손으로 고쳐도 되지만 `<style>` 의 CSS 변수 블록(라이트·다크)은 그대로 두십시오 (색을 직접 쓰면 다크 모드에서 안 보일 수 있음).
- 흐름이 바뀌면 같은 PR 에서 해당 SVG 의 단계와 맨 아래 "코드:" 줄도 함께 고칩니다 (`PR_CHECKLIST.md` 의 SVG 항목).
- 검증: `python3 -c "import xml.dom.minidom,sys; [xml.dom.minidom.parse(f) for f in sys.argv[1:]]" *.svg` (또는 `xmllint --noout *.svg`) 이 오류 없이 끝나야 합니다.
