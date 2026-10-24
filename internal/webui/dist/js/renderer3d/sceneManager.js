/**
 * sceneManager.js - Three.js WebGL 3D 씬 매니저
 * - FPS 1인칭 캐릭터 시점 조작 (W/S/↑/↓ 전후진, A/D 좌우이동, ←/→ 고개 좌우 회전, 마우스 Yaw/Pitch 자유 회전)
 * - 마우스 회전 감도 조절(0.2x ~ 2.5x) 지원
 * - 3D 물리 명판 적용으로 앞 랙에 가려진 뒷줄 랙 라벨이 투시되지 않도록 완벽 차폐(Depth Occlusion)
 * - 주 출입구(기준점: 레드) + 반대편 보조 출입구(에메랄드 그린) 모두 그리드 가장 뒤쪽 외곽 경계선에 배치
 */
(function (global) {
  'use strict';

  class SceneManager {
    constructor(containerEl, store, rackBuilder) {
      this.container = containerEl;
      this.store = store;
      this.rackBuilder = rackBuilder;

      this.scene = null;
      this.camera = null;
      this.renderer = null;
      this.controls = null;
      this.raycaster = null;
      this.mouse = null;

      this.racksGroup = null;
      this.floorGroup = null;
      this.rackGroupMap = new Map();

      // 현재 카메라 모드: 'orbit' | 'aisle' (FPS 1인칭) | 'top'
      this.cameraMode = 'orbit';

      // FPS 1인칭 카메라 회전 각도 (Yaw: 좌우 시선, Pitch: 상하 시선)
      this.fpsYaw = 0;   // 0 라디안 = -Z 방향 응시
      this.fpsPitch = 0; // 양수 = 위쪽, 음수 = 아래쪽
      this.mouseSensitivity = 0.4; // 마우스 및 회전 감도 배율 (0.1 ~ 2.5, 기본 0.4x)

      // 카메라 보간(Lerp) 상태
      this.targetCameraPos = null;
      this.targetControlTarget = null;
      this.isLerpingCamera = false;

      // 키보드 네비게이션 상태 (FPS 캐릭터 조작 지원)
      this.keyStates = {
        forward: false,   // W 또는 ↑
        backward: false,  // S 또는 ↓
        strafeLeft: false,// A (좌측 평행 좌우이동)
        strafeRight: false,// D (우측 평행 좌우이동)
        turnLeft: false,  // ← (좌측으로 시선/몸 회전)
        turnRight: false, // → (우측으로 시선/몸 회전)
        up: false,        // Q / PageUp (눈높이 상승)
        down: false,      // E / PageDown (눈높이 하강 / 앉기)
        sprint: false     // Shift (달리기)
      };

      // 키보드 이동 속도 배율 (사용자 토글 지원, 기본값)
      this.sprintSpeedMultiplier = 2.0; // Shift 달리기 속도 배율 (기본값 2.0x)
      this.vertSpeedMultiplier = 1.0;   // Q/E 눈높이 수직 이동 속도 배율 (기본값 1.0x)

      this.isInitialized = false;
      this.animationFrameId = null;
      this._boundAnimate = this.animate.bind(this);
      this._renderFramesRemaining = 60;
      this._isControlActive = false;
      this._lastWidth = 0;
      this._lastHeight = 0;

      // 매 프레임 클로저/벡터 할당으로 인한 가비지 컬렉션 및 메모리 증가 방지를 위한 재사용 벡터
      if (typeof THREE !== 'undefined') {
        this._tmpDir = new THREE.Vector3();
        this._tmpMove = new THREE.Vector3();
        this._tmpFwd = new THREE.Vector3();
        this._tmpRight = new THREE.Vector3();
        this._tmpUp = new THREE.Vector3(0, 1, 0);
      }
    }

    /**
     * 온디맨드(Dirty-Flag) 렌더링 요청
     * - 유휴 상태(마우스/키보드 조작 없음, 카메라 이동 완료)에서는 WebGL 드로우콜을 0회로 멈춰 GPU 커맨드 버퍼 및 RAM 누수를 원천 차단
     */
    requestRender(frames = 2) {
      if (frames > this._renderFramesRemaining) {
        this._renderFramesRemaining = frames;
      }
    }

    /**
     * 현재 모달 창(CSV 검증 리포트, 공간/랙 설정 등)이 열려 있는지 확인
     * - 모달이 열려 있을 때는 3D 데이터센터 시점 이동/확대가 절대 발생하지 않도록 차단
     */
    isModalOpen() {
      const openModal = document.querySelector('.dc-modal-overlay:not(.hidden)');
      return Boolean(openModal);
    }

    /**
     * 마우스 및 시선 회전 감도 설정 (0.1 ~ 2.5, 기본 0.4)
     */
    setMouseSensitivity(val) {
      const num = parseFloat(val);
      if (isNaN(num)) return;
      this.mouseSensitivity = Math.max(0.1, Math.min(2.5, num));
      if (this.controls) {
        this.controls.rotateSpeed = 0.75 * this.mouseSensitivity;
      }
      const hudSlider = document.getElementById('input-hud-mouse-sens');
      const hudVal = document.getElementById('lbl-hud-mouse-sens');
      if (hudSlider && hudSlider.value !== String(this.mouseSensitivity)) {
        hudSlider.value = this.mouseSensitivity;
      }
      if (hudVal) {
        hudVal.textContent = `${this.mouseSensitivity.toFixed(1)}x`;
      }
      const tbSlider = document.getElementById('input-tb-mouse-sens');
      const tbVal = document.getElementById('lbl-tb-mouse-sens');
      if (tbSlider && tbSlider.value !== String(this.mouseSensitivity)) {
        tbSlider.value = this.mouseSensitivity;
      }
      if (tbVal) {
        tbVal.textContent = `${this.mouseSensitivity.toFixed(1)}x`;
      }
    }

    /**
     * Shift 달리기 속도 배율 토글 (2.0x -> 3.0x -> 4.5x -> 1.2x -> 2.0x)
     */
    toggleSprintSpeed() {
      const presets = [2.0, 3.0, 4.5, 1.2];
      const cur = this.sprintSpeedMultiplier;
      let next = presets[0];
      for (let i = 0; i < presets.length; i++) {
        if (Math.abs(cur - presets[i]) < 0.1) {
          next = presets[(i + 1) % presets.length];
          break;
        }
      }
      this.setSprintSpeed(next);
      return next;
    }

    setSprintSpeed(val) {
      const num = parseFloat(val);
      if (isNaN(num)) return;
      this.sprintSpeedMultiplier = Math.max(0.5, Math.min(6.0, num));
      const lblHud = document.getElementById('lbl-hud-sprint-speed');
      if (lblHud) lblHud.textContent = `${this.sprintSpeedMultiplier.toFixed(1)}x`;
      const lblTb = document.getElementById('lbl-tb-sprint-speed');
      if (lblTb) lblTb.textContent = `${this.sprintSpeedMultiplier.toFixed(1)}x`;
    }

    /**
     * Q/E 수직 눈높이 이동 속도 배율 토글 (1.0x -> 1.8x -> 3.0x -> 0.5x -> 1.0x)
     */
    toggleVertSpeed() {
      const presets = [1.0, 1.8, 3.0, 0.5];
      const cur = this.vertSpeedMultiplier;
      let next = presets[0];
      for (let i = 0; i < presets.length; i++) {
        if (Math.abs(cur - presets[i]) < 0.1) {
          next = presets[(i + 1) % presets.length];
          break;
        }
      }
      this.setVertSpeed(next);
      return next;
    }

    setVertSpeed(val) {
      const num = parseFloat(val);
      if (isNaN(num)) return;
      this.vertSpeedMultiplier = Math.max(0.2, Math.min(5.0, num));
      const lblHud = document.getElementById('lbl-hud-vert-speed');
      if (lblHud) lblHud.textContent = `${this.vertSpeedMultiplier.toFixed(1)}x`;
      const lblTb = document.getElementById('lbl-tb-vert-speed');
      if (lblTb) lblTb.textContent = `${this.vertSpeedMultiplier.toFixed(1)}x`;
    }

    init() {
      if (typeof THREE === 'undefined') {
        console.error('Three.js 라이브러리가 로드되지 않았습니다.');
        return;
      }

      const width = this.container.clientWidth || window.innerWidth;
      const height = this.container.clientHeight || window.innerHeight;

      // 1. Scene 생성 (원거리 랙과 텍스트가 뿌옇게 흐려지지 않도록 안개 농도를 최소화)
      this.scene = new THREE.Scene();
      this.scene.background = new THREE.Color(0x0b0e14);
      this.scene.fog = new THREE.FogExp2(0x0b0e14, 0.0025);

      // 2. Camera 생성
      this.camera = new THREE.PerspectiveCamera(55, width / height, 0.08, 300);
      this.camera.rotation.order = 'YXZ'; // FPS 캐릭터 표준 회전 순서 (Yaw -> Pitch)
      this.camera.position.set(8, 6.5, 13);

      // 3. WebGLRenderer 생성 (초고해상도 2.5x 슈퍼샘플링에 의한 1.5GB VRAM 낭비를 방지하고 표준 1.0x~1.25x로 최적화)
      this.renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false, powerPreference: 'high-performance' });
      const targetPixelRatio = Math.min(window.devicePixelRatio || 1, 1.25);
      this.renderer.setPixelRatio(targetPixelRatio);
      this.renderer.setSize(width, height);
      this.renderer.shadowMap.enabled = true;
      this.renderer.shadowMap.type = THREE.PCFSoftShadowMap;
      this.container.appendChild(this.renderer.domElement);

      // 앱 창(--app 모드) 생성 직후 레이아웃 크기 확정 시 즉시 1:1 리사이즈 동기화
      if (typeof ResizeObserver !== 'undefined') {
        this._resizeObserver = new ResizeObserver(() => this.onWindowResize());
        this._resizeObserver.observe(this.container);
      }
      setTimeout(() => this.onWindowResize(), 100);

      // 4. OrbitControls 생성 (전체 뷰 / 상단 뷰에서 사용, 1인칭 통로 시점에서는 직접 FPS 컨트롤러로 전환)
      if (typeof THREE.OrbitControls !== 'undefined') {
        this.controls = new THREE.OrbitControls(this.camera, this.renderer.domElement);
        this.controls.enableDamping = true;
        this.controls.dampingFactor = 0.09;
        this.controls.maxPolarAngle = Math.PI / 2 - 0.01;
        this.controls.minDistance = 0.2;
        this.controls.maxDistance = 500;
        this.controls.zoomSpeed = 1.2;
        this.controls.rotateSpeed = 0.75 * this.mouseSensitivity;
        this.controls.target.set(5.5, 1.1, 5.5);

        this.controls.addEventListener('start', () => {
          this.isLerpingCamera = false;
          this._isControlActive = true;
          this.requestRender(10);
        });
        this.controls.addEventListener('change', () => {
          this.requestRender(2);
        });
        this.controls.addEventListener('end', () => {
          this._isControlActive = false;
          this.requestRender(35);
        });
      }

      // 5. 조작 안내 미니 HUD 생성 (마우스 감도 조절 슬라이더 포함)
      this.createNavigationHud();

      // 6. 조명
      this.setupLights();

      // 7. 그룹 계층 구조
      this.floorGroup = new THREE.Group();
      this.floorGroup.name = 'FloorGroup';
      this.scene.add(this.floorGroup);

      this.racksGroup = new THREE.Group();
      this.racksGroup.name = 'RacksGroup';
      this.scene.add(this.racksGroup);

      // 8. 인터랙션
      this.raycaster = new THREE.Raycaster();
      this.mouse = new THREE.Vector2();

      this.setupEvents();
      this.setupKeyboardNavigation();
      this.bindStoreEvents();

      // 9. 초기 바닥 및 랙 렌더링
      this.rebuildFloor();
      this.rebuildRacks();

      this.isInitialized = true;
      this.requestRender(60);
      this.animate();
    }

    /**
     * 카메라의 현재 바라보는 방향으로부터 FPS Yaw / Pitch 각도를 동기화
     */
    syncFpsAnglesFromCamera() {
      if (!this.camera) return;
      const dir = this._tmpDir || new THREE.Vector3();
      this.camera.getWorldDirection(dir);
      this.fpsPitch = Math.asin(Math.max(-0.98, Math.min(0.98, dir.y)));
      this.fpsYaw = Math.atan2(-dir.x, -dir.z);
    }

    /**
     * FPS Yaw / Pitch 각도를 카메라 회전 및 OrbitControls 타겟에 적용
     */
    applyFpsAnglesToCamera() {
      if (!this.camera) return;
      const maxPitch = Math.PI * 0.44;
      this.fpsPitch = Math.max(-maxPitch, Math.min(maxPitch, this.fpsPitch));

      this.camera.rotation.order = 'YXZ';
      this.camera.rotation.y = this.fpsYaw;
      this.camera.rotation.x = this.fpsPitch;
      this.camera.rotation.z = 0;

      if (this.controls) {
        const forward = this._tmpFwd || new THREE.Vector3();
        forward.set(
          -Math.sin(this.fpsYaw) * Math.cos(this.fpsPitch),
          Math.sin(this.fpsPitch),
          -Math.cos(this.fpsYaw) * Math.cos(this.fpsPitch)
        ).normalize();
        this.controls.target.copy(this.camera.position).addScaledVector(forward, 2.0);
      }
      this.requestRender(3);
    }

    createNavigationHud() {
      if (document.getElementById('dc-viewport-hud')) return;
      const hud = document.createElement('div');
      hud.id = 'dc-viewport-hud';
      hud.className = 'dc-viewport-hud';
      hud.innerHTML = `
        <div class="dc-hud-mode-badge" id="dc-hud-mode-label">🏠 전체 전경 시점 (Orbit)</div>
        <div class="dc-hud-controls">
          <span><kbd>W</kbd><kbd>S</kbd> / <kbd>↑</kbd><kbd>↓</kbd> 전후진</span>
          <span><kbd>A</kbd><kbd>D</kbd> 좌우이동</span>
          <span><kbd>←</kbd><kbd>→</kbd> / <kbd>마우스</kbd> 시선 회전</span>
          <span><kbd>Q</kbd><kbd>E</kbd> 눈높이</span>
          <span><kbd>Shift</kbd> 달리기</span>
        </div>
        <div class="dc-hud-sens-box" style="pointer-events: auto; display: flex; align-items: center; gap: 8px;">
          <span>🖱️ 감도</span>
          <input type="range" id="input-hud-mouse-sens" min="0.1" max="2.5" step="0.1" value="0.4">
          <strong id="lbl-hud-mouse-sens">0.4x</strong>
          <button type="button" class="dc-hud-speed-btn" id="btn-hud-sprint-speed" title="Shift 달리기 속도 토글 (기본: 2.0x)">⚡ Shift: <strong id="lbl-hud-sprint-speed">2.0x</strong></button>
          <button type="button" class="dc-hud-speed-btn" id="btn-hud-vert-speed" title="Q/E 눈높이 수직 이동 속도 토글 (기본: 1.0x)">↕ Q/E: <strong id="lbl-hud-vert-speed">1.0x</strong></button>
        </div>
      `;
      this.container.appendChild(hud);

      const slider = hud.querySelector('#input-hud-mouse-sens');
      if (slider) {
        slider.addEventListener('input', (e) => {
          this.setMouseSensitivity(e.target.value);
        });
      }
      const btnSprint = hud.querySelector('#btn-hud-sprint-speed');
      if (btnSprint) {
        btnSprint.addEventListener('click', (e) => {
          e.stopPropagation();
          this.toggleSprintSpeed();
        });
      }
      const btnVert = hud.querySelector('#btn-hud-vert-speed');
      if (btnVert) {
        btnVert.addEventListener('click', (e) => {
          e.stopPropagation();
          this.toggleVertSpeed();
        });
      }
    }

    updateHudModeLabel(modeText) {
      const el = document.getElementById('dc-hud-mode-label');
      if (el) el.textContent = modeText;
    }

    setupLights() {
      const ambientLight = new THREE.AmbientLight(0xffffff, 0.85);
      this.scene.add(ambientLight);

      const dirLight = new THREE.DirectionalLight(0xffffff, 0.65);
      dirLight.position.set(12, 24, 18);
      dirLight.castShadow = true;
      dirLight.shadow.mapSize.width = 2048;
      dirLight.shadow.mapSize.height = 2048;
      dirLight.shadow.bias = -0.0005;
      this.scene.add(dirLight);

      const hemiLight = new THREE.HemisphereLight(0xffffff, 0x1e293b, 0.45);
      this.scene.add(hemiLight);
    }

    /**
     * 바닥 그리드, 양방향 출입문(그리드 가장 뒤쪽 외곽 경계선에 위치), X/Y 축 데칼 재구성
     */
    rebuildFloor() {
      while (this.floorGroup.children.length > 0) {
        const obj = this.floorGroup.children[0];
        this.disposeObject(obj);
        this.floorGroup.remove(obj);
      }

      const site = this.store.getActiveSite();
      if (!site) return;

      const racks = this.store.getRacks();
      let maxGX = site.gridWidth || 26;
      let maxGZ = site.gridDepth || 24;
      let minRackX = Infinity;
      let maxRackX = 10;
      let maxRackZ = 15;

      racks.forEach(r => {
        const pos = this.store.getRackEffectivePosition(r);
        if (pos.gridX < minRackX) minRackX = pos.gridX;
        if (pos.gridX > maxRackX) maxRackX = pos.gridX;
        if (pos.gridZ > maxRackZ) maxRackZ = pos.gridZ;
        if (pos.gridX + 4 > maxGX) maxGX = pos.gridX + 4;
        if (pos.gridZ + 5 > maxGZ) maxGZ = pos.gridZ + 5;
      });
      if (!isFinite(minRackX)) minRackX = 3;

      const gridW = Math.max(20, maxGX);
      const gridD = Math.max(20, maxGZ);
      const tileSize = 1.0;

      // 그리드의 실제 외곽 경계 좌표 계산
      const maxDim = Math.max(gridW, gridD) + 4;
      const gridMinCoord = -2.5 * tileSize;
      const gridMaxCoord = (maxDim - 2.5) * tileSize;

      // 1. 바닥 베이스 플레이트
      const floorGeo = new THREE.PlaneGeometry(maxDim * tileSize, maxDim * tileSize);
      const floorMat = new THREE.MeshStandardMaterial({
        color: 0x11151d,
        roughness: 0.9,
        metalness: 0.1
      });
      const floorMesh = new THREE.Mesh(floorGeo, floorMat);
      floorMesh.rotation.x = -Math.PI / 2;
      floorMesh.position.set((maxDim * tileSize) / 2 - 2.5, -0.006, (maxDim * tileSize) / 2 - 2.5);
      floorMesh.receiveShadow = true;
      this.floorGroup.add(floorMesh);

      // 2. 바닥 그리드 라인
      const gridHelper = new THREE.GridHelper(maxDim * tileSize, maxDim, 0x334155, 0x1e293b);
      gridHelper.position.set((maxDim * tileSize) / 2 - 2.5, 0, (maxDim * tileSize) / 2 - 2.5);
      this.floorGroup.add(gridHelper);

      const texGen = global.DCTextureGenerator;

      // 3. 출입문 2개 배치 (그리드 가장 뒤쪽 외곽 경계선에 정확히 밀착!):
      // (A) 기준점 주 출입구 (X001Y001 방향 그리드 맨 외곽 끝 경계선 `gridMinCoord` — 빨간색 문)
      const doorLineX = (minRackX - 1) * tileSize; // Y001~Y002 라인(캡처의 빨간 원이 위치한 동일 라인!)
      this.buildEntranceDoor(texGen, {
        posX: doorLineX,
        posZ: gridMinCoord + 0.05,
        rotY: 0,
        isMain: true
      });

      // (B) 반대편 출입구 (사용자 캡처의 빨간색 동그라미 위치: X029Y001 너머 그리드 가장 뒤쪽 끝 경계선 `gridMaxCoord` — 에메랄드 그린 문)
      this.buildEntranceDoor(texGen, {
        posX: doorLineX,
        posZ: gridMaxCoord - 0.05,
        rotY: Math.PI,
        isMain: false
      });

      // 4. 바닥 X / Y 축 데칼 표시
      this.buildAxisFloorGuides(racks, texGen, tileSize);

      // 5. 통로 타공 타일 배치
      const tilePositions = new Set();
      racks.forEach(r => {
        const pos = this.store.getRackEffectivePosition(r);
        tilePositions.add(`${pos.gridX},${pos.gridZ + 1}`);
        tilePositions.add(`${pos.gridX},${pos.gridZ + 2}`);
        tilePositions.add(`${pos.gridX},${pos.gridZ - 2}`);
      });

      const perfTex = texGen ? texGen.getTileTexture('perforated') : null;
      const tileGeo = new THREE.PlaneGeometry(tileSize * 0.95, tileSize * 0.95);
      tileGeo.userData = { shared: true };
      const tileMat = new THREE.MeshStandardMaterial({
        map: perfTex,
        color: 0xffffff,
        roughness: 0.75
      });

      const validTiles = [];
      tilePositions.forEach(coord => {
        const [gx, gz] = coord.split(',').map(Number);
        if (gx >= 1 && gz >= 1) validTiles.push([gx, gz]);
      });

      if (validTiles.length > 0) {
        if (typeof THREE.InstancedMesh !== 'undefined') {
          const instancedTiles = new THREE.InstancedMesh(tileGeo, tileMat, validTiles.length);
          instancedTiles.receiveShadow = false;
          instancedTiles.matrixAutoUpdate = false;
          const dummy = new THREE.Object3D();
          dummy.rotation.x = -Math.PI / 2;
          for (let i = 0; i < validTiles.length; i++) {
            const [gx, gz] = validTiles[i];
            dummy.position.set((gx - 1) * tileSize, 0.002, (gz - 1) * tileSize);
            dummy.updateMatrix();
            instancedTiles.setMatrixAt(i, dummy.matrix);
          }
          instancedTiles.instanceMatrix.needsUpdate = true;
          instancedTiles.updateMatrix();
          this.floorGroup.add(instancedTiles);
        } else {
          validTiles.forEach(([gx, gz]) => {
            const tileMesh = new THREE.Mesh(tileGeo, tileMat);
            tileMesh.rotation.x = -Math.PI / 2;
            tileMesh.position.set((gx - 1) * tileSize, 0.002, (gz - 1) * tileSize);
            tileMesh.receiveShadow = false;
            tileMesh.matrixAutoUpdate = false;
            tileMesh.updateMatrix();
            this.floorGroup.add(tileMesh);
          });
        }
      }
    }

    /**
     * 주 출입구(기준점: 레드) 및 반대편 보조 출입구(에메랄드 그린) 3D 도어 생성
     */
    buildEntranceDoor(texGen, { posX = 1.5, posZ = -0.5, rotY = 0, isMain = true } = {}) {
      const doorGroup = new THREE.Group();
      doorGroup.name = isMain ? 'MainEntranceDoorGroup' : 'SubEntranceDoorGroup';

      const doorW = 1.8;
      const doorH = 2.4;
      const frameThick = 0.08;

      const frameMat = new THREE.MeshStandardMaterial({
        color: isMain ? 0xe53935 : 0x10b981,
        emissive: isMain ? 0x7f1d1d : 0x065f46,
        emissiveIntensity: 0.45,
        metalness: 0.5,
        roughness: 0.3
      });

      const glassMat = new THREE.MeshStandardMaterial({
        color: isMain ? 0x38bdf8 : 0x34d399,
        transparent: true,
        opacity: 0.28,
        metalness: 0.8,
        roughness: 0.2
      });

      const leftPost = new THREE.Mesh(new THREE.BoxGeometry(frameThick, doorH, frameThick), frameMat);
      leftPost.position.set(-doorW / 2, doorH / 2, 0);
      doorGroup.add(leftPost);

      const rightPost = new THREE.Mesh(new THREE.BoxGeometry(frameThick, doorH, frameThick), frameMat);
      rightPost.position.set(doorW / 2, doorH / 2, 0);
      doorGroup.add(rightPost);

      const topBar = new THREE.Mesh(new THREE.BoxGeometry(doorW + frameThick, frameThick, frameThick), frameMat);
      topBar.position.set(0, doorH, 0);
      doorGroup.add(topBar);

      const panelGeo = new THREE.BoxGeometry(doorW / 2 - 0.06, doorH - 0.08, 0.03);
      const leftDoor = new THREE.Mesh(panelGeo, glassMat);
      leftDoor.position.set(-doorW / 4, doorH / 2, 0);
      doorGroup.add(leftDoor);

      const rightDoor = new THREE.Mesh(panelGeo, glassMat);
      rightDoor.position.set(doorW / 4, doorH / 2, 0);
      doorGroup.add(rightDoor);

      const signTex = texGen ? texGen.getEntranceSignTexture(isMain) : null;
      if (signTex) {
        const signMat = new THREE.MeshBasicMaterial({ map: signTex, side: THREE.DoubleSide });
        const signMesh = new THREE.Mesh(new THREE.PlaneGeometry(2.0, 0.46), signMat);
        signMesh.position.set(0, doorH + 0.32, 0.02);
        doorGroup.add(signMesh);
      }

      // 바닥 출입구 가이드 매트 (주 출입구: 레드 / 반대편 출입구: 에메랄드 그린)
      const matGeo = new THREE.PlaneGeometry(doorW, 1.1);
      const matMesh = new THREE.Mesh(matGeo, new THREE.MeshBasicMaterial({
        color: isMain ? 0xef4444 : 0x10b981,
        transparent: true,
        opacity: 0.28,
        side: THREE.DoubleSide
      }));
      matMesh.rotation.x = -Math.PI / 2;
      matMesh.position.set(0, 0.004, 0.6);
      doorGroup.add(matMesh);

      doorGroup.position.set(posX, 0, posZ);
      doorGroup.rotation.y = rotY;
      this.floorGroup.add(doorGroup);
    }

    buildAxisFloorGuides(racks, texGen, tileSize = 1.0) {
      if (!texGen || !racks || racks.length === 0) return;

      const invertXY = this.store.state.invertXY;
      const zAxisMap = new Map();
      const xAxisMap = new Map();

      racks.forEach(r => {
        const pos = this.store.getRackEffectivePosition(r);
        const xStr = `X${String(pos.rawX || 1).padStart(3, '0')}`;
        const yStr = `Y${String(pos.rawY || 1).padStart(3, '0')}`;

        if (!invertXY) {
          if (!zAxisMap.has(pos.gridZ)) zAxisMap.set(pos.gridZ, xStr);
          if (!xAxisMap.has(pos.gridX)) xAxisMap.set(pos.gridX, yStr);
        } else {
          if (!xAxisMap.has(pos.gridX)) xAxisMap.set(pos.gridX, xStr);
          if (!zAxisMap.has(pos.gridZ)) zAxisMap.set(pos.gridZ, yStr);
        }
      });

      const labelGeo = new THREE.PlaneGeometry(0.92, 0.48);
      labelGeo.userData = { shared: true };

      zAxisMap.forEach((labelText, gz) => {
        const tex = texGen.getAxisFloorLabelTexture(labelText, !invertXY);
        const mat = new THREE.MeshBasicMaterial({ map: tex, transparent: true });
        const mesh = new THREE.Mesh(labelGeo, mat);
        mesh.rotation.x = -Math.PI / 2;
        mesh.position.set(0.4 * tileSize, 0.005, (gz - 1) * tileSize);
        this.floorGroup.add(mesh);
      });

      xAxisMap.forEach((labelText, gx) => {
        const tex = texGen.getAxisFloorLabelTexture(labelText, invertXY);
        const mat = new THREE.MeshBasicMaterial({ map: tex, transparent: true });
        const mesh = new THREE.Mesh(labelGeo, mat);
        mesh.rotation.x = -Math.PI / 2;
        mesh.position.set((gx - 1) * tileSize, 0.005, 0.5 * tileSize);
        this.floorGroup.add(mesh);
      });
    }

    /**
     * 랙 및 장비 3D 메쉬 전체 재구성 (대규모 랙/장비 환경 자동 최적화)
     */
    rebuildRacks() {
      while (this.racksGroup.children.length > 0) {
        const obj = this.racksGroup.children[0];
        this.disposeObject(obj);
        this.racksGroup.remove(obj);
      }
      this.rackGroupMap.clear();

      const racks = this.store.getRacks();
      const isMassive = racks.length > 60 || (this.store.state.devices && this.store.state.devices.length > 500);
      if (this.renderer && this.renderer.shadowMap) {
        this.renderer.shadowMap.autoUpdate = !isMassive;
      }
      const options = {
        selectedRackId: this.store.state.selectedRackId,
        simulationView: this.store.state.simulationView,
        globalOrientation: this.store.state.globalOrientation,
        showBadges: this.store.state.showBadges,
        massiveScale: isMassive,
        lazyDevices: false,
        tileSize: 1.0
      };

      for (let i = 0; i < racks.length; i++) {
        const rack = racks[i];
        const rackId = String(rack.rackId).toUpperCase();
        const devices = this.store.getDevicesForRack(rackId);
        const rackGroup = this.rackBuilder.createRackGroup(rack, devices, options);
        if (rackGroup) {
          this.racksGroup.add(rackGroup);
          this.rackGroupMap.set(rackId, rackGroup);
        }
      }
      this.updateMassiveScaleLOD();
    }

    rebuildSingleRack(rackId) {
      if (!rackId) return;
      const normId = String(rackId).toUpperCase();
      const oldGroup = this.rackGroupMap.get(normId);
      if (oldGroup) {
        this.disposeObject(oldGroup);
        this.racksGroup.remove(oldGroup);
        this.rackGroupMap.delete(normId);
      }

      const rack = this.store.getRack(normId);
      if (!rack) return;

      const devices = this.store.getDevicesForRack(normId);
      const isSelected = this.store.state.selectedRackId === normId;
      const isMassive = (this.store.getRacks() || []).length > 60 || (this.store.state.devices && this.store.state.devices.length > 500);
      const options = {
        selectedRackId: this.store.state.selectedRackId,
        simulationView: this.store.state.simulationView,
        globalOrientation: this.store.state.globalOrientation,
        showBadges: this.store.state.showBadges,
        massiveScale: isMassive,
        isDetailed: isSelected || !isMassive,
        lazyDevices: false,
        tileSize: 1.0
      };

      const newGroup = this.rackBuilder.createRackGroup(rack, devices, options);
      if (newGroup) {
        this.racksGroup.add(newGroup);
        this.rackGroupMap.set(normId, newGroup);
      }
      this.updateMassiveScaleLOD();
    }

    /**
     * 대규모 환경(서버 15,000대 / 랙 600개) 60fps 유지 뷰포트 LOD
     * - 상단뷰(Top View): 지붕(Roof)에 벤더 점유율이 불투명하게 표시되므로 내부 서버 15,000대 드로우콜 생략
     * - 전체뷰(Orbit View) 및 통로뷰(Aisle View): 모든 랙과 내부 서버를 100% 표시 (정적 행렬 캐싱 및 텍스처 템플릿 풀링으로 렉 없이 렌더링)
     */
    updateMassiveScaleLOD() {
      if (!this.racksGroup || !this.camera) return;
      const isTopView = this.cameraMode === 'top';
      const children = this.racksGroup.children;
      for (let i = 0; i < children.length; i++) {
        const rackGroup = children[i];
        const devGroup = (rackGroup.userData && rackGroup.userData.devGroup) ||
          rackGroup.getObjectByName(`DeviceGroup_${(rackGroup.userData && rackGroup.userData.rackId) || rackGroup.name.replace('RackGroup_', '')}`);
        if (devGroup) {
          devGroup.visible = !isTopView;
        }
      }
      this.requestRender(5);
    }

    disposeObject(obj) {
      if (!obj) return;
      if (obj.children) {
        for (let i = obj.children.length - 1; i >= 0; i--) {
          this.disposeObject(obj.children[i]);
        }
      }
      if (obj.geometry && (!obj.geometry.userData || !obj.geometry.userData.shared)) {
        obj.geometry.dispose();
      }
      if (obj.material) {
        if (Array.isArray(obj.material)) {
          obj.material.forEach(m => {
            if (m && (!m.userData || !m.userData.shared)) m.dispose();
          });
        } else if (!obj.material.userData || !obj.material.userData.shared) {
          obj.material.dispose();
        }
      }
    }

    /**
     * 1인칭 키보드 조작 바인딩
     * - W / S / ↑ / ↓ : 바라보는 시선 방향으로 전진 / 후진
     * - A / D : 바라보는 방향 기준 좌/우 이동(Strafe)
     * - ← / → (좌우 방향키) : 1인칭 시선 좌/우 회전(Yaw Turn)
     * - Q / E (또는 PageUp/PageDown) : 눈높이 위/아래 조절
     */
    setupKeyboardNavigation() {
      window.addEventListener('keydown', (e) => {
        if (['INPUT', 'SELECT', 'TEXTAREA'].includes(document.activeElement.tagName)) {
          return;
        }
        if (this.isModalOpen()) {
          return;
        }

        if (e.shiftKey) this.keyStates.sprint = true;

        const isNavKey = ['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'KeyW', 'KeyA', 'KeyS', 'KeyD', 'KeyQ', 'KeyE', 'PageUp', 'PageDown'].includes(e.code);
        if (isNavKey) {
          if (this.isLerpingCamera) {
            this.isLerpingCamera = false;
            this.syncFpsAnglesFromCamera();
          }
          this.requestRender(5);
        }

        switch (e.code) {
          case 'ArrowUp':
          case 'KeyW':
            this.keyStates.forward = true;
            if (e.code === 'ArrowUp') e.preventDefault();
            break;
          case 'ArrowDown':
          case 'KeyS':
            this.keyStates.backward = true;
            if (e.code === 'ArrowDown') e.preventDefault();
            break;
          case 'KeyA':
            this.keyStates.strafeLeft = true;
            break;
          case 'KeyD':
            this.keyStates.strafeRight = true;
            break;
          case 'ArrowLeft':
            this.keyStates.turnLeft = true;
            e.preventDefault();
            break;
          case 'ArrowRight':
            this.keyStates.turnRight = true;
            e.preventDefault();
            break;
          case 'KeyQ':
          case 'PageUp':
            this.keyStates.up = true;
            if (e.code === 'PageUp') e.preventDefault();
            break;
          case 'KeyE':
          case 'PageDown':
            this.keyStates.down = true;
            if (e.code === 'PageDown') e.preventDefault();
            break;
        }
      });

      window.addEventListener('keyup', (e) => {
        if (!e.shiftKey) this.keyStates.sprint = false;

        switch (e.code) {
          case 'ArrowUp':
          case 'KeyW':
            this.keyStates.forward = false;
            break;
          case 'ArrowDown':
          case 'KeyS':
            this.keyStates.backward = false;
            break;
          case 'KeyA':
            this.keyStates.strafeLeft = false;
            break;
          case 'KeyD':
            this.keyStates.strafeRight = false;
            break;
          case 'ArrowLeft':
            this.keyStates.turnLeft = false;
            break;
          case 'ArrowRight':
            this.keyStates.turnRight = false;
            break;
          case 'KeyQ':
          case 'PageUp':
            this.keyStates.up = false;
            break;
          case 'KeyE':
          case 'PageDown':
            this.keyStates.down = false;
            break;
        }
      });
    }

    /**
     * 매 프레임 1인칭 이동 및 시선 회전 업데이트 (이동 발생 시 true 반환)
     */
    updateKeyboardMovement(delta = 0.016) {
      if (!this.camera) return false;
      if (this.isModalOpen()) return false;

      const anyKeyActive =
        this.keyStates.forward ||
        this.keyStates.backward ||
        this.keyStates.strafeLeft ||
        this.keyStates.strafeRight ||
        this.keyStates.turnLeft ||
        this.keyStates.turnRight ||
        this.keyStates.up ||
        this.keyStates.down;

      if (!anyKeyActive) return false;

      // 1) 좌우 방향키(← / →) 입력 시 1인칭 시선(고개) 좌우 회전 (마우스 감도 반영)
      const turnSpeed = 1.65 * this.mouseSensitivity * delta;
      if (this.keyStates.turnLeft || this.keyStates.turnRight) {
        if (this.cameraMode !== 'aisle') {
          this.syncFpsAnglesFromCamera();
        }
        if (this.keyStates.turnLeft) this.fpsYaw += turnSpeed;
        if (this.keyStates.turnRight) this.fpsYaw -= turnSpeed;
        this.applyFpsAnglesToCamera();
      }

      // 2) 전후진(W/S/↑/↓) 및 좌우 이동(A/D) — 사전 할당된 벡터 재사용으로 GC 압박 원천 차단
      const baseSpeed = this.cameraMode === 'aisle' ? 3.5 : 5.5;
      const sprintMult = this.keyStates.sprint ? this.sprintSpeedMultiplier : 1.0;
      const moveSpeed = baseSpeed * sprintMult * delta;
      const moveVector = this._tmpMove || new THREE.Vector3();
      moveVector.set(0, 0, 0);

      const forward = this._tmpFwd || new THREE.Vector3();
      this.camera.getWorldDirection(forward);
      forward.y = 0;
      if (forward.lengthSq() < 0.0001) {
        forward.set(-Math.sin(this.fpsYaw), 0, -Math.cos(this.fpsYaw));
      } else {
        forward.normalize();
      }

      const right = this._tmpRight || new THREE.Vector3();
      const upVec = this._tmpUp || new THREE.Vector3(0, 1, 0);
      right.crossVectors(forward, upVec).normalize();

      if (this.keyStates.forward) moveVector.add(forward);
      if (this.keyStates.backward) moveVector.sub(forward);
      if (this.keyStates.strafeRight) moveVector.add(right);
      if (this.keyStates.strafeLeft) moveVector.sub(right);

      if (moveVector.lengthSq() > 0) {
        moveVector.normalize().multiplyScalar(moveSpeed);
        this.camera.position.add(moveVector);
        if (this.controls) {
          this.controls.target.add(moveVector);
        }
      }

      // 3) 수직 눈높이 조절 (Q/E)
      const vertSpeed = 2.2 * this.vertSpeedMultiplier * delta;
      if (this.keyStates.up) {
        this.camera.position.y = Math.min(18.0, this.camera.position.y + vertSpeed);
        if (this.controls) this.controls.target.y = Math.min(18.0, this.controls.target.y + vertSpeed);
      }
      if (this.keyStates.down) {
        this.camera.position.y = Math.max(0.25, this.camera.position.y - vertSpeed);
        if (this.controls) this.controls.target.y = Math.max(0.25, this.controls.target.y - vertSpeed);
      }
      this.requestRender(3);
      return true;
    }

    /**
     * 마우스 이벤트:
     * - 통로 시점('aisle')에서는 OrbitControls 대신 순수 1인칭 마우스 Look(Yaw/Pitch 직접 제어)으로 동작!
     * - pointerdown에서는 무거운 Raycast를 수행하지 않고, 드래그 없이 클릭했을 때만 Raycast를 수행하여 10,000대 환경에서도 마우스 회전이 렉 없이 동작!
     * - 모달 창(CSV 검증 리포트 등)이 열려 있을 때 스크롤을 내려도 3D 데이터센터 시점이 절대 변하지 않도록 캔버스 한정 이벤트로 격리!
     */
    setupEvents() {
      window.addEventListener('resize', () => this.onWindowResize());

      let pointerDownPos = { x: 0, y: 0 };
      let lastPointerPos = { x: 0, y: 0 };
      let hasDragged = false;
      let isPointerDown = false;
      let activeButton = 0;

      this.container.addEventListener('pointerdown', (e) => {
        if (this.isModalOpen()) return;
        isPointerDown = true;
        activeButton = e.button;
        pointerDownPos = { x: e.clientX, y: e.clientY };
        lastPointerPos = { x: e.clientX, y: e.clientY };
        hasDragged = false;

        if (this.isLerpingCamera) {
          this.isLerpingCamera = false;
          this.syncFpsAnglesFromCamera();
        }
        this.requestRender(5);
      });

      window.addEventListener('pointermove', (e) => {
        if (!isPointerDown || this.isModalOpen()) return;
        const dx = e.clientX - lastPointerPos.x;
        const dy = e.clientY - lastPointerPos.y;
        lastPointerPos = { x: e.clientX, y: e.clientY };

        if (Math.hypot(e.clientX - pointerDownPos.x, e.clientY - pointerDownPos.y) > 4) {
          hasDragged = true;
        }

        // 1인칭 통로 시점(aisle)에서는 마우스 드래그 시 제자리에서 고개(Yaw/Pitch)가 즉시 회전!
        if (this.cameraMode === 'aisle' && activeButton === 0) {
          const lookSensitivity = 0.0032 * this.mouseSensitivity;
          this.fpsYaw -= dx * lookSensitivity;
          this.fpsPitch -= dy * lookSensitivity;
          this.applyFpsAnglesToCamera();
        }
        this.requestRender(5);
      });

      window.addEventListener('pointerup', (e) => {
        if (isPointerDown) {
          if (Math.hypot(e.clientX - pointerDownPos.x, e.clientY - pointerDownPos.y) > 4) {
            hasDragged = true;
          }
          isPointerDown = false;
          if (this.cameraMode !== 'aisle') {
            this.syncFpsAnglesFromCamera();
          }
          this.requestRender(35);
        }
      });

      // wheel 이벤트를 window가 아닌 3D WebGL 캔버스(domElement)에만 바인딩 + 모달 열림 시 완벽 차단
      const wheelTarget = (this.renderer && this.renderer.domElement) ? this.renderer.domElement : this.container;
      wheelTarget.addEventListener('wheel', (e) => {
        if (this.isModalOpen()) return;
        this.isLerpingCamera = false;
        if (this.cameraMode === 'aisle' && this.camera) {
          const dir = this._tmpDir || new THREE.Vector3();
          this.camera.getWorldDirection(dir);
          dir.y = 0;
          dir.normalize();
          const step = e.deltaY < 0 ? 0.55 : -0.55;
          this.camera.position.addScaledVector(dir, step);
          if (this.controls) {
            this.controls.target.addScaledVector(dir, step);
          }
        }
        this.requestRender(35);
      }, { passive: true });

      this.container.addEventListener('dblclick', (e) => {
        const hit = this.raycast(e);
        if (hit && hit.userData) {
          const rackId = hit.userData.rackId;
          if (rackId) {
            this.store.selectRack(rackId);
            this.focusRack(rackId);
          }
        }
      });

      this.container.addEventListener('click', (e) => {
        if (hasDragged) {
          hasDragged = false;
          return;
        }

        const hit = this.raycast(e);
        if (hit && hit.userData) {
          if (hit.userData.type === 'device') {
            this.store.selectDevice(hit.userData.deviceId);
          } else if (hit.userData.type === 'rack') {
            this.store.selectRack(hit.userData.rackId);
          }
        }
      });
    }

    raycast(event) {
      if (!this.raycaster || !this.camera) return null;

      const rect = this.renderer.domElement.getBoundingClientRect();
      this.mouse.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      this.mouse.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;

      this.raycaster.setFromCamera(this.mouse, this.camera);
      const intersects = this.raycaster.intersectObjects(this.racksGroup.children, true);

      if (intersects.length > 0) {
        for (const hit of intersects) {
          let curr = hit.object;
          while (curr && curr !== this.racksGroup) {
            if (curr.userData && (curr.userData.type === 'device' || curr.userData.type === 'rack')) {
              return curr;
            }
            curr = curr.parent;
          }
        }
      }
      return null;
    }

    /**
     * 특정 랙 정면으로 카메라 이동
     */
    focusRack(rackId) {
      const rack = this.store.getRack(rackId);
      if (!rack) return;

      const tileSize = 1.0;
      const effPos = this.store.getRackEffectivePosition(rack);
      const posX = (effPos.gridX - 1) * tileSize;
      const posZ = (effPos.gridZ - 1) * tileSize;
      const maxU = rack.maxU || 52;
      const centerY = (maxU * 0.04445) * 0.55;

      const rotDeg = rack.rotationDeg || 0;
      const dist = 2.1;
      let camOffsetX = 0;
      let camOffsetZ = dist;

      if (rotDeg === 90) {
        camOffsetX = dist;
        camOffsetZ = 0;
      } else if (rotDeg === 180) {
        camOffsetX = 0;
        camOffsetZ = -dist;
      } else if (rotDeg === 270) {
        camOffsetX = -dist;
        camOffsetZ = 0;
      }

      const camPos = new THREE.Vector3(posX + camOffsetX, centerY, posZ + camOffsetZ);
      const rackLookTarget = new THREE.Vector3(posX, centerY, posZ);

      if (this.cameraMode === 'aisle') {
        this.camera.position.copy(camPos);
        this.camera.lookAt(rackLookTarget);
        this.syncFpsAnglesFromCamera();
        this.applyFpsAnglesToCamera();
        this.isLerpingCamera = false;
      } else {
        this.targetCameraPos = camPos;
        this.targetControlTarget = rackLookTarget;
        this.isLerpingCamera = true;
      }
      this.requestRender(45);
    }

    /**
     * 카메라 프리셋 1: Home (전체 전경 시점)
     */
    setCameraHome() {
      this.cameraMode = 'orbit';
      if (this.controls) {
        this.controls.enabled = true;
        this.controls.enableZoom = true;
        this.controls.rotateSpeed = 0.75 * this.mouseSensitivity;
        this.controls.maxPolarAngle = Math.PI / 2 - 0.01;
      }
      this.updateHudModeLabel('🏠 전체 전경 시점 (Orbit)');

      const racks = this.store.getRacks();
      let avgX = 5.0;
      let avgZ = 5.0;
      if (racks.length > 0) {
        let sumX = 0, sumZ = 0;
        racks.forEach(r => {
          const p = this.store.getRackEffectivePosition(r);
          sumX += (p.gridX - 1);
          sumZ += (p.gridZ - 1);
        });
        avgX = sumX / racks.length;
        avgZ = sumZ / racks.length;
      }

      this.targetControlTarget = new THREE.Vector3(avgX, 1.0, avgZ);
      this.targetCameraPos = new THREE.Vector3(avgX + 7, 8.5, avgZ + 11);
      this.isLerpingCamera = true;
      this.updateMassiveScaleLOD();
      this.requestRender(60);
    }

    /**
     * 카메라 프리셋 2: 1인칭 통로 시점
     */
    setCameraAisle() {
      this.cameraMode = 'aisle';
      this.isLerpingCamera = false;
      if (this.controls) {
        this.controls.enabled = false;
      }
      this.updateHudModeLabel('🚶 1인칭 통로 시점');

      const selectedId = this.store.state.selectedRackId;
      const racks = this.store.getRacks();
      const targetRack = (selectedId && this.store.getRack(selectedId)) || racks[0];

      let rackX = 2.0;
      let rackZ = 2.0;
      if (targetRack) {
        const effPos = this.store.getRackEffectivePosition(targetRack);
        rackX = (effPos.gridX - 1) * 1.0;
        rackZ = (effPos.gridZ - 1) * 1.0;
      }

      const eyeHeight = 1.45;
      this.camera.position.set(rackX, eyeHeight, rackZ + 2.0);
      this.fpsYaw = 0;   // 정면(-Z 방향) 랙 응시
      this.fpsPitch = 0; // 수평 눈높이
      this.applyFpsAnglesToCamera();
      this.updateMassiveScaleLOD();
      this.requestRender(10);
    }

    /**
     * 카메라 프리셋 3: 상단(Top) 수직 평면 뷰
     */
    setCameraTop() {
      this.cameraMode = 'top';
      if (this.controls) {
        this.controls.enabled = true;
        this.controls.enableZoom = true;
        this.controls.rotateSpeed = 0.7 * this.mouseSensitivity;
        this.controls.maxPolarAngle = Math.PI / 2 - 0.01;
      }
      this.updateHudModeLabel('📐 상단 평면 시점 (벤더 비중)');

      const racks = this.store.getRacks();
      let avgX = 5.5;
      let avgZ = 5.5;
      if (racks.length > 0) {
        let minX = Infinity, maxX = -Infinity, minZ = Infinity, maxZ = -Infinity;
        racks.forEach(r => {
          const p = this.store.getRackEffectivePosition(r);
          const x = p.gridX - 1;
          const z = p.gridZ - 1;
          if (x < minX) minX = x;
          if (x > maxX) maxX = x;
          if (z < minZ) minZ = z;
          if (z > maxZ) maxZ = z;
        });
        avgX = (minX + maxX) / 2;
        avgZ = (minZ + maxZ) / 2;
      }

      this.targetControlTarget = new THREE.Vector3(avgX, 0, avgZ);
      this.targetCameraPos = new THREE.Vector3(avgX, 19.5, avgZ + 0.05);
      this.isLerpingCamera = true;
      this.updateMassiveScaleLOD();
      this.requestRender(60);
    }

    rebuildRacksDebounced() {
      if (this._rebuildRacksPending) return;
      this._rebuildRacksPending = true;
      requestAnimationFrame(() => {
        this._rebuildRacksPending = false;
        this.rebuildRacks();
      });
    }

    rebuildFloorDebounced() {
      if (this._rebuildFloorPending) return;
      this._rebuildFloorPending = true;
      requestAnimationFrame(() => {
        this._rebuildFloorPending = false;
        this.rebuildFloor();
      });
    }

    bindStoreEvents() {
      this.store.subscribe('activeSiteChanged', () => {
        this.rebuildFloorDebounced();
        this.rebuildRacksDebounced();
      });

      this.store.subscribe('racksUpdated', () => {
        this.rebuildFloorDebounced();
        this.rebuildRacksDebounced();
      });

      this.store.subscribe('rackDevicesChanged', (rackId) => {
        this.rebuildSingleRack(rackId);
      });

      this.store.subscribe('devicesUpdated', () => {
        this.rebuildRacksDebounced();
      });

      this.store.subscribe('vendorColorsChanged', () => {
        if (global.DCTextureGenerator) {
          global.DCTextureGenerator.clearDeviceAndRoofCache();
        }
        this.rebuildRacksDebounced();
      });

      this.store.subscribe('invertXYChanged', () => {
        this.rebuildFloorDebounced();
        this.rebuildRacksDebounced();
      });

      this.store.subscribe('siteGridUpdated', () => {
        this.rebuildFloorDebounced();
        this.rebuildRacksDebounced();
      });

      this.store.subscribe('layoutLoaded', () => {
        this.rebuildFloor();
        this.rebuildRacks();
        this.setCameraHome();
      });

      this.store.subscribe('rackSelected', (payload) => {
        const rackId = payload && typeof payload === 'object' ? payload.rackId : payload;
        const prevRackId = payload && typeof payload === 'object' ? payload.prevRackId : null;

        const isMassive = (this.store.getRacks() || []).length > 120;
        if (isMassive) {
          if (prevRackId) this.rebuildSingleRack(prevRackId);
          if (rackId) this.rebuildSingleRack(rackId);
        } else {
          if (prevRackId && this.rackGroupMap.has(prevRackId)) {
            this.rackBuilder.updateRackSelectionVisual(this.rackGroupMap.get(prevRackId), false);
          }
          if (rackId && this.rackGroupMap.has(rackId)) {
            this.rackBuilder.updateRackSelectionVisual(this.rackGroupMap.get(rackId), true);
          }
        }
        if (rackId && this.cameraMode !== 'top') {
          this.focusRack(rackId);
        }
        this.updateMassiveScaleLOD();
        this.requestRender(45);
      });

      this.store.subscribe('globalOrientationChanged', () => this.rebuildRacksDebounced());
      this.store.subscribe('simulationViewChanged', () => this.rebuildRacksDebounced());
    }

    onWindowResize() {
      if (!this.container || !this.renderer || !this.camera) return;
      const width = this.container.clientWidth;
      const height = this.container.clientHeight;
      if (!width || !height) return;
      if (width === this._lastWidth && height === this._lastHeight) return;

      this._lastWidth = width;
      this._lastHeight = height;

      this.camera.aspect = width / height;
      this.camera.updateProjectionMatrix();

      this.renderer.setSize(width, height);
      this.requestRender(5);
    }

    animate() {
      this.animationFrameId = requestAnimationFrame(this._boundAnimate);

      const movedByKeys = this.updateKeyboardMovement();

      if (this.isLerpingCamera && this.targetCameraPos && this.targetControlTarget) {
        this.camera.position.lerp(this.targetCameraPos, 0.12);
        if (this.controls) {
          this.controls.target.lerp(this.targetControlTarget, 0.12);
        }
        this.requestRender(2);

        if (this.camera.position.distanceTo(this.targetCameraPos) < 0.03) {
          this.camera.position.copy(this.targetCameraPos);
          if (this.controls) {
            this.controls.target.copy(this.targetControlTarget);
          }
          this.isLerpingCamera = false;
          this.syncFpsAnglesFromCamera();
          this.updateMassiveScaleLOD();
          this.requestRender(5);
        }
      }

      // 유휴 상태(조작 없음, 카메라 이동 완료)에서는 WebGL 드로우콜 및 컨트롤 연산을 100% 생략하여 메모리 상승(OOM) 차단
      const shouldRender = movedByKeys || this.isLerpingCamera || this._isControlActive || this._renderFramesRemaining > 0;
      if (!shouldRender) {
        return;
      }

      if (this._renderFramesRemaining > 0 && !this._isControlActive && !this.isLerpingCamera && !movedByKeys) {
        this._renderFramesRemaining--;
      }

      if (this.controls && this.controls.enabled) {
        this.controls.update();
      }

      if (this.renderer && this.scene && this.camera) {
        this.renderer.render(this.scene, this.camera);
      }
    }

    destroy() {
      if (this.animationFrameId) {
        cancelAnimationFrame(this.animationFrameId);
      }
      if (this._resizeObserver) {
        this._resizeObserver.disconnect();
      }
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = SceneManager;
  } else {
    global.DCSceneManager = SceneManager;
  }

})(typeof window !== 'undefined' ? window : this);
