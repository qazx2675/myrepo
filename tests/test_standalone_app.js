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

async function runStandaloneAppTest() {
    console.log("===============================================================================");
    console.log("  >>> Standalone Desktop App (host-cross-checker.exe) E2E Validation <<<");
    console.log("===============================================================================");

    // 1. Cleanup old processes
    try {
        execSync('taskkill /F /IM host-cross-checker.exe', { stdio: 'ignore' });
    } catch(e) {}
    try {
        execSync('taskkill /F /IM cross-checker-agent.exe', { stdio: 'ignore' });
    } catch(e) {}
    await sleep(500);

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
    const exePath = path.join(__dirname, '..', 'dist', 'host-cross-checker.exe');
    if (!fs.existsSync(exePath)) {
        throw new Error(`Executable not found at ${exePath}. Run build.py first!`);
    }

    console.log(`[1/7] Spawning ${path.basename(exePath)} with auto-launch app mode enabled...`);
    const appEnv = Object.assign({}, process.env, {
        CROSS_CHECKER_DEBUG_PORT: String(DEBUG_PORT),
        CROSS_CHECKER_PROFILE_DIR: USER_DATA_DIR
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

        if (collectedCount !== 994 || !summaryText.includes("1000대 중 6대 제외됨")) {
            throw new Error(`AWX collection in standalone app failed. Count=${collectedCount}, summary=${summaryText}`);
        }
        console.log("✓ One-click collection succeeded: 1,000 hosts from AWX inventory #3 parsed, 6 default excluded, 994 collected.");

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
        console.log("✓ Tab 1 comparison verified: [MATCH] confirmed.");

        // [6/7] Test Tab 2 & Tab 3: Self-request & Approval workflow
        console.log("[6/7] Testing Tab 2 & Tab 3: Worker request and Checker approval cycle...");
        const worker5Hosts = "prd-web-0001\nprd-web-0002\nprd-web-0003\nprd-web-0004\nprd-web-0005";
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-detail').value = ${JSON.stringify(worker5Hosts)};
            document.getElementById('awx-vld-txt-incident').value = ${JSON.stringify(worker5Hosts)};
            document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
        `);
        await cdp.evaluate(`window.awxSwitchTab('worker')`);
        await sleep(600);

        const selOptions = await cdp.evaluate(`
            Array.from(document.querySelectorAll('#awx-vld-sel-checker option')).map(o => ({ value: o.value, text: o.innerText.trim() }))
        `);
        console.log("   Worker checker options loaded:", selOptions.length);

        await cdp.evaluate(`
            document.getElementById('awx-vld-sel-checker').value = '192.168.0.23';
            document.getElementById('awx-vld-sel-checker').dispatchEvent(new Event('change'));
            document.getElementById('awx-vld-btn-send-request').click();
        `);
        await sleep(1000);

        const session = await cdp.evaluate(`window.awxValidatorState.currentSession`);
        if (!session || session.status !== 'PENDING') {
            throw new Error("Failed to create session from worker tab in standalone app");
        }
        console.log("   Session created:", session.sessionId);

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

        await cdp.evaluate(`window.awxSubmitDecision('APPROVED', '단독 실행 앱 E2E 정상 승인')`);
        await sleep(1000);

        // Switch back to worker tab to verify approval
        await cdp.evaluate(`window.awxSwitchTab('worker')`);
        await sleep(500);
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
        // Simulate window close by sending beacon and closing page
        await cdp.evaluate(`
            try {
                if (navigator.sendBeacon) navigator.sendBeacon('/api/bye', 'bye');
            } catch(e) {}
        `);
        await cdp.send('Page.close');
        cdp.close();

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

        console.log("\n===============================================================================");
        console.log("  >>> SUCCESS: Standalone Desktop App (host-cross-checker.exe) 100% PASS <<<");
        console.log("===============================================================================");
        process.exit(0);

    } catch(err) {
        console.error("\n>>> TEST FAILED:", err);
        cdp.close();
        if (!procExited) {
            try { appProc.kill(); } catch(e) {}
        }
        process.exit(1);
    }
}

runStandaloneAppTest();
