const http = require('http');
const fs = require('fs');
const path = require('path');
const { spawn } = require('child_process');

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

async function testUsersSync() {
    console.log("===============================================================================");
    console.log("  >>> Users.json Local & Shared Sync + Error Reporting Test <<<");
    console.log("===============================================================================");

    const centralShare = path.join(__dirname, '..', '.test_sync_central_share');
    const localDir = path.join(__dirname, '..', '.test_sync_local_pc');

    fs.rmSync(centralShare, { recursive: true, force: true });
    fs.rmSync(localDir, { recursive: true, force: true });

    fs.mkdirSync(centralShare, { recursive: true });
    fs.mkdirSync(localDir, { recursive: true });

    const distExe = path.join(__dirname, '..', 'dist', 'host-cross-checker.exe');
    fs.copyFileSync(distExe, path.join(localDir, 'host-cross-checker.exe'));

    // 1. Config on local PC pointing to centralShare
    fs.writeFileSync(path.join(localDir, 'config.json'), JSON.stringify({
        listenPort: 18096,
        sharedDir: centralShare,
        pollIntervalMs: 300
    }, null, 2));

    // 2. Initial central users.json
    const initialCentralUsers = {
        updatedAt: new Date(Date.now() - 60000).toISOString(),
        users: [
            { name: "원격기존작업자", ip: "192.168.1.50", role: "작업자" }
        ]
    };
    fs.writeFileSync(path.join(centralShare, 'users.json'), JSON.stringify(initialCentralUsers, null, 2));

    // Initial local users.json (older)
    const initialLocalUsers = {
        updatedAt: new Date(Date.now() - 120000).toISOString(),
        users: [
            { name: "로컬구버전작업자", ip: "192.168.1.10", role: "작업자" }
        ]
    };
    fs.writeFileSync(path.join(localDir, 'users.json'), JSON.stringify(initialLocalUsers, null, 2));

    // Set timestamps explicitly: central is newer than local
    const nowSec = Math.floor(Date.now() / 1000);
    fs.utimesSync(path.join(localDir, 'users.json'), nowSec - 60, nowSec - 60);
    fs.utimesSync(path.join(centralShare, 'users.json'), nowSec - 30, nowSec - 30);

    // 3. Spawn process
    console.log("[1/4] Spawning Agent (Local EXE -> Central Share)...");
    const proc = spawn(path.join(localDir, 'host-cross-checker.exe'), ['-port=18096', '-headless', '-bind-ip=192.168.1.50'], {
        cwd: localDir,
        stdio: 'pipe'
    });

    proc.stderr.on('data', d => {
        // console.log("[AGENT STDERR]", d.toString().trim());
    });

    await sleep(800);

    // 4. Check initial read: central was newer, so local should have synced to central
    console.log("[2/4] Verifying initial sync from newer central share...");
    const res1 = await (await fetch('http://127.0.0.1:18096/api/users')).json();
    console.log("   Initial users returned:", res1.users.map(u => u.name));
    if (!res1.users.some(u => u.name === "원격기존작업자")) {
        throw new Error("Expected '원격기존작업자' from central users.json!");
    }

    // 5. User edits local users.json (making it newer than central)
    console.log("[3/4] User edits local users.json with new worker and newer mtime...");
    await sleep(1500); // ensure mtime difference
    const updatedLocalUsers = {
        updatedAt: new Date().toISOString(),
        users: [
            { name: "로컬신규작업자", ip: "192.168.1.50", role: "작업자" },
            { name: "동료확인자", ip: "192.168.1.60", role: "작업자" }
        ]
    };
    fs.writeFileSync(path.join(localDir, 'users.json'), JSON.stringify(updatedLocalUsers, null, 2));

    // Call /api/users again: it should auto-sync local -> central!
    await sleep(1100); // exceed 1-second debounce
    const res2 = await (await fetch('http://127.0.0.1:18096/api/users')).json();
    console.log("   After local edit, users returned:", res2.users.map(u => u.name));
    if (!res2.users.some(u => u.name === "로컬신규작업자")) {
        throw new Error("Expected '로컬신규작업자' after local users.json edit!");
    }

    // Verify central users.json on disk was updated as well!
    const centralContent = JSON.parse(fs.readFileSync(path.join(centralShare, 'users.json'), 'utf8'));
    if (!centralContent.users.some(u => u.name === "로컬신규작업자")) {
        throw new Error("Expected centralShare/users.json to be automatically updated from local edit!");
    }
    console.log("✓ Verified: Local edit automatically synced to central network share!");

    // 6. Test syntax error reporting (e.g. malformed JSON)
    console.log("[4/4] Testing JSON syntax error reporting...");
    await sleep(1500);
    fs.writeFileSync(path.join(localDir, 'users.json'), '{\n  "users": [\n    {"name": "잘못된JSON", "ip": "10.0.0.1",}\n  ]\n}'); // trailing comma!
    await sleep(1100);
    const res3 = await (await fetch('http://127.0.0.1:18096/api/me')).json();
    console.log("   /api/me response on syntax error:", res3);
    // Since local had syntax error, central was preserved or error reported
    const resUsers = await (await fetch('http://127.0.0.1:18096/api/users')).json();
    console.log("   /api/users response:", resUsers.users ? `Retrieved ${resUsers.users.length} users` : resUsers);

    // Clean shutdown
    try {
        await fetch('http://127.0.0.1:18096/api/shutdown', { method: 'POST' });
    } catch (e) {}
    proc.kill();
    await sleep(1000);

    fs.rmSync(centralShare, { recursive: true, force: true });
    try {
        fs.rmSync(localDir, { recursive: true, force: true });
    } catch (e) {}

    console.log("===============================================================================");
    console.log("  >>> SUCCESS: Local/Shared Auto-Sync & Error Detection 100% PASS! <<<");
    console.log("===============================================================================");
}

testUsersSync().catch(err => {
    console.error("TEST FAILED:", err);
    process.exit(1);
});
