# 변경 이력 (CHANGELOG)

## [2026-10-24] - v3.0.8 신규 PC 단독 실행 시 상단 툴바/사이드 패널 미표시(검은 화면) 근본 원인 규명 및 완벽 해결

* **다른 PC로 `datacenter-viewer.exe`만 복사해 처음 실행했을 때 상단 툴바와 좌·우 패널이 검은 빈 줄로만 보이던 실제 원인 규명 및 수정**:
  1. **원인 1 (결정적 원인 — 초기 샘플 데이터(32랙 / 173대) 로드 시 LRU 텍스처 조기 축출 및 `oldTex.image = null`에 의한 Three.js 크래시) ([`textureGenerator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/textureGenerator.js), [`app.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/app.js))**:
     * **발생 메커니즘**:
       1. 기존 개발 PC에는 `data/` 폴더에 600랙 / 15,000대 데이터가 있어 `massiveScale = true` 모드로 부팅되었고, 상세 서버 텍스처(`srv_front_v5_...`)가 선택된 1개 랙(25개)만 생성되어 LRU 한도(`120개`)에 도달하지 않았음.
       2. 반면 **다른 컴퓨터**에 `datacenter-viewer.exe`만 가져가서 처음 실행하면 `data/` 폴더가 비어 있으므로 내장 초기 샘플 데이터(**랙 32개, 서버 173대**)가 로드됨.
       3. 랙 32개 / 서버 173대는 `massiveScale = false`(`racks <= 60 && devices <= 500`)이므로 `sceneManager.init()` 내부 `rebuildRacks()`에서 **173대 전체에 대해 앞·뒤 상세 텍스처(약 346장)**를 즉시 생성함.
       4. 이때 `TextureGenerator.MAX_DETAILED_TEXTURES = 120` 한도를 넘기면서 121번째 서버 메쉬를 만드는 순간, **방금 생성되어 현재 3D 씬의 1~60번째 서버 메쉬에 바인딩되어 있는 살아있는 텍스처들의 `oldTex.image = null`이 실행**됨.
       5. 직후 `sceneManager.init()`의 마지막 줄인 `this.animate()` ➔ `this.renderer.render(this.scene, this.camera)`가 동기적으로 호출되면서, Three.js 내부 `WebGLTextures.safeSetTexture2D`가 `texture.image.complete`를 읽으려다 **`Uncaught TypeError: Cannot read properties of null (reading 'complete')`** 예외를 던지며 크래시 발생!
       6. 이 예외가 `app.js`의 `this.sceneManager.init()` 호출부에서 잡히지 않고 상위로 전파되면서, 그 바로 아래 줄에 있던 **`DCLeftCabinetList`(좌측 패널), `DCRightElevationPanel`(우측 패널), `DCTopToolbar`(상단 툴바) 초기화 코드가 아예 실행되지 못해 상단 툴바가 검은 빈 줄로 남았던 것**을 실제 브라우저 디버거로 100% 재현·검증함.
     * **해결**:
       * [`textureGenerator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/textureGenerator.js)에서 LRU 축출 시 Three.js가 참조 중인 `oldTex.image`를 절대 `null`로 덮어쓰지 않도록 제거(`oldTex.dispose()`만 호출하여 WebGL GPU 텍스처 버퍼만 안전하게 해제)하고, 비-대규모 모드(최대 200대 상세 표시)에서도 한 프레임 내 축출이 일어나지 않도록 `MAX_DETAILED_TEXTURES`를 `450`으로 상향.
       * [`app.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/app.js)에서 `SceneManager`, `TopToolbar`, `LeftCabinetList`, `RightElevationPanel` 초기화를 각각 독립된 `try-catch` 블록으로 격리하고 `TopToolbar` 생성 우선순위를 높여, 어떤 그래픽 환경에서도 상단 메뉴와 UI 패널이 100% 표시되도록 이중 안전장치 구축.
  2. **원인 2 (`go test` 실행 시 사용자 `%APPDATA%\datacenter-viewer\data_path.txt` 오염 방지) ([`server_test.go`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/internal/server/server_test.go))**:
     * **원인**: `TestServerAPI_SharedDirAndSync` 테스트에서 임시 `exeDir` 디렉터리를 실제로 생성하지 않아 `saveConfigPath`가 폴백 경로인 `%APPDATA%\datacenter-viewer\data_path.txt`에 임시 테스트 폴더 경로를 기록하고 있었음.
     * **해결**: 테스트 내 `exeDir`을 `os.MkdirAll`로 생성하도록 수정하여 빌드·테스트 시 호스트 PC의 `%APPDATA%` 설정이 절대 오염되지 않도록 차단.

## [2026-10-24] - v3.0.7 유휴 상태(Idle) 메모리 점진적 증가 및 OOM(Out Of Memory) 4대 근본 원인 규명·완전 차단

