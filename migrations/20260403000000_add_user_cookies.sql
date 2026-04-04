-- 20260403000000_add_user_cookies.sql
-- 添加用户 Cookie 管理表

CREATE TABLE IF NOT EXISTS user_cookies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    domain TEXT NOT NULL,          -- 如 bilibili.com, youtube.com
    content TEXT NOT NULL,         -- 解密后的原始内容
    is_shared BOOLEAN DEFAULT 0,   -- 是否允许其他用户共用 (管理员控制)
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(user_id) REFERENCES users(id)
);

-- 确保每个用户每个域名只有一个 Cookie 记录
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_domain ON user_cookies(user_id, domain);

-- 为经常查询的 domain 字段添加索引
CREATE INDEX IF NOT EXISTS idx_cookies_domain ON user_cookies(domain);
