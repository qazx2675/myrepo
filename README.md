# 데이터센터 서버 할당 및 입고 시뮬레이션 3D 렌더링 시스템

본 프로젝트는 신규 서버 및 네트워크 스위치 입고 전, 실제 데이터센터와 동일한 3D 공간 및 2D 랙 실장도(Elevation) 환경에서 장비를 사전 배치(CSV 자동 할당 및 수동 할당)하고 공간 충돌 여부를 시뮬레이션하는 웹 기반 3D 시스템입니다. (Sunbird dcTrack 스타일 벤치마크)

---

## 🚀 빠른 시작 (Quick Start)

본 시스템은 **Windows 무빌드(Zero-Build) 즉시 실행 환경**으로 설계되어 별도의 Node.js, npm, Webpack/Vite 번들러 설치 없이 즉시 실행할 수 있습니다.

### 방법 1: 더블클릭 실행 (Windows)
1. 탐색기에서 `run_windows.bat`를 더블클릭합니다.
2. 기본 웹 브라우저(Edge, Chrome 등)가 열리며 3D 데이터센터 화면이 즉시 표시됩니다.

### 방법 2: 브라우저로 직접 열기
* 루트 폴더의 `index.html` 파일을 크롬/엣지 브라우저로 드래그하거나 더블클릭하여 실행합니다.

---

## 🛠️ 주요 기능 명세

### 1. Sunbird dcTrack 스타일 3D 뷰포트 & 프로시저럴 텍스처
* **Three.js WebGL + CSS2DRenderer**:
  * 600mm 규격 항온항습 바닥 타일 그리드 및 통로 타공 타일(`perforated`), 구역 타일(`zone_green`, `zone_yellow`, `zone_red`)
  * 가변 U 랙(기본 42U, 1U~60U) 프레임 렌더링 및 사용률에 따른 프레임 색상 동적 변경 (여유: 녹색, 주의: 황색, 포화: 적색, 선택: 청색)
  * 랙 상단 2단 플로팅 배지 (`2A`~`2H` 랙 명칭 카드 + `Actual/Budget kW`, `Used U` 요약 카드)
* **HTML5 Canvas 기반 무의존성 프로시저럴 텍스처**:
  * **서버 전면**: 핫스왑 드라이브 베이 격자, 상태 LED, 랙 이어, 호스트네임 배지
  * **서버 후면**: 듀얼 파워 서플라이(PSU), 벌집형 배기 팬 그릴 메쉬, PCIe 슬롯 브래킷
  * **스위치 전면/후면**: 24/48포트 RJ45 매트릭스, SFP+ 10G 광포트 케이지, 시안 라인 섀시
  * **온도 센서 배지**: 서버 표면에 76.6°F, 81.7°F 등 실시간 센서 오버레이 태그 표시

### 2. 마우스 & 키보드 통로 네비게이션 (Aisle Inspection)
* **마우스**:
  * 좌클릭 드래그: 시점 회전 (Orbit)
  * 우클릭 드래그: 화면 평행 이동 (Pan)
  * 휠: 줌 인/아웃 (Dolly)
  * 랙/장비 더블클릭: 해당 랙 정면 눈높이로 부드러운 카메라 자동 포커스 (Smooth Camera Lerp)
* **키보드 통로 이동**:
  * `↑` / `W`: 카메라 시선 방향 전진 (랙 사이 복도 통로 진입)
  * `↓` / `S`: 카메라 시선 방향 후진
  * `←` / `A`: 좌측 평행 이동 (옆 랙으로 이동)
  * `→` / `D`: 우측 평행 이동
  * `Q` / `E` (또는 `PageUp` / `PageDown`): 눈높이 상하 조절 (1U ~ 42U)

### 3. 우측 Cabinet Elevation (2D 랙 실장도) & 수동 장비 할당
* 선택된 랙의 `1U`(하단)부터 `maxU`(상단, 기본 42U)까지 수직 슬롯 그리드 렌더링
* 좌우 PDU 레일(`RPDU-L`, `RPDU-R`) 표시
* **빈 슬롯 클릭 시**: 수동 장비 추가 폼이 열리며 해당 U 번호가 자동 입력되어 즉시 장비 할당 가능
* **기존 장비 클릭 시**: 장비 상세 정보 확인, 다른 U 위치로 이동, 앞/뒤 방향 반전, 폼팩터/모델 수정, 장비 삭제 제공

### 4. 5단계 CSV 자동 할당 및 정밀 충돌 검증
* **지원 인코딩**: UTF-8 및 EUC-KR(CP949, Windows 엑셀 기본 인코딩) 자동 감지
* **5단계 검증 엔진**:
  1. 필수 입력값 및 숫자 형식 검증 (1U 미만 불가)
  2. 데이터센터 및 랙 존재 여부 (미존재 랙 자동 생성 옵션 지원)
  3. 랙 최대 높이(`maxU`) 초과 검증 ($startU + formFactorU - 1 > maxU$)
  4. 기존 장비와의 U 슬롯 중복 충돌 검증 ($\max(A_{start}, B_{start}) \le \min(A_{end}, B_{end})$)
  5. 동일 CSV 내부 행 간 중복 검증
