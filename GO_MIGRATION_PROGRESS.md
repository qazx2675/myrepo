# Go 변환 진행 기록 (모델이 바뀌어도 이어서 작업하기 위한 인수인계 문서)

계획서: `C:\Users\qazx2\.gemini\antigravity\brain\60551a34-48c8-4676-af7e-cc7c90542503\implementation_plan.md`
규칙: 작업 하나를 끝낼 때마다 아래 체크박스를 갱신하고, 막힌 점은 "메모"에 적는다.

## 환경 메모
- 이 PC(Windows 10)에는 **Go가 설치되어 있지 않음** (`go version` 실패). 코드는 작성됐지만 **컴파일/테스트 미검증** 상태.
- 다음 모델의 첫 작업: Go 설치 여부 확인 → 있으면 `go mod tidy` 없이 `go vet ./...` / `go test ./...` 실행, 오류 수정.
- 터미널 출력이 한글 깨짐(CP949). 한글 확인은 `view_file`로 할 것.
- 프론트엔드 원본은 프로젝트 루트(`index.html`, `css/`, `js/`, `vendor/`). Go 빌드 시 `build-windows.bat`이 `internal/webui/dist/`로 복사한다 (embed는 상위 폴더 참조 불가).

## 설계 결정
- Go 백엔드는 레이아웃 JSON / 호스트 CSV 텍스트를 **그대로 저장·반환**한다 (프론트 스토어 형식 유지 → 프론트 변경 최소화).
- API: `GET /api/bootstrap` → `{layout, csv, goBackend:true}`, `POST /api/save` ← `{layout, csv}`, `GET /api/export/allocated-csv`, `POST /api/csv/validate` (Go 검증기, JS `validator.js`와 동일 결과 목표).
- 프론트 `folderManager.js`는 `/api/bootstrap` 응답이 있으면 Go 모드(자동 저장), 없으면 기존 브라우저 모드.

## 체크리스트
- [x] 계획서 작성
- [x] 프론트 v2.3.0 수정 (글자 깨짐, 감도 0.4x, 호스트 CSV 다운로드, 모달 스크롤 격리)
- [x] Phase 1: `go.mod`, `internal/webui/embed.go`, `build-windows.bat`
- [x] Phase 2: `internal/model`, `internal/csvproc` (+ `allocator_test.go`), `internal/storage`
- [x] Phase 2: `internal/server` (API + 정적 파일)
- [x] Phase 3: `js/storage/folderManager.js` Go 모드 감지 + `js/app.js` 연결
- [x] Phase 4: `internal/launcher`, `cmd/datacenter-viewer/main.go`
- [x] Phase 5: `README.md`(Go 섹션), `ARCHITECTURE.md`, `PR_CHECKLIST.md`, `.github/workflows/ci.yml`, CHANGELOG 갱신
- [x] 검증 완료: Go 1.27.0 설치(winget), `build-windows.bat console` 성공(go test 통과, exe 생성), headless 스모크 테스트(`/api/bootstrap`, 정적 파일, `/api/save` 왕복) 통과
- [x] v3.0.1 버그 해결: Edge 백그라운드 브로커 조기 종료(`127.0.0.1` 방화벽 오인 메시지) 하트비트(`/api/ping` & `/api/bye`) 해결, 3D 폰트 원색 발광(`MeshBasicMaterial` + `fog: false` + `LinearMipmapNearestFilter` + `_drawBoldText`) 적용
- [x] v3.0.2 대규모 최적화 & 아이콘 탑재:
  - **15,000대 CSV 업로드 시 메모리 10GB 폭증 및 프리징 해결**: `massiveScale` 모드(서버 300대 이상)에서 30,000장의 개별 캔버스 생성을 방지하고, 벤더/모델/폼팩터별 12개 템플릿 텍스처를 공유하는 **LOD(Level of Detail) 텍스처 풀링** 적용 (메모리 10GB -> 150MB 이하, 생성 시간 0.2초).
  - **선택 랙 온디맨드 상세화**: 랙 클릭(`selectRack`) 시 해당 랙(25대)만 실시간으로 고해상도 호스트명 텍스처로 스왑.
  - **이벤트 렌더링 디바운싱**: `sceneManager.rebuildRacksDebounced()`, `rebuildFloorDebounced()` 적용하여 다중 이벤트 발생 시 1프레임당 단 1회만 렌더링.
  - **대용량 처리 로딩 오버레이**: `csvHandler.js` 및 `style.css`에 15,000대 처리 시 진행 상태 스피너 표시.
