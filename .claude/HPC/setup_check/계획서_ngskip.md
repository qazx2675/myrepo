# 계획서 — stress-ng 만 건너뛰기 (k)

## 문제
`-run` 의 stress-ng 종료 시각 질문(askEnd)은 c(계속) / s(중지) 두 개뿐이고, s 는 `w.stop()` 으로 작업 전체를 멈춘다.
부하 단계에서는 이 질문이 CPU launch **앞**에 있으므로, stress-ng 를 안 하려고 s 를 누르면 CPU 테스트도 뜨지 않는다.
템플릿(CPU / stress-ng)마다 따로 멈출 수 있어야 한다.

## 결정 (인터뷰)
1. 종료 시각 질문에 **k = stress-ng 만 건너뜀** 을 추가한다. c 계속 / k 건너뜀 / s 전체 중지.
2. 부하 단계에서 k → 이번 stress-ng 대상은 띄우지 않고 CPU 는 그대로 launch·감시한다.
   건너뛴 호스트는 상태(`ng_skip`)에 기록하고, **다음 stress-ng 단계에서 다시 묻는다**(ngTargets 에 포함).
3. stress-ng 단계에서 k → stress-ng 를 돌리지 않는다. stress-ng 는 반드시 돌아야 하므로 열은 O 가 아닌 채(재판정 결과 그대로) 남고,
   단계 끝 질문을 한 번 더 띄운다(O 아닌 호스트가 있으니 r 다시 / x 제외하고 계속 / s 중지).
4. `-auto` 는 종료 시각을 자동으로 정하므로 변경 없음. 인벤토리 사용 중 질문(r/x)은 이미 묶음별이라 변경 없음.

## 변경
- state.go: `ansSkip = "k"`, runState 에 `NGSkip []string` (`json:"ng_skip,omitempty"`).
- worker.go:
  - askEnd: 선택지에 ansSkip 추가. 반환을 (end, result) 로: 계속 / 건너뜀 / 중지(stop() 호출).
  - doLoad: 건너뜀이면 stress 목록을 비우고 NGSkip 에 더한 뒤 CPU 만 진행.
  - doNG: 건너뜀이면 아무것도 띄우지 않고 "" 반환(재판정 → 단계 끝 질문). 실행하면 그 호스트를 NGSkip 에서 뺀다.
  - ngTargets: `(CPUTarget || NGSkip 에 있음) && CPU O && Stress != O`.
  - choiceLabel: k → "stress-ng 만 건너뜀(CPU 는 그대로)" / 단계에 맞는 문구.
  - 질문 문구에 k 안내 한 줄.
- README 해당 절, worker 테스트(부하 단계 k → CPU launch·stress-ng 미실행·다음 NG 단계 대상 포함 / NG 단계 k → 단계 끝 질문에 c 없음).
- 랩(.58) 빌드·go test·test/run_tests 변경분만, os8 바이너리 커밋, master·setup-check·(stress-test 는 변경 없음) push.
