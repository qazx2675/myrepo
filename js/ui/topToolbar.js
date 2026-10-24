/**
 * topToolbar.js - 상단 툴바: 카메라 프리셋(Home/1인칭 통로/상단뷰), X/Y 축 반전, 공간/랙 편집, 검색, 시뮬레이션, CSV/TSV 자동 할당
 */
(function (global) {
  'use strict';

  class TopToolbar {
    constructor(containerEl, store, sceneManager) {
      this.container = containerEl;
      this.store = store;
      this.sceneManager = sceneManager;
      this.initDom();
      this.renderSiteSelect();
      this.bindEvents();
      if (global.DCFolderManager && typeof global.DCFolderManager.updateFolderLabel === 'function') {
        global.DCFolderManager.updateFolderLabel();
      }
    }

    initDom() {
      this.container.innerHTML = `
        <div class="dc-topbar-inner">
          <!-- 좌측: 브랜드 및 사이트(센터) 선택 -->
          <div class="dc-brand-group">
            <span class="dc-brand-logo">🏢</span>
            <span class="dc-brand-title">서버 입고 마운트 3D 뷰어</span>
            <div class="dc-site-selector-box">
              <span class="dc-site-label">센터</span>
              <select id="select-active-site" class="dc-select-sm" title="데이터센터 선택 (CSV에 새로운 센터이름 지정 시 자동 추가됨)">
                <option value="SITE-A">SITE-A (52U / 서버 48U)</option>
              </select>
            </div>
          </div>

          <div class="dc-topbar-divider"></div>

          <!-- 중앙 좌측: 카메라 프리셋 및 X/Y 반전 -->
          <div class="dc-nav-tools">
            <button class="dc-tb-btn active" id="btn-camera-home" title="전체 전경 뷰">
              <span>🏠</span> 전체 뷰
            </button>
            <button class="dc-tb-btn" id="btn-camera-aisle" title="1인칭 서버실 복도 보행 점검 뷰 (WASD/방향키 이동 + 마우스 시선 회전)">
              <span>🚶</span> 1인칭 통로 시점
            </button>
            <button class="dc-tb-btn" id="btn-camera-top" title="상단 수직 평면 뷰 (랙당 벤더별 점유율 확인)">
              <span>📐</span> 상단 뷰 (벤더비중)
            </button>
            <button class="dc-tb-btn dc-btn-invert-xy" id="btn-invert-xy" title="X(행)와 Y(열) 배치 방향을 서로 반전합니다">
              <span>⇄</span> <span id="lbl-invert-xy">X:행 / Y:열 (반전)</span>
            </button>
            <button class="dc-tb-btn" id="btn-open-layout-editor" title="데이터센터 그리드 및 랙(52U/48U) 설정">
              <span>🛠️</span> 공간/랙 설정
            </button>
            <button class="dc-tb-btn" id="btn-toggle-global-orientation" title="장비 앞/뒤 일괄 보기 전환">
              <span>🔄</span> 앞/뒤 전환
            </button>
            <div class="dc-tb-sens-box" title="마우스 및 방향키 시선 회전 감도 조절 (0.1x ~ 2.5x, 기본 0.4x)">
              <span>🖱️ 감도</span>
              <input type="range" id="input-tb-mouse-sens" min="0.1" max="2.5" step="0.1" value="0.4">
              <strong id="lbl-tb-mouse-sens">0.4x</strong>
            </div>
            <button class="dc-tb-btn" id="btn-tb-sprint-speed" title="Shift 달리기 속도 배율 토글 (현재: 2.0x, 클릭 시 변경)">
              <span>⚡</span> Shift: <strong id="lbl-tb-sprint-speed">2.0x</strong>
            </button>
            <button class="dc-tb-btn" id="btn-tb-vert-speed" title="Q/E 눈높이 이동 속도 배율 토글 (현재: 1.0x, 클릭 시 변경)">
              <span>↕</span> Q/E: <strong id="lbl-tb-vert-speed">1.0x</strong>
            </button>
          </div>

          <div class="dc-topbar-divider"></div>

          <!-- 중앙: 검색창 -->
          <div class="dc-search-box">
            <span class="dc-search-icon">🔍</span>
            <input type="text" id="input-global-search" placeholder="호스트명, 모델코드, 랙 검색 (예: X029Y001, msvr)...">
          </div>

          <div class="dc-topbar-divider"></div>

          <!-- 중앙 우측: 신규 서버 입고 시뮬레이션 제어부 -->
          <div class="dc-simulation-control-group">
            <div class="dc-sim-switch-box" title="신규 서버 입고 시뮬레이션 모드 켜기/끄기">
              <label class="dc-switch">
                <input type="checkbox" id="chk-simulation-mode">
                <span class="dc-slider round"></span>
              </label>
              <span class="dc-sim-label" id="lbl-sim-mode">입고 시뮬레이션 OFF</span>
            </div>

            <select id="select-simulation-view" class="dc-select-sm" title="입고 전/후 상태 비교 뷰">
              <option value="all">입고 후 (신규 하이라이트)</option>
              <option value="before">입고 전 (기존 장비만)</option>
              <option value="after_real">입고 후 (실제 외형)</option>
            </select>

            <button class="dc-btn-xs dc-btn-success" id="btn-commit-simulation" title="신규 입고 장비들을 확정 반영">
              확정
            </button>
            <button class="dc-btn-xs dc-btn-outline" id="btn-rollback-simulation" title="입고 시뮬레이션 장비들만 초기화">
              초기화
            </button>
          </div>

          <div style="flex: 1;"></div>

          <!-- 우측: CSV/TSV 업로드, 붙여넣기 및 폴더 저장 -->
          <div class="dc-storage-actions">
            <input type="file" id="input-hidden-csv" accept=".csv,.tsv,.txt" style="display: none;">

            <button class="dc-tb-btn dc-btn-accent" id="btn-upload-csv" title="CSV/TSV 파일로 서버 자동 할당 ([센터이름,]모델코드,랙라벨,hostname,몇U,[수동지정U])">
              <span>📄</span> CSV 자동 할당
            </button>

            <button class="dc-tb-btn" id="btn-test-user-csv" title="보내주신 X029Y001~X029Y003 (212대, X029Y002 초과 검출 포함) 테스트 실행">
              <span>🧪</span> 샘플 CSV 검증 테스트
            </button>

            <button class="dc-tb-btn dc-btn-warning" id="btn-reset-initial" title="모든 장비를 비우고 초기 랙 구성(초반 상태: 32대 빈 랙)으로 되돌립니다">
              <span>🔄</span> 초기 랙 상태로 리셋
            </button>

            <button class="dc-tb-btn" id="btn-download-backup" title="현재 할당된 모든 호스트(장비)를 CSV로 다운로드">
              <span>💾</span> 할당 호스트 CSV 다운로드
            </button>

            <button class="dc-tb-btn" id="btn-sync-shared" title="공유 폴더에서 최신 랙 배치 및 호스트 목록 다시 불러오기 (수동 동기화)">
              <span>🔄</span> 동기화
            </button>

            <button class="dc-btn-sm dc-btn-folder" id="btn-pick-folder" title="Windows 로컬 폴더 또는 네트워크 공유 폴더(\\\\서버\\공유) 설정">
              <span>📁</span> <span id="lbl-folder-name">저장/공유 폴더</span>
            </button>
          </div>
        </div>
      `;
    }

    renderSiteSelect() {
      const sel = document.getElementById('select-active-site');
      if (!sel || !this.store) return;

      const sites = this.store.getSites ? this.store.getSites() : (this.store.state.sites || []);
      const activeId = this.store.state.activeSiteId;

      sel.innerHTML = '';
      sites.forEach(s => {
        const opt = document.createElement('option');
        opt.value = s.siteId;
        const rackCount = Array.isArray(s.racks) ? s.racks.length : 0;
        const displayName = s.name && s.name !== s.siteId ? `${s.siteId} - ${s.name}` : s.siteId;
        opt.textContent = `${displayName} (랙 ${rackCount}개)`;
        if (s.siteId === activeId) {
          opt.selected = true;
        }
        sel.appendChild(opt);
      });
    }

    setActiveCamBtn(activeId) {
      ['btn-camera-home', 'btn-camera-aisle', 'btn-camera-top'].forEach(id => {
        const el = document.getElementById(id);
        if (el) el.classList.toggle('active', id === activeId);
      });
    }

    bindEvents() {
      // 센터(Site) 드롭다운 전환 이벤트
      const siteSelect = document.getElementById('select-active-site');
      if (siteSelect) {
        siteSelect.addEventListener('change', (e) => {
          const newSite = this.store.setActiveSite(e.target.value);
          if (newSite && global.DCToast) {
            global.DCToast.info(`'${newSite.siteId}' 센터로 전환되었습니다.`);
          }
          if (this.sceneManager) {
            this.sceneManager.setCameraHome();
            this.setActiveCamBtn('btn-camera-home');
          }
        });
      }

      this.store.subscribe('sitesUpdated', () => this.renderSiteSelect());
      this.store.subscribe('racksUpdated', () => this.renderSiteSelect());
      this.store.subscribe('layoutLoaded', () => this.renderSiteSelect());
      this.store.subscribe('activeSiteChanged', () => {
        this.renderSiteSelect();
        if (this.sceneManager) {
          this.sceneManager.setCameraHome();
          this.setActiveCamBtn('btn-camera-home');
        }
      });

      document.getElementById('btn-camera-home').addEventListener('click', () => {
        this.setActiveCamBtn('btn-camera-home');
        if (this.sceneManager) this.sceneManager.setCameraHome();
      });

      document.getElementById('btn-camera-aisle').addEventListener('click', () => {
        this.setActiveCamBtn('btn-camera-aisle');
        if (this.sceneManager) this.sceneManager.setCameraAisle();
      });

      document.getElementById('btn-camera-top').addEventListener('click', () => {
        this.setActiveCamBtn('btn-camera-top');
        if (this.sceneManager) this.sceneManager.setCameraTop();
      });

      // X/Y 반전 버튼 (항목 4)
      document.getElementById('btn-invert-xy').addEventListener('click', () => {
        const inverted = this.store.toggleInvertXY();
        const lbl = document.getElementById('lbl-invert-xy');
        const btn = document.getElementById('btn-invert-xy');
        if (lbl) {
          lbl.textContent = inverted ? 'X:열 / Y:행 (반전됨)' : 'X:행 / Y:열 (반전)';
        }
        if (btn) {
          btn.classList.toggle('active', inverted);
        }
        if (global.DCToast) {
          global.DCToast.info(inverted
            ? 'X/Y 축이 반전되었습니다. (현재: X=열 가로, Y=행 세로)'
            : '기본 X/Y 축으로 전환되었습니다. (현재: X=행 세로, Y=열 가로)');
        }
      });

      // 공간/랙 설정 에디터 모달
      document.getElementById('btn-open-layout-editor').addEventListener('click', () => {
        if (global.DCLayoutEditorModal) {
          global.DCLayoutEditorModal.show();
        }
      });

      // 앞/뒤 전환
      document.getElementById('btn-toggle-global-orientation').addEventListener('click', () => {
        const next = this.store.toggleGlobalOrientation();
        if (global.DCToast) {
          global.DCToast.info(`장비 뷰 방향이 '${next === 'front' ? '전면(Front)' : '후면(Back)'}'으로 전환되었습니다.`);
        }
      });

      // 마우스 회전 감도 슬라이더
      const tbSensSlider = document.getElementById('input-tb-mouse-sens');
      if (tbSensSlider) {
        tbSensSlider.addEventListener('input', (e) => {
          if (this.sceneManager) {
            this.sceneManager.setMouseSensitivity(e.target.value);
          }
        });
      }

      // Shift 달리기 속도 토글
      const btnTbSprint = document.getElementById('btn-tb-sprint-speed');
      if (btnTbSprint) {
        btnTbSprint.addEventListener('click', () => {
          if (this.sceneManager) {
            const next = this.sceneManager.toggleSprintSpeed();
            if (global.DCToast) global.DCToast.info(`Shift 달리기 속도: ${next.toFixed(1)}x`);
          }
        });
      }

      // Q/E 눈높이 수직 이동 속도 토글
      const btnTbVert = document.getElementById('btn-tb-vert-speed');
      if (btnTbVert) {
        btnTbVert.addEventListener('click', () => {
          if (this.sceneManager) {
            const next = this.sceneManager.toggleVertSpeed();
            if (global.DCToast) global.DCToast.info(`Q/E 눈높이 이동 속도: ${next.toFixed(1)}x`);
          }
        });
      }

      // 검색
      const searchInput = document.getElementById('input-global-search');
      searchInput.addEventListener('input', (e) => {
        this.store.setSearchQuery(e.target.value);
      });

      // 시뮬레이션 토글
      const simCheckbox = document.getElementById('chk-simulation-mode');
      const simLabel = document.getElementById('lbl-sim-mode');
      simCheckbox.addEventListener('change', (e) => {
        const enabled = e.target.checked;
        this.store.setSimulationMode(enabled);
        simLabel.textContent = enabled ? '입고 시뮬레이션 ON' : '입고 시뮬레이션 OFF';
        simLabel.style.color = enabled ? '#00e5ff' : '#94a3b8';
      });

      // 시뮬레이션 뷰 선택
      document.getElementById('select-simulation-view').addEventListener('change', (e) => {
        this.store.setSimulationView(e.target.value);
      });

      // 시뮬레이션 확정
      document.getElementById('btn-commit-simulation').addEventListener('click', () => {
        const count = this.store.commitSimulation();
        if (count > 0) {
          if (global.DCToast) global.DCToast.success(`신규 입고 장비 ${count}대가 확정 반영되었습니다.`);
        } else {
          if (global.DCToast) global.DCToast.info('확정할 시뮬레이션 장비가 없습니다.');
        }
      });

      // 시뮬레이션 초기화
      document.getElementById('btn-rollback-simulation').addEventListener('click', () => {
        const removed = this.store.rollbackSimulation();
        if (global.DCToast) {
          global.DCToast.info(`시뮬레이션 장비 ${removed}대가 초기화되었습니다.`);
        }
      });

      // 공유 폴더 최신 데이터 즉시 동기화 버튼
      const btnSyncShared = document.getElementById('btn-sync-shared');
      if (btnSyncShared) {
        btnSyncShared.addEventListener('click', async () => {
          if (!global.DCFolderManager) return;
          try {
            btnSyncShared.disabled = true;
            btnSyncShared.innerHTML = '<span>⏳</span> 동기화 중...';
            await global.DCFolderManager.syncFromShared();
            const devCount = this.store && this.store.state.devices ? this.store.state.devices.length : 0;
            const rackCount = this.store && this.store.getRacks() ? this.store.getRacks().length : 0;
            if (global.DCToast) {
              global.DCToast.success(`공유 폴더 데이터 동기화 완료 (랙 ${rackCount}개, 장비 ${devCount}대)`);
            }
          } catch (err) {
            alert(`동기화 중 오류가 발생했습니다: ${err.message}`);
          } finally {
            btnSyncShared.disabled = false;
            btnSyncShared.innerHTML = '<span>🔄</span> 동기화';
          }
        });
      }

      // 폴더 연결 및 네트워크 공유 폴더 설정
      document.getElementById('btn-pick-folder').addEventListener('click', async () => {
        if (global.DCFolderManager && global.DCFolderManager.goMode) {
          if (global.DCSharedFolderModal) {
            global.DCSharedFolderModal.show();
          }
        } else if (global.DCFolderManager) {
          try {
            const res = await global.DCFolderManager.pickFolder();
            if (res.success) {
              document.getElementById('lbl-folder-name').textContent = res.folderName;
              if (global.DCToast) global.DCToast.success(`폴더 '${res.folderName}' 연결 완료`);
            }
          } catch (e) {
            alert(`폴더 지정 안내: ${e.message}`);
          }
        }
      });

      // CSV 업로드
      const csvInput = document.getElementById('input-hidden-csv');
      document.getElementById('btn-upload-csv').addEventListener('click', () => {
        csvInput.value = '';
        csvInput.click();
      });

      csvInput.addEventListener('change', async (e) => {
        const file = e.target.files[0];
        if (file && global.DCCsvHandler) {
          await global.DCCsvHandler.handleFileSelect(file);
        }
      });

      // 사용자 제공 212행 CSV 검증 테스트 버튼 (X029Y002 48U 초과 리포트 즉시 확인용)
      document.getElementById('btn-test-user-csv').addEventListener('click', async () => {
        if (global.DCEmbeddedData && global.DCEmbeddedData.userRawTsv && global.DCCsvHandler) {
          // 기존 X029Y001~003 장비를 비우고 원본 212행을 검증하여 48U 초과 에러 리포트 모달 시연
          this.store.state.devices = this.store.state.devices.filter(
            d => !['X029Y001', 'X029Y002', 'X029Y003'].includes(String(d.rackId).toUpperCase())
          );
          await global.DCCsvHandler.importCsvText(global.DCEmbeddedData.userRawTsv, {
            overwrite: false,
            isSimulation: this.store.state.simulationMode,
            autoCreateMissingRacks: true
          });
        }
      });

      // 초기 랙 상태로 리셋
      const btnReset = document.getElementById('btn-reset-initial');
      if (btnReset) {
        btnReset.addEventListener('click', () => {
          if (!confirm('현재 할당된 장비를 모두 비우고 초기 랙 구성(초반 32대 랙, 빈 랙 상태)으로 리셋하시겠습니까?\n\n리셋 후 준비하신 CSV를 [CSV 자동 할당]으로 바로 업로드하실 수 있습니다.')) {
            return;
          }
          this.store.resetToInitialState(false);
          if (global.DCFolderManager && global.DCFolderManager.goMode) {
            global.DCFolderManager.goSave();
          }
          if (this.sceneManager) {
            this.sceneManager.setCameraHome();
            this.setActiveCamBtn('btn-camera-home');
          }
          this.renderSiteSelect();
          if (global.DCToast) {
            global.DCToast.success('초기 랙 상태로 리셋되었습니다. 이제 [CSV 자동 할당]으로 직접 CSV를 넣어보세요!');
          }
        });
      }

      // 백업 다운로드
      document.getElementById('btn-download-backup').addEventListener('click', () => {
        if (global.DCFolderManager) {
          global.DCFolderManager.downloadProjectBackup();
        }
      });
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = TopToolbar;
  } else {
    global.DCTopToolbar = TopToolbar;
  }

})(typeof window !== 'undefined' ? window : this);
