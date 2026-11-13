const http = require('http');
const fs = require('fs');
const path = require('path');
const { spawn, execSync } = require('child_process');

const CHROME_PATH = "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe";
const USER_DATA_DIR = "C:\\Users\\qazx2\\.gemini\\antigravity\\brain\\7da7b144-0835-4ceb-8d8a-7a71e635f416\\scratch\\chrome_awx_profile_noext";
const DEBUG_PORT = 9225;

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
            } else if (msg.method === 'Page.javascriptDialogOpening') {
                this.send('Page.handleJavaScriptDialog', { accept: true }).catch(() => {});
            } else if (msg.method === 'Runtime.consoleAPICalled') {
                const text = (msg.params.args || []).map(a => a.value !== undefined ? a.value : a.description).join(' ');
                console.log('    [BROWSER CONSOLE]', text);
            } else if (msg.method === 'Runtime.exceptionThrown') {
                console.error('    [BROWSER EXCEPTION]', msg.params.exceptionDetails?.text, msg.params.exceptionDetails?.exception?.description);
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

async function runSelfRequestTest() {
    console.log("=== [TEST] 나 자신에게 요청하는 1인 크로스체크 플로우 검증 ===");

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
            { name: "박민수", ip: "192.168.0.105", role: "확인자" }
        ]
    };
    fs.writeFileSync(path.join(testShareDir, 'users.json'), JSON.stringify(initialUsers, null, 2), 'utf8');

    const agentBin = path.join(__dirname, '..', 'dist', 'cross-checker-agent.exe');
    const agentProcess = spawn(agentBin, [
        '-port=18080',
        `-share=${testShareDir}`,
        '-bind-ip=192.168.0.23'
    ], { stdio: 'ignore' });

    let agentReady = false;
    for (let i = 0; i < 20; i++) {
        try {
            const res = await fetch('http://127.0.0.1:18080/api/me');
            if (res.ok) {
                agentReady = true;
                break;
            }
        } catch(e) {}
        await sleep(200);
    }
    if (!agentReady) throw new Error("Agent failed to start on 18080");
    console.log("✓ Agent ready on port 18080");

    const chrome = spawn(CHROME_PATH, [
        `--remote-debugging-port=${DEBUG_PORT}`,
        `--user-data-dir=${USER_DATA_DIR}`,
        'http://192.168.0.58:30080/#/inventories/inventory/3/hosts'
    ], { stdio: 'ignore' });

    await sleep(4000);

    const resJson = await fetch(`http://127.0.0.1:${DEBUG_PORT}/json`);
    const targets = await resJson.json();
    let pageTarget = targets.find(t => t.type === 'page' && !t.url.startsWith('chrome-extension') && !t.url.startsWith('chrome://'));
    if (!pageTarget) pageTarget = targets.find(t => t.type === 'page');

    const cdp = new CDPClient(pageTarget.webSocketDebuggerUrl);
    await cdp.connect();
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');

    try {
        let currentUrl = await cdp.evaluate('window.location.href');
        if (currentUrl.includes('/#/login')) {
            console.log("Logging into AWX...");
            await cdp.evaluate(`document.getElementById('pf-login-username-id').focus();`);
            await cdp.send('Input.insertText', { text: 'test.kim' });
            await cdp.evaluate(`document.getElementById('pf-login-password-id').focus();`);
            await cdp.send('Input.insertText', { text: '11111111!' });
            await cdp.evaluate(`document.querySelector('button[type="submit"]').click();`);
            for (let i = 0; i < 15; i++) {
                await sleep(1000);
                const hash = await cdp.evaluate(`window.location.hash`);
                if (hash && !hash.includes('/login')) break;
            }
        }

        await cdp.send('Page.navigate', { url: 'http://192.168.0.58:30080/#/inventories/inventory/3/hosts' });
        for (let i = 0; i < 15; i++) {
            await sleep(1000);
            const rows = await cdp.evaluate(`document.querySelectorAll('table tbody tr').length`);
            if (rows > 0) break;
        }

        const bmkCode = fs.readFileSync(path.join(__dirname, '..', 'dist', 'bookmarklet.txt'), 'utf8');
        await cdp.send('Page.navigate', { url: bmkCode });
        await sleep(1500);

        await cdp.evaluate(`
            window.alert = function(msg) {
                console.log('[BROWSER ALERT]', msg);
                window.__lastAlertMsg = msg;
            };
        `);

        // 1. 작업자 탭으로 전환
        await cdp.evaluate(`window.awxSwitchTab('worker')`);
        await sleep(1000);

        // 2. 드롭다운 목록에서 나 자신([테스트]) 옵션 완전히 숨겨졌는지 검증
        const dropdownOptions = await cdp.evaluate(`
            ({
                checker: Array.from(document.querySelectorAll('#awx-vld-sel-checker option')).map(o => ({
                    value: o.value,
                    text: o.innerText.trim()
                })),
                reassign: Array.from(document.querySelectorAll('#awx-vld-sel-reassign option')).map(o => ({
                    value: o.value,
                    text: o.innerText.trim()
                }))
            })
        `);
        console.log("✓ Dropdown options loaded:", dropdownOptions);

        const forbiddenSelfChecker = dropdownOptions.checker.find(o => o.value === '192.168.0.23' || o.text.includes('[테스트]') || o.text.includes('나 자신'));
        const forbiddenSelfReassign = dropdownOptions.reassign.find(o => o.value === '192.168.0.23' || o.text.includes('[테스트]') || o.text.includes('나 자신'));
        if (forbiddenSelfChecker || forbiddenSelfReassign) {
            throw new Error(`Self-option must NOT appear in dropdown lists: ${JSON.stringify({ forbiddenSelfChecker, forbiddenSelfReassign })}`);
        }
        console.log("✓ Verified: Self-test option is hidden from checker and reassign dropdowns.");

        // 3A. 설정(톱니바퀴)에서 빈 입력 상태로 [셀프 테스트] 클릭 시 샘플 5대 자동 생성 확인
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-detail').value = "";
            document.getElementById('awx-vld-txt-incident').value = "";
            document.getElementById('awx-vld-txt-target').value = "";
            document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
            document.getElementById('awx-vld-btn-header-settings').click();
        `);
        await sleep(300);

        const hasSelfTestBtn = await cdp.evaluate(`
            (() => {
                const btn = document.getElementById('awx-vld-btn-test-self');
                const panel = document.getElementById('awx-vld-settings-panel');
                return !!(btn && panel && window.getComputedStyle(panel).display !== 'none');
            })()
        `);
        if (!hasSelfTestBtn) {
            throw new Error("Settings self-test button not visible inside settings modal");
        }

        console.log("Clicking [🧪 셀프 테스트] with empty inputs (expecting sample 5 hosts)...");
        await cdp.evaluate(`document.getElementById('awx-vld-btn-test-self').click()`);
        await sleep(1200);

        const sampleCheck = await cdp.evaluate(`
            (() => {
                const panel = document.getElementById('awx-vld-settings-panel');
                const detailVal = document.getElementById('awx-vld-txt-detail').value;
                const sess = window.awxValidatorState.currentSession;
                return {
                    settingsClosed: !panel || window.getComputedStyle(panel).display === 'none',
                    activeTab: window.awxValidatorState.activeTab,
                    detailLines: detailVal.split('\\n').filter(Boolean).length,
                    sessionId: sess?.sessionId,
                    status: sess?.status,
                    checkerIp: sess?.checkerIp
                };
            })()
        `);
        if (!sampleCheck.settingsClosed || sampleCheck.activeTab !== 'worker' || sampleCheck.detailLines !== 5 || sampleCheck.status !== 'PENDING' || sampleCheck.checkerIp !== '192.168.0.23') {
            throw new Error(`Sample self-test from settings failed: ${JSON.stringify(sampleCheck)}`);
        }
        console.log("✓ Sample self-test (5 hosts auto-fill) created via Settings:", sampleCheck.sessionId);

        // 3B. 입력란에 호스트가 있는 상태에서 설정(톱니바퀴) [셀프 테스트] 실행 시 입력 데이터 그대로 사용 확인
        const testHosts = "custom-db-01\\ncustom-db-02\\ncustom-db-03\\ncustom-db-04\\ncustom-db-05";
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-detail').value = "${testHosts}";
            document.getElementById('awx-vld-txt-incident').value = "${testHosts}";
            document.getElementById('awx-vld-txt-target').value = "";
            document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
            document.getElementById('awx-vld-btn-header-settings').click();
        `);
        await sleep(250);
        await cdp.evaluate(`document.getElementById('awx-vld-btn-test-self').click()`);
        await sleep(1200);
        const sessionInfo = await cdp.evaluate(`window.awxValidatorState.currentSession`);
        console.log("✓ Session created via Settings self-test button:", sessionInfo);

        if (!sessionInfo || sessionInfo.status !== 'PENDING') {
            throw new Error(`Failed to create session: ${JSON.stringify(sessionInfo)}`);
        }

        // 4. 세션 카드에 1인 테스트 모드 안내 표시 확인
        const noticeText = await cdp.evaluate(`document.getElementById('awx-vld-w-result-report')?.innerText || ''`);
        console.log("✓ Worker card notice:\n" + noticeText.trim());
        if (!noticeText.includes('1인 테스트 모드')) {
            throw new Error("Self test guidance notice not displayed in worker card");
        }

        // 5. 확인자 탭으로 전환
        console.log("Switching to checker tab...");
        await cdp.evaluate(`window.awxSwitchTab('checker')`);
        await sleep(1000);

        // 생성된 세션 선택
        await cdp.evaluate(`
            const sel = document.getElementById('awx-vld-sel-pending-sessions');
            sel.value = "${sessionInfo.sessionId}";
            sel.dispatchEvent(new Event('change'));
        `);
        await sleep(1000);

        // 6. 확인자 탭에 대기 중인 세션 로드 및 블라인드 확인
        const checkerStatus = await cdp.evaluate(`
            (() => {
                const badge = document.getElementById('awx-vld-badge-pending-count')?.innerText;
                const blindDetail = document.getElementById('awx-vld-chk-blind-detail')?.innerText;
                const workerName = document.getElementById('awx-vld-chk-worker-name')?.innerText;
                return { badge, blindDetail, workerName };
            })()
        `);
        console.log("✓ Checker tab loaded:", checkerStatus);
        if (!checkerStatus.blindDetail.includes('블라인드')) {
            throw new Error(`Blinding failed in checker view: ${JSON.stringify(checkerStatus)}`);
        }

        // 7. 확인자가 호스트 입력 및 교차 비교 실행
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-chk-detail').value = "${testHosts}";
            document.getElementById('awx-vld-txt-chk-incident').value = "${testHosts}";
            document.getElementById('awx-vld-txt-chk-detail').dispatchEvent(new Event('input'));
            document.getElementById('awx-vld-txt-chk-incident').dispatchEvent(new Event('input'));
        `);

        console.log("Executing checker comparison...");
        await cdp.evaluate(`window.awxDoCheckerCompare()`);
        await sleep(500);

        const cmpResultText = await cdp.evaluate(`document.getElementById('awx-vld-chk-result-box')?.innerText || ''`);
        console.log("✓ Checker compare result:\n" + cmpResultText.trim());
        if (!cmpResultText.includes('[MATCH]')) {
            throw new Error("Comparison did not produce [MATCH]");
        }

        // 8. 승인 클릭
        console.log("Submitting approval decision...");
        await cdp.evaluate(`window.awxSubmitDecision('APPROVED', '1인 셀프 테스트 정상 승인')`);
        await sleep(1000);

        // 9. 작업자 탭으로 복귀하여 실시간 승인 반영 확인
        console.log("Switching back to worker tab to check approval...");
        await cdp.evaluate(`window.awxSwitchTab('worker')`);
        await sleep(500);

        const workerFinal = await cdp.evaluate(`
            (() => {
                const status = document.getElementById('awx-vld-w-session-status')?.innerText;
                const report = document.getElementById('awx-vld-w-result-report')?.innerText;
                return { status, report };
            })()
        `);
        console.log("✓ Worker final status:", workerFinal);

        if (workerFinal.status !== 'APPROVED' || !workerFinal.report.includes('CROSS-CHECK APPROVED')) {
            throw new Error(`Worker tab did not receive approval: ${JSON.stringify(workerFinal)}`);
        }
        if (!workerFinal.report.includes(':set paste')) {
            throw new Error("Worker tab missing vim paste command in approved state");
        }

        console.log("\n========================================================");
        console.log("  >>> 1인 셀프 크로스체크 테스트 100% 성공 검증 완료! <<<");
        console.log("========================================================");

        cdp.close();
        chrome.kill();
        agentProcess.kill();
        process.exit(0);
    } catch(err) {
        console.error("TEST FAILED:", err);
        cdp.close();
        chrome.kill();
        agentProcess.kill();
        process.exit(1);
    }
}

runSelfRequestTest();
