# 업무프로세스 ntfy 챗봇

휴대폰(ntfy 앱 또는 웹앱)으로 질문하면 집 PC의 로컬 LLM(Ollama `qwen3:8b`)이 업무프로세스 표준 지침서 문서(`D:\업무프로세스\업무프로세스.md`)만 근거로 실시간 답변하고, 답변마다 👍/👎 피드백을 수집하여 향후 문서 보강 및 품질 관리에 활용할 수 있는 경량 챗봇 시스템입니다.

---

## 1. 빌드 및 설치 방법

### 1.1 필수 환경 요구사항
- **운영체제**: Windows 10/11 (64-bit)
- **하드웨어 권장사양**: NVIDIA RTX 3060 12GB 이상 VRAM, RAM 16GB 이상
- **Python**: Python 3.12 이상 (`py` 런처 포함)
- **LLM 엔진**: [Ollama](https://ollama.com/) 및 `qwen3:8b` 모델

### 1.2 설치 절차

1. **Python 설치** (기본 설치되지 않은 경우):
   ```powershell
   winget install Python.Python.3.12 --accept-package-agreements --accept-source-agreements
   ```
   설치 시 `py` 런처 및 PATH 등록이 활성화됩니다.
   외부 추가 라이브러리(`pip install`) 설치가 전혀 필요 없으며, Python 표준 라이브러리(`urllib`, `json`)만으로 구동됩니다.

2. **Ollama 모델 준비**:
   ```bash
   ollama pull qwen3:8b
   ```

3. **데이터 및 문서 폴더 준비**:
   `D:\업무프로세스` 디렉터리에 참조할 표준 지침서 문서(`업무프로세스.md`)를 배치합니다.
   ```
   D:\업무프로세스\
   ├── 업무프로세스.md        # 유일한 근거 문서 (모든 질문에 시스템 프롬프트로 기본 첨부)
   ├── bot\
   │   ├── bot.py
   │   ├── run_tests.py
   │   └── run.bat
   ├── logs\                  # Q&A, 미답변, 피드백 로그 자동 생성
   ├── tests\                 # 60개 테스트케이스 및 결과 CSV 저장
   └── .env                   # 토픽 및 모델 설정
   ```

---

## 2. 사용 방법

### 2.1 챗봇 실행
`run.bat` 파일을 더블클릭하거나 터미널에서 아래 명령을 실행합니다:
```cmd
run.bat
```
또는
```bash
py bot.py
```

실행 시 콘솔에 아래와 같이 연결 정보와 토픽이 출력됩니다:
```
============================================================
 [업무프로세스 ntfy 챗봇 시작 - 워밍업] 
 문서 경로: D:\업무프로세스\업무프로세스.md
 모델: qwen3:8b | 컨텍스트: 24576 | 온도: 0.2
 ntfy 서버: https://ntfy.sh
 질문 토픽: proc-q-xxxxxxxxxxxxxxxx
 답변 토픽: proc-a-xxxxxxxxxxxxxxxx
 피드백 토픽: proc-fb-xxxxxxxxxxxxxxxx
============================================================
워밍업 완료: 소요시간 13.68초 | 프롬프트 토큰: 8505
[현재 Ollama 프로세스 상태]
NAME        ID              SIZE      PROCESSOR    CONTEXT    RUNNER      UNTIL               
qwen3:8b    500a1f067a9f    8.6 GB    100% GPU     24576      llamacpp    29 minutes from now 
============================================================
[챗봇 대기 중] 휴대폰에서 질문을 전송하면 실시간으로 응답합니다. (종료: Ctrl+C)
```

### 2.2 휴대폰 연결 및 질의응답
1. **휴대폰 설정 (ntfy 앱)**:
   - Android: ntfy 앱 설치 후 **답변 토픽**(`proc-a-...`)을 구독합니다.
   - iPhone: ntfy 앱(알림 수신) 또는 Safari ntfy 웹앱(`https://ntfy.sh/app`)에서 **답변 토픽**을 구독합니다.
2. **질문 전송**:
   - 휴대폰에서 **질문 토픽**(`proc-q-...`) 화면 하단 입력창을 통해 자연어로 질문을 전송합니다.
3. **답변 수신 및 피드백**:
   - 집 PC의 봇이 문서를 기반으로 답변을 생성하여 답변 토픽으로 푸시 알림을 전송합니다.
   - 알림 하단의 **[👍 좋음]** 또는 **[👎 나쁨]** 버튼을 터치하면 집 PC의 `logs\feedback.jsonl`에 질문 ID와 함께 기록됩니다.

### 2.3 60개 자연어 테스트케이스 일괄 검증
```bash
py run_tests.py
```
`tests\테스트케이스.txt`의 60개 문항(A~F군)을 순차 실행하고 `tests\결과_YYYYMMDD.csv`로 결과를 저장합니다.

---

## 3. 옵션별 상세 설명

설정은 `D:\업무프로세스\.env` 파일에서 수정할 수 있습니다:

| 환경변수 키 | 기본값 | 필수 여부 | 상세 설명 |
|---|---|---|---|
| `NTFY_SERVER` | `https://ntfy.sh` | 필수 | ntfy 서버 주소 (공용 서버 기본 사용) |
| `NTFY_QUESTION_TOPIC` | `proc-q-<무작위16진수>` | 필수 | 휴대폰에서 질문을 전송하는 토픽 이름 (비밀번호 역할) |
| `NTFY_ANSWER_TOPIC` | `proc-a-<무작위16진수>` | 필수 | 봇이 답변을 발행하고 휴대폰이 수신하는 토픽 이름 |
| `NTFY_FEEDBACK_TOPIC` | `proc-fb-<무작위16진수>` | 필수 | 알림 버튼 클릭 시 👍/👎가 전송되는 피드백 토픽 이름 |
| `NTFY_TOKEN` | `""` (공란) | 선택 | ntfy 보호 토픽 사용 시 인증 토큰 |
| `OLLAMA_URL` | `http://127.0.0.1:11434` | 필수 | 로컬 Ollama REST API 엔드포인트 |
| `MODEL` | `qwen3:8b` | 필수 | 로컬 LLM 모델 태그 |
| `NUM_CTX` | `24576` | 필수 | 컨텍스트 토큰 크기 (문서 8.5K 토큰 기준 약 35% 점유) |
| `KEEP_ALIVE` | `30m` | 필수 | 마지막 질의 후 모델 VRAM 상주 시간 |
| `TEMPERATURE` | `0.2` | 필수 | 생성 온도 (문서 기반 정확도 유지를 위해 0.2 권장) |
| `DOC_PATH` | `D:\업무프로세스\업무프로세스.md` | 필수 | 기본 첨부 근거 문서 경로 |
| `NO_ANSWER_TEXT` | `문서에 없는 내용입니다.` | 필수 | 문서 밖 질문 시 표준 거절 문구 |

---

## 4. 문서별 설명 및 주의사항 (Disclaimer)

> [!NOTE]
> **주의사항 (Disclaimer)**: 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.

> [!CAUTION]
> **주의사항**: 설정 변경 스크립트의 경우, 설정 변경 완료 후 반드시 랜덤한 서버 몇 대를 직접 점검하여 변경 사항이 의도대로 정상 반영되었는지 교차 검증하십시오.

- **거절 규칙 준수**: 본 챗봇은 환각(Hallucination) 방지를 위해 `업무프로세스.md`에 기재되지 않은 내용에 대해서는 외부 상식을 동원하지 않고 "문서에 없는 내용입니다."로 정직하게 거절하도록 프롬프트가 강제되어 있습니다.
- **문서 수정 즉시 반영**: 문서는 매 질의마다 새로 로드되므로, `업무프로세스.md`를 편집하면 봇 재시작 없이 다음 질문부터 즉시 새 지침이 적용됩니다.

---

## 5. 전역 명령어로 사용하기 (선택 사항)

### Windows 시작 시 자동 실행 (작업 스케줄러 등록)
재부팅 후에도 백그라운드에서 상시 실행되도록 작업 스케줄러에 등록할 수 있습니다:

```powershell
$action = New-ScheduledTaskAction -Execute "py.exe" -Argument "D:\업무프로세스\bot\bot.py" -WorkingDirectory "D:\업무프로세스\bot"
$trigger = New-ScheduledTaskTrigger -AtLogOn
Register-ScheduledTask -TaskName "NtfyProcessChatbot" -Action $action -Trigger $trigger -Description "업무프로세스 ntfy 챗봇 자동 실행"
```

### 전역 실행 배치 스크립트 등록 (System32 또는 PATH 등록)
터미널 어디서나 `ntfy-proc-bot`으로 실행하려면 PATH가 지정된 디렉터리(예: `C:\Windows\System32` 또는 사용자 전용 bin)에 심볼릭 링크나 배치 파일을 생성합니다:
```cmd
echo @py "D:\업무프로세스\bot\bot.py" %%* > "C:\Users\%USERNAME%\AppData\Local\Programs\Python\Launcher\ntfy-proc-bot.cmd"
```
등록 후에는 아무 터미널에서나 `ntfy-proc-bot`을 입력하여 즉시 봇을 실행할 수 있습니다.
