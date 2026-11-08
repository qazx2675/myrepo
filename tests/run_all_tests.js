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
    console.log("==================================================================");

    const testLogs = [];
    function recordLog(tcId, name, result, detail) {
        const line = `[${result}] ${tcId}: ${name} - ${detail}`;
        console.log(line);
        testLogs.push({ tcId, name, result, detail });
    }

    console.log("Starting Chrome instance on port", DEBUG_PORT);
    const chrome = spawn(CHROME_PATH, [
        `--remote-debugging-port=${DEBUG_PORT}`,
        `--user-data-dir=${USER_DATA_DIR}`,
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

        // -------------------------------------------------------------
        // TC-01: 수집 신뢰성 증명 (AWX PostgreSQL DB 1000대 vs API 1000대 1:1 대조)
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-01: 호스트 수집 신뢰성 증명 (DB vs API 전수 대조)");
        const dbHostsRaw = execSshWithRetry("k3s kubectl exec -n awx awx-postgres-15-0 -- psql -U awx -d awx -t -A -c 'SELECT name FROM main_host WHERE inventory_id=3 ORDER BY name;'");
        const dbHosts = dbHostsRaw.trim().split(/\r?\n/).map(s => s.trim().toLowerCase()).filter(Boolean);
        const dbHash = crypto.createHash('sha256').update(dbHosts.join('\n')).digest('hex');

        // 제외값 비운 상태로 전체 1000대 수집
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
        // AWX 전체 전역 호스트 수는 /api/v2/hosts/ 의 count 필드로 신뢰성 있게 확인 (총 1024~1025대)
        const allAwxJson = await cdp.evaluate(`
            (async () => {
                const res = await fetch('/api/v2/hosts/?page_size=1');
                return await res.json();
            })()
        `);
        const totalAwxHosts = allAwxJson.count; // 1024대 이상
        if (totalAwxHosts > 1000 && apiCollectedCount === 1000) {
            recordLog('TC-02', '인벤토리 격리 증명', 'PASS', `AWX 전체 ${totalAwxHosts}대 중 인벤토리 #3의 정확한 1000대만 격리 수집됨`);
        } else {
            throw new Error(`TC-02 FAILED: Total AWX hosts is ${totalAwxHosts}, but collected is ${apiCollectedCount}`);
        }

        // -------------------------------------------------------------
        // TC-03: 화면 DOM 수집 무결성 (페이지별 누적 및 중복 클릭 시 카운트 증가 방지)
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-03: 화면 DOM 수집 무결성");
        await cdp.evaluate(`document.getElementById('awx-vld-btn-reset-awx').click()`);
        await cdp.evaluate(`document.getElementById('awx-vld-btn-page').click()`);
        let pageCount1 = await cdp.evaluate(`window.awxValidatorState.collectedHosts.size`);
        // 재클릭(2회, 3회) 시 중복 방지 및 요약문구 rawCount 증가 방지 확인
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
        // TC-04: AWX 수집 시 '|' 제외 필터링 및 요약 출력 검증
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
        // 필수 입력 비운 상태
        await cdp.evaluate(`
            document.getElementById('awx-vld-txt-detail').value = '';
            document.getElementById('awx-vld-txt-incident').value = '';
            window.awxDoCompare();
        `);
        let errorMsg = await cdp.evaluate(`document.getElementById('awx-vld-result-box').innerText`);
        if (!errorMsg.includes("오류: 필수 입력 항목 누락")) {
            throw new Error("TC-06 FAILED: 필수 입력 누락 차단 실패");
        }

        // 2개 소스 정상 입력 (AWX 0대, 작업대상 빈칸 상태)
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
        // 3개 소스 (작업대상에 1대 누락된 목록 입력)
        const sampleTargetMiss = "prd-web-0001\nprd-web-0002\nprd-web-0003\nprd-was-0001"; // prd-db-0001 누락
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

        // 4개 소스 (AWX 1000대 수집 활성화)
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
        // 상세 및 인시던트에 30대 입력 (prd-db-0001 ~ prd-db-0030)
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

        // 명단 펼치기 토글 테스트
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
        // TC-09: MISMATCH/MATCH 별 작업 권장 안내 문구 분기 & 중복 제거 & [펼치기] & 이모티콘 0건 검증
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-09: MISMATCH/MATCH 작업 권장 안내 분기 & 중복 제거 & [펼치기] 검증");
        // 1) 현재 MISMATCH 상태(TC-08 직후)에서 작업 권장 안내 중복 여부(1개만 존재) 및 재확인 안내 문구 검증
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

        // 2) MATCH 상태에서 vim :set paste 권장 문구 출력 검증
        const expectedMatchComment = "인시던트의 작업대상을 복사해서 붙여넣기를 권장합니다.vim [작업대상 파일] -> esc -> :set paste 입력(붙여넣기 모드) -> 붙여넣기";
        const matchGuideCheck = await cdp.evaluate(`
            (() => {
                document.getElementById('awx-vld-btn-reset-awx').click();
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
        // 이모티콘 검사 정규식
        const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;
        const hasEmoji = emojiRegex.test(panelText);

        if (!hasEmoji && !panelText.includes("v2.2")) {
            recordLog('TC-09', 'MISMATCH/MATCH 안내문구 분기 & 중복 제거 & 펼치기', 'PASS', `중복 박스 제거(단일 출력), MISMATCH 시 재확인 문구 / MATCH 시 vim 붙여넣기 문구 분기 및 [펼치기] 정상 확인`);
        } else {
            throw new Error(`TC-09 FAILED: hasEmoji=${hasEmoji}`);
        }

        // -------------------------------------------------------------
        // TC-10: 페이지 내 이동 & 상단 고정(Pin) & 투명도(마우스 진입 100%/이탈 30%/슬라이더/ON-OFF) & 독립 창 전환
        // -------------------------------------------------------------
        console.log("\n>>> Executing TC-10: 이동 & 상단 고정(Pin) & 투명도 조절(ON/OFF, 슬라이더, 마우스 호버) & 독립 창 전환");
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

        // 상단 고정(Pin), 투명도(마우스 진입 100%, 이탈 30%, 슬라이더 50% 조절, ON/OFF 토글), 독립 창 분리 및 자동 접기(미니 모드) 검증
        const pinOpacityDetachValid = await cdp.evaluate(`
            (() => {
                const panel = document.getElementById('awx-validator-panel');
                const btnPin = document.getElementById('awx-vld-btn-pin');
                if (btnPin) btnPin.click();
                const isPinned = window.awxValidatorState.isPinned === true && btnPin.innerText.includes('상단 고정됨');

                // 1. 마우스 진입 시 투명도 100% ('1')
                panel.dispatchEvent(new MouseEvent('mouseenter'));
                const opInside = panel.style.opacity;

                // 2. 마우스 이탈 시 기본 투명도 30% ('0.3')
                panel.dispatchEvent(new MouseEvent('mouseleave'));
                const opOutside30 = panel.style.opacity;

                // 3. 슬라이더 바로 50% 조절 시 ('0.5')
                const range = document.getElementById('awx-vld-range-opacity');
                range.value = '50';
                range.dispatchEvent(new Event('input'));
                const opSlider50 = panel.style.opacity;

                // 다시 30%로 원복
                range.value = '30';
                range.dispatchEvent(new Event('input'));

                // 4. 투명도 기능 OFF 토글 시 마우스 이탈 상태여도 100% ('1')
                const btnOpToggle = document.getElementById('awx-vld-btn-opacity-toggle');
                btnOpToggle.click();
                const opWhenOff = panel.style.opacity;
                const toggleOffText = btnOpToggle.innerText;

                // 다시 ON으로 켜고 마우스 진입 상태로 설정
                btnOpToggle.click();
                panel.dispatchEvent(new MouseEvent('mouseenter'));

                // 5. 독립 창으로 분리 후 마우스 이탈 시 헤더 줄로 자동 축소(미니 모드) 및 마우스 진입 시 펼치기 검증
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
                    // 페이지 내로 복귀 클릭
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

        await cdp.screenshot("C:\\Users\\qazx2\\.gemini\\antigravity\\scratch\\host-cross-checker\\tests\\screenshot_onetake_success.png");

        console.log("\n==================================================================");
        console.log("  [ONE-TAKE SUCCESS] 10개 테스트케이스 전원 100% 연속 통과!");
        console.log("==================================================================");

        return testLogs;
    } finally {
        if (cdp) cdp.close();
        chrome.kill();
    }
}

runOneTakeTests().then(logs => {
    fs.writeFileSync(path.join(__dirname, 'test_results.json'), JSON.stringify(logs, null, 2), 'utf8');
    process.exit(0);
}).catch(err => {
    console.error("\n[ONE-TAKE FAILURE]:", err.message);
    process.exit(1);
});