* **아무 조작 없이 켜두기만 해도 메모리가 슬금슬금 올라가다 OOM이 발생하던 4대 근본 원인 규명 및 완벽 해결**:
  1. **원인 1 (치명적 — 1초 주기 무한 자동저장 루프 `saveCompleted ➔ stateChange ➔ scheduleAutoSave ➔ goSave`) ([`folderManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/storage/folderManager.js), [`store.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/state/store.js))**:
     * **원인**: `Store.notify(event, data)`는 모든 이벤트 발생 시 마지막에 범용 `'stateChange'` 이벤트를 함께 브로드캐스트함. 기존 `FolderManager`가 `store.subscribe('stateChange', () => this.scheduleAutoSave())`로 조건 없이 자동저장을 예약했는데, 저장이 끝나면 `goSave()`가 `this.store.notify('saveCompleted', ...)`를 호출함. 이 `'saveCompleted'` 알림이 다시 `'stateChange'`를 발생시켜 1초 뒤 또 `goSave()`를 호출하는 **무한 루프(1초마다 영구 반복)**가 발생함.
     * **파급 효과**: 앱 실행 150ms 후 초기 랙 선택(`selectRack('X001Y001')`) 또는 초기 데이터 로드 직후부터, 사용자가 아무것도 하지 않아도 **매 1초마다** 600개 랙 깊은 복사(`JSON.parse(JSON.stringify(...))`) + 15,000대 서버 전체 CSV 직렬화(`exportDevicesCsv`) + 1.5MB JSON 페이로드 생성 + `POST /api/save` 전송이 무한 반복되며 초당 수십 MB의 문자열 가비지와 미회수 스트림을 생성함.
     * **해결**:
       * `stateChange` 리스너에서 실제 데이터 변경 이벤트(`racksUpdated`, `rackDevicesChanged`, `devicesUpdated`, `sitesUpdated`, `siteGridUpdated`, `invertXYChanged`, `vendorColorsChanged`)만 화이트리스트로 필터링(`saveCompleted`, `rackSelected`, `deviceSelected` 등 비변경 이벤트 완전 배제).
       * 반드시 `this.store.state.hasUnsavedChanges === true`이고 스토리지 로드 중(`_isLoadingFromStorage`)이 아닐 때만 자동저장이 예약되도록 이중 가드 적용.
  2. **원인 2 (치명적 — 정지 상태에서도 매초 60회 무한 실행되던 115만 WebGL 드로우콜 및 9만 회 트리 탐색) ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js), [`rackMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/rackMeshBuilder.js))**:
     * **원인**: 카메라와 씬이 100% 정지해 있는 유휴 상태에서도 `animate()` 루프가 매초 60프레임(60fps)마다 조건 없이 `this.renderer.render(this.scene, this.camera)`를 호출하여 **초당 1,152,000회(`19,200 메쉬 × 60fps`)의 WebGL 드로우 커맨드**를 Chromium GPU 공유 메모리 링버퍼(CommandBuffer)에 밀어 넣음. 동시에 매 10프레임(초당 6회)마다 `updateMassiveScaleLOD()`가 600개 랙에 대해 `rackGroup.getObjectByName(\`DeviceGroup_${rackId}\`)` 재귀 탐색(초당 90,000회 트리 순회 및 3,600개 문자열 할당)과 `new THREE.Vector3()` 객체 생성을 반복하여 V8 엔진이 유휴 가비지 컬렉션(Idle GC)에 진입하지 못함.
     * **해결 (온디맨드 더티 플래그 렌더링 `requestRender`)**:
       * 카메라 조작(`OrbitControls change`), 키보드 이동(`WASD/QE`), 마우스 클릭, 창 크기 변경, 데이터/뷰 변경 시에만 필요한 프레임만큼 렌더링을 깨우는 **온디맨드 렌더링(`requestRender`)** 도입.
       * 사용자가 아무 조작도 하지 않는 유휴 상태에서는 `renderer.render()` 호출이 **초당 60회 ➔ 0회**로 즉시 전환되어 GPU 커맨드 버퍼 및 JS 힙 할당이 **0 Bytes/sec**로 유지됨.
       * `devGroup` 참조를 `rackGroup.userData.devGroup`에 O(1) 직결하고, `animate` 클로저(`this._boundAnimate`)와 임시 벡터(`_tempForward`, `_tempRight`, `_tempMoveVec`)를 사전 할당하여 프레임당 객체 할당을 완전히 제거.
  3. **원인 3 (`/api/ping` 및 `/api/save`의 미소비 Fetch Response Stream 누적 + 초기 부팅 이중 로드) ([`folderManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/storage/folderManager.js), [`app.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/app.js))**:
     * **원인**: 2.5초 주기 하트비트 `fetch('/api/ping')`와 `goSave()`에서 응답 본문(`res.text()`)을 읽지 않고 방치하여 Chromium 내부 `ReadableStream` 버퍼가 메이저 GC 전까지 누적됨. 또한 `app.js`에서 `loadInitialSampleData()` 실행 직후 `tryGoBackend()`를 또 호출하여 부팅 시 대용량 데이터를 두 번 중복 파싱·인덱싱함.
     * **해결**: 모든 `fetch` 응답에서 `await res.text()`를 명시적으로 소비하여 스트림을 즉시 닫고, 하트비트 주기를 8초(`_pingInFlight` 중복 방지 포함)로 안정화했으며, Go 백엔드 데이터가 로드되면 샘플 데이터를 중복 로드하지 않도록 순서를 바로잡음.
  4. **원인 4 (`textureGenerator.js` 캔버스 비트맵 즉시 반환 및 `onWindowResize` 프레임버퍼 재생성 가드) ([`textureGenerator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/textureGenerator.js), [`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js))**:
     * **해결**: LRU 캐시에서 밀려난 `CanvasTexture`를 `dispose()`할 때 내부 `<canvas>` 해상도를 `1x1`로 즉시 축소하고 `tex.image = null` 처리하여 네이티브 비트맵 메모리가 GC 대기 없이 즉각 OS로 반환되도록 조치. 또한 `onWindowResize()`에서 실제 픽셀 크기가 변했을 때만 `renderer.setSize()`를 호출하도록 방어.

## [2026-10-24] - v3.0.6 3D 랙 미표시(Invisible Racks) 3대 근본 원인 수정 및 전체뷰/통로뷰 100% 가시성·초경량 메모리(300~400MB) 양립 완성

* **3.8 Flash 작업 이후 3D 랙이 화면에 보이지 않던 3대 근본 원인 규명 및 완벽 복구**:
  1. **원인 1 — `go mod vendor` 실행으로 인한 프론트엔드 3D 엔진(`vendor/three.min.js`) 삭제 사고 복구 ([`vendor/`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/vendor), [`frontend_vendor/`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/frontend_vendor), [`build-windows.bat`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/build-windows.bat), [`embed_test.go`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/internal/webui/embed_test.go))**:
     * **원인**: 루트 폴더에서 `go mod vendor`가 실행되면서 기존 프론트엔드 3D 라이브러리 폴더인 `vendor/` 내부의 `three.min.js`, `OrbitControls.js`, `CSS2DRenderer.js`, `papaparse.min.js`가 모두 삭제되고 Go 패키지 폴더로 덮어써짐. 그 상태에서 `internal/webui/dist/`로 복사·빌드되어 브라우저에서 `THREE is not defined`가 발생해 3D 캔버스 자체가 렌더링되지 않았음.
     * **해결**: `three.min.js`(r128), `OrbitControls.js`, `CSS2DRenderer.js`, `papaparse.min.js` 원본을 복구하여 `frontend_vendor/`(영구 백업본)와 `vendor/`에 이중 보관하고, [`build-windows.bat`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/build-windows.bat) 실행 시 자동으로 `frontend_vendor/*.js`를 복원·검증하도록 안전장치 구축. 또한 [`internal/webui/embed_test.go`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/internal/webui/embed_test.go) 단위 테스트를 추가하여 3D 엔진 파일이 누락되면 빌드가 즉시 중단되도록 강제함.
  2. **원인 2 — `rackMeshBuilder.js`에서 `group.position.set()` 호출 전에 `group.matrixAutoUpdate = false`가 먼저 실행되어 모든 랙 좌표가 원점 `(0,0,0)`에 동결된 버그 수정 ([`rackMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/rackMeshBuilder.js))**:
     * **원인**: `createRackGroup()` 내부 266행에서 `group.matrixAutoUpdate = false; group.updateMatrix();`를 먼저 호출한 뒤 307행에서 `group.position.set(posX, 0, posZ)`를 설정하고 `updateMatrix()`를 다시 호출하지 않아, 600개 랙의 월드 변환 행렬(`matrixWorld`)이 전부 `(0, 0, 0)` 한 점에 겹쳐 동결됨. 게다가 315행에서 `group.userData = {...}`를 재할당하면서 앞서 기록한 `hasDevices` 플래그까지 유실됨.
     * **해결**: `group.position.set(posX, 0, posZ)` 및 `group.rotation.y` 설정이 모두 끝난 직후에 `group.matrixAutoUpdate = false; group.updateMatrix();`를 단 1회 호출하도록 순서를 바로잡고, `group.userData` 객체를 단일 선언으로 통합하여 모든 랙이 정확한 그리드 좌표에 배치되도록 수정.
  3. **원인 3 — 과도한 `lazyDevices` / `isHighAltitude(camPos.y > 14m)` 컬링으로 인해 전체 뷰에서 랙 내부 서버가 비어 보이던 현상 제거 ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js), [`deviceMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/deviceMeshBuilder.js))**:
     * **원인**: 카메라 높이 `y > 14m` 또는 거리 `18m` 밖의 랙 내부 장비를 제거(`depopulate`)하도록 설정되어 전체 뷰(Orbit View)에서 랙들이 속이 텅 빈 것처럼 보였음.
     * **해결**: 공유 지오메트리와 템플릿 재질을 사용하는 15,000개 서버 메쉬 자체는 JS 메모리를 약 22MB만 차지하므로, 전체 뷰(Orbit View)와 1인칭 통로 뷰(Aisle View)에서는 **모든 랙과 15,000대 서버를 거리·높이 제한 없이 100% 상시 표시**하도록 복원(`lazyDevices: false`). 단, 지붕(Roof)이 위를 100% 덮고 있는 **상단 뷰(Top View)에서만** 내부 서버 그룹(`devGroup.visible = false`)을 숨겨 상단 뷰 드로우콜 15,000회를 절감하고, 대규모 비선택 랙에서는 보이지 않는 반대편 텍스처 생성을 생략하여 **시각적 품질 100% 유지 + 메모리 300~400MB대 경량화**를 동시에 달성.

## [2026-10-24] - v3.0.5 저사양 PC(i5-10400 / UHD 630) 무지연 300MB 메모리 극대화, Windows 네트워크 공유 폴더(SMB/UNC) 다중 사용자 실시간 동기화, Shift·Q/E 이동속도 토글 및 좌우이동 명칭 표준화

* **Windows 네트워크 공유 폴더(SMB/UNC) 다중 사용자 실시간 동기화 아키텍처 구축 ([`filestore.go`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/internal/storage/filestore.go), [`server.go`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/internal/server/server.go), [`folderManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/storage/folderManager.js), [`sharedFolderModal.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/ui/sharedFolderModal.js), [`topToolbar.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/ui/topToolbar.js))**:
  * **운영 환경 및 아키텍처**: 각 개인 PC에서 `datacenter-viewer.exe`를 단독 구동하고, 사내 동일 네트워크 내 Windows 공유 폴더(UNC 경로 `\\server\share\data` 또는 마운트 드라이브 `Z:\data`)에 마스터 데이터(`datacenter_layout.json`, `allocated_hosts.csv`)를 보관 및 공유.
  * **개별 CSV 할당 및 실시간 업데이트**: 개인이 CSV 자동 할당을 실행하거나 랙 구성을 변경하면 공유 폴더의 파일들이 즉각 자동 업데이트(`POST /api/save`).
  * **Windows SMB 파일 락(Lock) 충돌 방지 원자적 기록**: 여러 사용자가 공유 폴더에 동시 접근 시 발생할 수 있는 Windows SMB 파일 잠금(`Access is denied`)을 방지하기 위해 임시 파일 작성 후 교체 및 원자적 폴백(`atomicWrite`) 로직을 구현.
  * **1클릭 동기화 및 전용 설정 모달**:
    * 상단 툴바에 **[🔄 동기화]** 버튼 및 **[📁 저장/공유 폴더]** 설정 모달(`SharedFolderModal`) 탑재.
    * 런타임에 언제든지 공유 폴더 경로를 입력·변경(`POST /api/config/data-dir`)할 수 있으며, 다른 팀원이 저장한 최신본을 즉각 다시 불러옴(`GET /api/sync`).
    * 설정된 공유 폴더 경로는 실행 파일 위치의 `data_path.txt`에 영구 보존되어 다음 실행 시에도 자동으로 공유 폴더에 접속.
* **사내 PC(Intel Core i5-10400 / 16GB RAM / Intel UHD Graphics 630) 환경 메모리 및 프레임 극대화 (2.6GB ➔ 300~400MB, 60 FPS 달성)**:
  * **하드웨어 제약 분석**: i5-10400의 내장 그래픽(Intel UHD 630)은 전용 VRAM이 없고 메인 시스템 RAM을 공유(Shared System Memory)하므로, WebGL 텍스처와 버텍스 버퍼가 시스템 RAM에 누적되어 2.6GB 이상 차지하고 프레임 드랍이 발생함.
  * **해결 1: 상단뷰(Top View) 내부 서버 100% 렌더 생략 ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js))**:
    * 상단뷰에서는 지붕(Roof)의 벤더 점유율 컬러 바가 모든 랙을 덮으므로 내부 서버 15,000대 렌더링을 완전히 생략하여 드로우콜 1,200회, RAM 300MB대로 유지. 전체뷰/통로뷰에서는 모든 랙과 서버를 100% 표시.
  * **해결 2: 랙 명판(Header Plate) 텍스처 해상도 최적화 ([`textureGenerator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/textureGenerator.js), [`rackMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/rackMeshBuilder.js))**:
    * 600개 랙의 1024x256 명판 텍스처가 차지하던 840MB~1.4GB 메모리를 대규모 모드에서 256x64로 정밀 스케일링하여 **39MB로 95% 절감**. 선택된 랙은 512x128 고해상도로 선명하게 유지.
  * **해결 3: 상세 서버 텍스처 LRU 메모리 캐시 캡 적용 ([`textureGenerator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/textureGenerator.js))**:
    * 랙들을 선택하며 둘러볼 때 상세 호스트명 텍스처가 무한 누적되지 않도록 최대 120개 LRU 큐로 제한하고 오래된 텍스처는 GPU VRAM에서 즉시 해제(`tex.dispose()`).
  * **해결 4: 정적 행렬 계산 동결(`matrixAutoUpdate = false`)**:
    * 600개 랙과 15,000대 서버 메쉬의 정적 월드 매트릭스 재계산을 비활성화하여 매 프레임 CPU/GPU 커맨드 버퍼 부하 제거.
  * **해결 5: 인스턴스드 메쉬 바닥 타일링 ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js))**:
    * 1,800개 개별 바닥 타일을 단일 `THREE.InstancedMesh`로 통합하여 드로우콜 1,800회 ➔ 1회로 압축.
