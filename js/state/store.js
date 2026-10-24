/**
 * store.js - 멀티 센터(자동 생성/전환), 랙(52U/48U 서버영역), 장비, 벤더 색상, X/Y 반전 및 시뮬레이션 상태 관리 스토어
 * - 대규모 데이터(랙 2,000개 / 서버 10,000대 이상) 무지연 처리를 위한 O(1) 해시 인덱싱 및 캐시 적용
 */
(function (global) {
  'use strict';

  const DEFAULT_COLORS = (global.DCValidator && global.DCValidator.DEFAULT_VENDOR_COLORS) || {
    HP: '#00b388',
    DELL: '#007db8',
    CISCO: '#00bceb',
    CUSTOM: '#ff9800',
    SUPERMICRO: '#4caf50',
    LENOVO: '#e53935',
    TEMP: '#ab47bc',
    OTHER: '#78909c'
  };

  class DataCenterStore {
    constructor() {
      this.listeners = new Map();

      // O(1) 고속 조회용 내부 캐시 (2,000랙 / 10,000서버 대응)
      this._axisCache = new Map();     // siteId -> { xMap: Map<number, number>, yMap: Map<number, number> }
      this._rackLookupMap = new Map(); // `${siteId}::${rackId}` -> rack
      this._deviceIndex = null;        // `${siteId}::${rackId}` -> Device[]
      this._statsCache = new Map();    // `${siteId}::${rackId}::${simView}` -> stats

      // 기본 상태 초기화
      this.state = {
        version: "2.2.0",
        activeSiteId: "SITE-A",
        // false (기본): X가 행(Row, 세로축 Z방향 나열, 간격 4칸), Y가 열(Column, 가로축 X방향 나열)
        // true (반전): X가 열(Column, 가로축), Y가 행(Row, 세로축)
        invertXY: false,
        vendorColors: { ...DEFAULT_COLORS },
        sites: [
          {
            siteId: "SITE-A",
            name: "SITE-A (기본 센터)",
            gridWidth: 28,
            gridDepth: 28,
            tileSizeMm: 600,
            aisleTiles: [],
            racks: []
          }
        ],
        devices: [],
        selectedRackId: null,
        selectedDeviceId: null,
        simulationMode: false,
        simulationView: 'all', // 'all' (하이라이트), 'before' (기존만), 'after_real' (실제 외형)
        globalOrientation: 'front', // 'front' | 'back'
        searchQuery: '',
        showTiles: true,
        showBadges: true,
        activeFolderHandle: null,
        folderName: null,
        hasUnsavedChanges: false
      };
    }

    /**
     * 내부 캐시 무효화
     */
    _invalidateCaches(type = 'all') {
      if (type === 'all' || type === 'racks') {
        this._axisCache.clear();
        this._rackLookupMap.clear();
      }
      if (type === 'all' || type === 'devices') {
        this._deviceIndex = null;
      }
      this._statsCache.clear();
    }

    /**
     * 이벤트 리스너 등록
     */
    subscribe(event, callback) {
      if (!this.listeners.has(event)) {
        this.listeners.set(event, new Set());
      }
      this.listeners.get(event).add(callback);
      return () => {
        this.listeners.get(event).delete(callback);
      };
    }

    /**
     * 이벤트 발생
     */
    notify(event, data) {
      if (this.listeners.has(event)) {
        this.listeners.get(event).forEach(cb => {
          try {
            cb(data, this.state);
          } catch (e) {
            console.error(`Error in listener for event ${event}:`, e);
          }
        });
      }
      if (event !== 'stateChange' && this.listeners.has('stateChange')) {
        this.listeners.get('stateChange').forEach(cb => {
          try {
            cb({ event, data }, this.state);
          } catch (e) {
            console.error(`Error in stateChange listener:`, e);
          }
        });
      }
    }

    /**
     * 전체 센터(Site) 목록 반환
     */
    getSites() {
      return this.state.sites || [];
    }

    /**
     * 현재 활성 사이트 가져오기
     */
    getActiveSite() {
      return this.state.sites.find(s => s.siteId === this.state.activeSiteId) || this.state.sites[0];
    }

    /**
     * 센터 이름/ID로 기존 센터를 찾거나, 없으면 새로운 센터를 자동 생성하여 반환
     * @param {string} siteNameOrId
     * @returns {{ site: Object, created: boolean }}
     */
    findOrCreateSite(siteNameOrId) {
      const clean = String(siteNameOrId || '').trim() || 'SITE-A';
      const upper = clean.toUpperCase();

      let found = this.state.sites.find(s =>
        String(s.siteId).toUpperCase() === upper ||
        String(s.name || '').toUpperCase() === upper ||
        (upper === 'SITE A' && String(s.siteId).toUpperCase() === 'SITE-A')
      );

      if (found) {
        return { site: found, created: false };
      }

      const newSite = {
        siteId: clean,
        name: clean,
        gridWidth: 28,
        gridDepth: 28,
        tileSizeMm: 600,
        aisleTiles: [],
        racks: []
      };

      this.state.sites.push(newSite);
      this._invalidateCaches('all');
      this.notify('sitesUpdated', this.state.sites);
      return { site: newSite, created: true };
    }

    /**
     * 활성 센터(Site) 전환
     */
    setActiveSite(siteIdOrName) {
      const { site } = this.findOrCreateSite(siteIdOrName);
      if (!site) return null;

      const prevSiteId = this.state.activeSiteId;
      this.state.activeSiteId = site.siteId;
      this.state.selectedRackId = site.racks.length > 0 ? site.racks[0].rackId : null;
      this.state.selectedDeviceId = null;

      this._invalidateCaches('all');
      this.notify('activeSiteChanged', { siteId: site.siteId, prevSiteId, site });
      this.notify('racksUpdated', this.getRacks());
      this.notify('devicesUpdated', this.state.devices);
      return site;
    }

    /**
     * 현재 활성 사이트의 랙 목록
     */
    getRacks() {
      const site = this.getActiveSite();
      return site ? site.racks : [];
    }

    /**
     * 사이트별 X/Y 축 정렬 인덱스 맵 빌드 (2,000개 랙에서도 단 1회만 계산하여 O(1) 조회 보장)
     */
    _getSiteAxisOrder(siteId) {
      if (this._axisCache.has(siteId)) {
        return this._axisCache.get(siteId);
      }

      const site = this.state.sites.find(s => s.siteId === siteId) || this.getActiveSite();
      const allRacks = site ? site.racks : [];
      const xSet = new Set();
      const ySet = new Set();

      for (let i = 0; i < allRacks.length; i++) {
        const r = allRacks[i];
        let rx = r.rackX;
        let ry = r.rackY;
        if (!rx || !ry) {
          const p = global.DCValidator ? global.DCValidator.parseRackXY(r.rackId) : null;
          rx = rx || (p ? p.xNum : 1);
          ry = ry || (p ? p.yNum : 1);
          r.rackX = rx;
          r.rackY = ry;
        }
        xSet.add(rx);
        ySet.add(ry);
      }

      const uniqueXs = Array.from(xSet).sort((a, b) => a - b);
      const uniqueYs = Array.from(ySet).sort((a, b) => a - b);

      const xMap = new Map();
      uniqueXs.forEach((val, idx) => xMap.set(val, idx + 1));

      const yMap = new Map();
      uniqueYs.forEach((val, idx) => yMap.set(val, idx + 1));

      const entry = { xMap, yMap };
      this._axisCache.set(siteId, entry);
      return entry;
    }

    /**
     * 랙의 논리적 X(행), Y(열) 번호 추출 및 실제 그리드 타일 좌표(gridX, gridZ) 계산 (O(1) 캐시 적용)
     */
    getRackEffectivePosition(rack) {
      if (!rack) return { gridX: 3, gridZ: 3, xIdx: 1, yIdx: 1 };

      const siteId = rack.siteId || this.state.activeSiteId;
      let xNum = rack.rackX;
      let yNum = rack.rackY;
      let hasXYPattern = true;

      if (!xNum || !yNum) {
        const xy = global.DCValidator ? global.DCValidator.parseRackXY(rack.rackId) : null;
        if (xy) {
          xNum = xy.xNum;
          yNum = xy.yNum;
          rack.rackX = xNum;
          rack.rackY = yNum;
        } else {
          hasXYPattern = false;
          xNum = xNum || 1;
          yNum = yNum || 1;
        }
      }

      const { xMap, yMap } = this._getSiteAxisOrder(siteId);
      const effectiveXIdx = xMap.get(xNum) || 1;
      const effectiveYIdx = yNum <= 25 ? yNum : (yMap.get(yNum) || yNum);

      if (!hasXYPattern && rack.gridX && rack.gridZ) {
        return {
          gridX: this.state.invertXY ? rack.gridZ : rack.gridX,
          gridZ: this.state.invertXY ? rack.gridX : rack.gridZ,
          xIdx: effectiveXIdx,
          yIdx: effectiveYIdx,
          rawX: xNum,
          rawY: yNum
        };
      }

      if (!this.state.invertXY) {
        // 기본: X가 행(세로 Z 방향, 4칸 간격), Y가 열(가로 X 방향, 1칸 간격)
        const gx = 3 + (effectiveYIdx - 1) * 1;
        const gz = 3 + (effectiveXIdx - 1) * 4;
        return { gridX: gx, gridZ: gz, xIdx: effectiveXIdx, yIdx: effectiveYIdx, rawX: xNum, rawY: yNum };
      } else {
        // 반전(invertXY = true): X가 가로(열), Y가 세로(행, 4칸 간격)
        const gx = 3 + (effectiveXIdx - 1) * 1;
        const gz = 3 + (effectiveYIdx - 1) * 4;
        return { gridX: gx, gridZ: gz, xIdx: effectiveXIdx, yIdx: effectiveYIdx, rawX: xNum, rawY: yNum };
      }
    }

    /**
     * X/Y 축 반전 토글
     */
    toggleInvertXY() {
      this.state.invertXY = !this.state.invertXY;
      this.notify('invertXYChanged', this.state.invertXY);
      this.notify('racksUpdated', this.getRacks());
      return this.state.invertXY;
    }

    /**
     * 벤더 색상 변경 (임시할당 색상 포함)
     */
    setVendorColor(vendorKey, hexColor) {
      if (!vendorKey || !hexColor) return;
      this.state.vendorColors[vendorKey] = hexColor;
      this.state.devices.forEach(d => {
        const v = d.vendor || (global.DCValidator ? global.DCValidator.determineVendor(d.hostname, d.model) : 'OTHER');
        if (v === vendorKey) {
          d.colorTag = hexColor;
        }
      });
      this._invalidateCaches('devices');
      this.state.hasUnsavedChanges = true;
      this.notify('vendorColorsChanged', this.state.vendorColors);
      this.notify('devicesUpdated', this.state.devices);
    }

    /**
     * 특정 랙 가져오기 (O(1) 해시 룩업 지원)
     */
    getRack(rackId, siteId = null) {
      if (!rackId) return null;
      const targetSiteId = siteId || this.state.activeSiteId;
      const rId = String(rackId).toUpperCase();
      const cacheKey = `${targetSiteId}::${rId}`;

      if (this._rackLookupMap.has(cacheKey)) {
        return this._rackLookupMap.get(cacheKey);
      }

      const site = this.state.sites.find(s => s.siteId === targetSiteId) || this.getActiveSite();
      const racks = site ? site.racks : [];

      for (let i = 0; i < racks.length; i++) {
        const r = racks[i];
        const up = String(r.rackId).toUpperCase();
        this._rackLookupMap.set(`${targetSiteId}::${up}`, r);
        if (r.name) {
          this._rackLookupMap.set(`${targetSiteId}::${String(r.name).toUpperCase()}`, r);
        }
      }

      if (this._rackLookupMap.has(cacheKey)) {
        return this._rackLookupMap.get(cacheKey);
      }

      const legacyMap = {
        '2A': 'X001Y001',
        '2B': 'X001Y002',
        '2C': 'X001Y003',
        '2D': 'X001Y004',
        '2E': 'X001Y005',
        '2F': 'X001Y006',
        '2G': 'X001Y007',
        '2H': 'X001Y008'
      };
      if (legacyMap[rId]) {
        return this._rackLookupMap.get(`${targetSiteId}::${legacyMap[rId]}`) || null;
      }
      return null;
    }

    /**
     * 전체 장비를 (siteId::rackId) 기준으로 O(1) 인덱싱
     */
    _ensureDeviceIndex() {
      if (this._deviceIndex) return this._deviceIndex;
      this._deviceIndex = new Map();
      const defaultSiteId = this.state.activeSiteId || 'SITE-A';

      for (let i = 0; i < this.state.devices.length; i++) {
        const d = this.state.devices[i];
        const sId = d.siteId || defaultSiteId;
        const rId = String(d.rackId).toUpperCase();
        const key = `${sId}::${rId}`;
        let list = this._deviceIndex.get(key);
        if (!list) {
          list = [];
          this._deviceIndex.set(key, list);
        }
        list.push(d);
      }

      // 각 랙 내부 장비를 startU 오름차순으로 정렬
      this._deviceIndex.forEach(list => {
        list.sort((a, b) => a.startU - b.startU);
      });

      return this._deviceIndex;
    }

    /**
     * 특정 랙에 속한 장비 목록 (O(1) 조회)
     */
    getDevicesForRack(rackId, ignoreFilter = false, siteId = null) {
      if (!rackId) return [];
      const targetSiteId = siteId || this.state.activeSiteId;
      const rId = String(rackId).toUpperCase();
      const resolved = this.getRack(rId, targetSiteId);
      const targetId = resolved ? String(resolved.rackId).toUpperCase() : rId;

      const idxMap = this._ensureDeviceIndex();
      const devs = idxMap.get(`${targetSiteId}::${targetId}`) || idxMap.get(`${targetSiteId}::${rId}`) || [];

      if (!ignoreFilter && this.state.simulationView === 'before') {
        return devs.filter(d => !d.isSimulation);
      }
      return devs;
    }

    /**
     * 랙별 통계 및 벤더별 점유 비율 계산 (O(1) 캐시 지원)
     */
    getRackStats(rackId, siteId = null) {
      const targetSiteId = siteId || this.state.activeSiteId;
      const rId = String(rackId || '').toUpperCase();
      const cacheKey = `${targetSiteId}::${rId}::${this.state.simulationView}`;

      if (this._statsCache.has(cacheKey)) {
        return this._statsCache.get(cacheKey);
      }

      const rack = this.getRack(rId, targetSiteId);
      const maxU = rack ? (rack.maxU || 52) : 52;
      const serverMaxU = rack ? (rack.serverMaxU !== undefined ? rack.serverMaxU : Math.max(1, maxU - 4)) : 48;

      if (!rack) {
        return {
          usedU: 0,
          freeU: maxU,
          serverUsedU: 0,
          serverFreeU: serverMaxU,
          maxU,
          serverMaxU,
          percentage: 0,
          stagedU: 0,
          color: '#4caf50',
          vendorBreakdown: []
        };
      }

      const activeDevs = this.getDevicesForRack(rId, false, targetSiteId);

      let usedU = 0;
      let serverUsedU = 0;
      let stagedU = 0;
      const vendorUMap = new Map();

      for (let i = 0; i < activeDevs.length; i++) {
        const d = activeDevs[i];
        const uSize = d.formFactorU || 1;
        usedU += uSize;
        if (d.endU <= serverMaxU) {
          serverUsedU += uSize;
        }
        if (d.isSimulation) stagedU += uSize;

        const vKey = d.vendor || (global.DCValidator ? global.DCValidator.determineVendor(d.hostname, d.model) : 'OTHER');
        vendorUMap.set(vKey, (vendorUMap.get(vKey) || 0) + uSize);
      }

      const freeU = Math.max(0, maxU - usedU);
      const serverFreeU = Math.max(0, serverMaxU - serverUsedU);
      const percentage = Math.round((usedU / maxU) * 100);

      let color = '#4caf50';
      if (percentage >= 85) {
        color = '#b84a4a';
      } else if (percentage >= 50) {
        color = '#e6d235';
      }

      const vendorBreakdown = [];
      vendorUMap.forEach((uCount, vKey) => {
        vendorBreakdown.push({
          vendor: vKey,
          label: (global.DCValidator && global.DCValidator.VENDOR_LABELS[vKey]) || vKey,
          usedU: uCount,
          ratio: uCount / maxU,
          color: this.state.vendorColors[vKey] || DEFAULT_COLORS[vKey] || '#78909c'
        });
      });

      const stats = {
        usedU,
        freeU,
        serverUsedU,
        serverFreeU,
        maxU,
        serverMaxU,
        percentage,
        stagedU,
        color,
        vendorBreakdown,
        deviceCount: activeDevs.length
      };

      this._statsCache.set(cacheKey, stats);
      return stats;
    }

    /**
     * 사이트 레이아웃 설정 (불러오기)
     */
    setLayout(layoutData) {
      if (!layoutData) return;
      if (layoutData.sites) {
        this.state.sites = layoutData.sites;
      }
      if (layoutData.activeSiteId) {
        this.state.activeSiteId = layoutData.activeSiteId;
      }
      this._invalidateCaches('all');
      this.state.hasUnsavedChanges = false;
      this.notify('sitesUpdated', this.state.sites);
      this.notify('layoutLoaded', this.state);
      this.notify('racksUpdated', this.getRacks());
    }

    /**
     * 장비 목록 전체 설정
     */
    setDevices(deviceList) {
      this.state.devices = (Array.isArray(deviceList) ? deviceList : []).map(d => this._normalizeDevice(d));
      this._invalidateCaches('devices');
      this.state.hasUnsavedChanges = false;
      this.notify('devicesUpdated', this.state.devices);
    }

    /**
     * 초기 랙 구성(초반 상태: 32개 기본 랙)으로 리셋
     * @param {boolean} withSampleDevices - 샘플 장비 포함 여부 (기본: false - 빈 랙으로 리셋)
     */
    resetToInitialState(withSampleDevices = false) {
      if (global.DCEmbeddedData && global.DCEmbeddedData.defaultLayout) {
        const cleanLayout = JSON.parse(JSON.stringify(global.DCEmbeddedData.defaultLayout));
        this.setLayout(cleanLayout);
      }
      if (withSampleDevices && global.DCEmbeddedData && Array.isArray(global.DCEmbeddedData.defaultDevices)) {
        const cleanDevices = JSON.parse(JSON.stringify(global.DCEmbeddedData.defaultDevices));
        this.setDevices(cleanDevices);
      } else {
        this.setDevices([]);
      }
      this.state.selectedRackId = 'X001Y001';
      this.state.selectedDeviceId = null;
      this.state.simulationMode = false;
      this.state.hasUnsavedChanges = true;
      this._invalidateCaches('all');
      this.notify('sitesUpdated', this.state.sites);
      this.notify('racksUpdated', this.getRacks());
      this.notify('devicesUpdated', this.state.devices);
      this.notify('rackSelected', this.getRack('X001Y001'));
    }

    _normalizeDevice(devData) {
      const rackId = String(devData.rackId).toUpperCase();
      const startU = parseInt(devData.startU, 10) || 1;
      const formFactorU = parseInt(devData.formFactorU, 10) || 1;
      const endU = startU + formFactorU - 1;
      const cleanHost = String(devData.hostname || '').trim();
      const isTemp = cleanHost === '' || cleanHost === '[임시할당]' || !!devData.isTemp;
      const hostname = isTemp ? '' : cleanHost;
      const model = String(devData.model || '012_001').trim();
      const deviceType = devData.deviceType || (global.DCValidator ? global.DCValidator.determineDeviceType(model, hostname) : 'server');
      const vendor = devData.vendor || (global.DCValidator ? global.DCValidator.determineVendor(hostname, model) : 'OTHER');
      const isSim = !!devData.isSimulation;

      return {
        deviceId: devData.deviceId || `DEV-${Date.now()}-${Math.floor(Math.random() * 100000)}`,
        siteId: devData.siteId || this.state.activeSiteId,
        rackId,
        startU,
        endU,
        formFactorU,
        hostname,
        isTemp,
        vendor,
        model,
        deviceType,
        orientation: devData.orientation || 'front',
        isSimulation: isSim,
        colorTag: this.state.vendorColors[vendor] || DEFAULT_COLORS[vendor] || '#78909c',
        createdAt: devData.createdAt || new Date().toISOString()
      };
    }

    /**
     * 랙 선택
     */
    selectRack(rackId) {
      if (!rackId) {
        if (this.state.selectedRackId === null) return;
        const prevId = this.state.selectedRackId;
        this.state.selectedRackId = null;
        this.state.selectedDeviceId = null;
        this.notify('rackSelected', { rackId: null, prevRackId: prevId });
        return;
      }
      const resolved = this.getRack(rackId);
      const normId = resolved ? resolved.rackId : String(rackId).toUpperCase();
      if (this.state.selectedRackId === normId) return;
      const prevId = this.state.selectedRackId;
      this.state.selectedRackId = normId;
      this.state.selectedDeviceId = null;
      this.notify('rackSelected', { rackId: normId, prevRackId: prevId });
    }

    /**
     * 장비 선택
     */
    selectDevice(deviceId) {
      this.state.selectedDeviceId = deviceId;
      if (deviceId) {
        const dev = this.state.devices.find(d => d.deviceId === deviceId);
        if (dev) {
          if (dev.siteId && dev.siteId !== this.state.activeSiteId) {
            this.setActiveSite(dev.siteId);
          }
          if (dev.rackId !== this.state.selectedRackId) {
            const prevId = this.state.selectedRackId;
            this.state.selectedRackId = String(dev.rackId).toUpperCase();
            this.notify('rackSelected', { rackId: this.state.selectedRackId, prevRackId: prevId });
          }
        }
      }
      this.notify('deviceSelected', deviceId);
    }

    /**
     * 랙 추가 또는 수정 (기본 52U, 서버영역 48U)
     */
    upsertRack(rackData) {
      const targetSiteId = rackData.siteId || this.state.activeSiteId;
      const { site } = this.findOrCreateSite(targetSiteId);
      if (!site) return;

      const normId = String(rackData.rackId).trim().toUpperCase();
      const existingIdx = site.racks.findIndex(r => String(r.rackId).toUpperCase() === normId);
      const maxU = parseInt(rackData.maxU, 10) || 52;
      const serverMaxU = rackData.serverMaxU !== undefined
        ? parseInt(rackData.serverMaxU, 10)
        : Math.max(1, maxU - 4);

      const rack = {
        siteId: site.siteId,
        rackId: normId,
        name: rackData.name || normId,
        rackX: rackData.rackX,
        rackY: rackData.rackY,
        gridX: parseInt(rackData.gridX, 10) || 3,
        gridZ: parseInt(rackData.gridZ, 10) || 3,
        widthTiles: rackData.widthTiles || 1,
        depthTiles: rackData.depthTiles || 2,
        maxU: maxU,
        serverMaxU: serverMaxU,
        rotationDeg: [0, 90, 180, 270].includes(rackData.rotationDeg) ? rackData.rotationDeg : 0
      };

      if (existingIdx >= 0) {
        site.racks[existingIdx] = { ...site.racks[existingIdx], ...rack };
      } else {
        site.racks.push(rack);
      }

      this._invalidateCaches('racks');
      this.state.hasUnsavedChanges = true;
      this.notify('racksUpdated', this.getRacks());
    }

    /**
     * 랙 회전 (90도 시계방향)
     */
    rotateRack(rackId) {
      const rack = this.getRack(rackId);
      if (!rack) return;
      rack.rotationDeg = (rack.rotationDeg + 90) % 360;
      this._invalidateCaches('racks');
      this.state.hasUnsavedChanges = true;
      this.notify('rackUpdatedSingle', rack.rackId);
      this.notify('racksUpdated', this.getRacks());
    }

    /**
     * 랙 삭제
     */
    deleteRack(rackId) {
      const site = this.getActiveSite();
      if (!site) return;

      const normId = String(rackId).toUpperCase();
      site.racks = site.racks.filter(r => String(r.rackId).toUpperCase() !== normId);
      this.state.devices = this.state.devices.filter(d =>
        !(String(d.rackId).toUpperCase() === normId && (!d.siteId || d.siteId === site.siteId))
      );

      if (this.state.selectedRackId === normId) {
        this.state.selectedRackId = null;
        this.state.selectedDeviceId = null;
      }

      this._invalidateCaches('all');
      this.state.hasUnsavedChanges = true;
      this.notify('racksUpdated', site.racks);
      this.notify('devicesUpdated', this.state.devices);
    }

    /**
     * 사이트 그리드 규격 변경
     */
    updateSiteGrid(gridWidth, gridDepth, tileSizeMm) {
      const site = this.getActiveSite();
      if (!site) return;
      if (gridWidth) site.gridWidth = Math.max(5, Math.min(250, parseInt(gridWidth, 10)));
      if (gridDepth) site.gridDepth = Math.max(5, Math.min(250, parseInt(gridDepth, 10)));
      if (tileSizeMm) site.tileSizeMm = parseInt(tileSizeMm, 10);
      this.state.hasUnsavedChanges = true;
      this.notify('siteGridUpdated', site);
    }

    /**
     * 장비 단건 추가
     */
    addDevice(devData, silent = false) {
      const isSim = this.state.simulationMode || !!devData.isSimulation;
      const newDev = this._normalizeDevice({ ...devData, isSimulation: isSim });
      this.state.devices.push(newDev);
      this._invalidateCaches('devices');
      this.state.hasUnsavedChanges = true;
      if (!silent) {
        this.notify('rackDevicesChanged', newDev.rackId);
        this.notify('devicesUpdated', this.state.devices);
      }
      return newDev;
    }

    /**
     * 여러 장비 일괄 추가 (CSV 할당 등 — 신규 센터 자동 생성 및 해당 센터로 자동 포커스 이동 지원)
     */
    addDevicesBulk(newDevices, newlyCreatedRacks = [], options = {}) {
      const createdSites = [];
      let switchedSiteId = null;

      // 1) 명시적으로 지정된 센터 또는 신규 랙/장비에 포함된 센터가 있으면 자동 확인/생성
      const siteNamesToEnsure = new Set(options.targetSites || []);
      (newlyCreatedRacks || []).forEach(r => {
        if (r.siteId) siteNamesToEnsure.add(r.siteId);
      });
      (newDevices || []).forEach(d => {
        if (d.siteId) siteNamesToEnsure.add(d.siteId);
      });

      siteNamesToEnsure.forEach(sName => {
        const res = this.findOrCreateSite(sName);
        if (res.created) {
          createdSites.push(res.site.siteId);
        }
      });

      // 2) 신규 생성된 랙들을 각자의 소속 센터(siteId)에 추가
      if (newlyCreatedRacks && newlyCreatedRacks.length > 0) {
        newlyCreatedRacks.forEach(r => {
          const targetSiteId = r.siteId || this.state.activeSiteId;
          const { site } = this.findOrCreateSite(targetSiteId);
          if (site) {
            const normId = String(r.rackId).trim().toUpperCase();
            if (!site.racks.some(ex => String(ex.rackId).toUpperCase() === normId)) {
              site.racks.push({
                ...r,
                siteId: site.siteId,
                maxU: r.maxU || 52,
                serverMaxU: r.serverMaxU !== undefined ? r.serverMaxU : 48
              });
            }
          }
        });
      }

      // 3) 장비 정규화 및 반영
      const normalized = (newDevices || []).map(d => {
        const { site } = this.findOrCreateSite(d.siteId || this.state.activeSiteId);
        return this._normalizeDevice({ ...d, siteId: site.siteId });
      });

      if (options.overwrite) {
        this.state.devices = normalized;
      } else {
        this.state.devices.push(...normalized);
      }

      // 4) CSV에 센터이름이 지정되어 있었거나 다른 센터로 장비가 들어온 경우 해당 센터로 자동 전환!
      const targetSitesArr = Array.from(siteNamesToEnsure);
      if (targetSitesArr.length > 0) {
        const primaryTarget = this.findOrCreateSite(targetSitesArr[0]).site;
        if (primaryTarget && primaryTarget.siteId !== this.state.activeSiteId) {
          this.state.activeSiteId = primaryTarget.siteId;
          switchedSiteId = primaryTarget.siteId;
        } else if (options.targetSites && options.targetSites.length > 0) {
          switchedSiteId = primaryTarget.siteId;
        }
      }

      this._invalidateCaches('all');
      this.state.hasUnsavedChanges = true;

      this.notify('sitesUpdated', this.state.sites);
      if (switchedSiteId) {
        this.notify('activeSiteChanged', {
          siteId: this.state.activeSiteId,
          site: this.getActiveSite()
        });
      }
      this.notify('racksUpdated', this.getRacks());
      this.notify('devicesUpdated', this.state.devices);

      return {
        createdSites,
        switchedSiteId
      };
    }

    /**
     * 장비 수정
     */
    updateDevice(deviceId, patch) {
      const idx = this.state.devices.findIndex(d => d.deviceId === deviceId);
      if (idx === -1) return false;

      const current = this.state.devices[idx];
      const merged = { ...current, ...patch };
      const updated = this._normalizeDevice(merged);
      updated.deviceId = current.deviceId;

      this.state.devices[idx] = updated;
      this._invalidateCaches('devices');
      this.state.hasUnsavedChanges = true;
      this.notify('rackDevicesChanged', updated.rackId);
      this.notify('devicesUpdated', this.state.devices);
      return true;
    }

    /**
     * 장비 삭제
     */
    deleteDevice(deviceId) {
      const target = this.state.devices.find(d => d.deviceId === deviceId);
      if (!target) return;
      const rackId = target.rackId;
      this.state.devices = this.state.devices.filter(d => d.deviceId !== deviceId);
      if (this.state.selectedDeviceId === deviceId) {
        this.state.selectedDeviceId = null;
      }
      this._invalidateCaches('devices');
      this.state.hasUnsavedChanges = true;
      this.notify('rackDevicesChanged', rackId);
      this.notify('devicesUpdated', this.state.devices);
    }

    /**
     * 시뮬레이션 모드 토글
     */
    setSimulationMode(enabled) {
      this.state.simulationMode = !!enabled;
      this.notify('simulationModeChanged', this.state.simulationMode);
    }

    /**
     * 시뮬레이션 뷰 전환 ('all' | 'before' | 'after_real')
     */
    setSimulationView(viewMode) {
      this.state.simulationView = viewMode;
      this._invalidateCaches('stats');
      this.notify('simulationViewChanged', viewMode);
      this.notify('devicesUpdated', this.state.devices);
    }

    /**
     * 시뮬레이션 확정(Commit)
     */
    commitSimulation() {
      let count = 0;
      this.state.devices.forEach(d => {
        if (d.isSimulation) {
          d.isSimulation = false;
          count++;
        }
      });
      this._invalidateCaches('devices');
      this.state.hasUnsavedChanges = true;
      this.notify('devicesUpdated', this.state.devices);
      this.notify('simulationCommitted', { count });
      return count;
    }

    /**
     * 시뮬레이션 롤백(Rollback)
     */
    rollbackSimulation() {
      const beforeCount = this.state.devices.length;
      this.state.devices = this.state.devices.filter(d => !d.isSimulation);
      const removedCount = beforeCount - this.state.devices.length;
      this._invalidateCaches('devices');
      this.state.hasUnsavedChanges = true;
      this.notify('devicesUpdated', this.state.devices);
      this.notify('simulationRollback', { removedCount });
      return removedCount;
    }

    /**
     * 랙/장비 일괄 전면/후면 보기 전환
     */
    toggleGlobalOrientation() {
      this.state.globalOrientation = this.state.globalOrientation === 'front' ? 'back' : 'front';
      this.notify('globalOrientationChanged', this.state.globalOrientation);
      return this.state.globalOrientation;
    }

    /**
     * 검색어 필터링
     */
    setSearchQuery(query) {
      this.state.searchQuery = String(query || '').trim().toLowerCase();
      this.notify('searchQueryChanged', this.state.searchQuery);
    }

    /**
     * 레이아웃 JSON 객체 직렬화
     */
    exportLayoutJson() {
      return {
        version: this.state.version,
        activeSiteId: this.state.activeSiteId,
        invertXY: this.state.invertXY,
        vendorColors: this.state.vendorColors,
        sites: this.state.sites
      };
    }

    /**
     * 장비 CSV 데이터 생성 (센터이름,모델코드,랙라벨,hostname,몇U,수동지정U)
     */
    exportDevicesCsv(includeSimulation = true) {
      const list = includeSimulation
        ? this.state.devices
        : this.state.devices.filter(d => !d.isSimulation);

      const headers = ['센터이름', '모델코드', '랙라벨', 'hostname', '몇U', '수동지정U', '방향', '시뮬레이션'];
      const rows = list.map(d => [
        `"${(d.siteId || this.state.activeSiteId || 'SITE-A').replace(/"/g, '""')}"`,
        `"${(d.model || '').replace(/"/g, '""')}"`,
        d.rackId,
        `"${(d.hostname || '').replace(/"/g, '""')}"`,
        d.formFactorU,
        d.startU,
        d.orientation || 'front',
        d.isSimulation ? 'Y' : 'N'
      ]);

      return '\uFEFF' + [headers.join(','), ...rows.map(r => r.join(','))].join('\r\n');
    }
  }

  const store = new DataCenterStore();

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = store;
  } else {
    global.DCStore = store;
  }

})(typeof window !== 'undefined' ? window : this);
