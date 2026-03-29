// Package handler 提供 HTTP 请求处理器
package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/auth"
	"github.com/campus/collector/internal/engine"
	"github.com/campus/collector/internal/middleware"
	"github.com/google/uuid"
)

// TaskHandler 任务处理器
type TaskHandler struct {
	db           *sql.DB
	scheduler    *engine.TaskScheduler
	jwtMgr       *auth.JWTManager
	whitelistMgr *middleware.WhitelistManager
	auditLogger  *audit.Logger
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(db *sql.DB, scheduler *engine.TaskScheduler, jwtMgr *auth.JWTManager, whitelistMgr *middleware.WhitelistManager, auditLogger *audit.Logger) *TaskHandler {
	return &TaskHandler{
		db:           db,
		scheduler:    scheduler,
		jwtMgr:       jwtMgr,
		whitelistMgr: whitelistMgr,
		auditLogger:  auditLogger,
	}
}

// BatchTaskRequest 批量任务请求
type BatchTaskRequest struct {
	URLs     []string `json:"urls"`
	Quality  string   `json:"quality,omitempty"`
	Priority int      `json:"priority,omitempty"`
}

// BatchTaskResponse 批量任务响应
type BatchTaskResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message,omitempty"`
	Data    *BatchTaskData `json:"data,omitempty"`
}

// BatchTaskData 批量任务数据
type BatchTaskData struct {
	BatchID string            `json:"batch_id"`
	Total   int               `json:"total"`
	Tasks   []TaskSummaryData `json:"tasks"`
}

// TaskSummaryData 任务摘要数据
type TaskSummaryData struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
}

// BatchProgressResponse 批量任务进度响应
type BatchProgressResponse struct {
	Success bool               `json:"success"`
	Message string             `json:"message,omitempty"`
	Data    *BatchProgressData `json:"data,omitempty"`
}

// BatchProgressData 批量任务进度数据
type BatchProgressData struct {
	BatchID          string             `json:"batch_id"`
	Total            int                `json:"total"`
	Completed        int                `json:"completed"`
	Failed           int                `json:"failed"`
	Cancelled        int                `json:"cancelled"`
	Queued           int                `json:"queued"`
	Downloading      int                `json:"downloading"`
	OverallProgress  float64            `json:"overall_progress"`
	Status           string             `json:"status"`
	Tasks            []TaskProgressData `json:"tasks"`
	CreatedAt        time.Time          `json:"created_at"`
	EstimatedEndTime *time.Time         `json:"estimated_end_time,omitempty"`
}

// TaskProgressData 任务进度数据
type TaskProgressData struct {
	ID           string  `json:"id"`
	URL          string  `json:"url"`
	Status       string  `json:"status"`
	Progress     float64 `json:"progress"`
	Speed        float64 `json:"speed,omitempty"`
	ETA          int     `json:"eta,omitempty"`
	ErrorMessage string  `json:"error_message,omitempty"`
	FilePath     string  `json:"file_path,omitempty"`
}

// TaskCancelResponse 任务取消响应
type TaskCancelResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// URL 验证正则表达式
var urlRegex = regexp.MustCompile(`^https?://[^\s]+$`)

