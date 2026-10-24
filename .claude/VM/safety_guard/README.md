# safetyguard — 위험 작업 앞에 붙이는 안전장치 5단계 (Go, 표준 라이브러리만 사용)

`vm_power_onoff` 에서 만든 휴먼 에러 방지 안전장치를 다른 Go 도구에서도 쓰도록 따로 분리한 패키지입니다.
외부 의존성이 없어 폐쇄망·OS6(Go 1.20)에서도 폴더째 복사해 빌드할 수 있습니다.

## 1. 빌드·시험

```bash
cd safety_guard
go vet ./... && go test -race ./...          # 단위 시험
go build -o guarddemo ./cmd/guarddemo        # 체험용 실행 파일
bash test/guard_scenarios.sh ./guarddemo     # 파이프 입력 시나리오 12개 → PASS=12 FAIL=0
```

## 2. 사용 방법

```bash
./guarddemo        # 샘플 대상 3대로 5단계를 직접 체험 (실제 작업 없음)
```

다른 도구에 붙일 때:

```go
import sg "safetyguard"

in := sg.NewLineReader(os.Stdin)            // 프로세스에 하나만 만든다 (stdin 을 계속 읽는 고루틴이 있음)
err := sg.RunGuard(ctx, in, os.Stdout, targets, sg.GuardOpts{})
if err != nil { os.Exit(sg.ExitFail) }       // 취소 사유는 이미 화면에 출력됨
// ← 여기서부터 실제 위험 작업
```

추가 확인 코드(예: 특수 대상 경고)가 필요하면 `sg.ConfirmCode(ctx, in, out, prompt, sg.NewCode())`.

## 3. 안전장치 5단계

| 단계 | 내용 |
|---|---|
| 1 | 대문자+숫자 6자리 임의 코드를 화면에 보여주고 입력받음 → 통과하면 대상 목록과 총 대수 출력 |
| 2 | 60초 제한 안내 후 Enter 대기 |
| 3 | 제한시간(기본 60초) 안에 대상 이름 전체를 입력. 공백/줄바꿈 아무거나 구분자, 순서 자유. 누락·초과·중복·불일치는 즉시 취소 |
| 4 | `YeS` 정확히 (대소문자 구분) |
| 5 | 가로 카운트다운 10…1 (중간에 Ctrl+C 가능) |

모든 취소는 사유를 `[취소] …` 로 출력하고 종료 코드 1(`ExitFail`), 통과는 0(`ExitOK`). 모든 단계에서 Ctrl+C 를 처리합니다.

## 4. 옵션 (`GuardOpts`, 0/nil 이면 기본값)

| 필드 | 기본 | 설명 |
|---|---|---|
| `InputTimeout` | 60s | 3단계 전체 입력 제한 |
| `Countdown` | 10 | 5단계 시작 숫자 |
| `Tick` | 1s | 카운트다운 간격 (시험 때 줄임) |
| `NewCode` | `NewCode()` | 코드 생성기 교체(시험용) |
| `ShowTargets` | 한 줄에 하나 | 1단계 후 대상 목록 출력 방식 |

환경변수: `SAFETYGUARD_COLOR=never|always`(색상), `NO_COLOR`. `SAFETYGUARD_TEST_*` 는 `RunDemo` 에서만 읽습니다(실제 사용 경로에서는 읽지 않음).

## 5. 파일

| 파일 | 역할 |
|---|---|
| `guard.go` | `RunGuard`, `ConfirmCode`, `NewCode`, `RunDemo` — 5단계 본체 |
| `ui.go` | 색상, `LineReader`(공용 stdin 리더, 시간 제한 읽기) |
| `types.go` | 종료 코드 상수 |
| `guard_test.go` | 단위 시험 |
| `cmd/guarddemo/` | 체험용 실행 파일 |
| `test/guard_scenarios.sh` | 파이프 입력 시나리오 |
| `PROMPT.md` | **다른 도구에 안전장치를 붙일 때 Claude 에게 주는 프롬프트** |

## 6. 주의사항 (Disclaimer)

- 이 안전장치는 "실행한 사람이 대상을 눈으로 확인했는가"를 강제하는 보조 수단입니다. 코드가 같은 화면에 출력되므로 제3자(담당자) 승인을 증명하지는 않습니다.
- 100% 신뢰하지 말고 참고용 보조 도구로 사용하십시오. 이를 붙인 도구가 설정·전원 등을 변경했다면, 작업 후 **무작위 서버 몇 대를 직접 확인**해 실제로 반영되었는지 확인하십시오.
- `LineReader` 는 프로세스에 하나만 만들고, 비밀번호 등 다른 입력도 같은 리더로 읽으십시오(따로 읽으면 입력을 가로챔).
