CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    rbac_level    INTEGER NOT NULL CHECK (rbac_level BETWEEN 1 AND 5),
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS credentials (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    name                TEXT NOT NULL UNIQUE,
    vcenter_ip          TEXT NOT NULL,
    vc_id               TEXT NOT NULL,
    encrypted_password  BLOB NOT NULL,
    nonce               BLOB NOT NULL,
    esxi_encrypted_password BLOB,
    esxi_nonce          BLOB,
    created_by          INTEGER NOT NULL REFERENCES users(id),
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS jobs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    phase         TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    credential_id INTEGER NOT NULL REFERENCES credentials(id),
    worklist_file TEXT,
    mapfile       TEXT,
    params        TEXT,
    requested_by  INTEGER NOT NULL REFERENCES users(id),
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS job_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id     INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    line       TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER REFERENCES users(id),
    action     TEXT NOT NULL,
    target     TEXT,
    result     TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_job_logs_job_id ON job_logs(job_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
