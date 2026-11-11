const http = require('http');
const fs = require('fs');
const path = require('path');
const { spawn, execSync } = require('child_process');

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

async function testCleanDeploy() {
    console.log("===============================================================================");
    console.log("  >>> Clean Deployment Test: Only 3 files (exe + config.json + users.json) <<<");
    console.log("===============================================================================");

    // 1. Kill any existing instances
    try { execSync('taskkill /F /IM host-cross-checker.exe', { stdio: 'ignore' }); } catch(e) {}
    try { execSync('taskkill /F /IM cross-checker-agent.exe', { stdio: 'ignore' }); } catch(e) {}
    await sleep(500);

    // 2. Setup an isolated directory with ONLY the 3 files
    const isolatedDir = path.join(__dirname, '..', '.test_clean_deploy_dir');
    fs.rmSync(isolatedDir, { recursive: true, force: true });
    fs.mkdirSync(isolatedDir, { recursive: true });

    const distDir = path.join(__dirname, '..', 'dist');
    fs.copyFileSync(path.join(distDir, 'host-cross-checker.exe'), path.join(isolatedDir, 'host-cross-checker.exe'));
    fs.copyFileSync(path.join(distDir, 'config.json'), path.join(isolatedDir, 'config.json'));
    fs.copyFileSync(path.join(distDir, 'users.json'), path.join(isolatedDir, 'users.json'));

    const dirContents = fs.readdirSync(isolatedDir);
    console.log("[1/4] Files in deployment directory:", dirContents);
    if (dirContents.length !== 3) {
        throw new Error(`Expected exactly 3 files, found: ${dirContents.join(', ')}`);
    }

    // 3. Launch host-cross-checker.exe from another working directory (simulating UNC/desktop launch)
    console.log("[2/4] Spawning host-cross-checker.exe from isolated dir with cwd = tempDir...");
    const exePath = path.join(isolatedDir, 'host-cross-checker.exe');
    const proc = spawn(exePath, ['-port=18095', '-headless'], {
        cwd: path.dirname(isolatedDir), // CWD is deliberately NOT isolatedDir
        stdio: 'pipe'
    });

    proc.stdout.on('data', d => console.log('   [STDOUT]', d.toString().trim()));
    proc.stderr.on('data', d => console.log('   [STDERR]', d.toString().trim()));

    // 4. Wait for server to respond on 18095
    console.log("[3/4] Checking server health on port 18095...");
    let serverOk = false;
    for (let i = 0; i < 30; i++) {
        await sleep(200);
        try {
            const res = await fetch('http://127.0.0.1:18095/api/health');
            if (res.ok) {
                const data = await res.json();
                console.log("✓ Server healthy:", data);
                serverOk = true;
                break;
            }
        } catch(e) {}
    }

    if (!serverOk) {
        try { proc.kill(); } catch(e) {}
        throw new Error("Server failed to respond on port 18095!");
    }

    // Check users endpoint
    const usersRes = await fetch('http://127.0.0.1:18095/api/users');
    const usersData = await usersRes.json();
    console.log("✓ Users loaded from users.json:", usersData.users.length, "users found.");
    if (!usersData.users || usersData.users.length === 0) {
        try { proc.kill(); } catch(e) {}
        throw new Error("users.json was not loaded correctly!");
    }

    // Check sessions directory auto-creation in isolatedDir
    const sessionsDir = path.join(isolatedDir, 'sessions');
    if (!fs.existsSync(sessionsDir)) {
        try { proc.kill(); } catch(e) {}
        throw new Error(`sessions directory was not created inside ${isolatedDir}!`);
    }
    console.log("✓ sessions directory automatically created in isolated directory:", sessionsDir);

    // 5. Test clean shutdown via force bye
    console.log("[4/4] Sending shutdown signal...");
    await fetch('http://127.0.0.1:18095/api/bye?force=true', { method: 'POST' });
    await sleep(400);

    try { proc.kill(); } catch(e) {}
    fs.rmSync(isolatedDir, { recursive: true, force: true });

    console.log("\n===============================================================================");
    console.log("  >>> SUCCESS: 3-File Clean Deployment is 100% verified! <<<");
    console.log("===============================================================================");
}

testCleanDeploy().catch(err => {
    console.error("Clean deploy test FAILED:", err);
    process.exit(1);
});

