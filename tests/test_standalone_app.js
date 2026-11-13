const http = require('http');
const fs = require('fs');
const path = require('path');
const { spawn, execSync } = require('child_process');

const DEBUG_PORT = 9238;
const USER_DATA_DIR = path.join(__dirname, '..', '.test_standalone_profile');

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

class CDPClient {
    constructor(wsUrl) {
        this.wsUrl = wsUrl;
        this.ws = null;
        this.msgId = 1;
        this.callbacks = new Map();
    }

    async connect() {
        this.ws = new WebSocket(this.wsUrl);
        await new Promise((resolve, reject) => {
            this.ws.onopen = resolve;
            this.ws.onerror = reject;
        });

        this.ws.onmessage = (event) => {
            const msg = JSON.parse(event.data);
            if (msg.id && this.callbacks.has(msg.id)) {
                const cb = this.callbacks.get(msg.id);
                this.callbacks.delete(msg.id);
                if (msg.error) cb.reject(msg.error);
                else cb.resolve(msg.result);
            } else if (msg.method === 'Runtime.exceptionThrown') {
                console.error('[BROWSER EXCEPTION]', msg.params.exceptionDetails?.text, msg.params.exceptionDetails?.exception?.description);
            } else if (msg.method === 'Runtime.consoleAPICalled') {
                console.log('[BROWSER LOG]', ...msg.params.args.map(a => a.value || a.description));
            } else if (msg.method === 'Page.javascriptDialogOpening') {
                this.send('Page.handleJavaScriptDialog', { accept: true }).catch(() => {});
            }
        };
    }

    send(method, params = {}) {
        return new Promise((resolve, reject) => {
            const id = this.msgId++;
            this.callbacks.set(id, { resolve, reject });
            this.ws.send(JSON.stringify({ id, method, params }));
        });
    }

    async evaluate(expression) {
        const res = await this.send('Runtime.evaluate', {
            expression,
            returnByValue: true,
            awaitPromise: true
        });
        if (res.exceptionDetails) {
            throw new Error(`CDP Eval Exception: ${res.exceptionDetails.text} - ${JSON.stringify(res.exceptionDetails.exception)}`);
        }
        return res.result ? res.result.value : undefined;
    }

    close() {
        if (this.ws) {
            try { this.ws.close(); } catch(e) {}
        }
    }
}

async function createMockAwxServer() {
    let forceDirect401 = false;
    const inv3Hosts = Array.from({ length: 1000 }, (_, i) => {
        const num = i + 1;
        if (num >= 295 && num <= 300) {
            return { name: `prd-db-${String(num).padStart(4, '0')}` };
        }
        return { name: `prd-web-${String(num).padStart(4, '0')}` };
    });
    const validBasic = 'Basic ' + Buffer.from('uiuser:uipass123').toString('base64');
    const server = http.createServer((req, res) => {
        const u = new URL(req.url, 'http://127.0.0.1');
        if (u.pathname.startsWith('/api/')) {
            const ua = String(req.headers['user-agent'] || '');
            if (forceDirect401 && ua.includes('Chrome/124.0.0.0') && !ua.includes('Edg/')) {
                res.writeHead(401, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ detail: 'Simulated Direct API 401 failure' }));
                return;
            }
            const auth = req.headers.authorization || '';
            if (auth && auth !== 'Bearer init-tok-1000' && auth !== validBasic) {
                res.writeHead(401, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ detail: 'Invalid credentials' }));
                return;
            }
        }
        if (u.pathname === '/api/v2/inventories/') {
            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({
                count: 5,
                next: null,
                results: [
                    { id: 1, name: 'DEFAULT', kind: '', total_hosts: 1 },
                    { id: 3, name: 'Test 1000 Hosts Inventory', kind: '', total_hosts: 1000 },
                    { id: 15, name: 'Default Inventory', kind: '', total_hosts: 25 },
                    { id: 42, name: 'Smart Hosts', kind: 'smart', total_hosts: 4 },
                    { id: 77, name: 'Demo Inventory', kind: '', total_hosts: 2 }
                ]
            }));
            return;
        }
        if (u.pathname === '/api/v2/inventories/3/hosts/') {
            const page = parseInt(u.searchParams.get('page') || '1', 10);
            const pageSize = parseInt(u.searchParams.get('page_size') || '200', 10);
            const start = (page - 1) * pageSize;
            const slice = inv3Hosts.slice(start, start + pageSize);
            const next = (start + pageSize) < inv3Hosts.length
                ? `/api/v2/inventories/3/hosts/?page=${page + 1}&page_size=${pageSize}`
                : null;
            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ count: inv3Hosts.length, next, results: slice }));
            return;
        }
        if (u.pathname === '/api/v2/inventories/77/hosts/') {
            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({
                count: 2,
                next: null,
                results: [{ name: 'demo-only-01' }, { name: 'demo-only-02' }]
            }));
            return;
        }
        if (u.pathname === '/api/v2/hosts/') {
            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({
                count: 3,
                next: null,
                results: [{ name: 'all-inv-leak-01' }, { name: 'all-inv-leak-02' }, { name: 'demo-only-01' }]
            }));
            return;
        }
        if (u.pathname === '/api/v2/inventories/15/hosts/') {
            const page = parseInt(u.searchParams.get('page') || '1', 10);
            const pageSize = parseInt(u.searchParams.get('page_size') || '200', 10);
            res.writeHead(200, { 'Content-Type': 'application/json' });
            if (pageSize === 5) {
                res.end(JSON.stringify({ count: 25, next: null, results: [{ name: 'inv15-p1-01' }, { name: 'inv15-p1-02' }] }));
                return;
            }
            if (page === 2 && pageSize === 20) {
                const p2 = Array.from({ length: 20 }, (_, i) => ({ name: `inv15-p2-${String(i + 1).padStart(2, '0')}` }));
                res.end(JSON.stringify({ count: 25, next: null, results: p2 }));
                return;
            }
            if (page === 1) {
                const p1 = Array.from({ length: 20 }, (_, i) => ({ name: `inv15-h-${String(i + 1).padStart(2, '0')}` }));
                res.end(JSON.stringify({ count: 25, next: '/api/v2/inventories/15/hosts/?page=2&page_size=200', results: p1 }));
                return;
            }
            const p2All = Array.from({ length: 5 }, (_, i) => ({ name: `inv15-h-${String(i + 21).padStart(2, '0')}` }));
            res.end(JSON.stringify({ count: 25, next: null, results: p2All }));
            return;
        }
        if (u.pathname === '/api/v2/inventories/42/hosts/') {
            const page = parseInt(u.searchParams.get('page') || '1', 10);
            res.writeHead(200, { 'Content-Type': 'application/json' });
            if (page === 1) {
                res.end(JSON.stringify({
                    count: 4,
                    next: '/api/v2/inventories/42/hosts/?page=2',
                    results: [{ name: 'sm42-api-01' }, { name: 'sm42-api-02' }]
                }));
            } else {
                res.end(JSON.stringify({
                    count: 4,
                    next: null,
                    results: [{ name: 'sm42-api-03' }, { name: 'prd-db-0296' }]
                }));
            }
            return;
        }
        res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
        res.end(`<!doctype html><html><head><title>AWX Mock Console</title></head><body>
            <table><tbody>
                <tr><td><input type="checkbox"/></td><td data-label="Name"><a href="#/hosts/1">sm42-dom-01</a></td></tr>
                <tr><td><input type="checkbox"/></td><td data-label="Name"><a href="#/hosts/2">sm42-dom-02</a></td></tr>
                <tr><td><input type="checkbox"/></td><td data-label="Name"><a href="#/hosts/3">prd-db-0295</a></td></tr>
            </tbody></table>
        </body></html>`);
    });
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
    const port = server.address().port;
    return {
        server,
        baseUrl: `http://127.0.0.1:${port}`,
        setForceDirect401: (v) => { forceDirect401 = !!v; }
    };
}

