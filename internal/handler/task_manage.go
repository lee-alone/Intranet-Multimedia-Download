// Package handler 提供 HTTP 请求处理器
package handler

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/engine"
)

// GetBatchProgress 获取批量任务进度
// GET /api/v1/tasks/batch/{batch_id}
func (h *TaskHandler) GetBatchProgress(w http.ResponseWriter, r *http.Request) {
	// 获取用户信息
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}

	// 从 URL 路径中提取 batch_id
	// 路径格式：/api/v1/tasks/batch/{batch_id}
	// parts: [0]=api, [1]=v1, [2]=tasks, [3]=batch, [4]={batch_id}
	batchID, err := h.extractIDFromPath(r.URL.Path, 4)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求路径或 ID")
		return
	}

	if batchID == "" {
		writeError(w, http.StatusBadRequest, "批量任务 ID 不能为空")
		return
	}

	// 查询批量任务信息
	var dbBatchID string
	var userID int64
	var totalCount int
	var completedCount int
	var failedCount int
	var status string
	var createdAt time.Time

	err = h.db.QueryRow(`
		SELECT id, user_id, total_count, completed_count, failed_count, status, created_at
		FROM batch_tasks WHERE id = ?
	`, batchID).Scan(&dbBatchID, &userID, &totalCount, &completedCount, &failedCount, &status, &createdAt)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询批量任务失败")
		return
	}

	// 验证用户权限
	if int(userID) != claims.UserID && claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "无权访问此批量任务")
		return
	}

	// 查询批量任务下的所有子任务
	rows, err := h.db.Query(`
		SELECT id, url, status, progress, error_message, file_path
		FROM tasks WHERE batch_id = ?
	`, batchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询子任务失败")
		return
	}
	defer rows.Close()

	tasks := make([]TaskProgressData, 0)
	completed := 0
	failed := 0
	cancelled := 0
	queued := 0
	downloading := 0

	for rows.Next() {
		var taskID, url, taskStatus string
		var progress int
		var errorMsg, filePath sql.NullString

		if err := rows.Scan(&taskID, &url, &taskStatus, &progress, &errorMsg, &filePath); err != nil {
			continue
		}

		// 性能优化：仅对非终态任务查询调度器，减少锁竞争
		if !engine.TaskStatus(taskStatus).IsTerminal() {
			if task, err := h.scheduler.GetTask(taskID); err == nil {
				progress = int(task.GetProgress().Percent)
			}
		}

		taskData := TaskProgressData{
			ID:       taskID,
			URL:      url,
			Status:   taskStatus,
			Progress: float64(progress),
		}

		if errorMsg.Valid {
			taskData.ErrorMessage = errorMsg.String
		}
		if filePath.Valid {
			taskData.FilePath = filePath.String
		}

		// 统计各状态数量
		switch engine.TaskStatus(taskStatus) {
		case engine.TaskStatusCompleted:
			completed++
		case engine.TaskStatusFailed:
			failed++
		case engine.TaskStatusCancelled:
			cancelled++
		case engine.TaskStatusQueued:
			queued++
		case engine.TaskStatusDownloading, engine.TaskStatusMerging:
			downloading++
		}

		tasks = append(tasks, taskData)
	}

	// 计算整体进度
	total := len(tasks)
	overallProgress := 0.0
	if total > 0 {
		overallProgress = float64(completed+failed+cancelled) / float64(total) * 100
	}

	// 计算预计完成时间
	var estimatedEndTime *time.Time
	if downloading > 0 && overallProgress > 5.0 {
		// 基于当前进度估算剩余时间
		elapsed := time.Since(createdAt)
		if overallProgress > 0 && overallProgress < 100 {
			estimatedTotal := elapsed.Seconds() * 100 / overallProgress
			remaining := estimatedTotal - elapsed.Seconds()
			if remaining > 0 {
				eta := createdAt.Add(time.Duration(estimatedTotal) * time.Second)
				estimatedEndTime = &eta
			}
		}
	}

	// 确定批量任务状态
	batchStatus := status
	if completed+failed+cancelled == total {
		if failed > 0 || cancelled > 0 {
			if completed == 0 {
				batchStatus = "failed"
			} else {
				batchStatus = "partial"
			}
		} else {
			batchStatus = "completed"
		}
	}

	writeJSON(w, http.StatusOK, BatchProgressResponse{
		Success: true,
		Data: &BatchProgressData{
			BatchID:          batchID,
			Total:            total,
			Completed:        completed,
			Failed:           failed,
			Cancelled:        cancelled,
			Queued:           queued,
			Downloading:      downloading,
			OverallProgress:  overallProgress,
			Status:           batchStatus,
			Tasks:            tasks,
			CreatedAt:        createdAt,
			EstimatedEndTime: estimatedEndTime,
		},
	})
}

