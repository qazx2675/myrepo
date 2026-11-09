(function() {
    // 기본 제외 호스트 패턴 (실제 환경 소스 진행 시 자동 추가되는 6~7대 필수 서버를 미리 지정 가능)
    const DEFAULT_EXCLUDE_HOSTS = "prd-db-0295|prd-db-0296|prd-db-0297|prd-db-0298|prd-db-0299|prd-db-0300";

    // 기존 패널 및 스타일시트가 열려 있다면 제거 후 최신 상태로 렌더링
    const prevPanel = document.getElementById('awx-validator-panel');
    if (prevPanel) prevPanel.remove();
    const prevStyle = document.getElementById('awx-validator-styles');
    if (prevStyle) prevStyle.remove();

    // 상태 저장 객체 (재실행 시 기존 상태 유지 및 신규 필드 기본값 보장)
    window.awxValidatorState = window.awxValidatorState || {};
    const state = window.awxValidatorState;
    if (!state.collectedHosts) state.collectedHosts = new Set();
    if (!Array.isArray(state.excludedHosts)) state.excludedHosts = [];
    if (typeof state.rawCount !== 'number') state.rawCount = 0;
    if (typeof state.excludePattern !== 'string') state.excludePattern = DEFAULT_EXCLUDE_HOSTS;
    if (typeof state.detailText !== 'string') state.detailText = '';
    if (typeof state.incidentText !== 'string') state.incidentText = '';
    if (typeof state.targetText !== 'string') state.targetText = '';
    if (typeof state.isStandalone !== 'boolean') state.isStandalone = false;
    if (typeof state.isPinned !== 'boolean') state.isPinned = false;
    if (typeof state.guideExpanded !== 'boolean') state.guideExpanded = false;
    if (typeof state.opacityEnabled !== 'boolean') state.opacityEnabled = true;
    if (typeof state.opacityValue !== 'number') state.opacityValue = 30;
    if (typeof state.isMouseInside !== 'boolean') state.isMouseInside = true;
    if (!state.resetTargets || typeof state.resetTargets !== 'object') {
        state.resetTargets = {
            detail: true,
            incident: true,
            target: true
        };
    }
    if (typeof state.showResetConfig !== 'boolean') state.showResetConfig = false;

    // 현재 접속 중인 AWX 화면 컨텍스트 분석 (인벤토리 ID, 그룹 ID, 또는 전역)
    function getContext() {
        const hash = window.location.hash || '';
        const invMatch = hash.match(/\/inventories\/(?:inventory|smart_inventory)\/(\d+)/);
        if (invMatch) {
            return {
                url: `/api/v2/inventories/${invMatch[1]}/hosts/?page_size=200`,
                label: `인벤토리 #${invMatch[1]}`,
                isScoped: true
            };
        }
        const grpMatch = hash.match(/\/groups\/(\d+)\/(?:nested_hosts|hosts)/);
        if (grpMatch) {
            return {
                url: `/api/v2/groups/${grpMatch[1]}/all_hosts/?page_size=200`,
                label: `그룹 #${grpMatch[1]}`,
                isScoped: true
            };
        }
        return {
            url: '/api/v2/hosts/?page_size=200',
            label: '전체 인벤토리 전역',
            isScoped: false
        };
    }

    // 제외 패턴 문자열 파싱 (정확 일치 및 부분 포함 매칭)
    function getExcludeFilters(patternStr) {
        if (!patternStr || !patternStr.trim()) return [];
        return patternStr.split('|')
            .map(s => s.trim().toLowerCase())
            .filter(Boolean);
    }

    function isHostExcluded(host, filters) {
        if (!filters || filters.length === 0) return false;
        const h = host.toLowerCase();
        return filters.some(f => h === f || h.includes(f));
    }

    // 텍스트 파서 (줄바꿈, 공백, 콤마, 세미콜론, 탭 분리 및 시리얼/비고 제거)
    // / 앞뒤 공백 유무에 상관없이 hostname / AAAAAA 형태를 정상 인식하여 호스트명만 추출
    function parseHostText(rawText) {
        if (!rawText) return [];
        const normalized = rawText.replace(/\\n/g, '\n').trim();
        const resultSet = new Set();
        const lines = normalized.split(/[\r\n]+/);
        lines.forEach(line => {
            // 괄호 내용(비고/시리얼) 사전 제거: 'host1 ( AAAAAA )' -> 'host1'
            let cleanLine = line.replace(/\s*\([^)]*\)/g, '');
            // 콤마, 세미콜론, 탭으로 분리
            const entries = cleanLine.split(/[,;\t]+/);
            entries.forEach(entry => {
                const trimmed = entry.trim();
                if (!trimmed) return;
                // 슬래시(/)가 포함된 경우 (예: "hostname / AAAAAA" 또는 "hostname/AAAAAA")
                if (trimmed.includes('/')) {
                    // / 앞뒤 공백 정규화: "hostname / AAAAAA" -> "hostname/AAAAAA"
                    const slashNorm = trimmed.replace(/\s*\/\s*/g, '/');
                    const tokens = slashNorm.split(/\s+/);
                    tokens.forEach(tok => {
                        if (tok.includes('/')) {
                            let clean = tok.split('/')[0].trim().toLowerCase();
                            if (clean && clean !== '/' && clean !== '-' && !clean.startsWith('sn:') && !clean.startsWith('sn=')) {
                                resultSet.add(clean);
                            }
                        }
                    });
                } else {
                    // 슬래시가 없는 일반 공백 분리
                    const tokens = trimmed.split(/\s+/);
                    tokens.forEach(tok => {
                        let clean = tok.trim().toLowerCase();
                        if (clean && clean !== '/' && clean !== '-' && !clean.startsWith('sn:') && !clean.startsWith('sn=')) {
                            resultSet.add(clean);
                        }
                    });
                }
            });
        });
        return Array.from(resultSet);
    }

    // 작업 권장 안내 펼치기 시 표시할 작업대상 호스트 추출 (인시던트 호스트 우선, 없으면 작업대상/상세목록현황)
    function getGuideTargetHosts(txtIncidentVal, txtTargetVal, txtDetailVal) {
        const inc = parseHostText(txtIncidentVal);
        if (inc.length > 0) return inc;
        const tgt = parseHostText(txtTargetVal);
        if (tgt.length > 0) return tgt;
        const dtl = parseHostText(txtDetailVal);
        if (dtl.length > 0) return dtl;
        return [];
    }

    // 선명한 가독성과 엔터프라이즈 디자인을 위한 전용 스타일시트
    function getStylesheet() {
        return `
        #awx-validator-panel, .awx-vld-popup-root {
            font-family: "Pretendard Variable", "Pretendard", "Malgun Gothic", "맑은 고딕", "Apple SD Gothic Neo", -apple-system, BlinkMacSystemFont, system-ui, sans-serif !important;
            -webkit-font-smoothing: antialiased !important;
            -moz-osx-font-smoothing: grayscale !important;
            text-rendering: optimizeLegibility !important;
            color: #111827;
            font-size: 13px;
            line-height: 1.5;
            box-sizing: border-box;
        }
        #awx-validator-panel *, .awx-vld-popup-root * {
            box-sizing: border-box;
            font-family: inherit;
        }
        .awx-vld-mono {
            font-family: "JetBrains Mono", "D2Coding", "Consolas", "Malgun Gothic", "맑은 고딕", monospace !important;
            font-variant-numeric: tabular-nums;
            letter-spacing: -0.1px;
        }
        .awx-vld-card {
            background: #ffffff;
            border: 1px solid #d1d5db;
            border-radius: 6px;
            padding: 14px;
            box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03);
        }
        .awx-vld-section-title {
            font-size: 13px;
            font-weight: 700;
            color: #111827;
            letter-spacing: -0.2px;
        }
        .awx-vld-input, .awx-vld-textarea {
            width: 100%;
            border: 1px solid #cbd5e1;
            border-radius: 5px;
            padding: 7px 10px;
            font-size: 12.5px;
            line-height: 1.5;
            color: #111827;
            background: #ffffff;
            transition: border-color 0.15s ease, box-shadow 0.15s ease;
            outline: none;
        }
        .awx-vld-input:focus, .awx-vld-textarea:focus {
            border-color: #2563eb;
            box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.12);
        }
        .awx-vld-input::placeholder, .awx-vld-textarea::placeholder {
            color: #9ca3af;
            font-family: "Pretendard", "Malgun Gothic", "맑은 고딕", sans-serif !important;
        }
        .awx-vld-btn {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            gap: 5px;
            border-radius: 5px;
            font-size: 12px;
            font-weight: 600;
            padding: 7px 12px;
            cursor: pointer;
            transition: all 0.15s ease;
            user-select: none;
            line-height: 1.2;
        }
        .awx-vld-btn-secondary {
            background: #ffffff;
            color: #1f2937;
            border: 1px solid #cbd5e1;
        }
        .awx-vld-btn-secondary:hover {
            background: #f8fafc;
            border-color: #94a3b8;
        }
        .awx-vld-btn-dark {
            background: #1e293b;
            color: #ffffff;
            border: 1px solid #0f172a;
        }
        .awx-vld-btn-dark:hover {
            background: #0f172a;
        }
        .awx-vld-btn-primary {
            background: #2563eb;
            color: #ffffff;
            border: 1px solid #1d4ed8;
            font-size: 13.5px;
            font-weight: 700;
            padding: 11px 16px;
            box-shadow: 0 1px 3px rgba(37, 99, 235, 0.25);
        }
        .awx-vld-btn-primary:hover {
            background: #1d4ed8;
        }
        .awx-vld-btn-sm {
            padding: 4px 8px;
            font-size: 11.5px;
            font-weight: 600;
            border-radius: 4px;
        }
        .awx-vld-header-btn {
            display: inline-flex;
            align-items: center;
            gap: 4px;
            background: #1e293b;
            color: #e2e8f0;
            border: 1px solid #334155;
            padding: 4px 9px;
            border-radius: 4px;
            font-size: 11.5px;
            font-weight: 600;
            cursor: pointer;
            white-space: nowrap;
            flex-shrink: 0;
            transition: all 0.15s ease;
        }
        .awx-vld-header-btn:hover {
            background: #334155;
            color: #ffffff;
        }
        .awx-vld-header-btn.pinned {
            background: #2563eb;
            color: #ffffff;
            border-color: #60a5fa;
            box-shadow: 0 0 0 2px rgba(37, 99, 235, 0.35);
        }
        .awx-vld-badge-req {
            display: inline-block;
            background: #fee2e2;
            color: #b91c1c;
            font-size: 11px;
            font-weight: 700;
            padding: 1px 6px;
            border-radius: 3px;
            margin-left: 4px;
        }
        .awx-vld-badge-opt {
            display: inline-block;
            background: #f1f5f9;
            color: #475569;
            font-size: 11px;
            font-weight: 600;
            padding: 1px 6px;
            border-radius: 3px;
            margin-left: 4px;
        }
        .awx-vld-opacity-bar {
            background: #1e293b;
            color: #e2e8f0;
            padding: 6px 14px;
            display: flex;
            justify-content: space-between;
            align-items: center;
            gap: 10px;
            border-bottom: 1px solid #334155;
            font-size: 11.5px;
            user-select: none;
        }
        .awx-vld-opacity-toggle {
            background: #334155;
            color: #94a3b8;
            border: 1px solid #475569;
            padding: 2px 8px;
            border-radius: 4px;
            font-size: 11px;
            font-weight: 700;
            cursor: pointer;
            transition: all 0.15s ease;
        }
        .awx-vld-opacity-toggle.active {
            background: #10b981;
            color: #ffffff;
            border-color: #34d399;
        }
        #awx-vld-range-opacity {
            width: 110px;
            height: 5px;
            accent-color: #3b82f6;
            cursor: pointer;
            vertical-align: middle;
        }
        #awx-vld-range-opacity:disabled {
            opacity: 0.4;
            cursor: not-allowed;
        }
        `;
    }

    // 핀 아이콘 SVG (고정 상태에 따라 회전 및 채우기 변경)
    function getPinSvg(isPinned) {
        const transform = isPinned ? 'rotate(0)' : 'rotate(45 8 8)';
        const fill = isPinned ? 'currentColor' : 'none';
        return `<svg width="12" height="12" viewBox="0 0 16 16" fill="${fill}" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" style="transform:${transform}; transition:transform 0.15s ease;"><path d="M9.5 2.5L13.5 6.5L12 8L10 6L6.5 9.5L8.5 11.5L7 13L4.5 10.5L2 13L3 14L2 14L2 13L4.5 10.5L2 8L3.5 6.5L5.5 8.5L9 5L7 3L8.5 1.5Z"/></svg>`;
    }

    // 톱니바퀴 아이콘 SVG (초기화 대상 설정용)
    function getGearSvg() {
        return `<svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor" style="display:block;"><path d="M9.405 1.05c-.413-1.4-2.397-1.4-2.81 0l-.1.34a1.464 1.464 0 0 1-2.105.872l-.31-.17c-1.283-.698-2.686.705-1.987 1.987l.169.311c.446.82.023 1.841-.872 2.105l-.34.1c-1.4.413-1.4 2.397 0 2.81l.34.1a1.464 1.464 0 0 1 .872 2.105l-.17.31c-.698 1.283.705 2.686 1.987 1.987l.311-.169a1.464 1.464 0 0 1 2.105.872l.1.34c.413 1.4 2.397 1.4 2.81 0l.1-.34a1.464 1.464 0 0 1 2.105-.872l.31.17c1.283.698 2.686-.705 1.987-1.987l-.169-.311a1.464 1.464 0 0 1 .872-2.105l.34-.1c1.4-.413 1.4-2.397 0-2.81l-.34-.1a1.464 1.464 0 0 1-.872-2.105l.17-.31c.698-1.283-.705-2.686-1.987-1.987l-.311.169a1.464 1.464 0 0 1-2.105-.872l-.1-.34zM8 10.93a2.929 2.929 0 1 1 0-5.86 2.929 2.929 0 0 1 0 5.86z"/></svg>`;
    }

    // HTML UI 생성 함수 (패널 및 독립 창 공용)
    function buildUIHTML(ctx, isPopup = false) {
        const pinClass = state.isPinned ? 'awx-vld-header-btn pinned' : 'awx-vld-header-btn';
        const pinLabel = state.isPinned ? '상단 고정됨' : '상단 고정';
        const opToggleClass = state.opacityEnabled ? 'awx-vld-opacity-toggle active' : 'awx-vld-opacity-toggle';
        const opToggleLabel = isPopup
            ? (state.opacityEnabled ? '자동 접기 ON' : '자동 접기 OFF')
            : (state.opacityEnabled ? '투명도 ON' : '투명도 OFF');
        return `
        <div id="awx-vld-header" style="background:#0f172a; color:#f8fafc; padding:11px 14px; display:flex; justify-content:space-between; align-items:center; gap:8px; cursor:${isPopup ? 'default' : 'move'}; user-select:none; border-bottom:1px solid #1e293b;">
            <div style="display:flex; align-items:center; gap:7px; flex-shrink:0;">
                <span style="font-size:13.5px; font-weight:700; letter-spacing:-0.3px; color:#ffffff; white-space:nowrap;">작업대상 호스트 교차 검증기</span>
                <span id="awx-vld-ctx-badge" style="background:#1e293b; color:#cbd5e1; border:1px solid #334155; font-size:11px; padding:2px 6px; border-radius:4px; font-weight:600; white-space:nowrap;">${ctx.label}</span>
            </div>
            <div style="display:flex; align-items:center; gap:5px; flex-shrink:0;">
                <button id="awx-vld-btn-pin" class="${pinClass}" title="창을 항상 가장 위에 고정 (포커스 유지)">
                    ${getPinSvg(state.isPinned)}
                    <span id="awx-vld-pin-text">${pinLabel}</span>
                </button>
                <button id="awx-vld-btn-detach" class="awx-vld-header-btn" title="패널 크기 맞춤 독립 브라우저 팝업 창으로 띄우기">${isPopup ? '페이지 내로 복귀' : '독립 창으로 분리'}</button>
                <button id="awx-vld-btn-close" style="background:none; border:none; color:#94a3b8; font-size:18px; cursor:pointer; line-height:1; padding:0 4px;" title="닫기">&times;</button>
            </div>
        </div>

        <div id="awx-vld-opacity-bar" class="awx-vld-opacity-bar">
            <div style="display:flex; align-items:center; gap:8px;">
                <span style="font-weight:600; color:#cbd5e1;">${isPopup ? '마우스 이탈 시 자동 접기 (아래 창 보기)' : '투명도 자동 조절'}</span>
                <button id="awx-vld-btn-opacity-toggle" class="${opToggleClass}" title="기능 켜기/끄기">${opToggleLabel}</button>
            </div>
            <div style="display:flex; align-items:center; gap:7px;">
                <span style="color:#94a3b8; font-size:11px;">${isPopup ? '페이지 내 투명도' : '마우스 이탈 시'}</span>
                <input id="awx-vld-range-opacity" type="range" min="10" max="100" step="5" value="${state.opacityValue}" ${state.opacityEnabled ? '' : 'disabled'}>
                <span id="awx-vld-lbl-opacity" class="awx-vld-mono" style="font-weight:700; color:#ffffff; min-width:34px; text-align:right;">${state.opacityValue}%</span>
            </div>
        </div>

        <div id="awx-vld-body-wrap" style="padding:14px; overflow-y:auto; flex:1; display:flex; flex-direction:column; gap:12px; background:#f3f4f6;">
            <!-- 1. AWX 호스트 수집 및 제외 섹션 -->
            <div class="awx-vld-card">
                <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:10px;">
                    <div>
                        <span class="awx-vld-section-title">1. AWX 호스트 수집</span>
                        <span class="awx-vld-badge-opt">선택 - 1대 이상 시 비교 참여</span>
                    </div>
                    <div style="display:flex; align-items:center; gap:4px;">
                        <button id="awx-vld-btn-reset-awx" class="awx-vld-btn awx-vld-btn-secondary awx-vld-btn-sm" title="수집된 AWX 및 설정된 비교 대상을 초기화합니다">수집 초기화</button>
                        <button id="awx-vld-btn-reset-config" class="awx-vld-btn awx-vld-btn-secondary awx-vld-btn-sm" style="padding:4px 6px; ${state.showResetConfig ? 'background:#e2e8f0;' : ''}" title="초기화 대상 설정 (톱니바퀴)">
                            ${getGearSvg()}
                        </button>
                    </div>
                </div>

                <!-- 톱니바퀴 설정 패널: 2번 비교 대상 호스트 초기화 여부 체크박스 -->
                <div id="awx-vld-reset-config-panel" style="display:${state.showResetConfig ? 'block' : 'none'}; background:#f8fafc; border:1px solid #cbd5e1; border-radius:5px; padding:8px 10px; margin-bottom:10px; font-size:11.5px;">
                    <div style="font-weight:700; color:#334155; margin-bottom:6px; display:flex; justify-content:space-between; align-items:center;">
                        <span>수집 초기화 시 함께 초기화할 항목</span>
                        <span style="font-size:10.5px; color:#64748b; font-weight:normal;">(체크된 항목이 함께 초기화됩니다)</span>
                    </div>
                    <div style="display:flex; flex-wrap:wrap; gap:12px; color:#1e293b;">
                        <label style="display:inline-flex; align-items:center; gap:4px; cursor:pointer; user-select:none;">
                            <input id="awx-vld-chk-reset-detail" type="checkbox" ${state.resetTargets.detail ? 'checked' : ''} style="cursor:pointer; accent-color:#2563eb;">
                            <span>상세목록현황 호스트</span>
                        </label>
                        <label style="display:inline-flex; align-items:center; gap:4px; cursor:pointer; user-select:none;">
                            <input id="awx-vld-chk-reset-incident" type="checkbox" ${state.resetTargets.incident ? 'checked' : ''} style="cursor:pointer; accent-color:#2563eb;">
                            <span>인시던트 호스트</span>
                        </label>
                        <label style="display:inline-flex; align-items:center; gap:4px; cursor:pointer; user-select:none;">
                            <input id="awx-vld-chk-reset-target" type="checkbox" ${state.resetTargets.target ? 'checked' : ''} style="cursor:pointer; accent-color:#2563eb;">
                            <span>작업대상 호스트</span>
                        </label>
                    </div>
                </div>

                <!-- 제외 패턴 입력란 -->
                <div style="margin-bottom:10px;">
                    <label style="display:block; font-size:12px; font-weight:600; color:#374151; margin-bottom:4px;">제외 호스트 설정 (| 구분)</label>
                    <input id="awx-vld-input-exclude" class="awx-vld-input awx-vld-mono" type="text" value="${state.excludePattern}" placeholder="예: host01|host02|bastion">
                    <div id="awx-vld-exclude-hint" style="font-size:11.5px; color:#6b7280; margin-top:4px; font-weight:500; ${state.excludePattern ? 'display:none;' : ''}">OS설치에 필요한 서버명은 이미 제외되어 있음</div>
                </div>

                <!-- 수집 액션 버튼 -->
                <div style="display:flex; gap:8px; margin-bottom:10px;">
                    <button id="awx-vld-btn-page" class="awx-vld-btn awx-vld-btn-secondary" style="flex:1;">현재 화면 수집</button>
                    <button id="awx-vld-btn-collect-all" class="awx-vld-btn awx-vld-btn-dark" style="flex:1;">현재 인벤토리 전체 수집</button>
                </div>

                <!-- AWX 수집 결과 상태 및 제외 요약 -->
                <div id="awx-vld-awx-status" style="font-size:12px; line-height:1.5; color:#1f2937; background:#f8fafc; padding:9px 11px; border-radius:5px; border:1px solid #e2e8f0;">
                    현재 수집된 호스트: <b id="awx-vld-count-awx" class="awx-vld-mono" style="color:#111827; font-size:13px;">${state.collectedHosts.size}</b> 대
                    <div id="awx-vld-awx-summary" style="margin-top:4px;"></div>
                </div>
            </div>

            <!-- 2. 비교 대상 텍스트 입력창 (상세목록현황, 인시던트, 작업대상) -->
            <div class="awx-vld-card" style="display:flex; flex-direction:column; gap:12px;">
                <div class="awx-vld-section-title">2. 비교 대상 호스트 입력</div>

                <!-- 입력 1: 상세목록현황 [필수] -->
                <div>
                    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:4px;">
                        <span style="font-size:12px; font-weight:700; color:#111827;">상세목록현황 호스트 <span class="awx-vld-badge-req">[필수]</span></span>
                        <span style="font-size:11.5px; color:#4b5563;">인식: <b id="awx-vld-cnt-detail" class="awx-vld-mono" style="color:#111827;">0</b>대</span>
                    </div>
                    <textarea id="awx-vld-txt-detail" class="awx-vld-textarea awx-vld-mono" placeholder="상세목록현황 호스트 붙여넣기 (개행/공백/콤마/탭/시리얼 자동 분리)" style="height:68px; resize:vertical;">${state.detailText}</textarea>
                </div>

                <!-- 입력 2: 인시던트 [필수] -->
                <div>
                    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:4px;">
                        <span style="font-size:12px; font-weight:700; color:#111827;">인시던트 호스트 <span class="awx-vld-badge-req">[필수]</span></span>
                        <span style="font-size:11.5px; color:#4b5563;">인식: <b id="awx-vld-cnt-incident" class="awx-vld-mono" style="color:#111827;">0</b>대</span>
                    </div>
                    <textarea id="awx-vld-txt-incident" class="awx-vld-textarea awx-vld-mono" placeholder="인시던트 작업대상 호스트 붙여넣기 (개행/공백/콤마/탭/시리얼 자동 분리)" style="height:68px; resize:vertical;">${state.incidentText}</textarea>
                </div>

                <!-- 입력 3: 작업대상 [선택] -->
                <div>
                    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:4px;">
                        <span style="font-size:12px; font-weight:700; color:#111827;">작업대상 호스트 <span class="awx-vld-badge-opt">[선택 - 1대 이상 시 비교 참여]</span></span>
                        <span style="font-size:11.5px; color:#4b5563;">인식: <b id="awx-vld-cnt-target" class="awx-vld-mono" style="color:#111827;">0</b>대</span>
                    </div>
                    <textarea id="awx-vld-txt-target" class="awx-vld-textarea awx-vld-mono" placeholder="작업대상 호스트 붙여넣기 (선택사항, 비어있으면 비교 제외)" style="height:58px; resize:vertical;">${state.targetText}</textarea>
                </div>
            </div>

            <!-- 3. 비교 실행 버튼 -->
            <button id="awx-vld-btn-compare" class="awx-vld-btn awx-vld-btn-primary" style="width:100%;">
                교차 비교 실행 (2개 소스)
            </button>

            <!-- 4. 검증 결과 및 작업 권장 안내 리포트 영역 (중복 방지를 위해 단일 영역으로 통합) -->
            <div id="awx-vld-result-box" style="display:none; background:#ffffff; border:1px solid #cbd5e1; border-radius:6px; padding:14px; font-size:12.5px; line-height:1.5;"></div>
        </div>
        `;
    }

    // 스타일 주입
    const styleEl = document.createElement('style');
    styleEl.id = 'awx-validator-styles';
    styleEl.textContent = getStylesheet();
    document.head.appendChild(styleEl);

    // 메인 패널 엘리먼트 생성
    const panel = document.createElement('div');
    panel.id = 'awx-validator-panel';
    panel.style.cssText = `
        position: fixed; top: 50px; right: 20px; width: 550px; max-height: 90vh;
        background: #ffffff; border: 1px solid #0f172a; border-radius: 8px;
        box-shadow: 0 12px 32px rgba(15, 23, 42, 0.22); z-index: 9999999;
        display: flex; flex-direction: column; overflow: hidden;
        transition: opacity 0.18s ease;
    `;

    const ctx = getContext();
    panel.innerHTML = buildUIHTML(ctx, false);
    document.body.appendChild(panel);

    // 이벤트 및 로직 바인딩 함수
    function bindEvents(rootDoc, isPopup = false, popupWin = null) {
        const getEl = id => rootDoc.getElementById(id);

        const headerEl = getEl('awx-vld-header');
        const opacityBarEl = getEl('awx-vld-opacity-bar');
        const bodyWrapEl = getEl('awx-vld-body-wrap');
        const btnPin = getEl('awx-vld-btn-pin');
        const btnClose = getEl('awx-vld-btn-close');
        const btnDetach = getEl('awx-vld-btn-detach');
        const btnOpacityToggle = getEl('awx-vld-btn-opacity-toggle');
        const rangeOpacity = getEl('awx-vld-range-opacity');
        const lblOpacity = getEl('awx-vld-lbl-opacity');
        const inputExclude = getEl('awx-vld-input-exclude');
        const hintExclude = getEl('awx-vld-exclude-hint');
        const btnResetAwx = getEl('awx-vld-btn-reset-awx');
        const btnResetConfig = getEl('awx-vld-btn-reset-config');
        const panelResetConfig = getEl('awx-vld-reset-config-panel');
        const chkResetDetail = getEl('awx-vld-chk-reset-detail');
        const chkResetIncident = getEl('awx-vld-chk-reset-incident');
        const chkResetTarget = getEl('awx-vld-chk-reset-target');
        const btnPage = getEl('awx-vld-btn-page');
        const btnCollectAll = getEl('awx-vld-btn-collect-all');
        const countAwxEl = getEl('awx-vld-count-awx');
        const awxSummaryEl = getEl('awx-vld-awx-summary');
        const txtDetail = getEl('awx-vld-txt-detail');
        const txtIncident = getEl('awx-vld-txt-incident');
        const txtTarget = getEl('awx-vld-txt-target');
        const cntDetail = getEl('awx-vld-cnt-detail');
        const cntIncident = getEl('awx-vld-cnt-incident');
        const cntTarget = getEl('awx-vld-cnt-target');
        const btnCompare = getEl('awx-vld-btn-compare');
        const resultBox = getEl('awx-vld-result-box');

        // 독립 창(Popup)에서 마우스 이탈 시 아래 창이 보이도록 헤더 줄로 자동 축소(미니 모드) 상태 관리
        let popupExpandedWidth = 540;
        let popupExpandedHeight = 840;
        let popupCollapsed = false;
        let popupCollapseTimer = null;

        // 투명도(페이지 내) 및 자동 접기 미니 모드(독립 창) 적용 함수
        function applyOpacity(forcePreview = false) {
            if (btnOpacityToggle) {
                if (state.opacityEnabled) {
                    btnOpacityToggle.classList.add('active');
                    btnOpacityToggle.innerText = isPopup ? '자동 접기 ON' : '투명도 ON';
                } else {
                    btnOpacityToggle.classList.remove('active');
                    btnOpacityToggle.innerText = isPopup ? '자동 접기 OFF' : '투명도 OFF';
                }
            }
            if (rangeOpacity) {
                rangeOpacity.disabled = !state.opacityEnabled;
                rangeOpacity.value = String(state.opacityValue);
            }
            if (lblOpacity) {
                lblOpacity.innerText = `${state.opacityValue}%`;
            }

            // 1) 독립 창(Popup) 모드: 내부 글자를 흐리게 하지 않고(항상 선명도 100%), 마우스 이탈 시 창 높이를 헤더 줄로 축소하여 아래 창을 노출
            if (isPopup && popupWin) {
                rootDoc.body.style.opacity = '1';
                const shouldCollapse = state.opacityEnabled && !state.isMouseInside && !forcePreview;
                if (shouldCollapse) {
                    if (!popupCollapsed) {
                        if (popupWin.outerHeight && popupWin.outerHeight > 220) {
                            popupExpandedWidth = popupWin.outerWidth || 540;
                            popupExpandedHeight = popupWin.outerHeight || 840;
                        }
                        const frameDiff = Math.max(60, (popupWin.outerHeight || 840) - (popupWin.innerHeight || 760));
                        const headerBarsHeight = (headerEl ? headerEl.offsetHeight : 44) + (opacityBarEl ? opacityBarEl.offsetHeight : 32) + 4;
                        popupCollapsed = true;
                        popupWin.awxPopupCollapsed = true;
                        if (bodyWrapEl) bodyWrapEl.style.display = 'none';
                        try {
                            popupWin.resizeTo(popupExpandedWidth, frameDiff + headerBarsHeight);
                        } catch (e) {}
                    }
                } else {
                    if (popupCollapsed) {
                        popupCollapsed = false;
                        popupWin.awxPopupCollapsed = false;
                        if (bodyWrapEl) bodyWrapEl.style.display = 'flex';
                        try {
                            popupWin.resizeTo(popupExpandedWidth, popupExpandedHeight);
                        } catch (e) {}
                    }
                }
                return;
            }

            // 2) 페이지 내 패널 모드: 마우스 진입 시 100%('1'), 마우스 이탈 시 설정 투명도%(기본 30%)로 뒤의 AWX 화면 투과
            if (!panel) return;
            if (!state.opacityEnabled) {
                panel.style.opacity = '1';
                return;
            }
            if (forcePreview) {
                panel.style.opacity = String(state.opacityValue / 100);
                return;
            }
            if (state.isMouseInside) {
                panel.style.opacity = '1';
            } else {
                panel.style.opacity = String(state.opacityValue / 100);
            }
        }

        if (btnOpacityToggle) {
            btnOpacityToggle.onclick = () => {
                state.opacityEnabled = !state.opacityEnabled;
                applyOpacity(false);
            };
        }

        if (rangeOpacity) {
            rangeOpacity.oninput = () => {
                state.opacityValue = Number(rangeOpacity.value);
                if (lblOpacity) lblOpacity.innerText = `${state.opacityValue}%`;
                if (!isPopup) {
                    applyOpacity(true);
                }
            };
            rangeOpacity.onchange = () => {
                applyOpacity(false);
            };
        }

        // 마우스 진입/이탈 감지
        const hoverTarget = isPopup ? rootDoc.documentElement : panel;
        if (hoverTarget) {
            hoverTarget.addEventListener('mouseenter', () => {
                state.isMouseInside = true;
                applyOpacity(false);
            });
            hoverTarget.addEventListener('mouseleave', () => {
                state.isMouseInside = false;
                applyOpacity(false);
            });
        }
        window.awxApplyOpacity = applyOpacity;

        // 상단 고정(Pin) 상태 업데이트 및 포커스 유지 동작
        function applyPinState() {
            if (!btnPin) return;
            if (state.isPinned) {
                btnPin.classList.add('pinned');
                btnPin.innerHTML = `${getPinSvg(true)} <span id="awx-vld-pin-text">상단 고정됨</span>`;
                if (!isPopup) {
                    panel.style.zIndex = '2147483647';
                } else if (popupWin) {
                    popupWin.focus();
                }
            } else {
                btnPin.classList.remove('pinned');
                btnPin.innerHTML = `${getPinSvg(false)} <span id="awx-vld-pin-text">상단 고정</span>`;
                if (!isPopup) {
                    panel.style.zIndex = '9999999';
                }
            }
        }

        if (btnPin) {
            btnPin.onclick = () => {
                state.isPinned = !state.isPinned;
                applyPinState();
            };
        }

        // 독립 팝업 창에서 상단 고정(Pin) 시 포커스 유지 핸들러
        if (isPopup && popupWin) {
            popupWin.addEventListener('blur', () => {
                if (state.isPinned && !popupWin.closed) {
                    setTimeout(() => {
                        try { popupWin.focus(); } catch (e) {}
                    }, 50);
                }
            });
            // 부모 창 클릭 시에도 핀 고정 상태이면 팝업 창을 위로 유지
            window.addEventListener('mousedown', () => {
                if (state.isPinned && popupWin && !popupWin.closed) {
                    setTimeout(() => {
                        try { popupWin.focus(); } catch (e) {}
                    }, 80);
                }
            });
        }

        // 드래그 기능 (페이지 내 패널일 때만 적용)
        if (!isPopup && headerEl) {
            let isDragging = false;
            let startX, startY, initialLeft, initialTop;
            headerEl.addEventListener('mousedown', (e) => {
                if (e.target.closest('button')) return;
                isDragging = true;
                startX = e.clientX;
                startY = e.clientY;
                const rect = panel.getBoundingClientRect();
                initialLeft = rect.left;
                initialTop = rect.top;
                panel.style.right = 'auto';
                panel.style.left = initialLeft + 'px';
                panel.style.top = initialTop + 'px';
            });
            document.addEventListener('mousemove', (e) => {
                if (!isDragging) return;
                const dx = e.clientX - startX;
                const dy = e.clientY - startY;
                panel.style.left = Math.max(10, Math.min(window.innerWidth - panel.offsetWidth - 10, initialLeft + dx)) + 'px';
                panel.style.top = Math.max(10, Math.min(window.innerHeight - panel.offsetHeight - 10, initialTop + dy)) + 'px';
            });
            document.addEventListener('mouseup', () => { isDragging = false; });
        }

        // 닫기 버튼
        btnClose.onclick = () => {
            if (isPopup && popupWin) {
                popupWin.close();
            } else {
                panel.style.display = 'none';
            }
        };

        // 독립 창 분리 / 복귀 버튼
        if (isPopup) {
            btnDetach.onclick = () => {
                if (popupWin) popupWin.close();
                state.isMouseInside = true;
                panel.style.display = 'flex';
                panel.style.opacity = '1';
                updateCountsAndButton();
            };
        } else {
            btnDetach.onclick = () => {
                panel.style.display = 'none';
                state.isMouseInside = true;
                const popup = window.open('', 'AWXHostValidatorPopup', 'width=560,height=840,menubar=no,toolbar=no,location=no,status=no,resizable=yes,scrollbars=yes');
                if (!popup) {
                    alert('팝업 차단이 감지되었습니다. 팝업 허용 후 다시 시도해주세요.');
                    panel.style.display = 'flex';
                    return;
                }
                popup.document.write(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>작업대상 호스트 교차 검증기</title><style>${getStylesheet()}</style></head><body class="awx-vld-popup-root" style="margin:0; padding:0; background:#f3f4f6; display:flex; flex-direction:column; height:100vh; overflow:hidden;">${buildUIHTML(ctx, true)}</body></html>`);
                popup.document.close();
                bindEvents(popup.document, true, popup);
                window.awxPopupWindow = popup;
            };
        }

        // 제외 입력 안내 문구 토글
        inputExclude.oninput = () => {
            state.excludePattern = inputExclude.value.trim();
            if (state.excludePattern) {
                hintExclude.style.display = 'none';
            } else {
                hintExclude.style.display = 'block';
            }
        };

        // 텍스트 실시간 카운트 및 비교 버튼 라벨 갱신
        function updateCountsAndButton() {
            state.detailText = txtDetail.value;
            state.incidentText = txtIncident.value;
            state.targetText = txtTarget.value;

            const detailHosts = parseHostText(state.detailText);
            const incidentHosts = parseHostText(state.incidentText);
            const targetHosts = parseHostText(state.targetText);

            cntDetail.innerText = detailHosts.length;
            cntIncident.innerText = incidentHosts.length;
            cntTarget.innerText = targetHosts.length;
            countAwxEl.innerText = state.collectedHosts.size;

            let sourceCount = 0;
            if (detailHosts.length > 0) sourceCount++;
            if (incidentHosts.length > 0) sourceCount++;
            if (targetHosts.length > 0) sourceCount++;
            if (state.collectedHosts.size > 0) sourceCount++;

            btnCompare.innerText = `교차 비교 실행 (${Math.max(2, sourceCount)}개 소스)`;
        }

        txtDetail.oninput = updateCountsAndButton;
        txtIncident.oninput = updateCountsAndButton;
        txtTarget.oninput = updateCountsAndButton;

        // 톱니바퀴 초기화 대상 설정 패널 토글 및 체크박스 상태 변경 바인딩
        if (btnResetConfig && panelResetConfig) {
            btnResetConfig.onclick = () => {
                state.showResetConfig = !state.showResetConfig;
                panelResetConfig.style.display = state.showResetConfig ? 'block' : 'none';
                btnResetConfig.style.background = state.showResetConfig ? '#e2e8f0' : '#ffffff';
            };
        }

        if (chkResetDetail) {
            chkResetDetail.onchange = () => {
                state.resetTargets.detail = chkResetDetail.checked;
            };
        }
        if (chkResetIncident) {
            chkResetIncident.onchange = () => {
                state.resetTargets.incident = chkResetIncident.checked;
            };
        }
        if (chkResetTarget) {
            chkResetTarget.onchange = () => {
                state.resetTargets.target = chkResetTarget.checked;
            };
        }

        // 수집 초기화 (AWX 수집 초기화 + 톱니바퀴 설정에 따라 2번 비교 대상 호스트 입력 연동 초기화)
        btnResetAwx.onclick = () => {
            state.collectedHosts.clear();
            state.excludedHosts = [];
            state.rawCount = 0;
            awxSummaryEl.innerHTML = '';

            if (state.resetTargets.detail) {
                txtDetail.value = '';
                state.detailText = '';
            }
            if (state.resetTargets.incident) {
                txtIncident.value = '';
                state.incidentText = '';
            }
            if (state.resetTargets.target) {
                txtTarget.value = '';
                state.targetText = '';
            }

            // 비교 결과창 초기화
            if (resultBox) {
                resultBox.style.display = 'none';
                resultBox.innerHTML = '';
            }

            updateCountsAndButton();
        };

        // 1) 화면 UI에서 현재 페이지 수집 (중복 호스트 수집 시 카운트가 증가하지 않도록 중복 제거)
        btnPage.onclick = () => {
            const filters = getExcludeFilters(inputExclude.value.trim());
            document.querySelectorAll('table tbody tr').forEach(tr => {
                const nameEl = tr.querySelector('td[data-label="이름"] a, td[data-label="Name"] a, td a[href*="/hosts/"]') || tr.querySelector('td:nth-child(2) a');
                if (nameEl) {
                    const h = nameEl.innerText.trim().toLowerCase();
                    if (h) {
                        if (isHostExcluded(h, filters)) {
                            state.collectedHosts.delete(h);
                            if (!state.excludedHosts.includes(h)) {
                                state.excludedHosts.push(h);
                            }
                        } else {
                            state.excludedHosts = state.excludedHosts.filter(x => x !== h);
                            state.collectedHosts.add(h);
                        }
                    }
                }
            });
            state.rawCount = state.collectedHosts.size + state.excludedHosts.length;
            renderAwxSummary();
            updateCountsAndButton();
        };

        // 2) AWX API 전수 수집
        async function runCollectAll() {
            btnCollectAll.disabled = true;
            btnCollectAll.innerText = '수집 중...';
            const currentContext = getContext();
            const filters = getExcludeFilters(inputExclude.value.trim());

            state.collectedHosts.clear();
            state.excludedHosts = [];

            try {
                let nextUrl = currentContext.url;
                while (nextUrl) {
                    const res = await fetch(nextUrl);
                    const data = await res.json();
                    if (data.results) {
                        data.results.forEach(item => {
                            const name = item.name.trim().toLowerCase();
                            if (isHostExcluded(name, filters)) {
                                if (!state.excludedHosts.includes(name)) {
                                    state.excludedHosts.push(name);
                                }
                            } else {
                                state.collectedHosts.add(name);
                            }
                        });
                    }
                    nextUrl = data.next;
                }
                state.rawCount = state.collectedHosts.size + state.excludedHosts.length;
                renderAwxSummary();
            } catch (err) {
                awxSummaryEl.innerHTML = `<span style="color:#dc2626; font-weight:600;">수집 실패: ${err.message}</span>`;
            } finally {
                btnCollectAll.disabled = false;
                btnCollectAll.innerText = '현재 인벤토리 전체 수집';
                updateCountsAndButton();
            }
            return state.collectedHosts.size;
        }

        btnCollectAll.onclick = runCollectAll;
        window.awxRunCollectAll = runCollectAll;

        // AWX 수집 제외 요약 출력 렌더링
        function renderAwxSummary() {
            if (state.rawCount === 0) {
                awxSummaryEl.innerHTML = '';
                return;
            }
            const exCount = state.excludedHosts.length;
            if (exCount > 0) {
                awxSummaryEl.innerHTML = `
                    <div style="font-weight:700; color:#b45309; margin-top:4px; font-size:12px;">
                        ${state.rawCount}대 중 ${exCount}대 제외됨 (수집 대상: ${state.collectedHosts.size}대)
                    </div>
                    <div class="awx-vld-mono" style="font-size:11.5px; color:#374151; margin-top:3px; word-break:break-all; background:#ffffff; padding:5px 7px; border-radius:4px; border:1px solid #e5e7eb;">
                        ${state.excludedHosts.join(' ')}
                    </div>
                `;
            } else {
                awxSummaryEl.innerHTML = `<div style="color:#111827; margin-top:3px; font-weight:500;">${state.collectedHosts.size}대 수집 완료 (제외 0대)</div>`;
            }
        }

        // 3) 교차 비교 실행 함수
        function doCompare() {
            const detailList = parseHostText(txtDetail.value);
            const incidentList = parseHostText(txtIncident.value);
            const targetList = parseHostText(txtTarget.value);
            const awxList = Array.from(state.collectedHosts);

            // 필수 입력 검증
            if (detailList.length === 0 || incidentList.length === 0) {
                resultBox.style.display = 'block';
                resultBox.style.background = '#fef2f2';
                resultBox.style.borderColor = '#fca5a5';
                resultBox.innerHTML = `
                    <div style="font-weight:700; color:#dc2626; margin-bottom:4px; font-size:13px;">오류: 필수 입력 항목 누락</div>
                    <div style="color:#991b1b; font-size:12px;">
                        상세목록현황과 인시던트 호스트는 필수 입력 대상입니다. 두 입력창에 호스트를 입력해주세요.
                    </div>
                `;
                return;
            }

            // 활성 소스 목록 구성
            const activeSources = [];
            activeSources.push({ id: 'detail', name: '상세목록현황', hosts: new Set(detailList) });
            activeSources.push({ id: 'incident', name: '인시던트', hosts: new Set(incidentList) });

            if (targetList.length > 0) {
                activeSources.push({ id: 'target', name: '작업대상', hosts: new Set(targetList) });
            }
            if (awxList.length > 0) {
                activeSources.push({ id: 'awx', name: 'AWX 수집 목록', hosts: new Set(awxList) });
            }

            // 전체 합집합
            const unionSet = new Set();
            activeSources.forEach(s => s.hosts.forEach(h => unionSet.add(h)));

            // 각 소스별 차이 분석
            let isAllMatch = true;
            const diffs = [];

            activeSources.forEach(source => {
                const missingInSource = [];
                unionSet.forEach(h => {
                    if (!source.hosts.has(h)) missingInSource.push(h);
                });

                if (missingInSource.length > 0) {
                    isAllMatch = false;
                    diffs.push({
                        sourceName: source.name,
                        sourceCount: source.hosts.size,
                        missingCount: missingInSource.length,
                        missingList: missingInSource
                    });
                }
            });

            // 특수 분석: AWX 1000대 중 작업 대상(상세/인시던트)에 없는 호스트 별도 분석
            let awxSpecialReport = '';
            if (state.collectedHosts.size > 0) {
                const targetRef = new Set([...detailList, ...incidentList]);
                const notInTargetFromAwx = [];
                state.collectedHosts.forEach(h => {
                    if (!targetRef.has(h)) notInTargetFromAwx.push(h);
                });

                if (notInTargetFromAwx.length > 0) {
                    isAllMatch = false;
                    const toggleId = 'awx-toggle-' + Date.now();
                    awxSpecialReport = `
                        <div style="margin-top:8px; padding:9px 11px; background:#fff1f2; border:1px solid #fecdd3; border-radius:5px;">
                            <div style="display:flex; justify-content:space-between; align-items:center;">
                                <span style="font-weight:700; color:#be123c; font-size:12px;">대상에 없음: ${notInTargetFromAwx.length}대 (AWX ${state.collectedHosts.size}대 중 ${targetRef.size}대만 존재)</span>
                                <button id="btn-${toggleId}" class="awx-vld-btn awx-vld-btn-secondary awx-vld-btn-sm" style="border-color:#f43f5e; color:#be123c;">명단 펼치기</button>
                            </div>
                            <div id="${toggleId}" class="awx-vld-mono" style="display:none; margin-top:7px; max-height:120px; overflow-y:auto; font-size:11.5px; color:#881337; background:#ffffff; padding:8px; border:1px solid #fda4af; border-radius:4px; word-break:break-all;">
                                ${notInTargetFromAwx.join(' ')}
                            </div>
                        </div>
                    `;
                }
            }

            resultBox.style.display = 'block';
            const guideHosts = getGuideTargetHosts(txtIncident.value, txtTarget.value, txtDetail.value);
            const guideMessage = isAllMatch
                ? '인시던트의 작업대상을 복사해서 붙여넣기를 권장합니다.vim [작업대상 파일] -&gt; esc -&gt; :set paste 입력(붙여넣기 모드) -&gt; 붙여넣기'
                : '소스 간 불일치가 감지되었습니다. 작업대상을 다시 확인하고 일치시킨 후 작업을 진행하시기 바랍니다.';
            const guideTextColor = isAllMatch ? '#0f172a' : '#b91c1c';
            const guideBorderColor = isAllMatch ? '#cbd5e1' : '#fca5a5';
            const guideBgColor = isAllMatch ? '#f8fafc' : '#fff1f2';

            const mandatoryCommentHtml = `
                <div style="margin-top:10px; padding:9px 11px; background:${guideBgColor}; border:1px solid ${guideBorderColor}; border-radius:5px; color:${guideTextColor};">
                    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:4px;">
                        <span style="font-size:11.5px; font-weight:700; color:${isAllMatch ? '#334155' : '#991b1b'};">작업 권장 안내</span>
                        <button id="awx-vld-btn-guide-toggle" class="awx-vld-btn awx-vld-btn-secondary awx-vld-btn-sm" style="padding:2px 7px; font-size:11px;">펼치기</button>
                    </div>
                    <div id="awx-vld-guide-msg" class="awx-vld-mono" style="font-size:11.5px; font-weight:${isAllMatch ? '500' : '700'}; word-break:break-all;">
                        ${guideMessage}
                    </div>
                    <div id="awx-vld-guide-targets" style="display:none; margin-top:7px; padding-top:7px; border-top:1px dashed ${guideBorderColor};">
                        <div style="font-size:11px; font-weight:700; color:#334155; margin-bottom:4px;">작업대상 호스트 명단 (<span id="awx-vld-guide-target-count">${guideHosts.length}</span>대)</div>
                        <div id="awx-vld-guide-target-list" class="awx-vld-mono" style="max-height:120px; overflow-y:auto; background:#ffffff; border:1px solid #cbd5e1; border-radius:4px; padding:7px; font-size:11.5px; color:#111827; white-space:pre-wrap; word-break:break-all;">${guideHosts.join('\n')}</div>
                    </div>
                </div>
            `;

            if (isAllMatch) {
                resultBox.style.background = '#f0fdf4';
                resultBox.style.borderColor = '#86efac';
                resultBox.innerHTML = `
                    <div style="font-weight:700; color:#15803d; font-size:13.5px; margin-bottom:6px;">
                        [MATCH] 비교 대상 전수 일치 확인 완료
                    </div>
                    <div style="color:#166534; font-size:12px;">
                        • 참여 소스: <b>${activeSources.map(s => `${s.name}(${s.hosts.size}대)`).join(', ')}</b><br>
                        • 불일치 및 누락 호스트: <b>0대</b>
                        ${state.excludedHosts.length > 0 ? `<br>• AWX 제외: <b>${state.rawCount}대 중 ${state.excludedHosts.length}대 제외됨</b> (${state.excludedHosts.join(' ')})` : ''}
                    </div>
                    ${mandatoryCommentHtml}
                `;
            } else {
                resultBox.style.background = '#fef2f2';
                resultBox.style.borderColor = '#fca5a5';
                let diffHtml = '';
                diffs.forEach((d, idx) => {
                    const tid = `diff-toggle-${idx}-${Date.now()}`;
                    diffHtml += `
                        <div style="margin-top:6px; padding:8px 10px; background:#ffffff; border:1px solid #fecaca; border-radius:5px;">
                            <div style="display:flex; justify-content:space-between; align-items:center;">
                                <span style="font-weight:700; color:#991b1b; font-size:12px;">• ${d.sourceName}: 누락 ${d.missingCount}대 (보유: ${d.sourceCount}대)</span>
                                <button id="btn-${tid}" class="awx-vld-btn awx-vld-btn-secondary awx-vld-btn-sm">명단 펼치기</button>
                            </div>
                            <div id="${tid}" class="awx-vld-mono" style="display:none; margin-top:6px; max-height:100px; overflow-y:auto; font-size:11.5px; color:#b91c1c; background:#fef2f2; padding:7px; border-radius:4px; word-break:break-all;">
                                ${d.missingList.join(' ')}
                            </div>
                        </div>
                    `;
                });

                resultBox.innerHTML = `
                    <div style="font-weight:700; color:#dc2626; font-size:13.5px; margin-bottom:4px;">
                        [MISMATCH] 소스 간 불일치 감지 (${activeSources.length}개 소스 비교)
                    </div>
                    <div style="color:#7f1d1d; font-size:12px; margin-bottom:6px;">
                        참여 소스 현황: ${activeSources.map(s => `<b>${s.name}</b>(${s.hosts.size}대)`).join(' vs ')}
                    </div>
                    ${awxSpecialReport}
                    ${diffHtml}
                    ${mandatoryCommentHtml}
                `;
            }

            // 결과 영역 내 명단 펼치기 토글 바인딩
            resultBox.querySelectorAll('button[id^="btn-"]').forEach(btn => {
                btn.onclick = () => {
                    const targetId = btn.id.replace('btn-', '');
                    const targetEl = rootDoc.getElementById(targetId);
                    if (targetEl) {
                        const isHidden = targetEl.style.display === 'none';
                        targetEl.style.display = isHidden ? 'block' : 'none';
                        btn.innerText = isHidden ? '명단 접기' : '명단 펼치기';
                    }
                };
            });

            // 작업 권장 안내 [펼치기] 버튼 바인딩
            const btnGuideToggleEl = getEl('awx-vld-btn-guide-toggle');
            const guideTargetsEl = getEl('awx-vld-guide-targets');
            if (btnGuideToggleEl && guideTargetsEl) {
                btnGuideToggleEl.onclick = () => {
                    const isHidden = guideTargetsEl.style.display === 'none';
                    guideTargetsEl.style.display = isHidden ? 'block' : 'none';
                    btnGuideToggleEl.innerText = isHidden ? '접기' : '펼치기';
                };
            }
        }

        btnCompare.onclick = doCompare;
        window.awxDoCompare = doCompare;

        updateCountsAndButton();
        renderAwxSummary();
        applyPinState();
        applyOpacity(false);
    }

    bindEvents(document, false, null);
})();
