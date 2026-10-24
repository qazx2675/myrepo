/**
 * csvHandler.js - CSV/TSV 파일 인코딩 감지(UTF-8/EUC-KR), 탭/쉼표 자동 감지, 센터이름 자동 라우팅/신규 센터 자동 생성 및 자동 할당 처리
 */
(function (global) {
  'use strict';

  class CsvHandler {
    constructor(store, validator) {
      this.store = store;
      this.validator = validator;
    }

    /**
     * File 객체를 읽어 적절한 인코딩(UTF-8 또는 EUC-KR)으로 디코딩된 텍스트 반환
     */
    async readFileAsText(file) {
      const buffer = await file.arrayBuffer();
      const bytes = new Uint8Array(buffer);

      try {
        const utf8Decoder = new TextDecoder('utf-8', { fatal: true });
        return utf8Decoder.decode(bytes);
      } catch (e) {
        // UTF-8 실패 시 EUC-KR 시도
      }

      try {
        const eucKrDecoder = new TextDecoder('euc-kr', { fatal: false });
        return eucKrDecoder.decode(bytes);
      } catch (e) {
        const fallbackDecoder = new TextDecoder();
        return fallbackDecoder.decode(bytes);
      }
    }

    /**
     * 헤더가 없는 순수 데이터 행이거나 탭(\t) 구분 텍스트인 경우에도 유연하게 파싱
     * - 양식 A (모델코드부터 시작): 모델코드 | 랙라벨(X001Y001) | hostname | 몇U | [수동지정U]
     * - 양식 B (모델코드 앞에 센터이름 열 지정): 센터이름 | 모델코드 | 랙라벨(X001Y001) | hostname | 몇U | [수동지정U]
     * - 양식 C (모델코드 내부에 접두사로 센터이름 지정): 센터이름_모델코드 | 랙라벨(X001Y001) | hostname | 몇U | [수동지정U]
     */
    parseFlexibleCsvOrTsv(csvText) {
      const trimmed = String(csvText || '').trim().replace(/^[\uFEFF\xA0]+/, '');
      if (!trimmed) return [];

      const lines = trimmed.split(/\r?\n/).filter(l => l.trim().length > 0);
      if (lines.length === 0) return [];

      const firstLine = lines[0];
      const delimiter = firstLine.includes('\t') ? '\t' : ',';
      const firstTokens = firstLine.split(delimiter).map(t => t.trim());

      // 1) 두 번째 토큰(index 1)이 X001Y001 형태 -> [모델코드, 랙라벨, hostname, 몇U, 수동지정U]
      const isRackAtCol2 = /^X\d+Y\d+$/i.test(firstTokens[1] || '');
      // 2) 세 번째 토큰(index 2)이 X001Y001 형태 -> 모델코드 앞에 센터이름 열이 있는 [센터이름, 모델코드, 랙라벨, hostname, 몇U, 수동지정U]
      const isRackAtCol3 = /^X\d+Y\d+$/i.test(firstTokens[2] || '');

      let textToParse = trimmed;
      if (isRackAtCol3) {
        const defaultHeader = ['센터이름', '모델코드', '랙라벨', 'hostname', '몇U', '수동지정U']
          .slice(0, Math.max(5, firstTokens.length))
          .join(delimiter);
        textToParse = defaultHeader + '\n' + trimmed;
      } else if (isRackAtCol2) {
        const defaultHeader = ['모델코드', '랙라벨', 'hostname', '몇U', '수동지정U']
          .slice(0, Math.max(4, firstTokens.length))
          .join(delimiter);
        textToParse = defaultHeader + '\n' + trimmed;
      }

      if (typeof Papa !== 'undefined') {
        const parseResult = Papa.parse(textToParse, {
          header: true,
          delimiter: delimiter,
          skipEmptyLines: true,
          transformHeader: (header, idx) => {
            const clean = header.trim().replace(/^[\uFEFF\xA0]+/, '');
            if (!clean && idx >= 4) return '수동지정U';
            return clean;
          }
        });
        return parseResult.data || [];
      }

      // PapaParse 미로드 환경 폴백 파서
      const allLines = textToParse.split(/\r?\n/).filter(l => l.trim().length > 0);
      const headers = allLines[0].split(delimiter).map((h, i) => h.trim() || (i >= 4 ? '수동지정U' : `col_${i}`));
      return allLines.slice(1).map(line => {
        const parts = line.split(delimiter);
        const obj = {};
        headers.forEach((h, i) => {
          obj[h] = parts[i] !== undefined ? parts[i].trim() : '';
        });
        return obj;
      });
    }

    /**
     * 검증 통과된 장비 및 랙을 스토어에 반영하고, 지정된 센터(신규 생성 포함)로 자동 이동
     */
    _applyValidatedAllocations(validation, options = {}) {
      const result = this.store.addDevicesBulk(
        validation.validList,
        validation.newlyCreatedRacks,
        {
          overwrite: !!options.overwrite,
          targetSites: validation.targetSites || []
        }
      );

      if (global.DCToast) {
        const siteMsg = result.createdSites.length > 0
          ? `[신규 센터 '${result.createdSites.join(', ')}' 자동 추가 및 이동] `
          : (result.switchedSiteId ? `['${result.switchedSiteId}' 센터로 이동] ` : '');

        if (validation.invalidList.length > 0) {
          global.DCToast.success(`${siteMsg}정상 검증된 ${validation.validList.length}대 장비가 할당되었습니다. (오류 ${validation.invalidList.length}건 제외)`);
        } else {
          global.DCToast.success(`${siteMsg}총 ${validation.validList.length}대 장비가 100% 성공적으로 할당되었습니다.`);
        }
      }
    }

    _showLoading(title = '데이터 처리 중...', subtitle = '대용량 데이터(최대 15,000대) 고속 검증 및 3D 씬을 구성하고 있습니다...') {
      if (typeof document === 'undefined') return;
      let el = document.getElementById('dc-loading-overlay');
      if (!el) {
        el = document.createElement('div');
        el.id = 'dc-loading-overlay';
        el.className = 'dc-loading-overlay';
        el.innerHTML = `
          <div class="dc-loading-box">
            <div class="dc-loading-spinner"></div>
            <div id="dc-loading-msg" class="dc-loading-msg"></div>
            <div id="dc-loading-sub" class="dc-loading-sub"></div>
          </div>
        `;
        // 파일명 등 외부 문자열은 textContent 로만 넣는다
        el.querySelector('#dc-loading-msg').textContent = title;
        el.querySelector('#dc-loading-sub').textContent = subtitle;
        document.body.appendChild(el);
      } else {
        const msg = document.getElementById('dc-loading-msg');
        if (msg) msg.textContent = title;
        const sub = document.getElementById('dc-loading-sub');
        if (sub) sub.textContent = subtitle;
        el.classList.remove('hidden');
      }
    }

    _hideLoading() {
      if (typeof document === 'undefined') return;
      const el = document.getElementById('dc-loading-overlay');
      if (el) el.classList.add('hidden');
    }

    /**
     * CSV/TSV 텍스트를 파싱하고 검증 모달을 띄우거나 스토어에 반영
     * @param {string} csvText
     * @param {Object} options { overwrite: false, isSimulation: false, autoCreateMissingRacks: true }
     */
    async importCsvText(csvText, options = {}) {
      const isSim = options.isSimulation !== undefined ? options.isSimulation : this.store.state.simulationMode;
      const autoCreate = options.autoCreateMissingRacks !== undefined ? options.autoCreateMissingRacks : true;

      this._showLoading('CSV 데이터 읽는 중...', '파일 형식 및 랙 위치를 분석하고 있습니다...');
      await new Promise(r => setTimeout(r, 40)); // 브라우저가 로딩 UI를 그릴 수 있도록 1프레임 양보

      try {
        const rows = this.parseFlexibleCsvOrTsv(csvText);
        if (!rows || rows.length === 0) {
          throw new Error('CSV/TSV 데이터에 유효한 행이 없습니다.');
        }

        const isLarge = rows.length > 500;
        if (isLarge) {
          this._showLoading(`장비 ${rows.length.toLocaleString()}대 검증 및 할당 중...`, '랙 점유율 계산 및 충돌 검사를 실행하고 있습니다...');
          await new Promise(r => setTimeout(r, 20));
        }

        const currentSite = this.store.getActiveSite();
        const existingDevices = options.overwrite ? [] : this.store.state.devices;

        const validation = this.validator.validateCsvAllocations({
          rows: rows,
          siteLayout: currentSite,
          allSites: this.store.state.sites,
          existingDevices: existingDevices,
          autoCreateMissingRacks: autoCreate,
          isSimulation: isSim,
          vendorColors: this.store.state.vendorColors
        });

        if (isLarge) {
          this._showLoading(`장비 ${rows.length.toLocaleString()}대 3D 씬 반영 중...`, '대규모 고속 템플릿 풀링(LOD)을 적용하여 씬을 생성하고 있습니다...');
          await new Promise(r => setTimeout(r, 20));
        }

        // 오류가 있는 경우 에러 리포트 모달 표시
        if (validation.invalidList.length > 0) {
          this._hideLoading();
          if (global.DCErrorReportModal) {
            global.DCErrorReportModal.show(validation, (selectedAction) => {
              if (selectedAction === 'applyValidOnly') {
                this._showLoading('장비 3D 반영 중...', '잠시만 기다려주세요...');
                setTimeout(() => {
                  this._applyValidatedAllocations(validation, options);
                  this._hideLoading();
                }, 20);
              }
            });
          } else {
            this._applyValidatedAllocations(validation, options);
          }
        } else {
          this._applyValidatedAllocations(validation, options);
          this._hideLoading();
        }

        return validation;
      } catch (err) {
        this._hideLoading();
        throw err;
      }
    }

    /**
     * CSV 파일 업로드 이벤트 핸들러
     */
    async handleFileSelect(file, options = {}) {
      try {
        this._showLoading('파일 디코딩 중...', `${file.name} 파일을 읽고 있습니다...`);
        await new Promise(r => setTimeout(r, 20));
        const text = await this.readFileAsText(file);
        return await this.importCsvText(text, options);
      } catch (err) {
        this._hideLoading();
        console.error('CSV import error:', err);
        alert(`CSV 파일 처리 중 오류가 발생했습니다: ${err.message}`);
        throw err;
      }
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = CsvHandler;
  } else {
    global.DCCsvHandler = new CsvHandler(global.DCStore, global.DCValidator);
  }

})(typeof window !== 'undefined' ? window : this);
