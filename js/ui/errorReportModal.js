/**
 * errorReportModal.js - CSV 자동 할당 충돌 및 48U 서버 범위 초과 항목 상세 리포트 모달
 */
(function (global) {
  'use strict';

  class ErrorReportModal {
    constructor() {
      this.modalEl = null;
      this.currentValidation = null;
      this.callback = null;
      this.initDom();
    }

    initDom() {
      if (document.getElementById('dc-error-report-modal')) {
        this.modalEl = document.getElementById('dc-error-report-modal');
        return;
      }

      const modal = document.createElement('div');
      modal.id = 'dc-error-report-modal';
      modal.className = 'dc-modal-overlay hidden';
      modal.innerHTML = `
        <div class="dc-modal-dialog dc-modal-large">
          <div class="dc-modal-header dc-modal-header-danger">
            <div class="dc-modal-title">
              <span class="dc-modal-icon">⚠️</span>
              <h3>CSV 서버 자동 할당 검증 리포트 (48U 서버 영역 초과 / 중복 충돌 검출)</h3>
            </div>
            <button class="dc-modal-close" id="btn-close-error-modal">&times;</button>
          </div>
          <div class="dc-modal-body">
            <div class="dc-summary-banner" id="error-summary-banner"></div>

            <div class="dc-table-container">
              <table class="dc-report-table">
                <thead>
                  <tr>
                    <th style="width: 60px;">CSV 행</th>
                    <th style="width: 85px;">센터</th>
                    <th style="width: 85px;">모델코드</th>
                    <th style="width: 85px;">랙 라벨</th>
                    <th style="width: 110px;">요청 U</th>
                    <th style="width: 120px;">hostname</th>
                    <th>할당 불가 사유 상세</th>
                  </tr>
                </thead>
                <tbody id="error-report-tbody"></tbody>
              </table>
            </div>

            <div class="dc-report-note">
              * 랙 기본 규격은 <strong>52U (서버 장착 허용: 1U~48U / 스위치·네트워크 영역: 49U~52U)</strong>입니다.<br>
              * <strong>[정상 항목만 반영]</strong> 클릭 시 허용 범위 내의 정상 장비들은 즉시 랙에 마운트됩니다.
            </div>
          </div>
          <div class="dc-modal-footer">
            <button class="dc-btn dc-btn-secondary" id="btn-download-error-csv">
              📥 할당 불가 내역 CSV 다운로드
            </button>
            <button class="dc-btn dc-btn-secondary" id="btn-download-allocated-hosts-csv" style="border-color: #388bfd88; color: #79c0ff;">
              💾 현재 할당된 모든 호스트 CSV 다운로드
            </button>
            <button class="dc-btn dc-btn-outline" id="btn-cancel-error-modal">
              취소
            </button>
            <button class="dc-btn dc-btn-primary" id="btn-apply-valid-only">
              ✅ 정상 항목만 반영 (<span id="span-valid-count">0</span>대)
            </button>
          </div>
        </div>
      `;

      document.body.appendChild(modal);
      this.modalEl = modal;

      // 모달 내부에서 마우스 휠 스크롤이나 드래그 시 뒤쪽 3D 데이터센터 시점이 절대 확대/이동되지 않도록 이벤트 전파 차단
      modal.addEventListener('wheel', (e) => {
        e.stopPropagation();
      }, { passive: true });
      modal.addEventListener('pointerdown', (e) => {
        e.stopPropagation();
      });
      modal.addEventListener('pointermove', (e) => {
        e.stopPropagation();
      });

      document.getElementById('btn-close-error-modal').addEventListener('click', () => this.hide());
      document.getElementById('btn-cancel-error-modal').addEventListener('click', () => {
        this.hide();
        if (this.callback) this.callback('cancel');
      });

      document.getElementById('btn-apply-valid-only').addEventListener('click', () => {
        this.hide();
        if (this.callback) this.callback('applyValidOnly');
      });

      document.getElementById('btn-download-error-csv').addEventListener('click', () => {
        this.downloadErrorCsv();
      });

      document.getElementById('btn-download-allocated-hosts-csv').addEventListener('click', () => {
        this.downloadAllocatedHostsCsv();
      });
    }

    show(validation, callback) {
      this.initDom();
      this.currentValidation = validation;
      this.callback = callback;

      const { summary, invalidList } = validation;

      const bannerEl = document.getElementById('error-summary-banner');
      bannerEl.innerHTML = `
        <div class="dc-banner-stat dc-banner-stat-total">전체 요청 <strong>${summary.total}</strong>대</div>
        <div class="dc-banner-stat dc-banner-stat-success">정상 할당 가능 <strong>${summary.successCount}</strong>대</div>
        <div class="dc-banner-stat dc-banner-stat-error">할당 불가 (48U 초과/충돌) <strong>${summary.failCount}</strong>대</div>
      `;

      document.getElementById('span-valid-count').textContent = summary.successCount;
      const applyBtn = document.getElementById('btn-apply-valid-only');
      applyBtn.disabled = summary.successCount === 0;

      const tbody = document.getElementById('error-report-tbody');
      tbody.innerHTML = '';

      invalidList.forEach(item => {
        const tr = document.createElement('tr');
        tr.className = 'dc-table-row-error';
        tr.innerHTML = `
          <td class="text-center font-bold">#${item.rowNum}</td>
          <td class="text-center">${this.escapeHtml(item.dcName || 'SITE-A')}</td>
          <td class="text-center">${this.escapeHtml(item.model || '-')}</td>
          <td class="text-center font-bold text-highlight">${this.escapeHtml(item.rackId)}</td>
          <td class="text-center">${this.escapeHtml(item.uRange)}</td>
          <td><strong>${this.escapeHtml(item.hostname)}</strong></td>
          <td class="dc-reason-cell">${this.escapeHtml(item.reason)}</td>
        `;
        tbody.appendChild(tr);
      });

      this.modalEl.classList.remove('hidden');
    }

    hide() {
      if (this.modalEl) {
        this.modalEl.classList.add('hidden');
      }
    }

    downloadErrorCsv() {
      if (!this.currentValidation || !this.currentValidation.invalidList) return;

      const headers = ['CSV행번호', '센터이름', '모델코드', '랙라벨', 'hostname', '요청U범위', '실패사유'];
      const rows = this.currentValidation.invalidList.map(item => [
        item.rowNum,
        `"${(item.dcName || 'SITE-A').replace(/"/g, '""')}"`,
        `"${(item.model || '').replace(/"/g, '""')}"`,
        `"${(item.rackId || '').replace(/"/g, '""')}"`,
        `"${(item.hostname || '').replace(/"/g, '""')}"`,
        `"${(item.uRange || '').replace(/"/g, '""')}"`,
        `"${(item.reason || '').replace(/"/g, '""')}"`
      ]);

      const csvContent = '\uFEFF' + [headers.join(','), ...rows.map(r => r.join(','))].join('\r\n');
      const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `allocation_errors_${Date.now()}.csv`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    }

    /**
     * 현재 할당된 모든 호스트(기존 마운트된 장비 + 이번 검증에서 정상 할당 가능한 호스트 포함)를 CSV로 다운로드
     */
    downloadAllocatedHostsCsv() {
      const store = global.DCStore;
      const existingDevices = (store && store.state && Array.isArray(store.state.devices)) ? store.state.devices : [];
      const validStaged = (this.currentValidation && Array.isArray(this.currentValidation.validList))
        ? this.currentValidation.validList
        : [];

      // 중복 방지를 위해 기존 장비 + 이번 검증의 정상 할당 장비를 병합하여 CSV 생성
      const mergedMap = new Map();
      existingDevices.forEach(d => {
        const key = `${d.siteId || 'SITE-A'}|${d.rackId}|${d.startU}|${d.hostname || ''}`;
        mergedMap.set(key, d);
      });
      validStaged.forEach(d => {
        const key = `${d.siteId || 'SITE-A'}|${d.rackId}|${d.startU}|${d.hostname || ''}`;
        if (!mergedMap.has(key)) {
          mergedMap.set(key, d);
        }
      });

      const list = Array.from(mergedMap.values());
      const headers = ['센터이름', '모델코드', '랙라벨', 'hostname', '몇U', '수동지정U', '방향', '시뮬레이션'];
      const rows = list.map(d => [
        `"${(d.siteId || (store && store.state ? store.state.activeSiteId : 'SITE-A')).replace(/"/g, '""')}"`,
        `"${(d.model || '').replace(/"/g, '""')}"`,
        d.rackId,
        `"${(d.hostname || '').replace(/"/g, '""')}"`,
        d.formFactorU || 1,
        d.startU || 1,
        d.orientation || 'front',
        d.isSimulation ? 'Y' : 'N'
      ]);

      const csvContent = '\uFEFF' + [headers.join(','), ...rows.map(r => r.join(','))].join('\r\n');
      const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `allocated_hosts_${list.length}ea_${Date.now()}.csv`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);

      if (global.DCToast) {
        global.DCToast.success(`현재 할당된 호스트 총 ${list.length}대 CSV 다운로드 완료`);
      }
    }

    escapeHtml(str) {
      if (!str) return '';
      return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = ErrorReportModal;
  } else {
    if (typeof window !== 'undefined') {
      const initModal = () => {
        if (!global.DCErrorReportModal) {
          global.DCErrorReportModal = new ErrorReportModal();
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