// CancelTask 取消任务
// DELETE /api/v1/tasks/{id}
func (h *TaskHandler) CancelTask(w http.ResponseWriter, r *http.Request) {
	// 获取用户信息
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}

	// 从 URL 路径中提取 task_id
	// 路径格式：/api/v1/tasks/{task_id}
	// parts: [0]=api, [1]=v1, [2]=tasks, [3]={task_id}
	taskID, err := h.extractIDFromPath(r.URL.Path, 3)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求路径或 ID")
		return
	}

	if taskID == "" {
		writeError(w, http.StatusBadRequest, "任务 ID 不能为空")
		return
	}

	// 查询任务信息
	var userID int64
	var url, status string
	var filePath sql.NullString

	err = h.db.QueryRow(`
		SELECT user_id, url, status, file_path FROM tasks WHERE id = ?
	`, taskID).Scan(&userID, &url, &status, &filePath)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询任务失败")
		return
	}

	// 验证用户权限
	if int(userID) != claims.UserID && claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "无权取消此任务")
		return
	}

	// 检查任务状态
	taskStatus := engine.TaskStatus(status)
	if taskStatus.IsTerminal() {
		writeError(w, http.StatusBadRequest, "任务已完成/失败/已取消，无法取消")
		return
	}

	// 从调度器取消任务
	if err := h.scheduler.CancelTask(taskID); err != nil {
		// 如果错误仅仅是任务不在调度器内存中，我们记录一下但不作为 500 错误返回
		if strings.Contains(err.Error(), "任务不存在") {
			fmt.Printf("警告：尝试取消的任务 %s 不在活跃队列中，将直接更新数据库状态\n", taskID)
		} else {
			// 其他真正的系统错误（如状态转换非法）才返回 500
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("取消任务失败：%v", err))
			return
		}
	}

	// 异步清理临时文件，避免阻塞 API 响应
	// 即使 filePath 为空，也利用 taskID 构造路径进行"盲扫"清理
	targetPath := ""
	if filePath.Valid && filePath.String != "" {
		targetPath = filePath.String
	} else {
		targetPath = filepath.Join(h.outputDir, taskID+".mp4")
	}
	go func(p string) {
		time.Sleep(1 * time.Second) // 适当延长等待，确保进程句柄完全释放
		if err := cleanupTempFiles(p); err != nil {
			fmt.Printf("清理临时文件失败：%v\n", err)
		}
	}(targetPath)

	// 立即返回响应
	writeJSON(w, http.StatusOK, TaskCancelResponse{
		Success: true,
		Message: "任务已取消",
	})

	// 异步更新数据库状态
	go func() {
		_, err = h.db.Exec(`
		UPDATE tasks SET status = ?, completed_at = ? WHERE id = ?
	`, string(engine.TaskStatusCancelled), time.Now(), taskID)
		if err != nil {
			fmt.Printf("更新任务状态失败：%v\n", err)
		}

		// 如果是批量任务的一部分，更新批量任务计数
		var batchID sql.NullString
		err = h.db.QueryRow(`SELECT batch_id FROM tasks WHERE id = ?`, taskID).Scan(&batchID)
		if err == nil && batchID.Valid && batchID.String != "" {
			_, _ = h.db.Exec(`
			UPDATE batch_tasks
			SET completed_count = completed_count + 1
			WHERE id = ?
		`, batchID.String)
		}
	}()
}

