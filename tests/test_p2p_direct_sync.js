const http = require('http');
const fs = require('fs');
const path = require('path');
const { spawn, execSync } = require('child_process');

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

async function testP2PDirectSync() {
    console.log("===============================================================================");
    console.log("  >>> P2P Direct Cross-Machine Simulation Test (PC A <-> PC B) <<<");
    console.log("===============================================================================");

    // Clean up test directories
    const dirA = path.join(__dirname, '..', '.test_p2p_a');
    const dirB = path.join(__dirname, '..', '.test_p2p_b');
    fs.rmSync(dirA, { recursive: true, force: true });
    fs.rmSync(dirB, { recursive: true, force: true });
    fs.mkdirSync(dirA, { recursive: true });
    fs.mkdirSync(dirB, { recursive: true });

    const distExe = path.join(__dirname, '..', 'dist', 'host-cross-checker.exe');
    fs.copyFileSync(distExe, path.join(dirA, 'host-cross-checker.exe'));
    fs.copyFileSync(distExe, path.join(dirB, 'host-cross-checker.exe'));

    // Config for PC A: port 18081, sharedDir: "." (local disk A)
    fs.writeFileSync(path.join(dirA, 'config.json'), JSON.stringify({
        listenPort: 18081,
        sharedDir: "."
    }, null, 2));

    // Config for PC B: port 18082, sharedDir: "." (local disk B)
    fs.writeFileSync(path.join(dirB, 'config.json'), JSON.stringify({
        listenPort: 18082,
        sharedDir: "."
    }, null, 2));

    const users = {
        updatedAt: new Date().toISOString(),
        users: [
            { name: "작업자A", ip: "127.0.0.1", role: "작업자" },
            { name: "확인자B", ip: "127.0.0.1", role: "작업자" }
        ]
    };
    fs.writeFileSync(path.join(dirA, 'users.json'), JSON.stringify(users, null, 2));
    fs.writeFileSync(path.join(dirB, 'users.json'), JSON.stringify(users, null, 2));

    // 1. Spawn PC A (Worker on 18081)
    console.log("[1/6] Spawning Agent A (Worker, Port: 18081)...");
    const procA = spawn(path.join(dirA, 'host-cross-checker.exe'), ['-port=18081', '-headless'], {
        cwd: dirA,
        stdio: 'pipe'
    });

    // 2. Spawn PC B (Checker on 18082)
    console.log("[2/6] Spawning Agent B (Checker, Port: 18082)...");
    const procB = spawn(path.join(dirB, 'host-cross-checker.exe'), ['-port=18082', '-headless'], {
        cwd: dirB,
        stdio: 'pipe'
    });

    await sleep(800);

    // 3. Verify health
    const healthA = await (await fetch('http://127.0.0.1:18081/api/health')).json();
    const healthB = await (await fetch('http://127.0.0.1:18082/api/health')).json();
    console.log("✓ Agent A healthy on port 18081:", healthA);
    console.log("✓ Agent B healthy on port 18082:", healthB);

    try {
        // 4. Worker A creates a session requesting Checker B
        console.log("[3/6] Agent A submits cross-check request destined for Agent B...");
        const sessionPayload = {
            worker: { name: "작업자A", ip: "127.0.0.1" },
            checker: { name: "확인자B", ip: "127.0.0.1" }, // In test, sending to local host
            metadata: { incidentId: "INC-9999", specialNotes: "P2P direct sync test" },
            activeFields: { detail: true, incident: true },
            workerData: {
                detailHosts: ["svr-01", "svr-02"],
                incidentHosts: ["svr-01", "svr-02"],
                targetHosts: ["svr-01", "svr-02"],
                awxHosts: []
            }
        };

        const createRes = await fetch('http://127.0.0.1:18081/api/session/create', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(sessionPayload)
        });
        const createData = await createRes.json();
        console.log("✓ Agent A session created:", createData);
        const sid = createData.sessionId;

        // Simulate Agent A delivering peer notify to Agent B on port 18082
        console.log("[4/6] Sending peer notify from Agent A to Agent B (port 18082)...");
        const notifyRes = await fetch('http://127.0.0.1:18082/api/peer/notify', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                type: "NEW_SESSION",
                sessionId: sid,
                session: Object.assign({}, sessionPayload, { sessionId: sid, status: "PENDING", createdAt: new Date().toISOString() })
            })
        });
        const notifyData = await notifyRes.json();
        console.log("✓ Agent B acknowledged notify:", notifyData);

        // 5. Verify Agent B now has the session saved on Agent B's LOCAL disk (.test_p2p_b)
        console.log("[5/6] Checking Agent B's local session list on port 18082...");
        const listRes = await fetch('http://127.0.0.1:18082/api/session/list');
        const listData = await listRes.json();
        console.log("   Agent B sessions on disk:", listData.sessions.map(s => s.sessionId));

        const found = listData.sessions.find(s => s.sessionId === sid);
        if (!found) {
            throw new Error(`Session ${sid} was NOT saved to Agent B's local disk!`);
        }
        console.log("✓ Session successfully saved to Agent B's isolated local disk!");

        // 6. Agent B approves the session
        console.log("[6/6] Agent B approves session...");
        const decisionPayload = {
            sessionId: sid,
            checkerIp: "127.0.0.1",
            checkerData: {
                detailHosts: ["svr-01", "svr-02"],
                incidentHosts: ["svr-01", "svr-02"],
                targetHosts: ["svr-01", "svr-02"],
                isAllMatch: true,
                decision: "APPROVED",
                decisionComment: "P2P sync approval OK"
            }
        };

        const decRes = await fetch('http://127.0.0.1:18082/api/session/decision', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(decisionPayload)
        });
        const decData = await decRes.json();
        console.log("✓ Agent B decision submitted:", decData);

        // Notify Agent A of decision (port 18081)
        await fetch('http://127.0.0.1:18081/api/peer/update', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                type: "SESSION_DECIDED",
                sessionId: sid,
                status: "APPROVED",
                checkerData: decisionPayload.checkerData
            })
        });

        await sleep(300);

        // Check Agent A status
        const statusRes = await fetch(`http://127.0.0.1:18081/api/session/status?sessionId=${sid}&clientIp=127.0.0.1`);
        const statusData = await statusRes.json();
        console.log("✓ Agent A final status on port 18081:", statusData.status);
        if (statusData.status !== "APPROVED") {
            throw new Error(`Agent A status expected APPROVED, got: ${statusData.status}`);
        }

        console.log("\n===============================================================================");
        console.log("  >>> SUCCESS: P2P Direct Cross-Machine Sync 100% Verified! <<<");
        console.log("===============================================================================");

    } finally {
        try { procA.kill(); } catch(e) {}
        try { procB.kill(); } catch(e) {}
        await sleep(600);
        try { fs.rmSync(dirA, { recursive: true, force: true }); } catch(e) {}
        try { fs.rmSync(dirB, { recursive: true, force: true }); } catch(e) {}
    }
}

testP2PDirectSync().catch(err => {
    console.error("P2P test FAILED:", err);
    process.exit(1);
});