* **상세 에러 리포트 모달**:
  * 행 번호, 데이터센터, 랙, 요청 U 범위, 호스트네임, 실패 사유 상세 출력
  * `[정상 항목만 반영]`, `[전체 취소]`, `[오류 내역 CSV 다운로드]` 액션 제공

### 5. 신규 서버 입고 사전 시뮬레이션(Staging) 워크플로우
* 상단 **[신규 서버 입고 시뮬레이션]** 스위치를 켜면 추가되는 모든 장비가 `isSimulation: true`로 격리 등록
* **3D 뷰**: 청록색/네온 블루 발광 테두리(Wireframe Outline + Emissive)로 시각적 하이라이트
* **2D 실장도**: `[NEW 입고예정]` 배지와 점선/빗금 테두리 표시
* **3단계 뷰 전환**:
  * `입고 전 (기존 운영 장비만 보기)`
  * `입고 후 (신규 장비 하이라이트 보기)`
  * `입고 후 (실제 렌더링 외형 보기)`
* **확정(Commit) 및 초기화(Rollback)**:
  * 확정: 시뮬레이션 장비를 실제 운영 장비(`isSimulation: false`)로 일괄 전환 및 저장
  * 초기화: 기존 장비는 보존한 채 시뮬레이션 장비만 일괄 삭제

### 6. Windows 로컬 폴더 영속성 (Local Directory Persistence)
* Chromium 기반 브라우저(Edge, Chrome)의 **File System Access API (`window.showDirectoryPicker()`)**를 사용하여 사용자가 지정한 Windows 로컬 폴더에 프로젝트 파일들을 직접 읽고 자동 저장합니다.
  * `datacenter_layout.json`
  * `allocated_devices.csv`
  * `simulation_staging.json`
* 파일 다운로드 및 업로드 폴백(Fallback) 기능 내장

---

## 📁 디렉토리 구조

```text
datacenter-3d-renderer/
├── index.html                      # 메인 진입점 (더블클릭 또는 로컬 런처로 즉시 실행)
├── run_windows.bat                 # Windows용 즉시 실행 스크립트
├── README.md                       # 프로젝트 문서
├── vendor/                         # 오프라인/무빌드 실행을 위한 로컬 라이브러리 번들
│   ├── three.min.js                # Three.js r128 코어
│   ├── OrbitControls.js            # 3D 카메라 컨트롤러
│   ├── CSS2DRenderer.js            # 랙 상단 플로팅 배지(2A~2H) 렌더러
│   └── papaparse.min.js            # CSV 파서
├── css/
│   └── style.css                   # Sunbird dcTrack 스타일 다크 테마 UI 스타일시트
├── js/
│   ├── app.js                      # 메인 앱 초기화 및 전역 상태 바인딩
│   ├── state/
│   │   ├── store.js                # 사이트, 랙, 장비, 시뮬레이션 상태 관리 스토어
│   │   └── validator.js            # U 범위 계산(1~4U), 중복/초과 검증 및 에러 리포트 생성기
│   ├── storage/
│   │   ├── folderManager.js        # Windows 로컬 폴더 지정(File System Access API) 및 자동 동기화
│   │   └── csvHandler.js           # CSV 자동 할당 파서(UTF-8/EUC-KR) 및 내보내기 모듈
│   ├── renderer3d/
│   │   ├── sceneManager.js         # 3D 씬, 조명, 바닥 타일 그리드, 방향키/마우스 통로 이동
│   │   ├── rackMeshBuilder.js      # 가변 U 랙 프레임, 90도 단위 회전, 상단 플로팅 라벨 생성
│   │   ├── deviceMeshBuilder.js    # 서버/스위치 3D 메쉬 및 앞/뒤 방향 배치
│   │   └── textureGenerator.js     # 서버 전면/후면 및 스위치("switch") 프로시저럴 텍스처 생성
│   └── ui/
│       ├── topToolbar.js           # 상단 툴바, 검색, 시뮬레이션 토글, 폴더/CSV 액션
│       ├── leftCabinetList.js      # 좌측 Used RUs 범례 및 랙 목록 테이블
│       ├── rightElevationPanel.js  # 우측 2D 랙 실장도(1~maxU) 및 수동 할당/편집 UI
│       ├── layoutEditorModal.js    # 데이터센터 그리드 크기 조절 및 랙 자유 배치/회전 에디터
│       └── errorReportModal.js     # CSV 할당 불가 항목 상세 리포트 모달
└── sample_data/                    # 초기 구동 및 테스트용 샘플 데이터 폴더
    ├── datacenter_layout.json      # 기본 SITE A (2A~2H 랙 배치) 샘플 레이아웃
    ├── sample_servers.csv          # 정상 할당 테스트용 CSV (61대 장비)
    └── sample_Simulation_New.csv   # 신규 서버 입고 시뮬레이션 및 충돌 테스트용 CSV
```