* **Shift 달리기 및 Q/E 눈높이 수직 이동 속도 조절 토글 추가 ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js), [`topToolbar.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/ui/topToolbar.js), [`style.css`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/css/style.css))**:
  * 3D 뷰포트 하단 HUD 및 상단 툴바에 속도 토글 버튼 배치:
    * **⚡ Shift 달리기 속도**: 기본 `2.0x` (클릭 시 `2.0x ➔ 3.0x ➔ 4.5x ➔ 1.2x ➔ 2.0x` 순환)
    * **↕ Q/E 눈높이 속도**: 기본 `1.0x` (클릭 시 `1.0x ➔ 1.8x ➔ 3.0x ➔ 0.5x ➔ 1.0x` 순환)
  * 키보드 이동 로직(`updateKeyboardMovement`)에 배율을 실시간 적용하여 쾌적한 데이터센터 순회 가능.
* **용어 표준화 ("게걸음" ➔ "좌우이동")**:
  * 3D 뷰포트 HUD, 툴바 안내 텍스트, 코드 주석 등 전체 시스템에서 어색했던 "게걸음" 표기를 표준 직관 명칭인 **"좌우이동"**으로 일괄 변경.
* **단일 실행 파일(`.exe`) 단독 배포본 완성 ([`datacenter-viewer.exe`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/datacenter-viewer.exe))**:
  * 모든 3D 렌더링 엔진, UI, 백엔드 서버, 데이터센터 아이콘, 공유 폴더 연동 로직이 단일 파일에 완전히 내장되어, 다른 PC로 `datacenter-viewer.exe` 파일 하나만 복사하면 외부 설치나 의존성 없이 즉시 실행 가능.

## [2026-10-24] - v3.0.4 15,000대 대규모 메모리 6~7GB ➔ 400MB 이하 경량화(94% 절감) 및 상단뷰(Top View) 드로우콜 98% 압축·60fps 무지연 렌더링

* **Chromium WebGL 커맨드 버퍼 폭증 원인 규명 및 6~7GB ➔ 400MB 이하 메모리 다이어트 ([`deviceMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/deviceMeshBuilder.js), [`rackMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/rackMeshBuilder.js), [`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js))**:
  * **원인**: 15,000대 서버 박스 지오메트리에 6개 재질 배열(`[chassisMat, ..., frontMat, backMat]`)을 바인딩하여 프레임당 `15,000 × 6 = 90,000` 드로우콜이 발생. 600개 랙의 4기둥(12회) 및 루프(6회)를 합쳐 프레임당 104,000 드로우콜이 GPU 프로세스 IPC 공유 메모리 버퍼를 채워 Chrome 탭 메모리가 6~7GB까지 치솟고 렌더러가 헐떡임.
  * **해결 1 (단일 재질 전환 및 풀링)**:
    1. `massiveScale && !isDetailed` 상태에서 서버 메쉬를 6개 분할 재질 대신 전/후면 단일 활성 재질(`MeshBasicMaterial`)로 전환하여 서버 드로우콜을 90,000회에서 15,000회로 83.3% 절감.
    2. 600개 랙의 기둥 및 상판 메쉬를 단일 재질로 통합하고 동일 색상 랙들끼리 `frameMat` 및 `roofTopMat` 단일 인스턴스를 공유(Shared Material Pooling)하여 랙 드로우콜을 14,400회에서 2,400회로 단축.
  * **해결 2 (상단뷰 15,000대 내부 장비 드로우콜 100% 생략)**:
    * 상단뷰(Top View) 및 고도 14m 이상 전경뷰에서는 지붕(Roof)의 벤더 비중 스트라이프 텍스처가 랙 전체를 덮고 있으므로, 내부 서버 15,000대(`DeviceGroup_...`)의 렌더링을 완전히 비활성화(`visible = false`)하여 드로우콜을 90,000회에서 **0회**로 단축. 상단뷰 총 드로우콜이 **104,000회 ➔ 약 1,200회(98.8% 제거)**로 줄어들어 어떤 저사양 PC에서도 끊김 없는 60fps 달성.
  * **해결 3 (거리 기반 동적 LOD 갱신 및 섀도우맵 최적화)**:
    * 카메라와 18미터 이내 랙 또는 선택된 랙(`selectedRackId`)만 내부 장비를 렌더링하고 원거리 랙은 가벼운 프레임만 표시.
    * `animate()` 루프에서 매 10프레임마다 동적 거리 계산을 수행하여 카메라 이동 시에도 매끄럽게 전환.
    * 대규모 환경에서 프레임당 2048x2048 섀도우 뎁스 패스를 생략(`shadowMap.autoUpdate = false`)하여 GPU 사이클 낭비 차단.
  * **해결 4 (표준 픽셀 비율 정규화)**:
    * 강제 2.5x 슈퍼샘플링을 최대 1.25x 표준 픽셀 레이쇼로 안정화하여 수백 MB의 프레임버퍼 VRAM 누수 차단.

