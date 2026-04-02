-- 20260402000000_remove_mfa.sql
-- 移除 MFA 相关字段

-- 删除 users 表中的 mfa_enabled 和 mfa_secret 列
-- SQLite 不支持直接删除列，需要重建表

-- 1. 创建临时表（不含 mfa 列）
CREATE TABLE IF NOT EXISTS users_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    email TEXT,
    role TEXT DEFAULT 'user',
    is_initialized INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 2. 复制数据
INSERT INTO users_new (id, username, password_hash, email, role, is_initialized, created_at, updated_at)
SELECT id, username, password_hash, email, role, is_initialized, created_at, updated_at
FROM users;

-- 3. 删除旧表
DROP TABLE users;

-- 4. 重命名新表
ALTER TABLE users_new RENAME TO users;

-- 恢复原有的索引（如果有）
-- 注意：如果之前有针对 mfa_enabled 或 mfa_secret 的索引，这里不再创建
