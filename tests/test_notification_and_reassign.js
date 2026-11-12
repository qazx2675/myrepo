const http = require('http');
const fs = require('fs');
const path = require('path');
const { spawn } = require('child_process');

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

async function testNotificationAndReassign() {
    console.log("===============================================================================");
    console.log("  >>> Notification Endpoints & Reassignment Sync to Worker Test <<<");
    console.log("===============================================================================");

    const tempDir = path.join(__dirname, '..', '.test_notify_reassign_dir');
    fs.rmSync(tempDir, { recursive: true, force: true });
    fs.mkdirSync(tempDir, { recursive: true });

    const distExe = path.join(__dirname, '..', 'dist', 'host-cross-checker.exe');
    fs.copyFileSync(distExe, path.join(tempDir, 'host-cross-checker.exe'));

    fs.writeFileSync(path.join(tempDir, 'config.json'), JSON.stringify({
        listenPort: 18097,
        sharedDir: tempDir,
        pollIntervalMs: 300
    }, null, 2));

    fs.writeFileSync(path.join(tempDir, 'users.json'), JSON.stringify({
        updatedAt: new Date().toISOString(),
        users: [
            { name: "작업자A", ip: "192.168.1.10", role: "작업자" },
            { name: "확인자B", ip: "192.168.1.20", role: "작업자" },
            { name: "확인자C", ip: "192.168.1.30", role: "작업자" }
        ]
    }, null, 2));

    console.log("[1/5] Spawning Agent (Port: 18097)...");
    const proc = spawn(path.join(tempDir, 'host-cross-checker.exe'), ['-port=18097', '-headless', '-bind-ip=192.168.1.10'], {
        cwd: tempDir,
        stdio: 'pipe'
    });

    await sleep(800);

    const health = await (await fetch('http://127.0.0.1:18097/api/health')).json();
    console.log("✓ Agent healthy:", health);

    try {
        // 2. Test Window Flash & Restore Endpoints
        console.log("[2/5] Testing /api/window/flash and /api/window/restore...");
        const flashRes = await (await fetch('http://127.0.0.1:18097/api/window/flash', { method: 'POST' })).json();
        console.log("✓ /api/window/flash response:", flashRes);
        if (!flashRes.flashed) throw new Error("Expected flashed: true");

        const restoreRes = await (await fetch('http://127.0.0.1:18097/api/window/restore', { method: 'POST' })).json();
        console.log("✓ /api/window/restore response:", restoreRes);
        if (!restoreRes.restored) throw new Error("Expected restored: true");

        // 3. Create Session with Checker B
        console.log("[3/5] Creating Session with Checker B...");
        const createRes = await (await fetch('http://127.0.0.1:18097/api/session/create', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                worker: { name: "작업자A", ip: "192.168.1.10" },
                checker: { name: "확인자B", ip: "192.168.1.20" },
                incidentNo: "INC-20261112",
                hosts: ["srv-01", "srv-02"],
                blindSummary: { totalCount: 2, matchCount: 2 }
            })
        })).json();
        console.log("✓ Session created:", createRes.sessionId, "Checker:", createRes.session?.checker?.name || "확인자B");
        const sid = createRes.sessionId;

        // 4. Reassign to Checker C
        console.log("[4/5] Reassigning session to Checker C...");
        const reassignRes = await (await fetch('http://127.0.0.1:18097/api/session/reassign', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                sessionId: sid,
                newChecker: { name: "확인자C", ip: "192.168.1.30", role: "작업자" }
            })
        })).json();
        console.log("✓ Reassign response:", reassignRes);
        if (reassignRes.checker?.name !== "확인자C") {
            throw new Error("Expected checker name to be 확인자C");
        }

        // 5. Verify session status on disk reflects Checker C
        console.log("[5/5] Checking session status via /api/session/status...");
        const statusRes = await (await fetch(`http://127.0.0.1:18097/api/session/status?sessionId=${sid}&clientIp=192.168.1.10`)).json();
        console.log("✓ Session status retrieved:", statusRes.status, "New Checker:", statusRes.checker?.name);
        if (statusRes.checker?.name !== "확인자C") {
            throw new Error("Expected updated checker to be 확인자C!");
        }

    } finally {
        try {
            await fetch('http://127.0.0.1:18097/api/shutdown', { method: 'POST' });
        } catch (e) {}
        proc.kill();
        await sleep(800);
        try {
            fs.rmSync(tempDir, { recursive: true, force: true });
        } catch (e) {}
    }

    console.log("===============================================================================");
    console.log("  >>> SUCCESS: Notification & Reassign Sync 100% PASS! <<<");
    console.log("===============================================================================");
}

testNotificationAndReassign().catch(err => {
    console.error("TEST FAILED:", err);
    process.exit(1);
});
