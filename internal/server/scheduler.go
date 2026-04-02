package server

import (
	"log"
	"time"

	"github.com/campus/collector/internal/engine"
	"github.com/campus/collector/internal/handler"
)

// setupSchedulerCallbacks 设置调度器回调函数，用于同步数据库和通知客户端
func (s *Server) setupSchedulerCallbacks() {
	if s.scheduler == nil {
		return
	}

	// 任务状态改变回调
	s.scheduler.SetOnTaskUpdate(func(task *engine.Task) {
		// 1. 同步到数据库
		_, err := s.db.Exec(`
			UPDATE tasks
			SET status = ?,
				progress = ?,
				file_path = ?,
				file_size = ?,
				error_message = ?,
				started_at = ?,
				completed_at = ?,
				updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, string(task.Status), task.Progress.Percent, task.FilePath, task.Progress.Total,
			formatError(task.Error), task.StartedAt, task.CompletedAt, task.ID)

		if err != nil {
			log.Printf("Failed to sync task %s status to DB: %v", task.ID, err)
		}

		// 如果是批量任务，更新批量任务进度
		if task.BatchID != "" {
			s.updateBatchProgress(task.BatchID)
		}

		// 2. 发送 WebSocket 通知
		handler.NotifyTaskUpdate(task)
	})

	// 任务进度更新回调
	s.scheduler.SetOnProgressUpdate(func(task *engine.Task) {
		// 进度更新频率较高，一般只发送 WebSocket 通知，不频繁更新数据库
		// 但每隔一定百分比（如 5%）或一定时间可以同步一次数据库

		// 仅发送 WebSocket 通知
		handler.NotifyTaskUpdate(task)
	})
}

// formatError 格式化错误对象为字符串
func formatError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// updateBatchProgress 更新批量任务的完成/失败计数
func (s *Server) updateBatchProgress(batchID string) {
	_, err := s.db.Exec(`
		UPDATE batch_tasks
		SET completed_count = (SELECT COUNT(*) FROM tasks WHERE batch_id = ? AND status = 'completed'),
			failed_count = (SELECT COUNT(*) FROM tasks WHERE batch_id = ? AND status = 'failed'),
			status = CASE
				WHEN (SELECT COUNT(*) FROM tasks WHERE batch_id = ? AND status NOT IN ('completed', 'failed', 'cancelled')) = 0 THEN 'completed'
				ELSE 'processing'
			END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, batchID, batchID, batchID, batchID)

	if err != nil {
		log.Printf("Failed to update batch progress for %s: %v", batchID, err)
	}
}

// logRotateLoop 日志轮转循环（每小时检查一次）
func (s *Server) logRotateLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if err := s.logRotator.Rotate(); err != nil {
			log.Printf("日志轮转失败：%v", err)
		}
	}
}
