-- 20260329000000_add_sso_and_agreement.sql
-- W4D16: 添加 SSO 认证和用户协议相关字段

-- 1. 添加 SSO 相关字段到 users 表
ALTER TABLE users ADD COLUMN sso_provider TEXT DEFAULT NULL;
ALTER TABLE users ADD COLUMN sso_user_id TEXT DEFAULT NULL;
ALTER TABLE users ADD COLUMN sso_email TEXT;
ALTER TABLE users ADD COLUMN last_login_at DATETIME;
ALTER TABLE users ADD COLUMN last_login_provider TEXT;

-- 2. 添加用户协议版本字段到 system_config
-- 当前协议版本，格式：major.minor (例如：1.0)
INSERT OR REPLACE INTO system_config (key, value) VALUES ('agreement_version', '1.0');

-- 3. 创建用户协议同意记录表
CREATE TABLE IF NOT EXISTS user_agreements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    agreement_version TEXT NOT NULL,
    agreed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    ip_address TEXT,
    user_agent TEXT,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- 4. 创建索引
CREATE INDEX IF NOT EXISTS idx_user_agreements_user_id ON user_agreements(user_id);
CREATE INDEX IF NOT EXISTS idx_user_agreements_agreed_at ON user_agreements(agreed_at);
CREATE INDEX IF NOT EXISTS idx_users_sso_provider ON users(sso_provider);

-- 5. 创建 SSO 会话表（用于存储临时 SSO 登录状态）
CREATE TABLE IF NOT EXISTS sso_sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER,
    sso_provider TEXT NOT NULL,
    sso_user_id TEXT,
    sso_email TEXT,
    sso_username TEXT,
    state TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_sso_sessions_state ON sso_sessions(state);
CREATE INDEX IF NOT EXISTS idx_sso_sessions_expires_at ON sso_sessions(expires_at);