// DeleteTask 删除任务（从数据库中删除已完成/失败/已取消的任务记录）
// DELETE /api/v1/tasks/{id}/delete
func (h *TaskHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	// 获取用户信息
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}

	// 从 URL 路径中提取 task_id
	taskID, err := h.extractIDFromPath(r.URL.Path, 3)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求路径或 ID")
		return
	}

	if taskID == "" {
		writeError(w, http.StatusBadRequest, "任务 ID 不能为空")
		return
	}

	// 查询任务信息
	var userID int64
	var status string
	var filePath sql.NullString

	err = h.db.QueryRow(`
	SELECT user_id, status, file_path FROM tasks WHERE id = ?
	`, taskID).Scan(&userID, &status, &filePath)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询任务失败")
		return
	}

	// 验证用户权限
	if int(userID) != claims.UserID && claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "无权删除此任务")
		return
	}

	// 检查任务状态 - 只有已完成/失败/已取消的任务才能删除
	taskStatus := engine.TaskStatus(status)
	if !taskStatus.IsTerminal() {
		writeError(w, http.StatusBadRequest, "只能删除已完成/失败/已取消的任务")
		return
	}

	// 清理相关文件
	if filePath.Valid && filePath.String != "" {
		if err := cleanupTempFiles(filePath.String); err != nil {
			fmt.Printf("清理文件失败：%v\n", err)
		}
	}

	// 从数据库中删除任务记录
	_, err = h.db.Exec(`DELETE FROM tasks WHERE id = ?`, taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除任务失败")
		return
	}

	// 如果是批量任务的一部分，更新批量任务计数
	var batchID sql.NullString
	err = h.db.QueryRow(`SELECT batch_id FROM tasks WHERE id = ?`, taskID).Scan(&batchID)
	if err == nil && batchID.Valid && batchID.String != "" {
		// 更新批量任务的计数（删除时减少总数）
		_, _ = h.db.Exec(`
		UPDATE batch_tasks
		SET total_count = total_count - 1
		WHERE id = ?
		`, batchID.String)
	}

	// 记录审计日志
	userIDVal := int64(claims.UserID)
	h.auditLogger.Log(&audit.AuditLog{
		UserID:    &userIDVal,
		Action:    audit.ActionDeleteTask,
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
		Detail: map[string]interface{}{
			"task_id": taskID,
			"status":  "success",
		},
		CreatedAt: time.Now(),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "任务已删除",
	})
}