async function runStandaloneAppTest() {
    console.log("===============================================================================");
    console.log("  >>> Standalone Desktop App (host-cross-checker.exe) E2E Validation <<<");
    console.log("===============================================================================");

    // 1. Cleanup old processes and temporary dist/config.json
    try {
        execSync('taskkill /F /IM host-cross-checker.exe', { stdio: 'ignore' });
    } catch(e) {}
    try {
        execSync('taskkill /F /IM cross-checker-agent.exe', { stdio: 'ignore' });
    } catch(e) {}
    await sleep(500);

    const exePath = path.join(__dirname, '..', 'dist', 'host-cross-checker.exe');
    const distConfigPath = path.join(path.dirname(exePath), 'config.json');
    const hadDistConfig = fs.existsSync(distConfigPath);
    const prevDistConfig = hadDistConfig ? fs.readFileSync(distConfigPath, 'utf8') : null;
    if (hadDistConfig) fs.rmSync(distConfigPath, { force: true });

    const mockAwx = await createMockAwxServer();
    const mockAwxBase = mockAwx.baseUrl;

    const testShareDir = path.join(__dirname, '..', 'test_share');
    fs.rmSync(path.join(testShareDir, 'sessions'), { recursive: true, force: true });
    fs.mkdirSync(testShareDir, { recursive: true });
    fs.mkdirSync(path.join(testShareDir, 'sessions'), { recursive: true });

    const initialUsers = {
        updatedAt: new Date().toISOString(),
        users: [
            { name: "김철수", ip: "192.168.0.23", role: "작업자" },
            { name: "이영희", ip: "192.168.0.102", role: "확인자" },
            { name: "박민수", ip: "192.168.0.105", role: "확인자" },
            { name: "정관리", ip: "192.168.0.58", role: "엔지니어" },
            { name: "최신규", ip: "192.168.0.199", role: "확인자" },
            { name: "오프라인확인자", ip: "192.0.2.1", role: "작업자" }
        ]
    };
    fs.writeFileSync(path.join(testShareDir, 'users.json'), JSON.stringify(initialUsers, null, 2), 'utf8');

    // 2. Launch host-cross-checker.exe in normal mode (NO -headless!)
    if (!fs.existsSync(exePath)) {
        throw new Error(`Executable not found at ${exePath}. Run build.py first!`);
    }

    console.log(`[1/7] Spawning ${path.basename(exePath)} with auto-launch app mode enabled...`);
    const appEnv = Object.assign({}, process.env, {
        CROSS_CHECKER_DEBUG_PORT: String(DEBUG_PORT),
        CROSS_CHECKER_PROFILE_DIR: USER_DATA_DIR,
        AWX_BASE_URL: mockAwxBase,
        AWX_TOKEN: 'init-tok-1000',
        AWX_DEFAULT_SCOPE: 'inventory:3'
    });

    const appProc = spawn(exePath, [
        '-port=18080',
        `-share=${testShareDir}`,
        '-bind-ip=192.168.0.23',
        '-open-app'
    ], {
        env: appEnv,
        stdio: 'pipe'
    });

    let procExited = false;
    let procExitCode = null;
    appProc.on('exit', (code) => {
        procExited = true;
        procExitCode = code;
    });

    appProc.stdout.on('data', d => console.log('   [APP STDOUT]', d.toString().trim()));
    appProc.stderr.on('data', d => console.log('   [APP STDERR]', d.toString().trim()));

    // 3. Connect to the auto-launched Edge/Chrome --app window via CDP
    console.log(`[2/7] Waiting for auto-launched desktop app window on debug port ${DEBUG_PORT}...`);
    let pageTarget = null;
    for (let i = 0; i < 35; i++) {
        await sleep(300);
        try {
            const res = await fetch(`http://127.0.0.1:${DEBUG_PORT}/json`);
            if (res.ok) {
                const targets = await res.json();
                pageTarget = targets.find(t => t.type === 'page' && (t.url.includes('127.0.0.1:18080') || t.url.includes('localhost:18080')));
                if (pageTarget) break;
            }
        } catch(e) {}
    }

    if (!pageTarget) {
        throw new Error("Failed to connect to auto-launched --app window on port " + DEBUG_PORT);
    }
    console.log("✓ Connected to desktop app window:", pageTarget.title, `(${pageTarget.url})`);

    const cdp = new CDPClient(pageTarget.webSocketDebuggerUrl);
    await cdp.connect();
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');
    await sleep(800);

    // Disable blocking alert dialogs
    await cdp.evaluate(`
        window.alert = function(msg) {
            console.log('[BROWSER ALERT]', msg);
            window.__lastAlertMsg = msg;
        };
    `);

    try {
        // [3/7] Verify standalone environment & flush window UI
        console.log("[3/7] Verifying standalone layout, embedded assets & header controls...");
        const envInfo = await cdp.evaluate(`
            (() => {
                const panel = document.getElementById('awx-validator-panel');
                const detachBtn = document.getElementById('awx-vld-btn-detach');
                const closeBtn = document.getElementById('awx-vld-btn-close');
                const userBadge = document.getElementById('awx-vld-user-badge');
                const userBadgeName = document.getElementById('awx-vld-user-badge-name');
                const userBadgeDot = document.getElementById('awx-vld-user-badge-dot');
                const globalStatusBar = document.getElementById('awx-vld-global-status-bar');
                const helpWorker = document.querySelector('.awx-vld-tab-help[data-help="worker"]');
                const helpChecker = document.querySelector('.awx-vld-tab-help[data-help="checker"]');
                const tabs = Array.from(document.querySelectorAll('.awx-vld-tab-btn')).map(b => b.innerText.trim());
                const isStandalone = Boolean(window.__AWX_STANDALONE_APP__);
                const panelStyle = panel ? window.getComputedStyle(panel) : null;
                return {
                    isStandalone,
                    hasDetachBtn: !!detachBtn,
                    hasCloseBtn: !!closeBtn,
                    hasUserBadge: !!userBadge && !!userBadgeDot,
                    userName: userBadgeName?.innerText,
                    hasGlobalStatusBar: !!globalStatusBar,
                    hasHelpWorker: !!helpWorker,
                    hasHelpChecker: !!helpChecker,
                    tabCount: tabs.length,
                    tabs,
                    panelWidth: panelStyle?.width,
                    panelPosition: panelStyle?.position
                };
            })()
        `);
        console.log("   Standalone layout info:", envInfo);

        if (!envInfo.isStandalone) {
            throw new Error("window.__AWX_STANDALONE_APP__ is not true in standalone window");
        }
        if (envInfo.hasCloseBtn || envInfo.hasDetachBtn) {
            throw new Error("Close button or Detach/Refresh button unexpectedly present in header");
        }
        if (!envInfo.hasUserBadge) {
            throw new Error("User badge missing in header");
        }
        if (!envInfo.hasHelpWorker || !envInfo.hasHelpChecker) {
            throw new Error("Workflow [?] help buttons missing on Tab 2 / Tab 3");
        }
        if (!envInfo.hasGlobalStatusBar) {
            throw new Error("Global 6 o'clock status bar element missing");
        }
        if (envInfo.tabCount !== 3) {
            throw new Error(`Expected 3 tabs, got: ${envInfo.tabCount}`);
        }
        if (!envInfo.tabs[0].includes('1. 작업 전 자체확인') ||
            !envInfo.tabs[1].includes('2. 작업자 요청/승인대기') ||
            !envInfo.tabs[2].includes('3. 확인자 검증/승인')) {
            throw new Error(`Tab labels do not match expected guide names: ${JSON.stringify(envInfo.tabs)}`);
        }
        console.log("✓ Standalone UI validated: clean header with live user badge, 3 guide tabs with [?] icons, responsive layout, global status bar.");

        // [3.5/7] Verify standalone window auto-collapse on mouseleave and expand on mouseenter
        console.log("   Testing standalone window physical resizing (resizeTo) on mouseleave/mouseenter...");
        const windowResizeResult = await cdp.evaluate(`
            (async () => {
                const initialH = window.outerHeight;
                const initialW = window.outerWidth;
                const bodyWrap = document.getElementById('awx-vld-body-wrap');

                // 1. Mouse leave -> window collapses physically
                document.documentElement.dispatchEvent(new MouseEvent('mouseleave'));
                await new Promise(r => setTimeout(r, 450));
                const collapsedH = window.outerHeight;
                const isCollapsed = window.awxWindowCollapsed === true;
                const bodyOverflowWhenCollapsed = bodyWrap ? bodyWrap.style.overflowY : '';
                const bodyMaxHeightWhenCollapsed = bodyWrap ? bodyWrap.style.maxHeight : '';

                // 2. Mouse enter -> window restores to original height
                document.documentElement.dispatchEvent(new MouseEvent('mouseenter'));
                await new Promise(r => setTimeout(r, 450));
                const restoredH = window.outerHeight;
                const isRestored = window.awxWindowCollapsed === false;

                // 3. Test settings modal slider adjustment (50%)
                const btnSettings = document.getElementById('awx-vld-btn-header-settings');
                btnSettings.click();
                const rangeCollapse = document.getElementById('awx-vld-range-collapse');
                rangeCollapse.value = '50';
                rangeCollapse.dispatchEvent(new Event('input'));
                document.documentElement.dispatchEvent(new MouseEvent('mouseleave'));
                await new Promise(r => setTimeout(r, 450));
                const collapsed50H = window.outerHeight;

                // Restore slider to 30% and close modal
                rangeCollapse.value = '30';
                rangeCollapse.dispatchEvent(new Event('input'));
                btnSettings.click();
                document.documentElement.dispatchEvent(new MouseEvent('mouseenter'));
                await new Promise(r => setTimeout(r, 450));

                return {
                    initialH,
                    initialW,
                    collapsedH,
                    isCollapsed,
                    bodyOverflowWhenCollapsed,
                    bodyMaxHeightWhenCollapsed,
                    restoredH,
                    isRestored,
                    collapsed50H
                };
            })()
        `);
        console.log("   Window auto-collapse test result:", windowResizeResult);

        if (!windowResizeResult.isCollapsed || windowResizeResult.collapsedH >= windowResizeResult.initialH - 100) {
            throw new Error(`Window did not shrink on mouseleave: initialH=${windowResizeResult.initialH}, collapsedH=${windowResizeResult.collapsedH}`);
        }
        if (windowResizeResult.bodyMaxHeightWhenCollapsed !== '' || windowResizeResult.bodyOverflowWhenCollapsed !== 'auto') {
            throw new Error(`Inner content was artificially masked with overflow/maxHeight: ${JSON.stringify(windowResizeResult)}`);
        }
        if (!windowResizeResult.isRestored || windowResizeResult.restoredH < 600) {
            throw new Error(`Window did not expand back on mouseenter: initialH=${windowResizeResult.initialH}, restoredH=${windowResizeResult.restoredH}`);
        }
        console.log("✓ Physical window resizing verified: window collapses to mini height on mouseleave without masking content, and fully expands on mouseenter.");

        //Helper: check zero horizontal overflow & unclipped visible UI elements
        async function assertNoLayoutClipping(stage) {
            const audit = await cdp.evaluate(`
                (() => {
                    const vw = window.innerWidth;
                    const vh = window.innerHeight;
                    const docOverflowX = document.documentElement.scrollWidth - document.documentElement.clientWidth;
                    const headerIds = ['awx-vld-user-badge', 'awx-vld-btn-header-settings', 'awx-vld-btn-pin'];
                    const clippedHeaders = [];
                    for (const id of headerIds) {
                        const el = document.getElementById(id);
                        if (!el) { clippedHeaders.push(id + ':missing'); continue; }
                        const r = el.getBoundingClientRect();
                        if (r.left < -0.5 || r.right > vw + 0.5 || r.width < 10) {
                            clippedHeaders.push(id + ':' + JSON.stringify({ left: r.left, right: r.right, vw, width: r.width }));
                        }
                    }
                    const clippedTabs = [];
                    for (const btn of document.querySelectorAll('.awx-vld-tab-btn')) {
                        const r = btn.getBoundingClientRect();
                        if (r.left < -0.5 || r.right > vw + 0.5 || btn.scrollWidth > btn.clientWidth + 2) {
                            clippedTabs.push(btn.id + ':' + JSON.stringify({ left: r.left, right: r.right, vw, sw: btn.scrollWidth, cw: btn.clientWidth }));
                        }
                    }
                    const statusBar = document.getElementById('awx-vld-global-status-bar');
                    let clippedStatusBar = null;
                    if (statusBar && window.getComputedStyle(statusBar).display !== 'none') {
                        const r = statusBar.getBoundingClientRect();
                        if (r.right > vw + 0.5 || r.bottom > vh + 0.5 || statusBar.scrollWidth > statusBar.clientWidth + 2) {
                            clippedStatusBar = { left: r.left, right: r.right, bottom: r.bottom, vw, vh, sw: statusBar.scrollWidth, cw: statusBar.clientWidth };
                        }
                    }
                    const settingsPanel = document.getElementById('awx-vld-settings-panel');
                    let clippedSettings = null;
                    if (settingsPanel && window.getComputedStyle(settingsPanel).display !== 'none') {
                        const r = settingsPanel.getBoundingClientRect();
                        if (r.right > vw + 0.5 || r.bottom > vh + 0.5) {
                            clippedSettings = { right: r.right, bottom: r.bottom, vw, vh };
                        }
                        for (const sid of ['awx-vld-btn-test-self', 'awx-vld-btn-test-notify']) {
                            const sb = document.getElementById(sid);
                            if (!sb) { clippedSettings = { missing: sid }; break; }
                            const sr = sb.getBoundingClientRect();
                            if (sr.left < -0.5 || sr.right > vw + 0.5 || sr.bottom > vh + 0.5 || sr.width < 20) {
                                clippedSettings = { btn: sid, rect: { left: sr.left, right: sr.right, bottom: sr.bottom }, vw, vh };
                                break;
                            }
                        }
                    }
                    return { vw, vh, docOverflowX, clippedHeaders, clippedTabs, clippedStatusBar, clippedSettings };
                })()
            `);
            if (audit.docOverflowX > 1 || audit.clippedHeaders.length > 0 || audit.clippedTabs.length > 0 || audit.clippedStatusBar || audit.clippedSettings) {
                throw new Error(`Layout clipping detected at [${stage}]: ${JSON.stringify(audit)}`);
            }
            console.log(`   [Layout OK] ${stage}: vw=${audit.vw}, vh=${audit.vh}, overflowX=${audit.docOverflowX}`);
        }

        // [3.6/7] Verify OS-level Always-on-Top (상단 고정 / WS_EX_TOPMOST) toggle & collapse persistence
        console.log("   [3.6/7] Testing OS-level Always-on-Top (상단 고정 -> WS_EX_TOPMOST)...");
        const pinBefore = await (await fetch('http://127.0.0.1:18080/api/window/pin')).json();
        if (!pinBefore.found || pinBefore.pinned) {
            throw new Error(`Expected initial OS pin found=true & pinned=false, got: ${JSON.stringify(pinBefore)}`);
        }
        await cdp.evaluate(`document.getElementById('awx-vld-btn-pin').click()`);
        let pinAfter = null;
        for (let i = 0; i < 15; i++) {
            await sleep(100);
            pinAfter = await (await fetch('http://127.0.0.1:18080/api/window/pin')).json();
            if (pinAfter.pinned) break;
        }
        const pinUiText = await cdp.evaluate(`document.getElementById('awx-vld-pin-text')?.innerText || ''`);
        if (!pinAfter || !pinAfter.found || !pinAfter.pinned || pinUiText !== '상단 고정됨') {
            throw new Error(`OS topmost pin did not activate: ui=${pinUiText}, os=${JSON.stringify(pinAfter)}`);
        }

        // Verify topmost stays active across physical window collapse/expand
        await cdp.evaluate(`document.documentElement.dispatchEvent(new MouseEvent('mouseleave'));`);
        await sleep(500);
        const pinCollapsed = await (await fetch('http://127.0.0.1:18080/api/window/pin')).json();
        await cdp.evaluate(`document.documentElement.dispatchEvent(new MouseEvent('mouseenter'));`);
        await sleep(500);
        if (!pinCollapsed.pinned) {
            throw new Error(`OS topmost was lost when window collapsed: ${JSON.stringify(pinCollapsed)}`);
        }

        // Unpin and verify WS_EX_TOPMOST is cleared
        await cdp.evaluate(`document.getElementById('awx-vld-btn-pin').click()`);
        let pinCleared = null;
        for (let i = 0; i < 15; i++) {
            await sleep(100);
            pinCleared = await (await fetch('http://127.0.0.1:18080/api/window/pin')).json();
            if (!pinCleared.pinned) break;
        }
        if (!pinCleared || pinCleared.pinned) {
            throw new Error(`OS topmost did not clear on second click: ${JSON.stringify(pinCleared)}`);
        }
        console.log("✓ OS-level Always-on-Top (상단 고정) verified via Win32 WS_EX_TOPMOST (toggle ON -> collapse -> toggle OFF).");

        // [3.7/7] Verify [?] Workflow SVG Popovers render unclipped inside viewport
        console.log("   [3.7/7] Testing [?] workflow SVG hover popovers for zero clipping...");
        for (const helpType of ['worker', 'checker']) {
            const popCheck = await cdp.evaluate(`
                (async () => {
                    const btn = document.querySelector('.awx-vld-tab-help[data-help="${helpType}"]');
                    btn.dispatchEvent(new MouseEvent('mouseenter'));
                    await new Promise(r => setTimeout(r, 80));
                    const pop = document.getElementById('awx-vld-workflow-popover');
                    const svg = pop.querySelector('svg');
                    const popR = pop.getBoundingClientRect();
                    const svgR = svg ? svg.getBoundingClientRect() : null;
                    const txt = svg ? svg.textContent : '';
                    const computedDisplay = window.getComputedStyle(pop).display;
                    btn.dispatchEvent(new MouseEvent('mouseleave'));
                    const hiddenAfterLeave = (window.getComputedStyle(pop).display === 'none');
                    return {
                        vw: window.innerWidth,
                        vh: window.innerHeight,
                        visible: computedDisplay === 'block',
                        hiddenAfterLeave,
                        popRect: { left: popR.left, right: popR.right, top: popR.top, bottom: popR.bottom, width: popR.width, height: popR.height },
                        svgRect: svgR ? { left: svgR.left, right: svgR.right, top: svgR.top, bottom: svgR.bottom, width: svgR.width, height: svgR.height } : null,
                        txt
                    };
                })()
            `);
            if (!popCheck.visible || !popCheck.hiddenAfterLeave || !popCheck.svgRect) {
                throw new Error(`[?] ${helpType} popover/SVG not visible: ${JSON.stringify(popCheck)}`);
            }
            const { vw, vh, popRect: p, svgRect: s } = popCheck;
            if (p.left < 0 || p.right > vw + 0.5 || p.top < 0 || p.bottom > vh + 0.5 ||
                s.left < p.left - 0.5 || s.right > p.right + 0.5 || s.top < p.top - 0.5 || s.bottom > p.bottom + 0.5 ||
                s.width < 350 || s.height < 180) {
                throw new Error(`[?] ${helpType} SVG is clipped or undersized: ${JSON.stringify(popCheck)}`);
            }
            if (helpType === 'worker' && !popCheck.txt.includes('[크로스체크 확인 요청 발송]')) {
                throw new Error(`Worker SVG label mismatch: ${popCheck.txt}`);
            }
            if (helpType === 'checker' && !popCheck.txt.includes('[교차 비교 실행]')) {
                throw new Error(`Checker SVG label mismatch: ${popCheck.txt}`);
            }
            console.log(`   [SVG OK] ${helpType}: svg=${Math.round(s.width)}x${Math.round(s.height)} inside pop=${Math.round(p.width)}x${Math.round(p.height)} (vw=${vw})`);
        }

        // Settings modal layout check
        await cdp.evaluate(`document.getElementById('awx-vld-btn-header-settings').click()`);
        await sleep(150);
        await assertNoLayoutClipping('Settings Modal Open');
        await cdp.evaluate(`document.getElementById('awx-vld-btn-header-settings').click()`);
        await sleep(150);

        // [4/7] Verify AWX 1,000 hosts one-click collection via backend endpoint
        console.log("[4/7] Testing Tab 1: One-click 1,000 AWX hosts collection via Go backend...");
        await cdp.evaluate(`document.getElementById('awx-vld-tab-single').click()`);
        await sleep(300);

        // Click [현재 인벤토리 전체 수집]
        console.log("   Clicking [현재 인벤토리 전체 수집] in standalone window...");
        const collectedCount = await cdp.evaluate(`window.awxRunCollectAll()`);
        const summaryText = await cdp.evaluate(`document.getElementById('awx-vld-awx-summary').innerText`);
        console.log(`   Collection finished: ${collectedCount} hosts collected.`);
        console.log(`   Summary text: "${summaryText.replace(/\n/g, ' ')}"`);

        if (collectedCount !== 994 || !summaryText.includes("1000대 중 6대 제외됨") || !summaryText.includes("직접 API")) {
            throw new Error(`AWX collection in standalone app failed. Count=${collectedCount}, summary=${summaryText}`);
        }
        console.log("✓ One-click collection succeeded: 1,000 hosts from AWX inventory #3 parsed, 6 default excluded, 994 collected via Direct API.");

        // [4.5/7] Verify custom AWX Settings (URL paste + API collection + DPAPI + Fallback + Prefer Bridge) & Live Open AWX Browser Tab Bridge
        console.log("[4.5/7] Testing Settings AWX URL/API configuration, P1-1..P2-2 & Live Edge AWX Tab Bridge...");

        let awxTabCdp = null;
        let awxTabTargetId = null;
        try {
            // A) Open Settings, verify scheme-less URL parse + full pasted URL auto-parse & Test/Save + Collect + DPAPI/no-localStorage secrets
            await cdp.evaluate(`document.getElementById('awx-vld-btn-open-awx-settings').click()`);
            await sleep(250);
            await cdp.evaluate(`
                (() => {
                    const u = document.getElementById('awx-vld-awx-url');
                    u.value = 'awxhost/#/inventories/inventory/33/hosts?page=3';
                    u.dispatchEvent(new Event('input'));
                })()
            `);
            await sleep(80);
            const schemelessParsed = await cdp.evaluate(`({
                scope: document.getElementById('awx-vld-awx-scope').value,
                page: document.getElementById('awx-vld-awx-page').value,
                hasOpenBtn: !!document.getElementById('awx-vld-btn-awx-open')
            })`);
            if (schemelessParsed.scope !== 'inventory:33' || schemelessParsed.page !== '3' || !schemelessParsed.hasOpenBtn) {
                throw new Error(`AWX scheme-less parse or AWX open button check failed: ${JSON.stringify(schemelessParsed)}`);
            }
            const pastedUrl = `${mockAwxBase}/#/inventories/inventory/15/hosts?page=2`;
            await cdp.evaluate(`
                (() => {
                    const u = document.getElementById('awx-vld-awx-url');
                    u.value = ${JSON.stringify(pastedUrl)};
                    u.dispatchEvent(new Event('input'));
                    document.getElementById('awx-vld-awx-user').value = 'uiuser';
                    document.getElementById('awx-vld-awx-pass').value = 'uipass123';
                    document.getElementById('awx-vld-awx-token').value = '';
                })()
            `);
            await sleep(100);
            const parsedFields = await cdp.evaluate(`({
                scope: document.getElementById('awx-vld-awx-scope').value,
                page: document.getElementById('awx-vld-awx-page').value
            })`);
            if (parsedFields.scope !== 'inventory:15' || parsedFields.page !== '2') {
                throw new Error(`AWX URL paste auto-parse failed: ${JSON.stringify(parsedFields)}`);
            }
            await cdp.evaluate(`document.getElementById('awx-vld-btn-awx-save').click()`);
            for (let i = 0; i < 20; i++) {
                await sleep(150);
                const r = await cdp.evaluate(`document.getElementById('awx-vld-awx-test-result')?.innerText || ''`);
                if (r.includes('연결 성공')) break;
            }
            const saveTestMsg = await cdp.evaluate(`document.getElementById('awx-vld-awx-test-result')?.innerText || ''`);
            if (!saveTestMsg.includes('연결 성공')) {
                throw new Error(`Settings AWX test & save failed: ${saveTestMsg}`);
            }
            // P1-5: Inventory list source label check
            const invSourceLabel = await cdp.evaluate(`document.getElementById('awx-vld-awx-inv-source')?.innerText || ''`);
            if (!invSourceLabel.includes('직접 API')) {
                throw new Error(`Expected P1-5 '직접 API' source label, got: ${invSourceLabel}`);
            }
            // P2-1 & P2-2: Ensure DPAPI ciphertext persisted on disk and NO plaintext secrets in config.json or localStorage
            if (!fs.existsSync(distConfigPath)) {
                throw new Error(`Expected DPAPI config file at ${distConfigPath}`);
            }
            const diskCfgTxt = fs.readFileSync(distConfigPath, 'utf8');
            if (!diskCfgTxt.includes('"awxPasswordEnc"') || diskCfgTxt.includes('uipass123') || diskCfgTxt.includes('init-tok-1000')) {
                throw new Error(`P2-1 config.json check failed: ${diskCfgTxt}`);
            }
            const lsRaw = await cdp.evaluate(`localStorage.getItem('awx_validator_direct_config_v1') || ''`);
            if (lsRaw.includes('uipass123') || lsRaw.includes('"password"') || lsRaw.includes('"token"')) {
                throw new Error(`P2-2 localStorage leaked secret fields: ${lsRaw}`);
            }

            await assertNoLayoutClipping('Settings Modal AWX Configured');
            await cdp.evaluate(`document.getElementById('awx-vld-btn-close-settings').click()`);
            await sleep(150);

            // Collect configured page 2 (20 hosts) via [현재 화면 수집] (Direct API)
            await cdp.evaluate(`document.getElementById('awx-vld-btn-page').click()`);
            await sleep(400);
            const page2Cnt = await cdp.evaluate(`window.awxValidatorState.collectedHosts.size`);
            const page2Summary = await cdp.evaluate(`document.getElementById('awx-vld-awx-summary')?.innerText || ''`);
            if (page2Cnt !== 20 || !page2Summary.includes('직접 API')) {
                throw new Error(`Expected 20 hosts via Direct API on page 2, got cnt=${page2Cnt} summary=${page2Summary}`);
            }

            // Collect full inventory #15 (25 hosts across 2 pages) via [현재 인벤토리 전체 수집] (Direct API)
            const inv15Cnt = await cdp.evaluate(`window.awxRunCollectAll()`);
            if (inv15Cnt !== 25) {
                throw new Error(`Expected 25 hosts from custom AWX inventory #15, got ${inv15Cnt}`);
            }
            console.log("   Verified P1-1/P1-5/P2-1/P2-2: Direct API first, DPAPI config.json, zero localStorage secrets, page=2 (20) & inv #15 (25).");

            // B) Open a real second Edge tab with mock AWX console & run the generated bridge bookmarklet
            const bridgeHref = await cdp.evaluate(`document.getElementById('awx-vld-btn-bridge-bookmark')?.getAttribute('href') || ''`);
            if (!bridgeHref.startsWith('javascript:')) {
                throw new Error(`Expected javascript: bookmarklet href, got: ${bridgeHref.slice(0, 60)}`);
            }
            const createdTarget = await cdp.send('Target.createTarget', {
                url: `${mockAwxBase}/#/inventories/smart_inventory/42/hosts?page=1`
            });
            awxTabTargetId = createdTarget.targetId;

            let awxTarget = null;
            for (let i = 0; i < 20; i++) {
                await sleep(200);
                const tList = await (await fetch(`http://127.0.0.1:${DEBUG_PORT}/json`)).json();
                awxTarget = tList.find(t => t.id === awxTabTargetId);
                if (awxTarget && awxTarget.webSocketDebuggerUrl) break;
            }
            if (!awxTarget) throw new Error("Failed to open mock AWX browser tab via CDP");
            awxTabCdp = new CDPClient(awxTarget.webSocketDebuggerUrl);
            await awxTabCdp.connect();
            await awxTabCdp.send('Runtime.enable');
            await sleep(500);

            // Inject the exact generated bookmarklet JS (strip leading 'javascript:')
            const bridgeJS = bridgeHref.slice('javascript:'.length);
            await awxTabCdp.evaluate(bridgeJS);

            let bridgeConnectedUI = false;
            for (let i = 0; i < 25; i++) {
                await sleep(150);
                const brgOk = await cdp.evaluate(`({
                    src: document.getElementById('awx-vld-awx-source-label')?.innerText || '',
                    conn: !!window.awxValidatorState?.awxBridge?.connected,
                    sid: window.awxValidatorState?.awxBridge?.scopeId
                })`);
                if (brgOk.conn && brgOk.sid === '42' && brgOk.src.includes('AWX 탭 대기(1개)')) {
                    bridgeConnectedUI = true;
                    break;
                }
            }
            if (!bridgeConnectedUI) {
                const dbg = await cdp.evaluate(`document.getElementById('awx-vld-awx-source-label')?.innerText || ''`);
                throw new Error(`Standalone app did not reflect live bridged AWX tab: ${dbg}`);
            }

            // P1-2 & P1-1: Verify bridge count label, Direct->Bridge fallback on 401, P1-5 inventory list fallback label, and dual error display
            const brCountTxt = await cdp.evaluate(`document.getElementById('awx-vld-awx-bridge-status')?.innerText || ''`);
            if (!brCountTxt.includes('연결됨 1개')) {
                throw new Error(`Expected P1-2 bridge count '연결됨 1개', got: ${brCountTxt}`);
            }
            mockAwx.setForceDirect401(true);
            const fallbackCnt = await cdp.evaluate(`window.awxRunCollectAll()`);
            const fallbackSummary = await cdp.evaluate(`document.getElementById('awx-vld-awx-summary')?.innerText || ''`);
            if (fallbackCnt !== 3 || !fallbackSummary.includes('AWX 탭')) {
                throw new Error(`Expected Direct 401 -> Bridge fallback with 3 hosts, got cnt=${fallbackCnt} summary=${fallbackSummary}`);
            }
            await cdp.evaluate(`document.getElementById('awx-vld-btn-open-awx-settings').click()`);
            await sleep(200);
            await cdp.evaluate(`document.getElementById('awx-vld-btn-awx-refresh-inv').click()`);
            await sleep(500);
            const fallbackInvLabel = await cdp.evaluate(`document.getElementById('awx-vld-awx-inv-source')?.innerText || ''`);
            if (!fallbackInvLabel.includes('목록: AWX 탭')) {
                throw new Error(`Expected P1-5 '목록: AWX 탭' fallback label, got: ${fallbackInvLabel}`);
            }
            await cdp.evaluate(`document.getElementById('awx-vld-btn-close-settings').click()`);
            await sleep(150);

            // Force bridged tab fetch to 401 too -> verify BOTH Direct & Bridge errors are displayed together
            await awxTabCdp.evaluate(`
                (() => {
                    window.__origFetch = window.fetch;
                    window.fetch = async (url, opts) => {
                        if (String(url).includes('/hosts/')) {
                            return new Response(JSON.stringify({ detail: 'Simulated Bridge 401' }), { status: 401, headers: { 'Content-Type': 'application/json' } });
                        }
                        return window.__origFetch(url, opts);
                    };
                })()
            `);
            const dualFailCnt = await cdp.evaluate(`window.awxRunCollectAll()`);
            const dualFailSummary = await cdp.evaluate(`document.getElementById('awx-vld-awx-summary')?.innerText || ''`);
            if (dualFailCnt !== 0 || !dualFailSummary.includes('직접 API 실패:') || !dualFailSummary.includes('AWX 탭 실패:')) {
                throw new Error(`Expected P1-1 dual failure message with both Direct & Bridge errors, got cnt=${dualFailCnt} summary=${dualFailSummary}`);
            }
            await awxTabCdp.evaluate(`if (window.__origFetch) { window.fetch = window.__origFetch; delete window.__origFetch; }`);
            mockAwx.setForceDirect401(false);

            // Enable "AWX 탭 우선" (preferBridge) so Case B exercises live bridged AWX tab DOM/API with healthy Direct API present
            await cdp.evaluate(`
                (() => {
                    const cb = document.getElementById('awx-vld-awx-prefer-bridge');
                    cb.checked = true;
                    cb.dispatchEvent(new Event('change'));
                })()
            `);
            await sleep(200);
            const preferBridgeSrc = await cdp.evaluate(`document.getElementById('awx-vld-awx-source-label')?.innerText || ''`);
            if (!preferBridgeSrc.includes('열린 AWX 탭 연결됨') || !preferBridgeSrc.includes('스마트 인벤토리 #42')) {
                throw new Error(`Expected '열린 AWX 탭 연결됨 · 스마트 인벤토리 #42' when preferBridge=true, got: ${preferBridgeSrc}`);
            }

            // Collect visible DOM screen from bridged AWX tab -> 3 DOM hosts, 1 excluded ('prd-db-0295') => 2 collected
            await cdp.evaluate(`document.getElementById('awx-vld-btn-page').click()`);
            await sleep(500);
            const brgPageRes = await cdp.evaluate(`({
                collected: Array.from(window.awxValidatorState.collectedHosts),
                excluded: window.awxValidatorState.excludedHosts,
                summary: document.getElementById('awx-vld-awx-summary')?.innerText || ''
            })`);
            if (brgPageRes.collected.length !== 2 || brgPageRes.excluded.length !== 1 || !brgPageRes.summary.includes('AWX 탭')) {
                throw new Error(`Expected 2 kept & 1 excluded via AWX 탭 DOM, got: ${JSON.stringify(brgPageRes)}`);
            }

            // Collect full smart_inventory #42 from bridged AWX tab API -> 4 total, 1 excluded ('prd-db-0296') => 3 collected
            const brgAllCnt = await cdp.evaluate(`window.awxRunCollectAll()`);
            if (brgAllCnt !== 3) {
                throw new Error(`Expected 3 kept hosts from bridged AWX full inventory #42, got ${brgAllCnt}`);
            }
            console.log("   Verified live open Edge AWX tab bridge: auto-detected smart_inventory #42, collected DOM screen (2/3) & full API (3/4).");

            // Reset Direct API credentials before Case C/D so pure bridge mode (#/inventories & #/hosts guards) is verified without Direct fallback masking
            await cdp.evaluate(`document.getElementById('awx-vld-btn-awx-reset').click()`);
            await sleep(300);

            // C) Switch bridged AWX tab to #/inventories list (screenshot case: DEFAULT, Default Inventory, Demo Inventory, Test 1000)
            //    Must NEVER scrape inventory names as hosts, must NEVER dump all inventories via /api/v2/hosts/,
            //    and picking Demo Inventory (#77) from Settings dropdown must collect ONLY Demo Inventory's hosts!
            await awxTabCdp.evaluate(`
                (() => {
                    document.body.innerHTML = '<table><tbody>' +
                        '<tr><td><input type="checkbox"/></td><td data-label="이름"><a href="#/inventories/inventory/1/details">DEFAULT</a></td><td data-label="유형">인벤토리</td></tr>' +
                        '<tr><td><input type="checkbox"/></td><td data-label="이름"><a href="#/inventories/inventory/15/details">Default Inventory</a></td><td data-label="유형">인벤토리</td></tr>' +
                        '<tr><td><input type="checkbox"/></td><td data-label="이름"><a href="#/inventories/inventory/77/details">Demo Inventory</a></td><td data-label="유형">인벤토리</td></tr>' +
                        '<tr><td><input type="checkbox"/></td><td data-label="이름"><a href="#/inventories/inventory/3/details">Test 1000 Hosts Inventory</a></td><td data-label="유형">인벤토리</td></tr>' +
                    '</tbody></table>';
                    location.hash = '#/inventories';
                })()
            `);
            let invListSynced = false;
            for (let i = 0; i < 25; i++) {
                await sleep(150);
                const sTxt = await cdp.evaluate(`document.getElementById('awx-vld-awx-source-label')?.innerText || ''`);
                const pCount = await cdp.evaluate(`window.awxValidatorState?.awxBridge?.pageCount ?? -1`);
                if (sTxt.includes('인벤토리') && pCount === 0 && await cdp.evaluate(`window.awxValidatorState?.awxBridge?.scopeType`) === 'inventories_list') {
                    invListSynced = true;
                    break;
                }
            }
            if (!invListSynced) {
                const dbg = await cdp.evaluate(`JSON.stringify(window.awxValidatorState?.awxBridge)`);
                throw new Error(`Bridged tab on #/inventories did not report inventories_list with 0 scraped hosts: ${dbg}`);
            }

            // Clear userCustomized flag to prove unchosen inventories_list rejects /api/v2/hosts/ dump cleanly
            await cdp.evaluate(`window.awxValidatorState.awxConfig.userCustomized = false;`);
            const leakGuardCnt = await cdp.evaluate(`window.awxRunCollectAll()`);
            const errBoxTxt = await cdp.evaluate(`document.getElementById('awx-vld-awx-summary')?.innerText || ''`);
            if (leakGuardCnt !== 0 || !errBoxTxt.includes('인벤토리 목록')) {
                throw new Error(`Expected #/inventories without selection to block full dump, got cnt=${leakGuardCnt}, summary=${errBoxTxt}`);
            }

            // Open Settings, verify Demo Inventory appears in <select> dropdown, pick inventory:77, close & collect ONLY Demo Inventory hosts!
            await cdp.send('Page.bringToFront');
            await cdp.evaluate(`window.focus(); document.documentElement.dispatchEvent(new MouseEvent('mouseenter'));`);
            await sleep(500);
            await cdp.evaluate(`document.getElementById('awx-vld-btn-open-awx-settings').click()`);
            await sleep(250);
            const dropOpts = await cdp.evaluate(`Array.from(document.querySelectorAll('#awx-vld-awx-inventory-select option')).map(o => o.value + '|' + o.textContent)`);
            if (!dropOpts.some(t => t.includes('inventory:77|Demo Inventory'))) {
                throw new Error(`Expected Demo Inventory (#77) in settings inventory dropdown, got: ${JSON.stringify(dropOpts)}`);
            }
            await cdp.evaluate(`
                (() => {
                    const sel = document.getElementById('awx-vld-awx-inventory-select');
                    sel.value = 'inventory:77';
                    sel.dispatchEvent(new Event('change'));
                })()
            `);
            await sleep(150);
            await assertNoLayoutClipping('Settings Modal Inventory Dropdown Selected');
            await cdp.evaluate(`document.getElementById('awx-vld-btn-close-settings').click()`);
            await sleep(150);

            const demoCnt = await cdp.evaluate(`window.awxRunCollectAll()`);
            const demoHosts = await cdp.evaluate(`Array.from(window.awxValidatorState.collectedHosts).sort()`);
            if (demoCnt !== 2 || demoHosts[0] !== 'demo-only-01' || demoHosts[1] !== 'demo-only-02') {
                throw new Error(`Expected ONLY Demo Inventory hosts ['demo-only-01', 'demo-only-02'], got cnt=${demoCnt} hosts=${JSON.stringify(demoHosts)}`);
            }
            console.log("   Verified #/inventories screenshot scenario: 0 false scrapes, blocks global dump, and dropdown-selected Demo Inventory collects ONLY Demo Inventory hosts (2).");

            // D) Navigate bridged AWX tab to #/hosts (Global All Hosts DOM) while Demo Inventory (#77) stays pinned from dropdown:
            //    BOTH [현재 화면 수집] (first page of #77) and [현재 인벤토리 전체 수집] MUST stay strictly on #77!
            await awxTabCdp.evaluate(`
                (() => {
                    document.body.innerHTML = '<table><tbody>' +
                        '<tr><td><input type="checkbox"/></td><td data-label="Name"><a href="#/hosts/901/details">all-dom-leak-01</a></td></tr>' +
                        '<tr><td><input type="checkbox"/></td><td data-label="Name"><a href="#/hosts/902/details">all-dom-leak-02</a></td></tr>' +
                    '</tbody></table>';
                    location.hash = '#/hosts';
                })()
            `);
            let allTabSynced = false;
            for (let i = 0; i < 25; i++) {
                await sleep(150);
                if (await cdp.evaluate(`window.awxValidatorState?.awxBridge?.scopeType === 'all' && window.awxValidatorState?.awxBridge?.bridgeVersion === 2`)) {
                    allTabSynced = true;
                    break;
                }
            }
            if (!allTabSynced) {
                throw new Error(`Bridged tab did not sync #/hosts state: ${await cdp.evaluate('JSON.stringify(window.awxValidatorState?.awxBridge)')}`);
            }
            const srcLabelOnAll = await cdp.evaluate(`document.getElementById('awx-vld-awx-source-label')?.innerText || ''`);
            if (!srcLabelOnAll.includes('선택 대상') || !srcLabelOnAll.includes('77')) {
                throw new Error(`Expected pinned #77 source label when tab sits on #/hosts, got: ${srcLabelOnAll}`);
            }

            // [현재 화면 수집] must collect page 1 of selected Demo Inventory (#77), NOT #/hosts DOM!
            await cdp.evaluate(`document.getElementById('awx-vld-btn-page').click()`);
            await sleep(500);
            const caseDPageHosts = await cdp.evaluate(`Array.from(window.awxValidatorState.collectedHosts).sort()`);
            if (caseDPageHosts.length !== 2 || caseDPageHosts[0] !== 'demo-only-01' || caseDPageHosts[1] !== 'demo-only-02') {
                throw new Error(`Case D [현재 화면 수집] leaked #/hosts instead of Demo Inventory page 1: ${JSON.stringify(caseDPageHosts)}`);
            }

            // [현재 인벤토리 전체 수집] must collect ONLY Demo Inventory (#77), NOT /api/v2/hosts/!
            const caseDAllCnt = await cdp.evaluate(`window.awxRunCollectAll()`);
            const caseDAllHosts = await cdp.evaluate(`Array.from(window.awxValidatorState.collectedHosts).sort()`);
            if (caseDAllCnt !== 2 || caseDAllHosts[0] !== 'demo-only-01' || caseDAllHosts[1] !== 'demo-only-02') {
                throw new Error(`Case D [현재 인벤토리 전체 수집] leaked /api/v2/hosts/: cnt=${caseDAllCnt}, hosts=${JSON.stringify(caseDAllHosts)}`);
            }

            // Also navigate bridged tab to a DIFFERENT specific inventory (#15 page 2), unpin naturally,
            // then re-pick Demo Inventory (#77) in dropdown and verify BOTH [현재 화면 수집] & [전체 수집] obey #77 page 1!
            await awxTabCdp.evaluate(`
                (() => {
                    document.body.innerHTML = '<table><tbody>' +
                        '<tr><td><input type="checkbox"/></td><td data-label="Name"><a href="#/hosts/501/details">inv15-dom-leak</a></td></tr>' +
                    '</tbody></table>';
                    location.hash = '#/inventories/inventory/15/hosts?page=2';
                })()
            `);
            let inv15Unpinned = false;
            for (let i = 0; i < 25; i++) {
                await sleep(150);
                const lbl = await cdp.evaluate(`document.getElementById('awx-vld-awx-source-label')?.innerText || ''`);
                if (lbl.includes('인벤토리 #15') && !(await cdp.evaluate(`window.awxValidatorState?.awxConfig?.scopePinned`))) {
                    inv15Unpinned = true;
                    break;
                }
            }
            if (!inv15Unpinned) {
                throw new Error(`Navigating bridged tab to #15 did not unpin previous dropdown override: ${await cdp.evaluate('document.getElementById("awx-vld-awx-source-label")?.innerText')}`);
            }

            await cdp.evaluate(`document.getElementById('awx-vld-btn-open-awx-settings').click()`);
            await sleep(200);
            await cdp.evaluate(`
                (() => {
                    const sel = document.getElementById('awx-vld-awx-inventory-select');
                    sel.value = 'inventory:77';
                    sel.dispatchEvent(new Event('change'));
                })()
            `);
            await sleep(200);
            await cdp.evaluate(`document.getElementById('awx-vld-btn-close-settings').click()`);
            await sleep(150);

            await cdp.evaluate(`document.getElementById('awx-vld-btn-page').click()`);
            await sleep(500);
            const overridePageHosts = await cdp.evaluate(`Array.from(window.awxValidatorState.collectedHosts).sort()`);
            if (overridePageHosts.length !== 2 || overridePageHosts[0] !== 'demo-only-01' || overridePageHosts[1] !== 'demo-only-02') {
                throw new Error(`Expected dropdown #77 to override #15 page 2 on [현재 화면 수집], got: ${JSON.stringify(overridePageHosts)}`);
            }
            const overrideAllCnt = await cdp.evaluate(`window.awxRunCollectAll()`);
            if (overrideAllCnt !== 2) {
                throw new Error(`Expected dropdown #77 to override #15 on [전체 수집], got cnt=${overrideAllCnt}`);
            }
            console.log("   Verified Case D: #/hosts & wrong-inventory (#15 p.2) tabs stay strictly isolated to dropdown-selected #77 for both [현재 화면 수집] (p.1) & [전체 수집].");
        } finally {
            mockAwx.setForceDirect401(false);
            if (awxTabCdp) awxTabCdp.close();
            if (awxTabTargetId) {
                await cdp.send('Target.closeTarget', { targetId: awxTabTargetId }).catch(() => {});
            }
            await sleep(300);
            // Persist inventory:77 (page 1) DPAPI config & disable preferBridge so [8/8] restart verifies passwordless Direct API collection
            await cdp.evaluate(`
                (async () => {
                    const cb = document.getElementById('awx-vld-awx-prefer-bridge');
                    if (cb) { cb.checked = false; cb.dispatchEvent(new Event('change')); }
                    const u = document.getElementById('awx-vld-awx-url');
                    const usr = document.getElementById('awx-vld-awx-user');
                    const pwd = document.getElementById('awx-vld-awx-pass');
                    const tok = document.getElementById('awx-vld-awx-token');
                    if (u) {
                        u.value = ${JSON.stringify(mockAwxBase + '/#/inventories/inventory/77/hosts?page=1')};
                        u.dispatchEvent(new Event('input'));
                    }
                    if (usr) usr.value = 'uiuser';
                    if (pwd) pwd.value = 'uipass123';
                    if (tok) tok.value = '';
                    const saveBtn = document.getElementById('awx-vld-btn-awx-save');
                    if (saveBtn) saveBtn.click();
                })()
            `).catch(() => {});
            for (let i = 0; i < 20; i++) {
                await sleep(150);
                const msg = await cdp.evaluate(`document.getElementById('awx-vld-awx-test-result')?.innerText || ''`).catch(() => '');
                if (msg.includes('연결 성공')) break;
            }
            await cdp.evaluate(`document.getElementById('awx-vld-btn-close-settings')?.click()`).catch(() => {});
            await sleep(200);
        }

        // [5/7] Test Comparison in Tab 1
        console.log("[5/7] Testing Tab 1: Local cross-checking comparison...");
        const sampleMatchHosts = "prd-web-0001\nprd-web-0002\nprd-web-0003";
        await cdp.evaluate(`
            document.getElementById('awx-vld-btn-reset-awx').click();
            document.getElementById('awx-vld-txt-detail').value = ${JSON.stringify(sampleMatchHosts)};
            document.getElementById('awx-vld-txt-incident').value = ${JSON.stringify(sampleMatchHosts)};
            document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
            window.awxDoCompare();
        `);
        const cmpResult = await cdp.evaluate(`document.getElementById('awx-vld-result-box').innerText`);
        if (!cmpResult.includes('[MATCH]')) {
            throw new Error("Local comparison did not yield [MATCH]: " + cmpResult);
        }
        await assertNoLayoutClipping('Tab 1 Match Result');

        // Also test MISMATCH report box + Reset button
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-incident').value = "prd-web-0001\\nprd-web-9999";
            document.getElementById('awx-vld-txt-incident').dispatchEvent(new Event('input'));
            window.awxDoCompare();
        `);
        const mismatchResult = await cdp.evaluate(`document.getElementById('awx-vld-result-box').innerText`);
        if (!mismatchResult.includes('[MISMATCH]')) {
            throw new Error("Local comparison did not yield [MISMATCH]: " + mismatchResult);
        }
        await assertNoLayoutClipping('Tab 1 Mismatch Result');
        await cdp.evaluate(`document.getElementById('awx-vld-btn-reset-awx').click()`);
        const afterResetLen = await cdp.evaluate(`document.getElementById('awx-vld-txt-detail').value.length`);
        if (afterResetLen !== 0) {
            throw new Error("Reset button did not clear txt-detail");
        }
        console.log("✓ Tab 1 comparison ([MATCH], [MISMATCH], Reset) & layout verified.");

        // [6/7] Test Tab 2 & Tab 3: Self-test hidden from dropdowns, executed via Settings gear
        console.log("[6/7] Testing Tab 2 & Tab 3: Self-test via Settings & Checker approval cycle...");
        await cdp.evaluate(`window.awxSwitchTab('worker')`);
        await sleep(600);

        const selOptions = await cdp.evaluate(`
            ({
                checker: Array.from(document.querySelectorAll('#awx-vld-sel-checker option')).map(o => ({ value: o.value, text: o.innerText.trim() })),
                reassign: Array.from(document.querySelectorAll('#awx-vld-sel-reassign option')).map(o => ({ value: o.value, text: o.innerText.trim() }))
            })
        `);
        console.log("   Worker peer checker options loaded:", selOptions.checker.length);
        const selfInDropdown = selOptions.checker.find(o => o.value === '192.168.0.23' || o.text.includes('[테스트]') || o.text.includes('나 자신')) ||
            selOptions.reassign.find(o => o.value === '192.168.0.23' || o.text.includes('[테스트]') || o.text.includes('나 자신'));
        if (selfInDropdown) {
            throw new Error(`Self-test option unexpectedly visible in dropdown: ${JSON.stringify(selfInDropdown)}`);
        }

        // Verify clicking Settings -> [🧪 셀프 테스트] with empty inputs auto-fills sample 5 hosts
        await cdp.evaluate(`
            document.getElementById('awx-vld-btn-header-settings').click();
        `);
        await sleep(200);
        await cdp.evaluate(`document.getElementById('awx-vld-btn-test-self').click()`);
        await sleep(800);
        const sampleSelfSession = await cdp.evaluate(`
            ({
                sid: window.awxValidatorState.currentSession?.sessionId,
                status: window.awxValidatorState.currentSession?.status,
                detailCnt: document.getElementById('awx-vld-w-cnt-detail')?.innerText
            })
        `);
        if (!sampleSelfSession.sid || sampleSelfSession.status !== 'PENDING' || sampleSelfSession.detailCnt !== '5') {
            throw new Error(`Sample self-test failed with empty inputs: ${JSON.stringify(sampleSelfSession)}`);
        }
        console.log("   Verified: Empty input + Settings [셀프 테스트] auto-generated 5 sample hosts:", sampleSelfSession.sid);

        // Now set specific 5 hosts and execute Settings [셀프 테스트] using entered data
        const worker5Hosts = "prd-web-0001\nprd-web-0002\nprd-web-0003\nprd-web-0004\nprd-web-0005";
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-detail').value = ${JSON.stringify(worker5Hosts)};
            document.getElementById('awx-vld-txt-incident').value = ${JSON.stringify(worker5Hosts)};
            document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
            document.getElementById('awx-vld-btn-header-settings').click();
        `);
        await sleep(200);
        await cdp.evaluate(`document.getElementById('awx-vld-btn-test-self').click()`);
        await sleep(1000);

        const session = await cdp.evaluate(`window.awxValidatorState.currentSession`);
        if (!session || session.status !== 'PENDING' || session.sessionId === sampleSelfSession.sid) {
            throw new Error("Failed to create self-test session from Settings modal with entered hosts");
        }
        console.log("   Session created via Settings [셀프 테스트]:", session.sessionId);

        // Switch to checker tab
        await cdp.evaluate(`window.awxSwitchTab('checker')`);
        await sleep(600);

        await cdp.evaluate(`
            const sel = document.getElementById('awx-vld-sel-pending-sessions');
            sel.value = "${session.sessionId}";
            sel.dispatchEvent(new Event('change'));
        `);
        await sleep(600);

        // Check blind masking (5 hosts -> top 3 shown, 2 blinded)
        const blindText = await cdp.evaluate(`document.getElementById('awx-vld-chk-blind-detail')?.innerText || ''`);
        if (!blindText.includes('블라인드')) {
            throw new Error("Blind masking missing in checker view: " + blindText);
        }
        console.log("   Blind masking verified:", blindText.replace(/\n/g, ' '));

        // Checker fills in hosts and approves
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-chk-detail').value = ${JSON.stringify(worker5Hosts)};
            document.getElementById('awx-vld-txt-chk-incident').value = ${JSON.stringify(worker5Hosts)};
            document.getElementById('awx-vld-txt-chk-detail').dispatchEvent(new Event('input'));
            document.getElementById('awx-vld-txt-chk-incident').dispatchEvent(new Event('input'));
            window.awxDoCheckerCompare();
        `);
        await sleep(400);

        const chkCmp = await cdp.evaluate(`document.getElementById('awx-vld-chk-result-box')?.innerText || ''`);
        if (!chkCmp.includes('[MATCH]')) {
            throw new Error("Checker compare did not produce [MATCH]: " + chkCmp);
        }

        await assertNoLayoutClipping('Tab 3 Checker Matched');

        await cdp.evaluate(`window.awxSubmitDecision('APPROVED', '단독 실행 앱 E2E 정상 승인')`);
        await sleep(1000);

        // Switch back to worker tab to verify approval
        await cdp.evaluate(`window.awxSwitchTab('worker')`);
        await sleep(500);
        await assertNoLayoutClipping('Tab 2 Worker Approved');
        const finalStatus = await cdp.evaluate(`document.getElementById('awx-vld-w-session-status')?.innerText`);
        if (finalStatus !== 'APPROVED') {
            throw new Error(`Worker status was not APPROVED, got: ${finalStatus}`);
        }
        console.log("✓ Worker & Checker workflow in standalone app verified: APPROVED.");

        // Verify bottom 6 o'clock global status bar
        console.log("   Verifying 6 o'clock global status bar across all tabs...");
        const statusBarInfo = await cdp.evaluate(`
            (() => {
                const bar = document.getElementById('awx-vld-global-status-bar');
                const text = document.getElementById('awx-vld-global-status-text')?.innerText || '';
                return {
                    visible: bar && bar.style.display !== 'none',
                    text
                };
            })()
        `);
        console.log("   Global status bar state:", statusBarInfo);
        if (!statusBarInfo.visible || !statusBarInfo.text.includes('크로스체크 승인 완료')) {
            throw new Error("Global status bar did not display approved status: " + JSON.stringify(statusBarInfo));
        }

        // Switch to Tab 1 and verify status bar is STILL visible at 6 o'clock
        await cdp.evaluate(`document.getElementById('awx-vld-tab-single').click()`);
        await sleep(300);
        const statusBarOnTab1 = await cdp.evaluate(`
            (() => {
                const bar = document.getElementById('awx-vld-global-status-bar');
                const pane1 = document.getElementById('awx-vld-pane-single');
                return {
                    pane1Visible: pane1 && pane1.style.display !== 'none',
                    barVisible: bar && bar.style.display !== 'none'
                };
            })()
        `);
        if (!statusBarOnTab1.pane1Visible || !statusBarOnTab1.barVisible) {
            throw new Error("Global status bar should remain visible on Tab 1: " + JSON.stringify(statusBarOnTab1));
        }

        // Click status bar and verify it returns to Tab 2
        await cdp.evaluate(`document.getElementById('awx-vld-global-status-bar').click()`);
        await sleep(300);
        const pane2Visible = await cdp.evaluate(`
            (() => {
                const pane2 = document.getElementById('awx-vld-pane-worker');
                return pane2 && pane2.style.display !== 'none';
            })()
        `);
        if (!pane2Visible) {
            throw new Error("Clicking global status bar did not navigate to Tab 2");
        }
        console.log("✓ 6 o'clock global status bar verified: visible across all tabs and clicks navigate to Tab 2.");

        // [7/7] Test Window Close & Process Termination Lifecycle
        console.log("[7/7] Testing Window Close & Auto-Exit Lifecycle...");
        // Simulate window close: close page first so in-page ping/WS intervals stop, then send /api/bye if unload didn't already
        await cdp.send('Page.close');
        cdp.close();
        await sleep(100);
        try {
            await fetch('http://127.0.0.1:18080/api/bye', { method: 'POST', body: 'bye' });
        } catch(e) {}

        console.log("   Waiting for host-cross-checker.exe to terminate automatically...");
        for (let i = 0; i < 30; i++) {
            await sleep(200);
            if (procExited) break;
        }

        if (!procExited) {
            // Check if process has stopped responding on port 18080
            let portAlive = false;
            try {
                const res = await fetch('http://127.0.0.1:18080/api/health');
                portAlive = res.ok;
            } catch(e) {}

            if (portAlive) {
                appProc.kill();
                throw new Error("host-cross-checker.exe did not terminate within 6 seconds after window closed!");
            }
        }
        console.log("✓ Process auto-exit verified: host-cross-checker.exe terminated cleanly.");

        // [8/8] Restart Acceptance Test: Relaunch WITHOUT AWX_* env vars and collect via BOTH buttons using DPAPI config.json
        console.log("[8/8] Testing exe restart without AWX env vars (DPAPI persistence + both collect buttons)...");
        const restartEnv = Object.assign({}, process.env, {
            CROSS_CHECKER_DEBUG_PORT: String(DEBUG_PORT),
            CROSS_CHECKER_PROFILE_DIR: USER_DATA_DIR
        });
        delete restartEnv.AWX_BASE_URL;
        delete restartEnv.AWX_USERNAME;
        delete restartEnv.AWX_PASSWORD;
        delete restartEnv.AWX_TOKEN;
        delete restartEnv.AWX_DEFAULT_SCOPE;

        let restartExited = false;
        const restartProc = spawn(exePath, [
            '-port=18080',
            `-share=${testShareDir}`,
            '-bind-ip=192.168.0.23',
            '-open-app'
        ], { env: restartEnv, stdio: 'pipe' });
        restartProc.on('exit', () => { restartExited = true; });
        restartProc.stdout.on('data', d => console.log('   [RESTART STDOUT]', d.toString().trim()));
        restartProc.stderr.on('data', d => console.log('   [RESTART STDERR]', d.toString().trim()));

        let restartPageTarget = null;
        for (let i = 0; i < 35; i++) {
            await sleep(300);
            try {
                const res = await fetch(`http://127.0.0.1:${DEBUG_PORT}/json`);
                if (res.ok) {
                    const targets = await res.json();
                    restartPageTarget = targets.find(t => t.type === 'page' && (t.url.includes('127.0.0.1:18080') || t.url.includes('localhost:18080')));
                    if (restartPageTarget && restartPageTarget.webSocketDebuggerUrl) break;
                }
            } catch (e) {}
        }
        if (!restartPageTarget || !restartPageTarget.webSocketDebuggerUrl) {
            if (!restartExited) try { restartProc.kill(); } catch (e) {}
            throw new Error("Restarted standalone app failed to open CDP window");
        }
        const cdp2 = new CDPClient(restartPageTarget.webSocketDebuggerUrl);
        await cdp2.connect();
        await cdp2.send('Runtime.enable');
        await cdp2.send('Page.enable');
        for (let i = 0; i < 25; i++) {
            await sleep(200);
            const hasBtn = await cdp2.evaluate(`!!document.getElementById('awx-vld-btn-page')`).catch(() => false);
            if (hasBtn) break;
        }
        try {
            // Collect current page & full inventory WITHOUT touching Settings or entering password
            await cdp2.evaluate(`document.getElementById('awx-vld-btn-page').click()`);
            await sleep(500);
            const restartPageHosts = await cdp2.evaluate(`Array.from(window.awxValidatorState.collectedHosts).sort()`);
            if (restartPageHosts.length !== 2 || restartPageHosts[0] !== 'demo-only-01' || restartPageHosts[1] !== 'demo-only-02') {
                throw new Error(`Restarted [현재 화면 수집] failed without password re-entry: ${JSON.stringify(restartPageHosts)}`);
            }
            const restartAllCnt = await cdp2.evaluate(`window.awxRunCollectAll()`);
            const restartSummary = await cdp2.evaluate(`document.getElementById('awx-vld-awx-summary')?.innerText || ''`);
            if (restartAllCnt !== 2 || !restartSummary.includes('직접 API')) {
                throw new Error(`Restarted [현재 인벤토리 전체 수집] failed: cnt=${restartAllCnt}, summary=${restartSummary}`);
            }

            // Verify Reset clears config.json DPAPI secrets
            await cdp2.evaluate(`document.getElementById('awx-vld-btn-open-awx-settings').click()`);
            await sleep(200);
            await cdp2.evaluate(`document.getElementById('awx-vld-btn-awx-reset').click()`);
            await sleep(400);
            if (fs.existsSync(distConfigPath)) {
                const afterResetCfg = fs.readFileSync(distConfigPath, 'utf8');
                if (afterResetCfg.includes('awxPasswordEnc') || afterResetCfg.includes('awxTokenEnc')) {
                    throw new Error(`Reset did not clear encrypted AWX credentials from config.json: ${afterResetCfg}`);
                }
            }
            console.log("✓ P2-1 Restart Acceptance Verified: both collect buttons succeeded after exe restart without re-entering credentials, and Reset cleared config.json.");

            await cdp2.evaluate(`try { if (navigator.sendBeacon) navigator.sendBeacon('/api/bye', 'bye'); } catch(e) {}`);
            await cdp2.send('Page.close');
        } finally {
            cdp2.close();
            for (let i = 0; i < 20; i++) {
                await sleep(150);
                if (restartExited) break;
            }
            if (!restartExited) {
                try { restartProc.kill(); } catch (e) {}
            }
        }

        console.log("\n===============================================================================");
        console.log("  >>> SUCCESS: Standalone Desktop App (host-cross-checker.exe) 100% PASS <<<");
        console.log("===============================================================================");
        process.exitCode = 0;

    } catch(err) {
        console.error("\n>>> TEST FAILED:", err);
        cdp.close();
        if (!procExited) {
            try { appProc.kill(); } catch(e) {}
        }
        process.exitCode = 1;
    } finally {
        try { mockAwx.server.close(); } catch (e) {}
        if (hadDistConfig) {
            fs.writeFileSync(distConfigPath, prevDistConfig, 'utf8');
        } else {
            fs.rmSync(distConfigPath, { force: true });
        }
    }
}

runStandaloneAppTest().then(() => process.exit(process.exitCode || 0));
