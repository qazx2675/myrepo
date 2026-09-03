# pwsh_linux_setting (폐쇄망 윈도우용 PowerShell 리눅스 환경 구성 도구)

폐쇄망(Air-gapped) 환경의 Windows PC에서 **PowerShell(Windows PowerShell 5.1 및 PowerShell 7+)을 리눅스 Bash처럼 자연스럽게 사용할 수 있도록 자동 구성**해 주는 도구입니다.

---

## 1. 개요 및 배경

Windows PowerShell의 기본 `ls`, `cat`, `rm` 명령어는 실제 리눅스 유틸리티가 아니라 PowerShell 내부 Cmdlet의 별칭(Alias)입니다. 이로 인해 리눅스에서 일상적으로 사용하는 옵션(예: `ls -la`, `rm -rf`)을 입력하면 파라미터 바인딩 에러가 발생합니다.

본 도구는 폐쇄망 윈도우 환경에 반입된 **Git for Windows의 GNU Coreutils(`usr/bin`)**를 감지하여 PowerShell에 바인딩하고, 충돌하는 별칭을 제거하며, 리눅스 Bash 단축키(`Ctrl+A`, `Ctrl+E`, `Ctrl+R` 등)를 원클릭으로 활성화합니다.

---

## 2. 폴더 구조

```
pwsh_linux_setting/
├── install.bat          # 윈도우 원클릭 설치 배치파일 (더블클릭 실행)
├── install.ps1          # 핵심 설치 스크립트 (경로 자동 탐색 및 프로필 주입)
├── uninstall.bat        # 원클릭 제거 배치파일
├── uninstall.ps1        # 프로필 원복 및 삭제 스크립트
├── README.md            # 본 사용 매뉴얼
└── bin/ (선택)          # (Git 미설치 폐쇄망용) PortableGit의 usr/bin 파일들을 여기에 복사해두면 자동 인식
```

---

## 3. 폐쇄망 반입 및 사전 준비

### 방법 A) 폐쇄망 PC에 Git for Windows가 이미 설치되어 있는 경우 (가장 흔함)
- 별도 준비 없이 본 폴더를 복사한 후 바로 `install.bat`를 실행하시면 됩니다.
- 스크립트가 표준 경로(`C:\Program Files\Git\usr\bin` 등)를 1초 만에 자동 탐색합니다.

### 방법 B) Git이 설치되어 있지 않은 순수 오프라인 PC인 경우
- 인터넷이 되는 환경에서 **PortableGit**(무설치 압축본)을 다운로드하여 폐쇄망으로 반입합니다.
- 반입 후 아래 둘 중 편한 방식을 선택합니다:
  1. 본 폴더 내에 `bin` 폴더를 만들고 PortableGit 내부의 `usr/bin` 파일들을 복사해 둡니다.
  2. 또는 임의의 폴더(예: `D:\PortableGit`)에 압축을 푼 뒤 `-CustomBinPath` 옵션으로 경로를 지정합니다.

---

## 4. 사용 방법

### 가장 간단한 실행
1. `install.bat` 파일을 마우스 우클릭 → **관리자 권한으로 실행** (또는 일반 실행)합니다.
2. 스크립트가 리눅스 바이너리 경로와 사용자 프로필 경로(OneDrive 동기화 문서 포함)를 자동 탐색하여 등록합니다.
3. 작업 완료 후 **새 PowerShell 창**을 열어 리눅스 명령어를 테스트합니다.

### PowerShell 콘솔에서 직접 실행
```powershell
# 기본 자동 탐색 설치
.\install.ps1

# 특정 경로에 있는 Linux 바이너리를 직접 지정할 때
.\install.ps1 -CustomBinPath "D:\Tools\PortableGit\usr\bin"
```

---

## 5. 적용되는 주요 기능

| 기능 | 설명 | 사용 예시 |
| :--- | :--- | :--- |
| **GNU 리눅스 명령어 365종 연동** | `grep`, `sed`, `awk`, `find`, `cat`, `rm`, `head`, `tail`, `less`, `which`, `tar`, `touch` 등 완벽 동작 | `grep -rn "error" .`<br>`cat app.log \| head -n 20` |
| **리눅스 옵션 문법 활성화** | PowerShell 기본 Alias를 해제하여 `-la`, `-rf` 등 리눅스 플래그 정상 처리 | `ls -la`<br>`rm -rf ./temp_dir` |
| **단축 편의 함수 제공** | 리눅스 필수 단축 명령어 내장 | `ll` (`ls -la --color=auto`)<br>`la` (`ls -a --color=auto`)<br>`which grep` |
| **Bash/Emacs 키바인딩** | 리눅스 터미널의 커서 제어 단축키 활성화 | `Ctrl + A` (줄 맨 앞)<br>`Ctrl + E` (줄 맨 끝)<br>`Ctrl + U` (줄 전체 삭제)<br>`Ctrl + R` (명령어 히스토리 역방향 검색) |
| **한글 UTF-8 인코딩** | 리눅스 도구들과 윈도우 파이프라인 간 한글 깨짐 방지 | `[Console]::OutputEncoding = UTF-8` 자동 적용 |

---

## 6. 제거 (원복) 방법

설정을 제거하고 원래의 윈도우 기본 PowerShell로 되돌리려면 `uninstall.bat`를 실행하거나 PowerShell에서 `.\uninstall.ps1`을 실행하시면 됩니다.
기존 프로필은 설치 시 자동으로 `*.bak_YYYYMMDDHHMMSS` 형식으로 백업되므로 언제든 수동 복구도 가능합니다.