// CancelOrDeleteTask 根据任务状态自动判断是取消还是删除任务
// DELETE /api/v1/tasks/{id}
// - 如果任务是进行中的 (queued/downloading/merging)，则取消任务
// - 如果任务是终态的 (completed/failed/cancelled)，则删除任务
func (h *TaskHandler) CancelOrDeleteTask(w http.ResponseWriter, r *http.Request) {
	// 获取用户信息
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}

	// 从 URL 路径中提取 task_id
	taskID, err := h.extractIDFromPath(r.URL.Path, 3)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求路径或 ID")
		return
	}

	if taskID == "" {
		writeError(w, http.StatusBadRequest, "任务 ID 不能为空")
		return
	}

	// 查询任务信息
	var userID int64
	var status string
	var filePath sql.NullString

	err = h.db.QueryRow(`
	SELECT user_id, status, file_path FROM tasks WHERE id = ?
	`, taskID).Scan(&userID, &status, &filePath)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询任务失败")
		return
	}

	// 验证用户权限
	if int(userID) != claims.UserID && claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "无权操作此任务")
		return
	}

	// 根据任务状态决定是取消还是删除
	taskStatus := engine.TaskStatus(status)
	if taskStatus.IsTerminal() {
		// 终态任务：删除
		// 清理相关文件
		if filePath.Valid && filePath.String != "" {
			if err := cleanupTempFiles(filePath.String); err != nil {
				fmt.Printf("清理文件失败：%v\n", err)
			}
		}

		// 从数据库中删除任务记录
		_, err = h.db.Exec(`DELETE FROM tasks WHERE id = ?`, taskID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "删除任务失败")
			return
		}

		// 如果是批量任务的一部分，更新批量任务计数
		var batchID sql.NullString
		err = h.db.QueryRow(`SELECT batch_id FROM tasks WHERE id = ?`, taskID).Scan(&batchID)
		if err == nil && batchID.Valid && batchID.String != "" {
			_, _ = h.db.Exec(`
			UPDATE batch_tasks
			SET total_count = total_count - 1
			WHERE id = ?
			`, batchID.String)
		}

		// 记录审计日志
		userIDVal := int64(claims.UserID)
		h.auditLogger.Log(&audit.AuditLog{
			UserID:    &userIDVal,
			Action:    audit.ActionDeleteTask,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
			Detail: map[string]interface{}{
				"task_id": taskID,
				"status":  "success",
			},
			CreatedAt: time.Now(),
		})

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "任务已删除",
		})
	} else {
		// 非终态任务：取消
		// 从调度器取消任务
		if err := h.scheduler.CancelTask(taskID); err != nil {
			// 如果错误仅仅是任务不在调度器内存中，我们记录一下但不作为 500 错误返回
			if strings.Contains(err.Error(), "任务不存在") {
				fmt.Printf("警告：尝试取消的任务 %s 不在活跃队列中，将直接更新数据库状态\n", taskID)
			} else {
				// 其他真正的系统错误（如状态转换非法）才返回 500
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("取消任务失败：%v", err))
				return
			}
		}

		// 异步清理临时文件，避免阻塞 API 响应
		// 即使 filePath 为空，也利用 taskID 构造路径进行"盲扫"清理
		targetPath := ""
		if filePath.Valid && filePath.String != "" {
			targetPath = filePath.String
		} else {
			targetPath = filepath.Join(h.outputDir, taskID+".mp4")
		}
		go func(p string) {
			time.Sleep(1 * time.Second) // 适当延长等待，确保进程句柄完全释放
			if err := cleanupTempFiles(p); err != nil {
				fmt.Printf("清理临时文件失败：%v\n", err)
			}
		}(targetPath)

		// 立即返回响应
		writeJSON(w, http.StatusOK, TaskCancelResponse{
			Success: true,
			Message: "任务已取消",
		})

		// 异步更新数据库状态
		go func() {
			_, err = h.db.Exec(`
			UPDATE tasks SET status = ?, completed_at = ? WHERE id = ?
			`, string(engine.TaskStatusCancelled), time.Now(), taskID)
			if err != nil {
				fmt.Printf("更新任务状态失败：%v\n", err)
			}

			// 如果是批量任务的一部分，更新批量任务计数
			var batchID sql.NullString
			err = h.db.QueryRow(`SELECT batch_id FROM tasks WHERE id = ?`, taskID).Scan(&batchID)
			if err == nil && batchID.Valid && batchID.String != "" {
				_, _ = h.db.Exec(`
				UPDATE batch_tasks
				SET completed_count = completed_count + 1
				WHERE id = ?
				`, batchID.String)
			}
		}()
	}
}

// extractIDFromPath 从 URL 路径中提取指定位置的 ID
func (h *TaskHandler) extractIDFromPath(path string, index int) (string, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if index >= len(parts) {
		return "", fmt.Errorf("index out of range")
	}
	return parts[index], nil
}

