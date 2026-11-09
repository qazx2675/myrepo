# 계획서 — B서버에서 stress-test 실행 · 접속불가 무시 · 로그 창 색

## 배경
A서버(모든 망 접근 가능)에서 new_setup -run 을 돌린다. awxkit·git 업로드가 되는(stress-test 를 돌릴 수 있는) 곳은 B서버다.
그래서 CPU·stress-ng 실행(stress-test)은 B에서 해야 한다. 판정·CPU 감시(Probe)는 지금처럼 A에서 한다.
B에서 gossh 로 접속이 안 되는 호스트는 stress-test 가 접속불가로 빼 버려 CPU 테스트가 돌지 않는다.

## 결정 (인터뷰)
1. **B 실행 = ssh + 비밀번호**. 계정은 root 고정, 비밀번호는 코드에 하드코딩(`abc135!!`).
   공개 저장소에 노출되는 것은 사용자가 감수(2026-11-09 확인).
   비밀번호는 명령줄에 넣지 않고 환경변수로만 넘긴다(history·ps 에 안 보이게).
   방식: new_setup 바이너리 자신을 SSH_ASKPASS 로 지정(환경변수 표식이 있으면 비밀번호만 출력하고 끝).
   OpenSSH 8.0 호환을 위해 DISPLAY 가 비면 채우고, `SSH_ASKPASS_REQUIRE=force`, ssh 는 setsid(제어 터미널 없음)로 띄운다.
   설정 `STRESS_REMOTE=<B주소>`. 비면 지금처럼 A 로컬 실행.
2. **접속불가 무시 옵션**: stress-test `-ignore-offline` 플래그, new_setup 설정 `STRESS_IGNORE_OFFLINE=y` 면 넘긴다.
   켜면 B에서 접속체크에 응답 없는 호스트도 AWX 로 launch 한다. B 는 그 호스트의 부하를 못 재므로 감시에서 빼고,
   결과는 `실행(감시불가)`. 재실행 대상이 아니고 미실행(종료코드 1)으로 세지 않는다.
   DNS 실패 호스트는 지금처럼 제외.
3. **로그 창 색(-run TUI)**:
   - 명령 줄(`+ ` 로 시작): **굵은 청록**
   - 외부 프로그램 출력(워커 시각 머리말 없는 줄): 회색
   - 워커 진행 메시지: 기본색
   - 경고: 노랑. 오류·실패·시간초과: 빨강. 질문·답: 보라.
   - NoColor 면 색 없음, 폭 계산·자르기는 지금과 같게.

## 단계
1. stress-test: `-ignore-offline` 구현 + 단위 시험 + README.
2. setup_check: 원격 실행.
   - `STRESS_REMOTE` 설정, askpass 모드, 목록 파일을 B 의 `/tmp/new_setup_<user>/` 로 복사.
   - StressTest·YamlState 를 ssh 로 실행. stdin(r/x 답)·stdout 감시 표식(`[감시]`·`[등록]`·`[인벤토리사용중]`)은 지금과 같다.
   - 중지·시간초과 때는 원격 pid 에 SIGTERM 을 보낸다.
   - `STRESS_IGNORE_OFFLINE`, doctor·README 반영, 가짜 ssh 로 시험.
3. setup_check: TUI 로그 창 색 + 시험.
4. 랩(.58) 빌드·go test·run_tests·run_flow_tests 변경분, os8 바이너리 커밋.
   push 대상: master, setup-check, stress-test.
