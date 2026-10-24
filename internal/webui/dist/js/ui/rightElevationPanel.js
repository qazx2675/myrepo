/**
 * rightElevationPanel.js - 우측 패널: 52U 랙 실장도(1~48U 서버 영역 + 49~52U 스위치/네트워크 전용 영역 구분) 및 수동/자동 할당
 */
(function (global) {
  'use strict';

  // CSV/공유 폴더에서 들어온 문자열(호스트명, 모델, 랙 이름, 색상)을 innerHTML 에 넣기 전에 이스케이프 (XSS 방지)
  function escapeHtml(str) {
    if (str === undefined || str === null) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  class RightElevationPanel {
    constructor(containerEl, store) {
      this.container = containerEl;
      this.store = store;
      this.currentViewOrientation = 'front';
      this.initDom();
      this.bindEvents();
    }

    initDom() {
      this.container.innerHTML = `
        <div class="dc-panel-header">
          <div class="dc-panel-title">
            <span>🗄️</span> 실장도 (Elevation): <strong id="lbl-elevation-rack-id">-</strong>
          </div>
          <button class="dc-icon-btn" id="btn-collapse-right" title="패널 접기/펼치기">▶</button>
        </div>

        <div class="dc-elevation-toolbar">
          <div class="dc-btn-group">
            <button class="dc-sub-btn active" id="btn-elev-front">전면(Front)</button>
            <button class="dc-sub-btn" id="btn-elev-back">후면(Back)</button>
          </div>
          <button class="dc-btn-xs dc-btn-primary" id="btn-quick-add-dev">+ 서버/장비 추가</button>
        </div>

        <div class="dc-elevation-content">
          <!-- 중앙 랙 실장도 슬롯 컨테이너 (1U ~ 52U) -->
          <div class="dc-rack-slots-wrapper" id="dc-rack-slots-container">
            <div class="dc-elevation-placeholder">
              3D 뷰포트나 좌측 목록에서 랙을 선택하세요.
            </div>
          </div>
        </div>

        <!-- 수동 장비 추가/편집 모달 -->
        <div class="dc-mini-modal hidden" id="dc-device-edit-modal">
          <div class="dc-mini-modal-header">
            <span id="lbl-device-modal-title">장비 마운트 설정</span>
            <button class="dc-close-xs" id="btn-close-device-modal">&times;</button>
          </div>
          <div class="dc-mini-modal-body">
            <input type="hidden" id="input-dev-id">
            <div class="dc-form-grid-2">
              <div class="dc-form-row">
                <label>랙 라벨</label>
                <input type="text" id="input-dev-rack" readonly class="dc-input-readonly">
              </div>
              <div class="dc-form-row">
                <label>모델코드 *</label>
                <input type="text" id="input-dev-model" placeholder="예: 012_001, switch">
              </div>
            </div>
            <div class="dc-form-row">
              <label>hostname (공란 시 '임시할당'으로 등록)</label>
              <input type="text" id="input-dev-host" placeholder="비워두면 임시할당 (^p:HP, ^d:DELL, ^c:Cisco, ^h:조립, ^m:SMCI, ^l:Lenovo)">
            </div>
            <div class="dc-form-grid-2">
              <div class="dc-form-row">
                <label>몇U (폼팩터 높이) *</label>
                <input type="number" id="input-dev-size" min="1" max="20" value="1">
              </div>
              <div class="dc-form-row">
                <label>장착 시작 U (비우면 1U부터 자동할당)</label>
                <input type="number" id="input-dev-start-u" min="1" max="60" placeholder="자동할당">
              </div>
            </div>
            <div class="dc-form-grid-2">
              <div class="dc-form-row">
                <label>마운트 방향</label>
                <select id="select-dev-orientation">
                  <option value="front">Front (전면)</option>
                  <option value="back">Back (후면)</option>
                </select>
              </div>
              <div class="dc-form-row">
                <label>입고 상태</label>
                <select id="select-dev-sim">
                  <option value="false">일반 (확정)</option>
                  <option value="true">신규 입고예정 (시뮬레이션)</option>
                </select>
              </div>
            </div>
            <div class="dc-error-msg hidden" id="msg-device-error"></div>
          </div>
          <div class="dc-mini-modal-footer">
            <button class="dc-btn-xs dc-btn-danger hidden" id="btn-delete-device">삭제</button>
            <div style="flex: 1;"></div>
            <button class="dc-btn-xs dc-btn-outline" id="btn-cancel-device-modal">취소</button>
            <button class="dc-btn-xs dc-btn-primary" id="btn-save-device">저장</button>
          </div>
        </div>
      `;
    }

    bindEvents() {
      const collapseBtn = document.getElementById('btn-collapse-right');
      if (collapseBtn) {
        collapseBtn.addEventListener('click', () => {
          this.container.classList.toggle('collapsed');
          collapseBtn.textContent = this.container.classList.contains('collapsed') ? '◀' : '▶';
        });
      }

      const btnFront = document.getElementById('btn-elev-front');
      const btnBack = document.getElementById('btn-elev-back');

      btnFront.addEventListener('click', () => {
        btnFront.classList.add('active');
        btnBack.classList.remove('active');
        this.currentViewOrientation = 'front';
        this.renderElevation();
      });

      btnBack.addEventListener('click', () => {
        btnBack.classList.add('active');
        btnFront.classList.remove('active');
        this.currentViewOrientation = 'back';
        this.renderElevation();
      });

      document.getElementById('btn-quick-add-dev').addEventListener('click', () => {
        const rackId = this.store.state.selectedRackId;
        if (!rackId) {
          alert('장비를 추가할 랙을 먼저 선택해주세요.');
          return;
        }
        this.openDeviceModal(null, '');
      });

      document.getElementById('btn-close-device-modal').addEventListener('click', () => this.closeDeviceModal());
      document.getElementById('btn-cancel-device-modal').addEventListener('click', () => this.closeDeviceModal());
      document.getElementById('btn-save-device').addEventListener('click', () => this.saveDeviceFromModal());

      document.getElementById('btn-delete-device').addEventListener('click', () => {
        const devId = document.getElementById('input-dev-id').value;
        if (devId) {
          this.store.deleteDevice(devId);
          this.closeDeviceModal();
        }
      });

      this.store.subscribe('rackSelected', () => this.renderElevation());
      this.store.subscribe('activeSiteChanged', () => this.renderElevation());
      this.store.subscribe('devicesUpdated', () => this.renderElevation());
      this.store.subscribe('rackDevicesChanged', () => this.renderElevation());
      this.store.subscribe('vendorColorsChanged', () => this.renderElevation());
      this.store.subscribe('deviceSelected', (devId) => this.highlightSelectedDevice(devId));
    }

    renderElevation() {
      const rackId = this.store.state.selectedRackId;
      const titleEl = document.getElementById('lbl-elevation-rack-id');
      const container = document.getElementById('dc-rack-slots-container');

      if (!rackId) {
        titleEl.textContent = '-';
        container.innerHTML = `
          <div class="dc-elevation-placeholder">
            3D 뷰포트나 좌측 목록에서 랙을 선택하세요.
          </div>
        `;
        return;
      }

      const rack = this.store.getRack(rackId);
      if (!rack) return;

      const maxU = rack.maxU || 52;
      const serverMaxU = rack.serverMaxU !== undefined ? rack.serverMaxU : Math.max(1, maxU - 4);
      titleEl.textContent = `${rack.name || rackId} (${maxU}U / 서버 ${serverMaxU}U)`;
      container.innerHTML = '';

      const devices = this.store.getDevicesForRack(rackId);
      const vendorColors = this.store.state.vendorColors || {};

      const slotMap = new Map();
      devices.forEach(dev => {
        for (let u = dev.startU; u <= dev.endU; u++) {
          slotMap.set(u, dev);
        }
      });

      // 상단(maxU=52U)부터 하단(1U) 순서로 렌더링
      for (let u = maxU; u >= 1; u--) {
        // 48U와 49U 사이에 네트워크/서버 영역 구분 배너 삽입
        if (u === maxU && maxU > serverMaxU) {
          const netHeader = document.createElement('div');
          netHeader.className = 'dc-zone-divider dc-zone-network';
          netHeader.textContent = `🌐 ${serverMaxU + 1}U ~ ${maxU}U : 스위치 / 네트워크 전용 영역 (4U)`;
          container.appendChild(netHeader);
        }
        if (u === serverMaxU) {
          const srvHeader = document.createElement('div');
          srvHeader.className = 'dc-zone-divider dc-zone-server';
          srvHeader.textContent = `🖥️ 1U ~ ${serverMaxU}U : 서버 마운트 영역 (${serverMaxU}U)`;
          container.appendChild(srvHeader);
        }

        const dev = slotMap.get(u);

        if (dev) {
          if (u === dev.endU) {
            const blockEl = document.createElement('div');
            const heightPx = dev.formFactorU * 23;
            blockEl.style.height = `${heightPx}px`;

            const isSim = !!dev.isSimulation;
            const isSwitch = dev.deviceType === 'switch';
            const vendor = dev.vendor || (global.DCValidator ? global.DCValidator.determineVendor(dev.hostname, dev.model) : 'OTHER');
            const vColor = vendorColors[vendor] || dev.colorTag || '#4caf50';
            const isTemp = !dev.hostname || dev.isTemp;

            blockEl.className = `dc-elevation-slot dc-slot-occupied ${isSim ? 'dc-elev-sim' : ''} ${dev.deviceId === this.store.state.selectedDeviceId ? 'selected' : ''}`;
            blockEl.style.borderLeft = `6px solid ${vColor}`;
            if (!isSim) {
              blockEl.style.background = `linear-gradient(90deg, ${vColor}33 0%, #1c2128 85%)`;
            }
            blockEl.setAttribute('data-device-id', dev.deviceId);

            const simTag = isSim ? '<span class="dc-tag-sim">입고예정</span>' : '';
            const typeTag = isSwitch ? '<span class="dc-tag-switch">SW</span>' : '';
            const hostLabel = isTemp
              ? `<span style="color:#f3b3ff; font-weight:800;">[임시할당]</span>`
              : `<strong>${escapeHtml(dev.hostname)}</strong>`;

            blockEl.innerHTML = `
              <div class="dc-elev-content">
                ${simTag}${typeTag}
                <span class="dc-elev-vendor-badge" style="background:${escapeHtml(vColor)};">${escapeHtml(vendor)}</span>
                <span class="dc-elev-model-code">${escapeHtml(dev.model || '-')}</span>
                ${hostLabel}
              </div>
              <div class="dc-u-number-label" style="color:#ffd600; font-weight:700;">
                ${escapeHtml(dev.startU)}${dev.formFactorU > 1 ? `~${escapeHtml(dev.endU)}` : ''}U (${escapeHtml(dev.formFactorU)}U)
              </div>
            `;

            blockEl.addEventListener('click', (e) => {
              e.stopPropagation();
              this.store.selectDevice(dev.deviceId);
              this.openDeviceModal(dev);
            });

            container.appendChild(blockEl);
          }
        } else {
          const isNetZone = u > serverMaxU;
          const emptySlotEl = document.createElement('div');
          emptySlotEl.className = `dc-elevation-slot dc-slot-empty ${isNetZone ? 'dc-slot-empty-net' : ''}`;
          emptySlotEl.style.height = '22px';
          emptySlotEl.innerHTML = `
            <div class="dc-empty-u-inner">
              <span class="dc-empty-plus">+</span>
              <span class="dc-empty-text">${isNetZone ? '네트워크/스위치 빈 슬롯' : '서버 빈 슬롯'}</span>
            </div>
            <div class="dc-u-number-label">${u}U</div>
          `;

          emptySlotEl.addEventListener('click', () => {
            this.openDeviceModal(null, u);
          });

          container.appendChild(emptySlotEl);
        }
      }
    }

    openDeviceModal(dev = null, defaultStartU = '') {
      const rackId = this.store.state.selectedRackId;
      if (!rackId) return;

      const modal = document.getElementById('dc-device-edit-modal');
      const titleEl = document.getElementById('lbl-device-modal-title');
      const deleteBtn = document.getElementById('btn-delete-device');
      const errorMsg = document.getElementById('msg-device-error');

      errorMsg.classList.add('hidden');
      errorMsg.textContent = '';

      document.getElementById('input-dev-rack').value = rackId;

      if (dev) {
        titleEl.textContent = `장비 마운트 수정 (${dev.hostname || '임시할당'})`;
        document.getElementById('input-dev-id').value = dev.deviceId;
        document.getElementById('input-dev-host').value = dev.hostname || '';
        document.getElementById('input-dev-start-u').value = dev.startU;
        document.getElementById('input-dev-size').value = dev.formFactorU;
        document.getElementById('input-dev-model').value = dev.model || '012_001';
        document.getElementById('select-dev-orientation').value = dev.orientation || 'front';
        document.getElementById('select-dev-sim').value = String(!!dev.isSimulation);
        deleteBtn.classList.remove('hidden');
      } else {
        titleEl.textContent = `새 장비 할당 (${rackId} 랙)`;
        document.getElementById('input-dev-id').value = '';
        document.getElementById('input-dev-host').value = '';
        document.getElementById('input-dev-start-u').value = defaultStartU !== '' ? defaultStartU : '';
        document.getElementById('input-dev-size').value = 1;
        document.getElementById('input-dev-model').value = (defaultStartU && defaultStartU > 48) ? 'switch' : '012_001';
        document.getElementById('select-dev-orientation').value = this.currentViewOrientation;
        document.getElementById('select-dev-sim').value = String(this.store.state.simulationMode);
        deleteBtn.classList.add('hidden');
      }

      modal.classList.remove('hidden');
    }

    closeDeviceModal() {
      const modal = document.getElementById('dc-device-edit-modal');
      if (modal) modal.classList.add('hidden');
    }

    saveDeviceFromModal() {
      const rackId = document.getElementById('input-dev-rack').value;
      const devId = document.getElementById('input-dev-id').value;
      const hostname = document.getElementById('input-dev-host').value.trim();
      const rawStartU = document.getElementById('input-dev-start-u').value.trim();
      const formFactorU = parseInt(document.getElementById('input-dev-size').value, 10);
      const model = document.getElementById('input-dev-model').value.trim() || '012_001';
      const orientation = document.getElementById('select-dev-orientation').value;
      const isSim = document.getElementById('select-dev-sim').value === 'true';

      const errorMsg = document.getElementById('msg-device-error');

      if (isNaN(formFactorU) || formFactorU < 1) {
        errorMsg.textContent = '몇U(폼팩터 크기)는 1 이상의 정수여야 합니다.';
        errorMsg.classList.remove('hidden');
        return;
      }

      const rack = this.store.getRack(rackId);
      const maxU = rack ? (rack.maxU || 52) : 52;
      const serverMaxU = rack ? (rack.serverMaxU !== undefined ? rack.serverMaxU : Math.max(1, maxU - 4)) : 48;
      const deviceType = global.DCValidator ? global.DCValidator.determineDeviceType(model, hostname) : 'server';

      const existingDevs = this.store.getDevicesForRack(rackId, true).filter(d => d.deviceId !== devId);
      let startU = rawStartU ? parseInt(rawStartU, 10) : NaN;

      // 시작 U가 비어있으면 아래(1U)부터 빈 슬롯 자동 할당
      if (isNaN(startU) || startU < 1) {
        const occupied = new Set();
        existingDevs.forEach(d => {
          for (let u = d.startU; u <= d.endU; u++) occupied.add(u);
        });
        const limitU = deviceType === 'switch' ? maxU : serverMaxU;
        const beginU = deviceType === 'switch' ? (serverMaxU + 1) : 1;

        for (let cand = beginU; cand <= limitU - formFactorU + 1; cand++) {
          let fits = true;
          for (let off = 0; off < formFactorU; off++) {
            if (occupied.has(cand + off)) {
              fits = false;
              break;
            }
          }
          if (fits) {
            startU = cand;
            break;
          }
        }

        if (isNaN(startU) || startU < 1) {
          errorMsg.textContent = `할당 불가: 랙 내에 ${formFactorU}U 연속 가용 슬롯(1~${limitU}U)이 없습니다.`;
          errorMsg.classList.remove('hidden');
          return;
        }
      }

      const endU = startU + formFactorU - 1;

      if (endU > maxU) {
        errorMsg.textContent = `할당 불가: 랙 최대 높이(${maxU}U)를 초과했습니다. (요청: ${startU}~${endU}U)`;
        errorMsg.classList.remove('hidden');
        return;
      }

      if (deviceType !== 'switch' && endU > serverMaxU) {
        errorMsg.textContent = `할당 불가: 일반 서버는 1U~${serverMaxU}U까지만 장착 가능합니다. (${serverMaxU + 1}~${maxU}U는 스위치/네트워크 전용)`;
        errorMsg.classList.remove('hidden');
        return;
      }

      for (const d of existingDevs) {
        if (Math.max(d.startU, startU) <= Math.min(d.endU, endU)) {
          errorMsg.textContent = `U 위치 중복 충돌: ${d.startU}U~${d.endU}U에 '${d.hostname || '[임시할당]'}' 장비가 이미 있습니다.`;
          errorMsg.classList.remove('hidden');
          return;
        }
      }

      if (devId) {
        this.store.updateDevice(devId, {
          hostname,
          startU,
          formFactorU,
          model,
          orientation,
          isSimulation: isSim
        });
      } else {
        this.store.addDevice({
          rackId,
          startU,
          formFactorU,
          hostname,
          model,
          orientation,
          isSimulation: isSim
        });
      }

      this.closeDeviceModal();
    }

    highlightSelectedDevice(deviceId) {
      const slots = this.container.querySelectorAll('.dc-slot-occupied');
      slots.forEach(slot => {
        if (slot.getAttribute('data-device-id') === deviceId) {
          slot.classList.add('selected');
          slot.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        } else {
          slot.classList.remove('selected');
        }
      });
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = RightElevationPanel;
  } else {
    global.DCRightElevationPanel = RightElevationPanel;
  }

})(typeof window !== 'undefined' ? window : this);
