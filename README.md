# SOP(표준운영절차서) 자동 생성 프로그램 (v1.2 최종본)

원본 엑셀(`target.xlsx`)의 각 시트(작업 항목)에 작성된 **`H2` 병합 셀 본문과 포함된 이미지/도형**을 사용자가 체크한 순서대로 조합하여, 표준운영절차서(SOP) 양식의 **단일 `작업방법` 병합 셀**에 누락이나 겹침 없이 세로 적재하여 새로운 엑셀 파일(`.xlsx`)로 생성해주는 Go 기반 독립 실행형 소프트웨어입니다.

---

## 1. 빌드 및 설치 방법

### 1.1 폐쇄망(오프라인 / 에어갭 환경) 빌드 방법
본 저장소에는 `vendor/` 디렉터리가 기본 포함되어 있어, 인터넷 연결이 차단된 사내 폐쇄망 환경에서도 추가 다운로드 없이 즉시 컴파일할 수 있습니다.

```powershell
# 1. 소스 디렉터리로 이동
cd C:\Users\qazx2\.gemini\antigravity-ide\scratch\excel-test-runner

# 2. 리소스 패키징 (아이콘 및 매니페스트)
& "C:\Users\qazx2\go\bin\rsrc.exe" -manifest excel-test-runner.manifest -ico app.ico -arch amd64 -o rsrc_windows_amd64.syso

# 3. 오프라인 벤더 모드로 콘솔 없는 단일 윈도우 GUI 실행 파일 빌드
go build -mod=vendor -ldflags="-H windowsgui -s -w" -o excel-test-runner.exe .
```

### 1.2 조건정의서(DoD v1.2) 16개 항목 전체 자동 검증
```powershell
# 원클릭 배치 파일 실행
.\run_dod_test.bat

# 또는 Go로 직접 실행
go run -mod=vendor test_dod.go
```

---

## 2. 사용 방법

### 2.1 GUI 모드 실행 (추천)
1. `run.bat` 또는 `excel-test-runner.exe`를 더블 클릭하여 실행합니다.
2. **작업자 이름 설정**:
   - 최초 실행 시 작업자 이름을 입력하는 모달 창이 표시되며, 입력한 이름은 `config.json`에 영구 저장됩니다.
   - 상단 헤더의 `[작업자 변경]` 버튼을 통해 언제든 수정할 수 있습니다.
3. **Target 파일 및 시트 선택**:
   - `[시트 조회]` 버튼을 눌러 대상 엑셀 파일(`target.xlsx`)의 시트 목록을 불러옵니다.
   - 원하는 작업을 원하는 순서대로 클릭하면 배지(`1`, `2`, `3`...)가 부여되며 조합 순서가 지정됩니다.
4. **양식 및 분류 입력**:
   - 기본 양식을 사용하거나 `[양식 업로드]`를 통해 사용자 커스텀 양식을 선택합니다.
   - 대분류 / 중분류를 드롭다운에서 선택하거나 직접 입력합니다.
   - 상세내용(줄바꿈 지원)을 입력하고 생성할 파일명을 지정합니다.
5. **생성 및 결과 확인**:
   - **[🚀 SOP 엑셀 자동 생성 시작]** 버튼을 클릭합니다.
   - 생성이 완료되면 `[📂 생성된 엑셀 파일 열기]` 또는 `[📁 저장 폴더 열기]` 버튼으로 결과를 바로 확인합니다.

### 2.2 CLI 명령줄 실행 예시
명령줄 환경에서 화면 없이 배치 작업이나 자동화를 실행할 수 있습니다.