// CreateBatchTask 创建批量任务
// POST /api/v1/tasks/batch
func (h *TaskHandler) CreateBatchTask(w http.ResponseWriter, r *http.Request) {
	// 获取用户信息
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}

	// 解析请求
	var req BatchTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求体")
		return
	}

	// 验证 URL 列表
	if len(req.URLs) == 0 {
		writeError(w, http.StatusBadRequest, "URL 列表不能为空")
		return
	}

	if len(req.URLs) > 100 {
		writeError(w, http.StatusBadRequest, "单次批量任务最多支持 100 个 URL")
		return
	}

	// 验证并清理 URL
	validURLs := make([]string, 0, len(req.URLs))
	for _, url := range req.URLs {
		url = strings.TrimSpace(url)
		if url == "" {
			continue
		}
		if !urlRegex.MatchString(url) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("无效的 URL 格式: %s", url))
			return
		}
		validURLs = append(validURLs, url)
	}

	if len(validURLs) == 0 {
		writeError(w, http.StatusBadRequest, "没有有效的 URL")
		return
	}

	// 生成批量 ID
	batchID := uuid.New().String()

	// 1. 开启事务保证批量操作的原子性
	tx, err := h.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "开启数据库事务失败")
		return
	}
	defer tx.Rollback()

	// 设置默认值
	quality := req.Quality
	if quality == "" {
		quality = "best"
	}

	priority := engine.TaskPriority(req.Priority)
	if priority < engine.PriorityLow || priority > engine.PriorityUrgent {
		priority = engine.PriorityNormal
	}

	// 创建批量任务记录
	_, err = tx.Exec(`
		INSERT INTO batch_tasks (id, user_id, total_count, status, created_at)
		VALUES (?, ?, ?, 'pending', ?)
	`, batchID, claims.UserID, len(validURLs), time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "数据库操作失败")
		return
	}

	// 创建任务列表
	tasks := make([]TaskSummaryData, 0, len(validURLs))
	createdTasks := make([]*engine.Task, 0, len(validURLs))
	for _, url := range validURLs {
		taskID := uuid.New().String()

		// 创建任务对象
		task := &engine.Task{
			ID:       taskID,
			URL:      url,
			Priority: priority,
			Status:   engine.TaskStatusQueued,
			Options: engine.DownloadOptions{
				Quality: quality,
			},
			BatchID:   batchID,
			CreatedAt: time.Now(),
		}

		// 保存到数据库
		_, err := tx.Exec(`
			INSERT INTO tasks (id, user_id, url, status, quality, engine, batch_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, taskID, claims.UserID, url, string(engine.TaskStatusQueued), quality, "", batchID, time.Now())
		if err != nil {
			log.Printf("Failed to insert task %s to database: %v", taskID, err)
			continue
		}

		createdTasks = append(createdTasks, task)
		tasks = append(tasks, TaskSummaryData{
			ID:       taskID,
			URL:      url,
			Status:   string(engine.TaskStatusQueued),
			Priority: int(priority),
		})
	}

	// 2. 提交事务
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "提交任务失败")
		return
	}

	// 3. 事务成功后，将任务加入调度器
	actualStarted := 0
	for _, task := range createdTasks {
		if err := h.scheduler.AddTask(task); err != nil {
			log.Printf("Failed to add task %s to scheduler: %v", task.ID, err)
			continue
		}
		actualStarted++
	}

	// 更新批量任务状态
	batchStatus := "processing"
	if actualStarted == 0 {
		batchStatus = "failed"
	}

	if _, err = h.db.Exec(`UPDATE batch_tasks SET status = ? WHERE id = ?`, batchStatus, batchID); err != nil {
		log.Printf("Failed to update batch status for %s: %v", batchID, err)
	}

	writeJSON(w, http.StatusCreated, BatchTaskResponse{
		Success: true,
		Message: "批量任务创建成功",
		Data: &BatchTaskData{
			BatchID: batchID,
			Total:   len(tasks),
			Tasks:   tasks,
		},
	})
}

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
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("取消任务失败: %v", err))
		return
	}

	// 清理临时文件
	if filePath.Valid && filePath.String != "" {
		if err := cleanupTempFiles(filePath.String); err != nil {
			// 记录错误但不影响取消操作
			fmt.Printf("清理临时文件失败: %v\n", err)
		}
	}

	// 更新数据库状态
	_, err = h.db.Exec(`
		UPDATE tasks SET status = ?, completed_at = ? WHERE id = ?
	`, string(engine.TaskStatusCancelled), time.Now(), taskID)
	if err != nil {
		// 记录错误但不影响响应
		fmt.Printf("更新任务状态失败: %v\n", err)
	}

	// 如果是批量任务的一部分，更新批量任务计数
	var batchID sql.NullString
	err = h.db.QueryRow(`SELECT batch_id FROM tasks WHERE id = ?`, taskID).Scan(&batchID)
	if err == nil && batchID.Valid && batchID.String != "" {
		// 更新批量任务的取消计数
		_, _ = h.db.Exec(`
			UPDATE batch_tasks 
			SET completed_count = completed_count + 1 
			WHERE id = ?
		`, batchID.String)
	}

	writeJSON(w, http.StatusOK, TaskCancelResponse{
		Success: true,
		Message: "任务已取消",
	})
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
	// 获取文件目录
	dir := filepath.Dir(filePath)

	// 获取文件名（不含扩展名）
	baseName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))

	// 清理相关的临时文件
	patterns := []string{
		filePath + ".part",
		filePath + ".temp",
		filepath.Join(dir, baseName+".part*"),
		filepath.Join(dir, baseName+".temp*"),
		filepath.Join(dir, baseName+".f*.part"),
		filepath.Join(dir, baseName+".f*.temp"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			log.Printf("Pattern glob error %s: %v", pattern, err)
			continue
		}
		for _, match := range matches {
			// 忽略不存在错误，并针对 Windows 占用情况添加重试机制
			for i := 0; i < 2; i++ {
				err := os.Remove(match)
				if err == nil || os.IsNotExist(err) {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}

	// 如果原文件存在也删除
	if _, err := os.Stat(filePath); err == nil {
		if err := os.Remove(filePath); err != nil {
			return fmt.Errorf("删除文件失败: %w", err)
		}
	}

	return nil
}

// DownloadFile 处理文件下载请求（支持断点续传）
// GET /api/v1/tasks/:id/download
func (h *TaskHandler) DownloadFile(w http.ResponseWriter, r *http.Request) {
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
	var filePath sql.NullString
	var title sql.NullString
	var status string

	err = h.db.QueryRow(`
		SELECT user_id, file_path, title, status FROM tasks WHERE id = ?
		`, taskID).Scan(&userID, &filePath, &title, &status)
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
		writeError(w, http.StatusForbidden, "无权下载此文件")
		return
	}

	// 检查任务状态
	if status != string(engine.TaskStatusCompleted) {
		writeError(w, http.StatusBadRequest, "任务尚未完成，无法下载")
		return
	}

	// 检查文件路径
	if !filePath.Valid || filePath.String == "" {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}

	// 打开文件
	file, err := os.Open(filePath.String)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "打开文件失败")
		return
	}
	defer file.Close()

	// 获取文件信息
	fileInfo, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取文件信息失败")
		return
	}
	fileSize := fileInfo.Size()

	// 获取文件名（带【教学引用】前缀）
	filename := "download.mp4"
	if title.Valid && title.String != "" {
		filename = sanitizeFilename(title.String) + ".mp4"
	}
	// 添加【教学引用】前缀
	displayFilename := "【教学引用】" + filename

	// 解析 Range 头（断点续传支持）
	rangeHeader := r.Header.Get("Range")
	var start, end int64 = 0, fileSize - 1
	var contentRange string
	var statusCode int

	if rangeHeader != "" {
		// 解析 Range: bytes=start-end
		parts := strings.TrimPrefix(rangeHeader, "bytes=")
		rangeParts := strings.Split(parts, "-")
		if len(rangeParts) == 2 {
			if rangeParts[0] != "" {
				var parseErr error
				start, parseErr = strconv.ParseInt(rangeParts[0], 10, 64)
				if parseErr != nil {
					writeError(w, http.StatusBadRequest, "无效的 Range 格式")
					return
				}
			}
			if rangeParts[1] != "" {
				var parseErr error
				end, parseErr = strconv.ParseInt(rangeParts[1], 10, 64)
				if parseErr != nil {
					writeError(w, http.StatusBadRequest, "无效的 Range 格式")
					return
				}
			}
		}

		// 验证范围
		if start < 0 || start >= fileSize || end < start || end >= fileSize {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		// 设置 Content-Range 头
		contentRange = fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize)
		statusCode = http.StatusPartialContent

		// 定位到起始位置
		if _, err := file.Seek(start, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, "文件定位失败")
			return
		}
	} else {
		// 无 Range 头，从头开始
		statusCode = http.StatusOK
	}

	// 计算内容长度
	contentLength := end - start + 1

	// 根据文件扩展名动态设置 Content-Type
	ext := strings.ToLower(filepath.Ext(filePath.String))
	contentType := "application/octet-stream"
	switch ext {
	case ".mp4", ".m4v":
		contentType = "video/mp4"
	case ".webm":
		contentType = "video/webm"
	case ".mov":
		contentType = "video/quicktime"
	case ".avi":
		contentType = "video/x-msvideo"
	case ".mp3":
		contentType = "audio/mpeg"
	case ".wav":
		contentType = "audio/wav"
	case ".pdf":
		contentType = "application/pdf"
	case ".doc", ".docx":
		contentType = "application/msword"
	case ".txt":
		contentType = "text/plain"
	}

	// 设置响应头
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", displayFilename))
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "public, max-age=31536000")

	if contentRange != "" {
		w.Header().Set("Content-Range", contentRange)
	}

	// 写入状态码
	w.WriteHeader(statusCode)

	// 记录审计日志（开始下载）
	{
		userID := int64(claims.UserID)
		auditLog := &audit.AuditLog{
			UserID:    &userID,
			Action:    audit.ActionDownload,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
			Detail: map[string]interface{}{
				"task_id":   taskID,
				"filename":  displayFilename,
				"file_size": fileSize,
				"range":     rangeHeader,
				"status":    "success",
			},
			CreatedAt: time.Now(),
		}
		if err := h.auditLogger.Log(auditLog); err != nil {
			log.Printf("审计日志记录失败：%v", err)
		}
	}

	// 使用 io.CopyN 精确传输请求的字节数
	_, err = io.CopyN(w, file, contentLength)
	if err != nil {
		log.Printf("文件传输中断：%v", err)
		// 记录审计日志（下载失败）
		userID := int64(claims.UserID)
		failedLog := &audit.AuditLog{
			UserID:    &userID,
			Action:    audit.ActionDownload,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
			Detail: map[string]interface{}{
				"task_id":  taskID,
				"filename": displayFilename,
				"status":   "failed",
				"error":    err.Error(),
			},
			CreatedAt: time.Now(),
		}
		if err := h.auditLogger.Log(failedLog); err != nil {
			log.Printf("审计日志记录失败：%v", err)
		}
	}
}

// sanitizeFilename 清理文件名中的非法字符
func sanitizeFilename(filename string) string {
	// 安全获取文件名（防止路径遍历）
	filename = filepath.Base(filename)

	// 移除扩展名
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		filename = filename[:idx]
	}

	// 替换非法字符（包括 ASCII 和 Unicode 控制字符）
	filename = strings.Map(func(r rune) rune {
		// ASCII 控制字符
		if r < 32 || r == 127 {
			return '_'
		}
		// Windows/Unix 非法字符
		if strings.ContainsRune("<>:\"/\\|？*", r) {
			return '_'
		}
		return r
	}, filename)

	// Windows 保留名称检查
	reservedNames := map[string]bool{
		"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"COM1": true, "COM2": true, "COM3": true, "COM4": true,
		"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
		"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
		"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	}
	upperName := strings.ToUpper(filename)
	if reservedNames[upperName] {
		filename = "_" + filename
	}

	// 限制长度（使用 rune 计数，避免截断 UTF-8）
	runes := []rune(filename)
	if len(runes) > 100 {
		filename = string(runes[:100])
	}

	return strings.TrimSpace(filename)
}
