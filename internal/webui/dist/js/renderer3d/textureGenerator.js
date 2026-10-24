/**
 * textureGenerator.js - HTML5 Canvas 기반 고해상도 프로시저럴 텍스처 생성기
 * - 서버 전면/후면: 장착 U 위치([12U | 1U]), 벤더/모델코드, 호스트네임을 크고 선명하게 표시
 * - 랙 상판(Top Cap): 상단뷰에서 랙당 벤더별 서버 점유율(캡처 4 스타일)을 스택 컬러 바로 표시
 * - 랙 기둥(Post) U 눈금 및 바닥 X/Y 축 라벨 텍스처 생성
 */
(function (global) {
  'use strict';

  class TextureGenerator {
    constructor() {
      this.cache = new Map();
      this._detailedServerKeys = [];
      this.MAX_DETAILED_TEXTURES = 450;
    }

    /**
     * 캐시 초기화 (벤더 색상 변경 시 호출)
     */
    clearDeviceAndRoofCache() {
      for (const key of Array.from(this.cache.keys())) {
        if (key.startsWith('srv_') || key.startsWith('sw_') || key.startsWith('roof_')) {
          const tex = this.cache.get(key);
          if (tex) {
            if (tex.dispose) tex.dispose();
            if (tex.image && tex.image.width) {
              tex.image.width = 1;
              tex.image.height = 1;
            }
          }
          this.cache.delete(key);
        }
      }
      this._detailedServerKeys = [];
    }

    /**
     * 글자 외곽선(크리스프 블랙 스트로크) + 고대비 채우기로 어떤 거리/각도에서도 진하고 선명하게 텍스트 렌더링
     */
    _drawBoldText(ctx, text, x, y, maxW, strokeWidth = 6) {
      ctx.save();
      ctx.lineJoin = 'round';
      ctx.miterLimit = 2;
      ctx.strokeStyle = '#000000';
      ctx.lineWidth = strokeWidth;
      if (maxW !== undefined) {
        ctx.strokeText(text, x, y, maxW);
        ctx.fillText(text, x, y, maxW);
      } else {
        ctx.strokeText(text, x, y);
        ctx.fillText(text, x, y);
      }
      ctx.restore();
    }

    /**
     * 캔버스 텍스처를 Three.js CanvasTexture로 변환 및 캐싱
     */
    getOrCreateTexture(key, drawFn, width = 1024, height = 256) {
      if (this.cache.has(key)) {
        return this.cache.get(key);
      }

      if (typeof document === 'undefined') return null;

      // 상세 서버 텍스처 LRU 메모리 관리 (최대 450개로 제한하여 메모리 누수 원천 차단)
      // 주의: oldTex.image = null 로 설정하면 아직 메쉬에 바인딩된 텍스처를 Three.js WebGLTextures.safeSetTexture2D 가 읽을 때
      // Cannot read properties of null (reading 'complete') 크래시가 발생하므로 절대 image 참조를 null 로 만들지 않는다!
      const isDetailedServerTex = key.startsWith('srv_front_v5_') || key.startsWith('srv_back_v5_');
      if (isDetailedServerTex) {
        if (this._detailedServerKeys.length >= this.MAX_DETAILED_TEXTURES) {
          const evictKey = this._detailedServerKeys.shift();
          if (evictKey && this.cache.has(evictKey)) {
            const oldTex = this.cache.get(evictKey);
            if (oldTex) {
              if (oldTex.dispose) oldTex.dispose();
            }
            this.cache.delete(evictKey);
          }
        }
        this._detailedServerKeys.push(key);
      }

      const canvas = document.createElement('canvas');
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext('2d');

      drawFn(ctx, width, height);

      let texture = null;
      if (typeof THREE !== 'undefined') {
        texture = new THREE.CanvasTexture(canvas);
        texture.anisotropy = 16;
        texture.generateMipmaps = true;
        // 밉맵 단계 간 블렌딩으로 인한 글씨 흐림을 방지하기 위해 LinearMipmapNearestFilter 적용
        texture.minFilter = THREE.LinearMipmapNearestFilter;
        texture.magFilter = THREE.LinearFilter;
      } else {
        texture = { canvas };
      }

      this.cache.set(key, texture);
      return texture;
    }

    /**
     * 1. 서버 전면 고해상도 텍스처 (2의 거듭제곱 POT 규격: 1U=2048x256, 2U+=2048x512 -> Mipmap + 16x Anisotropy 완벽 작동!)
     * - 대규모 환경(서버 15,000대 / 랙 600개) 최적화:
     *   (A) 전체 뷰(massiveScale=true & isDetailed=false): 30,000장을 개별 생성하지 않고 벤더/모델/폼팩터별 공통 템플릿 텍스처(약 12장)를 공유 (메모리 10GB -> 150MB 이하, 0.2초 로드)
     *   (B) 랙 선택/상세 뷰(isDetailed=true 또는 일반 규모): 실제 호스트명(dsvr...) 및 정확한 시작U를 100% 반영하여 고해상도로 렌더링
     */
    getServerFrontTexture(device, vendorColor = '#4caf50', massiveScale = false, isDetailed = false) {
      const hostname = device.hostname || '';
      const isTemp = !hostname || device.isTemp;
      const model = device.model || '012_001';
      const vendor = device.vendor || 'OTHER';
      const startU = device.startU || 1;
      const endU = device.endU || startU;
      const formFactorU = device.formFactorU || 1;
      const isSim = !!device.isSimulation;
      const useTemplate = massiveScale && !isDetailed;

      const key = useTemplate
        ? `srv_front_tpl_v5_${formFactorU}_${vendor}_${model}_${isSim}_${vendorColor}`
        : `srv_front_v5_${startU}_${formFactorU}_${vendor}_${model}_${hostname}_${isSim}_${vendorColor}`;

      const width = massiveScale ? 1024 : 2048;
      const height = formFactorU >= 2 ? 512 : 256;
      const scaleX = width / 2048;
      const scaleY = height / (formFactorU >= 2 ? 512 : 256);

      return this.getOrCreateTexture(key, (ctx, w, h) => {
        ctx.imageSmoothingEnabled = true;
        ctx.imageSmoothingQuality = 'high';

        // 1) 다크 메탈 섀시 배경 (고대비 블랙 톤)
        const grad = ctx.createLinearGradient(0, 0, 0, h);
        grad.addColorStop(0, '#181c22');
        grad.addColorStop(0.5, '#101318');
        grad.addColorStop(1, '#090b0e');
        ctx.fillStyle = grad;
        ctx.fillRect(0, 0, w, h);

        // 2) 좌우 랙 이어 (Rack Ears)
        const earW = Math.round(40 * scaleX);
        ctx.fillStyle = '#07090b';
        ctx.fillRect(0, 0, earW, h);
        ctx.fillRect(w - earW, 0, earW, h);

        // 벤더 텍스트 색상 및 라벨 준비 (최대 명도/채도로 가독성 극대화)
        const vendorShort = vendor === 'SUPERMICRO' ? 'SMCI' : (vendor === 'CUSTOM' ? '조립' : (vendor === 'TEMP' ? '임시할당' : vendor));
        const modelLabel = `${vendorShort} | ${model}`;
        const hostDisplay = useTemplate
          ? `${model}`
          : (isTemp ? '⚡ [임시할당]' : hostname);

        let vendorTextColor = '#a5ffaf';
        if (vendor === 'DELL') vendorTextColor = '#79c0ff';
        else if (vendor === 'HP') vendorTextColor = '#56ff78';
        else if (vendor === 'CISCO') vendorTextColor = '#33f0ff';
        else if (vendor === 'CUSTOM') vendorTextColor = '#ffca68';
        else if (vendor === 'LENOVO') vendorTextColor = '#ff948b';
        else if (vendor === 'TEMP') vendorTextColor = '#f0b3ff';

        // 3) 좌측 장착 U 위치 & 몇U 표시 고대비 배지 박스 (넉넉한 폭 확보)
        const padY = Math.round(12 * scaleY);
        const uBadgeX = earW + Math.round(14 * scaleX);
        const uBadgeW = Math.round(440 * scaleX);
        const uBadgeH = h - padY * 2;

        ctx.fillStyle = '#05070a';
        ctx.fillRect(uBadgeX, padY, uBadgeW, uBadgeH);
        ctx.strokeStyle = '#ffea00';
        ctx.lineWidth = Math.max(4, Math.round(6 * scaleX));
        ctx.strokeRect(uBadgeX, padY, uBadgeW, uBadgeH);

        // 4) 중앙~우측 메인 명판 플레이트 (순수 다크 배경으로 글자 대비 극대화)
        const plateX = uBadgeX + uBadgeW + Math.round(18 * scaleX);
        const plateW = w - plateX - earW - Math.round(18 * scaleX);
        const plateH = h - padY * 2;

        ctx.fillStyle = isSim ? '#082f68' : '#070a0f';
        ctx.fillRect(plateX, padY, plateW, plateH);

        // 벤더 고유 색상 좌측 액센트 바 & 외곽선
        const accentW = Math.round(34 * scaleX);
        ctx.fillStyle = vendorColor;
        ctx.fillRect(plateX, padY, accentW, plateH);
        ctx.strokeStyle = isSim ? '#00e5ff' : vendorColor;
        ctx.lineWidth = isSim ? Math.round(7 * scaleX) : Math.round(5 * scaleX);
        ctx.strokeRect(plateX, padY, plateW, plateH);

        const strokeW = Math.max(4, Math.round(8 * scaleX));

        if (formFactorU >= 2) {
          // =========================================================================
          // [Multi-U (2U~4U+) 전용 2단 상하 분리 레이아웃 (2048x512 POT)]
          // =========================================================================
          const uPosText = useTemplate ? `[ ${formFactorU}U ]` : `${startU}~${endU}U`;
          const uSizeText = useTemplate ? `SERVER` : `[ ${formFactorU}U ]`;
          const uFont = Math.round(102 * scaleX);

          ctx.textAlign = 'center';
          ctx.textBaseline = 'middle';

          // 좌측 박스 1층
          ctx.fillStyle = '#ffea00';
          ctx.font = `900 ${uFont}px "Malgun Gothic", "Segoe UI", Consolas, monospace`;
          this._drawBoldText(ctx, uPosText, uBadgeX + uBadgeW / 2, padY + uBadgeH * 0.32, uBadgeW - 20, strokeW);

          // 좌측 박스 2층
          ctx.fillStyle = '#00f0ff';
          ctx.font = `900 ${Math.round(uFont * 0.92)}px "Malgun Gothic", "Segoe UI", Consolas, monospace`;
          this._drawBoldText(ctx, uSizeText, uBadgeX + uBadgeW / 2, padY + uBadgeH * 0.74, uBadgeW - 20, strokeW);

          // 우측 명판 상/하 층 구분선
          const midY = padY + plateH * 0.5;
          ctx.strokeStyle = 'rgba(255, 255, 255, 0.28)';
          ctx.lineWidth = 4;
          ctx.beginPath();
          ctx.moveTo(plateX + accentW + 14, midY);
          ctx.lineTo(plateX + plateW - 14, midY);
          ctx.stroke();

          const mainFont = Math.round(106 * scaleX);
          const innerPlateW = plateW - accentW - Math.round(56 * scaleX);

          // 우측 명판 1층(상단): 벤더 | 모델코드 (좌측 정렬)
          ctx.fillStyle = vendorTextColor;
          ctx.font = `900 ${mainFont}px "Malgun Gothic", "Segoe UI", sans-serif`;
          ctx.textAlign = 'left';
          this._drawBoldText(ctx, modelLabel, plateX + accentW + Math.round(28 * scaleX), padY + plateH * 0.27, innerPlateW, strokeW);

          // 우측 명판 2층(하단): 호스트네임 (우측 정렬)
          ctx.fillStyle = isTemp ? '#f8c4ff' : '#ffffff';
          ctx.font = `900 ${Math.round(mainFont * 1.08)}px "Malgun Gothic", "Segoe UI", Consolas, monospace`;
          ctx.textAlign = 'right';
          this._drawBoldText(ctx, hostDisplay, plateX + plateW - Math.round(28 * scaleX), padY + plateH * 0.74, innerPlateW, strokeW);
        } else {
          // =========================================================================
          // [1U 전용 3구획 독립 박스 레이아웃 (2048x256 POT)]
          // =========================================================================
          const uText = useTemplate ? `1U [SERVER]` : `${startU}U [${formFactorU}U]`;
          const fontSize1U = Math.round(92 * scaleX);

          // 구획 1: U 위치
          ctx.fillStyle = '#ffea00';
          ctx.font = `900 ${fontSize1U}px "Malgun Gothic", "Segoe UI", Consolas, monospace`;
          ctx.textAlign = 'center';
          ctx.textBaseline = 'middle';
          this._drawBoldText(ctx, uText, uBadgeX + uBadgeW / 2, h / 2 + 2, uBadgeW - 18, strokeW);

          // 우측 플레이트를 좌(벤더|모델 48%) / 우(호스트명 52%) 독립 칸으로 분할
          const splitX = plateX + accentW + Math.round((plateW - accentW) * 0.48);
          ctx.strokeStyle = 'rgba(255, 255, 255, 0.32)';
          ctx.lineWidth = 4;
          ctx.beginPath();
          ctx.moveTo(splitX, padY + 8);
          ctx.lineTo(splitX, h - padY - 8);
          ctx.stroke();

          // 구획 2: 벤더 | 모델코드 칸
          const modelBoxX = plateX + accentW + Math.round(24 * scaleX);
          const modelBoxMaxW = splitX - modelBoxX - Math.round(18 * scaleX);
          ctx.fillStyle = vendorTextColor;
          ctx.font = `900 ${Math.round(86 * scaleX)}px "Malgun Gothic", "Segoe UI", sans-serif`;
          ctx.textAlign = 'left';
          this._drawBoldText(ctx, modelLabel, modelBoxX, h / 2 + 2, modelBoxMaxW, strokeW);

          // 구획 3: 호스트네임 칸
          const hostBoxRightX = plateX + plateW - Math.round(24 * scaleX);
          const hostBoxMaxW = hostBoxRightX - splitX - Math.round(20 * scaleX);
          ctx.fillStyle = isTemp ? '#f8c4ff' : '#ffffff';
          ctx.font = `900 ${fontSize1U}px "Malgun Gothic", "Segoe UI", Consolas, monospace`;
          ctx.textAlign = 'right';
          this._drawBoldText(ctx, hostDisplay, hostBoxRightX, h / 2 + 2, hostBoxMaxW, strokeW);
        }

        // 상태 LED 포인트
        ctx.fillStyle = '#00e676';
        ctx.beginPath();
        ctx.arc(w - earW / 2, h / 2, Math.max(5, Math.round(9 * scaleX)), 0, Math.PI * 2);
        ctx.fill();
      }, width, height);
    }
    /**
     * 2. 서버 후면 고해상도 텍스처 (2048x256 / 2048x512 POT 규격)
     */
    getServerBackTexture(device, vendorColor = '#4caf50', massiveScale = false, isDetailed = false) {
      const hostname = device.hostname || '';
      const isTemp = !hostname || device.isTemp;
      const model = device.model || '012_001';
      const vendor = device.vendor || 'OTHER';
      const startU = device.startU || 1;
      const endU = device.endU || startU;
      const formFactorU = device.formFactorU || 1;
      const isSim = !!device.isSimulation;
      const useTemplate = massiveScale && !isDetailed;

      const key = useTemplate
        ? `srv_back_tpl_v5_${formFactorU}_${vendor}_${model}_${isSim}_${vendorColor}`
        : `srv_back_v5_${startU}_${formFactorU}_${vendor}_${model}_${hostname}_${isSim}_${vendorColor}`;

      const width = massiveScale ? 1024 : 2048;
      const height = formFactorU >= 2 ? 512 : 256;
      const scaleX = width / 2048;

      return this.getOrCreateTexture(key, (ctx, w, h) => {
        const grad = ctx.createLinearGradient(0, 0, 0, h);
        grad.addColorStop(0, '#2a2e35');
        grad.addColorStop(1, '#181a1f');
        ctx.fillStyle = grad;
        ctx.fillRect(0, 0, w, h);

        const earW = Math.round(40 * scaleX);
        ctx.fillStyle = '#0d0f12';
        ctx.fillRect(0, 0, earW, h);
        ctx.fillRect(w - earW, 0, earW, h);

        // 좌측 듀얼 PSU 소켓 표현
        const psuW = Math.round(200 * scaleX);
        ctx.fillStyle = '#0c0e11';
        ctx.fillRect(earW + 10, 16, psuW, h - 32);
        ctx.strokeStyle = '#ffb300';
        ctx.lineWidth = 4;
        ctx.strokeRect(earW + 10, 16, psuW, h - 32);
        ctx.fillStyle = '#00e676';
        ctx.beginPath();
        ctx.arc(earW + Math.round(44 * scaleX), h / 2, Math.max(6, Math.round(10 * scaleX)), 0, Math.PI * 2);
        ctx.fill();

        const uPosText = useTemplate
          ? (formFactorU > 1 ? `[ ${formFactorU}U ]` : `1U`)
          : (formFactorU > 1 ? `${startU}~${endU}U` : `${startU}U`);
        const badgeX = earW + psuW + Math.round(24 * scaleX);
        const badgeW = w - badgeX - earW - Math.round(20 * scaleX);
        const badgeH = h - 28;

        ctx.fillStyle = '#070a0f';
        ctx.fillRect(badgeX, 14, badgeW, badgeH);
        ctx.strokeStyle = vendorColor;
        ctx.lineWidth = 6;
        ctx.strokeRect(badgeX, 14, badgeW, badgeH);

        const hostDisplay = useTemplate
          ? `${model} (REAR)`
          : (isTemp ? '[임시할당]' : hostname);
        const strokeW = Math.max(4, Math.round(8 * scaleX));
        ctx.textBaseline = 'middle';

        if (formFactorU >= 2) {
          const fontSize = Math.round(96 * scaleX);
          ctx.font = `900 ${fontSize}px "Malgun Gothic", "Segoe UI", Consolas, monospace`;
          ctx.fillStyle = '#ffea00';
          ctx.textAlign = 'left';
          this._drawBoldText(ctx, `${uPosText} [${formFactorU}U]  |  ${model}`, badgeX + 32, 14 + badgeH * 0.32, badgeW - 64, strokeW);

          ctx.fillStyle = '#ffffff';
          ctx.textAlign = 'right';
          this._drawBoldText(ctx, `${hostDisplay}`, badgeX + badgeW - 32, 14 + badgeH * 0.72, badgeW - 64, strokeW);
        } else {
          const fontSize = Math.round(86 * scaleX);
          const splitX = badgeX + Math.round(badgeW * 0.34);
          ctx.font = `900 ${fontSize}px "Malgun Gothic", "Segoe UI", Consolas, monospace`;
          ctx.fillStyle = '#ffea00';
          ctx.textAlign = 'left';
          this._drawBoldText(ctx, `${uPosText} [${formFactorU}U]`, badgeX + 26, h / 2 + 2, splitX - badgeX - 32, strokeW);

          ctx.fillStyle = '#ffffff';
          ctx.textAlign = 'right';
          this._drawBoldText(ctx, `${model} | ${hostDisplay}`, badgeX + badgeW - 26, h / 2 + 2, badgeX + badgeW - splitX - 32, strokeW);
        }
      }, width, height);
    }

    /**
     * 3. 네트워크 스위치 전면/후면 텍스처 (2048x256 / 2048x512 POT 규격)
     */
    getSwitchTexture(device, isBack = false) {
      const hostname = device.hostname || 'TOR-SW';
      const model = device.model || 'Switch';
      const startU = device.startU || 49;
      const endU = device.endU || startU;
      const formFactorU = device.formFactorU || 1;
      const isSim = !!device.isSimulation;

      const key = `sw_v4_${isBack ? 'back' : 'front'}_${startU}_${formFactorU}_${model}_${hostname}_${isSim}`;
      const width = 2048;
      const height = formFactorU >= 2 ? 512 : 256;

      return this.getOrCreateTexture(key, (ctx, w, h) => {
        ctx.fillStyle = '#081020';
        ctx.fillRect(0, 0, w, h);

        ctx.strokeStyle = '#00f0ff';
        ctx.lineWidth = 7;
        ctx.strokeRect(6, 6, w - 12, h - 12);

        const uPosText = formFactorU > 1 ? `${startU}~${endU}U` : `${startU}U`;
        const fontSize = 88;

        ctx.fillStyle = '#00f0ff';
        ctx.font = `900 ${fontSize}px "Malgun Gothic", "Segoe UI", Consolas, monospace`;
        ctx.textAlign = 'left';
        ctx.textBaseline = 'middle';
        this._drawBoldText(ctx, `${uPosText} [${formFactorU}U SW]`, 44, h / 2 + 2, w * 0.38, 8);

        ctx.fillStyle = '#ffffff';
        ctx.textAlign = 'right';
        this._drawBoldText(ctx, `${model} | ${hostname || '[SW 임시]'}`, w - 44, h / 2 + 2, w * 0.56, 8);
      }, width, height);
    }

    /**
     * 4. 상단뷰(Top View) 전용 랙 상판(Top Roof) 벤더별 점유율 컬러 바 텍스처 (캡처 4 완벽 구현)
     * - 랙의 총 U(기본 52U) 대비 각 벤더가 차지하는 비율만큼 가로 스트라이프 블록으로 채우고,
     *   남은 가용 공간은 어두운 슬레이트 색으로 표시하여 상단뷰에서 벤더 비중을 한눈에 파악 가능
     */
    getRackRoofVendorTexture(rackId, stats, massiveScale = false, isDetailed = false) {
      const breakdown = (stats && stats.vendorBreakdown) || [];
      const sig = breakdown.map(b => `${b.vendor}:${b.usedU}:${b.color}`).join('|');
      const maxU = (stats && stats.maxU) || 52;
      const useTemplate = massiveScale && !isDetailed;
      const key = useTemplate
        ? `roof_tpl_${maxU}_${sig}`
        : `roof_${rackId}_${maxU}_${sig}`;

      const texW = useTemplate ? 128 : 256;
      const texH = useTemplate ? 256 : 512;

      return this.getOrCreateTexture(key, (ctx, w, h) => {
        // 기본 빈 공간 배경 (다크 그린/차콜 톤)
        ctx.fillStyle = '#1b2430';
        ctx.fillRect(0, 0, w, h);

        // 벤더별 점유 비율을 위에서 아래로(Z축 방향) 스택 바로 채우기 (캡처 4의 X001Y001 형태)
        const pad = Math.round(14 * (w / 256));
        const innerW = w - pad * 2;
        const innerH = h - pad * 2;
        let currentY = pad;

        if (breakdown.length === 0) {
          // 비어있는 랙
          ctx.fillStyle = '#1e3a2f';
          ctx.fillRect(pad, pad, innerW, innerH);
        } else {
          breakdown.forEach(item => {
            const segH = Math.max(Math.round(16 * (h / 512)), Math.round((item.usedU / maxU) * innerH));
            const drawH = Math.min(segH, pad + innerH - currentY);
            if (drawH > 0) {
              ctx.fillStyle = item.color;
              ctx.fillRect(pad, currentY, innerW, drawH);

              // 세그먼트 구분선
              ctx.strokeStyle = 'rgba(0,0,0,0.6)';
              ctx.lineWidth = Math.max(1, Math.round(3 * (w / 256)));
              ctx.strokeRect(pad, currentY, innerW, drawH);

              // 비중이 충분히 큰 경우 벤더명/U수를 상판에 표시
              const minTextH = Math.round(36 * (h / 512));
              if (drawH >= minTextH) {
                ctx.fillStyle = '#ffffff';
                const fontSize = Math.round(24 * (w / 256));
                ctx.font = `bold ${fontSize}px Consolas, sans-serif`;
                ctx.textAlign = 'center';
                ctx.textBaseline = 'middle';
                ctx.shadowColor = 'rgba(0,0,0,0.85)';
                ctx.shadowBlur = 3;
                const shortV = item.vendor === 'SUPERMICRO' ? 'SMCI' : (item.vendor === 'CUSTOM' ? '조립' : (item.vendor === 'TEMP' ? '임시' : item.vendor));
                ctx.fillText(`${shortV} (${item.usedU}U)`, w / 2, currentY + drawH / 2);
                ctx.shadowBlur = 0;
              }
              currentY += drawH;
            }
          });

          // 잔여 빈 공간 영역 표시
          if (currentY < pad + innerH) {
            const remainH = pad + innerH - currentY;
            ctx.fillStyle = '#1f2937';
            ctx.fillRect(pad, currentY, innerW, remainH);
            if (remainH >= Math.round(36 * (h / 512))) {
              ctx.fillStyle = '#94a3b8';
              const freeFontSize = Math.round(20 * (w / 256));
              ctx.font = `bold ${freeFontSize}px Consolas, sans-serif`;
              ctx.textAlign = 'center';
              ctx.textBaseline = 'middle';
              ctx.fillText(`여유 ${stats.serverFreeU !== undefined ? stats.serverFreeU : stats.freeU}U`, w / 2, currentY + remainH / 2);
            }
          }
        }

        // 상단뷰에서도 랙 번호가 선명하게 보이도록 하단 가장자리에 랙 ID 캡션 바 추가
        const barH = Math.round(50 * (h / 512));
        ctx.fillStyle = 'rgba(13, 17, 23, 0.92)';
        ctx.fillRect(4, h - barH - 4, w - 8, barH);
        ctx.fillStyle = '#00e5ff';
        const idFontSize = Math.round(30 * (w / 256));
        ctx.font = `900 ${idFontSize}px Consolas, monospace`;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.fillText(useTemplate ? 'RACK' : rackId, w / 2, h - barH / 2 - 2);

        // 외곽 테두리
        ctx.strokeStyle = '#475569';
        ctx.lineWidth = Math.max(2, Math.round(8 * (w / 256)));
        ctx.strokeRect(4, 4, w - 8, h - 8);
      }, texW, texH);
    }

    /**
     * 5. 랙 전면 기둥(Vertical Post) U 눈금 및 49~52U 네트워크 전용 구역 표시 텍스처
     */
    getRackPostRulerTexture(maxU = 52, serverMaxU = 48) {
      const key = `post_ruler_${maxU}_${serverMaxU}`;
      return this.getOrCreateTexture(key, (ctx, w, h) => {
        ctx.fillStyle = '#181c24';
        ctx.fillRect(0, 0, w, h);

        const uStep = h / maxU;
        for (let u = 1; u <= maxU; u++) {
          // 캔버스 Y는 위가 0이므로 상단이 maxU, 하단이 1U
          const yTop = h - u * uStep;
          const isNetZone = u > serverMaxU;

          if (isNetZone) {
            ctx.fillStyle = 'rgba(0, 229, 255, 0.18)';
            ctx.fillRect(0, yTop, w, uStep);
          }

          ctx.strokeStyle = (u % 5 === 0 || u === serverMaxU) ? '#94a3b8' : '#334155';
          ctx.lineWidth = (u === serverMaxU) ? 3 : 1;
          ctx.beginPath();
          ctx.moveTo(0, yTop);
          ctx.lineTo(w, yTop);
          ctx.stroke();

          if (u === 1 || u % 5 === 0 || u === serverMaxU || u === maxU) {
            ctx.fillStyle = isNetZone ? '#00e5ff' : (u === serverMaxU ? '#ffd600' : '#cbd5e1');
            ctx.font = 'bold 20px Consolas, monospace';
            ctx.textAlign = 'center';
            ctx.textBaseline = 'middle';
            ctx.fillText(`${u}`, w / 2, yTop + uStep / 2);
          }
        }
      }, 64, 2048);
    }

    /**
     * 6. 바닥 X / Y 축 라인 가이드 라벨 텍스처 (캡처 2 스타일)
     */
    getAxisFloorLabelTexture(text, isRowAxis = true) {
      const key = `axis_lbl_v4_${text}_${isRowAxis}`;
      return this.getOrCreateTexture(key, (ctx, w, h) => {
        ctx.fillStyle = isRowAxis ? 'rgba(220, 38, 38, 0.25)' : 'rgba(37, 99, 235, 0.25)';
        ctx.fillRect(4, 4, w - 8, h - 8);

        ctx.strokeStyle = isRowAxis ? '#ef4444' : '#3b82f6';
        ctx.lineWidth = 5;
        ctx.strokeRect(4, 4, w - 8, h - 8);

        ctx.fillStyle = isRowAxis ? '#fecaca' : '#bfdbfe';
        ctx.font = '900 58px "Malgun Gothic", "Segoe UI", Consolas, monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        this._drawBoldText(ctx, text, w / 2, h / 2, w - 16, 6);
      }, 256, 128);
    }

    /**
     * 7. 3D 랙 상단 전면/후면 물리적 명판(Header Plate) 텍스처
     * - 대규모 환경(랙 600개)에서는 256x64로 해상도를 최적화하여 840MB에 달하던 텍스처 VRAM을 40MB 이하로 95% 절감!
     * - 단일 랙 선택/상세 뷰에서는 512x128 고해상도로 선명하게 렌더링
     */
    getRackHeaderPlateTexture(rackName, isSelected = false, massiveScale = false) {
      const useLowRes = massiveScale && !isSelected;
      const key = `rack_plate_v5_${rackName}_${isSelected}_${useLowRes ? 'low' : 'hi'}`;
      const width = useLowRes ? 256 : 512;
      const height = useLowRes ? 64 : 128;
      const scale = width / 1024;

      return this.getOrCreateTexture(key, (ctx, w, h) => {
        // 배경 그라데이션
        const grad = ctx.createLinearGradient(0, 0, 0, h);
        if (isSelected) {
          grad.addColorStop(0, '#0d47a1');
          grad.addColorStop(1, '#07224d');
        } else {
          grad.addColorStop(0, '#0d1117');
          grad.addColorStop(1, '#05070a');
        }
        ctx.fillStyle = grad;
        ctx.fillRect(0, 0, w, h);

        // 테두리
        ctx.strokeStyle = isSelected ? '#00f0ff' : '#38bdf8';
        const strokeW = Math.max(2, Math.round((isSelected ? 16 : 12) * scale));
        ctx.lineWidth = strokeW;
        const inset = Math.max(2, Math.round(8 * scale));
        ctx.strokeRect(inset, inset, w - inset * 2, h - inset * 2);

        // 텍스트
        ctx.fillStyle = isSelected ? '#00f0ff' : '#ffffff';
        const fontSize = Math.round(152 * scale);
        ctx.font = `900 ${fontSize}px "Malgun Gothic", "Segoe UI", Consolas, monospace`;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        this._drawBoldText(ctx, rackName, w / 2, h / 2 + Math.max(1, Math.round(4 * scale)), w - Math.round(40 * scale), Math.max(2, Math.round(10 * scale)));
      }, width, height);
    }

    /**
     * 8. 출입문(Entrance) 표지판 텍스처 (주 출입구: 레드 기준점 / 반대편 보조 출입구: 에메랄드·블루 구분, 1024x256 POT 규격)
     */
    getEntranceSignTexture(isMain = true) {
      const key = `entrance_door_sign_v4_${isMain ? 'main' : 'sub'}`;
      return this.getOrCreateTexture(key, (ctx, w, h) => {
        ctx.fillStyle = isMain ? '#180c0c' : '#08151c';
        ctx.fillRect(0, 0, w, h);
        ctx.strokeStyle = isMain ? '#ff5252' : '#10b981';
        ctx.lineWidth = 16;
        ctx.strokeRect(10, 10, w - 20, h - 20);

        ctx.fillStyle = isMain ? '#ff6b6b' : '#34d399';
        ctx.font = '900 94px "Malgun Gothic", "Segoe UI", sans-serif';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        const title = isMain ? '🚪 주 출입구 (기준점)' : '🚪 반대편 출입구 (SUB)';
        this._drawBoldText(ctx, title, w / 2, h / 2, w - 48, 8);
      }, 1024, 256);
    }

    /**
     * 8. 바닥 타일 프로시저럴 텍스처
     */
    getTileTexture(type = 'standard') {
      const key = `tile_${type}`;
      return this.getOrCreateTexture(key, (ctx, w, h) => {
        let baseColor = '#d6d8db';
        let edgeColor = '#6c757d';

        if (type === 'zone_green') {
          baseColor = '#a5d6a7';
          edgeColor = '#388e3c';
        } else if (type === 'zone_yellow') {
          baseColor = '#fff59d';
          edgeColor = '#fbc02d';
        } else if (type === 'zone_red') {
          baseColor = '#ef9a9a';
          edgeColor = '#d32f2f';
        }

        ctx.fillStyle = baseColor;
        ctx.fillRect(0, 0, w, h);

        ctx.strokeStyle = edgeColor;
        ctx.lineWidth = 4;
        ctx.strokeRect(2, 2, w - 4, h - 4);

        if (type === 'perforated') {
          ctx.fillStyle = '#212529';
          const dotStep = 20;
          for (let x = 24; x <= w - 24; x += dotStep) {
            for (let y = 24; y <= h - 24; y += dotStep) {
              ctx.beginPath();
              ctx.arc(x, y, 4, 0, Math.PI * 2);
              ctx.fill();
            }
          }
        }
      }, 256, 256);
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = TextureGenerator;
  } else {
    global.DCTextureGenerator = new TextureGenerator();
  }

})(typeof window !== 'undefined' ? window : this);
