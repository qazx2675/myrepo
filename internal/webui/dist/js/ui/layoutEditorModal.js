/**
 * layoutEditorModal.js - 데이터센터 그리드 규격 및 랙 설정 에디터 (기본 52U / 서버 영역 48U 설정, 전력 항목 제거)
 */
(function (global) {
  'use strict';

  class LayoutEditorModal {
    constructor(store) {
      this.store = store;
      this.modalEl = null;
      this.initDom();
      this.bindEvents();
    }

    initDom() {
      if (document.getElementById('dc-layout-editor-modal')) {
        this.modalEl = document.getElementById('dc-layout-editor-modal');
        return;
      }

      const modal = document.createElement('div');
      modal.id = 'dc-layout-editor-modal';
      modal.className = 'dc-modal-overlay hidden';
      modal.innerHTML = `
        <div class="dc-modal-dialog dc-modal-large">
          <div class="dc-modal-header">
            <div class="dc-modal-title">
              <span>🏗️</span>
              <h3>데이터센터 공간 및 랙(U 높이 · 서버 장착 범위) 설정</h3>
            </div>
            <button class="dc-modal-close" id="btn-close-layout-modal">&times;</button>
          </div>

          <div class="dc-modal-body dc-layout-editor-body">
            <!-- 1. 사이트 그리드 규격 설정 -->
            <div class="dc-card">
              <div class="dc-card-title">📐 데이터센터 바닥 타일 그리드 규격</div>
              <div class="dc-form-grid-3">
                <div class="dc-form-row">
                  <label>가로 타일 수 (Width: 10~120)</label>
                  <input type="number" id="input-grid-width" min="10" max="120" value="28">
                </div>
                <div class="dc-form-row">
                  <label>세로 타일 수 (Depth: 10~120)</label>
                  <input type="number" id="input-grid-depth" min="10" max="120" value="28">
                </div>
                <div class="dc-form-row">
                  <label>1타일 규격 (mm)</label>
                  <input type="number" id="input-tile-size" value="600" readonly class="dc-input-readonly">
                </div>
              </div>
              <button class="dc-btn-xs dc-btn-secondary" id="btn-apply-grid-size" style="margin-top: 8px;">
                그리드 크기 즉시 적용
              </button>
            </div>

            <!-- 2. 랙 추가 및 수정 폼 & 2D 인터랙티브 미니 맵 -->
            <div class="dc-editor-split">
              <!-- 좌측: 랙 속성 편집 폼 -->
              <div class="dc-card dc-rack-form-card">
                <div class="dc-card-title">🗄️ 랙(캐비닛) U 규격 및 위치 편집</div>
                
                <div class="dc-form-grid-2">
                  <div class="dc-form-row">
                    <label>랙 라벨 (예: X001Y001) *</label>
                    <input type="text" id="input-rack-id" placeholder="예: X001Y001, X029Y001">
                  </div>
                  <div class="dc-form-row">
                    <label>회전 각도 (90° 단위)</label>
                    <select id="select-rack-rotation">
                      <option value="0">0° (전면: +Z)</option>
                      <option value="90">90° (전면: +X)</option>
                      <option value="180">180° (전면: -Z)</option>
                      <option value="270">270° (전면: -X)</option>
                    </select>
                  </div>
                </div>

                <div class="dc-form-grid-2">
                  <div class="dc-form-row">
                    <label>그리드 가로 좌표</label>
                    <input type="number" id="input-rack-gx" min="1" max="120" value="3">
                  </div>
                  <div class="dc-form-row">
                    <label>그리드 세로 좌표 (통로 4칸 간격 권장)</label>
                    <input type="number" id="input-rack-gz" min="1" max="120" value="3">
                  </div>
                </div>

                <div class="dc-form-grid-2">
                  <div class="dc-form-row">
                    <label>전체 랙 높이 (maxU, 기본 52U) *</label>
                    <div style="display: flex; gap: 6px;">
                      <select id="select-rack-max-u-preset" style="width: 110px;">
                        <option value="52" selected>52U (기본)</option>
                        <option value="48">48U</option>
                        <option value="45">45U</option>
                        <option value="42">42U</option>
                        <option value="custom">직접 입력</option>
                      </select>
                      <input type="number" id="input-rack-max-u" min="10" max="64" value="52" style="flex: 1;">
                    </div>
                  </div>
                  <div class="dc-form-row">
                    <label>서버 장착 최대 범위 (나머지는 스위치 영역) *</label>
                    <input type="number" id="input-rack-server-max-u" min="1" max="64" value="48">
                  </div>
                </div>

                <div class="dc-report-note" style="margin-top:6px;">
                  * 기본값: 전체 <strong>52U</strong> 중 <strong>1U~48U</strong>는 서버 장착 영역, 상단 <strong>49U~52U(4U)</strong>는 스위치/네트워크 전용 영역으로 동작합니다.
                </div>

                <div class="dc-form-actions" style="margin-top: 14px; display: flex; gap: 8px;">
                  <button class="dc-btn dc-btn-primary" id="btn-save-rack-config" style="flex: 1;">
                    💾 랙 설정 저장
                  </button>
                  <button class="dc-btn dc-btn-danger" id="btn-delete-rack-config">
                    삭제
                  </button>
                </div>
              </div>

              <!-- 우측: 2D 바닥 그리드 미니맵 -->
              <div class="dc-card dc-minimap-card">
                <div class="dc-card-title">🗺️ 2D 플로어 맵 (클릭하여 랙 선택)</div>
                <div class="dc-minimap-wrapper">
                  <canvas id="dc-layout-minimap-canvas" width="380" height="280"></canvas>
                </div>
                <div class="dc-minimap-legend">
                  <span>* 랙 사각형 클릭 시 해당 랙 정보를 불러옵니다.</span>
                </div>
              </div>
            </div>
          </div>

          <div class="dc-modal-footer">
            <button class="dc-btn dc-btn-outline" id="btn-close-layout-modal-footer">닫기</button>
          </div>
        </div>
      `;

      document.body.appendChild(modal);
      this.modalEl = modal;

      modal.addEventListener('wheel', (e) => {
        e.stopPropagation();
      }, { passive: true });
      modal.addEventListener('pointerdown', (e) => {
        e.stopPropagation();
      });
      modal.addEventListener('pointermove', (e) => {
        e.stopPropagation();
      });
    }

    bindEvents() {
      if (this._eventsBound) return;
      this._eventsBound = true;

      const closeBtn = document.getElementById('btn-close-layout-modal');
      if (closeBtn) closeBtn.onclick = () => this.hide();
      const closeFooterBtn = document.getElementById('btn-close-layout-modal-footer');
      if (closeFooterBtn) closeFooterBtn.onclick = () => this.hide();

      const applyGridBtn = document.getElementById('btn-apply-grid-size');
      if (applyGridBtn) {
        applyGridBtn.onclick = () => {
          const w = parseInt(document.getElementById('input-grid-width').value, 10);
          const d = parseInt(document.getElementById('input-grid-depth').value, 10);
          if (w >= 5 && d >= 5) {
            this.store.updateSiteGrid(w, d, 600);
            this.renderMinimap();
            if (global.DCToast) global.DCToast.success(`그리드가 ${w} × ${d} 타일로 변경되었습니다.`);
          }
        };
      }

      const presetSelect = document.getElementById('select-rack-max-u-preset');
      const inputMaxU = document.getElementById('input-rack-max-u');
      const inputSrvMaxU = document.getElementById('input-rack-server-max-u');

      if (presetSelect) {
        presetSelect.onchange = (e) => {
          const val = e.target.value;
          if (val !== 'custom') {
            const num = parseInt(val, 10);
            inputMaxU.value = num;
            inputSrvMaxU.value = Math.max(1, num - 4);
          }
        };
      }

      if (inputMaxU) {
        inputMaxU.oninput = (e) => {
          const num = parseInt(e.target.value, 10);
          if (!isNaN(num) && num > 4) {
            inputSrvMaxU.value = num - 4;
          }
        };
      }

      const saveRackBtn = document.getElementById('btn-save-rack-config');
      if (saveRackBtn) {
        saveRackBtn.onclick = () => {
          const rackId = document.getElementById('input-rack-id').value.trim().toUpperCase();
          const gx = parseInt(document.getElementById('input-rack-gx').value, 10);
          const gz = parseInt(document.getElementById('input-rack-gz').value, 10);
          const rot = parseInt(document.getElementById('select-rack-rotation').value, 10);
          const maxU = parseInt(document.getElementById('input-rack-max-u').value, 10) || 52;
          const serverMaxU = parseInt(document.getElementById('input-rack-server-max-u').value, 10) || Math.max(1, maxU - 4);

          if (!rackId) {
            alert('랙 라벨을 입력해주세요.');
            return;
          }

          this.store.upsertRack({
            rackId,
            name: rackId,
            gridX: gx,
            gridZ: gz,
            rotationDeg: rot,
            maxU,
            serverMaxU: Math.min(maxU, serverMaxU)
          });

          this.renderMinimap();
          if (global.DCToast) {
            global.DCToast.success(`랙 '${rackId}' (${maxU}U / 서버 ${serverMaxU}U) 설정이 저장되었습니다.`);
          }
        };
      }

      const deleteRackBtn = document.getElementById('btn-delete-rack-config');
      if (deleteRackBtn) {
        deleteRackBtn.onclick = () => {
          const rackId = document.getElementById('input-rack-id').value.trim().toUpperCase();
          if (rackId) {
            this.store.deleteRack(rackId);
            this.renderMinimap();
          }
        };
      }

      const canvas = document.getElementById('dc-layout-minimap-canvas');
      canvas.addEventListener('click', (e) => {
        const rect = canvas.getBoundingClientRect();
        const clickX = e.clientX - rect.left;
        const clickY = e.clientY - rect.top;

        const site = this.store.getActiveSite();
        const gw = site ? site.gridWidth : 28;
        const gd = site ? site.gridDepth : 28;

        const cellW = canvas.width / gw;
        const cellH = canvas.height / gd;

        const gx = Math.min(gw, Math.max(1, Math.floor(clickX / cellW) + 1));
        const gz = Math.min(gd, Math.max(1, Math.floor(clickY / cellH) + 1));

        document.getElementById('input-rack-gx').value = gx;
        document.getElementById('input-rack-gz').value = gz;

        const racks = this.store.getRacks();
        const clickedRack = racks.find(r => {
          const p = this.store.getRackEffectivePosition(r);
          return p.gridX === gx && (p.gridZ === gz || p.gridZ + 1 === gz);
        });
        if (clickedRack) {
          this.loadRackToForm(clickedRack);
        }

        this.renderMinimap();
      });
    }

    show() {
      this.initDom();
      const site = this.store.getActiveSite();
      if (site) {
        document.getElementById('input-grid-width').value = site.gridWidth || 28;
        document.getElementById('input-grid-depth').value = site.gridDepth || 28;
      }

      const selectedId = this.store.state.selectedRackId;
      if (selectedId) {
        const rack = this.store.getRack(selectedId);
        if (rack) this.loadRackToForm(rack);
      }

      this.modalEl.classList.remove('hidden');
      this.renderMinimap();
    }

    hide() {
      if (this.modalEl) {
        this.modalEl.classList.add('hidden');
      }
    }

    loadRackToForm(rack) {
      const p = this.store.getRackEffectivePosition(rack);
      const maxU = rack.maxU || 52;
      const srvMaxU = rack.serverMaxU !== undefined ? rack.serverMaxU : Math.max(1, maxU - 4);
      document.getElementById('input-rack-id').value = rack.rackId;
      document.getElementById('input-rack-gx').value = p.gridX;
      document.getElementById('input-rack-gz').value = p.gridZ;
      document.getElementById('select-rack-rotation').value = rack.rotationDeg || 0;
      document.getElementById('input-rack-max-u').value = maxU;
      document.getElementById('input-rack-server-max-u').value = srvMaxU;

      const presetSelect = document.getElementById('select-rack-max-u-preset');
      if (['42', '45', '48', '52'].includes(String(maxU))) {
        presetSelect.value = String(maxU);
      } else {
        presetSelect.value = 'custom';
      }
    }

    renderMinimap() {
      const canvas = document.getElementById('dc-layout-minimap-canvas');
      if (!canvas) return;

      const ctx = canvas.getContext('2d');
      const w = canvas.width;
      const h = canvas.height;

      ctx.fillStyle = '#0f172a';
      ctx.fillRect(0, 0, w, h);

      const site = this.store.getActiveSite();
      const gw = site ? site.gridWidth : 28;
      const gd = site ? site.gridDepth : 28;

      const cellW = w / gw;
      const cellH = h / gd;

      ctx.strokeStyle = '#1e293b';
      ctx.lineWidth = 1;
      for (let x = 0; x <= gw; x++) {
        ctx.beginPath();
        ctx.moveTo(x * cellW, 0);
        ctx.lineTo(x * cellW, h);
        ctx.stroke();
      }
      for (let z = 0; z <= gd; z++) {
        ctx.beginPath();
        ctx.moveTo(0, z * cellH);
        ctx.lineTo(w, z * cellH);
        ctx.stroke();
      }

      const racks = this.store.getRacks();
      const selectedId = this.store.state.selectedRackId;

      racks.forEach(rack => {
        const p = this.store.getRackEffectivePosition(rack);
        const rx = (p.gridX - 1) * cellW;
        const rz = (p.gridZ - 1) * cellH;
        const rw = cellW * (rack.widthTiles || 1);
        const rh = cellH * (rack.depthTiles || 2);

        const isSel = String(rack.rackId).toUpperCase() === selectedId;
        ctx.fillStyle = isSel ? '#2979ff' : '#10b981';
        ctx.fillRect(rx, rz, rw, rh);
        ctx.strokeStyle = isSel ? '#00e5ff' : '#059669';
        ctx.strokeRect(rx, rz, rw, rh);
      });
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = LayoutEditorModal;
  } else {
    if (typeof window !== 'undefined') {
      const initModal = () => {
        if (!global.DCLayoutEditorModal) {
          global.DCLayoutEditorModal = new LayoutEditorModal(global.DCStore);
        }
      };
      if (document.readyState === 'loading') {
        window.addEventListener('DOMContentLoaded', initModal);
      } else {
        initModal();
      }
    }
  }

})(typeof window !== 'undefined' ? window : this);
