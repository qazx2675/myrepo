const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

async function testBuiltBookmarklet() {
    const cwd = 'C:\\Users\\qazx2\\.gemini\\antigravity\\scratch\\host-cross-checker';
    const bmkPath = path.join(cwd, 'dist', 'bookmarklet.txt');
    const bookmarkUrl = fs.readFileSync(bmkPath, 'utf8').trim();
    console.log('Testing dist/bookmarklet.txt length:', bookmarkUrl.length);

    // 1. Verify syntax by simulated decode
    try {
        const decoded = decodeURIComponent(bookmarkUrl.replace(/^javascript:/, ''));
        new Function(decoded);
        console.log('✓ Syntax check: decoded JavaScript compiles without errors.');
    } catch (e) {
        console.error('✗ Syntax check FAIL:', e.message);
        process.exit(1);
    }

    // 2. Launch real Chrome via CDP and navigate to the bookmarklet URL
    const CHROME_PATH = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
    const profileDir = path.join(cwd, '.test_chrome_profile');

    const chrome = spawn(CHROME_PATH, [
        '--remote-debugging-port=9230',
        '--user-data-dir=' + profileDir,
        'http://192.168.0.58:30080/#/inventories/inventory/3/hosts'
    ], { stdio: 'ignore' });

    await new Promise(r => setTimeout(r, 4000));

    try {
        const res = await fetch('http://127.0.0.1:9230/json');
        const targets = await res.json();
        let page = targets.find(t => t.type === 'page' && !t.url.startsWith('chrome-extension') && !t.url.startsWith('chrome://'));
        if (!page) page = targets.find(t => t.type === 'page');

        const ws = new WebSocket(page.webSocketDebuggerUrl);
        await new Promise(r => ws.onopen = r);

        let msgId = 1;
        const callbacks = new Map();
        let exceptionOccurred = null;

        ws.onmessage = (event) => {
            const msg = JSON.parse(event.data);
            if (msg.id && callbacks.has(msg.id)) {
                const cb = callbacks.get(msg.id);
                callbacks.delete(msg.id);
                if (msg.error) cb.reject(msg.error);
                else cb.resolve(msg.result);
            } else if (msg.method === 'Runtime.exceptionThrown') {
                exceptionOccurred = msg.params.exceptionDetails?.text + ' ' + (msg.params.exceptionDetails?.exception?.description || '');
            }
        };

        function send(method, params = {}) {
            return new Promise((resolve, reject) => {
                const id = msgId++;
                callbacks.set(id, { resolve, reject });
                ws.send(JSON.stringify({ id, method, params }));
            });
        }

        await send('Runtime.enable');
        await send('Page.enable');

        console.log('Navigating to dist/bookmarklet.txt in Chrome...');
        await send('Page.navigate', { url: bookmarkUrl });
        await new Promise(r => setTimeout(r, 2000));

        const panelResult = await send('Runtime.evaluate', {
            expression: '!!document.getElementById("awx-validator-panel")'
        });
        const headerTitleResult = await send('Runtime.evaluate', {
            expression: 'document.querySelector("#awx-vld-header span")?.innerText'
        });
        const tabsResult = await send('Runtime.evaluate', {
            expression: 'Array.from(document.querySelectorAll(".awx-vld-tab-btn")).map(b => b.innerText.trim())',
            returnByValue: true
        });

        console.log('✓ Panel created:', panelResult.result?.value);
        console.log('✓ Header title:', headerTitleResult.result?.value);
        console.log('✓ Available tabs:', tabsResult.result?.value);
        console.log('✓ Exception thrown:', exceptionOccurred || 'None');

        ws.close();
        chrome.kill();

        if (panelResult.result?.value && !exceptionOccurred) {
            console.log('\n>>> SUCCESS: Bookmarklet operates 100% normally in Chrome!');
            process.exit(0);
        } else {
            console.error('\n>>> FAIL: Panel not found or exception occurred.');
            process.exit(1);
        }
    } catch (e) {
        console.error('Error:', e.message);
        chrome.kill();
        process.exit(1);
    }
}

testBuiltBookmarklet();
