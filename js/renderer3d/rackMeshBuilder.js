/**
 * rackMeshBuilder.js - 52U 가변 랙 3D 프레임 생성, 전면 기둥 U 눈금, 상단뷰 벤더 점유율 루프 텍스처 및 3D 물리 명판(Depth Occlusion 지원)
 * - 대규모 환경(랙 2,000개 / 서버 10,000대) 60fps 유지를 위한 지오메트리·재질 풀링(Shared Pooling) 적용
 */
(function (global) {
  'use strict';

  const U_HEIGHT = 0.04445;
  const RACK_WIDTH = 0.60;  // 600mm
  const RACK_DEPTH = 1.05;  // 1050mm 표준 서버 랙 깊이
  const PILLAR_THICKNESS = 0.04;

  class RackMeshBuilder {
    constructor(deviceMeshBuilder, store) {
      this.deviceBuilder = deviceMeshBuilder;
      this.store = store;
      this.sharedGeos = {};
      this.sharedMats = {};
    }

    _getBaseGeo() {
      if (!this.sharedGeos.base) {
        this.sharedGeos.base = new THREE.BoxGeometry(RACK_WIDTH, 0.04, RACK_DEPTH);
        this.sharedGeos.base.userData = { shared: true };
      }
      return this.sharedGeos.base;
    }

    _getHeaderPlateGeo() {
      if (!this.sharedGeos.headerPlate) {
        this.sharedGeos.headerPlate = new THREE.BoxGeometry(RACK_WIDTH - 0.02, 0.11, 0.025);
        this.sharedGeos.headerPlate.userData = { shared: true };
      }
      return this.sharedGeos.headerPlate;
    }

    _getPillarGeo(maxU) {
      const key = `pillar_${maxU}`;
      if (!this.sharedGeos[key]) {
        const rackHeight = maxU * U_HEIGHT;
        const geo = new THREE.BoxGeometry(PILLAR_THICKNESS, rackHeight, PILLAR_THICKNESS);
        geo.userData = { shared: true };
        this.sharedGeos[key] = geo;
      }
      return this.sharedGeos[key];
    }

    _getDividerGeo() {
      if (!this.sharedGeos.divider) {
        this.sharedGeos.divider = new THREE.BoxGeometry(RACK_WIDTH - 0.02, 0.008, RACK_DEPTH + 0.01);
        this.sharedGeos.divider.userData = { shared: true };
      }
      return this.sharedGeos.divider;
    }

    _getSidePanelGeo(maxU) {
      const key = `sidePanel_${maxU}`;
      if (!this.sharedGeos[key]) {
        const rackHeight = maxU * U_HEIGHT;
        const geo = new THREE.BoxGeometry(0.014, rackHeight - 0.02, RACK_DEPTH - 0.06);
        geo.userData = { shared: true };
        this.sharedGeos[key] = geo;
      }
      return this.sharedGeos[key];
    }

    _getSidePanelMat() {
      if (!this.sharedMats.sidePanel) {
        const mat = new THREE.MeshStandardMaterial({
          color: 0x16191f,
          metalness: 0.7,
          roughness: 0.5
        });
        mat.userData = { shared: true };
        this.sharedMats.sidePanel = mat;
      }
      return this.sharedMats.sidePanel;
    }

    _getDividerMat() {
      if (!this.sharedMats.divider) {
        const mat = new THREE.MeshBasicMaterial({ color: 0x00e5ff });
        mat.userData = { shared: true };
        this.sharedMats.divider = mat;
      }
      return this.sharedMats.divider;
    }

    _getRulerMat(maxU, serverMaxU, texGen) {
      const key = `ruler_${maxU}_${serverMaxU}`;
      if (!this.sharedMats[key]) {
        const rulerTex = texGen ? texGen.getRackPostRulerTexture(maxU, serverMaxU) : null;
        const mat = new THREE.MeshBasicMaterial({
          map: rulerTex,
          color: 0xffffff,
          fog: false
        });
        mat.userData = { shared: true };
        this.sharedMats[key] = mat;
      }
      return this.sharedMats[key];
    }

    /**
     * 랙 3D 그룹 생성
     */
    createRackGroup(rack, devices = [], options = {}) {
      if (typeof THREE === 'undefined') return null;

      const maxU = rack.maxU || 52;
      const serverMaxU = rack.serverMaxU !== undefined ? rack.serverMaxU : Math.max(1, maxU - 4);
      const rackHeight = maxU * U_HEIGHT;
      const rackId = String(rack.rackId).toUpperCase();

      const group = new THREE.Group();
      group.name = `RackGroup_${rackId}`;

      const stats = this.store ? this.store.getRackStats(rackId) : {
        usedU: 0,
        freeU: maxU,
        serverFreeU: serverMaxU,
        maxU: maxU,
        serverMaxU: serverMaxU,
        percentage: 0,
        color: '#4caf50',
        vendorBreakdown: []
      };

      const isSelected = options.selectedRackId === rackId;
      const frameColorHex = isSelected ? 0x2979ff : parseInt(stats.color.replace('#', '0x'), 16);
      const enableShadows = !options.massiveScale;

      // 랙별 프레임 재질 (대규모 뷰에서는 동일 색상 랙들끼리 단일 재질 인스턴스를 공유하여 메모리 대폭 절감)
      const frameMatKey = `frame_${frameColorHex}`;
      let frameMat = (options.massiveScale && !isSelected) ? this.sharedMats[frameMatKey] : null;
      if (!frameMat) {
        frameMat = new THREE.MeshStandardMaterial({
          color: frameColorHex,
          metalness: 0.55,
          roughness: 0.4
        });
        if (options.massiveScale && !isSelected) {
          frameMat.userData = { shared: true, isRackFrameMat: true };
          this.sharedMats[frameMatKey] = frameMat;
        } else {
          frameMat.userData = { isRackFrameMat: true };
        }
      }

      const sidePanelMat = this._getSidePanelMat();
      const texGen = global.DCTextureGenerator;

      // 1. 하단 베이스 (Bottom Plinth)
      const baseGeo = this._getBaseGeo();
      const baseMesh = new THREE.Mesh(baseGeo, frameMat);
      baseMesh.matrixAutoUpdate = false;
      baseMesh.updateMatrix();
      group.add(baseMesh);

      // 2. 상단 캡 (Top Roof) — 상단뷰에서 벤더별 점유율(캡처 4)이 한눈에 보이도록 상판(+Y면)에 벤더 스택 텍스처 매핑
      const roofTex = texGen ? texGen.getRackRoofVendorTexture(rackId, stats, options.massiveScale, isSelected) : null;
      const roofMatKey = roofTex ? (roofTex.uuid || 'roof_tex') : `roof_c_${frameColorHex}`;
      let roofTopMat = (options.massiveScale && !isSelected) ? this.sharedMats[roofMatKey] : null;
      if (!roofTopMat) {
        roofTopMat = new THREE.MeshBasicMaterial({
          map: roofTex,
          color: roofTex ? 0xffffff : frameColorHex,
          fog: false
        });
        if (options.massiveScale && !isSelected) {
          roofTopMat.userData = { shared: true };
          this.sharedMats[roofMatKey] = roofTopMat;
        }
      }
      const topMesh = (options.massiveScale && !isSelected)
        ? new THREE.Mesh(baseGeo, roofTopMat)
        : new THREE.Mesh(baseGeo, [frameMat, frameMat, roofTopMat, frameMat, frameMat, frameMat]);
      topMesh.position.set(0, rackHeight + 0.02, 0);
      topMesh.castShadow = enableShadows;
      topMesh.matrixAutoUpdate = false;
      topMesh.updateMatrix();
      group.add(topMesh);

      // 3. 4개 수직 기둥 (선택된 랙에는 전면 기둥 1U~52U 눈금 텍스처 부착, 대규모 뷰에서는 단일 재질로 드로우콜 12회->2회 절감)
      const halfW = (RACK_WIDTH - PILLAR_THICKNESS) / 2;
      const halfD = (RACK_DEPTH - PILLAR_THICKNESS) / 2;
      const pillarGeo = this._getPillarGeo(maxU);
      const rulerMat = (texGen && (!options.massiveScale || isSelected)) ? this._getRulerMat(maxU, serverMaxU, texGen) : frameMat;
      const frontPillarMats = (options.massiveScale && !isSelected)
        ? frameMat
        : [frameMat, frameMat, frameMat, frameMat, rulerMat, rulerMat];

      [
        [-halfW, -halfD, false],
        [halfW, -halfD, false],
        [-halfW, halfD, true],
        [halfW, halfD, true]
      ].forEach(([x, z, isFront]) => {
        const pillar = new THREE.Mesh(pillarGeo, isFront ? frontPillarMats : frameMat);
        pillar.position.set(x, rackHeight / 2 + 0.02, z);
        pillar.castShadow = enableShadows;
        pillar.matrixAutoUpdate = false;
        pillar.updateMatrix();
        group.add(pillar);
      });

      // 4. 48U 서버 영역과 49~52U 네트워크 영역 경계 구분선 (시안 가이드 바)
      if (serverMaxU < maxU) {
        const dividerY = serverMaxU * U_HEIGHT + 0.04;
        const divMesh = new THREE.Mesh(this._getDividerGeo(), this._getDividerMat());
        divMesh.position.set(0, dividerY, 0);
        divMesh.matrixAutoUpdate = false;
        divMesh.updateMatrix();
        group.add(divMesh);
      }

      // 5. 측면 패널 (Left & Right Side Panels)
      const sidePanelGeo = this._getSidePanelGeo(maxU);
      const leftPanel = new THREE.Mesh(sidePanelGeo, sidePanelMat);
      leftPanel.position.set(-RACK_WIDTH / 2 + 0.007, rackHeight / 2 + 0.02, 0);
      leftPanel.matrixAutoUpdate = false;
      leftPanel.updateMatrix();
      group.add(leftPanel);

      const rightPanel = new THREE.Mesh(sidePanelGeo, sidePanelMat);
      rightPanel.position.set(RACK_WIDTH / 2 - 0.007, rackHeight / 2 + 0.02, 0);
      rightPanel.matrixAutoUpdate = false;
      rightPanel.updateMatrix();
      group.add(rightPanel);

      let hasDevices = false;
      let deviceGroupRef = null;

      // 6. 랙 내부 장비 메쉬 추가 (lazyDevices 가 아닌 경우 또는 선택된 랙인 경우 즉시 생성)
      if (!options.lazyDevices || isSelected) {
        const deviceGroup = new THREE.Group();
        deviceGroup.name = `DeviceGroup_${rackId}`;
        deviceGroup.position.set(0, 0.04, 0);
        deviceGroup.matrixAutoUpdate = false;
        deviceGroup.updateMatrix();

        const devOptions = {
          ...options,
          isDetailed: isSelected || !options.massiveScale,
          vendorColors: this.store ? this.store.state.vendorColors : undefined
        };

        for (let i = 0; i < devices.length; i++) {
          const devMesh = this.deviceBuilder.createDeviceMesh(devices[i], rack, devOptions);
          if (devMesh) {
            devMesh.matrixAutoUpdate = false;
            devMesh.updateMatrix();
            deviceGroup.add(devMesh);
          }
        }
        group.add(deviceGroup);
        hasDevices = true;
        deviceGroupRef = deviceGroup;
      }

      // 7. 3D 물리적 랙 상단 명판 (HTML CSS2D 대신 WebGL 3D 메쉬로 부착하여 앞 랙에 의해 뒤에 있는 랙 라벨이 100% 가려지도록 처리!)
      if (options.showBadges !== false && texGen) {
        const plateTex = texGen.getRackHeaderPlateTexture(rack.name || rackId, isSelected, options.massiveScale);
        const plateFaceMat = new THREE.MeshBasicMaterial({ map: plateTex, fog: false });
        plateFaceMat.userData = { isRackHeaderPlate: true, rackName: rack.name || rackId };

        const plateMaterials = [
          frameMat,     // +X
          frameMat,     // -X
          frameMat,     // +Y
          frameMat,     // -Y
          plateFaceMat, // +Z (전면 명판)
          plateFaceMat  // -Z (후면 명판)
        ];

        const plateGeo = this._getHeaderPlateGeo();
        const frontPlate = new THREE.Mesh(plateGeo, plateMaterials);
        frontPlate.position.set(0, rackHeight + 0.075, RACK_DEPTH / 2 - 0.01);
        frontPlate.matrixAutoUpdate = false;
        frontPlate.updateMatrix();
        group.add(frontPlate);

        const backPlate = new THREE.Mesh(plateGeo, plateMaterials);
        backPlate.position.set(0, rackHeight + 0.075, -RACK_DEPTH / 2 + 0.01);
        backPlate.matrixAutoUpdate = false;
        backPlate.updateMatrix();
        group.add(backPlate);
      }

      // 랙 위치 및 회전 설정 (X/Y 반전 상태 반영)
      const tileSize = options.tileSize || 1.0;
      const effPos = this.store ? this.store.getRackEffectivePosition(rack) : {
        gridX: rack.gridX || 3,
        gridZ: rack.gridZ || 3
      };
      const posX = (effPos.gridX - 1) * tileSize;
      const posZ = (effPos.gridZ - 1) * tileSize;

      group.position.set(posX, 0, posZ);

      const rotDeg = rack.rotationDeg || 0;
      group.rotation.y = THREE.MathUtils.degToRad(rotDeg);

      group.matrixAutoUpdate = false;
      group.updateMatrix();

      group.userData = {
        type: 'rack',
        rackId: rackId,
        rack: rack,
        hasDevices: hasDevices,
        devGroup: deviceGroupRef,
        name: rack.name || rackId,
        gridX: effPos.gridX,
        gridZ: effPos.gridZ,
        posX: posX,
        posZ: posZ,
        maxU: maxU,
        serverMaxU: serverMaxU,
        rotationDeg: rotDeg,
        stats: stats
      };

      return group;
    }

    /**
     * 해제되는 텍스처를 쓰던 공유 재질(상판 템플릿, 눈금 등)을 풀에서 제거
     */
    dropMaterialsForTexture(texture) {
      if (!texture) return;
      Object.keys(this.sharedMats).forEach(key => {
        const mat = this.sharedMats[key];
        if (mat && mat.map === texture) {
          mat.dispose();
          delete this.sharedMats[key];
        }
      });
    }

    /**
     * 전체 랙을 다시 만들지 않고 선택 하이라이트 상태만 O(1)로 즉시 업데이트
     */
    updateRackSelectionVisual(rackGroup, isSelected) {
      if (!rackGroup) return;
      const stats = rackGroup.userData.stats || { color: '#4caf50' };
      const frameColorHex = isSelected ? 0x2979ff : parseInt(stats.color.replace('#', '0x'), 16);
      const texGen = global.DCTextureGenerator;

      rackGroup.traverse(obj => {
        if (obj.isMesh && obj.material) {
          if (Array.isArray(obj.material)) {
            obj.material.forEach(m => {
              if (!m) return;
              if (m.userData && m.userData.isRackFrameMat) {
                m.color.setHex(frameColorHex);
              }
              if (m.userData && m.userData.isRackHeaderPlate && texGen) {
                m.map = texGen.getRackHeaderPlateTexture(m.userData.rackName, isSelected, false);
                m.needsUpdate = true;
              }
            });
          } else if (obj.material.userData && obj.material.userData.isRackFrameMat) {
            obj.material.color.setHex(frameColorHex);
          }
        }
      });
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = RackMeshBuilder;
  } else {
    global.DCRackMeshBuilder = new RackMeshBuilder(global.DCDeviceMeshBuilder, global.DCStore);
  }

})(typeof window !== 'undefined' ? window : this);