// cleanupTempFiles 清理临时文件
func cleanupTempFiles(filePath string) error {
	if filePath == "" {
		return nil
	}

	// 获取文件目录
	dir := filepath.Dir(filePath)

	// 获取文件名（不含扩展名）
	baseName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))

	// 清理相关的临时文件 - 增强模式匹配
	patterns := []string{
		// 原文件及常见变体
		filePath,
		filePath + ".part",
		filePath + ".temp",
		filePath + ".ytdl",
		filePath + ".download",
		// yt-dlp 分片文件 (fxxx.mp4.part, fxxx.webm.part 等)
		filepath.Join(dir, baseName+".part*"),
		filepath.Join(dir, baseName+".ytdl*"),
		filepath.Join(dir, baseName+".temp*"),
		filepath.Join(dir, baseName+".download*"),
		// yt-dlp 格式特定分片
		filepath.Join(dir, baseName+".f*.part"),
		filepath.Join(dir, baseName+".f*.temp"),
		filepath.Join(dir, baseName+".f*.mp4.part"),
		filepath.Join(dir, baseName+".f*.webm.part"),
		// lux/annie 临时文件
		filepath.Join(dir, baseName+".tmp"),
		filepath.Join(dir, baseName+".tmp*"),
		// 其他常见临时文件
		filepath.Join(dir, baseName+".aria2"),
		filepath.Join(dir, baseName+".aria2*"),
		// 合并前的音视频分离文件
		filepath.Join(dir, baseName+".f*.mp4"),
		filepath.Join(dir, baseName+".f*.webm"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			log.Printf("Pattern glob error %s: %v", pattern, err)
			continue
		}
		for _, match := range matches {
			// 针对 Windows 占用情况添加重试机制
			for i := 0; i < 3; i++ {
				err := os.Remove(match)
				if err == nil || os.IsNotExist(err) {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
		}
	}

	return nil
}

// RetryTask 重新执行任务（克隆旧任务为新任务）
// POST /api/v1/tasks/{id}/retry
func (h *TaskHandler) RetryTask(w http.ResponseWriter, r *http.Request) {
	// 获取用户信息
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}

	// 从 URL 路径中提取 task_id
	taskID, err := h.extractIDFromPath(r.URL.Path, 3)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求路径或 ID")
		return
	}

	if taskID == "" {
		writeError(w, http.StatusBadRequest, "任务 ID 不能为空")
		return
	}

	// 查询旧任务信息
	var userID int64
	var url, status string
	var quality, engineType, batchID sql.NullString

	err = h.db.QueryRow(`
		SELECT user_id, url, status, quality, engine, batch_id FROM tasks WHERE id = ?
	`, taskID).Scan(&userID, &url, &status, &quality, &engineType, &batchID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询任务失败")
		return
	}

	// 验证用户权限
	if int(userID) != claims.UserID && claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "无权重试此任务")
		return
	}

	// 检查任务状态 - 仅允许失败或已取消的任务重试
	taskStatus := engine.TaskStatus(status)
	if taskStatus != engine.TaskStatusFailed && taskStatus != engine.TaskStatusCancelled && taskStatus != engine.TaskStatusCompleted {
		writeError(w, http.StatusBadRequest, "只能重试失败、已取消或已完成的任务")
		return
	}

	// 生成新任务 ID
	newTaskID := generateTaskID()

	// 克隆任务到数据库
	qualityVal := ""
	if quality.Valid {
		qualityVal = quality.String
	}
	engineVal := ""
	if engineType.Valid {
		engineVal = engineType.String
	}
	batchIDVal := sql.NullString{}
	if batchID.Valid {
		batchIDVal = batchID
	}

	_, err = h.db.Exec(`
		INSERT INTO tasks (id, user_id, url, status, quality, engine, batch_id, progress, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)
	`, newTaskID, userID, url, string(engine.TaskStatusQueued), qualityVal, engineVal, batchIDVal, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建新任务失败")
		return
	}

	// 如果是批量任务的一部分，更新 batch_tasks 的 total_count
	if batchIDVal.Valid && batchIDVal.String != "" {
		_, _ = h.db.Exec(`
			UPDATE batch_tasks SET total_count = total_count + 1 WHERE id = ?
		`, batchIDVal.String)
	}

	// 构造新任务加入调度器
	opts := engine.DownloadOptions{
		OutputDir: h.outputDir,
		TaskID:    newTaskID,
	}
	if qualityVal != "" {
		opts.Quality = qualityVal
	}

	newTask := &engine.Task{
		ID:        newTaskID,
		URL:       url,
		Options:   opts,
		Status:    engine.TaskStatusQueued,
		CreatedAt: time.Now(),
	}

	if err := h.scheduler.AddTask(newTask); err != nil {
		// 如果加入调度器失败，回滚数据库记录
		_, _ = h.db.Exec(`DELETE FROM tasks WHERE id = ?`, newTaskID)
		writeError(w, http.StatusInternalServerError, "加入任务队列失败")
		return
	}

	// 记录审计日志
	userIDVal := int64(claims.UserID)
	h.auditLogger.Log(&audit.AuditLog{
		UserID:    &userIDVal,
		Action:    audit.ActionRetryTask,
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
		Detail: map[string]interface{}{
			"old_task_id": taskID,
			"new_task_id": newTaskID,
			"url":         url,
		},
		CreatedAt: time.Now(),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"message":     "任务已重新加入队列",
		"new_task_id": newTaskID,
		"old_task_id": taskID,
	})
}

// generateTaskID 生成唯一任务 ID
func generateTaskID() string {
	return fmt.Sprintf("task_%d_%d", time.Now().UnixNano(), time.Now().Nanosecond())
}