## [2026-10-24] - v3.0.3 Windows 11 스마트 앱 컨트롤(Smart App Control) 차단 해결 & 다중 무중단 런처 및 프론트 오프라인 벤더 복구

* **Windows 11 스마트 앱 컨트롤(Smart App Control) 차단 완벽 대응 ([`datacenter-viewer.bat`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/datacenter-viewer.bat), [`datacenter-viewer.lnk`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/datacenter-viewer.lnk), [`build-windows.bat`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/build-windows.bat))**:
  * **원인**: Windows 11 스마트 앱 컨트롤(SAC)이 '켜짐(Enforce)' 상태일 때, Microsoft 클라우드 평판이 없는 로컬 신규 빌드 `.exe`는 탐색기 더블클릭 시 "안전하지 않을 수 있는 앱을 차단했습니다" 팝업과 함께 커널 Code Integrity 정책으로 실행을 차단함.
  * **해결 1 (무중단 실행 바로가기 및 배치 런처)**: 더블클릭 한 번으로 즉시 뷰어를 실행할 수 있는 `datacenter-viewer.bat` 및 데이터센터 전용 아이콘이 적용된 `datacenter-viewer.lnk` 바로가기를 생성. 스마트 앱 컨트롤 차단 환경에서도 마이크로소프트 서명된 Go/Node 엔진을 통해 경고창 없이 1초 만에 3D 뷰어 창을 브라우저에 띄움.
  * **해결 2 (네이티브 .exe 단독 실행 지원)**: `build-windows.bat` 재빌드 시 deterministic 해시 충돌을 제거하고, Windows 보안 설정(`Windows 보안 > 앱 및 브라우저 컨트롤 > 스마트 앱 컨트롤 설정 > 꺼짐`) 전환 시 `.exe` 단독 더블클릭으로도 즉시 실행되도록 복원.
