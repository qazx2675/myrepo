(function() {
    // 기본 제외 호스트 패턴 (실제 환경 소스 진행 시 자동 추가되는 6~7대 필수 서버를 미리 지정 가능)
    const DEFAULT_EXCLUDE_HOSTS = "prd-db-0295|prd-db-0296|prd-db-0297|prd-db-0298|prd-db-0299|prd-db-0300";
    const AGENT_BASE = "http://127.0.0.1:18080";

    // AgentBridge: WebSocket 기반 통신 (AWX CSP connect-src 'self' ws: wss: 100% 준수)
    // 브라우저의 엄격한 CSP 환경에서도 127.0.0.1:18080 Go 에이전트와 양방향 RPC 및 실시간 이벤트 통신 수행
    class AgentBridge {
        constructor(wsUrl = 'ws://127.0.0.1:18080/ws', httpUrl = 'http://127.0.0.1:18080') {
            this.wsUrl = wsUrl;
            this.httpUrl = httpUrl;
            this.ws = null;
            this.reqId = 1;
            this.pendingReqs = new Map();
            this.eventListeners = new Map();
            this.connected = false;
            this.connectPromise = null;
            this.reconnectTimer = null;
        }

        connect() {
            if (this.connected && this.ws && this.ws.readyState === WebSocket.OPEN) {
                return Promise.resolve(true);
            }
            if (this.connectPromise) return this.connectPromise;

            this.connectPromise = new Promise((resolve) => {
                try {
                    this.ws = new WebSocket(this.wsUrl);
                    this.ws.onopen = () => {
                        this.connected = true;
                        this.connectPromise = null;
                        resolve(true);
                    };
                    this.ws.onmessage = (event) => {
                        try {
                            const msg = JSON.parse(event.data);
                            if (msg.id && this.pendingReqs.has(msg.id)) {
                                const { resolve: res, reject: rej } = this.pendingReqs.get(msg.id);
                                this.pendingReqs.delete(msg.id);
                                res(msg);
                            } else if (msg.event) {
                                const listeners = this.eventListeners.get(msg.event) || [];
                                listeners.forEach(fn => { try { fn(msg.data, msg); } catch(e) {} });
                                const allListeners = this.eventListeners.get('*') || [];
                                allListeners.forEach(fn => { try { fn(msg.data, msg); } catch(e) {} });
                            }
                        } catch(e) {}
                    };
                    this.ws.onclose = () => {
                        this.connected = false;
                        this.connectPromise = null;
                        this.scheduleReconnect();
                    };
                    this.ws.onerror = () => {
                        this.connected = false;
                        this.connectPromise = null;
                        resolve(false);
                    };
                } catch(e) {
                    this.connected = false;
                    this.connectPromise = null;
                    resolve(false);
                }
            });
            return this.connectPromise;
        }

        scheduleReconnect() {
            if (this.reconnectTimer) return;
            this.reconnectTimer = setTimeout(() => {
                this.reconnectTimer = null;
                this.connect();
            }, 2000);
        }

        on(event, callback) {
            if (!this.eventListeners.has(event)) {
                this.eventListeners.set(event, []);
            }
            this.eventListeners.get(event).push(callback);
        }

        async request(method, path, body = null, timeoutMs = 6000) {
            await this.connect();
            if (!this.connected) {
                return this.fallbackFetch(method, path, body);
            }
            const id = 'req_' + (this.reqId++);
            return new Promise((resolve, reject) => {
                const timer = setTimeout(() => {
                    if (this.pendingReqs.has(id)) {
                        this.pendingReqs.delete(id);
                        reject(new Error(`WebSocket request to ${path} timed out after ${timeoutMs}ms`));
                    }
                }, timeoutMs);

                this.pendingReqs.set(id, {
                    resolve: (msg) => {
                        clearTimeout(timer);
                        const isOk = msg.status >= 200 && msg.status < 300;
                        resolve({
                            ok: isOk,
                            status: msg.status,
                            statusText: isOk ? 'OK' : 'Error',
                            json: async () => msg.data,
                            text: async () => (typeof msg.data === 'string' ? msg.data : JSON.stringify(msg.data))
                        });
                    },
                    reject: (err) => {
                        clearTimeout(timer);
                        reject(err);
                    }
                });

                const payload = { id, method: method || 'GET', path };
                if (body !== null && body !== undefined) payload.body = body;
                this.ws.send(JSON.stringify(payload));
            });
        }

        async fallbackFetch(method, path, body) {
            const url = path.startsWith('http') ? path : `${this.httpUrl}${path}`;
            const opts = { method: method || 'GET', headers: { 'Content-Type': 'application/json' } };
            if (body) opts.body = typeof body === 'string' ? body : JSON.stringify(body);
            return await fetch(url, opts);
        }
    }

    const agentBridge = window.__awxAgentBridge || new AgentBridge();
    window.__awxAgentBridge = agentBridge;

    async function agentFetch(url, options = {}) {
        const method = options.method || 'GET';
        let path = url;
        if (path.startsWith(AGENT_BASE)) {
            path = path.substring(AGENT_BASE.length);
        }
        let body = null;
        if (options.body) {
            try {
                body = typeof options.body === 'string' ? JSON.parse(options.body) : options.body;
            } catch(e) {
                body = options.body;
            }
        }
        return await agentBridge.request(method, path, body);
    }

    // 기존 패널 및 스타일시트가 열려 있다면 제거 후 최신 상태로 렌더링
    const prevPanel = document.getElementById('awx-validator-panel');
    if (prevPanel) prevPanel.remove();
    const prevStyle = document.getElementById('awx-validator-styles');
    if (prevStyle) prevStyle.remove();

    // 독립 실행 앱(--app 모드) 여부 감지
    const isStandaloneApp = Boolean(window.__AWX_STANDALONE_APP__);

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
    state.isStandalone = isStandaloneApp;
    if (typeof state.isPinned !== 'boolean') state.isPinned = false;
    if (typeof state.guideExpanded !== 'boolean') state.guideExpanded = false;
    if (typeof state.opacityEnabled !== 'boolean') state.opacityEnabled = !isStandaloneApp;
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

    // 신규 크로스체크 연동 상태 필드 ('single' | 'worker' | 'checker')
    if (typeof state.activeTab !== 'string') state.activeTab = 'single';
    if (typeof state.incidentNo !== 'string') state.incidentNo = '';
    if (typeof state.specialNotes !== 'string') state.specialNotes = '';
    if (typeof state.selectedCheckerIp !== 'string') state.selectedCheckerIp = '';
    if (!state.agentInfo) state.agentInfo = { connected: false, ip: '', name: '', registered: false };
    if (!Array.isArray(state.usersList)) state.usersList = [];
    if (!state.currentSession) state.currentSession = null;
    if (!Array.isArray(state.pendingSessions)) state.pendingSessions = [];
    if (!state.loadedCheckerSession) state.loadedCheckerSession = null;
    if (typeof state.chkDetailText !== 'string') state.chkDetailText = '';
    if (typeof state.chkIncidentText !== 'string') state.chkIncidentText = '';
    if (typeof state.chkTargetText !== 'string') state.chkTargetText = '';

    // 현재 접속 중인 AWX 화면 컨텍스트 분석 (인벤토리 ID, 그룹 ID, 독립 앱 모드, 또는 전역)
    function getContext() {
        const hash = window.location.hash || '';
        const invMatch = hash.match(/\/inventories\/(?:inventory|smart_inventory)\/(\d+)/);
        if (invMatch) {
            return {
                url: `/api/v2/inventories/${invMatch[1]}/hosts/?page_size=200`,
                inventoryId: invMatch[1],
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
        if (isStandaloneApp) {
            const params = new URLSearchParams(window.location.search);
            const invId = params.get('inventory') || '3';
            return {
                url: `${AGENT_BASE}/api/awx/hosts?inventory=${invId}`,
                inventoryId: invId,
                label: `인벤토리 #${invId}`,
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
            let cleanLine = line.replace(/\s*\([^)]*\)/g, '');
            const entries = cleanLine.split(/[,;\t]+/);
            entries.forEach(entry => {
                const trimmed = entry.trim();
                if (!trimmed) return;
                if (trimmed.includes('/')) {
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

    // 작업 권장 안내 펼치기 시 표시할 작업대상 호스트 추출
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
            font-size: 13px;
            font-weight: 700;
            padding: 10px 16px;
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
        .awx-vld-badge-rec {
            display: inline-block;
            background: #dbeafe;
            color: #1e40af;
            font-size: 11px;
            font-weight: 700;
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
        /* 탭 바 스타일 */
        .awx-vld-tab-bar {
            display: flex;
            background: #0f172a;
            border-bottom: 1px solid #334155;
            padding: 0 8px;
            gap: 4px;
            user-select: none;
        }
        .awx-vld-tab-btn {
            flex: 1;
            background: #1e293b;
            color: #94a3b8;
            border: none;
            border-bottom: 2px solid transparent;
            padding: 8px 6px;
            font-size: 11.5px;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.15s ease;
            text-align: center;
            border-top-left-radius: 4px;
            border-top-right-radius: 4px;
            white-space: nowrap;
        }
        .awx-vld-tab-btn:hover {
            color: #f1f5f9;
            background: #334155;
        }
        .awx-vld-tab-btn.active {
            color: #ffffff;
            background: #2563eb;
            border-bottom: 2px solid #60a5fa;
            font-weight: 700;
        }
        .awx-vld-tab-badge {
            display: inline-block;
            background: #ef4444;
            color: #ffffff;
            font-size: 10px;
            font-weight: 700;
            padding: 1px 5px;
            border-radius: 8px;
            margin-left: 4px;
        }
        `;
    }

    function getPinSvg(isPinned) {
        const transform = isPinned ? 'rotate(0)' : 'rotate(45 8 8)';
        const fill = isPinned ? 'currentColor' : 'none';
        return `<svg width="12" height="12" viewBox="0 0 16 16" fill="${fill}" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" style="transform:${transform}; transition:transform 0.15s ease;"><path d="M9.5 2.5L13.5 6.5L12 8L10 6L6.5 9.5L8.5 11.5L7 13L4.5 10.5L2 13L3 14L2 14L2 13L4.5 10.5L2 8L3.5 6.5L5.5 8.5L9 5L7 3L8.5 1.5Z"/></svg>`;
    }

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
        const detachLabel = isPopup ? '페이지 내로 복귀' : (isStandaloneApp ? '새로고침' : '독립 창으로 분리');
        const detachTitle = isPopup ? '페이지 내로 복귀' : (isStandaloneApp ? '연결 및 세션 상태 새로고침' : '패널 크기 맞춤 독립 브라우저 팝업 창으로 띄우기');

        return `
        <div id="awx-vld-header" style="background:#0f172a; color:#f8fafc; padding:10px 14px; display:flex; justify-content:space-between; align-items:center; gap:8px; cursor:${(isPopup || isStandaloneApp) ? 'default' : 'move'}; user-select:none; border-bottom:1px solid #1e293b;">
            <div style="display:flex; align-items:center; gap:7px; flex-shrink:0;">
                <span style="font-size:13px; font-weight:700; letter-spacing:-0.3px; color:#ffffff; white-space:nowrap;">작업대상 호스트 교차 검증기</span>
                <span id="awx-vld-ctx-badge" style="background:#1e293b; color:#cbd5e1; border:1px solid #334155; font-size:11px; padding:1px 6px; border-radius:4px; font-weight:600; white-space:nowrap;">${ctx.label}</span>
            </div>
            <div style="display:flex; align-items:center; gap:5px; flex-shrink:0;">
                <button id="awx-vld-btn-pin" class="${pinClass}" title="창을 항상 가장 위에 고정 (포커스 유지)">
                    ${getPinSvg(state.isPinned)}
                    <span id="awx-vld-pin-text">${pinLabel}</span>
                </button>
                <button id="awx-vld-btn-detach" class="awx-vld-header-btn" title="${detachTitle}">${detachLabel}</button>
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

        <!-- 3개 모드 탭 바 -->
        <div id="awx-vld-tab-bar" class="awx-vld-tab-bar">
            <button id="awx-vld-tab-single" class="awx-vld-tab-btn ${state.activeTab === 'single' ? 'active' : ''}">내 PC 단독 확인</button>
            <button id="awx-vld-tab-worker" class="awx-vld-tab-btn ${state.activeTab === 'worker' ? 'active' : ''}">크로스체크 요청 (작업자)</button>
            <button id="awx-vld-tab-checker" class="awx-vld-tab-btn ${state.activeTab === 'checker' ? 'active' : ''}">
                크로스체크 확인 (확인자)
                <span id="awx-vld-badge-pending-count" class="awx-vld-tab-badge" style="display:${state.pendingSessions.length > 0 ? 'inline-block' : 'none'};">${state.pendingSessions.length}</span>
            </button>
        </div>

        <div id="awx-vld-body-wrap" style="padding:14px; overflow-y:auto; flex:1; display:flex; flex-direction:column; gap:12px; background:#f3f4f6;">
            
            <!-- [탭 1] 내 PC 단독 확인 (기존 TC-01 ~ TC-12 전담) -->
            <div id="awx-vld-pane-single" style="display:${state.activeTab === 'single' ? 'flex' : 'none'}; flex-direction:column; gap:12px;">
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

                <!-- 4. 검증 결과 및 작업 권장 안내 리포트 영역 -->
                <div id="awx-vld-result-box" style="display:none; background:#ffffff; border:1px solid #cbd5e1; border-radius:6px; padding:14px; font-size:12.5px; line-height:1.5;"></div>
            </div>

            <!-- [탭 2] 크로스체크 요청 (작업자 전용 탭) -->
            <div id="awx-vld-pane-worker" style="display:${state.activeTab === 'worker' ? 'flex' : 'none'}; flex-direction:column; gap:12px;">
                <!-- 에이전트 연결 상태 카드 -->
                <div id="awx-vld-worker-agent-bar" style="font-size:12px; padding:8px 12px; border-radius:5px; background:#1e293b; color:#e2e8f0; display:flex; justify-content:space-between; align-items:center;">
                    <div id="awx-vld-worker-agent-msg">에이전트 연결 확인 중...</div>
                    <button id="awx-vld-btn-check-agent" class="awx-vld-btn awx-vld-btn-secondary awx-vld-btn-sm" style="padding:2px 7px;">상태 갱신</button>
                </div>

                <!-- 요청 정보 입력 카드 -->
                <div class="awx-vld-card" style="display:flex; flex-direction:column; gap:11px;">
                    <div class="awx-vld-section-title">크로스체크 확인 요청 정보</div>

                    <!-- 인시던트 번호 [권장] -->
                    <div>
                        <label style="display:block; font-size:12px; font-weight:700; color:#111827; margin-bottom:4px;">
                            인시던트 번호 <span class="awx-vld-badge-rec">[권장]</span>
                        </label>
                        <input id="awx-vld-worker-incident-no" class="awx-vld-input awx-vld-mono" type="text" value="${state.incidentNo}" placeholder="예: INC-2026-00123">
                        <div id="awx-vld-worker-incident-hint" style="font-size:11.5px; color:#6b7280; margin-top:3px; ${state.incidentNo ? 'display:none;' : ''}">인시던트 진행 시 번호 입력을 권장합니다</div>
                    </div>

                    <!-- 특이사항 전달 메모 [선택] -->
                    <div>
                        <label style="display:block; font-size:12px; font-weight:700; color:#111827; margin-bottom:4px;">
                            특이사항 전달 메모 <span class="awx-vld-badge-opt">[선택]</span>
                        </label>
                        <textarea id="awx-vld-worker-notes" class="awx-vld-textarea" placeholder="확인자에게 전달할 특이사항이나 메모를 입력하세요 (예: DB 6대 제외 확인 필요, 23시 작업)" style="height:62px; resize:vertical;">${state.specialNotes}</textarea>
                    </div>

                    <!-- 크로스체크 확인자 지정 [필수] -->
                    <div>
                        <label style="display:block; font-size:12px; font-weight:700; color:#111827; margin-bottom:4px;">
                            크로스체크 확인자 지정 <span class="awx-vld-badge-req">[필수]</span>
                        </label>
                        <select id="awx-vld-sel-checker" class="awx-vld-input" style="cursor:pointer;">
                            <option value="">확인자를 선택하세요</option>
                        </select>
                    </div>

                    <!-- 전송 항목 현황 (단독 탭과 실시간 공유) -->
                    <div style="font-size:11.5px; line-height:1.5; color:#334155; background:#f8fafc; padding:8px 10px; border-radius:5px; border:1px solid #e2e8f0;">
                        <b>전송할 호스트 현황 (단독 확인 탭과 연동)</b><br>
                        • AWX 수집: <b id="awx-vld-w-cnt-awx" class="awx-vld-mono">${state.collectedHosts.size}</b>대 | 
                        상세목록: <b id="awx-vld-w-cnt-detail" class="awx-vld-mono">0</b>대 | 
                        인시던트: <b id="awx-vld-w-cnt-incident" class="awx-vld-mono">0</b>대 | 
                        작업대상: <b id="awx-vld-w-cnt-target" class="awx-vld-mono">0</b>대
                    </div>

                    <!-- 요청 발송 버튼 -->
                    <button id="awx-vld-btn-send-request" class="awx-vld-btn awx-vld-btn-primary" style="width:100%;">
                        크로스체크 확인 요청 발송
                    </button>
                </div>

                <!-- 진행 중인 세션 상태 및 확인자 즉시 변경(쿨타임 0초) 카드 -->
                <div id="awx-vld-worker-session-card" class="awx-vld-card" style="display:${state.currentSession ? 'flex' : 'none'}; flex-direction:column; gap:10px;">
                    <div style="display:flex; justify-content:space-between; align-items:center; border-bottom:1px solid #e2e8f0; padding-bottom:8px;">
                        <span class="awx-vld-section-title">요청 진행 상태: <b id="awx-vld-w-session-status" class="awx-vld-mono" style="color:#2563eb;">-</b></span>
                        <span id="awx-vld-w-session-id" class="awx-vld-mono" style="font-size:11px; color:#64748b;"></span>
                    </div>

                    <div style="font-size:12px; color:#1e293b;">
                        현재 확인자: <b id="awx-vld-w-assigned-checker" style="color:#0f172a;">-</b>
                    </div>

                    <!-- 쿨타임 없는 확인자 즉시 변경 바 -->
                    <div id="awx-vld-w-reassign-box" style="display:flex; align-items:center; gap:6px; background:#f1f5f9; padding:8px; border-radius:5px;">
                        <span style="font-size:11.5px; font-weight:600; color:#334155; white-space:nowrap;">확인자 즉시 변경:</span>
                        <select id="awx-vld-sel-reassign" class="awx-vld-input" style="flex:1; padding:4px 8px; font-size:11.5px;">
                            <option value="">변경할 확인자 선택</option>
                        </select>
                        <button id="awx-vld-btn-reassign" class="awx-vld-btn awx-vld-btn-secondary awx-vld-btn-sm" style="white-space:nowrap;">즉시 변경</button>
                    </div>

                    <!-- 최종 결과 표시 박스 -->
                    <div id="awx-vld-w-result-report" style="display:none; font-size:12px; line-height:1.5;"></div>
                </div>
            </div>

            <!-- [탭 3] 크로스체크 확인 (확인자 전용 탭) -->
            <div id="awx-vld-pane-checker" style="display:${state.activeTab === 'checker' ? 'flex' : 'none'}; flex-direction:column; gap:12px;">
                <!-- 수신 대기 요청 목록 카드 -->
                <div class="awx-vld-card">
                    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:8px;">
                        <span class="awx-vld-section-title">확인할 요청 선택</span>
                        <button id="awx-vld-btn-refresh-pending" class="awx-vld-btn awx-vld-btn-secondary awx-vld-btn-sm">새로고침</button>
                    </div>
                    <select id="awx-vld-sel-pending-sessions" class="awx-vld-input" style="cursor:pointer;">
                        <option value="">도착한 요청을 선택하세요</option>
                    </select>
                </div>

                <!-- 요청 상세 브리핑 & 블라인드 카드 & 확인자 입력 영역 (선택 시 노출) -->
                <div id="awx-vld-checker-content" style="display:${state.loadedCheckerSession ? 'flex' : 'none'}; flex-direction:column; gap:12px;">
                    
                    <!-- 1. 작업자 브리핑 카드 -->
                    <div class="awx-vld-card" style="background:#f8fafc; border-color:#cbd5e1;">
                        <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:6px;">
                            <span style="font-size:12.5px; font-weight:700; color:#1e293b;">
                                작업자: <span id="awx-vld-chk-worker-name" style="color:#2563eb;">-</span> 
                                (<span id="awx-vld-chk-worker-ip" class="awx-vld-mono">-</span>)
                            </span>
                            <span id="awx-vld-chk-status-badge" style="font-size:11px; font-weight:700; padding:2px 7px; border-radius:4px; background:#e2e8f0;">-</span>
                        </div>
                        <div style="font-size:12px; color:#334155; margin-bottom:4px;">
                            인시던트 번호: <b id="awx-vld-chk-incident-no" class="awx-vld-mono" style="color:#111827;">-</b>
                        </div>
                        <div id="awx-vld-chk-notes-box" style="display:none; font-size:11.5px; background:#fef9c3; border:1px solid #fde047; padding:7px 9px; border-radius:4px; color:#854d0e; margin-top:5px; word-break:break-all;">
                            <b>특이사항:</b> <span id="awx-vld-chk-special-notes"></span>
                        </div>
                        <div id="awx-vld-chk-awx-info" style="font-size:11.5px; color:#475569; margin-top:5px;"></div>
                    </div>

                    <!-- 2. 작업자 호스트 블라인드 요약 (앞 3대만 노출 + 외 N대 블라인드) -->
                    <div class="awx-vld-card" style="background:#ffffff; border-color:#e2e8f0;">
                        <div style="font-size:12px; font-weight:700; color:#475569; margin-bottom:6px; display:flex; justify-content:space-between;">
                            <span>작업자 입력 요약 (복사 방지 블라인드 적용)</span>
                            <span style="font-size:11px; color:#64748b;">(비교 실행 시 전체 대조)</span>
                        </div>
                        <div style="display:flex; flex-direction:column; gap:4px; font-size:11.5px;">
                            <div id="awx-vld-chk-blind-detail" style="color:#1e293b;"></div>
                            <div id="awx-vld-chk-blind-incident" style="color:#1e293b;"></div>
                            <div id="awx-vld-chk-blind-target" style="color:#1e293b;"></div>
                        </div>
                    </div>

                    <!-- 3. 확인자 직접 입력란 (작업자 활성 항목과 동적 일치) -->
                    <div class="awx-vld-card" style="display:flex; flex-direction:column; gap:11px;">
                        <div class="awx-vld-section-title">확인자 호스트 직접 입력</div>

                        <!-- 확인자 상세목록현황 [필수] -->
                        <div>
                            <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:4px;">
                                <span style="font-size:12px; font-weight:700; color:#111827;">상세목록현황 호스트 <span class="awx-vld-badge-req">[필수]</span></span>
                                <span style="font-size:11.5px; color:#4b5563;">인식: <b id="awx-vld-chk-cnt-detail" class="awx-vld-mono" style="color:#111827;">0</b>대</span>
                            </div>
                            <textarea id="awx-vld-txt-chk-detail" class="awx-vld-textarea awx-vld-mono" placeholder="확인자 상세목록현황 호스트 붙여넣기" style="height:60px; resize:vertical;">${state.chkDetailText}</textarea>
                        </div>

                        <!-- 확인자 인시던트 [필수] -->
                        <div>
                            <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:4px;">
                                <span style="font-size:12px; font-weight:700; color:#111827;">인시던트 호스트 <span class="awx-vld-badge-req">[필수]</span></span>
                                <span style="font-size:11.5px; color:#4b5563;">인식: <b id="awx-vld-chk-cnt-incident" class="awx-vld-mono" style="color:#111827;">0</b>대</span>
                            </div>
                            <textarea id="awx-vld-txt-chk-incident" class="awx-vld-textarea awx-vld-mono" placeholder="확인자 인시던트 호스트 붙여넣기" style="height:60px; resize:vertical;">${state.chkIncidentText}</textarea>
                        </div>

                        <!-- 확인자 작업대상 [작업자 활성화 시 필수] -->
                        <div id="awx-vld-chk-target-wrap">
                            <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:4px;">
                                <span style="font-size:12px; font-weight:700; color:#111827;">
                                    작업대상 호스트 <span id="awx-vld-chk-target-badge" class="awx-vld-badge-opt">[선택]</span>
                                </span>
                                <span style="font-size:11.5px; color:#4b5563;">인식: <b id="awx-vld-chk-cnt-target" class="awx-vld-mono" style="color:#111827;">0</b>대</span>
                            </div>
                            <textarea id="awx-vld-txt-chk-target" class="awx-vld-textarea awx-vld-mono" placeholder="작업자가 작업대상 호스트를 사용하지 않은 요청입니다 (비교 제외)" style="height:55px; resize:vertical;">${state.chkTargetText}</textarea>
                        </div>

                        <!-- 비교 실행 버튼 -->
                        <button id="awx-vld-btn-chk-compare" class="awx-vld-btn awx-vld-btn-primary" style="width:100%;">
                            교차 비교 실행 (전체 소스 통합 대조)
                        </button>
                    </div>

                    <!-- 4. 확인자 비교 결과 및 승인/반려 카드 -->
                    <div id="awx-vld-chk-result-box" style="display:none; background:#ffffff; border:1px solid #cbd5e1; border-radius:6px; padding:14px; font-size:12.5px; line-height:1.5;"></div>
                </div>
            </div>

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
    if (isStandaloneApp) {
        panel.style.cssText = `
            position: relative; top: 0; left: 0; width: 100vw; height: 100vh; max-height: 100vh;
            background: #ffffff; border: none; border-radius: 0;
            box-shadow: none; z-index: 1;
            display: flex; flex-direction: column; overflow: hidden;
            transition: opacity 0.18s ease;
        `;
    } else {
        panel.style.cssText = `
            position: fixed; top: 50px; right: 20px; width: 560px; max-height: 90vh;
            background: #ffffff; border: 1px solid #0f172a; border-radius: 8px;
            box-shadow: 0 12px 32px rgba(15, 23, 42, 0.22); z-index: 9999999;
            display: flex; flex-direction: column; overflow: hidden;
            transition: opacity 0.18s ease;
        `;
    }

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

        // 탭 버튼 및 패널
        const tabSingle = getEl('awx-vld-tab-single');
        const tabWorker = getEl('awx-vld-tab-worker');
        const tabChecker = getEl('awx-vld-tab-checker');
        const paneSingle = getEl('awx-vld-pane-single');
        const paneWorker = getEl('awx-vld-pane-worker');
        const paneChecker = getEl('awx-vld-pane-checker');
        const badgePendingCount = getEl('awx-vld-badge-pending-count');

        // [탭 1] 단독 확인 엘리먼트
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

        // [탭 2] 작업자 크로스체크 요청 엘리먼트
        const workerAgentMsg = getEl('awx-vld-worker-agent-msg');
        const btnCheckAgent = getEl('awx-vld-btn-check-agent');
        const workerIncidentNo = getEl('awx-vld-worker-incident-no');
        const workerIncidentHint = getEl('awx-vld-worker-incident-hint');
        const workerNotes = getEl('awx-vld-worker-notes');
        const selChecker = getEl('awx-vld-sel-checker');
        const btnSendRequest = getEl('awx-vld-btn-send-request');
        const wCntAwx = getEl('awx-vld-w-cnt-awx');
        const wCntDetail = getEl('awx-vld-w-cnt-detail');
        const wCntIncident = getEl('awx-vld-w-cnt-incident');
        const wCntTarget = getEl('awx-vld-w-cnt-target');
        const workerSessionCard = getEl('awx-vld-worker-session-card');
        const wSessionStatus = getEl('awx-vld-w-session-status');
        const wSessionId = getEl('awx-vld-w-session-id');
        const wAssignedChecker = getEl('awx-vld-w-assigned-checker');
        const selReassign = getEl('awx-vld-sel-reassign');
        const btnReassign = getEl('awx-vld-btn-reassign');
        const wResultReport = getEl('awx-vld-w-result-report');

        // [탭 3] 확인자 크로스체크 확인 엘리먼트
        const btnRefreshPending = getEl('awx-vld-btn-refresh-pending');
        const selPendingSessions = getEl('awx-vld-sel-pending-sessions');
        const checkerContent = getEl('awx-vld-checker-content');
        const chkWorkerName = getEl('awx-vld-chk-worker-name');
        const chkWorkerIp = getEl('awx-vld-chk-worker-ip');
        const chkStatusBadge = getEl('awx-vld-chk-status-badge');
        const chkIncidentNo = getEl('awx-vld-chk-incident-no');
        const chkNotesBox = getEl('awx-vld-chk-notes-box');
        const chkSpecialNotes = getEl('awx-vld-chk-special-notes');
        const chkAwxInfo = getEl('awx-vld-chk-awx-info');
        const chkBlindDetail = getEl('awx-vld-chk-blind-detail');
        const chkBlindIncident = getEl('awx-vld-chk-blind-incident');
        const chkBlindTarget = getEl('awx-vld-chk-blind-target');
        const txtChkDetail = getEl('awx-vld-txt-chk-detail');
        const txtChkIncident = getEl('awx-vld-txt-chk-incident');
        const txtChkTarget = getEl('awx-vld-txt-chk-target');
        const chkTargetBadge = getEl('awx-vld-chk-target-badge');
        const chkCntDetail = getEl('awx-vld-chk-cnt-detail');
        const chkCntIncident = getEl('awx-vld-chk-cnt-incident');
        const chkCntTarget = getEl('awx-vld-chk-cnt-target');
        const btnChkCompare = getEl('awx-vld-btn-chk-compare');
        const chkResultBox = getEl('awx-vld-chk-result-box');

        // 탭 전환 핸들러
        async function switchTab(tabName) {
            state.activeTab = tabName;
            if (tabSingle) tabSingle.classList.toggle('active', tabName === 'single');
            if (tabWorker) tabWorker.classList.toggle('active', tabName === 'worker');
            if (tabChecker) tabChecker.classList.toggle('active', tabName === 'checker');

            if (paneSingle) paneSingle.style.display = tabName === 'single' ? 'flex' : 'none';
            if (paneWorker) paneWorker.style.display = tabName === 'worker' ? 'flex' : 'none';
            if (paneChecker) paneChecker.style.display = tabName === 'checker' ? 'flex' : 'none';

            if (tabName === 'worker') {
                updateWorkerTabSync();
                await fetchUsersAndStatus();
            } else if (tabName === 'checker') {
                await fetchPendingSessions();
            }
        }

        if (tabSingle) tabSingle.onclick = () => switchTab('single');
        if (tabWorker) tabWorker.onclick = () => switchTab('worker');
        if (tabChecker) tabChecker.onclick = () => switchTab('checker');
        window.awxSwitchTab = switchTab;

        // 독립 창(Popup)에서 마우스 이탈 시 아래 창이 보이도록 헤더 줄로 자동 축소(미니 모드) 상태 관리
        let popupExpandedWidth = 560;
        let popupExpandedHeight = 840;
        let popupCollapsed = false;

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

            if (isPopup && popupWin) {
                rootDoc.body.style.opacity = '1';
                const shouldCollapse = state.opacityEnabled && !state.isMouseInside && !forcePreview;
                if (shouldCollapse) {
                    if (!popupCollapsed) {
                        if (popupWin.outerHeight && popupWin.outerHeight > 220) {
                            popupExpandedWidth = popupWin.outerWidth || 560;
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

        if (isPopup && popupWin) {
            popupWin.addEventListener('blur', () => {
                if (state.isPinned && !popupWin.closed) {
                    setTimeout(() => {
                        try { popupWin.focus(); } catch (e) {}
                    }, 50);
                }
            });
            window.addEventListener('mousedown', () => {
                if (state.isPinned && popupWin && !popupWin.closed) {
                    setTimeout(() => {
                        try { popupWin.focus(); } catch (e) {}
                    }, 80);
                }
            });
        }

        // 드래그 기능 (페이지 내 패널일 때만 적용, 드래그 중에만 리스너 바인딩하여 CPU/이벤트 누수 방지)
        if (!isPopup && !isStandaloneApp && headerEl) {
            let isDragging = false;
            let startX, startY, initialLeft, initialTop;
            const onMouseMove = (e) => {
                if (!isDragging) return;
                const dx = e.clientX - startX;
                const dy = e.clientY - startY;
                panel.style.left = Math.max(10, Math.min(window.innerWidth - panel.offsetWidth - 10, initialLeft + dx)) + 'px';
                panel.style.top = Math.max(10, Math.min(window.innerHeight - panel.offsetHeight - 10, initialTop + dy)) + 'px';
            };
            const onMouseUp = () => {
                isDragging = false;
                document.removeEventListener('mousemove', onMouseMove);
                document.removeEventListener('mouseup', onMouseUp);
            };
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
                document.addEventListener('mousemove', onMouseMove);
                document.addEventListener('mouseup', onMouseUp);
            });
        }

        // 닫기 버튼
        btnClose.onclick = () => {
            if (isPopup && popupWin) {
                popupWin.close();
            } else if (isStandaloneApp) {
                try {
                    if (navigator.sendBeacon) {
                        navigator.sendBeacon('/api/bye', 'bye');
                    } else {
                        fetch('/api/bye', { method: 'POST', keepalive: true }).catch(() => {});
                    }
                } catch (e) {}
                window.close();
            } else {
                panel.style.display = 'none';
            }
        };

        // 독립 창 분리 / 복귀 / 새로고침 버튼
        if (isPopup) {
            btnDetach.onclick = () => {
                if (popupWin) popupWin.close();
                state.isMouseInside = true;
                panel.style.display = 'flex';
                panel.style.opacity = '1';
                bindEvents(document, false, null);
                updateCountsAndButton();
            };
        } else if (isStandaloneApp) {
            btnDetach.onclick = async () => {
                await fetchUsersAndStatus();
                await fetchPendingSessions();
                updateCountsAndButton();
            };
        } else {
            btnDetach.onclick = () => {
                panel.style.display = 'none';
                state.isMouseInside = true;
                const popup = window.open('', 'AWXHostValidatorPopup', 'width=580,height=860,menubar=no,toolbar=no,location=no,status=no,resizable=yes,scrollbars=yes');
                if (!popup) {
                    alert('팝업 차단이 감지되었습니다. 팝업 허용 후 다시 시도해주세요.');
                    panel.style.display = 'flex';
                    return;
                }
                popup.document.write(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>작업대상 호스트 교차 검증기</title><style>${getStylesheet()}</style></head><body class="awx-vld-popup-root" style="margin:0; padding:0; background:#f3f4f6; display:flex; flex-direction:column; height:100vh; overflow:hidden;">${buildUIHTML(ctx, true)}</body></html>`);
                popup.document.close();
                bindEvents(popup.document, true, popup);
                window.awxPopupWindow = popup;
                popup.addEventListener('unload', () => {
                    state.isMouseInside = true;
                    panel.style.display = 'flex';
                    panel.style.opacity = '1';
                    bindEvents(document, false, null);
                });
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
            updateWorkerTabSync();
        }

        function updateWorkerTabSync() {
            if (wCntAwx) wCntAwx.innerText = state.collectedHosts.size;
            if (wCntDetail) wCntDetail.innerText = parseHostText(txtDetail.value).length;
            if (wCntIncident) wCntIncident.innerText = parseHostText(txtIncident.value).length;
            if (wCntTarget) wCntTarget.innerText = parseHostText(txtTarget.value).length;
        }

        txtDetail.oninput = updateCountsAndButton;
        txtIncident.oninput = updateCountsAndButton;
        txtTarget.oninput = updateCountsAndButton;

        if (btnResetConfig && panelResetConfig) {
            btnResetConfig.onclick = () => {
                state.showResetConfig = !state.showResetConfig;
                panelResetConfig.style.display = state.showResetConfig ? 'block' : 'none';
                btnResetConfig.style.background = state.showResetConfig ? '#e2e8f0' : '#ffffff';
            };
        }

        if (chkResetDetail) chkResetDetail.onchange = () => { state.resetTargets.detail = chkResetDetail.checked; };
        if (chkResetIncident) chkResetIncident.onchange = () => { state.resetTargets.incident = chkResetIncident.checked; };
        if (chkResetTarget) chkResetTarget.onchange = () => { state.resetTargets.target = chkResetTarget.checked; };

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

            if (resultBox) {
                resultBox.style.display = 'none';
                resultBox.innerHTML = '';
            }

            updateCountsAndButton();
        };

        // 화면 DOM 수집 (독립 앱 모드에서는 1페이지 20대 API 수집 지원)
        btnPage.onclick = async () => {
            const filters = getExcludeFilters(inputExclude.value.trim());
            const rows = document.querySelectorAll('table tbody tr');
            if (rows.length > 0) {
                rows.forEach(tr => {
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
            } else if (isStandaloneApp) {
                try {
                    const currentContext = getContext();
                    const invId = currentContext.inventoryId || '3';
                    const res = await fetch(`${AGENT_BASE}/api/awx/hosts?inventory=${invId}&limit=20`);
                    const data = await res.json();
                    if (data.results) {
                        data.results.forEach(item => {
                            const h = (typeof item === 'string' ? item : item.name).trim().toLowerCase();
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
                        });
                    }
                    state.rawCount = state.collectedHosts.size + state.excludedHosts.length;
                    renderAwxSummary();
                    updateCountsAndButton();
                } catch (err) {
                    awxSummaryEl.innerHTML = `<span style="color:#dc2626; font-weight:600;">페이지 수집 실패: ${err.message}</span>`;
                }
            }
        };

        // AWX API 전수 수집
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
                    let res = await fetch(nextUrl);
                    if (!res.ok && !nextUrl.startsWith(AGENT_BASE)) {
                        const invId = currentContext.inventoryId || '3';
                        res = await fetch(`${AGENT_BASE}/api/awx/hosts?inventory=${invId}`);
                    }
                    const data = await res.json();
                    if (data.results) {
                        data.results.forEach(item => {
                            const name = (typeof item === 'string' ? item : item.name).trim().toLowerCase();
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

        // 단독 모드 비교 실행
        function doCompare() {
            const detailList = parseHostText(txtDetail.value);
            const incidentList = parseHostText(txtIncident.value);
            const targetList = parseHostText(txtTarget.value);
            const awxList = Array.from(state.collectedHosts);

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

            const activeSources = [];
            activeSources.push({ id: 'detail', name: '상세목록현황', hosts: new Set(detailList) });
            activeSources.push({ id: 'incident', name: '인시던트', hosts: new Set(incidentList) });

            if (targetList.length > 0) {
                activeSources.push({ id: 'target', name: '작업대상', hosts: new Set(targetList) });
            }
            if (awxList.length > 0) {
                activeSources.push({ id: 'awx', name: 'AWX 수집 목록', hosts: new Set(awxList) });
            }

            const unionSet = new Set();
            activeSources.forEach(s => s.hosts.forEach(h => unionSet.add(h)));

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
                                <span style="font-weight:700; color:#991b1b; font-size:12px;">• ${d.sourceName}: 누락 ${d.missingCount}대 (${d.missingList.slice(0, 5).join(', ')}${d.missingCount > 5 ? ' 외' : ''}) (보유: ${d.sourceCount}대)</span>
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

        // =========================================================================
        // 크로스체크 연동 로직 (Go 에이전트 연동: 작업자 & 확인자)
        // =========================================================================
        let eventSource = null;

        async function fetchUsersAndStatus() {
            try {
                const resMe = await agentFetch(`${AGENT_BASE}/api/me`, { cache: 'no-store' });
                if (resMe.ok) {
                    const meData = await resMe.json();
                    state.agentInfo = { connected: true, ip: meData.ip, name: meData.name, registered: meData.registered };
                    if (workerAgentMsg) {
                        const myLabel = meData.name ? `${meData.name} (${meData.ip})` : `미등록 IP (${meData.ip})`;
                        workerAgentMsg.innerHTML = `● Go 에이전트 연결됨: <b style="color:#38bdf8;">${myLabel}</b>`;
                    }
                    initEventSource();
                } else {
                    throw new Error("HTTP " + resMe.status);
                }

                const resUsers = await agentFetch(`${AGENT_BASE}/api/users`, { cache: 'no-store' });
                if (resUsers.ok) {
                    const uData = await resUsers.json();
                    state.usersList = uData.users || [];
                    populateCheckerDropdowns();
                }
            } catch (err) {
                state.agentInfo = { connected: false, ip: '', name: '', registered: false };
                if (workerAgentMsg) {
                    workerAgentMsg.innerHTML = `○ Go 에이전트 미연결 (공유폴더에서 cross-checker-agent.exe 실행 시 연동)`;
                }
            }
        }

        window.awxFetchUsersAndStatus = fetchUsersAndStatus;

        function populateCheckerDropdowns() {
            if (!selChecker && !selReassign) return;
            const currentMyIp = state.agentInfo.ip;

            const buildOptions = (selectedVal) => {
                let html = '<option value="">확인자를 선택하세요</option>';
                state.usersList.forEach(u => {
                    // 평소에는 자기 자신 제외 (정상 운영 규칙 엄격 준수)
                    if (u.ip === currentMyIp) return;
                    const isSel = (u.ip === selectedVal) ? 'selected' : '';
                    html += `<option value="${u.ip}" data-name="${u.name}" ${isSel}>${u.name} (${u.ip})</option>`;
                });
                // 이번 1인 테스트 전용: 나 자신에게 테스트 요청 옵션 제공 (단, 확인자 호출은 나 자신에게 발생하지 않음)
                if (currentMyIp) {
                    const myName = state.agentInfo.name || "나 자신";
                    const isSel = (currentMyIp === selectedVal) ? 'selected' : '';
                    html += `<option value="${currentMyIp}" data-name="${myName} (테스트)" ${isSel} style="color:#2563eb; font-weight:700;">[테스트] ${myName} (나 자신: ${currentMyIp})</option>`;
                }
                return html;
            };

            if (selChecker) selChecker.innerHTML = buildOptions(state.selectedCheckerIp);
            if (selReassign) selReassign.innerHTML = buildOptions('');
        }

        function initEventSource() {
            if (state.__eventSourceInitialized) return;
            state.__eventSourceInitialized = true;
            agentBridge.on('*', (data, msg) => {
                if (msg && msg.event) {
                    handleAgentSSE({
                        type: msg.event,
                        sessionId: msg.sessionId,
                        data: msg.data
                    });
                }
            });
        }

        function handleAgentSSE(ev) {
            if (ev.type === 'NEW_SESSION' || ev.type === 'PEER_NOTIFY' || ev.type === 'SESSION_REASSIGNED') {
                fetchPendingSessions();
            } else if (ev.type === 'SESSION_DECIDED' || ev.type === 'PEER_UPDATE') {
                if (state.currentSession && (state.currentSession.sessionId === ev.sessionId || ev.data?.sessionId === state.currentSession.sessionId)) {
                    refreshWorkerSessionStatus(state.currentSession.sessionId);
                }
                fetchPendingSessions();
            } else if (ev.type === 'REASSIGNED_AWAY') {
                fetchPendingSessions();
            }
        }

        // 작업자: 인시던트 번호 힌트 토글
        if (workerIncidentNo && workerIncidentHint) {
            workerIncidentNo.oninput = () => {
                state.incidentNo = workerIncidentNo.value.trim();
                workerIncidentHint.style.display = state.incidentNo ? 'none' : 'block';
            };
        }
        if (workerNotes) {
            workerNotes.oninput = () => {
                state.specialNotes = workerNotes.value.trim();
            };
        }
        if (selChecker) {
            selChecker.onchange = () => {
                state.selectedCheckerIp = selChecker.value;
            };
        }

        // 작업자: 요청 발송
        async function sendCrossCheckRequest(overrideCheckerIp = null) {
            const detailList = parseHostText(txtDetail.value || state.detailText || '');
            const incidentList = parseHostText(txtIncident.value || state.incidentText || '');
            const targetList = parseHostText(txtTarget.value || state.targetText || '');
            const awxList = Array.from(state.collectedHosts);

            if (detailList.length === 0 || incidentList.length === 0) {
                console.warn("[awxCrossCheck] detail or incident is empty");
                alert("상세목록현황과 인시던트 호스트는 필수 입력 대상입니다. 1번 탭 또는 입력란에 호스트를 입력해주세요.");
                return null;
            }

            // 이벤트 객체(MouseEvent 등)가 인자로 넘어오는 경우 대비 엄격한 문자열 타입 검증
            const explicitIp = (typeof overrideCheckerIp === 'string' && overrideCheckerIp.trim()) ? overrideCheckerIp.trim() : null;
            const checkerOpt = selChecker ? selChecker.selectedOptions[0] : null;
            const checkerIp = String(explicitIp || (selChecker ? selChecker.value : '') || (typeof state.selectedCheckerIp === 'string' ? state.selectedCheckerIp : '') || '').trim();
            let checkerName = '';
            if (explicitIp && state.usersList) {
                const u = state.usersList.find(x => x.ip === explicitIp);
                if (u) checkerName = u.name;
            }
            if (!checkerName && checkerOpt) {
                checkerName = (checkerOpt.getAttribute('data-name') || checkerOpt.innerText.split(' ')[0]);
            }
            if (!checkerName && checkerIp && state.usersList) {
                const u = state.usersList.find(x => x.ip === checkerIp);
                if (u) checkerName = u.name;
            }
            if (!checkerName) checkerName = "확인자";
            if (selChecker && checkerIp) {
                selChecker.value = checkerIp;
                state.selectedCheckerIp = checkerIp;
            }

            if (!checkerIp) {
                console.warn("[awxCrossCheck] checkerIp is empty");
                alert("크로스체크 확인자를 선택해주세요.");
                return null;
            }

            btnSendRequest.disabled = true;
            btnSendRequest.innerText = "요청 발송 중...";

            const payload = {
                worker: {
                    name: String(state.agentInfo.name || "작업자"),
                    ip: String(state.agentInfo.ip || "127.0.0.1")
                },
                checker: {
                    name: String(checkerName),
                    ip: String(checkerIp)
                },
                metadata: {
                    incidentId: workerIncidentNo ? workerIncidentNo.value.trim() : '',
                    specialNotes: workerNotes ? workerNotes.value.trim() : '',
                    awxContextLabel: ctx.label,
                    excludePattern: state.excludePattern
                },
                activeFields: {
                    awx: awxList.length > 0,
                    detail: detailList.length > 0,
                    incident: incidentList.length > 0,
                    target: targetList.length > 0
                },
                workerData: {
                    awxHosts: awxList,
                    excludedHosts: state.excludedHosts,
                    rawAwxCount: state.rawCount,
                    detailHosts: detailList,
                    incidentHosts: incidentList,
                    targetHosts: targetList
                }
            };

            try {
                const res = await agentFetch(`${AGENT_BASE}/api/session/create`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                const resData = await res.json();
                if (!res.ok) throw new Error(resData.error || "요청 생성 실패");

                state.currentSession = {
                    sessionId: resData.sessionId,
                    checkerName: checkerName,
                    checkerIp: checkerIp,
                    status: resData.status || "PENDING",
                    peerDelivered: resData.peerDelivered,
                    peerError: resData.peerError
                };

                renderWorkerSessionCard();
                return state.currentSession;
            } catch (err) {
                console.error("[awxCrossCheck] sendCrossCheckRequest error:", err);
                alert("크로스체크 요청 발송 실패: " + err.message);
                return null;
            } finally {
                btnSendRequest.disabled = false;
                btnSendRequest.innerText = "크로스체크 확인 요청 발송";
            }
        }

        if (btnSendRequest) btnSendRequest.onclick = () => sendCrossCheckRequest();
        window.awxSendCrossCheckRequest = sendCrossCheckRequest;

        function renderWorkerSessionCard() {
            if (!state.currentSession || !workerSessionCard) return;
            workerSessionCard.style.display = 'flex';
            if (wSessionId) wSessionId.innerText = state.currentSession.sessionId;
            if (wAssignedChecker) wAssignedChecker.innerText = `${state.currentSession.checkerName} (${state.currentSession.checkerIp})`;
            if (wSessionStatus) {
                const st = state.currentSession.status;
                wSessionStatus.innerText = st;
                wSessionStatus.style.color = (st === 'APPROVED') ? '#16a34a' : (st === 'REJECTED') ? '#dc2626' : '#2563eb';
            }

            if (state.currentSession.status === 'APPROVED') {
                if (wResultReport) {
                    wResultReport.style.display = 'block';
                    wResultReport.style.background = '#f0fdf4';
                    wResultReport.style.border = '1px solid #86efac';
                    wResultReport.style.borderRadius = '5px';
                    wResultReport.style.padding = '10px';
                    wResultReport.innerHTML = `
                        <div style="font-weight:700; color:#15803d; font-size:13px; margin-bottom:4px;">
                            [CROSS-CHECK APPROVED] 크로스체크 교차 검증 승인 완료
                        </div>
                        <div style="color:#166534; font-size:11.5px; margin-bottom:8px;">
                            확인자: <b>${state.currentSession.checkerName} (${state.currentSession.checkerIp})</b> | 상태: 전수 일치 승인
                        </div>
                        <div class="awx-vld-mono" style="background:#ffffff; border:1px solid #bbf7d0; padding:8px; border-radius:4px; color:#14532d; font-size:11.5px; word-break:break-all;">
                            인시던트의 작업대상을 복사해서 붙여넣기를 권장합니다.vim [작업대상 파일] -&gt; esc -&gt; :set paste 입력(붙여넣기 모드) -&gt; 붙여넣기
                        </div>
                    `;
                }
            } else if (state.currentSession.status === 'REJECTED') {
                if (wResultReport) {
                    wResultReport.style.display = 'block';
                    wResultReport.style.background = '#fef2f2';
                    wResultReport.style.border = '1px solid #fca5a5';
                    wResultReport.style.borderRadius = '5px';
                    wResultReport.style.padding = '10px';
                    wResultReport.innerHTML = `
                        <div style="font-weight:700; color:#dc2626; font-size:13px; margin-bottom:4px;">
                            [CROSS-CHECK REJECTED] 크로스체크 반려됨
                        </div>
                        <div style="color:#991b1b; font-size:11.5px;">
                            확인자: <b>${state.currentSession.checkerName}</b> | 사유: ${state.currentSession.decisionComment || '불일치 감지'}
                        </div>
                    `;
                }
            } else if (state.currentSession.checkerIp === state.agentInfo.ip && state.currentSession.status === 'PENDING') {
                if (wResultReport) {
                    wResultReport.style.display = 'block';
                    wResultReport.style.background = '#eff6ff';
                    wResultReport.style.border = '1px solid #bfdbfe';
                    wResultReport.style.borderRadius = '5px';
                    wResultReport.style.padding = '10px';
                    wResultReport.innerHTML = `
                        <div style="font-weight:700; color:#1d4ed8; font-size:12.5px; margin-bottom:4px;">
                            [1인 테스트 모드 진행 안내]
                        </div>
                        <div style="color:#1e40af; font-size:11.5px; line-height:1.5;">
                            나 자신에게 발송된 테스트 요청입니다. (확인자 외부 호출 없이 로컬 세션 생성 완료)<br>
                            상단 탭에서 <b>[크로스체크 확인 (확인자)]</b> 탭을 클릭하여 확인자 화면에서 직접 검증 및 승인/반려를 진행하세요.
                        </div>
                    `;
                }
            } else if (state.currentSession.status === 'PENDING') {
                if (wResultReport) {
                    wResultReport.style.display = 'block';
                    if (state.currentSession.peerDelivered === false) {
                        wResultReport.style.background = '#fffbeb';
                        wResultReport.style.border = '1px solid #fcd34d';
                        wResultReport.style.borderRadius = '5px';
                        wResultReport.style.padding = '10px';
                        wResultReport.innerHTML = `
                            <div style="font-weight:700; color:#b45309; font-size:12.5px; margin-bottom:4px;">
                                ⚠️ 확인자 PC(${state.currentSession.checkerIp}:18080) 직접 연결 불가 안내
                            </div>
                            <div style="color:#92400e; font-size:11.5px; line-height:1.5;">
                                세션이 생성되었으나 확인자 PC 18080 포트로 직접 알림이 닿지 않았습니다.<br>
                                • <b>공유폴더 방식(권장)</b>: config.json의 "sharedDir"를 공용 공유폴더(예: \\\\서버\\share)로 지정하면 방화벽과 무관하게 자동 동기화됩니다.<br>
                                • <b>로컬 실행 방식</b>: 확인자 PC의 Windows 방화벽에서 18080 포트 인바운드 허용이 필요합니다.
                            </div>
                        `;
                    } else {
                        wResultReport.style.background = '#f0fdf4';
                        wResultReport.style.border = '1px solid #86efac';
                        wResultReport.style.borderRadius = '5px';
                        wResultReport.style.padding = '10px';
                        wResultReport.innerHTML = `
                            <div style="font-weight:700; color:#15803d; font-size:12.5px; margin-bottom:4px;">
                                ✓ 확인자 PC로 요청 전달 완료
                            </div>
                            <div style="color:#166534; font-size:11.5px; line-height:1.5;">
                                확인자(<b>${state.currentSession.checkerName}</b>) PC의 [크로스체크 확인] 화면에 요청이 성공적으로 등록되었습니다.
                            </div>
                        `;
                    }
                }
            } else {
                if (wResultReport) wResultReport.style.display = 'none';
            }
        }

        // 작업자: 확인자 즉시 변경 (쿨타임 0초)
        async function reassignChecker(newIp = null) {
            if (!state.currentSession) return;
            const explicitIp = (typeof newIp === 'string' && newIp.trim()) ? newIp.trim() : null;
            const targetIp = String(explicitIp || (selReassign ? selReassign.value : '') || '').trim();
            if (!targetIp) {
                alert("변경할 확인자를 선택해주세요.");
                return;
            }

            let newName = '';
            const opt = selReassign ? Array.from(selReassign.options).find(o => o.value === targetIp) : null;
            if (opt) {
                newName = opt.getAttribute('data-name') || opt.innerText.split(' ')[0];
            } else {
                const user = state.usersList.find(u => u.ip === targetIp);
                newName = user ? user.name : '확인자';
            }

            btnReassign.disabled = true;
            btnReassign.innerText = "변경 중...";

            try {
                const res = await agentFetch(`${AGENT_BASE}/api/session/reassign`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        sessionId: state.currentSession.sessionId,
                        newChecker: {
                            name: newName,
                            ip: targetIp
                        }
                    })
                });
                const resData = await res.json();
                if (!res.ok) throw new Error(resData.error || "확인자 변경 실패");

                state.currentSession.checkerName = newName;
                state.currentSession.checkerIp = targetIp;
                renderWorkerSessionCard();
            } catch (err) {
                alert("확인자 변경 실패: " + err.message);
            } finally {
                btnReassign.disabled = false;
                btnReassign.innerText = "즉시 변경";
            }
        }

        if (btnReassign) btnReassign.onclick = () => reassignChecker();
        window.awxReassignChecker = reassignChecker;

        async function refreshWorkerSessionStatus(sessionId) {
            try {
                const res = await agentFetch(`${AGENT_BASE}/api/session/status?sessionId=${sessionId}&clientIp=${state.agentInfo.ip || ''}`);
                if (res.ok) {
                    const data = await res.json();
                    if (state.currentSession && state.currentSession.sessionId === sessionId) {
                        state.currentSession.status = data.status;
                        state.currentSession.checkerName = data.checker?.name || state.currentSession.checkerName;
                        state.currentSession.checkerIp = data.checker?.ip || state.currentSession.checkerIp;
                        state.currentSession.decisionComment = data.checkerData?.decisionComment || '';
                        renderWorkerSessionCard();
                    }
                }
            } catch (e) {}
        }
        window.awxRefreshWorkerSessionStatus = refreshWorkerSessionStatus;

        // 확인자: 대기 중인 세션 목록 조회
        async function fetchPendingSessions() {
            try {
                const myIp = state.agentInfo.ip || '';
                const res = await agentFetch(`${AGENT_BASE}/api/session/list?clientIp=${myIp}`);
                if (res.ok) {
                    const data = await res.json();
                    const list = (data.sessions || []).filter(s => s.status === 'PENDING' || s.status === 'CHECKING');
                    state.pendingSessions = list;

                    if (badgePendingCount) {
                        badgePendingCount.innerText = list.length;
                        badgePendingCount.style.display = list.length > 0 ? 'inline-block' : 'none';
                    }

                    if (selPendingSessions) {
                        let html = '<option value="">도착한 요청을 선택하세요</option>';
                        list.forEach(s => {
                            const inc = s.metadata?.incidentId ? `[${s.metadata.incidentId}] ` : '';
                            html += `<option value="${s.sessionId}">${inc}${s.worker.name}님의 요청 (${s.sessionId})</option>`;
                        });
                        selPendingSessions.innerHTML = html;
                        if (list.length === 1 && !state.loadedCheckerSession) {
                            selPendingSessions.value = list[0].sessionId;
                            loadCheckerSession(list[0].sessionId);
                        }
                    }
                }
            } catch (e) {}
        }
        window.awxFetchPendingSessions = fetchPendingSessions;

        if (btnRefreshPending) btnRefreshPending.onclick = fetchPendingSessions;
        if (selPendingSessions) {
            selPendingSessions.onchange = () => {
                const sid = selPendingSessions.value;
                if (sid) {
                    loadCheckerSession(sid);
                } else {
                    state.loadedCheckerSession = null;
                    if (checkerContent) checkerContent.style.display = 'none';
                }
            };
        }

        // 확인자: 특정 세션 로드 (블라인드 데이터 포함)
        async function loadCheckerSession(sessionId, overrideClientIp = null) {
            try {
                const reqIp = overrideClientIp || (state.loadedCheckerSession?.checker?.ip || state.agentInfo.ip || '');
                const res = await agentFetch(`${AGENT_BASE}/api/session/status?sessionId=${sessionId}&clientIp=${reqIp}&asChecker=true`);
                if (!res.ok) {
                    const err = await res.json();
                    alert("요청 로드 실패: " + (err.error || res.statusText));
                    return;
                }
                const sess = await res.json();
                state.loadedCheckerSession = sess;

                if (checkerContent) checkerContent.style.display = 'flex';
                if (chkWorkerName) chkWorkerName.innerText = sess.worker?.name || '-';
                if (chkWorkerIp) chkWorkerIp.innerText = sess.worker?.ip || '-';
                if (chkStatusBadge) chkStatusBadge.innerText = sess.status;
                if (chkIncidentNo) {
                    chkIncidentNo.innerText = sess.metadata?.incidentId ? sess.metadata.incidentId : '(미입력)';
                }
                if (chkNotesBox && chkSpecialNotes) {
                    if (sess.metadata?.specialNotes) {
                        chkSpecialNotes.innerText = sess.metadata.specialNotes;
                        chkNotesBox.style.display = 'block';
                    } else {
                        chkNotesBox.style.display = 'none';
                    }
                }
                if (chkAwxInfo) {
                    const awxCnt = sess.workerData?.rawAwxCount || sess.workerData?.awxCount || 0;
                    chkAwxInfo.innerHTML = `• AWX 연동: <b>${sess.activeFields?.awx ? '참여 (' + awxCnt + '대 수집됨)' : '미사용'}</b> (비교 시 자동 대조)`;
                }

                // 블라인드 요약 (앞 3대 노출 + 외 N대 블라인드)
                const wData = sess.workerData || {};
                const renderBlind = (name, prevObj, count) => {
                    if (!prevObj && !count) return `• ${name}: 미입력 (비교 제외)`;
                    const pList = prevObj?.previewHosts || [];
                    const total = prevObj?.totalCount || count || pList.length;
                    const blinded = prevObj?.blindedCount || Math.max(0, total - pList.length);
                    let text = `• ${name} (${total}대): `;
                    if (pList.length > 0) {
                        text += `<b>${pList.join(', ')}</b>`;
                        if (blinded > 0) {
                            text += ` 외 ${blinded}대 <span style="color:#d97706; font-weight:600;">[블라인드 - 직접 입력 후 비교 시 공개]</span>`;
                        }
                    } else {
                        text += `총 ${total}대 <span style="color:#d97706; font-weight:600;">[블라인드 처리됨]</span>`;
                    }
                    return text;
                };

                if (chkBlindDetail) chkBlindDetail.innerHTML = renderBlind('상세목록현황', wData.detailHostsPreview, (wData.detailHosts || []).length);
                if (chkBlindIncident) chkBlindIncident.innerHTML = renderBlind('인시던트 호스트', wData.incidentHostsPreview, (wData.incidentHosts || []).length);
                if (chkBlindTarget) {
                    if (sess.activeFields?.target) {
                        chkBlindTarget.innerHTML = renderBlind('작업대상 호스트', wData.targetHostsPreview, (wData.targetHosts || []).length);
                        if (txtChkTarget) {
                            txtChkTarget.disabled = false;
                            txtChkTarget.placeholder = "확인자 작업대상 호스트 붙여넣기 [필수]";
                        }
                        if (chkTargetBadge) {
                            chkTargetBadge.innerText = "[필수]";
                            chkTargetBadge.className = "awx-vld-badge-req";
                        }
                    } else {
                        chkBlindTarget.innerHTML = `• 작업대상 호스트: 작업자 미입력 (비교 제외)`;
                        if (txtChkTarget) {
                            txtChkTarget.disabled = true;
                            txtChkTarget.value = '';
                            txtChkTarget.placeholder = "작업자가 작업대상 호스트를 사용하지 않은 요청입니다 (비교 제외)";
                        }
                        if (chkTargetBadge) {
                            chkTargetBadge.innerText = "[제외]";
                            chkTargetBadge.className = "awx-vld-badge-opt";
                        }
                    }
                }

                if (chkResultBox) chkResultBox.style.display = 'none';
            } catch (err) {
                alert("세션 로드 오류: " + err.message);
            }
        }
        window.awxLoadCheckerSession = loadCheckerSession;

        if (txtChkDetail) {
            txtChkDetail.oninput = () => {
                if (chkCntDetail) chkCntDetail.innerText = parseHostText(txtChkDetail.value).length;
            };
        }
        if (txtChkIncident) {
            txtChkIncident.oninput = () => {
                if (chkCntIncident) chkCntIncident.innerText = parseHostText(txtChkIncident.value).length;
            };
        }
        if (txtChkTarget) {
            txtChkTarget.oninput = () => {
                if (chkCntTarget) chkCntTarget.innerText = parseHostText(txtChkTarget.value).length;
            };
        }

        // 확인자: 전체 소스 교차 비교 실행
        async function doCheckerCompare() {
            if (!state.loadedCheckerSession) {
                alert("확인할 세션이 로드되지 않았습니다.");
                return;
            }

            const sid = state.loadedCheckerSession.sessionId;
            const checkerIp = state.loadedCheckerSession.checker?.ip || state.agentInfo.ip || '';

            // 비교를 위해 compare=true 로 전체 원본 데이터 요청 (백엔드가 블라인드 해제하여 전송)
            let fullSess = null;
            try {
                const res = await agentFetch(`${AGENT_BASE}/api/session/status?sessionId=${sid}&clientIp=${checkerIp}&compare=true`);
                if (!res.ok) throw new Error("전체 데이터 로드 실패");
                fullSess = await res.json();
            } catch (err) {
                alert("비교 데이터 로드 실패: " + err.message);
                return;
            }

            const chkDetailList = parseHostText(txtChkDetail.value);
            const chkIncidentList = parseHostText(txtChkIncident.value);
            const chkTargetList = parseHostText(txtChkTarget.value);

            // 필수 입력 검증
            if (chkDetailList.length === 0 || chkIncidentList.length === 0) {
                chkResultBox.style.display = 'block';
                chkResultBox.style.background = '#fef2f2';
                chkResultBox.style.borderColor = '#fca5a5';
                chkResultBox.innerHTML = `
                    <div style="font-weight:700; color:#dc2626; margin-bottom:4px; font-size:13px;">오류: 확인자 필수 입력 항목 누락</div>
                    <div style="color:#991b1b; font-size:12px;">상세목록현황과 인시던트 호스트는 필수 입력 대상입니다. 두 입력창에 호스트를 붙여넣어주세요.</div>
                `;
                return;
            }

            if (fullSess.activeFields?.target && chkTargetList.length === 0) {
                chkResultBox.style.display = 'block';
                chkResultBox.style.background = '#fef2f2';
                chkResultBox.style.borderColor = '#fca5a5';
                chkResultBox.innerHTML = `
                    <div style="font-weight:700; color:#dc2626; margin-bottom:4px; font-size:13px;">오류: 작업대상 호스트 누락</div>
                    <div style="color:#991b1b; font-size:12px;">작업자가 작업대상 호스트를 포함하여 요청했습니다. 작업대상 호스트를 입력해주세요.</div>
                `;
                return;
            }

            // 모든 소스 모으기
            const activeSources = [];
            // 작업자 소스
            if (fullSess.activeFields?.awx && fullSess.workerData?.awxHosts?.length > 0) {
                activeSources.push({ id: 'w_awx', name: '작업자 AWX 수집', hosts: new Set(fullSess.workerData.awxHosts) });
            }
            activeSources.push({ id: 'w_detail', name: '작업자 상세목록', hosts: new Set(fullSess.workerData.detailHosts || []) });
            activeSources.push({ id: 'w_incident', name: '작업자 인시던트', hosts: new Set(fullSess.workerData.incidentHosts || []) });
            if (fullSess.activeFields?.target) {
                activeSources.push({ id: 'w_target', name: '작업자 작업대상', hosts: new Set(fullSess.workerData.targetHosts || []) });
            }

            // 확인자 소스
            activeSources.push({ id: 'c_detail', name: '확인자 상세목록', hosts: new Set(chkDetailList) });
            activeSources.push({ id: 'c_incident', name: '확인자 인시던트', hosts: new Set(chkIncidentList) });
            if (fullSess.activeFields?.target) {
                activeSources.push({ id: 'c_target', name: '확인자 작업대상', hosts: new Set(chkTargetList) });
            }

            // 전체 합집합
            const unionSet = new Set();
            activeSources.forEach(s => s.hosts.forEach(h => unionSet.add(h)));

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

            state.lastCompareResult = {
                isAllMatch,
                detailHosts: chkDetailList,
                incidentHosts: chkIncidentList,
                targetHosts: chkTargetList
            };

            chkResultBox.style.display = 'block';

            if (isAllMatch) {
                chkResultBox.style.background = '#f0fdf4';
                chkResultBox.style.borderColor = '#86efac';
                chkResultBox.innerHTML = `
                    <div style="font-weight:700; color:#15803d; font-size:13.5px; margin-bottom:6px;">
                        [MATCH] 전체 ${activeSources.length}개 소스 전수 일치 확인 완료
                    </div>
                    <div style="color:#166534; font-size:12px; margin-bottom:10px;">
                        • 참여 소스: <b>${activeSources.map(s => `${s.name}(${s.hosts.size}대)`).join(', ')}</b><br>
                        • 불일치 및 누락 호스트: <b>0대</b>
                    </div>
                    <button id="awx-vld-btn-approve" class="awx-vld-btn awx-vld-btn-primary" style="width:100%; font-size:13.5px; padding:11px; background:#16a34a; border-color:#15803d;">
                        확인 완료 (승인)
                    </button>
                `;
                const btnApprove = getEl('awx-vld-btn-approve');
                if (btnApprove) btnApprove.onclick = () => submitDecision('APPROVED', '');
            } else {
                chkResultBox.style.background = '#fef2f2';
                chkResultBox.style.borderColor = '#fca5a5';
                let diffHtml = '';
                diffs.forEach((d, idx) => {
                    const tid = `chk-diff-${idx}-${Date.now()}`;
                    diffHtml += `
                        <div style="margin-top:6px; padding:8px 10px; background:#ffffff; border:1px solid #fecaca; border-radius:5px;">
                            <div style="display:flex; justify-content:space-between; align-items:center;">
                                <span style="font-weight:700; color:#991b1b; font-size:12px;">• ${d.sourceName}: 누락 ${d.missingCount}대 (${d.missingList.slice(0, 5).join(', ')}${d.missingCount > 5 ? ' 외' : ''}) (보유: ${d.sourceCount}대)</span>
                                <button id="btn-${tid}" class="awx-vld-btn awx-vld-btn-secondary awx-vld-btn-sm">명단 펼치기</button>
                            </div>
                            <div id="${tid}" class="awx-vld-mono" style="display:none; margin-top:6px; max-height:100px; overflow-y:auto; font-size:11.5px; color:#b91c1c; background:#fef2f2; padding:7px; border-radius:4px; word-break:break-all;">
                                ${d.missingList.join(' ')}
                            </div>
                        </div>
                    `;
                });

                chkResultBox.innerHTML = `
                    <div style="font-weight:700; color:#dc2626; font-size:13.5px; margin-bottom:4px;">
                        [MISMATCH] 소스 간 불일치 감지 (${activeSources.length}개 소스 비교)
                    </div>
                    <div style="color:#7f1d1d; font-size:12px; margin-bottom:6px;">
                        참여 소스 현황: ${activeSources.map(s => `<b>${s.name}</b>(${s.hosts.size}대)`).join(' vs ')}
                    </div>
                    ${diffHtml}
                    <div style="margin-top:12px; padding-top:10px; border-top:1px dashed #fca5a5;">
                        <label style="display:block; font-size:12px; font-weight:700; color:#991b1b; margin-bottom:4px;">반려 사유 입력</label>
                        <textarea id="awx-vld-txt-reject-reason" class="awx-vld-textarea" placeholder="작업자에게 전달할 반려 사유를 입력하세요 (예: 상세목록에 1대 누락)" style="height:50px;"></textarea>
                        <div style="display:flex; gap:8px; margin-top:8px;">
                            <button id="awx-vld-btn-reject" class="awx-vld-btn awx-vld-btn-dark" style="flex:1; background:#dc2626; border-color:#b91c1c;">반려 / 재확인 요청</button>
                        </div>
                    </div>
                `;

                chkResultBox.querySelectorAll('button[id^="btn-"]').forEach(btn => {
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

                const btnReject = getEl('awx-vld-btn-reject');
                if (btnReject) {
                    btnReject.onclick = () => {
                        const reasonEl = getEl('awx-vld-txt-reject-reason');
                        const reason = reasonEl ? reasonEl.value.trim() : '';
                        submitDecision('REJECTED', reason || '소스 간 불일치 감지');
                    };
                }
            }
        }

        if (btnChkCompare) btnChkCompare.onclick = doCheckerCompare;
        window.awxDoCheckerCompare = doCheckerCompare;

        // 확인자: 최종 결정 (APPROVED 또는 REJECTED) 전송
        async function submitDecision(decision, comment = '') {
            if (!state.loadedCheckerSession) return;
            const sid = state.loadedCheckerSession.sessionId;
            const checkerIp = state.loadedCheckerSession.checker?.ip || state.agentInfo.ip || '';
            const cmpRes = state.lastCompareResult || { isAllMatch: false, detailHosts: [], incidentHosts: [], targetHosts: [] };

            const payload = {
                sessionId: sid,
                checkerIp: checkerIp,
                checkerData: {
                    detailHosts: cmpRes.detailHosts,
                    incidentHosts: cmpRes.incidentHosts,
                    targetHosts: cmpRes.targetHosts,
                    isAllMatch: cmpRes.isAllMatch,
                    decision: decision,
                    decisionComment: comment
                }
            };

            try {
                const res = await agentFetch(`${AGENT_BASE}/api/session/decision`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                const resData = await res.json();
                if (!res.ok) throw new Error(resData.error || "결정 제출 실패");

                alert(decision === 'APPROVED' ? "크로스체크가 승인 완료되었습니다." : "작업자에게 반려 요청이 전송되었습니다.");
                fetchPendingSessions();
                loadCheckerSession(sid);
            } catch (err) {
                alert("결정 제출 실패: " + err.message);
            }
        }
        window.awxSubmitDecision = submitDecision;

        // 초기화 시 실행
        updateCountsAndButton();
        renderAwxSummary();
        applyPinState();
        applyOpacity(false);

        // 에이전트 연결 시도
        fetchUsersAndStatus();
        if (btnCheckAgent) btnCheckAgent.onclick = fetchUsersAndStatus;
    }

    bindEvents(document, false, null);
})();
