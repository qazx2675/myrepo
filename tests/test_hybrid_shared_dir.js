const http = require('http');
const fs = require('fs');
const path = require('path');
const { spawn } = require('child_process');

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

async function testHybridSharedDir() {
    console.log("===============================================================================");
    console.log("  >>> Hybrid Test: Local EXEs with Central Network Shared Folder <<<");
    console.log("===============================================================================");

    // 1. Setup paths
    const centralShare = path.join(__dirname, '..', '.test_central_nas_share');
    const localPcA = path.join(__dirname, '..', '.test_local_pc_a');
    const localPcB = path.join(__dirname, '..', '.test_local_pc_b');

    fs.rmSync(centralShare, { recursive: true, force: true });
    fs.rmSync(localPcA, { recursive: true, force: true });
    fs.rmSync(localPcB, { recursive: true, force: true });

    fs.mkdirSync(centralShare, { recursive: true });
    fs.mkdirSync(localPcA, { recursive: true });
    fs.mkdirSync(localPcB, { recursive: true });

    const distExe = path.join(__dirname, '..', 'dist', 'host-cross-checker.exe');
    fs.copyFileSync(distExe, path.join(localPcA, 'host-cross-checker.exe'));
    fs.copyFileSync(distExe, path.join(localPcB, 'host-cross-checker.exe'));

    // Config on PC A: EXE is local, but sharedDir points to centralShare!
    fs.writeFileSync(path.join(localPcA, 'config.json'), JSON.stringify({
        listenPort: 18083,
        sharedDir: centralShare,
        pollIntervalMs: 300
    }, null, 2));

    // Config on PC B: EXE is local, but sharedDir points to centralShare!
    fs.writeFileSync(path.join(localPcB, 'config.json'), JSON.stringify({
        listenPort: 18084,
        sharedDir: centralShare,
        pollIntervalMs: 300
    }, null, 2));

    const users = {
        updatedAt: new Date().toISOString(),
        users: [
            { name: "작업자A", ip: "192.168.10.10", role: "작업자" },
            { name: "확인자B", ip: "192.168.10.20", role: "작업자" }
        ]
    };
    // Put users.json in centralShare (or localPcA, it will sync)
    fs.writeFileSync(path.join(centralShare, 'users.json'), JSON.stringify(users, null, 2));

    // 2. Spawn both processes locally
    console.log("[1/5] Spawning Local Agent A (Port: 18083, SharedDir -> NAS)...");
    const procA = spawn(path.join(localPcA, 'host-cross-checker.exe'), ['-port=18083', '-headless', '-bind-ip=192.168.10.10'], {
        cwd: localPcA,
        stdio: 'pipe'
    });

    console.log("[2/5] Spawning Local Agent B (Port: 18084, SharedDir -> NAS)...");
    const procB = spawn(path.join(localPcB, 'host-cross-checker.exe'), ['-port=18084', '-headless', '-bind-ip=192.168.10.20'], {
        cwd: localPcB,
        stdio: 'pipe'
    });

    await sleep(800);

    const healthA = await (await fetch('http://127.0.0.1:18083/api/health')).json();
    const healthB = await (await fetch('http://127.0.0.1:18084/api/health')).json();
    console.log("✓ Agent A started on PC A:", healthA);
    console.log("✓ Agent B started on PC B:", healthB);

    try {
        // 3. Worker A submits session (requesting Checker B at 192.168.10.20, which is firewalled/unreachable directly)
        console.log("[3/5] Worker A submits session to local agent (saves to central NAS share)...");
        const sessionPayload = {
            worker: { name: "작업자A", ip: "192.168.10.10" },
            checker: { name: "확인자B", ip: "192.168.10.20" },
            metadata: { incidentId: "INC-SHARED-001", specialNotes: "Shared Folder Hybrid Test" },
            activeFields: { detail: true, incident: true },
            workerData: {
                detailHosts: ["host-alpha-01", "host-alpha-02"],
                incidentHosts: ["host-alpha-01", "host-alpha-02"],
                targetHosts: ["host-alpha-01", "host-alpha-02"],
                awxHosts: []
            }
        };

        const createRes = await fetch('http://127.0.0.1:18083/api/session/create', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(sessionPayload)
        });
        const createData = await createRes.json();
        console.log("✓ Session created by Agent A:", createData);
        const sid = createData.sessionId;

        // Verify the file was written to centralShare/sessions/
        const nasFilePath = path.join(centralShare, 'sessions', sid + '.json');
        if (!fs.existsSync(nasFilePath)) {
            throw new Error(`Session file was not found in central NAS share: ${nasFilePath}`);
        }
        console.log("✓ Confirmed: Session file saved on central shared folder:", nasFilePath);

        // 4. Wait for Agent B's background polling (300ms) to detect the new file on centralShare
        console.log("[4/5] Waiting for Agent B's shared folder poller to detect the new session...");
        let foundOnB = false;
        for (let i = 0; i < 20; i++) {
            await sleep(250);
            const listRes = await fetch('http://127.0.0.1:18084/api/session/list?clientIp=192.168.10.20');
            const listData = await listRes.json();
            const s = (listData.sessions || []).find(item => item.sessionId === sid);
            if (s) {
                console.log(`✓ Agent B detected session via shared folder polling on try ${i+1}:`, s.sessionId, `(Status: ${s.status})`);
                foundOnB = true;
                break;
            }
        }

        if (!foundOnB) {
            throw new Error("Agent B failed to detect session on central shared folder via polling!");
        }

        // 5. Checker B approves the session on centralShare
        console.log("[5/5] Checker B approves the session on central share...");
        const decisionPayload = {
            sessionId: sid,
            checkerIp: "192.168.10.20",
            checkerData: {
                detailHosts: ["host-alpha-01", "host-alpha-02"],
                incidentHosts: ["host-alpha-01", "host-alpha-02"],
                targetHosts: ["host-alpha-01", "host-alpha-02"],
                isAllMatch: true,
                decision: "APPROVED",
                decisionComment: "중앙 공유폴더 연동 승인 완료"
            }
        };

        const decRes = await fetch('http://127.0.0.1:18084/api/session/decision', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(decisionPayload)
        });
        const decData = await decRes.json();
        console.log("✓ Checker B decision recorded:", decData);

        // Wait for Agent A's background polling to detect the approval on centralShare
        console.log("   Waiting for Agent A's shared folder poller to detect the approval...");
        let approvedOnA = false;
        for (let i = 0; i < 20; i++) {
            await sleep(250);
            const statusRes = await fetch(`http://127.0.0.1:18083/api/session/status?sessionId=${sid}&clientIp=192.168.10.10`);
            const statusData = await statusRes.json();
            if (statusData.status === "APPROVED") {
                console.log(`✓ Agent A confirmed APPROVED status via shared folder polling on try ${i+1}!`);
                approvedOnA = true;
                break;
            }
        }

        if (!approvedOnA) {
            throw new Error("Agent A failed to detect APPROVED status on central shared folder!");
        }

        console.log("\n===============================================================================");
        console.log("  >>> SUCCESS: Local EXE + Central Shared Folder Hybrid 100% PASS! <<<");
        console.log("===============================================================================");

    } finally {
        try { procA.kill(); } catch(e) {}
        try { procB.kill(); } catch(e) {}
        await sleep(500);
        try { fs.rmSync(centralShare, { recursive: true, force: true }); } catch(e) {}
        try { fs.rmSync(localPcA, { recursive: true, force: true }); } catch(e) {}
        try { fs.rmSync(localPcB, { recursive: true, force: true }); } catch(e) {}
    }
}

testHybridSharedDir().catch(err => {
    console.error("Hybrid test FAILED:", err);
    process.exit(1);
});

