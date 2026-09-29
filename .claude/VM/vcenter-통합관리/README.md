# vcenter-portal (vCenter 통합 관리 포털)

여러 대의 vCenter(약 15대, VM 13,000대 이상)에 흩어진 호스트/VM 을 **하나의 vCenter 에 있는 것처럼**
검색하고 트리로 탐색하는 조회 전용 포털입니다. 웹 서버/DB 없이 **사내 공유폴더의 정적 파일만으로 동작**하며,
객체마다 "vCenter에서 열기" 링크로 원래 vCenter 화면을 **로그인된 상태로** 열 수 있습니다.

구성 요소는 세 가지입니다.

| 구성 요소 | 하는 일 |
|---|---|
| 수집기 `vcportal-collector.exe` | Windows 서버에서 매일 12:00 에 모든 vCenter 를 병렬 조회해 `data\*.js` 생성 |
| 웹 UI `index.html` + `assets\` | 탐색기에서 더블클릭으로 열어 검색/트리/Summary 조회 (vSphere Client 8 디자인) |
| 로그인 런처 `vcportal.exe` | `vcportal://` 링크를 받아 전용 Edge 프로필로 vCenter 에 자동 로그인 후 해당 화면 열기 |

> 설계 배경과 결정 사항은 [vcenter-integrated-portal-plan.md](vcenter-integrated-portal-plan.md),
> 데이터 파일 형식은 [docs/DATA_SCHEMA.md](docs/DATA_SCHEMA.md) 를 참고하십시오.

---

## ⚠️ 주의사항 (Disclaimer)

본 도구는 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 표시되는 정보는 매일 12:00 수집 시점 기준이며, 최신 상태는 'vCenter에서 열기'로 실제 vCenter 에서 확인하십시오.

이 도구는 vCenter 를 **조회만** 합니다(VM/호스트 제어 기능 없음).

### 알려진 제한 사항

- **Edge 확인 창**: `index.html` 을 탐색기에서 직접 연 경우 "vCenter에서 열기"를 누르면 Edge 가
  "vcportal 을(를) 열려고 합니다" 확인 창을 띄웁니다(매번 **[열기]** 필요). 바탕화면 `vCenter 포털` 바로가기(포털 모드)로 열면 확인 창이 없습니다.
  (없애려면 Edge 정책 `AutoLaunchProtocolsFromOrigins` 를 GPO/Intune 으로 배포해야 하며, 이 도구는 설정하지 않습니다.)
- **인증서 오류 무시**: 사내 vCenter 가 자체 서명 인증서라 런처가 띄우는 **전용 프로필 브라우저에서만**
  `--ignore-certificate-errors` 를 사용합니다. 평소 쓰는 Edge 프로필에는 영향이 없습니다.
- **원격 디버깅 포트 9333**: 런처가 띄운 전용 프로필 브라우저는 `127.0.0.1:9333` 에서만 원격 디버깅을 받습니다
  (같은 PC 의 다른 프로세스가 접근할 수 있으므로, 이 전용 브라우저로는 vCenter 외 사이트를 열지 마십시오).
- **클러스터 필드 미검증**: 랩 vCenter 에 클러스터가 없어, Cluster 관련 수집 필드(DRS/HA, 용량 등)는 실제 환경에서 검증하지 못했습니다.
- **삭제된 vCenter 의 데이터 파일**: conf 의 `[vcenters]` 에서 vCenter 를 제거해도 `data\<id>.js` 는 자동 삭제되지 않고 남습니다
  (검색 인덱스와 트리에서는 제외됨). 필요하면 수동으로 삭제하십시오.
