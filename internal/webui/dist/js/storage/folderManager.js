/**
 * folderManager.js - Windows 로컬 데이터 폴더 연결(File System Access API) 및 파일 영속성 관리
 */
(function (global) {
  'use strict';

  class FolderManager {
    constructor(store) {
      this.store = store;
      this.dirHandle = null;
      this.folderName = null;
      this.autoSaveTimer = null;
      this._isLoadingFromStorage = false;
      this._pingInFlight = false;

      // 실제 데이터 변경 이벤트에만 자동 저장 스케줄링 (saveCompleted, rackSelected 등 조회/상태 알림에 의한 무한 저장 루프 원천 차단!)
      const mutatingEvents = new Set([
        'racksUpdated',
        'rackDevicesChanged',
        'devicesUpdated',
        'sitesUpdated',
        'siteGridUpdated',
        'invertXYChanged',
        'vendorColorsChanged'
      ]);
      if (this.store) {
        this.store.subscribe('stateChange', (payload) => {
          if (this._isLoadingFromStorage) return;
          const ev = payload && payload.event;
          if (!ev || !mutatingEvents.has(ev)) return;
          if (!this.store.state.hasUnsavedChanges) return;
          this.scheduleAutoSave();
        });
      }
    }

    /**
     * File System Access API 지원 여부
     */
    isSupported() {
      return typeof window !== 'undefined' && 'showDirectoryPicker' in window;
    }

    /**
     * 사용자가 작업 폴더를 선택하도록 프롬프트
     */
    async pickFolder() {
      if (!this.isSupported()) {
        throw new Error('이 브라우저는 File System Access API를 지원하지 않습니다. 파일 다운로드/업로드 폴백을 이용하세요.');
      }

      try {
        const handle = await window.showDirectoryPicker({
          id: 'datacenter-project-folder',
          mode: 'readwrite'
        });

        // 권한 확인 및 요청
        const permission = await this.verifyPermission(handle, true);
        if (!permission) {
          throw new Error('폴더 읽기/쓰기 권한이 거부되었습니다.');
        }

        this.dirHandle = handle;
        this.folderName = handle.name;
        this.store.state.activeFolderHandle = handle;
        this.store.state.folderName = handle.name;

        // 기존 파일들 로드 시도
        await this.loadFromFolder();

        this.store.notify('folderConnected', {
          name: this.folderName,
          supported: true
        });

        return { success: true, folderName: this.folderName };
      } catch (err) {
        if (err.name === 'AbortError') {
          return { success: false, aborted: true };
        }
        console.error('Folder pick error:', err);
        throw err;
      }
    }

    /**
     * 권한 확인 및 요청
     */
    async verifyPermission(fileHandle, readWrite) {
      const options = {};
      if (readWrite) {
        options.mode = 'readwrite';
      }
      if ((await fileHandle.queryPermission(options)) === 'granted') {
        return true;
      }
      if ((await fileHandle.requestPermission(options)) === 'granted') {
        return true;
      }
      return false;
    }

    /**
     * 연결된 로컬 폴더에서 데이터 불러오기
     */
    async loadFromFolder() {
      if (!this.dirHandle) return;
      this._isLoadingFromStorage = true;
      if (this.autoSaveTimer) {
        clearTimeout(this.autoSaveTimer);
        this.autoSaveTimer = null;
      }

      try {
        // 1. datacenter_layout.json 불러오기
        try {
          const layoutFileHandle = await this.dirHandle.getFileHandle('datacenter_layout.json');
          const file = await layoutFileHandle.getFile();
          const text = await file.text();
          const layoutData = JSON.parse(text);
          this.store.setLayout(layoutData);
          console.log('[FolderManager] datacenter_layout.json 로드 완료');
        } catch (e) {
          console.warn('[FolderManager] datacenter_layout.json 파일이 없거나 파싱 실패');
        }

        // 2. allocated_devices.csv 불러오기
        try {
          const csvFileHandle = await this.dirHandle.getFileHandle('allocated_devices.csv');
          const file = await csvFileHandle.getFile();
          const text = await file.text();
          if (global.DCCsvHandler) {
            await global.DCCsvHandler.importCsvText(text, { overwrite: true, isSimulation: false });
          }
          console.log('[FolderManager] allocated_devices.csv 로드 완료');
        } catch (e) {
          console.warn('[FolderManager] allocated_devices.csv 파일이 없습니다.');
        }

        // 3. simulation_staging.json 불러오기 (입고 시뮬레이션 데이터)
        try {
          const stagingFileHandle = await this.dirHandle.getFileHandle('simulation_staging.json');
          const file = await stagingFileHandle.getFile();
          const text = await file.text();
          const stagingData = JSON.parse(text);
          if (Array.isArray(stagingData)) {
            // 시뮬레이션 장비 목록 추가
            const simDevs = stagingData.map(d => ({ ...d, isSimulation: true }));
            this.store.addDevicesBulk(simDevs);
          }
          console.log('[FolderManager] simulation_staging.json 로드 완료');
        } catch (e) {
          // 파일 없을 수 있음 (정상)
        }
        this.store.state.hasUnsavedChanges = false;
      } catch (err) {
        console.error('[FolderManager] Error loading from folder:', err);
      } finally {
        this._isLoadingFromStorage = false;
      }
    }

    /**
     * 연결된 폴더에 현재 데이터 전체 저장
     */
    async saveToFolder() {
      if (this.goMode) {
        return this.goSave();
      }
      if (!this.dirHandle) return;

      try {
        // 1. datacenter_layout.json 저장
        const layoutData = this.store.exportLayoutJson();
        const layoutFileHandle = await this.dirHandle.getFileHandle('datacenter_layout.json', { create: true });
        const layoutWritable = await layoutFileHandle.createWritable();
        await layoutWritable.write(JSON.stringify(layoutData, null, 2));
        await layoutWritable.close();

        // 2. allocated_devices.csv 저장 (실제 운영 장비들만)
        const csvContent = this.store.exportDevicesCsv(false);
        const csvFileHandle = await this.dirHandle.getFileHandle('allocated_devices.csv', { create: true });
        const csvWritable = await csvFileHandle.createWritable();
        await csvWritable.write(csvContent);
        await csvWritable.close();

        // 3. simulation_staging.json 저장 (시뮬레이션 대기 중인 장비들)
        const simDevices = this.store.state.devices.filter(d => d.isSimulation);
        const stagingFileHandle = await this.dirHandle.getFileHandle('simulation_staging.json', { create: true });
        const stagingWritable = await stagingFileHandle.createWritable();
        await stagingWritable.write(JSON.stringify(simDevices, null, 2));
        await stagingWritable.close();

        this.store.state.hasUnsavedChanges = false;
        this.store.notify('saveCompleted', { folderName: this.folderName, timestamp: new Date() });
        console.log('[FolderManager] 폴더 데이터 자동 저장 완료');
      } catch (err) {
        console.error('[FolderManager] Error saving to folder:', err);
      }
    }

    /**
     * Go 백엔드(datacenter-viewer.exe)가 있으면 ./data/ 와 자동 동기화 모드로 전환하고 저장된 데이터를 불러온다.
     * 순수 HTML 서버(server.js 등)에서는 /api/bootstrap 이 없으므로 false 를 반환하고 기존 브라우저 모드를 유지한다.
     */
    async tryGoBackend() {
      if (typeof fetch === 'undefined' || (typeof location !== 'undefined' && location.protocol === 'file:')) {
        return false;
      }
      this._isLoadingFromStorage = true;
      try {
        const res = await fetch('/api/bootstrap', { cache: 'no-store' });
        if (!res.ok) return false;
        const data = await res.json();
        if (!data || !data.goBackend) return false;

        this.goMode = true;
        this.folderName = data.dataDir || 'data';
        this.isCustom = Boolean(data.info && data.info.isCustom);
        if (data.layout) {
          this.store.setLayout(data.layout);
        }
        if (data.csv && data.csv.trim() && global.DCCsvHandler) {
          await global.DCCsvHandler.importCsvText(data.csv, { overwrite: true, isSimulation: false, silentToast: true });
        }
        if (this.autoSaveTimer) {
          clearTimeout(this.autoSaveTimer);
          this.autoSaveTimer = null;
        }
        this.store.state.hasUnsavedChanges = false;
        this.updateFolderLabel();

        // 브라우저 창이 열려 있는 동안 Go 로컬 서버 종료를 방지하는 경량 하트비트(8초 간격, 응답 스트림 즉시 해제)
        if (!this._heartbeatTimer) {
          const sendPing = () => {
            if (this._pingInFlight) return;
            this._pingInFlight = true;
            fetch('/api/ping', { cache: 'no-store' })
              .then(r => r.text())
              .catch(() => {})
              .finally(() => { this._pingInFlight = false; });
          };
          sendPing();
          this._heartbeatTimer = setInterval(sendPing, 8000);
          if (typeof window !== 'undefined') {
            const sendBye = () => {
              if (navigator.sendBeacon) {
                navigator.sendBeacon('/api/bye');
              } else {
                fetch('/api/bye', { method: 'POST', keepalive: true }).catch(() => {});
              }
            };
            window.addEventListener('pagehide', sendBye);
            window.addEventListener('beforeunload', sendBye);
          }
        }

        console.log('[FolderManager] Go 백엔드 연결, 데이터 폴더:', this.folderName);
        return Boolean(data.layout || (data.csv && data.csv.trim()));
      } catch (e) {
        return false;
      } finally {
        this._isLoadingFromStorage = false;
      }
    }

    /**
     * 현재 데이터 폴더 설정 및 상태 정보 조회
     */
    async getDataDirInfo() {
      if (!this.goMode) return null;
      try {
        const res = await fetch('/api/config/data-dir', { cache: 'no-store' });
        if (!res.ok) return null;
        return await res.json();
      } catch (e) {
        return null;
      }
    }

    /**
     * 네트워크 공유 폴더(UNC / 로컬 드라이브)로 저장 디렉터리 변경 및 데이터 로드
     */
    async setSharedDataDir(newPath) {
      if (!this.goMode) {
        throw new Error('Go 백엔드가 실행 중이어야 네트워크 공유 폴더를 지정할 수 있습니다.');
      }
      this._isLoadingFromStorage = true;
      try {
        const res = await fetch('/api/config/data-dir', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ dataDir: newPath })
        });
        if (!res.ok) {
          const errText = await res.text();
          throw new Error(errText || `경로 변경 실패 (HTTP ${res.status})`);
        }
        const data = await res.json();
        this.folderName = data.dataDir || newPath;
        this.isCustom = Boolean(data.info && data.info.isCustom);

        if (data.layout) {
          this.store.setLayout(data.layout);
        }
        if (data.csv && data.csv.trim() && global.DCCsvHandler) {
          await global.DCCsvHandler.importCsvText(data.csv, { overwrite: true, isSimulation: false });
        } else if (!data.csv || !data.csv.trim()) {
          this.store.state.devices = [];
          this.store.notify('devicesUpdated', { devices: [] });
        }

        if (this.autoSaveTimer) {
          clearTimeout(this.autoSaveTimer);
          this.autoSaveTimer = null;
        }
        this.store.state.hasUnsavedChanges = false;
        this.updateFolderLabel();
        return data;
      } finally {
        this._isLoadingFromStorage = false;
      }
    }

    /**
     * 공유 폴더에서 최신 랙 구성 및 호스트 목록 다시 불러오기 (수동 동기화)
     */
    async syncFromShared() {
      if (!this.goMode) {
        throw new Error('Go 백엔드가 실행 중이어야 동기화를 수행할 수 있습니다.');
      }
      this._isLoadingFromStorage = true;
      try {
        const res = await fetch('/api/sync', { cache: 'no-store' });
        if (!res.ok) {
          const errText = await res.text();
          throw new Error(errText || `동기화 실패 (HTTP ${res.status})`);
        }
        const data = await res.json();
        if (data.layout) {
          this.store.setLayout(data.layout);
        }
        if (data.csv && data.csv.trim() && global.DCCsvHandler) {
          await global.DCCsvHandler.importCsvText(data.csv, { overwrite: true, isSimulation: false });
        } else if (!data.csv || !data.csv.trim()) {
          this.store.state.devices = [];
          this.store.notify('devicesUpdated', { devices: [] });
        }
        if (this.autoSaveTimer) {
          clearTimeout(this.autoSaveTimer);
          this.autoSaveTimer = null;
        }
        this.store.state.hasUnsavedChanges = false;
        this.updateFolderLabel();
        return data;
      } finally {
        this._isLoadingFromStorage = false;
      }
    }

    /**
     * 상단 툴바의 폴더 라벨 UI 갱신
     */
    updateFolderLabel() {
      const lbl = document.getElementById('lbl-folder-name');
      if (!lbl) return;

      if (this.folderName && (this.folderName.startsWith('\\\\') || this.folderName.includes(':') || this.isCustom)) {
        const shortName = this.folderName.length > 22 ? '...' + this.folderName.slice(-18) : this.folderName;
        lbl.textContent = `공유: ${shortName}`;
        lbl.title = `현재 저장 위치 (Windows 네트워크 공유): ${this.folderName}`;
      } else if (this.goMode) {
        lbl.textContent = '로컬 data/ 폴더';
        lbl.title = `현재 저장 위치: ${this.folderName || 'data'}`;
      } else if (this.dirHandle) {
        lbl.textContent = this.folderName || '로컬 폴더';
      } else {
        lbl.textContent = '저장/공유 폴더';
      }
    }

    async goSave() {
      if (this._isSaving) return;
      this._isSaving = true;
      try {
        const body = JSON.stringify({
          layout: this.store.exportLayoutJson(),
          csv: this.store.exportDevicesCsv(false)
        });
        const res = await fetch('/api/save', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body
        });
        await res.text();
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        this.store.state.hasUnsavedChanges = false;
        this.store.notify('saveCompleted', { folderName: this.folderName, timestamp: new Date() });
      } catch (err) {
        console.error('[FolderManager] Go 백엔드 저장 실패:', err);
      } finally {
        this._isSaving = false;
      }
    }

    /**
     * 자동 저장 스케줄러 (디바운스 1초)
     */
    scheduleAutoSave() {
      if (this._isLoadingFromStorage || this._isSaving) return;
      if (!this.dirHandle && !this.goMode) return;
      if (!this.store || !this.store.state.hasUnsavedChanges) return;
      if (this.autoSaveTimer) {
        clearTimeout(this.autoSaveTimer);
      }
      this.autoSaveTimer = setTimeout(() => {
        this.autoSaveTimer = null;
        this.saveToFolder();
      }, 1000);
    }

    /**
     * 현재 할당된 모든 호스트(장비) 목록을 CSV 파일로 즉시 다운로드
     */
    downloadAllocatedHostsCsv() {
      const csvData = this.store.exportDevicesCsv(true);
      const siteId = (this.store.state && this.store.state.activeSiteId) ? this.store.state.activeSiteId : 'ALL';
      const totalCount = this.store.state && Array.isArray(this.store.state.devices) ? this.store.state.devices.length : 0;
      const filename = `allocated_hosts_${siteId}_${totalCount}ea.csv`;
      this.downloadBlob(csvData, filename, 'text/csv;charset=utf-8;');
      if (global.DCToast) {
        global.DCToast.success(`현재 할당된 전체 호스트(${totalCount}대) CSV 다운로드 완료 (${filename})`);
      }
    }

    /**
     * 폴백: 브라우저 다운로드 방식으로 현재 할당된 모든 호스트 CSV 내보내기
     */
    downloadProjectBackup() {
      this.downloadAllocatedHostsCsv();
    }

    /**
     * 파일 다운로드 유틸리티
     */
    downloadBlob(content, filename, mimeType) {
      const blob = new Blob([content], { type: mimeType });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = FolderManager;
  } else {
    global.DCFolderManager = new FolderManager(global.DCStore);
  }

})(typeof window !== 'undefined' ? window : this);