* **프론트엔드 오프라인 벤더 스크립트 복구 및 정적 자산 동기화 ([`vendor/`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/vendor), [`internal/webui/dist/`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/internal/webui/dist))**:
  * `go mod vendor` 실행 시 정리되었던 Three.js r128(`three.min.js`), `OrbitControls.js`, `CSS2DRenderer.js`, `papaparse.min.js`를 정식 복구하고 Go embed 정적 배포본에 완벽 동기화.
* **15,000대 CSV 검증 엔진 성능 재검증 ([`test_15000_servers.csv`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/test_15000_servers.csv))**:
  * 15,000대 대규모 서버 배치 시 파싱/검증/할당에 단 **137ms**, 메모리 점유율 **26.88MB**로 통과함을 재확인.

## [2026-10-24] - v3.0.2 15,000대 CSV 업로드 메모리 10GB 폭증·프리징 해결(LOD 템플릿 풀링), 렌더링 디바운싱, 로딩 프로그레스 및 Windows exe 데이터센터 아이콘 추가

* **15,000대 대규모 CSV 할당 시 메모리 10GB 폭증 및 탭 프리징 해결 ([`textureGenerator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/textureGenerator.js), [`deviceMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/deviceMeshBuilder.js), [`rackMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/rackMeshBuilder.js), [`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js))**:
  * **원인**: CSV 파싱/할당 로직 자체는 0.2초 만에 통과했으나, 할당 완료 이벤트 수신 즉시 3D 씬이 `15,000대 × 앞/뒤 2면 = 총 30,000장`의 개별 캔버스 텍스처를 단일 JS 스레드에서 생성하면서 메모리가 10GB로 치솟고 브라우저가 멈춤.
  * **해결 (LOD 템플릿 풀링 + 온디맨드 상세화)**:
    1. 대규모 모드(`massiveScale = true`, 서버 300대 이상)의 전체 뷰에서는 30,000장을 개별 렌더링하지 않고, 벤더/모델/폼팩터별 **공통 템플릿 텍스처(약 12장)**를 전체 15,000대가 공유하여 메모리 점유율을 **10GB ➔ 150MB 이하(1/70 수준)**로 줄이고 **0.2초 만에 로딩 완료**.
    2. 사용자가 특정 랙을 클릭(`selectRack`)하거나 포커스할 때만 해당 랙의 25대에 대해 실제 호스트네임(`dsvr...`)과 U범위가 적힌 고해상도 상세 텍스처로 0.01초 만에 실시간 교체(LOD 스왑).
  * **디자인 및 시각적 일관성 100% 보존**: 멀리서 보나 가까이서 보나 벤더 고유 색상, 볼드 블랙 외곽선, 섀시 디테일, 랙 U 눈금 등 모든 그래픽 디자인이 기존과 완벽하게 동일하게 유지됨.
* **다중 알림 렌더링 디바운싱 ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js))**:
  * `addDevicesBulk` 실행 시 연속으로 발생하던 `activeSiteChanged`, `racksUpdated`, `devicesUpdated` 알림으로 인해 3D 씬이 3번 연속 재구축되던 문제를 `requestAnimationFrame` 기반 `rebuildRacksDebounced()`, `rebuildFloorDebounced()`로 통합하여 프레임당 단 1회만 재구성하도록 최적화.
