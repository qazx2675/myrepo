const http = require('http');
const fs = require('fs');
const path = require('path');
const { spawn, execSync } = require('child_process');
const crypto = require('crypto');

function execSshWithRetry(cmd, maxRetries = 3) {
    for (let i = 0; i < maxRetries; i++) {
        try {
            return execSync(`ssh -o BatchMode=yes -o ConnectTimeout=8 root@192.168.0.58 "${cmd}"`, { encoding: 'utf8' });
        } catch (err) {
            if (i === maxRetries - 1) throw err;
            console.log(`  [SSH Retry ${i + 1}/${maxRetries}] Waiting 1.5s...`);
            execSync("ping 127.0.0.1 -n 2 > nul");
        }
    }
}

const CHROME_PATH = "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe";
const USER_DATA_DIR = "C:\\Users\\qazx2\\.gemini\\antigravity\\brain\\7da7b144-0835-4ceb-8d8a-7a71e635f416\\scratch\\chrome_awx_profile_noext";
const DEBUG_PORT = 9224;

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

function fetchJson(url) {
    return new Promise((resolve, reject) => {
        http.get(url, (res) => {
            let data = '';
            res.on('data', chunk => data += chunk);
            res.on('end', () => {
                try { resolve(JSON.parse(data)); } catch(e) { reject(e); }
            });
        }).on('error', reject);
    });
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

    send(method, params = {}, timeoutMs = 15000) {
        return new Promise((resolve, reject) => {
            const id = this.msgId++;
            const timer = setTimeout(() => {
                if (this.callbacks.has(id)) {
                    this.callbacks.delete(id);
                    reject(new Error(`CDP command ${method} timed out after ${timeoutMs}ms`));
                }
            }, timeoutMs);

            this.callbacks.set(id, {
                resolve: (res) => { clearTimeout(timer); resolve(res); },
                reject: (err) => { clearTimeout(timer); reject(err); }
            });
            this.ws.send(JSON.stringify({ id, method, params }));
        });
    }

    async evaluate(expr) {
        const res = await this.send('Runtime.evaluate', {
            expression: expr,
            userGesture: true,
            awaitPromise: true,
            returnByValue: true
        });
        if (res.exceptionDetails) {
            throw new Error(JSON.stringify(res.exceptionDetails));
        }
        return res.result?.value;
    }

    async screenshot(filepath) {
        const res = await this.send('Page.captureScreenshot', { format: 'png' });
        fs.writeFileSync(filepath, Buffer.from(res.data, 'base64'));
        console.log(`  [Screenshot] ${filepath}`);
    }

    close() {
        if (this.ws) this.ws.close();
    }
}

