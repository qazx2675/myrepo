/**
 * validator.js - U 점유 계산, 52U(서버 1~48U / 네트워크 49~52U) 검증, 호스트네임 접두사 벤더 판별 및 CSV/TSV 자동 할당 모듈
 */
(function (global) {
  'use strict';

  /**
   * 기본 벤더 목록 및 기본 색상 정의
   * 호스트네임 규칙:
   * - ^p : HP (HPE)
   * - ^d : DELL
   * - ^c : Cisco
   * - ^h : 조립서버
   * - ^m : Supermicro
   * - ^l : Lenovo
   * - 호스트네임 미지정(공란) : 임시할당 (TEMP)
   */
  const DEFAULT_VENDOR_COLORS = {
    HP: '#00b388',         // HPE 시그니처 그린/틸
    DELL: '#007db8',       // DELL 블루
    CISCO: '#00bceb',      // Cisco 시안
    CUSTOM: '#ff9800',     // 조립서버 오렌지
    SUPERMICRO: '#4caf50', // Supermicro 그린
    LENOVO: '#e53935',     // Lenovo 레드
    TEMP: '#ab47bc',       // 호스트 없는 임시할당 (사용자 지정 색상 가능)
    OTHER: '#78909c'       // 기타 벤더
  };

  const VENDOR_LABELS = {
    HP: 'HP (^p)',
    DELL: 'DELL (^d)',
    CISCO: 'Cisco (^c)',
    CUSTOM: '조립서버 (^h)',
    SUPERMICRO: 'Supermicro (^m)',
    LENOVO: 'Lenovo (^l)',
    TEMP: '임시할당 (호스트 미지정)',
    OTHER: '기타'
  };

  /**
   * 호스트네임 첫 글자 규칙(^p, ^d, ^c, ^h, ^m, ^l)에 따라 벤더 키 판별
   * @param {string} hostname
   * @param {string} model
   * @returns {'HP'|'DELL'|'CISCO'|'CUSTOM'|'SUPERMICRO'|'LENOVO'|'TEMP'|'OTHER'}
   */
  function determineVendor(hostname, model = '') {
    const h = String(hostname || '').trim();
    if (!h || h === '(임시할당)' || h.startsWith('[임시')) {
      return 'TEMP';
    }
    const lower = h.toLowerCase();
    if (lower.startsWith('p')) return 'HP';
    if (lower.startsWith('d')) return 'DELL';
    if (lower.startsWith('c')) return 'CISCO';
    if (lower.startsWith('h')) return 'CUSTOM';
    if (lower.startsWith('m')) return 'SUPERMICRO';
    if (lower.startsWith('l')) return 'LENOVO';

    // 접두사에 해당하지 않으면 모델명 보조 판별
    const m = String(model || '').toLowerCase();
    if (m.includes('hp') || m.includes('dl3')) return 'HP';
    if (m.includes('dell') || m.includes('r7') || m.includes('r6')) return 'DELL';
    if (m.includes('cisco') || m.includes('switch')) return 'CISCO';
    if (m.includes('supermicro')) return 'SUPERMICRO';
    if (m.includes('lenovo')) return 'LENOVO';

    return 'OTHER';
  }

  /**
   * 점유하는 U 슬롯 번호 배열 계산 (startU부터 startU + formFactorU - 1까지)
   */
  function calculateOccupiedSlots(startU, formFactorU) {
    const s = parseInt(startU, 10);
    const f = parseInt(formFactorU, 10);
    if (isNaN(s) || isNaN(f) || s < 1 || f < 1) {
      return [];
    }
    const slots = [];
    for (let u = s; u < s + f; u++) {
      slots.push(u);
    }
    return slots;
  }

  /**
   * 두 U 구간 [startA, endA]와 [startB, endB]가 겹치는지 검사하고 겹치는 슬롯 배열 반환
   */
  function getOverlapSlots(startA, endA, startB, endB) {
    const overlapStart = Math.max(startA, startB);
    const overlapEnd = Math.min(endA, endB);
    if (overlapStart <= overlapEnd) {
      const slots = [];
      for (let u = overlapStart; u <= overlapEnd; u++) {
        slots.push(u);
      }
      return slots;
    }
    return null;
  }

  /**
   * 모델명 또는 호스트네임으로 장비 유형(server | switch) 판별
   */
  function determineDeviceType(model, hostname = '') {
    const m = String(model || '').toLowerCase();
    const h = String(hostname || '').toLowerCase();
    if (
      m.includes('switch') ||
      m.includes('스위치') ||
      m.includes('네트워크') ||
      m.includes('tor') ||
      h.startsWith('csw') ||
      h.includes('switch') ||
      h.includes('-sw')
    ) {
      return 'switch';
    }
    return 'server';
  }

  /**
   * 문자열에서 정수 추출 (예: "4U" -> 4, " 15 " -> 15)
   */
  function parseIntegerWithUnit(val) {
    if (val === undefined || val === null) return NaN;
    const str = String(val).trim();
    if (!str || str.toUpperCase() === 'AUTO') return NaN;
    const cleaned = str.replace(/u$/i, '').trim();
    return parseInt(cleaned, 10);
  }

  /**
   * 랙 ID에서 X, Y 번호 파싱 (예: X001Y002 -> { xNum: 1, yNum: 2 }, X029Y001 -> { xNum: 29, yNum: 1 })
   */
  function parseRackXY(rackId) {
    const match = String(rackId || '').trim().match(/^X(\d+)Y(\d+)$/i);
    if (!match) return null;
    return {
      xNum: parseInt(match[1], 10),
      yNum: parseInt(match[2], 10)
    };
  }

  /**
   * CSV의 센터이름 열 또는 모델코드 앞 접두사(예: "판교센터_012_001", "SITE-B:012_001", "센터2/012_001")에서
   * 센터 이름과 순수 모델코드를 분리 추출
   */
  function parseCenterAndModel(rawCenter, rawModel, fallbackSiteId = 'SITE-A') {
    let centerName = String(rawCenter || '').trim();
    let modelCode = String(rawModel || '').trim();

    // 모델코드 앞에 센터이름이 붙어있는 경우 자동 분리:
    // 예: "판교_012_001", "SITE-B_012_001", "센터2:012_001", "센터2/012_001", "[판교]012_001"
    // 단, 순수 숫자로만 이루어진 표준 모델코드("012_001")는 분리하지 않음
    if (!centerName && modelCode) {
      const bracketMatch = modelCode.match(/^\[([^\]]+)\]\s*(.+)$/);
      if (bracketMatch) {
        centerName = bracketMatch[1].trim();
        modelCode = bracketMatch[2].trim();
      } else {
        const prefixMatch = modelCode.match(/^([^\d_:/\s-][^_:/]*?)[_:/](.+)$/);
        if (prefixMatch) {
          centerName = prefixMatch[1].trim();
          modelCode = prefixMatch[2].trim();
        } else {
          // 숫자로 시작하더라도 "1센터_012_001" 처럼 뒤에 "012_001" 패턴이 오는 경우 분리
          const modelTailMatch = modelCode.match(/^(.+?)[_:/](\d{2,4}_\d{2,4})$/);
          if (modelTailMatch && !/^\d+$/.test(modelTailMatch[1])) {
            centerName = modelTailMatch[1].trim();
            modelCode = modelTailMatch[2].trim();
          }
        }
      }
    }

    return {
      centerName: centerName || fallbackSiteId || 'SITE-A',
      hasExplicitCenter: !!centerName,
      modelCode: modelCode || '012_001'
    };
  }

  /**
   * CSV/TSV 장비 할당 검증 함수
   * - 지원 양식 1 (센터 포함 양식): [센터이름] | 모델코드 | 랙라벨 | hostname | 몇U | [수동지정U(선택)]
   * - 지원 양식 2 (모델코드 앞 센터 접두사): "센터명_012_001" | 랙라벨 | hostname | 몇U | [수동지정U(선택)]
   * - 지원 양식 3 (기본 실무 양식): 모델코드 | 랙라벨 | hostname | 몇U | [수동지정U(선택)]
   * - hostname이 비어있으면 임시할당(TEMP)으로 정상 허용
   * - 랙 기본 높이 52U, 일반 서버 장착 범위는 1U ~ 48U (maxU - 4), 49U~52U는 스위치/네트워크 전용
   */
  function validateCsvAllocations({
    rows = [],
    siteLayout = null,
    allSites = [],
    existingDevices = [],
    autoCreateMissingRacks = true,
    isSimulation = false,
    vendorColors = DEFAULT_VENDOR_COLORS
  }) {
    const validList = [];
    const invalidList = [];
    const newlyCreatedRacks = [];
    const targetSitesSet = new Set();

    const defaultSiteId = (siteLayout && siteLayout.siteId) || 'SITE-A';

    // 기존 사이트 이름/ID 매핑 헬퍼 (대소문자/공백 무시 매칭)
    const resolveSiteId = (rawCenterName) => {
      const clean = String(rawCenterName || '').trim();
      if (!clean) return defaultSiteId;
      const upper = clean.toUpperCase();
      if (Array.isArray(allSites)) {
        const matched = allSites.find(s =>
          String(s.siteId).toUpperCase() === upper ||
          String(s.name || '').toUpperCase() === upper ||
          (upper === 'SITE A' && String(s.siteId).toUpperCase() === 'SITE-A')
        );
        if (matched) return matched.siteId;
      }
      return clean;
    };

    // 사이트+랙 복합 키 룩업 맵 구성 (`${siteId}::${rackId}` -> rack)
    const rackMap = new Map();
    if (Array.isArray(allSites) && allSites.length > 0) {
      allSites.forEach(s => {
        if (Array.isArray(s.racks)) {
          s.racks.forEach(r => {
            rackMap.set(`${s.siteId}::${String(r.rackId).toUpperCase()}`, { ...r, siteId: s.siteId });
          });
        }
      });
    } else if (siteLayout && Array.isArray(siteLayout.racks)) {
      siteLayout.racks.forEach(r => {
        rackMap.set(`${defaultSiteId}::${String(r.rackId).toUpperCase()}`, { ...r, siteId: defaultSiteId });
      });
    }

    // 사이트+랙별 점유 상태 추적 맵 (`${siteId}::${rackId}` -> [{ deviceId, hostname, startU, endU, isSim }])
    const occupancyByRack = new Map();
    existingDevices.forEach(d => {
      const sId = d.siteId || defaultSiteId;
      const rId = String(d.rackId).toUpperCase();
      const compositeKey = `${sId}::${rId}`;
      if (!occupancyByRack.has(compositeKey)) {
        occupancyByRack.set(compositeKey, []);
      }
      occupancyByRack.get(compositeKey).push({
        deviceId: d.deviceId,
        hostname: d.hostname || '[임시할당]',
        startU: d.startU,
        endU: d.endU,
        formFactorU: d.formFactorU,
        isSimulation: d.isSimulation || false,
        source: 'existing'
      });
    });

    rows.forEach((row, index) => {
      const rowNum = index + 1;

      const keys = Object.keys(row);
      const rawDc = row.센터이름 || row.센터명 || row.센터 || row.데이터센터위치 || row.데이터센터 || row.dc_location || row.site || row.site_id || row.center || '';
      const rawRack = row.랙라벨 || row.랙위치 || row.rack_label || row.rack_location || row.rack || row.rack_id || row.cabinet || row['랙'] || '';
      const rawModel = row.모델코드 || row.모델 || row.model_code || row.model || row.device_model || row['모델명'] || '';
      const rawHostname = row.hostname !== undefined ? row.hostname : (row.호스트네임 !== undefined ? row.호스트네임 : (row.host !== undefined ? row.host : (row['호스트명'] !== undefined ? row['호스트명'] : '')));

      // 몇U (폼팩터 크기) vs 수동 지정 시작U 구분
      let rawFormFactor = row.몇U || row.폼팩터 || row.form_factor || row.u_size || row.height || row.size || row['크기'] || '';
      let rawStartU = row.수동지정U || row.시작U || row.랙내위치 || row.장착위치 || row.u_position || row.start_u || row.slot || row['u위치'] || '';

      const knownHeaders = [
        '센터이름', '센터명', '센터', '데이터센터위치', '데이터센터', 'dc_location', 'site', 'site_id', 'center',
        '랙위치', '호스트네임', '폼팩터', '모델', '방향', '시뮬레이션', '모델코드', '랙라벨', 'hostname', '몇U'
      ];
      if (!rawStartU && keys.length >= 5) {
        const extraKey = keys.find((k, idx) => idx >= 4 && !knownHeaders.includes(k));
        if (extraKey) {
          rawStartU = row[extraKey];
        }
      }

      const rawOrientation = row.방향 || row.orientation || row.face || row.view || row['앞뒤'] || 'front';

      // 센터이름과 모델코드 분리 파싱
      const parsedCM = parseCenterAndModel(rawDc, rawModel, defaultSiteId);
      const resolvedSiteId = resolveSiteId(parsedCM.centerName);
      if (parsedCM.hasExplicitCenter) {
        targetSitesSet.add(resolvedSiteId);
      }

      const dcName = resolvedSiteId;
      const rackId = String(rawRack).trim().toUpperCase();
      const modelCode = parsedCM.modelCode;
      const cleanHost = String(rawHostname || '').trim();
      const isTemp = cleanHost === '';

      let formFactorU = parseIntegerWithUnit(rawFormFactor);
      let manualStartU = parseIntegerWithUnit(rawStartU);

      let orientation = 'front';
      const oriStr = String(rawOrientation).trim().toLowerCase();
      if (oriStr === 'back' || oriStr === 'rear' || oriStr === '뒤' || oriStr === 'b') {
        orientation = 'back';
      }

      const deviceType = determineDeviceType(modelCode, cleanHost);
      const vendor = determineVendor(cleanHost, modelCode);

      // 1단계: 필수값 검증 (랙라벨 및 몇U 폼팩터 크기 필수, hostname은 공란 허용!)
      if (!rackId || isNaN(formFactorU) || formFactorU < 1) {
        invalidList.push({
          rowNum,
          dcName,
          rackId: rackId || '(누락)',
          uRange: `크기: ${rawFormFactor || '?'}U`,
          hostname: cleanHost || '[임시할당]',
          model: modelCode,
          reason: `[할당 불가: 필수 입력값 누락 또는 폼팩터(몇U) 크기 오류 (센터: '${dcName}', 랙라벨: '${rackId || '누락'}', 몇U: '${rawFormFactor || '누락'}')]`,
          rawRow: row
        });
        return;
      }

      // 2단계: 해당 센터(dcName) 내 랙 존재 여부 검증 (미존재 시 X=행, Y=열 규칙 및 4칸 간격으로 자동 배치)
      const compositeKey = `${dcName}::${rackId}`;
      let rack = rackMap.get(compositeKey);
      if (!rack) {
        if (autoCreateMissingRacks) {
          let rowIdx = 1;
          let colIdx = 1;
          const xy = parseRackXY(rackId);
          if (xy) {
            rowIdx = xy.xNum;
            colIdx = xy.yNum;
          } else {
            rowIdx = 1;
            colIdx = rackMap.size + 1;
          }

          rack = {
            siteId: dcName,
            rackId: rackId,
            name: rackId,
            rackX: rowIdx,
            rackY: colIdx,
            gridX: 3 + (colIdx - 1),
            gridZ: 3 + (rowIdx - 1) * 4,
            widthTiles: 1,
            depthTiles: 2,
            maxU: 52,
            serverMaxU: 48,
            rotationDeg: 0
          };
          rackMap.set(compositeKey, rack);
          newlyCreatedRacks.push(rack);
        } else {
          invalidList.push({
            rowNum,
            dcName,
            rackId,
            uRange: `${rawStartU || 'AUTO'} (${formFactorU}U)`,
            hostname: cleanHost || '[임시할당]',
            model: modelCode,
            reason: `[할당 불가: 센터 '${dcName}'에 존재하지 않는 랙 위치 ('${rackId}')]`,
            rawRow: row
          });
          return;
        }
      }

      const maxU = rack.maxU || 52;
      const serverMaxU = rack.serverMaxU !== undefined ? rack.serverMaxU : Math.max(1, maxU - 4);
      const effectiveMaxU = deviceType === 'switch' ? maxU : serverMaxU;

      // 3단계: 장착 위치(startU) 결정 — 수동 지정값이 있으면 반영, 빈열이면 1U(아래)부터 자동 할당
      let startU = manualStartU;
      let isAutoAllocated = false;

      const currentRackDevices = occupancyByRack.get(compositeKey) || [];
      const occupiedSet = new Set();
      currentRackDevices.forEach(d => {
        for (let u = d.startU; u <= d.endU; u++) {
          occupiedSet.add(u);
        }
      });

      if (isNaN(startU) || startU < 1) {
        const searchStart = deviceType === 'switch' ? (serverMaxU + 1) : 1;
        const searchEnd = effectiveMaxU - formFactorU + 1;

        for (let candidateU = searchStart; candidateU <= searchEnd; candidateU++) {
          let fits = true;
          for (let offset = 0; offset < formFactorU; offset++) {
            if (occupiedSet.has(candidateU + offset)) {
              fits = false;
              break;
            }
          }
          if (fits) {
            startU = candidateU;
            isAutoAllocated = true;
            break;
          }
        }

        if ((isNaN(startU) || startU < 1) && deviceType === 'switch') {
          for (let candidateU = 1; candidateU <= maxU - formFactorU + 1; candidateU++) {
            let fits = true;
            for (let offset = 0; offset < formFactorU; offset++) {
              if (occupiedSet.has(candidateU + offset)) {
                fits = false;
                break;
              }
            }
            if (fits) {
              startU = candidateU;
              isAutoAllocated = true;
              break;
            }
          }
        }

        if (isNaN(startU) || startU < 1) {
          const limitDesc = deviceType === 'switch'
            ? `랙 최대 높이(${maxU}U)`
            : `서버 장착 허용 범위(1U~${serverMaxU}U, 상단 ${serverMaxU + 1}~${maxU}U는 스위치/네트워크 전용)`;
          invalidList.push({
            rowNum,
            dcName,
            rackId,
            uRange: `${formFactorU}U (자동할당 불가)`,
            hostname: cleanHost || '[임시할당]',
            model: modelCode,
            reason: `[할당 불가: 센터 '${dcName}' 랙 '${rackId}'의 ${limitDesc} 내에 ${formFactorU}U 연속 빈 슬롯이 없습니다 (용량 초과)]`,
            rawRow: row
          });
          return;
        }
      }

      const endU = startU + formFactorU - 1;
      const uRangeStr = `${startU}U${formFactorU > 1 ? `~${endU}U` : ''} (${formFactorU}U${isAutoAllocated ? ', 자동' : ', 수동'})`;

      // 4단계: 수동 지정 시 랙 최대 높이(maxU) 및 서버 장착 제한(serverMaxU=48U) 초과 검증
      if (endU > maxU) {
        invalidList.push({
          rowNum,
          dcName,
          rackId,
          uRange: uRangeStr,
          hostname: cleanHost || '[임시할당]',
          model: modelCode,
          reason: `[할당 불가: 랙 최대 높이 초과 (요청 위치: ${startU}U~${endU}U, 랙 최대: ${maxU}U)]`,
          rawRow: row
        });
        return;
      }

      if (deviceType !== 'switch' && endU > serverMaxU) {
        invalidList.push({
          rowNum,
          dcName,
          rackId,
          uRange: uRangeStr,
          hostname: cleanHost || '[임시할당]',
          model: modelCode,
          reason: `[할당 불가: 서버 장착 가능 범위(1U~${serverMaxU}U) 초과 — ${serverMaxU + 1}U~${maxU}U 영역은 스위치/네트워크 전용입니다 (요청: ${startU}U~${endU}U)]`,
          rawRow: row
        });
        return;
      }

      // 5단계: U 슬롯 중복 충돌 검증
      let collisionItem = null;
      for (const dev of currentRackDevices) {
        const overlap = getOverlapSlots(dev.startU, dev.endU, startU, endU);
        if (overlap && overlap.length > 0) {
          collisionItem = {
            target: dev,
            overlapSlots: overlap
          };
          break;
        }
      }

      if (collisionItem) {
        const overlapStr = collisionItem.overlapSlots.map(u => `${u}U`).join(', ');
        if (collisionItem.target.source === 'csv') {
          invalidList.push({
            rowNum,
            dcName,
            rackId,
            uRange: uRangeStr,
            hostname: cleanHost || '[임시할당]',
            model: modelCode,
            reason: `[할당 불가: CSV 내 중복 할당 - CSV ${collisionItem.target.rowNum}행의 '${collisionItem.target.hostname}'(${collisionItem.target.startU}U~${collisionItem.target.endU}U) 장비와 ${overlapStr} 위치가 겹칩니다]`,
            rawRow: row
          });
        } else {
          invalidList.push({
            rowNum,
            dcName,
            rackId,
            uRange: uRangeStr,
            hostname: cleanHost || '[임시할당]',
            model: modelCode,
            reason: `[할당 불가: U 위치 중복 충돌 - ${rackId} 랙의 ${overlapStr}에 이미 '${collisionItem.target.hostname}'(${collisionItem.target.startU}U~${collisionItem.target.endU}U) 장비가 마운트되어 있습니다]`,
            rawRow: row
          });
        }
        return;
      }

      const displayHostname = isTemp ? '' : cleanHost;
      const colorTag = (vendorColors && vendorColors[vendor]) || DEFAULT_VENDOR_COLORS[vendor] || '#78909c';

      const newDev = {
        deviceId: `DEV-${Date.now()}-${index}-${Math.floor(Math.random() * 10000)}`,
        siteId: dcName,
        rackId: rackId,
        startU: startU,
        endU: endU,
        formFactorU: formFactorU,
        hostname: displayHostname,
        isTemp: isTemp,
        vendor: vendor,
        model: modelCode,
        deviceType: deviceType,
        orientation: orientation,
        isSimulation: isSimulation,
        colorTag: colorTag,
        createdAt: new Date().toISOString(),
        rowNum: rowNum,
        source: 'csv'
      };

      if (!occupancyByRack.has(compositeKey)) {
        occupancyByRack.set(compositeKey, []);
      }
      occupancyByRack.get(compositeKey).push({
        ...newDev,
        hostname: displayHostname || '[임시할당]'
      });

      validList.push(newDev);
    });

    return {
      validList,
      invalidList,
      newlyCreatedRacks,
      targetSites: Array.from(targetSitesSet),
      summary: {
        total: rows.length,
        successCount: validList.length,
        failCount: invalidList.length
      }
    };
  }

  const DCValidator = {
    DEFAULT_VENDOR_COLORS,
    VENDOR_LABELS,
    determineVendor,
    calculateOccupiedSlots,
    getOverlapSlots,
    determineDeviceType,
    parseIntegerWithUnit,
    parseRackXY,
    parseCenterAndModel,
    validateCsvAllocations
  };

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = DCValidator;
  } else {
    global.DCValidator = DCValidator;
  }

})(typeof window !== 'undefined' ? window : this);