- **평문 비밀번호**: `config\vcportal.conf` 에 공용 계정 비밀번호가 평문으로 들어갑니다. 공유폴더 및 `config\` 폴더의 접근 권한을 제한하십시오.
- 로그인 자동화는 vSphere Client 8.0.3 의 SSO 로그인 화면 구조에 의존합니다(업그레이드 시 선택자 수정이 필요할 수 있음, ARCHITECTURE.md 참고).
  자동 로그인이 실패하면 로그인 화면에서 멈추므로 수동으로 로그인하면 됩니다.

---

## 1. 빌드 및 설치 방법

### 1.1 요구사항

| 항목 | 값 |
|---|---|
| Go | 1.26.5 이상 (`go.mod` 기준, 빌드할 때만 필요) |
| 빌드 OS | **Windows**: PowerShell 5.1 + `build.ps1` (bash 불필요) / **Linux**: bash + `build.sh`. 결과물은 둘 다 Windows/amd64 exe |
| 수집 서버 / 사용자 PC | Windows 10/11 또는 Server 2016+, Windows PowerShell 5.1, Microsoft Edge(또는 Chrome) |
| 네트워크 | 수집 서버 → 각 vCenter 443/tcp, 사용자 PC → vCenter 443/tcp |
| vCenter | 8.0.3 (딥링크 URL 형식이 8.0.3 기준) |

### 1.2 빌드 (폐쇄망 지원)

의존성이 `vendor/` 에 모두 포함되어 있어 **이 폴더를 통째로 내려받으면 인터넷 없이 빌드**됩니다.

**Windows (회사, PowerShell 만 있는 환경)** — Go 1.26.5 설치 필요(`go version` 으로 확인)

```powershell
cd <내려받은 폴더>\.claude\VM\vcenter-통합관리
powershell -ExecutionPolicy Bypass -File .\build.ps1
```

`build.ps1` 은 폐쇄망용으로 `GOFLAGS=-mod=vendor`, `GOPROXY=off`, `GOSUMDB=off`, `GOTOOLCHAIN=local` 을
설정한 뒤 빌드하므로 네트워크에 전혀 접근하지 않습니다. 설치된 Go 가 `go.mod` 요구 버전보다 낮으면
"Go 1.26.5 를 설치하세요" 를 출력하고 멈춥니다(툴체인 자동 다운로드를 시도하지 않음).

**Linux (랩, bash)**

```bash
cd <저장소>/.claude/VM/vcenter-통합관리
./build.sh
```

두 스크립트 모두 `./cmd/*` 를 windows/amd64 로 빌드(`bin/windows/`, `build.sh` 는 linux 용도 추가)한 뒤,
공유폴더에 그대로 복사할 **`dist/vc-portal/`** 을 조립합니다. 런처(`vcportal.exe`)는 콘솔 창이 뜨지 않도록
GUI 서브시스템(`-H windowsgui`)으로 빌드됩니다.

```
dist/vc-portal/
├─ index.html, assets/
├─ data/                          (비어 있음. 수집기가 채움)
├─ config/vcportal.conf.example
├─ launcher/  vcportal.exe, install-launcher.ps1
└─ collector/ vcportal-collector.exe, run-collector.ps1
```

`.ps1` 과 `.example` 은 Windows 에서 한글이 깨지지 않도록 UTF-8(BOM) + CRLF 로 만들어집니다.

### 1.3 공유폴더에 배포

1. `dist/vc-portal` 폴더를 공유폴더로 복사합니다. 예: `\\fileserver\share\vc-portal`
2. `config\vcportal.conf.example` 을 `config\vcportal.conf` 로 복사하고 메모장으로 수정합니다.
   - 경로는 **탐색기 주소창에서 복사한 그대로** 붙여넣으면 됩니다(역슬래시 이스케이프 불필요, 따옴표/끝의 `\` 는 자동 정리).
   - 공용 계정 비밀번호는 평문으로 적습니다(공유폴더 접근 권한으로 보호).
   - `output_dir` 은 공유폴더의 `vc-portal` 폴더 자체입니다(이 아래 `data\` 에 결과가 생성됨).
   - 공유폴더는 드라이브 문자(`Z:\`)가 아니라 **UNC 경로(`\\서버\공유\...`)** 로 쓰십시오.
3. 로그인 런처와 수집기는 같은 `config\vcportal.conf` 를 읽습니다.

### 1.4 수집 서버 설정 (매일 12:00 자동 수집)

`collector\` 폴더는 공유폴더에서 바로 실행하거나, **수집 서버 로컬 디스크로 복사**해서 쓸 수 있습니다
(로컬 복사 시 `-Conf` 로 공유폴더의 conf 경로를 지정하십시오).

```powershell
# 1) 사전 점검: conf 해석, 경로 쓰기 권한, 각 vCenter 로그인 확인 (아무것도 수집/변경하지 않음)
powershell -ExecutionPolicy Bypass -File \\fileserver\share\vc-portal\collector\run-collector.ps1 -Check

# 2) 작업 스케줄러에 매일 12:00 실행 등록 (관리자 PowerShell 권장)
powershell -ExecutionPolicy Bypass -File \\fileserver\share\vc-portal\collector\run-collector.ps1 -RegisterTask
```

- `-Check` 가 종료 코드 0 이면 다음 단계로 진행합니다.
- 작업은 **등록한 사용자 계정**으로 실행되며, 그 계정에 `output_dir`(공유폴더) **쓰기 권한**이 있어야 합니다.
- 첫 데이터가 필요하면 `-RegisterTask` 없이 `run-collector.ps1` 을 한 번 직접 실행하십시오.

### 1.5 사용자 PC 설정 (1회)

각 사용자가 자기 PC 에서 공유폴더의 스크립트를 **한 번만** 실행합니다(관리자 권한 불필요, HKCU 에만 등록).

```powershell
powershell -ExecutionPolicy Bypass -File \\fileserver\share\vc-portal\launcher\install-launcher.ps1
```

스크립트는 두 가지를 합니다.
- **바탕화면에 `vCenter 포털` 바로가기 생성** (공유폴더의 `launcher\vcportal.exe` 를 인자 없이 실행 = 포털 모드)
- `vcportal://` 프로토콜 등록 (`index.html` 을 탐색기에서 직접 연 경우용)

**포털은 바탕화면의 `vCenter 포털` 바로가기로 여는 것을 권장합니다.** 이렇게 열면 "vCenter에서 열기"가
Edge 확인 창 없이 바로 자동 로그인 탭을 엽니다. `index.html` 을 직접 연 경우에는 Edge 가
"이 사이트에서 vcportal 을(를) 열려고 합니다" 확인 창을 띄우므로 **[열기]** 를 눌러야 합니다.

해제하려면 `install-launcher.ps1 -Uninstall` 을 실행합니다(바로가기와 프로토콜 등록 모두 제거).

---

## 2. 사용 방법

1. 바탕화면의 **`vCenter 포털`** 바로가기를 실행합니다(1.5 에서 생성). 전용 Edge 창에 포털이 열립니다.
   이미 열려 있으면 같은 창에 포털 탭이 하나 더 열립니다. (탐색기에서 `index.html` 을 직접 열어도 되지만,
   그때는 "vCenter에서 열기" 마다 Edge 확인 창의 [열기] 가 필요합니다.)
2. **검색**: 상단 검색창에 호스트명 / VM 이름 / IP / guest 호스트명 일부를 입력하면 전체 vCenter 에서 즉시 검색됩니다.
   결과를 클릭하면 해당 객체의 Summary 로 이동합니다.
3. **트리**: 좌측 Navigator 는 `vCenter > Datacenter > (Folder) > Cluster > Host > VM` 구조(Hosts and Clusters 기준)입니다.
   노드를 펼치면 하위 항목이 지연 로딩됩니다. 아이콘 탭으로 Hosts/VMs/Datastores/Networks 뷰를 전환합니다.
4. **Summary**: Host(모델, ESXi 버전/빌드, 상태, CPU·메모리 사용량, 가동 시간, VM 목록, 데이터스토어, 네트워크),
   VM(게스트 OS, IP, 하드웨어, 디스크, NIC), Cluster, Datacenter, vCenter 화면을 볼 수 있습니다.
5. **vCenter에서 열기 vs 직접 열기**
   - **vCenter에서 열기**: 로그인 런처가 전용 Edge 창에 새 탭을 열고 **자동 로그인한 뒤** 해당 객체의 vSphere Client 화면을 엽니다.
     바로가기로 연 포털에서는 확인 창 없이 바로 동작하고, `index.html` 을 직접 연 경우에는 `vcportal://` 확인 창을 거칩니다.
     (사전에 1.5 의 `install-launcher.ps1` 실행 필요)
   - **직접 열기**: 런처 없이 일반 브라우저 새 탭에서 vCenter 딥링크를 엽니다. 로그인은 직접 해야 합니다(런처가 동작하지 않을 때의 대안).
6. **수집 상태**: 화면 상단의 수집 상태 표시줄에서 마지막 수집 시각과 vCenter 별 성공/실패를 볼 수 있습니다.
   - 마지막 수집이 **26시간 이상** 지났으면 경고 배너가 표시됩니다(수집기 미동작 의심).
   - 일부 vCenter 수집이 실패하면 배너에 실패 사유가 표시되며, 해당 vCenter 는 **직전 성공 시점의 데이터**를 유지해 보여줍니다
     (성공 이력이 없으면 데이터 없음).

---

## 3. 옵션별 상세 설명

### 3.1 `config\vcportal.conf`

INI 형식입니다. 주석은 **줄 맨 앞의 `#` 또는 `;`** 만 인정하므로 값 뒤에 주석을 붙이지 마십시오(비밀번호에 `#` 가능).
저장 형식은 UTF-8(BOM 포함) 또는 ANSI(CP949) 모두 가능합니다.

| 섹션 | 키 | 필수 | 기본값 | 설명 |
|---|---|---|---|---|
| `[paths]` | `output_dir` | 예 | - | 포털 배포 폴더(공유폴더의 `vc-portal`). 이 아래 `data\` 에 결과 생성. UNC 경로 권장 |
| `[paths]` | `work_dir` | 아니오 | `%TEMP%\vcportal-work` | 수집 중간 파일/직전 성공 캐시 폴더(수집 서버 로컬 디스크). 완성 후 `output_dir` 로 복사·교체 |
| `[paths]` | `log_dir` | 아니오 | (파일 로그 없음) | 로그 폴더. `collector-YYYYMMDD.log` 로 기록 |
| `[collect]` | `parallel` | 아니오 | `5` | 동시에 수집할 vCenter 수 (1 이상 정수) |
| `[collect]` | `timeout` | 아니오 | `300` | vCenter 1대당 제한 시간(초, 1 이상 정수) |
| `[account]` | `user` | 예 | - | 모든 vCenter 공통 조회 전용 계정 (예: `lscsystems@vsphere.local`) |
| `[account]` | `password` | 예 | - | 위 계정의 비밀번호(평문) |
| `[browser]` | `type` | 아니오 | `edge` | 런처가 쓸 브라우저: `edge` 또는 `chrome` |
| `[vcenters]` | `<id>` | 1개 이상 | - | `id = 주소` 형식. id 는 영문/숫자/`_`/`-` 만 가능(파일명 `data\<id>.js` 로 사용). 주소는 `https://` 생략 가능. id/호스트 중복 불가 |

경로 값 정리 규칙: 앞뒤 공백과 따옴표 한 쌍, 끝의 `\` 또는 `/` 를 제거합니다(`C:\` 루트는 유지). 공백/한글 폴더명 사용 가능.
드라이브 문자(네트워크 드라이브) 경로는 수집 시작 시 차단되며 UNC 경로 사용을 안내합니다(로컬 디스크 드라이브는 허용).

### 3.2 수집기 `vcportal-collector.exe`

```
vcportal-collector.exe [--conf <vcportal.conf 경로>] [--check]
```

| 옵션 | 기본값 | 설명 |
|---|---|---|
| `--conf` | exe 와 같은 폴더의 `vcportal.conf` | 설정 파일 경로 |
| `--check` | 끔 | 설정 해석 결과 출력 + 경로 쓰기 권한 + 각 vCenter 로그인/버전 확인만 하고 종료(수집 안 함) |

종료 코드

| 코드 | 의미 |
|---|---|
| 0 | 전부 성공 (`--check` 는 모든 점검 정상) |
| 1 | conf/경로 오류 (`--check` 는 점검 실패 1건 이상) |
| 2 | 일부 vCenter 수집 실패(성공분은 반영, 실패분은 직전 데이터 유지) |
| 3 | 전부 실패 |
| 4 | (`run-collector.ps1` 만) 이미 다른 수집이 실행 중이라 건너뜀 |

### 3.3 `collector\run-collector.ps1`

| 파라미터 | 기본값 | 설명 |
|---|---|---|
| `-Conf <경로>` | `..\config\vcportal.conf`(스크립트 기준)가 있으면 그것, 없으면 스크립트 폴더의 `vcportal.conf` | 설정 파일 경로. 배포 구조 그대로 쓰면 생략 가능, 로컬에 복사해 쓰면 지정 필요 |
| `-Check` | 끔 | exe 에 `--check` 를 전달(점검만) |
| `-RegisterTask` | 끔 | 작업 스케줄러에 `vcportal-collector` 작업을 매일 12:00 실행으로 등록(현재 사용자 계정). conf 가 없으면 등록하지 않고 종료 코드 1 |

이름 있는 뮤텍스(`Global\vcportal-collector`)로 동시 실행을 막습니다(두 번째 실행은 종료 코드 4).
그 외 종료 코드는 exe 의 코드를 그대로 돌려줍니다.
bash 환경(랩)에서는 `scripts/run-collector.sh` 가 같은 역할을 합니다(`--conf`, `--check` 를 그대로 전달, 없으면 자동 빌드).

### 3.4 `launcher\install-launcher.ps1`

| 파라미터 | 기본값 | 설명 |
|---|---|---|
| `-ExePath <경로>` | 스크립트와 같은 폴더의 `vcportal.exe` | 바로가기 대상이자 프로토콜 핸들러로 등록할 런처 exe |
| `-Uninstall` | 끔 | 바탕화면 `vCenter 포털.lnk` 삭제 + `vcportal://` 등록 해제 (`HKCU:\Software\Classes\vcportal` 삭제) |

관리자 권한 불필요(HKCU). 네트워크 드라이브로 실행하면 UNC 사용 권장 경고를 표시합니다.

### 3.5 로그인 런처 `vcportal.exe`

```
vcportal.exe [--conf <vcportal.conf 경로>]                       포털 모드 (바탕화면 바로가기)
vcportal.exe "vcportal://open?url=<인코딩된 딥링크>"              프로토콜 링크 (index.html 직접 연 경우)
vcportal.exe --url <딥링크> [--conf <vcportal.conf 경로>]
```

| 인자 | 기본값 | 설명 |
|---|---|---|
| (없음) | - | **포털 모드**. 전용 Edge 에 `<exe 폴더>\..\index.html` 을 열고 브라우저가 닫힐 때까지 대기. 전용 Edge 의 `file://` 탭마다 `window.vcpOpen` 을 연결해 두어, "vCenter에서 열기"를 확인 창 없이 처리. 이미 포털 모드가 실행 중이면 새 포털 탭만 열고 종료 |
| (위치 인자) | - | `vcportal://open?url=...` 링크. `index.html` 을 직접 연 포털의 버튼이 이 형식으로 호출 |
| `--url` | - | 열 vCenter 딥링크(https). 위치 인자 대신 직접 지정 |
| `--conf` | `<exe 폴더>\..\config\vcportal.conf` | 설정 파일 경로 |

동작: 딥링크가 `https` 이고, 호스트가 conf `[vcenters]` 에 등록되어 있으며, 경로가 `/ui/` 인 경우에만 엽니다.
전용 프로필(`%LOCALAPPDATA%\vcportal\browser-profile`)로 브라우저를 띄우고(이미 떠 있으면 새 탭),
로그인이 필요하면 SSO 폼에 conf 계정을 자동 입력한 뒤 딥링크로 이동합니다.
로그는 `%LOCALAPPDATA%\vcportal\launcher.log` 에 남으며 비밀번호는 기록하지 않습니다. 오류는 메시지 창으로 표시되고 종료 코드 1 입니다.

---

## 4. 문서별 설명

| 경로 | 설명 |
|---|---|
| `README.md` | 이 문서. 설치/사용/옵션 |
| `ARCHITECTURE.md` | 폴더·파일별 역할, 데이터 흐름, "무엇을 바꾸려면 어디를 고치나" |
| `CHANGELOG.md` | 변경 이력(최신이 위) |
| `PR_CHECKLIST.md` | 배포/수정 전 확인 목록 |
| `사용법.txt` | 수동 테스트용 명령 모음(랩 빌드/수집/가짜 데이터/Windows 절차) |
| `vcenter-integrated-portal-plan.md` | 확정된 프로젝트 계획서(설계 배경, 리스크) |
| `docs/DATA_SCHEMA.md` | 수집기 → 웹 UI 데이터 파일(`manifest.js`/`index.js`/`<id>.js`) 형식 |
| `design-reference.png` | 화면 디자인 참고 이미지(vSphere Client 8) |
| `build.ps1` | Windows(PowerShell) 빌드 + `dist\vc-portal\` 조립 (폐쇄망, bash 불필요) |
| `build.sh` | Linux(bash) 빌드 + `dist/vc-portal/` 조립 |
| `config/vcportal.conf.example` | 설정 파일 예제 |
| `cmd/`, `internal/`, `web/`, `scripts/`, `testdata/` | 소스(수집기, 런처, 공용 conf 패키지, 웹 UI, 래퍼 스크립트, 테스트 데이터) |
| `vendor/`, `go.mod`, `go.sum`, `tools.go` | 폐쇄망 빌드를 위한 의존성 |
| `.github/workflows/vcenter-portal.yml` (저장소 루트) | CI(vet/test/Windows 크로스 빌드/JS 문법 검사) |