---

## Go 단일 실행 파일 버전 (`datacenter-viewer.exe`)

Windows 10 에서 Node/브라우저 서버 설치 없이 `.exe` 하나로 실행하는 버전입니다. 프론트엔드(3D 뷰어)는 그대로이고, Go 가 정적 파일 제공 + `./data/` 자동 저장을 담당합니다.

### 1. 빌드 및 설치
1. Go 1.21 이상 설치 (<https://go.dev/dl/>). 외부 Go 의존성이 없어 인터넷 없이도 빌드됩니다 (폐쇄망 OK).
2. 이 폴더를 통째로 복사한 뒤 `build-windows.bat` 실행 (콘솔 창이 필요하면 `build-windows.bat console`).
   - 내부 동작: `index.html`/`css`/`js`/`vendor` → `internal/webui/dist/` 복사 → `go test` → `go build` (`CGO_ENABLED=0`).
3. 생성된 `datacenter-viewer.exe` 한 파일만 원하는 PC 로 복사하면 설치 끝입니다.

### 2. 사용 방법
- `datacenter-viewer.exe` 더블클릭 → Edge(없으면 Chrome, 둘 다 없으면 기본 브라우저)가 주소창 없는 앱 창으로 열립니다. 창을 닫으면 프로그램도 안전하게 자동 종료됩니다.
- **로컬 및 사내 네트워크 공유 폴더 지원**:
  * 기본 상태에서는 exe 옆 `data/datacenter_layout.json`, `data/allocated_hosts.csv`에 자동 저장됩니다.
  * 상단 툴바의 **[📁 저장/공유 폴더]** 버튼을 눌러 Windows 공유 폴더(UNC 경로: `\\192.168.1.100\datacenter_share\data` 또는 마운트 드라이브: `Z:\dc_data`)를 지정할 수 있습니다.
  * 여러 사용자가 각자의 PC에서 `datacenter-viewer.exe`를 구동하더라도 동일한 공유 폴더를 원본으로 바라보며, 개인이 CSV를 할당하고 저장하면 공유 폴더의 마스터 데이터가 실시간 갱신됩니다.
  * 다른 팀원이 업데이트한 최신 랙 배치 및 입고 현황은 상단 툴바의 **[🔄 동기화]** 버튼 한 번으로 즉시 불러올 수 있습니다.
  * 한 번 지정된 공유 폴더 경로는 exe 위치의 `data_path.txt`에 영구 보존됩니다.
- **키보드 조작 및 속도 제어**:
  * `W`/`S`/`↑`/`↓`: 시선 방향 전진/후진
  * `A`/`D`: 좌우이동 (Strafe)
  * `←`/`→` 또는 마우스 드래그: 시선(고개) 회전 (감도 조절 지원: 0.1x~2.5x)
  * `Q`/`E`: 눈높이 수직 조절 (토글 지원: `1.0x -> 1.8x -> 3.0x -> 0.5x`)
  * `Shift`: 달리기 이동 (토글 지원: `2.0x -> 3.0x -> 4.5x -> 1.2x`)
- **저사양 PC (i5-10400 / 16GB / 내장 그래픽 Intel UHD 630) 무지연 60 FPS 보장**:
  * 15,000대 서버 환경에서도 동적 LOD 스트리밍, 상단뷰 서버 렌더 생략, 랙 명판 해상도 정밀화(256x64), 정적 매트릭스 동결 및 인스턴스드 바닥 메쉬를 통해 메모리 점유율을 **300~350MB** 수준으로 극대화했습니다.

### 3. 옵션
| 옵션 | 설명 |
|---|---|
| `-port N` | 리슨 포트 (기본 0 = 자동) |
| `-data-dir 경로` | 데이터 폴더 또는 Windows 공유 폴더 (기본 exe 옆 `data`, 또는 `data_path.txt` 설정값) |
| `-headless` | 창 없이 서버만 실행 (Ctrl+C 종료) → 다른 브라우저로 출력 URL 접속 |

### 4. 면책 / 주의
- 서버는 로컬 `127.0.0.1` 에 바인딩되어 외부 PC 에서 백엔드 포트로 직접 접근하지 않으며, 팀원 간 공유는 Windows 파일 공유(SMB) 경로를 통해 안전하게 이루어집니다.
- 시뮬레이션(입고 전 가배치) 장비는 자동 저장 대상이 아니며, [확정] 버튼 클릭 시 정식 장비로 저장됩니다.
- Windows SMB 공유 폴더 파일 락에 대비한 원자적 기록(`atomicWrite`) 로직이 내장되어 있습니다.

### 5. 전역 명령어
`datacenter-viewer.exe` 가 있는 폴더를 시스템 `PATH` 에 추가하면 어느 경로에서든 `datacenter-viewer` 로 실행됩니다.
