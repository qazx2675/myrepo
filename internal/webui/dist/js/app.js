/**
 * app.js - 메인 애플리케이션 진입점 및 전역 컴포넌트 라이프사이클 관리
 */
(function (global) {
  'use strict';

  class ToastManager {
    constructor() {
      this.container = null;
      this.initDom();
    }

    initDom() {
      if (document.getElementById('dc-toast-container')) {
        this.container = document.getElementById('dc-toast-container');
        return;
      }
      const el = document.createElement('div');
      el.id = 'dc-toast-container';
      document.body.appendChild(el);
      this.container = el;
    }

    show(msg, type = 'info', duration = 3000) {
      this.initDom();
      const toast = document.createElement('div');
      toast.className = `dc-toast dc-toast-${type}`;
      toast.textContent = msg;
      this.container.appendChild(toast);

      setTimeout(() => {
        toast.style.opacity = '0';
        toast.style.transform = 'translateX(100%)';
        toast.style.transition = 'all 0.3s ease';
        setTimeout(() => {
          if (toast.parentElement) toast.parentElement.removeChild(toast);
        }, 300);
      }, duration);
    }

    info(msg, d) { this.show(msg, 'info', d); }
    success(msg, d) { this.show(msg, 'success', d); }
    error(msg, d) { this.show(msg, 'danger', d); }
  }

  global.DCToast = new ToastManager();

  class DataCenterApp {
    constructor() {
      this.store = global.DCStore;
      this.sceneManager = null;
      this.leftPanel = null;
      this.rightPanel = null;
      this.topToolbar = null;
    }

    async init() {
      console.log('[DCApp] 애플리케이션 초기화 시작');

      // 1. Go 백엔드(datacenter-viewer.exe) 구동 시 ./data/ 또는 공유 폴더 저장본 우선 로드
      let loadedFromGo = false;
      if (global.DCFolderManager && typeof global.DCFolderManager.tryGoBackend === 'function') {
        loadedFromGo = await global.DCFolderManager.tryGoBackend();
      }

      // 1-2. 백엔드 저장본이 없을 때만 초기 샘플 데이터 스토어에 적재 (불필요한 이중 로드/인덱싱 방지)
      if (!loadedFromGo) {
        await this.loadInitialSampleData();
      }
      this.store.state.hasUnsavedChanges = false;

      // 2. 3D 씬 매니저 초기화 (독립 예외 격리로 3D 오류가 상단 툴바/패널 생성을 절대 막지 않도록 보장)
      const viewportContainer = document.getElementById('viewport-container');
      if (viewportContainer && global.DCSceneManager) {
        try {
          this.sceneManager = new global.DCSceneManager(
            viewportContainer,
            this.store,
            global.DCRackMeshBuilder
          );
          this.sceneManager.init();
        } catch (err) {
          console.error('[DCApp] 3D 씬 매니저 초기화 중 예외 발생:', err);
        }
      }

      // 3. 상단 툴바 초기화
      const topToolbarContainer = document.getElementById('top-toolbar');
      if (topToolbarContainer && global.DCTopToolbar) {
        try {
          this.topToolbar = new global.DCTopToolbar(topToolbarContainer, this.store, this.sceneManager);
        } catch (err) {
          console.error('[DCApp] 상단 툴바 초기화 중 예외 발생:', err);
        }
      }

      // 4. 좌측 패널 초기화
      const leftContainer = document.getElementById('left-panel');
      if (leftContainer && global.DCLeftCabinetList) {
        try {
          this.leftPanel = new global.DCLeftCabinetList(leftContainer, this.store);
        } catch (err) {
          console.error('[DCApp] 좌측 패널 초기화 중 예외 발생:', err);
        }
      }

      // 5. 우측 실장도 패널 초기화
      const rightContainer = document.getElementById('right-panel');
      if (rightContainer && global.DCRightElevationPanel) {
        try {
          this.rightPanel = new global.DCRightElevationPanel(rightContainer, this.store);
        } catch (err) {
          console.error('[DCApp] 우측 실장도 패널 초기화 중 예외 발생:', err);
        }
      }

      // 6. 기본 X001Y001 랙 선택 및 전체 전경 뷰(Home) 배치
      setTimeout(() => {
        try {
          this.store.selectRack('X001Y001');
          if (this.sceneManager) {
            this.sceneManager.setCameraHome();
          }
        } catch (err) {
          console.error('[DCApp] 초기 랙 포커스 설정 중 예외 발생:', err);
        }
      }, 150);

      console.log('[DCApp] 초기화 완료');
    }

    async loadInitialSampleData() {
      if (global.DCEmbeddedData) {
        if (global.DCEmbeddedData.defaultLayout) {
          this.store.setLayout(global.DCEmbeddedData.defaultLayout);
        }
        if (Array.isArray(global.DCEmbeddedData.defaultDevices)) {
          this.store.setDevices(global.DCEmbeddedData.defaultDevices);
          console.log(`[DCApp] 내장 기본 서버 ${global.DCEmbeddedData.defaultDevices.length}대 일괄 로드 완료`);
        }
      }
    }
  }

  window.addEventListener('DOMContentLoaded', () => {
    global.DCApp = new DataCenterApp();
    global.DCApp.init();
  });

})(typeof window !== 'undefined' ? window : this);
