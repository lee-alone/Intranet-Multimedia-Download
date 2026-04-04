-- 20260403000000_add_use_cookies_to_tasks.sql
-- 为 tasks 表添加 use_cookies 字段，用于记录任务是否开启了 Cookie

ALTER TABLE tasks ADD COLUMN use_cookies INTEGER DEFAULT 0;

-- 创建索引以加速查询
CREATE INDEX IF NOT EXISTS idx_tasks_use_cookies ON tasks(use_cookies);
