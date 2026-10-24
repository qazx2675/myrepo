/**
 * deviceMeshBuilder.js - 3D 서버 및 스위치 장비 메쉬 생성
 * - 지오메트리/재질 풀링(Material Cache) 및 고해상도 U위치·벤더·모델 텍스처 매핑 (서버 10,000대 무지연 대응)
 */
(function (global) {
  'use strict';

  const U_HEIGHT = 0.04445; // 1U 표준 높이 (미터 단위)
  const DEVICE_WIDTH = 0.52; // 19인치 랙 실장 장비 표준 폭
  const DEVICE_DEPTH = 0.88; // 서버/스위치 표준 깊이

  class DeviceMeshBuilder {
    constructor(textureGenerator) {
      this.textureGen = textureGenerator;
      this.geometryCache = new Map();
      this.wireGeometryCache = new Map();
      this.faceMaterialCache = new Map();
      this.chassisMatNormal = null;
      this.chassisMatSim = null;
      this.wireMatSim = null;
    }

    _ensureSharedMaterials() {
      if (typeof THREE === 'undefined') return;
      if (!this.chassisMatNormal) {
        this.chassisMatNormal = new THREE.MeshStandardMaterial({
          color: 0x22262c,
          metalness: 0.7,
          roughness: 0.4
        });
        this.chassisMatNormal.userData = { shared: true };
      }
      if (!this.chassisMatSim) {
        this.chassisMatSim = new THREE.MeshStandardMaterial({
          color: 0x1a365d,
          metalness: 0.7,
          roughness: 0.4
        });
        this.chassisMatSim.userData = { shared: true };
      }
      if (!this.wireMatSim) {
        this.wireMatSim = new THREE.LineBasicMaterial({
          color: 0x00e5ff,
          linewidth: 2
        });
        this.wireMatSim.userData = { shared: true };
      }
    }

    _getBoxGeometry(formFactorU) {
      if (!this.geometryCache.has(formFactorU)) {
        const height = formFactorU * U_HEIGHT;
        const geo = new THREE.BoxGeometry(DEVICE_WIDTH, height - 0.002, DEVICE_DEPTH);
        geo.userData = { shared: true };
        this.geometryCache.set(formFactorU, geo);
      }
      return this.geometryCache.get(formFactorU);
    }

    _getWireGeometry(formFactorU) {
      if (!this.wireGeometryCache.has(formFactorU)) {
        const boxGeo = this._getBoxGeometry(formFactorU);
        const wireGeo = new THREE.EdgesGeometry(boxGeo);
        wireGeo.userData = { shared: true };
        this.wireGeometryCache.set(formFactorU, wireGeo);
      }
      return this.wireGeometryCache.get(formFactorU);
    }

    _getFaceMaterial(texture, isSim, isBack = false) {
      const texId = texture ? (texture.uuid || 'notex') : 'notex';
      const key = `${texId}_${isSim ? '1' : '0'}_${isBack ? 'B' : 'F'}`;
      if (!this.faceMaterialCache.has(key)) {
        // 조명 각도나 거리 안개(Fog)에 의해 글씨가 흐려지거나 어두워지지 않도록 100% 원색 발광(MeshBasicMaterial + fog: false) 적용
        const mat = new THREE.MeshBasicMaterial({
          map: texture,
          color: 0xffffff,
          fog: false
        });
        mat.userData = { shared: true };
        this.faceMaterialCache.set(key, mat);
      }
      return this.faceMaterialCache.get(key);
    }

    /**
     * 해제되는 텍스처를 쓰던 면 재질을 캐시에서 제거 (재질이 텍스처/캔버스를 붙잡아 GC 되지 않던 누수 차단)
     */
    dropMaterialsForTexture(texture) {
      if (!texture) return;
      for (const [key, mat] of Array.from(this.faceMaterialCache.entries())) {
        if (mat.map === texture) {
          mat.dispose();
          this.faceMaterialCache.delete(key);
        }
      }
    }

    _resolveVendor(device, options) {
      const vendorColors = options.vendorColors || (global.DCValidator && global.DCValidator.DEFAULT_VENDOR_COLORS) || {};
      const vendor = device.vendor || (global.DCValidator ? global.DCValidator.determineVendor(device.hostname, device.model) : 'OTHER');
      const vColor = vendorColors[vendor] || device.colorTag || '#4caf50';
      return { vendor, vColor };
    }

    _getMassiveTemplateMaterial(device, vColor, isSim, isFacingBack, isSwitch) {
      if (isSwitch) {
        const tex = this.textureGen.getSwitchTexture(device, isFacingBack);
        return this._getFaceMaterial(tex, isSim, isFacingBack);
      }
      const tex = isFacingBack
        ? this.textureGen.getServerBackTexture(device, vColor, true, false)
        : this.textureGen.getServerFrontTexture(device, vColor, true, false);
      return this._getFaceMaterial(tex, isSim, isFacingBack);
    }

    getDeviceLocalY(device) {
      const height = (device.formFactorU || 1) * U_HEIGHT;
      const startU = device.startU || 1;
      return (startU - 1) * U_HEIGHT + height / 2;
    }

    /**
     * 대규모 비상세 템플릿 서버(인스턴싱 대상) 여부 - createDeviceMesh 의 템플릿 분기와 같은 조건
     */
    isInstanceable(device, options) {
      const isDetailed = !!options.isDetailed || !options.massiveScale;
      return !!options.massiveScale && !isDetailed && device.deviceType !== 'switch' && !device.isSimulation;
    }

    /**
     * 인스턴싱 대상 장비의 공유 지오메트리/템플릿 재질/랙 내부 높이 (createDeviceMesh 와 같은 순서로 텍스처 요청)
     */
    getInstanceParts(device, options) {
      this._ensureSharedMaterials();
      const { vColor } = this._resolveVendor(device, options);
      const isFacingBack = (device.orientation || options.globalOrientation || 'front') === 'back';
      return {
        geometry: this._getBoxGeometry(device.formFactorU || 1),
        material: this._getMassiveTemplateMaterial(device, vColor, false, isFacingBack, false),
        localY: this.getDeviceLocalY(device)
      };
    }

    /**
     * 단일 장비(서버 또는 스위치) 3D 메쉬 생성
     */
    createDeviceMesh(device, rack, options = {}) {
      if (typeof THREE === 'undefined') return null;
      this._ensureSharedMaterials();

      const formFactorU = device.formFactorU || 1;
      const height = formFactorU * U_HEIGHT;
      const isSim = !!device.isSimulation;
      const isSwitch = device.deviceType === 'switch';

      const geometry = this._getBoxGeometry(formFactorU);
      const { vendor, vColor } = this._resolveVendor(device, options);

      const orientation = device.orientation || options.globalOrientation || 'front';
      const isFacingBack = orientation === 'back';
      const isDetailed = !!options.isDetailed || !options.massiveScale;

      let activeMat = null;
      if (options.massiveScale && !isDetailed) {
        activeMat = this._getMassiveTemplateMaterial(device, vColor, isSim, isFacingBack, isSwitch);
      } else {
        let frontTex = null;
        let backTex = null;
        if (isSwitch) {
          frontTex = this.textureGen.getSwitchTexture(device, false);
          backTex = this.textureGen.getSwitchTexture(device, true);
        } else {
          frontTex = this.textureGen.getServerFrontTexture(device, vColor, options.massiveScale, isDetailed);
          backTex = this.textureGen.getServerBackTexture(device, vColor, options.massiveScale, isDetailed);
        }
        const chassisMat = isSim ? this.chassisMatSim : this.chassisMatNormal;
        const frontMat = this._getFaceMaterial(frontTex, isSim, false);
        const backMat = this._getFaceMaterial(backTex, isSim, true);
        activeMat = [
          chassisMat,
          chassisMat,
          chassisMat,
          chassisMat,
          isFacingBack ? backMat : frontMat,
          isFacingBack ? frontMat : backMat
        ];
      }

      const mesh = new THREE.Mesh(geometry, activeMat);
      mesh.castShadow = false;
      mesh.receiveShadow = false;
      mesh.matrixAutoUpdate = false;

      const posY = this.getDeviceLocalY(device);
      mesh.position.set(0, posY, 0);
      mesh.updateMatrix();

      mesh.userData = {
        type: 'device',
        deviceId: device.deviceId,
        rackId: device.rackId,
        hostname: device.hostname || '[임시할당]',
        vendor: vendor,
        model: device.model,
        startU: device.startU,
        endU: device.endU,
        formFactorU: device.formFactorU,
        deviceType: device.deviceType,
        orientation: orientation,
        isSimulation: isSim
      };

      // 시뮬레이션 하이라이트 와이어프레임 외곽선 추가
      if (isSim && options.simulationView !== 'after_real') {
        const wireGeo = this._getWireGeometry(formFactorU);
        const wireframe = new THREE.LineSegments(wireGeo, this.wireMatSim);
        mesh.add(wireframe);
      }

      return mesh;
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = DeviceMeshBuilder;
  } else {
    global.DCDeviceMeshBuilder = new DeviceMeshBuilder(global.DCTextureGenerator);
  }

})(typeof window !== 'undefined' ? window : this);