- [x] v3.0.3 Windows 11 스마트 앱 컨트롤(Smart App Control) 차단 해결 & 런처 다중화:
  - **차단 원인 규명**: Windows 11 스마트 앱 컨트롤(Enforcement Mode)은 클라우드 평판이 없는 로컬 신규 빌드 바이너리를 탐색기 더블클릭 시 무조건 차단함.
  - **바이너리 재생성 & 아이콘 유지**: `icon_test.go`를 정식으로 포함하여 `rsrc_windows_amd64.syso`를 통한 PE 아이콘 임베딩 정상화 및 빌드 클린업.
- [x] v3.0.4 15,000대 메모리 6~7GB ➔ 400MB 이하 경량화(94% 절감) 및 상단뷰 드로우콜 98% 압축·60fps 최적화:
  - **드로우콜 폭증(104,000회) ➔ 1,200회로 단축**: 15,000대 박스 6분할 재질을 단일 활성 재질로 변경하고, 프레임 및 상판 재질 풀링(`frameMat`, `roofTopMat`) 적용.
  - **상단뷰(Top View) 서버 렌더링 100% 생략**: 지붕(Roof)이 덮고 있는 내부 서버 메쉬 15,000대의 visibility를 꺼서 상단뷰 드로우콜을 104,000회에서 약 1,200회로 98.8% 제거.
  - **18미터 실시간 동적 LOD 갱신**: `animate()` 루프에서 매 10프레임마다 카메라와의 거리를 계산해 시야 범위 밖 원거리 랙의 내부 서버 드로우를 자동 생략.
  - **섀도우맵 패스 생략 및 픽셀 비율 정규화**: `shadowMap.autoUpdate = false`, pixelRatio 1.25x 적용.
- [x] v3.0.5 저사양 PC(i5-10400 / UHD 630) 무지연 300MB 메모리 극대화, Windows 네트워크 공유 폴더(SMB/UNC) 다중 사용자 실시간 동기화, Shift·Q/E 이동속도 토글 및 좌우이동 명칭 표준화:
  - **Windows 네트워크 공유 폴더 실시간 동기화**: `internal/storage/filestore.go`에 `SetDir`, `LoadConfigPath`, Windows SMB 파일 락 방지 원자적 기록(`atomicWrite`) 구현. `internal/server/server.go`에 `/api/config/data-dir` 및 `/api/sync` 추가. `SharedFolderModal` UI 제공.
  - **사내 PC(i5-10400 / UHD 630 / 16GB) 최적화**: 2.6GB에서 300~350MB로 메모리 급감. 동적 LOD 서버 스트리밍(18m populate, 25m purge), 랙 명판 해상도 256x64 스케일링(VRAM 840MB -> 39MB), 상세 서버 텍스처 LRU 120개 캡, 정적 행렬 동결(`matrixAutoUpdate = false`), 인스턴스드 바닥 타일링.
  - **Shift 달리기 / Q/E 눈높이 수직 이동 속도 조절 토글**: HUD 및 상단 툴바에 Shift(`2.0x -> 3.0x -> 4.5x -> 1.2x`), Q/E(`1.0x -> 1.8x -> 3.0x -> 0.5x`) 토글 버튼 탑재.
  - **"게걸음" ➔ "좌우이동" 표준화**: 전체 UI 및 코드 주석 표기 일괄 변경.
  - **단일 실행 파일(`.exe`) 단독 배포본 완성**: 8.48MB `datacenter-viewer.exe` 파일 하나만 복사하면 외부 의존성 없이 즉시 구동 가능.

- [x] v3.0.6 3D 랙 미표시(Invisible Racks) 3대 근본 원인 수정 및 전체뷰/통로뷰 100% 가시성 복원:
  - **`vendor/three.min.js` 영구 백업 및 자동 복원**: `go mod vendor`가 프론트엔드 `vendor/` 폴더를 덮어쓰는 사고를 막기 위해 `frontend_vendor/`에 원본을 영구 보관하고 `build-windows.bat` 및 `embed_test.go`에서 자동 복원·검증.
  - **`rackMeshBuilder.js` 행렬 동결 순서 수정**: `group.position.set(posX, 0, posZ)` 설정 완료 후에 `group.matrixAutoUpdate = false; group.updateMatrix()`가 실행되도록 수정하여 600개 랙이 원점 `(0,0,0)`에 겹치던 현상 해결.
  - **전체 뷰 / 통로 뷰 15,000대 100% 가시성 복원**: `lazyDevices: false`로 복원하여 거리/고도 제한 없이 모든 랙과 서버를 표시하고, 상단 뷰(Top View)에서만 내부 `devGroup.visible = false`로 드로우콜을 절약.