async function runOneTakeTests() {
    console.log("==================================================================");
    console.log("  [ONE-TAKE TEST SUITE] 작업대상 호스트 교차 검증기 전체 신뢰성 검증");
    console.log("  (TC-01 ~ TC-12: 단독 모드, TC-13 ~ TC-24: 크로스체크 실시간 연동)");
    console.log("==================================================================");

    const testLogs = [];
    function recordLog(tcId, name, result, detail) {
        const line = `[${result}] ${tcId}: ${name} - ${detail}`;
        console.log(line);
        testLogs.push({ tcId, name, result, detail });
    }

    // 1. 테스트용 공유폴더 및 Go 에이전트 시작
    const testShareDir = path.join(__dirname, '..', 'test_share');
    fs.mkdirSync(testShareDir, { recursive: true });
    fs.mkdirSync(path.join(testShareDir, 'sessions'), { recursive: true });

    const initialUsers = {
        updatedAt: new Date().toISOString(),
        users: [
            { name: "김철수", ip: "192.168.0.23", role: "작업자" },
            { name: "이영희", ip: "192.168.0.102", role: "확인자" },
            { name: "박민수", ip: "192.168.0.105", role: "확인자" },
            { name: "정관리", ip: "192.168.0.58", role: "엔지니어" }
        ]
    };
    fs.writeFileSync(path.join(testShareDir, 'users.json'), JSON.stringify(initialUsers, null, 2), 'utf8');

    const agentBin = path.join(__dirname, '..', 'dist', 'cross-checker-agent.exe');
    console.log("Starting cross-checker-agent.exe on port 18080...");
    const agentProcess = spawn(agentBin, [
        '-port=18080',
        `-share=${testShareDir}`,
        '-bind-ip=192.168.0.23'
    ], { stdio: 'ignore' });

    let agentReady = false;
    for (let i = 0; i < 25; i++) {
        try {
            const res = await fetch('http://127.0.0.1:18080/api/me');
            if (res.ok) {
                agentReady = true;
                break;
            }
        } catch(e) {}
        await sleep(200);
    }
    if (!agentReady) throw new Error("cross-checker-agent failed to start on port 18080");
    console.log("cross-checker-agent.exe is ready on port 18080!");

    // 2. Chrome 시작
    console.log("Starting Chrome instance on port", DEBUG_PORT);
    const chrome = spawn(CHROME_PATH, [
        `--remote-debugging-port=${DEBUG_PORT}`,
        `--user-data-dir=${USER_DATA_DIR}`,
        '--disable-web-security',
        '--allow-running-insecure-content',
        '--disable-features=BlockInsecurePrivateNetworkRequests',
        '--disable-gpu',
        '--window-size=1600,1000',
        '--no-first-run',
        '--disable-extensions',
        '--disable-default-apps',
        'http://192.168.0.58:30080/#/inventories/inventory/3/hosts'
    ], { stdio: 'ignore' });

    let cdp = null;

    try {
        await sleep(3500);
        const targets = await fetchJson(`http://127.0.0.1:${DEBUG_PORT}/json`);
        const pageTarget = targets.find(t => t.type === 'page' && !t.url.startsWith('chrome-extension')) || targets[0];
        if (!pageTarget) throw new Error("No Chrome page target found");

        cdp = new CDPClient(pageTarget.webSocketDebuggerUrl);
        await cdp.connect();
        await cdp.send('Page.enable');
        await cdp.send('Runtime.enable');
        await sleep(2000);

        // 로그인 확인
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

        // 인벤토리 #3 호스트 페이지로 이동
        await cdp.send('Page.navigate', { url: 'http://192.168.0.58:30080/#/inventories/inventory/3/hosts' });
        for (let i = 0; i < 15; i++) {
            await sleep(1000);
            const rows = await cdp.evaluate(`document.querySelectorAll('table tbody tr').length`);
            if (rows > 0) break;
        }

        // 최신 소스코드 주입
        const srcPath = path.join(__dirname, '..', 'src', 'awx_host_checker.js');
        const code = fs.readFileSync(srcPath, 'utf8');
        await cdp.evaluate(code);
        await sleep(1000);

        // 다이얼로그 차단 방지 (테스트 환경 안전화)
        await cdp.evaluate(`
            window.alert = function(msg) {
                console.log('[BROWSER ALERT]', msg);
                window.__lastAlertMsg = msg;
            };
        `);

        // -------------------------------------------------------------
        // TC-01: 수집 신뢰성 증명 (AWX PostgreSQL DB 1000대 vs API 1000대 1:1 대조)
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-01: 호스트 수집 신뢰성 증명 (DB vs API 전수 대조)");
        const dbHostsRaw = execSshWithRetry("k3s kubectl exec -n awx awx-postgres-15-0 -- psql -U awx -d awx -t -A -c 'SELECT name FROM main_host WHERE inventory_id=3 ORDER BY name;'");
        const dbHosts = dbHostsRaw.trim().split(/\r?\n/).map(s => s.trim().toLowerCase()).filter(Boolean);
        const dbHash = crypto.createHash('sha256').update(dbHosts.join('\n')).digest('hex');

        await cdp.evaluate(`
            document.getElementById('awx-vld-input-exclude').value = '';
            document.getElementById('awx-vld-input-exclude').dispatchEvent(new Event('input'));
        `);
        const apiCollectedCount = await cdp.evaluate(`window.awxRunCollectAll()`);
        const apiHosts = await cdp.evaluate(`Array.from(window.awxValidatorState.collectedHosts).sort()`);
        const apiHash = crypto.createHash('sha256').update(apiHosts.join('\n')).digest('hex');

        if (dbHosts.length === 1000 && apiCollectedCount === 1000 && dbHash === apiHash) {
            recordLog('TC-01', '호스트 수집 신뢰성 증명', 'PASS', `DB 1000대와 API 1000대 전수 일치 (SHA256: ${dbHash.substring(0, 16)}...)`);
        } else {
            throw new Error(`TC-01 FAILED: DB(${dbHosts.length}) vs API(${apiCollectedCount}), HashMatch: ${dbHash === apiHash}`);
        }

        // -------------------------------------------------------------
        // TC-02: 인벤토리 격리 증명 (타 인벤토리 24대 혼입 차단)
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-02: 인벤토리 격리 증명");
        const allAwxJson = await cdp.evaluate(`
            (async () => {
                const res = await fetch('/api/v2/hosts/?page_size=1');
                return await res.json();
            })()
        `);
        const totalAwxHosts = allAwxJson.count;
        if (totalAwxHosts > 1000 && apiCollectedCount === 1000) {
            recordLog('TC-02', '인벤토리 격리 증명', 'PASS', `AWX 전체 ${totalAwxHosts}대 중 인벤토리 #3의 정확한 1000대만 격리 수집됨`);
        } else {
            throw new Error(`TC-02 FAILED: Total AWX hosts is ${totalAwxHosts}, but collected is ${apiCollectedCount}`);
        }

        // -------------------------------------------------------------
        // TC-03: 화면 DOM 수집 무결성
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-03: 화면 DOM 수집 무결성");
        await cdp.evaluate(`document.getElementById('awx-vld-btn-reset-awx').click()`);
        await cdp.evaluate(`document.getElementById('awx-vld-btn-page').click()`);
        let pageCount1 = await cdp.evaluate(`window.awxValidatorState.collectedHosts.size`);
        await cdp.evaluate(`document.getElementById('awx-vld-btn-page').click()`);
        await cdp.evaluate(`document.getElementById('awx-vld-btn-page').click()`);
        let pageCount2 = await cdp.evaluate(`window.awxValidatorState.collectedHosts.size`);
        let rawCount2 = await cdp.evaluate(`window.awxValidatorState.rawCount`);
        let pageSummaryText = await cdp.evaluate(`document.getElementById('awx-vld-awx-summary').innerText`);

        if (pageCount1 === 20 && pageCount2 === 20 && rawCount2 === 20 && pageSummaryText.includes("20대 수집 완료")) {
            recordLog('TC-03', '화면 DOM 수집 무결성', 'PASS', `1페이지 20대 정상 수집 및 3회 연속 재클릭 시 중복 카운트 증가 방지("${pageSummaryText.trim()}") 확인`);
        } else {
            throw new Error(`TC-03 FAILED: pageCount1=${pageCount1}, pageCount2=${pageCount2}, rawCount2=${rawCount2}, summary=${pageSummaryText}`);
        }

        // -------------------------------------------------------------
        // TC-04: AWX 수집 시 '|' 제외 필터링 및 요약 출력
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-04: AWX 수집 시 '|' 제외 필터링 및 요약 출력");
        const excludePipe = "prd-db-0295|prd-db-0296|prd-db-0297|prd-db-0298|prd-db-0299|prd-db-0300";
        await cdp.evaluate(`
            document.getElementById('awx-vld-input-exclude').value = ${JSON.stringify(excludePipe)};
            document.getElementById('awx-vld-input-exclude').dispatchEvent(new Event('input'));
        `);
        const afterExcludeCount = await cdp.evaluate(`window.awxRunCollectAll()`);
        const summaryText = await cdp.evaluate(`document.getElementById('awx-vld-awx-summary').innerText`);
        const excludedList = await cdp.evaluate(`window.awxValidatorState.excludedHosts`);

        if (afterExcludeCount === 994 && excludedList.length === 6 && summaryText.includes("1000대 중 6대 제외됨")) {
            recordLog('TC-04', "AWX 수집 시 '|' 제외 필터링", 'PASS', `1000대 중 6대 제외되어 994대 수집 반영 및 요약 문구 확인: "${summaryText.trim().replace(/\n/g, ' ')}"`);
        } else {
            throw new Error(`TC-04 FAILED: afterCount=${afterExcludeCount}, excludedCount=${excludedList.length}, summary=${summaryText}`);
        }

        // -------------------------------------------------------------
        // TC-05: 제외 미입력 시 회색 안내 메시지 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-05: 제외 미입력 시 회색 안내 메시지 검증");
        await cdp.evaluate(`
            document.getElementById('awx-vld-input-exclude').value = '';
            document.getElementById('awx-vld-input-exclude').dispatchEvent(new Event('input'));
        `);
        const hintDisplay = await cdp.evaluate(`document.getElementById('awx-vld-exclude-hint').style.display`);
        const hintText = await cdp.evaluate(`document.getElementById('awx-vld-exclude-hint').innerText`);

        if (hintDisplay !== 'none' && hintText.includes("OS설치에 필요한 서버명은 이미 제외되어 있음")) {
            recordLog('TC-05', '제외 미입력 안내문구', 'PASS', `미입력 시 안내 문구 정확히 노출: "${hintText}"`);
        } else {
            throw new Error(`TC-05 FAILED: hintDisplay=${hintDisplay}, hintText=${hintText}`);
        }

        // -------------------------------------------------------------
        // TC-06: 필수 입력 누락 차단 & 2개 소스 비교 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-06: 필수 입력 검증 & 2개 소스 비교");
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-detail').value = '';
            document.getElementById('awx-vld-txt-incident').value = '';
            window.awxDoCompare();
        `);
        let errorMsg = await cdp.evaluate(`document.getElementById('awx-vld-result-box').innerText`);
        if (!errorMsg.includes("오류: 필수 입력 항목 누락")) {
            throw new Error("TC-06 FAILED: 필수 입력 누락 차단 실패");
        }

        await cdp.evaluate(`document.getElementById('awx-vld-btn-reset-awx').click()`);
        const sample20 = "prd-web-0001\nprd-web-0002\nprd-web-0003\nprd-was-0001\nprd-db-0001";
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-detail').value = ${JSON.stringify(sample20)};
            document.getElementById('awx-vld-txt-incident').value = ${JSON.stringify(sample20)};
            document.getElementById('awx-vld-txt-target').value = '';
            document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
            window.awxDoCompare();
        `);
        let matchMsg2 = await cdp.evaluate(`document.getElementById('awx-vld-result-box').innerText`);
        let btnText2 = await cdp.evaluate(`document.getElementById('awx-vld-btn-compare').innerText`);

        if (btnText2.includes("2개 소스") && matchMsg2.includes("[MATCH]")) {
            recordLog('TC-06', '필수 입력 검증 & 2개 소스 비교', 'PASS', `필수 미입력 차단 확인 및 2개 소스(상세목록현황, 인시던트) 5대 전수 일치 통과`);
        } else {
            throw new Error(`TC-06 FAILED: btnText=${btnText2}, matchMsg=${matchMsg2}`);
        }

        // -------------------------------------------------------------
        // TC-07: 3개 및 4개 소스 비교 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-07: 3개 및 4개 소스 비교 검증");
        const sampleTargetMiss = "prd-web-0001\nprd-web-0002\nprd-web-0003\nprd-was-0001";
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-target').value = ${JSON.stringify(sampleTargetMiss)};
            document.getElementById('awx-vld-txt-target').dispatchEvent(new Event('input'));
            window.awxDoCompare();
        `);
        let btnText3 = await cdp.evaluate(`document.getElementById('awx-vld-btn-compare').innerText`);
        let diffMsg3 = await cdp.evaluate(`document.getElementById('awx-vld-result-box').innerText`);

        if (!btnText3.includes("3개 소스") || !diffMsg3.includes("작업대상: 누락 1대")) {
            throw new Error(`TC-07 FAILED (3 sources): btnText=${btnText3}, diffMsg=${diffMsg3}`);
        }

        await cdp.evaluate(`window.awxRunCollectAll()`);
        let btnText4 = await cdp.evaluate(`document.getElementById('awx-vld-btn-compare').innerText`);
        if (!btnText4.includes("4개 소스")) {
            throw new Error(`TC-07 FAILED (4 sources): btnText=${btnText4}`);
        }
        recordLog('TC-07', '3개 및 4개 소스 동적 비교', 'PASS', `작업대상 입력 시 3개 소스 감지 및 누락 1대 검출, AWX 수집 포함 시 4개 소스 비교 자동 전환 확인`);

        // -------------------------------------------------------------
        // TC-08: AWX 1000대 vs 대상 30대 ➔ 970대 누락 검출 & [명단 펼치기] 토글
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-08: 970대 누락 검출 & [명단 펼치기] 토글");
        const hosts30 = Array.from({ length: 30 }, (_, i) => `prd-db-${String(i + 1).padStart(4, '0')}`).join('\n');
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-detail').value = ${JSON.stringify(hosts30)};
            document.getElementById('awx-vld-txt-incident').value = ${JSON.stringify(hosts30)};
            document.getElementById('awx-vld-txt-target').value = '';
            document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
            window.awxDoCompare();
        `);
        const resultText30 = await cdp.evaluate(`document.getElementById('awx-vld-result-box').innerText`);
        if (!resultText30.includes("대상에 없음: 970대")) {
            throw new Error(`TC-08 FAILED: "대상에 없음: 970대" not found. Got: ${resultText30}`);
        }

        const toggleBtn = await cdp.evaluate(`
            (() => {
                const btn = document.querySelector('button[id^="btn-awx-toggle-"]');
                if (!btn) return false;
                btn.click();
                const targetId = btn.id.replace('btn-', '');
                const targetEl = document.getElementById(targetId);
                const isVisible = targetEl && targetEl.style.display !== 'none';
                return { btnText: btn.innerText, isVisible, hostCount: targetEl.innerText.trim().split(/\\s+/).length };
            })()
        `);

        if (toggleBtn && toggleBtn.isVisible && toggleBtn.hostCount === 970 && toggleBtn.btnText === '명단 접기') {
            recordLog('TC-08', '970대 누락 검출 & [명단 펼치기] 토글', 'PASS', `"대상에 없음: 970대 (AWX 1000대 중 30대만 존재)" 검출 및 버튼 클릭 시 970대 명단 펼치기 동작 검증 완료`);
        } else {
            throw new Error(`TC-08 FAILED (toggle): ${JSON.stringify(toggleBtn)}`);
        }

        // -------------------------------------------------------------
        // TC-09: MISMATCH/MATCH 작업 권장 안내 분기 & 중복 제거 & [펼치기] & 이모티콘 0건 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-09: MISMATCH/MATCH 작업 권장 안내 분기 & 중복 제거 & [펼치기] 검증");
        const mismatchGuideCheck = await cdp.evaluate(`
            (() => {
                const guideBtns = document.querySelectorAll('#awx-vld-btn-guide-toggle');
                const msgEl = document.getElementById('awx-vld-guide-msg');
                const btn = document.getElementById('awx-vld-btn-guide-toggle');
                const box = document.getElementById('awx-vld-guide-targets');
                const cnt = document.getElementById('awx-vld-guide-target-count');
                const list = document.getElementById('awx-vld-guide-target-list');
                if (btn) btn.click();
                return {
                    guideBoxCount: guideBtns.length,
                    mismatchMsg: msgEl ? msgEl.innerText.trim() : '',
                    expandedVisible: box ? box.style.display === 'block' : false,
                    targetCount: cnt ? cnt.innerText : '',
                    hasHost: list ? list.innerText.includes('prd-db-0001') : false
                };
            })()
        `);

        if (
            mismatchGuideCheck.guideBoxCount !== 1 ||
            !mismatchGuideCheck.mismatchMsg.includes("작업대상을 다시 확인하고") ||
            !mismatchGuideCheck.expandedVisible ||
            mismatchGuideCheck.targetCount !== '30' ||
            !mismatchGuideCheck.hasHost
        ) {
            throw new Error(`TC-09 FAILED (MISMATCH guide check): ${JSON.stringify(mismatchGuideCheck)}`);
        }

        const expectedMatchComment = "인시던트의 작업대상을 복사해서 붙여넣기를 권장합니다.vim [작업대상 파일] -> esc -> :set paste 입력(붙여넣기 모드) -> 붙여넣기";
        const matchGuideCheck = await cdp.evaluate(`
            (() => {
                document.getElementById('awx-vld-btn-reset-awx').click();
                const sampleMatch = "prd-db-0001\\nprd-db-0002\\nprd-db-0003";
                document.getElementById('awx-vld-txt-detail').value = sampleMatch;
                document.getElementById('awx-vld-txt-incident').value = sampleMatch;
                document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
                window.awxDoCompare();
                const guideBtns = document.querySelectorAll('#awx-vld-btn-guide-toggle');
                const msgEl = document.getElementById('awx-vld-guide-msg');
                return {
                    guideBoxCount: guideBtns.length,
                    matchMsg: msgEl ? msgEl.innerText.trim() : ''
                };
            })()
        `);

        if (matchGuideCheck.guideBoxCount !== 1 || !matchGuideCheck.matchMsg.includes(expectedMatchComment)) {
            throw new Error(`TC-09 FAILED (MATCH guide check): ${JSON.stringify(matchGuideCheck)}`);
        }

        const panelText = await cdp.evaluate(`document.getElementById('awx-validator-panel').innerText`);
        const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;
        const hasEmoji = emojiRegex.test(panelText);

        if (!hasEmoji && !panelText.includes("v2.2")) {
            recordLog('TC-09', 'MISMATCH/MATCH 안내문구 분기 & 중복 제거 & 펼치기', 'PASS', `중복 박스 제거(단일 출력), MISMATCH 시 재확인 문구 / MATCH 시 vim 붙여넣기 문구 분기 및 [펼치기] 정상 확인`);
        } else {
            throw new Error(`TC-09 FAILED: hasEmoji=${hasEmoji}`);
        }

        // -------------------------------------------------------------
        // TC-10: 이동 & 상단 고정(Pin) & 투명도 & 독립 창 전환
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-10: 이동 & 상단 고정(Pin) & 투명도 조절 & 독립 창 전환");
        const dragTest = await cdp.evaluate(`
            (() => {
                const panel = document.getElementById('awx-validator-panel');
                const header = document.getElementById('awx-vld-header');
                const rectBefore = panel.getBoundingClientRect();
                const before = { left: rectBefore.left, top: rectBefore.top };

                header.dispatchEvent(new MouseEvent('mousedown', { clientX: rectBefore.left + 50, clientY: rectBefore.top + 10, bubbles: true }));
                document.dispatchEvent(new MouseEvent('mousemove', { clientX: rectBefore.left + 50 - 150, clientY: rectBefore.top + 10 + 50, bubbles: true }));
                document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));

                const rectAfter = panel.getBoundingClientRect();
                return { before, after: { left: rectAfter.left, top: rectAfter.top }, moved: (rectAfter.left !== before.left) };
            })()
        `);

        const pinOpacityDetachValid = await cdp.evaluate(`
            (() => {
                const panel = document.getElementById('awx-validator-panel');
                const btnPin = document.getElementById('awx-vld-btn-pin');
                if (btnPin) btnPin.click();
                const isPinned = window.awxValidatorState.isPinned === true && btnPin.innerText.includes('상단 고정됨');

                panel.dispatchEvent(new MouseEvent('mouseenter'));
                const opInside = panel.style.opacity;

                panel.dispatchEvent(new MouseEvent('mouseleave'));
                const opOutside30 = panel.style.opacity;

                const range = document.getElementById('awx-vld-range-opacity');
                range.value = '50';
                range.dispatchEvent(new Event('input'));
                const opSlider50 = panel.style.opacity;

                range.value = '30';
                range.dispatchEvent(new Event('input'));

                const btnOpToggle = document.getElementById('awx-vld-btn-opacity-toggle');
                btnOpToggle.click();
                const opWhenOff = panel.style.opacity;
                const toggleOffText = btnOpToggle.innerText;

                btnOpToggle.click();
                panel.dispatchEvent(new MouseEvent('mouseenter'));

                const btnDetach = document.getElementById('awx-vld-btn-detach');
                btnDetach.click();
                const popup = window.awxPopupWindow;
                let popupMiniModeOk = false;
                if (popup && popup.document) {
                    popup.document.documentElement.dispatchEvent(new MouseEvent('mouseleave'));
                    const collapsedState = popup.awxPopupCollapsed === true &&
                        popup.document.getElementById('awx-vld-body-wrap').style.display === 'none' &&
                        popup.document.body.style.opacity === '1';

                    popup.document.documentElement.dispatchEvent(new MouseEvent('mouseenter'));
                    const expandedState = popup.awxPopupCollapsed === false &&
                        popup.document.getElementById('awx-vld-body-wrap').style.display === 'flex';

                    popupMiniModeOk = collapsedState && expandedState;
                    const popupReturnBtn = popup.document.getElementById('awx-vld-btn-detach');
                    if (popupReturnBtn) popupReturnBtn.click();
                }

                return {
                    isPinned,
                    opInside,
                    opOutside30,
                    opSlider50,
                    opWhenOff,
                    toggleOffText,
                    popupMiniModeOk
                };
            })()
        `);

        if (
            dragTest.moved &&
            pinOpacityDetachValid.isPinned &&
            pinOpacityDetachValid.opInside === '1' &&
            pinOpacityDetachValid.opOutside30 === '0.3' &&
            pinOpacityDetachValid.opSlider50 === '0.5' &&
            pinOpacityDetachValid.opWhenOff === '1' &&
            pinOpacityDetachValid.toggleOffText === '투명도 OFF' &&
            pinOpacityDetachValid.popupMiniModeOk
        ) {
            recordLog('TC-10', '이동 & 상단고정 & 페이지 투명도 & 독립창 자동접기(미니모드)', 'PASS', `드래그 이동, 상단 고정, 페이지 투명도(100%/30%/50%/OFF) 및 독립 창 이탈 시 헤더 줄 자동 축소(아래 창 보기)/복원 확인`);
        } else {
            throw new Error(`TC-10 FAILED: drag=${JSON.stringify(dragTest)}, details=${JSON.stringify(pinOpacityDetachValid)}`);
        }

        // -------------------------------------------------------------
        // TC-11: 수집 초기화 연동 및 톱니바퀴 설정(체크박스) 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-11: 수집 초기화 연동 및 톱니바퀴 설정(체크박스) 검증");
        const resetConfigTest = await cdp.evaluate(`
            (() => {
                const btnConfig = document.getElementById('awx-vld-btn-reset-config');
                const panelConfig = document.getElementById('awx-vld-reset-config-panel');
                const chkDetail = document.getElementById('awx-vld-chk-reset-detail');
                const chkIncident = document.getElementById('awx-vld-chk-reset-incident');
                const chkTarget = document.getElementById('awx-vld-chk-reset-target');
                const btnReset = document.getElementById('awx-vld-btn-reset-awx');
                const txtDetail = document.getElementById('awx-vld-txt-detail');
                const txtIncident = document.getElementById('awx-vld-txt-incident');
                const txtTarget = document.getElementById('awx-vld-txt-target');

                const initialPanelDisplay = panelConfig.style.display;
                btnConfig.click();
                const openedPanelDisplay = panelConfig.style.display;
                btnConfig.click();
                const closedPanelDisplay = panelConfig.style.display;

                txtDetail.value = 'server-a1 server-a2';
                txtIncident.value = 'server-b1 server-b2';
                txtTarget.value = 'server-c1 server-c2';
                txtDetail.dispatchEvent(new Event('input'));
                btnReset.click();
                const fullResetOk = (txtDetail.value === '' && txtIncident.value === '' && txtTarget.value === '');

                chkTarget.click();
                txtDetail.value = 'server-d1';
                txtIncident.value = 'server-e1';
                txtTarget.value = 'server-preserve';
                txtDetail.dispatchEvent(new Event('input'));
                btnReset.click();
                const targetPreservedOk = (txtDetail.value === '' && txtIncident.value === '' && txtTarget.value === 'server-preserve');

                chkDetail.click();
                chkIncident.click();
                txtDetail.value = 'keep-detail';
                txtIncident.value = 'keep-incident';
                txtTarget.value = 'keep-target';
                txtDetail.dispatchEvent(new Event('input'));
                btnReset.click();
                const allPreservedOk = (txtDetail.value === 'keep-detail' && txtIncident.value === 'keep-incident' && txtTarget.value === 'keep-target');

                chkDetail.click();
                chkIncident.click();
                chkTarget.click();
                btnReset.click();

                return {
                    initialPanelDisplay,
                    openedPanelDisplay,
                    closedPanelDisplay,
                    fullResetOk,
                    targetPreservedOk,
                    allPreservedOk
                };
            })()
        `);

        if (
            resetConfigTest.initialPanelDisplay === 'none' &&
            resetConfigTest.openedPanelDisplay === 'block' &&
            resetConfigTest.closedPanelDisplay === 'none' &&
            resetConfigTest.fullResetOk &&
            resetConfigTest.targetPreservedOk &&
            resetConfigTest.allPreservedOk
        ) {
            recordLog('TC-11', '수집 초기화 연동 및 톱니바퀴 설정(체크박스) 검증', 'PASS', `톱니바퀴 패널 토글, 기본 전체 연동 초기화, 선택적 초기화(작업대상 보존), 전체 보존 모드 정상 검증 완료`);
        } else {
            throw new Error(`TC-11 FAILED: ${JSON.stringify(resetConfigTest)}`);
        }

        // -------------------------------------------------------------
        // TC-12: 슬래시(/) 앞뒤 공백 유무에 따른 시리얼 정제 및 호스트 2대 인식 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-12: 슬래시(/) 앞뒤 공백 유무에 따른 호스트 인식 무결성");
        const slashTest = await cdp.evaluate(`
            (() => {
                const txtDetail = document.getElementById('awx-vld-txt-detail');
                const txtIncident = document.getElementById('awx-vld-txt-incident');
                const txtTarget = document.getElementById('awx-vld-txt-target');
                const cntDetail = document.getElementById('awx-vld-cnt-detail');
                const cntIncident = document.getElementById('awx-vld-cnt-incident');
                const cntTarget = document.getElementById('awx-vld-cnt-target');
                const btnReset = document.getElementById('awx-vld-btn-reset-awx');

                btnReset.click();

                const inputWithSpaces = "pivy2351ev01 / AAAAAA\\npivy2351ev02 / BBBBBB";
                txtDetail.value = inputWithSpaces;
                txtDetail.dispatchEvent(new Event('input'));
                const countSpaces = cntDetail.innerText;

                const inputNoSpaces = "pivy2351ev01/AAAAAA\\npivy2351ev02/BBBBBB";
                txtIncident.value = inputNoSpaces;
                txtIncident.dispatchEvent(new Event('input'));
                const countNoSpaces = cntIncident.innerText;

                const inputMixed = "pivy2351ev01 /AAAAAA\\npivy2351ev02/ BBBBBB";
                txtTarget.value = inputMixed;
                txtTarget.dispatchEvent(new Event('input'));
                const countMixed = cntTarget.innerText;

                document.getElementById('awx-vld-btn-compare').click();
                const resultBox = document.getElementById('awx-vld-result-box');
                const matchSuccess = resultBox.innerText.includes('[MATCH]');
                const resultText = resultBox.innerText;

                return {
                    countSpaces,
                    countNoSpaces,
                    countMixed,
                    matchSuccess,
                    resultText,
                    awxSize: window.awxValidatorState.collectedHosts.size
                };
            })()
        `);

        if (
            slashTest.countSpaces === '2' &&
            slashTest.countNoSpaces === '2' &&
            slashTest.countMixed === '2' &&
            slashTest.matchSuccess
        ) {
            recordLog('TC-12', '슬래시(/) 앞뒤 공백 정제 무결성', 'PASS', `hostname / AAAAAA 형태 2대 입력 시 4대가 아닌 정확히 2대로 인식 및 [MATCH] 성공 확인`);
        } else {
            throw new Error(`TC-12 FAILED: ${JSON.stringify(slashTest)}`);
        }

        // =============================================================
        // 신규 크로스체크 확인자 연동 테스트케이스 (TC-13 ~ TC-24)
        // =============================================================

        // -------------------------------------------------------------
        // TC-13: 사용자/IP 명부(users.json) 자동 매칭 및 신규 등록
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-13: 사용자/IP 명부(users.json) 자동 매칭 및 신규 등록");
        const meRes = await fetch('http://127.0.0.1:18080/api/me');
        const meData = await meRes.json();
        if (meData.ip !== '192.168.0.23' || meData.name !== '김철수' || !meData.registered) {
            throw new Error(`TC-13 FAILED (me): ${JSON.stringify(meData)}`);
        }

        const regRes = await fetch('http://127.0.0.1:18080/api/users/register', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: "최신규", ip: "192.168.0.199", role: "확인자" })
        });
        if (!regRes.ok) throw new Error(`TC-13 FAILED (register status): ${regRes.status}`);

        const usersRes = await fetch('http://127.0.0.1:18080/api/users');
        const usersData = await usersRes.json();
        const foundNewUser = usersData.users.find(u => u.name === "최신규" && u.ip === "192.168.0.199");
        if (!foundNewUser) throw new Error(`TC-13 FAILED (users list): ${JSON.stringify(usersData)}`);

        recordLog('TC-13', '사용자/IP 명부(users.json) 자동 매칭 및 신규 등록', 'PASS', `IP(192.168.0.23) -> "김철수" 자동 식별 및 신규 사용자("최신규", 192.168.0.199) 등록 및 파일 동기화 확인`);

        // -------------------------------------------------------------
        // TC-14: 인시던트 번호(권장) & 특이사항 전달 무결성
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-14: 인시던트 번호(권장) & 특이사항 전달 무결성");
        const t14Result = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('worker');
                const incInput = document.getElementById('awx-vld-worker-incident-no');
                const hintEl = document.getElementById('awx-vld-worker-incident-hint');
                const notesInput = document.getElementById('awx-vld-worker-notes');

                const hintVisibleInitially = hintEl && hintEl.style.display !== 'none';

                incInput.value = 'INC-2026-7788';
                incInput.dispatchEvent(new Event('input'));
                const hintHiddenAfterType = hintEl && hintEl.style.display === 'none';

                notesInput.value = 'DB 6대 제외 확인 필요 및 야간 배포 건';
                notesInput.dispatchEvent(new Event('input'));

                document.getElementById('awx-vld-txt-detail').value = "prd-web-0001\\nprd-web-0002\\nprd-web-0003";
                document.getElementById('awx-vld-txt-incident').value = "prd-web-0001\\nprd-web-0002\\nprd-web-0003";
                document.getElementById('awx-vld-txt-target').value = "";
                document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));

                const sel = document.getElementById('awx-vld-sel-checker');
                if (sel) {
                    sel.value = '192.168.0.102';
                    sel.dispatchEvent(new Event('change'));
                }

                let sendRet = null;
                let sendErr = null;
                try {
                    sendRet = await window.awxSendCrossCheckRequest('192.168.0.102');
                } catch(e) {
                    sendErr = e.message;
                }
                const sid = window.awxValidatorState.currentSession ? window.awxValidatorState.currentSession.sessionId : null;
                return {
                    hintVisibleInitially,
                    hintHiddenAfterType,
                    sessionId: sid,
                    sendRet,
                    sendErr,
                    lastAlert: window.__lastAlertMsg,
                    agentConnected: window.awxValidatorState.agentInfo ? window.awxValidatorState.agentInfo.connected : false
                };
            })()
        `);

        if (!t14Result.hintVisibleInitially || !t14Result.hintHiddenAfterType || !t14Result.sessionId) {
            throw new Error(`TC-14 FAILED (worker side): ${JSON.stringify(t14Result)}`);
        }

        const cdpCheckSessRes = await fetch(`http://127.0.0.1:18080/api/session/status?sessionId=${t14Result.sessionId}&clientIp=192.168.0.102`);
        const cdpCheckSess = await cdpCheckSessRes.json();
        if (cdpCheckSess.metadata.incidentId !== 'INC-2026-7788' || !cdpCheckSess.metadata.specialNotes.includes('DB 6대 제외')) {
            throw new Error(`TC-14 FAILED (checker metadata): ${JSON.stringify(cdpCheckSess.metadata)}`);
        }
        recordLog('TC-14', '인시던트 번호(권장) & 특이사항 전달 무결성', 'PASS', `인시던트 번호(INC-2026-7788) 및 특이사항 메모가 수신자 세션에 손실 없이 100% 전달됨 확인`);

        // -------------------------------------------------------------
        // TC-15: 작업자 활성 항목(2~4개) 동적 동기화 (AWX 제외 규칙)
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-15: 작업자 활성 항목(2~4개) 동적 동기화");
        if (cdpCheckSess.activeFields.target !== false || cdpCheckSess.activeFields.detail !== true || cdpCheckSess.activeFields.incident !== true) {
            throw new Error(`TC-15 FAILED (3 sources active fields mismatch): ${JSON.stringify(cdpCheckSess.activeFields)}`);
        }

        const t15CheckerDom1 = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('checker');
                await window.awxLoadCheckerSession('${t14Result.sessionId}', '192.168.0.102');
                const targetInput = document.getElementById('awx-vld-txt-chk-target');
                const badge = document.getElementById('awx-vld-chk-target-badge');
                return {
                    targetDisabled: targetInput ? targetInput.disabled : false,
                    badgeText: badge ? badge.innerText : ''
                };
            })()
        `);
        if (!t15CheckerDom1.targetDisabled || !t15CheckerDom1.badgeText.includes('제외')) {
            throw new Error(`TC-15 FAILED (checker target should be disabled): ${JSON.stringify(t15CheckerDom1)}`);
        }

        // 대상 입력란 활성화 테스트
        const t15Result2 = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('worker');
                document.getElementById('awx-vld-txt-target').value = "prd-web-0001\\nprd-web-0002\\nprd-web-0003";
                document.getElementById('awx-vld-txt-target').dispatchEvent(new Event('input'));
                const sel = document.getElementById('awx-vld-sel-checker');
                if (sel) {
                    sel.value = '192.168.0.102';
                    sel.dispatchEvent(new Event('change'));
                }
                await window.awxSendCrossCheckRequest('192.168.0.102');
                return window.awxValidatorState.currentSession.sessionId;
            })()
        `);
        const t15CheckerDom2 = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('checker');
                await window.awxLoadCheckerSession('${t15Result2}', '192.168.0.102');
                const targetInput = document.getElementById('awx-vld-txt-chk-target');
                const badge = document.getElementById('awx-vld-chk-target-badge');
                return {
                    targetDisabled: targetInput ? targetInput.disabled : true,
                    badgeText: badge ? badge.innerText : ''
                };
            })()
        `);
        if (t15CheckerDom2.targetDisabled || !t15CheckerDom2.badgeText.includes('필수')) {
            throw new Error(`TC-15 FAILED (checker target should be enabled): ${JSON.stringify(t15CheckerDom2)}`);
        }
        recordLog('TC-15', '작업자 활성 항목(2~4개) 동적 동기화', 'PASS', `작업대상 미입력 시 확인자 화면 비활성화(제외), 작업대상 입력 시 필수 항목으로 자동 전환 확인`);

        // -------------------------------------------------------------
        // TC-16: 작업자 호스트 블라인드(앞 3대만 노출) 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-16: 작업자 호스트 블라인드(앞 3대만 노출) 검증");
        const hosts30List = Array.from({ length: 30 }, (_, i) => `prd-app-${String(i + 1).padStart(4, '0')}`).join('\n');
        const t16Sid = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('worker');
                document.getElementById('awx-vld-txt-detail').value = ${JSON.stringify(hosts30List)};
                document.getElementById('awx-vld-txt-incident').value = ${JSON.stringify(hosts30List)};
                document.getElementById('awx-vld-txt-target').value = "";
                document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
                const sel = document.getElementById('awx-vld-sel-checker');
                if (sel) {
                    sel.value = '192.168.0.102';
                    sel.dispatchEvent(new Event('change'));
                }
                await window.awxSendCrossCheckRequest('192.168.0.102');
                return window.awxValidatorState.currentSession.sessionId;
            })()
        `);

        const t16ApiRes = await fetch(`http://127.0.0.1:18080/api/session/status?sessionId=${t16Sid}&clientIp=192.168.0.102`);
        const t16ApiData = await t16ApiRes.json();
        if (
            !t16ApiData.blinded ||
            t16ApiData.workerData.detailHostsPreview.previewHosts.length !== 3 ||
            t16ApiData.workerData.detailHostsPreview.totalCount !== 30 ||
            t16ApiData.workerData.detailHostsPreview.blindedCount !== 27 ||
            t16ApiData.workerData.detailHosts.length !== 0
        ) {
            throw new Error(`TC-16 FAILED (blinded API data): ${JSON.stringify(t16ApiData.workerData)}`);
        }

        const t16Dom = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('checker');
                await window.awxLoadCheckerSession('${t16Sid}', '192.168.0.102');
                const blindText = document.getElementById('awx-vld-chk-blind-detail').innerText;
                return {
                    blindText,
                    hasTop3: blindText.includes('prd-app-0001') && blindText.includes('prd-app-0002') && blindText.includes('prd-app-0003'),
                    hasBlinded27: blindText.includes('외 27대') && blindText.includes('블라인드'),
                    hasLastHost: blindText.includes('prd-app-0030')
                };
            })()
        `);
        if (!t16Dom.hasTop3 || !t16Dom.hasBlinded27 || t16Dom.hasLastHost) {
            throw new Error(`TC-16 FAILED (blinded DOM): ${JSON.stringify(t16Dom)}`);
        }
        recordLog('TC-16', '작업자 호스트 블라인드(앞 3대만 노출) 검증', 'PASS', `30대 중 앞 3대만 노출되고 나머지 27대는 블라인드 처리되어 복사 방지 무결성 증명 완료`);

        // -------------------------------------------------------------
        // TC-17: 확인자 필수 입력 누락 차단 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-17: 확인자 필수 입력 누락 차단 검증");
        const t17Result = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('checker');
                document.getElementById('awx-vld-txt-chk-detail').value = "";
                document.getElementById('awx-vld-txt-chk-incident').value = "";
                await window.awxDoCheckerCompare();
                const resBox = document.getElementById('awx-vld-chk-result-box');
                return {
                    boxVisible: resBox.style.display !== 'none',
                    boxText: resBox.innerText,
                    hasApproveBtn: !!document.getElementById('awx-vld-btn-approve')
                };
            })()
        `);
        if (!t17Result.boxVisible || !t17Result.boxText.includes('필수 입력 항목 누락') || t17Result.hasApproveBtn) {
            throw new Error(`TC-17 FAILED: ${JSON.stringify(t17Result)}`);
        }
        recordLog('TC-17', '확인자 필수 입력 누락 차단 검증', 'PASS', `확인자가 필수 입력란을 비워두었을 때 비교 차단 및 승인 버튼 노출 방지 확인`);

        // -------------------------------------------------------------
        // TC-18: 권한 없는 제3자(타 IP) 접근 및 실행 원천 차단
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-18: 권한 없는 제3자(타 IP) 접근 및 실행 원천 차단");
        const unauthStatus = await fetch(`http://127.0.0.1:18080/api/session/status?sessionId=${t16Sid}&clientIp=192.168.0.250`);
        if (unauthStatus.status !== 403) {
            throw new Error(`TC-18 FAILED: expected 403 on status, got ${unauthStatus.status}`);
        }

        const unauthDecision = await fetch(`http://127.0.0.1:18080/api/session/decision`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                sessionId: t16Sid,
                checkerIp: "192.168.0.250",
                checkerData: {
                    decision: "APPROVED",
                    isAllMatch: true
                }
            })
        });
        if (unauthDecision.status !== 403) {
            throw new Error(`TC-18 FAILED: expected 403 on decision, got ${unauthDecision.status}`);
        }
        recordLog('TC-18', '권한 없는 제3자(타 IP) 접근 차단', 'PASS', `지정되지 않은 타 IP(192.168.0.250)의 세션 조회 및 승인 시도에 대해 HTTP 403 Forbidden 차단 확인`);

        // -------------------------------------------------------------
        // TC-19: 무쿨타임(0초) 확인자 즉시 변경 (Reassign) 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-19: 무쿨타임(0초) 확인자 즉시 변경 (Reassign) 검증");
        const t19Result = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('worker');
                await window.awxReassignChecker('192.168.0.105');
                const c1 = window.awxValidatorState.currentSession.checkerIp;

                await window.awxReassignChecker('192.168.0.58');
                const c2 = window.awxValidatorState.currentSession.checkerIp;

                return { c1, c2, sid: window.awxValidatorState.currentSession.sessionId };
            })()
        `);

        const t19SessRes = await fetch(`http://127.0.0.1:18080/api/session/status?sessionId=${t19Result.sid}&clientIp=192.168.0.23`);
        const t19Sess = await t19SessRes.json();
        if (t19Sess.checker.ip !== '192.168.0.58' || t19Sess.checkerHistory.length < 2) {
            throw new Error(`TC-19 FAILED: ${JSON.stringify(t19Sess.checkerHistory)}`);
        }
        recordLog('TC-19', '무쿨타임(0초) 확인자 즉시 변경 (Reassign)', 'PASS', `대기시간(쿨타임) 0초로 확인자 변경 2회 연속 수행 및 이력(checkerHistory: 2건) 정상 기록 확인`);

        // -------------------------------------------------------------
        // TC-20: 작업자 입력분 vs 확인자 입력분 불일치([MISMATCH]) 검출 및 승인 잠금
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-20: 불일치([MISMATCH]) 검출 및 승인 잠금");
        const t20Sid = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('worker');
                const hosts5 = "prd-web-0001\\nprd-web-0002\\nprd-web-0003\\nprd-web-0004\\nprd-web-0005";
                document.getElementById('awx-vld-txt-detail').value = hosts5;
                document.getElementById('awx-vld-txt-incident').value = hosts5;
                document.getElementById('awx-vld-txt-target').value = "";
                document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
                const sel = document.getElementById('awx-vld-sel-checker');
                if (sel) {
                    sel.value = '192.168.0.102';
                    sel.dispatchEvent(new Event('change'));
                }
                await window.awxSendCrossCheckRequest('192.168.0.102');
                return window.awxValidatorState.currentSession.sessionId;
            })()
        `);

        const t20Check = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('checker');
                await window.awxLoadCheckerSession('${t20Sid}', '192.168.0.102');
                const typoHosts = "prd-web-0001\\nprd-web-0002\\nprd-web-0003\\nprd-web-0004\\nprd-web-9999";
                document.getElementById('awx-vld-txt-chk-detail').value = typoHosts;
                document.getElementById('awx-vld-txt-chk-incident').value = typoHosts;
                document.getElementById('awx-vld-txt-chk-detail').dispatchEvent(new Event('input'));
                await window.awxDoCheckerCompare();
                const resBox = document.getElementById('awx-vld-chk-result-box');
                const btnApprove = document.getElementById('awx-vld-btn-approve');
                return {
                    hasMismatch: resBox.innerText.includes('[MISMATCH]'),
                    hasTypoDetected: resBox.innerText.includes('prd-web-9999') || resBox.innerText.includes('prd-web-0005'),
                    hasApproveBtn: !!btnApprove,
                    hasRejectBtn: !!document.getElementById('awx-vld-btn-reject')
                };
            })()
        `);
        if (!t20Check.hasMismatch || !t20Check.hasTypoDetected || t20Check.hasApproveBtn || !t20Check.hasRejectBtn) {
            throw new Error(`TC-20 FAILED: ${JSON.stringify(t20Check)}`);
        }
        recordLog('TC-20', '불일치([MISMATCH]) 검출 및 승인 잠금', 'PASS', `확인자 오타/누락 입력 시 [MISMATCH] 정확히 적발, [확인 완료(승인)] 버튼 잠금 및 반려 입력 폼 활성화 확인`);

        // -------------------------------------------------------------
        // TC-21: 확인자 [반려 / 재확인 요청] 및 작업자 실시간 수신 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-21: 확인자 [반려 / 재확인 요청] 및 작업자 실시간 수신 검증");
        await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('checker');
                const reasonInput = document.getElementById('awx-vld-txt-reject-reason');
                if (reasonInput) reasonInput.value = "prd-web-0005 누락 및 9999 오타 감지됨";
                await window.awxSubmitDecision('REJECTED', "prd-web-0005 누락 및 9999 오타 감지됨");
                return true;
            })()
        `);

        await sleep(300);
        const t21WorkerDom = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('worker');
                await window.awxRefreshWorkerSessionStatus('${t20Sid}');
                const stText = document.getElementById('awx-vld-w-session-status').innerText;
                const repText = document.getElementById('awx-vld-w-result-report').innerText;
                return { stText, repText };
            })()
        `);
        if (!t21WorkerDom.stText.includes('REJECTED') || !t21WorkerDom.repText.includes('CROSS-CHECK REJECTED')) {
            throw new Error(`TC-21 FAILED: ${JSON.stringify(t21WorkerDom)}`);
        }
        recordLog('TC-21', '확인자 [반려 / 재확인 요청] 및 작업자 실시간 수신', 'PASS', `반려 사유 입력 후 제출 시 세션이 REJECTED 상태로 전이되고 작업자 화면에 [CROSS-CHECK REJECTED] 및 사유가 실시간 표시됨 확인`);

        // -------------------------------------------------------------
        // TC-22: 전체 소스 전수 일치([MATCH]) 및 [확인 완료(승인)] E2E 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-22: 전체 소스 전수 일치([MATCH]) 및 [확인 완료(승인)] E2E 검증");
        const matchHosts30 = Array.from({ length: 30 }, (_, i) => `prd-db-${String(i + 1).padStart(4, '0')}`).join('\n');
        const t22Sid = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('worker');
                document.getElementById('awx-vld-txt-detail').value = ${JSON.stringify(matchHosts30)};
                document.getElementById('awx-vld-txt-incident').value = ${JSON.stringify(matchHosts30)};
                document.getElementById('awx-vld-txt-target').value = "";
                document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
                const sel = document.getElementById('awx-vld-sel-checker');
                if (sel) {
                    sel.value = '192.168.0.102';
                    sel.dispatchEvent(new Event('change'));
                }
                await window.awxSendCrossCheckRequest('192.168.0.102');
                return window.awxValidatorState.currentSession.sessionId;
            })()
        `);

        const t22Approve = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('checker');
                await window.awxLoadCheckerSession('${t22Sid}', '192.168.0.102');
                document.getElementById('awx-vld-txt-chk-detail').value = ${JSON.stringify(matchHosts30)};
                document.getElementById('awx-vld-txt-chk-incident').value = ${JSON.stringify(matchHosts30)};
                document.getElementById('awx-vld-txt-chk-detail').dispatchEvent(new Event('input'));
                await window.awxDoCheckerCompare();
                const hasMatch = document.getElementById('awx-vld-chk-result-box').innerText.includes('[MATCH]');
                await window.awxSubmitDecision('APPROVED', '');
                return { hasMatch };
            })()
        `);
        if (!t22Approve.hasMatch) {
            throw new Error(`TC-22 FAILED on compare: ${JSON.stringify(t22Approve)}`);
        }

        await sleep(300);
        const t22WorkerDom = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('worker');
                await window.awxRefreshWorkerSessionStatus('${t22Sid}');
                const stText = document.getElementById('awx-vld-w-session-status').innerText;
                const repText = document.getElementById('awx-vld-w-result-report').innerText;
                return {
                    stText,
                    repText,
                    hasApprovedBadge: repText.includes('[CROSS-CHECK APPROVED]'),
                    hasVimComment: repText.includes(':set paste 입력(붙여넣기 모드)')
                };
            })()
        `);
        if (!t22WorkerDom.hasApprovedBadge || !t22WorkerDom.hasVimComment) {
            throw new Error(`TC-22 FAILED: ${JSON.stringify(t22WorkerDom)}`);
        }
        recordLog('TC-22', '전체 소스 전수 일치([MATCH]) 및 [확인 완료(승인)] E2E', 'PASS', `30대 전수 일치 [MATCH] 판정 후 [확인 완료(승인)] 클릭 시 작업자 화면에 [CROSS-CHECK APPROVED] 및 vim 붙여넣기 권장 코멘트 출력 확인`);

        // -------------------------------------------------------------
        // TC-23: 실시간 연동 전송 속도 (Latency < 3,000ms) 성능 증명
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-23: 실시간 연동 전송 속도 (Latency < 3,000ms) 성능 증명");
        const tStart = Date.now();
        const t23Sid = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('worker');
                const sample10 = "host-01\\nhost-02\\nhost-03\\nhost-04\\nhost-05";
                document.getElementById('awx-vld-txt-detail').value = sample10;
                document.getElementById('awx-vld-txt-incident').value = sample10;
                document.getElementById('awx-vld-txt-target').value = "";
                document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));
                const sel = document.getElementById('awx-vld-sel-checker');
                if (sel) {
                    sel.value = '192.168.0.102';
                    sel.dispatchEvent(new Event('change'));
                }
                await window.awxSendCrossCheckRequest('192.168.0.102');
                return window.awxValidatorState.currentSession.sessionId;
            })()
        `);
        await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('checker');
                await window.awxLoadCheckerSession('${t23Sid}', '192.168.0.102');
                const sample10 = "host-01\\nhost-02\\nhost-03\\nhost-04\\nhost-05";
                document.getElementById('awx-vld-txt-chk-detail').value = sample10;
                document.getElementById('awx-vld-txt-chk-incident').value = sample10;
                document.getElementById('awx-vld-txt-chk-detail').dispatchEvent(new Event('input'));
                await window.awxDoCheckerCompare();
                await window.awxSubmitDecision('APPROVED', '');
            })()
        `);
        await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('worker');
                await window.awxRefreshWorkerSessionStatus('${t23Sid}');
            })()
        `);
        const totalLatencyMs = Date.now() - tStart;
        if (totalLatencyMs >= 3000) {
            throw new Error(`TC-23 FAILED: Latency was ${totalLatencyMs}ms, expected < 3000ms`);
        }
        recordLog('TC-23', '실시간 연동 전송 속도 (Latency < 3000ms)', 'PASS', `요청 생성 -> 확인자 수신 -> 비교 -> 승인 -> 작업자 화면 반영 전체 왕복 속도: ${totalLatencyMs}ms (< 3,000ms 요구사항 통과)`);

        // -------------------------------------------------------------
        // TC-24: Direct HTTP 차단 시 공유폴더 파일 폴백(Fallback) 동기화 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-24: Direct HTTP 차단 시 공유폴더 파일 폴백(Fallback) 동기화 검증");
        await fetch('http://127.0.0.1:18080/api/users/register', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({ name: "오프라인확인자", ip: "192.0.2.1" })
        });

        const t24Sid = await cdp.evaluate(`
            (async () => {
                await window.awxSwitchTab('worker');
                const sHosts = "fallback-srv-01\\nfallback-srv-02";
                document.getElementById('awx-vld-txt-detail').value = sHosts;
                document.getElementById('awx-vld-txt-incident').value = sHosts;
                document.getElementById('awx-vld-txt-target').value = "";
                document.getElementById('awx-vld-txt-detail').dispatchEvent(new Event('input'));

                const sel = document.getElementById('awx-vld-sel-checker');
                if (sel) {
                    sel.innerHTML += '<option value="192.0.2.1" selected>오프라인확인자 (192.0.2.1)</option>';
                    sel.value = '192.0.2.1';
                    sel.dispatchEvent(new Event('change'));
                }
                await window.awxSendCrossCheckRequest('192.0.2.1');
                return window.awxValidatorState.currentSession.sessionId;
            })()
        `);

        // 파일 직접 수정 (공유폴더 SMB 파일 복제 시뮬레이션)
        const sessFilePath = path.join(testShareDir, 'sessions', `${t24Sid}.json`);
        const sessContent = JSON.parse(fs.readFileSync(sessFilePath, 'utf8'));
        sessContent.status = "APPROVED";
        sessContent.decidedAt = new Date().toISOString();
        sessContent.checkerData = {
            decision: "APPROVED",
            isAllMatch: true,
            decisionComment: "공유폴더 파일 직접 동기화"
        };
        fs.writeFileSync(sessFilePath, JSON.stringify(sessContent, null, 2), 'utf8');

        const fallbackStart = Date.now();
        let fallbackDetected = false;
        for (let i = 0; i < 10; i++) {
            await sleep(350);
            const wStatus = await cdp.evaluate(`
                (async () => {
                    await window.awxRefreshWorkerSessionStatus('${t24Sid}');
                    return window.awxValidatorState.currentSession ? window.awxValidatorState.currentSession.status : '';
                })()
            `);
            if (wStatus === 'APPROVED') {
                fallbackDetected = true;
                break;
            }
        }
        const fallbackTime = Date.now() - fallbackStart;
        if (!fallbackDetected || fallbackTime >= 3000) {
            throw new Error(`TC-24 FAILED: Fallback detection took ${fallbackTime}ms, detected: ${fallbackDetected}`);
        }
        recordLog('TC-24', '공유폴더 파일 폴백(Fallback) 동기화 검증', 'PASS', `Direct HTTP 차단 상태에서 공유폴더(sessions/${t24Sid}.json) 파일 감시(500ms)로 ${fallbackTime}ms 만에 승인 상태 동기화 완료`);

        // 최종 성공 스크린샷 캡처
        await cdp.screenshot(path.join(__dirname, "screenshot_onetake_success.png"));

        console.log("\n==================================================================");
        console.log("  [ONE-TAKE SUCCESS] 24개 테스트케이스 전원 100% 연속 통과!");
        console.log("==================================================================");

        return testLogs;
    } finally {
        if (cdp) cdp.close();
        chrome.kill();
        if (agentProcess) {
            try { agentProcess.kill(); } catch (e) {}
        }
    }
}

runOneTakeTests().then(logs => {
    fs.writeFileSync(path.join(__dirname, 'test_results.json'), JSON.stringify(logs, null, 2), 'utf8');
    process.exit(0);
}).catch(err => {
    console.error("\n[ONE-TAKE FAILURE]:", err.message);
    process.exit(1);
});