```powershell
# 1. 3개 시트가 포함된 테스트용 샘플 target.xlsx 자동 생성
.\excel-test-runner.exe -cli -create-sample

# 2. 기본 표준 양식(templates/default_template.xlsx) 자동 생성
.\excel-test-runner.exe -cli -create-default-template

# 3. 작업자 이름 등록
.\excel-test-runner.exe -cli -worker="홍길동"

# 4. 순서를 지정하여 SOP 엑셀 파일 생성 (3번 시트 -> 1번 시트 -> 2번 시트 순서)
.\excel-test-runner.exe -cli -target="target.xlsx" -major="VM설치" -minor="서버" -detail="정기 점검 및 설치 절차서" -sheets="3_보안점검,1_OS설치,2_네트워크구성" -out="SOP_결과.xlsx"
```

---

## 3. 옵션별 상세 설명

| CLI 플래그 | 기본값 | 필수 여부 | 상세 설명 |
|---|---|---|---|
| `-cli` | `false` | 선택 | GUI 창 없이 명령줄(CLI)에서 즉시 실행 |
| `-target` | `target.xlsx` | 선택 | 작업 시트들이 포함된 원본 엑셀 파일명/경로 |
| `-template` | `templates/default_template.xlsx` | 선택 | 적용할 양식 엑셀 파일명/경로 (상단 헤더 보존, 하단 플레이스홀더 교체) |
| `-major` | `VM설치` | 선택 | 대분류 명칭 (드롭다운 목록에 없는 경우 새 분류로 자동 등록) |
| `-minor` | `공통` | 선택 | 중분류 명칭 (드롭다운 목록에 없는 경우 새 분류로 자동 등록) |
| `-detail` | `""` | 선택 | 상세내용 멀티라인 텍스트 (줄바꿈 `\n` 유지) |
| `-sheets` | `""` | 필수 (생성시) | 조합할 시트 이름 목록 (쉼표 구분, 입력한 순서대로 1., 2., 3. 번호 부여) |
| `-out` | `SOP_완료.xlsx` | 선택 | 프로그램(`.exe`) 위치에 생성될 결과 엑셀 파일명 |
| `-worker` | `""` | 선택 | 작업자 이름 설정 (설정 파일에 영구 반영) |
| `-get-worker` | `false` | 선택 | 현재 저장된 작업자 이름 출력 |
| `-create-sample` | `false` | 선택 | 3개 작업 시트와 이미지가 포함된 테스트용 `target.xlsx` 생성 |
| `-create-default-template` | `false` | 선택 | 표준 규격 `default_template.xlsx` 생성 |

---

## 4. 문서별 설명 및 주의사항 (Disclaimer)

> **주의사항 (Disclaimer)**: 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.
>
> **주의사항**: 설정 변경 스크립트의 경우, 설정 변경 완료 후 반드시 랜덤한 서버 몇 대를 직접 점검하여 변경 사항이 의도대로 정상 반영되었는지 교차 검증하십시오.
>
> **폐쇄망 DRM 및 엑셀 프로세스 주의사항**: 본 프로그램은 사내 보안 DRM 환경을 지원하기 위해 원본 파일 및 양식 파일을 직접 덮어쓰지 않고, 엑셀 인프로세스 COM을 통해 새로운 통합문서를 만들어 저장합니다. 생성 중에는 엑셀 애플리케이션이 백그라운드에서 동작하므로 프로그램이 완료될 때까지 기다려 주십시오.

---

## 5. 전역 명령어로 사용하기 (선택 사항)

어느 경로에서든 `sop-runner` 명령어로 실행할 수 있도록 `PATH` 환경 변수에 등록할 수 있습니다.

### Windows PowerShell
```powershell
# 현재 실행 폴더를 사용자 PATH에 등록
$exeDir = "C:\Users\qazx2\.gemini\antigravity-ide\scratch\excel-test-runner"
[Environment]::SetEnvironmentVariable("Path", $env:Path + ";$exeDir", [EnvironmentVariableTarget]::User)

# 어디서나 실행
excel-test-runner.exe -cli -target="target.xlsx" -sheets="1_OS설치,2_네트워크구성" -out="결과.xlsx"
```