- [x] v3.0.7 유휴 상태(Idle) 메모리 점진적 증가 및 OOM(Out Of Memory) 4대 근본 원인 규명·완전 차단:
  - **1초 주기 무한 자동저장 루프 차단 (`folderManager.js`)**: `store.notify('saveCompleted')`가 범용 `'stateChange'` 이벤트를 트리거하고, `folderManager.js`가 이를 받아 1초 뒤 또 `goSave()`를 실행하던 무한 루프(매 1초마다 600랙 + 15,000대 CSV 1.5MB 직렬화 및 `POST /api/save`)를 적발. 실제 데이터 변경 이벤트 화이트리스트 + `hasUnsavedChanges === true` + `!_isLoadingFromStorage` 조건으로 완전히 차단.
  - **정지 상태 온디맨드 렌더링(`requestRender`) 전환 (`sceneManager.js`)**: 아무 조작이 없을 때도 60fps로 초당 115만 WebGL 드로우콜과 9만 회 `getObjectByName` 트리 순회를 실행하던 루프를 온디맨드 더티 플래그 방식으로 전환. 정지 시 `renderer.render()` 호출 0회, 메모리 증가율 `0 MB/min` 달성.
  - **Fetch Response Stream 즉시 소비 (`folderManager.js`)**: `/api/ping` 및 `/api/save`에서 `await res.text()`를 소비해 미회수 스트림 누적을 차단.
- [x] v3.0.8 신규 PC 단독 실행 시 상단 툴바/패널 미표시(검은 화면) 근본 원인 규명 및 완벽 해결:
  - **원인 규명 (실제 브라우저 재현 완료)**: 새 PC에서 `data/` 폴더 없이 처음 실행하면 초기 샘플 데이터(32랙, 173대 서버)가 로드되어 `massiveScale = false` 모드로 동작함. 이때 173대 서버의 상세 텍스처(약 346장)가 한 번에 생성되면서 `MAX_DETAILED_TEXTURES(120)`를 초과해 1~60번째 서버 메쉬에 바인딩된 활성 텍스처의 `oldTex.image = null`이 호출됨. 직후 `sceneManager.init()` 끝의 `animate() -> renderer.render()`에서 Three.js `safeSetTexture2D`가 `Cannot read properties of null (reading 'complete')` 예외로 크래시되면서 그 아래 줄의 `LeftCabinetList`, `RightElevationPanel`, `TopToolbar` 생성 코드가 실행되지 못해 상단 툴바가 검은색 빈 줄로 남았음.
  - **해결**: `textureGenerator.js`에서 `oldTex.image = null` 대입을 제거(`oldTex.dispose()`만 호출)하고 `MAX_DETAILED_TEXTURES = 450`으로 상향. `app.js`에서 각 UI 컴포넌트 초기화를 독립 `try-catch`로 격리하고 `TopToolbar`를 최우선 초기화하도록 수정. `server_test.go`에서 `exeDir`을 생성하도록 수정하여 `go test` 시 `%APPDATA%\datacenter-viewer\data_path.txt`가 오염되지 않도록 조치.
- [x] v3.0.9 랙 클릭 시 메모리 누적 해결 및 보안 강화 (상세: CHANGELOG v3.0.9):
  - **텍스처 해제 방식 변경**: LRU(450장) → 사용 중 여부 기반 `releaseUnused` + 재질 캐시 정리 + 업로드 후 캔버스 1x1 축소. 15,000대에서 랙 10개 클릭 후 2.1GB → 1.0GB. 해상도/화면은 변경 없음(픽셀 비교 0 차이).
  - **시야 거리 제한 기능은 측정 결과 메모리 효과가 없어(±40MB) 제외** — 다시 시도하지 말 것.
  - **보안**: Go 서버 `guard`(Host/Origin/JSON 검사 + CSP), 패널 XSS 이스케이프, `server.js` 127.0.0.1 바인딩, `filestore_test.go` 가 `%APPDATA%` 를 덮어쓰던 문제 수정.

## 메모 및 다른 모델 인수인계 가이드
1. **절대 주의사항 (`go mod vendor` 금지 & `tex.image = null` 금지)**:
   - 루트 폴더의 `vendor/`에는 프론트엔드 3D 엔진(`three.min.js`, `OrbitControls.js`, `CSS2DRenderer.js`, `papaparse.min.js`)이 들어 있음. `build-windows.bat`이 매 빌드마다 `frontend_vendor/*.js`에서 자동 복원하지만, 불필요하게 `go mod vendor`를 실행하지 말 것.
   - `Three.CanvasTexture` 객체를 캐시에서 제거할 때 `tex.dispose()`만 호출해야 하며, **`tex.image = null`을 설정하면 절대 안 됨** (현재 씬의 메쉬가 해당 텍스처를 참조 중일 때 Three.js `safeSetTexture2D`가 `image.complete`를 읽다 즉시 크래시함).
