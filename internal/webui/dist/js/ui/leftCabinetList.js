/**
 * leftCabinetList.js - 좌측 패널: 벤더별 색상 설정(임시할당 커스텀 색상 포함) + 랙별 서버 마운트 현황 리스트 (온도/전력 제거)
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

  const VENDOR_META = [
    { key: 'HP', label: 'HP (^p)' },
    { key: 'DELL', label: 'DELL (^d)' },
    { key: 'CISCO', label: 'Cisco (^c)' },
    { key: 'CUSTOM', label: '조립서버 (^h)' },
    { key: 'SUPERMICRO', label: 'Supermicro (^m)' },
    { key: 'LENOVO', label: 'Lenovo (^l)' },
    { key: 'TEMP', label: '임시할당 (호스트 미지정)', highlight: true }
  ];

  class LeftCabinetList {
    constructor(containerEl, store) {
      this.container = containerEl;
      this.store = store;
      this.initDom();
      this.bindEvents();
      this.renderVendorLegend();
      this.renderCabinetList();
    }

    initDom() {
      this.container.innerHTML = `
        <div class="dc-panel-header">
          <div class="dc-panel-title">
            <span>📦</span> 벤더 점유율 & 랙 입고 현황
          </div>
          <button class="dc-icon-btn" id="btn-collapse-left" title="패널 접기/펼치기">◀</button>
        </div>

        <div class="dc-panel-content">
          <!-- 1. 상단뷰 벤더별 색상 및 임시할당 색상 커스터마이저 (항목 7 반영) -->
          <div class="dc-legend-section">
            <div class="dc-legend-title">🎨 벤더별 점유 색상 (클릭하여 변경)</div>
            <div class="dc-vendor-color-grid" id="dc-vendor-legend-grid">
              <!-- 동적 생성 -->
            </div>
            <div class="dc-legend-note">
              * 상단 뷰(Top View) 랙 상판 및 3D 장비 명판에 위 벤더 비중과 색상이 실시간 반영됩니다.
            </div>
          </div>

          <!-- 2. Cabinet Summary Table -->
          <div class="dc-cabinet-table-section">
            <div class="dc-section-header">
              <span class="font-bold">랙별 마운트 현황 (서버 48U / 전체 52U)</span>
              <span class="dc-counter-badge" id="lbl-cabinet-count">0</span>
            </div>

            <div class="dc-table-scroll-wrapper">
              <table class="dc-cabinet-table">
                <thead>
                  <tr>
                    <th>랙 라벨</th>
                    <th>벤더 비중</th>
                    <th class="text-right">서버U</th>
                    <th class="text-right">잔여</th>
                  </tr>
                </thead>
                <tbody id="dc-cabinet-tbody">
                  <!-- 동적 렌더링 -->
                </tbody>
              </table>
            </div>
          </div>
        </div>
      `;
    }

    renderVendorLegend() {
      const grid = document.getElementById('dc-vendor-legend-grid');
      if (!grid) return;

      const colors = this.store.state.vendorColors || {};
      grid.innerHTML = '';

      VENDOR_META.forEach(v => {
        const currentColor = colors[v.key] || '#78909c';
        const item = document.createElement('label');
        item.className = `dc-vendor-chip ${v.highlight ? 'dc-vendor-chip-temp' : ''}`;
        item.title = `${v.label} 색상 변경`;

        item.innerHTML = `
          <input type="color" class="dc-vendor-color-input" data-vendor="${v.key}" value="${escapeHtml(currentColor)}">
          <span class="dc-vendor-swatch" style="background-color: ${escapeHtml(currentColor)};"></span>
          <span class="dc-vendor-name">${v.label}</span>
        `;

        const colorInput = item.querySelector('.dc-vendor-color-input');
        const swatch = item.querySelector('.dc-vendor-swatch');
        colorInput.addEventListener('input', (e) => {
          const newHex = e.target.value;
          swatch.style.backgroundColor = newHex;
          this.store.setVendorColor(v.key, newHex);
        });

        grid.appendChild(item);
      });
    }

    bindEvents() {
      const collapseBtn = document.getElementById('btn-collapse-left');
      if (collapseBtn) {
        collapseBtn.addEventListener('click', () => {
          this.container.classList.toggle('collapsed');
          collapseBtn.textContent = this.container.classList.contains('collapsed') ? '▶' : '◀';
        });
      }

      this.store.subscribe('activeSiteChanged', () => this.renderCabinetList());
      this.store.subscribe('racksUpdated', () => this.renderCabinetList());
      this.store.subscribe('devicesUpdated', () => this.renderCabinetList());
      this.store.subscribe('rackDevicesChanged', () => this.renderCabinetList());
      this.store.subscribe('vendorColorsChanged', () => {
        this.renderVendorLegend();
        this.renderCabinetList();
      });
      this.store.subscribe('layoutLoaded', () => {
        this.renderVendorLegend();
        this.renderCabinetList();
      });
      this.store.subscribe('rackSelected', () => this.highlightSelectedRow());
      this.store.subscribe('searchQueryChanged', () => this.renderCabinetList());
    }

    renderCabinetList() {
      const tbody = document.getElementById('dc-cabinet-tbody');
      if (!tbody) return;

      const racks = this.store.getRacks();
      const countBadge = document.getElementById('lbl-cabinet-count');
      if (countBadge) countBadge.textContent = racks.length;

      tbody.innerHTML = '';

      const query = this.store.state.searchQuery || '';
      const fragment = document.createDocumentFragment();
      const maxDomRows = 300; // 2,000개 랙 환경에서도 DOM 프리징 방지를 위한 가상 캡
      let renderedCount = 0;
      let totalMatched = 0;

      for (let i = 0; i < racks.length; i++) {
        const rack = racks[i];
        const rackId = String(rack.rackId).toUpperCase();
        const devs = this.store.getDevicesForRack(rackId);

        if (query) {
          const matchRack = rackId.toLowerCase().includes(query) || (rack.name || '').toLowerCase().includes(query);
          const matchDev = devs.some(d =>
            (d.hostname || '').toLowerCase().includes(query) ||
            (d.model || '').toLowerCase().includes(query)
          );
          if (!matchRack && !matchDev) continue;
        }

        totalMatched++;
        if (renderedCount >= maxDomRows) continue;
        renderedCount++;

        const stats = this.store.getRackStats(rackId);
        const isSelected = this.store.state.selectedRackId === rackId;

        const tr = document.createElement('tr');
        tr.className = `dc-cabinet-row ${isSelected ? 'selected' : ''}`;
        tr.setAttribute('data-rack-id', rackId);

        const segHtml = stats.vendorBreakdown.map(b => {
          const pct = Math.max(8, Math.round((b.usedU / stats.maxU) * 100));
          return `<div style="width:${pct}%; background:${escapeHtml(b.color)}; height:100%;" title="${escapeHtml(b.label)}: ${escapeHtml(b.usedU)}U"></div>`;
        }).join('');

        tr.innerHTML = `
          <td>
            <div class="dc-cab-name-cell">
              <span class="dc-status-dot" style="background-color: ${escapeHtml(stats.color)};"></span>
              <strong class="dc-cab-name">${escapeHtml(rack.name || rackId)}</strong>
            </div>
          </td>
          <td>
            <div class="dc-mini-vendor-bar" title="총 ${stats.usedU}U / ${stats.maxU}U 사용 중">
              ${segHtml}
            </div>
          </td>
          <td class="text-right">
            <span class="font-bold">${stats.serverUsedU}</span><small class="text-muted">/${stats.serverMaxU}U</small>
          </td>
          <td class="text-right">
            <span class="font-mono text-highlight font-bold">${stats.serverFreeU}U</span>
          </td>
        `;

        tr.addEventListener('click', () => {
          this.store.selectRack(rackId);
        });

        fragment.appendChild(tr);
      }

      if (totalMatched > maxDomRows) {
        const infoTr = document.createElement('tr');
        infoTr.innerHTML = `
          <td colspan="4" class="text-center text-muted" style="padding: 8px; font-size: 11px;">
            전체 ${totalMatched}개 중 상위 ${maxDomRows}개 표시됨 (상단 검색창에서 랙/호스트 검색 가능)
          </td>
        `;
        fragment.appendChild(infoTr);
      }

      tbody.appendChild(fragment);
    }

    highlightSelectedRow() {
      const selectedId = this.store.state.selectedRackId;
      const rows = this.container.querySelectorAll('.dc-cabinet-row');
      rows.forEach(row => {
        if (row.getAttribute('data-rack-id') === selectedId) {
          row.classList.add('selected');
          row.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        } else {
          row.classList.remove('selected');
        }
      });
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = LeftCabinetList;
  } else {
    global.DCLeftCabinetList = LeftCabinetList;
  }

})(typeof window !== 'undefined' ? window : this);
