/**
 * sharedFolderModal.js - Windows 네트워크 공유 폴더(SMB/UNC) 및 로컬 데이터 저장 위치 설정 모달
 */
(function (global) {
  'use strict';

  class SharedFolderModal {
    constructor(store, folderManager) {
      this.store = store;
      this.folderManager = folderManager;
      this.modalEl = null;
      this.initDom();
      this.bindEvents();
    }

    initDom() {
      if (document.getElementById('dc-shared-folder-modal')) {
        this.modalEl = document.getElementById('dc-shared-folder-modal');
        return;
      }

      const modal = document.createElement('div');
      modal.id = 'dc-shared-folder-modal';
      modal.className = 'dc-modal-overlay hidden';
      modal.innerHTML = `
        <div class="dc-modal-dialog" style="width: 720px; max-width: 95vw;">
          <div class="dc-modal-header">
            <div class="dc-modal-title">
              <span style="font-size: 20px;">📁</span>
              <h3>Windows 공유 폴더 및 데이터 저장 위치 설정</h3>
            </div>
            <button class="dc-modal-close" id="btn-close-shared-modal">&times;</button>
          </div>

          <div class="dc-modal-body">
            <!-- 1. 현재 저장 위치 상태 카드 -->
            <div class="dc-card">
              <div class="dc-card-title">📌 현재 데이터 저장 위치 및 상태</div>
              <div style="display: flex; flex-direction: column; gap: 8px; font-size: 12px; margin-top: 6px;">
                <div style="display: flex; justify-content: space-between; align-items: center;">
                  <span style="color: var(--text-secondary);">저장 방식:</span>
                  <span id="lbl-shared-mode-badge" style="font-weight: 700; color: #58a6ff;">확인 중...</span>
                </div>
                <div style="display: flex; flex-direction: column; gap: 4px;">
                  <span style="color: var(--text-secondary);">활성 디렉터리 경로:</span>
                  <div id="lbl-shared-active-path" style="font-family: Consolas, monospace; background: #05070a; padding: 7px 10px; border-radius: 4px; border: 1px solid #30363d; word-break: break-all; color: #38bdf8;">
                    확인 중...
                  </div>
                </div>
                <div style="display: flex; justify-content: space-between; align-items: center; margin-top: 4px;">
                  <span style="color: var(--text-secondary);">저장 파일 유무:</span>
                  <span id="lbl-shared-files-status" style="color: #8b949e;">확인 중...</span>
                </div>
              </div>

              <!-- 공유 폴더 최신 데이터 즉시 동기화 버튼 -->
              <div style="margin-top: 14px; padding-top: 12px; border-top: 1px dashed #30363d; display: flex; align-items: center; justify-content: space-between;">
                <div style="font-size: 11px; color: #8b949e;">
                  다른 사용자가 CSV를 할당하거나 랙을 변경한 경우 최신본을 불러옵니다.
                </div>
                <button type="button" class="dc-tb-btn dc-btn-accent" id="btn-modal-sync-now" style="font-weight: 600; padding: 6px 14px;">
                  <span>🔄</span> 최신 데이터 즉시 동기화
                </button>
              </div>
            </div>

            <!-- 2. 공유 폴더 경로 변경 입력 폼 -->
            <div class="dc-card" style="margin-top: 12px;">
              <div class="dc-card-title">🌐 저장 경로 변경 (Windows UNC 네트워크 공유 경로 또는 로컬/네트워크 드라이브)</div>
              <div style="font-size: 11px; color: var(--text-secondary); margin-bottom: 8px;">
                동일 네트워크 내 Windows 공유 폴더 경로(UNC)나 마운트된 드라이브(Z:\\ 등)를 입력하세요.
              </div>
              <div style="display: flex; gap: 8px; align-items: center;">
                <input type="text" id="input-shared-dir-path" class="dc-input" placeholder="예) \\\\\\\\192.168.1.100\\\\datacenter_share\\\\data 또는 Z:\\\\dc_data" style="flex: 1; padding: 7px 10px; font-family: Consolas, monospace; font-size: 12px; background: #05070a; border: 1px solid #30363d; color: #f0f6fc; border-radius: 4px;">
                <button type="button" class="dc-tb-btn dc-btn-folder" id="btn-modal-apply-path" style="padding: 7px 14px; font-weight: 700;">
                  <span>💾</span> 경로 변경 적용
                </button>
              </div>
              <div style="margin-top: 10px; display: flex; justify-content: flex-end;">
                <button type="button" class="dc-tb-btn" id="btn-modal-reset-local" style="font-size: 11px; color: #94a3b8;">
                  <span>🏠</span> 기본 로컬 data/ 폴더로 복원
                </button>
              </div>
            </div>

            <!-- 3. 사용 안내 가이드 -->
            <div class="dc-report-note" style="background: rgba(13, 17, 23, 0.7); padding: 10px 12px; border-radius: 6px; border: 1px solid #21262d; line-height: 1.6;">
              <strong style="color: #e2e8f0;">💡 다중 사용자 공유 폴더 활용 가이드:</strong><br>
              1. <strong>각자 PC에서 단독 실행:</strong> <code>datacenter-viewer.exe</code>는 팀원 각자의 개인 PC에서 개별 실행됩니다.<br>
              2. <strong>마스터 데이터 실시간 공유:</strong> 사내 윈도우 공유 폴더(예: <code>\\\\\\\\서버IP\\\\공유이름\\\\data</code>)를 지정하면 모든 사용자가 동일한 랙과 입고 현황을 참조합니다.<br>
              3. <strong>자동 저장 & 락 방지:</strong> 개인이 CSV를 업로드하거나 랙을 변경하면 공유 폴더의 <code>datacenter_layout.json</code>과 <code>allocated_hosts.csv</code>에 즉각 안전하게 자동 저장됩니다.<br>
              4. <strong>경로 영구 보존:</strong> 설정된 경로는 실행 파일 위치의 <code>data_path.txt</code>에 저장되므로 다음 실행 시에도 자동으로 공유 폴더에 접속합니다.
            </div>
          </div>

          <div class="dc-modal-footer">
            <button type="button" class="dc-tb-btn" id="btn-close-shared-modal-foot">닫기</button>
          </div>
        </div>
      `;
      document.body.appendChild(modal);
      this.modalEl = modal;
    }

    async refreshStatus() {
      if (!this.folderManager) return;
      try {
        const info = await this.folderManager.getDataDirInfo();
        const badgeEl = document.getElementById('lbl-shared-mode-badge');
        const pathEl = document.getElementById('lbl-shared-active-path');
        const filesEl = document.getElementById('lbl-shared-files-status');
        const inputEl = document.getElementById('input-shared-dir-path');

        if (!info) {
          if (badgeEl) badgeEl.textContent = '웹 브라우저 모드 (로컬 폴더 연결)';
          if (pathEl) pathEl.textContent = this.folderManager.folderName || '연결된 폴더 없음';
          if (filesEl) filesEl.textContent = '-';
          return;
        }

        if (badgeEl) {
          if (info.isCustom) {
            badgeEl.innerHTML = '<span style="color: #00e5ff;">🟢 Windows 네트워크 공유 폴더 (다중 사용자 동기화)</span>';
          } else {
            badgeEl.innerHTML = '<span style="color: #7ee787;">🏠 실행 파일 기본 로컬 폴더 (./data)</span>';
          }
        }

        if (pathEl) {
          pathEl.textContent = info.dir || info.absolute;
        }

        if (filesEl) {
          const lOk = info.layoutExists ? '<span style="color:#7ee787">datacenter_layout.json (존재함)</span>' : '<span style="color:#f85149">datacenter_layout.json (없음)</span>';
          const cOk = info.csvExists ? '<span style="color:#7ee787">allocated_hosts.csv (존재함)</span>' : '<span style="color:#8b949e">allocated_hosts.csv (비어있음)</span>';
          filesEl.innerHTML = `${lOk}  |  ${cOk}`;
        }

        if (inputEl && !inputEl.value) {
          inputEl.value = info.isCustom ? info.dir : '';
        }
      } catch (err) {
        console.error('refreshStatus error:', err);
      }
    }

    show() {
      if (!this.modalEl) this.initDom();
      this.modalEl.classList.remove('hidden');
      this.refreshStatus();
    }

    hide() {
      if (this.modalEl) {
        this.modalEl.classList.add('hidden');
      }
    }

    bindEvents() {
      const closeTop = document.getElementById('btn-close-shared-modal');
      const closeFoot = document.getElementById('btn-close-shared-modal-foot');
      if (closeTop) closeTop.addEventListener('click', () => this.hide());
      if (closeFoot) closeFoot.addEventListener('click', () => this.hide());

      // 모달 바깥 배경 클릭 시 닫기
      if (this.modalEl) {
        this.modalEl.addEventListener('click', (e) => {
          if (e.target === this.modalEl) this.hide();
        });
      }

      // 1. 최신 데이터 동기화 버튼
      const btnSync = document.getElementById('btn-modal-sync-now');
      if (btnSync) {
        btnSync.addEventListener('click', async () => {
          try {
            btnSync.disabled = true;
            btnSync.innerHTML = '<span>⏳</span> 동기화 중...';
            await this.folderManager.syncFromShared();
            const devCount = this.store && this.store.state.devices ? this.store.state.devices.length : 0;
            const rackCount = this.store && this.store.getRacks() ? this.store.getRacks().length : 0;
            if (global.DCToast) {
              global.DCToast.success(`공유 폴더 데이터 동기화 완료 (랙 ${rackCount}개, 장비 ${devCount}대)`);
            }
            await this.refreshStatus();
          } catch (err) {
            alert(`동기화 중 오류가 발생했습니다: ${err.message}`);
          } finally {
            btnSync.disabled = false;
            btnSync.innerHTML = '<span>🔄</span> 최신 데이터 즉시 동기화';
          }
        });
      }

      // 2. 경로 변경 적용 버튼
      const btnApply = document.getElementById('btn-modal-apply-path');
      const inputPath = document.getElementById('input-shared-dir-path');
      if (btnApply && inputPath) {
        btnApply.addEventListener('click', async () => {
          const pathVal = inputPath.value.trim();
          if (!pathVal) {
            alert('공유 폴더 경로나 로컬 폴더 경로를 입력해주세요.\n예: \\\\\\\\192.168.1.100\\\\datacenter_share\\\\data 또는 Z:\\\\dc_data');
            return;
          }
          try {
            btnApply.disabled = true;
            btnApply.innerHTML = '<span>⏳</span> 연결 중...';
            await this.folderManager.setSharedDataDir(pathVal);
            if (global.DCToast) {
              global.DCToast.success(`데이터 저장 위치가 '${pathVal}'(으)로 변경되었습니다.`);
            }
            await this.refreshStatus();
          } catch (err) {
            alert(`경로 연결 오류: ${err.message}\n\n* Windows 탐색기에서 해당 공유 폴더에 정상 접근 가능한지 확인해주세요.`);
          } finally {
            btnApply.disabled = false;
            btnApply.innerHTML = '<span>💾</span> 경로 변경 적용';
          }
        });
      }

      // 3. 기본 로컬 data/ 폴더 복원
      const btnReset = document.getElementById('btn-modal-reset-local');
      if (btnReset) {
        btnReset.addEventListener('click', async () => {
          if (!confirm('기본 로컬 data/ 폴더로 저장 위치를 복원하시겠습니까?')) {
            return;
          }
          try {
            await this.folderManager.setSharedDataDir('data');
            if (inputPath) inputPath.value = '';
            if (global.DCToast) {
              global.DCToast.success('기본 로컬 data/ 폴더로 복원되었습니다.');
            }
            await this.refreshStatus();
          } catch (err) {
            alert(`복원 오류: ${err.message}`);
          }
        });
      }
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = SharedFolderModal;
  } else {
    global.DCSharedFolderModal = new SharedFolderModal(global.DCStore, global.DCFolderManager);
  }

})(typeof window !== 'undefined' ? window : this);