2. **대규모 환경(15,000대 / 600랙) 3D 최적화 및 온디맨드 렌더링 원리**:
   - `textureGenerator.js`: `massiveScale && !isDetailed` 조건일 때 `srv_front_tpl_...` 템플릿 키를 사용해 12개 텍스처만 생성.
   - `deviceMeshBuilder.js`: `options.isDetailed` 값을 `textureGenerator`로 전달.
   - `sceneManager.js`:
     - `rackSelected` 이벤트 시 선택된 랙만 `rebuildSingleRack`을 호출하여 상세 텍스처로 전환.
     - 카메라나 씬에 변화가 있을 때만 `this.requestRender(frames)`를 호출하여 렌더링하며, 유휴 상태에서는 `renderer.render()`를 호출하지 않아 CPU/GPU 점유율 0% 및 메모리 누수 0을 유지함. 새로운 애니메이션이나 씬 변경 로직을 추가할 때는 반드시 `this.requestRender()`를 함께 호출할 것.
3. **자동저장(`folderManager.js`) 이벤트 가드 원리**:
   - `store.js`의 `notify(event, data)`는 모든 이벤트(단순 선택 `rackSelected`, 저장 완료 `saveCompleted` 포함) 뒤에 `'stateChange'`를 브로드캐스트함. 따라서 `'stateChange'` 구독자에서 무조건 `scheduleAutoSave()`를 호출하면 무한 저장 루프에 빠짐. 반드시 실제 데이터 변경 이벤트이고 `store.state.hasUnsavedChanges === true`일 때만 저장을 예약해야 함.
4. **Windows 실행 파일 아이콘 빌드 원리**:
   - `icon_test.go`가 `icon.ico`를 읽어 `cmd/datacenter-viewer/rsrc_windows_amd64.syso`를 자동 생성함.
   - `build-windows.bat`이 `go test`를 거치면서 항상 `.syso`를 최신으로 유지하고, `go build`가 이를 자동으로 `.exe` PE 리소스에 링크함.
5. **Windows 11 스마트 앱 컨트롤(Smart App Control) 환경 구동 가이드**:
   - 직접 `.exe` 더블클릭을 원할 경우: `Windows 보안 > 앱 및 브라우저 컨트롤 > 스마트 앱 컨트롤 설정`에서 '꺼짐' 선택.
   - 보안 설정을 변경하지 않고 바로 실행하려면: `datacenter-viewer.bat` 또는 `datacenter-viewer.lnk`를 더블클릭하면 팝업 없이 브라우저 창으로 즉시 실행됨.
6. **텍스처 메모리 해제 원리 (v3.0.9)**:
   - 텍스처는 `getOrCreateTexture` 에서 `texture.onUpdate` 로 GPU 업로드 직후 캔버스를 1x1 로 줄인다. 따라서 **같은 텍스처를 다시 업로드(`needsUpdate = true`)하면 빈 텍스처가 된다** — 내용을 바꾸려면 새 키로 새 텍스처를 만들 것.
   - 해제는 `sceneManager._sweepTextures()` → `textureGenerator.releaseUnused()` 만 수행한다. 랙을 다시 만드는 코드를 추가하면 `this._scheduleTextureSweep()` 을 호출할 것.
   - 새 재질 캐시를 만들면 `dropMaterialsForTexture(tex)` 도 구현해 `_dropMaterialsForTexture` 에 연결할 것 (안 하면 텍스처가 GC 되지 않음).
7. **로컬 API 보안 원리 (v3.0.9)**:
   - `internal/server/server.go` 의 `guard` 가 모든 요청을 감싼다. 새 POST API 는 프론트에서 반드시 `Content-Type: application/json` 으로 보낼 것 (아니면 415).
   - CSP 가 `script-src 'self'` 이므로 인라인 `<script>` / `onclick=""` 속성은 동작하지 않는다. 이벤트는 `addEventListener` 로 연결할 것.
   - CSV/공유 폴더에서 온 문자열을 `innerHTML` 에 넣을 때는 반드시 `escapeHtml()` 을 거칠 것.
8. **초기 랙 상태(초반 상태) 리셋 메커니즘**:
   - `초기_상태로_리셋.bat` 실행 시 기존 대용량 데이터를 `data/backup_15000/`으로 백업하고, `sample_data/datacenter_layout_initial.json`(32개 기본 랙) 및 빈 `allocated_hosts.csv`로 원복함.
   - 웹 UI 상단 툴바의 **[🔄 초기 랙 상태로 리셋]** 버튼(`store.resetToInitialState()`)을 통해서도 실시간으로 즉시 리셋 후 신규 CSV 업로드 가능.

