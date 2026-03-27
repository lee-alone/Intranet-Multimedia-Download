-- 20260326000000_add_auth_type.sql
-- 添加用户认证类型字段

-- 添加 auth_type 字段，用于区分本地用户和 LDAP 用户
ALTER TABLE users ADD COLUMN auth_type TEXT DEFAULT 'local';

-- 更新现有用户为本地用户
UPDATE users SET auth_type = 'local' WHERE auth_type IS NULL;