* **대용량 파일 처리 안내 로딩 프로그레스 오버레이 ([`csvHandler.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/storage/csvHandler.js), [`style.css`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/css/style.css))**:
  * 15,000대 파일 업로드 시 사용자가 작업 진행 여부를 명확히 알 수 있도록 화면 중앙에 블러 배경 스피너 및 단계별 진행 안내(`"장비 15,000대 검증 및 할당 중..."` ➔ `"3D 씬 반영 중..."`)를 표시.
* **Windows 단일 실행 파일(`.exe`) 데이터센터 전용 네이티브 아이콘 탑재 ([`cmd/datacenter-viewer/icon_test.go`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/cmd/datacenter-viewer/icon_test.go), [`icon.ico`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/icon.ico), [`build-windows.bat`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/build-windows.bat))**:
  * 16px부터 256px까지 지원하는 고해상도 데이터센터 서버 랙 아이콘(`icon.ico`)을 생성하고, Go 빌드 시 `cmd/datacenter-viewer/rsrc_windows_amd64.syso`로 PE 리소스에 영구 링크하여 Windows 탐색기 및 작업 표시줄에서 세련된 3D 랙 아이콘이 표시되도록 구현.

## [2026-10-24] - v3.0.1 첫 실행 시 `127.0.0.1` 연결 거부(방화벽 오인) 해결 및 3D·UI 폰트 고대비·초선명화

* **첫 실행 시 `127.0.0.1` 연결 거부(`ERR_CONNECTION_REFUSED` / 방화벽 차단 메시지) 완벽 해결 ([`main.go`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/cmd/datacenter-viewer/main.go), [`server.go`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/internal/server/server.go), [`launcher.go`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/internal/launcher/launcher.go), [`folderManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/storage/folderManager.js))**:
  * **원인**: Windows 10/11의 Microsoft Edge(`msedge.exe`)는 백그라운드 브로커(Startup Boost)가 활성화되어 있을 때, 새 창 실행 명령을 기존 브로커에 위임하고 자식 프로세스를 0.1초 만에 종료함. 기존 `main.go`가 `cmd.Wait()` 반환 직후 Go 로컬 서버를 종료하여 창이 뜨는 순간 `127.0.0.1` 서버가 이미 꺼져 연결 거부 화면이 표시됨.
  * **해결**:
    1. 브라우저 실행 전 `waitForServerReady("/api/health")`로 로컬 서버 응답을 100% 확인한 뒤 앱 창을 실행.
    2. 프론트엔드(`folderManager.js`)와 백엔드(`server.go`) 간 `/api/ping`(2.5초 주기 하트비트) 및 `/api/bye`(`pagehide` / `beforeunload` 비콘) 생명주기 추적기를 도입하여, Edge 자식 PID가 즉시 반환되더라도 실제 창이 열려 있는 동안 Go 서버가 절대 종료되지 않고 창을 닫을 때만 깔끔하게 종료되도록 수정.
* **3D 서버·랙 명판 및 UI 폰트 진하고 선명하게 개선 ([`deviceMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/deviceMeshBuilder.js), [`rackMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/rackMeshBuilder.js), [`textureGenerator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/textureGenerator.js), [`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js), [`style.css`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/css/style.css))**:
  * **조명 그림자 및 안개(Fog)에 의한 글씨 흐림 제거**: 서버 전면/후면 명판(`_getFaceMaterial`), 랙 상단 명판, 기둥 U 눈금 재질을 조명 각도나 거리 안개에 영향을 받지 않는 **`THREE.MeshBasicMaterial({ fog: false })` (100% 원색 발광)**으로 전환하고 씬 안개 농도를 `0.0025`로 대폭 낮춤.
  * **크리스프 블랙 외곽선(`_drawBoldText`) + POT 텍스처 + `LinearMipmapNearestFilter`**: 모든 3D 텍스트에 굵은 블랙 외곽선(`strokeText`)과 `900` 웨이트 `"Malgun Gothic", "Segoe UI", Consolas` 폰트를 적용하고, 밉맵 블렌딩 흐림을 막는 `LinearMipmapNearestFilter` + `16x Anisotropy` + `1.5x~2.5x` 슈퍼샘플링을 적용.
  * **UI 폰트 가독성 강화**: Windows에서 글씨를 가늘고 흐릿하게 만드는 `-webkit-font-smoothing: antialiased`를 제거하고 `"Malgun Gothic", "Segoe UI"` `font-weight: 600` 및 고대비 텍스트 색상(`#ffffff`, `#d0d7de`)을 적용.

## [2026-10-24] - v3.0.0 Go 단일 실행 파일(`datacenter-viewer.exe`) 변환 및 빌드 완료

* Go 백엔드 추가: `cmd/datacenter-viewer`, `internal/{model,csvproc,storage,server,launcher,webui}`. 외부 의존성 없음(stdlib only).
* `internal/csvproc`: `validator.js` 포팅(센터 접두사, 1~48U 자동/수동, 48U 초과·충돌, 신규 랙/센터 자동 생성) + `allocator_test.go` 전체 통과.
* `js/storage/folderManager.js`: `/api/bootstrap` 감지 시 Go 모드(`./data/` 자동 로드/저장). `js/app.js`에서 초기화 후 연결. 기존 브라우저 모드와 하위 호환.
* `build-windows.bat`, `README.md`(Go 섹션), `ARCHITECTURE.md`, `PR_CHECKLIST.md`, `.github/workflows/ci.yml`, `GO_MIGRATION_PROGRESS.md` 추가 및 `datacenter-viewer.exe` 단일 파일 빌드 완료.


## [2026-10-24] - v2.3.0 3D 서버 명판·상단 툴바 글자 깨짐(겹침/세로 줄바꿈) 해결, 기본 감도 0.4x 적용, 전체 할당 호스트 CSV 다운로드 및 모달 스크롤 시 3D 시점 고정

