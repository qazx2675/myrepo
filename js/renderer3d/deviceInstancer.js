/**
 * deviceInstancer.js - 대규모 장면의 비선택 랙 장비를 InstancedMesh 로 묶는 인스턴스 레이어
 * - 그룹 키 = 공유 지오메트리 uuid + 템플릿 재질 uuid. 재질은 템플릿을 clone 한 인스턴스 전용 재질(정점 변환만 다름)
 * - 화면 동일: 장비별 modelView(카메라 역행렬 x 장비 world, double)를 CPU 에서 계산해 instanceMatrix 에 넣고
 *   셰이더는 그 행렬 하나만 곱한다 (개별 Mesh 의 modelViewMatrix * pos 와 같은 식·같은 입력값)
 * - 숨김: 배치마다 보이는 슬롯만 앞으로 모으고 count 를 줄임 (렌더/raycast 모두 count 까지만)
 */
(function (global) {
  'use strict';

  // 모든 인스턴스 재질이 같은 함수 객체를 써서 셰이더 프로그램 1개를 공유 (program 캐시 키 = onBeforeCompile.toString())
  function PROJECT_PATCH(shader) {
    shader.vertexShader = shader.vertexShader.replace('#include <project_vertex>',
      'vec4 mvPosition = vec4( transformed, 1.0 );\nmvPosition = instanceMatrix * mvPosition;\ngl_Position = projectionMatrix * mvPosition;');
  }

  let _inst = null;
  let _w = null;
  let _dummy = null;
  const _hits = [];

  // r128 InstancedMesh.raycast 와 같은 루프. 행렬은 instanceMatrix(modelView) 대신 원본 world(double)에서 읽는다
  function batchRaycast(raycaster, intersects) {
    const b = this.userData.batch;
    if (!b || this.material === undefined) return;
    if (!_dummy) {
      _inst = new THREE.Matrix4();
      _w = new THREE.Matrix4();
      _dummy = new THREE.Mesh();
    }
    _dummy.geometry = this.geometry;
    _dummy.material = this.material;
    for (let s = 0; s < this.count; s++) {
      _inst.fromArray(b.master, 16 * b.slotToEntry[s]);
      _w.multiplyMatrices(this.matrixWorld, _inst);
      _dummy.matrixWorld = _w;
      _dummy.raycast(raycaster, _hits);
      for (let i = 0; i < _hits.length; i++) {
        _hits[i].instanceId = s;
        _hits[i].object = this;
        intersects.push(_hits[i]);
      }
      _hits.length = 0;
    }
  }

  class InstanceLayer {
    constructor(name, hitType) {
      this.group = new THREE.Group();
      this.group.name = name;
      this.group.matrixAutoUpdate = false;
      this.hitType = hitType;
      this._batches = [];
      this._staging = null;
      this._rackToBatches = new Map();
      this._built = new Set();
      this._invalid = new Set();
      this._selected = new Set();
      this._instMats = new Map();
      this._lastView = new Float64Array(16).fill(NaN);
      this._local = new THREE.Matrix4();
      this._world = new THREE.Matrix4();
      this._mv = new THREE.Matrix4();
      this._pos = new THREE.Vector3();
      this._quat = new THREE.Quaternion();
      this._scale = new THREE.Vector3(1, 1, 1);
    }

    get meshCount() {
      return this._batches.length;
    }

    _instMaterial(tpl) {
      let m = this._instMats.get(tpl.uuid);
      if (!m) {
        m = tpl.clone();
        m.onBeforeCompile = PROJECT_PATCH;
        m.userData = { shared: true };
        this._instMats.set(tpl.uuid, m);
      }
      return m;
    }

    begin() {
      this.clear();
      this._staging = new Map();
    }

    addInstance(rackKey, geometry, templateMaterial, worldMatrix, hitId, hitRackId) {
      const key = geometry.uuid + '|' + templateMaterial.uuid;
      let st = this._staging.get(key);
      if (!st) {
        st = { geometry, templateMaterial, worlds: [], hitIds: [], hitRackIds: [], ranges: [] };
        this._staging.set(key, st);
      }
      const k = st.hitIds.length;
      const e = worldMatrix.elements;
      for (let i = 0; i < 16; i++) st.worlds.push(e[i]);
      st.hitIds.push(hitId);
      st.hitRackIds.push(hitRackId);
      const last = st.ranges[st.ranges.length - 1];
      if (last && last[0] === rackKey && last[2] === k) last[2] = k + 1;
      else st.ranges.push([rackKey, k, k + 1]);
    }

    // 장비 world = baseMatrix x T(0, localY, 0) (개별 Mesh 의 matrixWorld 와 같은 곱셈 순서)
    _addDevices(rackKey, baseMatrix, devices, deviceBuilder, devOpts) {
      for (let i = 0; i < devices.length; i++) {
        const device = devices[i];
        if (!deviceBuilder.isInstanceable(device, devOpts)) continue;
        const parts = deviceBuilder.getInstanceParts(device, devOpts);
        this._pos.set(0, parts.localY, 0);
        this._local.compose(this._pos, this._quat, this._scale);
        this._world.multiplyMatrices(baseMatrix, this._local);
        this.addInstance(rackKey, parts.geometry, parts.material, this._world, device.deviceId, device.rackId);
      }
    }

    addRackDevices(rackKey, baseMatrix, devices, deviceBuilder, devOpts) {
      this._built.add(rackKey);
      this._addDevices(rackKey, baseMatrix, devices, deviceBuilder, devOpts);
    }

    commit() {
      if (!this._staging) return;
      for (const [key, st] of this._staging) {
        this._createBatch(key, st.geometry, st.templateMaterial, new Float64Array(st.worlds), st.hitIds, st.hitRackIds, st.ranges);
      }
      this._staging = null;
    }

    _createBatch(key, geometry, templateMaterial, master, hitIds, hitRackIds, ranges) {
      const n = hitIds.length;
      const mesh = new THREE.InstancedMesh(geometry, this._instMaterial(templateMaterial), n);
      mesh.instanceMatrix.setUsage(THREE.DynamicDrawUsage);
      mesh.matrixAutoUpdate = false;
      mesh.castShadow = false;
      mesh.receiveShadow = false;
      mesh.raycast = batchRaycast;
      const batch = { key, geometry, templateMaterial, mesh, master, hitIds, hitRackIds, ranges, slotToEntry: new Int32Array(n), dirty: true };
      mesh.userData = { instanceLayer: this, batch };
      this.group.add(mesh);
      this._batches.push(batch);
      for (let i = 0; i < ranges.length; i++) {
        let set = this._rackToBatches.get(ranges[i][0]);
        if (!set) {
          set = new Set();
          this._rackToBatches.set(ranges[i][0], set);
        }
        set.add(batch);
      }
      this._compact(batch);
      return batch;
    }

    _disposeBatch(batch) {
      this.group.remove(batch.mesh);
      batch.mesh.dispose();
      batch.mesh.userData = {};
    }

    /**
     * 전체 재구성 때 선택 상태였던 랙을 선택 해제 시 기존 레이어에 추가 (키별로 기존 배치에 이어 붙이거나 새 배치)
     */
    appendRack(rackKey, baseMatrix, devices, deviceBuilder, devOpts) {
      if (this._built.has(rackKey)) return false;
      this._built.add(rackKey);
      this._staging = new Map();
      this._addDevices(rackKey, baseMatrix, devices, deviceBuilder, devOpts);
      const staged = this._staging;
      this._staging = null;
      if (staged.size === 0) return false;

      for (const [key, st] of staged) {
        const old = this._batches.find(b => b.key === key);
        if (!old) {
          this._createBatch(key, st.geometry, st.templateMaterial, new Float64Array(st.worlds), st.hitIds, st.hitRackIds, st.ranges);
          continue;
        }
        const base = old.hitIds.length;
        const master = new Float64Array(old.master.length + st.worlds.length);
        master.set(old.master, 0);
        master.set(st.worlds, old.master.length);
        const ranges = old.ranges.concat(st.ranges.map(r => [r[0], r[1] + base, r[2] + base]));
        this._disposeBatch(old);
        this._batches.splice(this._batches.indexOf(old), 1);
        for (const set of this._rackToBatches.values()) set.delete(old);
        this._createBatch(key, old.geometry, old.templateMaterial, master,
          old.hitIds.concat(st.hitIds), old.hitRackIds.concat(st.hitRackIds), ranges);
      }
      return true;
    }

    _compact(batch) {
      const slots = batch.slotToEntry;
      let w = 0;
      for (let r = 0; r < batch.ranges.length; r++) {
        const rg = batch.ranges[r];
        if (this._selected.has(rg[0]) || this._invalid.has(rg[0])) continue;
        for (let k = rg[1]; k < rg[2]; k++) slots[w++] = k;
      }
      batch.mesh.count = w;
      batch.mesh.visible = w > 0;
      batch.dirty = true;
    }

    _compactRacks(rackKeys) {
      const touched = new Set();
      for (const rk of rackKeys) {
        const set = this._rackToBatches.get(rk);
        if (set) set.forEach(b => touched.add(b));
      }
      touched.forEach(b => this._compact(b));
      return touched.size > 0;
    }

    setSelectedRacks(ids) {
      const next = new Set(ids || []);
      const diff = [];
      next.forEach(id => { if (!this._selected.has(id)) diff.push(id); });
      this._selected.forEach(id => { if (!next.has(id)) diff.push(id); });
      this._selected = next;
      return diff.length > 0 && this._compactRacks(diff);
    }

    invalidateRack(rackKey) {
      if (this._invalid.has(rackKey)) return false;
      this._invalid.add(rackKey);
      return this._compactRacks([rackKey]);
    }

    isInvalid(rackKey) {
      return this._invalid.has(rackKey);
    }

    coversRack(rackKey) {
      return this._built.has(rackKey) && !this._invalid.has(rackKey);
    }

    /**
     * scene.onBeforeRender 에서 호출: 카메라 행렬 갱신 직후, instanceMatrix 업로드 전
     */
    updateViewMatrices(camera) {
      if (!this.group.visible || this._batches.length === 0) return;
      const view = camera.matrixWorldInverse;
      const ve = view.elements;
      let camChanged = false;
      for (let i = 0; i < 16; i++) {
        if (!Object.is(ve[i], this._lastView[i])) { camChanged = true; break; }
      }
      for (let b = 0; b < this._batches.length; b++) {
        const batch = this._batches[b];
        if (!camChanged && !batch.dirty) continue;
        const mesh = batch.mesh;
        const arr = mesh.instanceMatrix.array;
        const slots = batch.slotToEntry;
        for (let s = 0; s < mesh.count; s++) {
          this._world.fromArray(batch.master, 16 * slots[s]);
          this._mv.multiplyMatrices(view, this._world);
          this._mv.toArray(arr, 16 * s);
        }
        mesh.instanceMatrix.needsUpdate = true;
        batch.dirty = false;
      }
      for (let i = 0; i < 16; i++) this._lastView[i] = ve[i];
    }

    getHitUserData(mesh, instanceId) {
      const batch = mesh && mesh.userData ? mesh.userData.batch : null;
      if (!batch || instanceId === undefined || instanceId === null || instanceId >= mesh.count) return null;
      const e = batch.slotToEntry[instanceId];
      if (this.hitType === 'rack') return { type: 'rack', rackId: batch.hitRackIds[e] };
      return { type: 'device', deviceId: batch.hitIds[e], rackId: batch.hitRackIds[e] };
    }

    /**
     * 해제되는 텍스처를 쓰던 인스턴스 재질을 캐시에서 제거 (DeviceMeshBuilder.dropMaterialsForTexture 와 같은 형태)
     */
    dropMaterialsForTexture(texture) {
      if (!texture) return;
      for (const [key, mat] of Array.from(this._instMats.entries())) {
        if (mat.map === texture) {
          mat.dispose();
          this._instMats.delete(key);
        }
      }
    }

    clear() {
      for (let i = 0; i < this._batches.length; i++) this._disposeBatch(this._batches[i]);
      this._batches = [];
      this._staging = null;
      this._rackToBatches.clear();
      this._built.clear();
      this._invalid.clear();
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = InstanceLayer;
  } else {
    global.DCInstanceLayer = InstanceLayer;
  }

})(typeof window !== 'undefined' ? window : this);
