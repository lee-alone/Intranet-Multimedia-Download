-- 20260326000000_fix_schema_migrations.sql
-- 修复 schema_migrations 表的 version 字段，添加 NOT NULL 约束

-- SQLite 不支持 ALTER TABLE 修改列约束，需要重建表
-- 创建新表
CREATE TABLE IF NOT EXISTS schema_migrations_new (
    version TEXT NOT NULL PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 迁移数据（如果旧表存在）
INSERT OR IGNORE INTO schema_migrations_new (version, applied_at)
SELECT version, applied_at FROM schema_migrations;

-- 删除旧表
DROP TABLE IF EXISTS schema_migrations;

-- 重命名新表
ALTER TABLE schema_migrations_new RENAME TO schema_migrations;