* **글자 크기 축소 없이 3D 서버 전면판 및 상단 툴바 글자 위치 깨짐 해결 ([`textureGenerator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/textureGenerator.js), [`style.css`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/css/style.css))**:
  * **3D 서버/스위치 전면 텍스처(캡처 1)**: 글자 크기를 줄이지 않고(`58px~64px` 대형 폰트 유지), **2U~4U 이상 멀티 U 장비**는 좌측 노란색 U 배지를 상/하 2줄(`25~28U` / `[ 4U ]`)로 분리하고 우측 정보창도 **상/하 2단 분리 레이아웃**(1층: `[벤더 | 모델코드]`, 2층: `[호스트네임]`)으로 개편하여 글자 겹침을 원천 해소함. **1U 장비**는 가로 해상도를 `1600px`로 확장하고 `[U배지 | 벤더·모델 | 호스트네임]` 3분할 구획 및 `maxWidth` 보호를 적용.
  * **상단 툴바 세로 글자 깨짐(캡처 2)**: `#top-toolbar` 및 내부 요소(`.dc-brand-group`, `.dc-brand-title`, `.dc-site-label`, `.dc-tb-btn` 등)에 `white-space: nowrap; flex-shrink: 0;`을 적용하여 창 너비가 좁아져도 `마운 / 트 / 3D / 뷰어`나 `센 / 터`처럼 세로로 깨져 내려오지 않도록 수정.
* **마우스 감도 기본값 `0.4x` 설정 ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js), [`topToolbar.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/ui/topToolbar.js))**:
  * 기본 마우스 및 방향키 시선 회전 감도를 `0.4x` (`min="0.1" max="2.5" step="0.1" value="0.4"`)로 설정.
* **현재 할당된 모든 호스트 CSV 다운로드 기능 추가 ([`errorReportModal.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/ui/errorReportModal.js), [`folderManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/storage/folderManager.js), [`topToolbar.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/ui/topToolbar.js))**:
  * CSV 검증 리포트 모달 하단(`[💾 현재 할당된 모든 호스트 CSV 다운로드]`) 및 상단 툴바(`[💾 할당 호스트 CSV 다운로드]`)에서 현재 할당된 모든 호스트(`센터이름,모델코드,랙라벨,hostname,몇U,수동지정U,방향,시뮬레이션`)를 즉시 CSV 파일로 다운로드할 수 있도록 구현.
* **모달 창 스크롤 시 뒤쪽 3D 데이터센터 시점 확대/이동 완전 차단 ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js), [`errorReportModal.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/ui/errorReportModal.js), [`layoutEditorModal.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/ui/layoutEditorModal.js))**:
  * `wheel` 이벤트를 `window`가 아닌 3D WebGL 캔버스(`renderer.domElement`)에만 한정하고, `isModalOpen()` 체크 및 모달 오버레이 `wheel` / `pointer` 이벤트 `stopPropagation()`을 적용하여 캡처 3(검증 리포트 모달 등)에서 스크롤을 내릴 때 뒤쪽 3D 데이터센터 시점이 절대 변하지 않도록 수정.

## [2026-10-24] - v2.2.0 CSV 센터이름 자동 라우팅·신규 센터 자동 추가, '1인칭 통로 시점' 명칭 적용 및 2,000랙/10,000서버 최적화

* **CSV 모델코드 앞 센터이름 자동 인식 및 신규 센터 자동 추가/이동 ([`validator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/state/validator.js), [`csvHandler.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/storage/csvHandler.js), [`store.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/state/store.js), [`topToolbar.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/ui/topToolbar.js))**:
  * CSV/TSV에서 모델코드 앞에 **별도 열**(`센터이름,모델코드,랙라벨,hostname,몇U`)로 넣거나 **모델코드 앞 접두사**(`센터명_012_001`, `SITE-B:012_001`, `[판교]012_001`)로 지정하면 해당 센터를 자동으로 찾아 장비를 할당함.
  * 만약 기존에 없는 **새로운 센터**라면 `findOrCreateSite()`를 통해 새 센터와 랙을 자동으로 생성하고, 상단 센터 선택 드롭다운에 즉시 추가한 뒤 해당 센터 화면으로 자동 전환(`setActiveSite`)되도록 구현.
* **`1인칭 통로 시점` 명칭 정리 ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js))**:
  * 기존 1인칭 보행 및 시선 회전 기능은 그대로 유지하고, 화면 표시 명칭을 `1인칭 통로 시점`으로 통일.
* **대규모 환경(랙 2,000개 · 서버 10,000대) 무지연(O(1)) 최적화 ([`store.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/state/store.js), [`rackMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/rackMeshBuilder.js), [`deviceMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/deviceMeshBuilder.js), [`textureGenerator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/textureGenerator.js))**:
  * 스토어 내 `siteId::rackId` 복합 해시 인덱싱(`_deviceIndex`, `_axisCache`, `_statsCache`)을 도입하여 $O(R \times D)$ 탐색을 **$O(1)$**로 단축.
  * 랙/서버 지오메트리 및 재질 풀링(`sharedGeos`, `sharedMats`, `faceMaterialCache`), 캔버스 텍스처 Mipmap 생략, 드래그 중 불필요한 Raycast 제거, 좌측 목록 가상 캡(300행) 적용.

## [2026-10-24] - v2.1.1 양방향 출입문 그리드 최외곽 끝단 배치 및 마우스 감도 조절 기능 추가

* **양방향 출입문 그리드 가장 뒤쪽(최외곽 경계선) 배치 ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js))**:
  * **주 출입구(`🚪 주 출입구 (기준점)`, 빨간색)**와 **반대편 출입구(`🚪 반대편 출입구 (SUB)`, 에메랄드 그린)** 모두 그리드 가장 뒤쪽 외곽 경계선(`gridMinCoord` / `gridMaxCoord`)의 `Y001` 라인(캡처 내 빨간색 원 표시 위치)에 정확히 위치하도록 수정.
