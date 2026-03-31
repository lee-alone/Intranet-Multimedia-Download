-- 20260331000000_add_updated_at_to_tasks.sql
-- 添加 updated_at 字段到 tasks 表

-- 添加 updated_at 字段
ALTER TABLE tasks ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP;

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_tasks_updated_at ON tasks(updated_at);