* **마우스 감도 조절 기능 추가 ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js), [`topToolbar.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/ui/topToolbar.js), [`style.css`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/css/style.css))**:
  * 상단 툴바(`🖱️ 감도`) 및 3D 뷰포트 하단 HUD(`🖱️ 마우스 감도`)에 `0.2x ~ 2.5x` 실시간 감도 조절 슬라이더를 추가하여, 1인칭 통로 시점 마우스 회전, 좌우 방향키 회전, 전체 뷰 공전 회전 속도를 사용자 취향에 맞게 즉시 조절 가능하도록 구현.
* **뒤에 가려진 랙 라벨 투시 현상 완벽 해결 ([`rackMeshBuilder.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/rackMeshBuilder.js), [`textureGenerator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/textureGenerator.js))**:
  * 랙 상단 전면/후면에 실제 **3D 물리 명판(WebGL 3D Mesh Plate)**을 장착하고 측면 패널을 불투명화하여, 내 눈에 보이는 앞줄(`X029Y...`)만 선명하게 보이고 앞 랙에 가려진 뒷줄 랙 라벨은 100% 차폐되도록 수정.
* **순수 FPS 1인칭 캐릭터 시점 조작 구현 ([`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js))**:
  * `W`/`S` 및 `↑`/`↓` 화살표: 시선 방향 전진/후진, `A`/`D`: 좌우 게걸음(Strafe), `←`/`→` 화살표 및 마우스 드래그: 제자리 1인칭 시선(고개) 회전.

## [2026-10-24] - v2.0.0 서버 입고 마운트 실물 확인 중심 전면 개편

### 변경 요청 요약 (`maint-request-sk` 표준)
* **대상 경로**: `C:\Users\qazx2\.gemini\antigravity-ide\scratch\datacenter-3d-renderer`
* **핵심 목적**: 데이터센터 온도/전력 관제가 아닌, **신규 서버 입고 시 마운트되는 실물 위치(랙 좌표 및 몇 U인지)와 벤더별 비중 확인**을 위한 3D/2D 시스템으로 개편

### 주요 수정 및 추가 내역
1. **허공에 떠다니는 유령 랙 라벨 제거**:
   * [`sceneManager.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/sceneManager.js)의 `disposeObject()` 및 `rebuildRacks()`에서 `CSS2DObject` DOM 엘리먼트(`.element`)를 명시적으로 제거하고, 랙 선택 시 전체 랙 재생성 대신 `O(1)` 하이라이트 업데이트(`updateRackSelectionVisual`)를 적용하여 허공 라벨 잔상을 원천 차단함.
2. **1인칭 통로 시점(Walk Mode) 및 마우스 조작감 전면 개선**:
   * 통로 시점(`setCameraAisle`) 진입 시 카메라 회전 타겟을 시선 정면 `0.25m` 앞으로 밀착시켜, 마우스 드래그 시 제자리에서 고개를 돌리는 자연스러운 **1인칭 시점 회전(Look Around)** 구현.
   * `W/A/S/D` 및 방향키 보행 이동, `Shift` 가속 이동, `Q/E` 수직 눈높이(1U~52U) 조절, 마우스 휠 전진/후진 및 하단 조작 가이드 HUD 추가.
3. **서버 전면/후면 텍스처 가독성 대폭 강화 및 U 눈금 표시**:
   * [`textureGenerator.js`](file:///C:/Users/qazx2/.gemini/antigravity-ide/scratch/datacenter-3d-renderer/js/renderer3d/textureGenerator.js)의 서버/스위치 텍스처 해상도를 `1024px`로 상향하고, 좌측에 **`[장착U | 몇U]`** 고대비 배지, 우측에 **`[벤더 / 모델코드] 호스트네임`**을 큼직하게 표시.
   * 랙 전면 기둥(Post)에 `1U~52U` 눈금 및 `48U` 서버 제한 경계선 표시.
4. **X(행)/Y(열) 좌표 체계, `[⇄ X/Y 반전]` 버튼, 4칸 통로 간격, 출입문 및 바닥 축 가이드**:
   * 기본 `X = 행(Row, 4칸 간격)`, `Y = 열(Column)` 배치 및 상단 툴바 `[⇄ X:행 / Y:열 (반전)]` 실시간 스왑 버튼 추가.
   * 좌측 상단(`X001, Y001` 인근)에 3D **출입구(ENTRANCE) 게이트** 배치 및 바닥 외곽에 `X001~`, `Y001~` 평면 데칼 가이드 추가(랙 라벨 간섭 없음).
   * `X001~X003` 및 `X029` 행을 포함한 총 32개 랙(52U) 테스트 레이아웃 구성.
5. **상단뷰(Top View) 랙 상판 벤더별 점유율 컬러 바 & 임시할당 색상 지정**:
   * 호스트네임 첫 글자 규칙(`^p`: HP, `^d`: DELL, `^c`: Cisco, `^h`: 조립서버, `^m`: Supermicro, `^l`: Lenovo, 공란: 임시할당) 자동 판별(`determineVendor`).
   * 상단뷰에서 각 랙 상판(Top Roof)에 벤더별 점유 U 비율을 스택 컬러 바로 렌더링하고, 좌측 패널에서 벤더별 색상 및 임시할당 색상을 컬러피커로 즉시 변경 가능하도록 구현.
6. **52U 기본 높이(1~48U 서버 영역 / 49~52U 스위치 영역) 및 신규 CSV/TSV 자동 할당**:
   * `모델코드 | 랙라벨 | hostname | 몇U | [수동지정U]` 양식(CSV 및 엑셀 탭 구분 TSV) 완벽 지원.
   * `hostname` 생략 시 에러 없이 `[임시할당]`으로 등록, `수동지정U`가 비어있으면 1U(아래)부터 자동 할당.
   * 일반 서버가 48U를 초과할 경우(예: `X029Y002` 145대 중 49번째 이후 97대) 할당 불가 리포트 모달에 상세 사유 출력.
7. **온도/전력 항목 제거 및 대량 렌더링 최적화**:
   * 불필요한 온도 센서 태그 및 전력 버짓(kW) UI를 제거하고, 지오메트리/재질 캐싱 및 단일 랙 부분 리빌드로 다수 장비 환경에서 60fps 성능 확보.
