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
	outputDir    string
	tempDir      string
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(db *sql.DB, scheduler *engine.TaskScheduler, jwtMgr *auth.JWTManager, whitelistMgr *middleware.WhitelistManager, auditLogger *audit.Logger) *TaskHandler {
	// 获取下载目录和临时目录（相对于程序运行目录）
	execDir, err := os.Getwd()
	if err != nil {
		execDir = "."
	}
	outputDir := filepath.Join(execDir, "downloads")
	tempDir := filepath.Join(execDir, "temp")

	// 确保目录存在
	os.MkdirAll(outputDir, 0755)
	os.MkdirAll(tempDir, 0755)

	return &TaskHandler{
		db:           db,
		scheduler:    scheduler,
		jwtMgr:       jwtMgr,
		whitelistMgr: whitelistMgr,
		auditLogger:  auditLogger,
		outputDir:    outputDir,
		tempDir:      tempDir,
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

// CreateTask 创建单个任务
// POST /api/v1/tasks
func (h *TaskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	// 获取用户信息
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}

	// 解析请求
	var req struct {
		URL      string `json:"url"`
		Quality  string `json:"quality,omitempty"`
		Priority int    `json:"priority,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求体")
		return
	}

	// 验证 URL
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "URL 不能为空")
		return
	}

	if !urlRegex.MatchString(req.URL) {
		writeError(w, http.StatusBadRequest, "无效的 URL 格式")
		return
	}

	// 域名白名单校验
	if err := middleware.ValidateURL(h.whitelistMgr, req.URL); err != nil {
		if vErr, ok := err.(*middleware.URLValidationError); ok {
			middleware.WriteURLValidationError(w, vErr)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 设置默认值
	quality := req.Quality
	if quality == "" {
		quality = "best"
	}

	priority := engine.TaskPriority(req.Priority)
	if priority < engine.PriorityLow || priority > engine.PriorityUrgent {
		priority = engine.PriorityNormal
	}

	// 生成任务 ID
	taskID := uuid.New().String()

	// 创建任务对象
	task := &engine.Task{
		ID:       taskID,
		URL:      req.URL,
		Priority: priority,
		Status:   engine.TaskStatusQueued,
		Options: engine.DownloadOptions{
			Quality:   quality,
			OutputDir: h.outputDir,
			Timeout:   time.Duration(3600) * time.Second, // 默认 1 小时超时
		},
		CreatedAt: time.Now(),
	}

	// 保存到数据库
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	_, err := h.db.Exec(`
		INSERT INTO tasks (id, user_id, url, status, quality, engine, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		`, taskID, int64(claims.UserID), req.URL, string(engine.TaskStatusQueued), quality, "", nowStr)
	if err != nil {
		log.Printf("Failed to insert task %s: %v", taskID, err)
		writeError(w, http.StatusInternalServerError, "数据库操作失败")
		return
	}

	// 将任务加入调度器
	if err := h.scheduler.AddTask(task); err != nil {
		log.Printf("Failed to add task %s to scheduler: %v", taskID, err)
		writeError(w, http.StatusInternalServerError, "添加任务失败")
		return
	}

	// 记录审计日志
	{
		userIDVal := int64(claims.UserID)
		h.auditLogger.Log(&audit.AuditLog{
			UserID:    &userIDVal,
			Action:    audit.ActionCreateTask,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
			Detail: map[string]interface{}{
				"url":    req.URL,
				"status": "success",
			},
			CreatedAt: time.Now(),
		})
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"message": "任务创建成功",
		"data": map[string]interface{}{
			"id":     taskID,
			"url":    req.URL,
			"status": string(engine.TaskStatusQueued),
		},
	})
}

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

	// 验证并清理 URL，同时校验白名单
	validURLs := make([]string, 0, len(req.URLs))
	for _, rawURL := range req.URLs {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}
		if !urlRegex.MatchString(rawURL) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("无效的 URL 格式: %s", rawURL))
			return
		}

		// 域名白名单校验
		if err := middleware.ValidateURL(h.whitelistMgr, rawURL); err != nil {
			if vErr, ok := err.(*middleware.URLValidationError); ok {
				middleware.WriteURLValidationError(w, vErr)
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		validURLs = append(validURLs, rawURL)
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
		INSERT INTO batch_tasks (id, user_id, total_count, status)
		VALUES (?, ?, ?, 'pending')
	`, batchID, int64(claims.UserID), len(validURLs))
	if err != nil {
		log.Printf("Failed to insert batch_task %s: %v", batchID, err)
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
				Quality:   quality,
				OutputDir: h.outputDir,
				Timeout:   time.Duration(3600) * time.Second,
			},
			BatchID:   batchID,
			CreatedAt: time.Now(),
		}

		// 保存到数据库
		nowStr := time.Now().Format("2006-01-02 15:04:05")
		_, err := tx.Exec(`
			INSERT INTO tasks (id, user_id, url, status, quality, engine, batch_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, taskID, int64(claims.UserID), url, string(engine.TaskStatusQueued), quality, "", batchID, nowStr)
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

	// 记录审计日志
	{
		userIDVal := int64(claims.UserID)
		h.auditLogger.Log(&audit.AuditLog{
			UserID:    &userIDVal,
			Action:    audit.ActionCreateTask,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
			Detail: map[string]interface{}{
				"batch_id": batchID,
				"count":    len(validURLs),
				"status":   "success",
			},
			CreatedAt: time.Now(),
		})
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

// GetTasks 获取任务列表
// GET /api/v1/tasks
func (h *TaskHandler) GetTasks(w http.ResponseWriter, r *http.Request) {
	// 获取用户信息
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}

	// 解析查询参数
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
			if limit > 1000 {
				limit = 1000
			}
		}
	}

	// 查询用户的任务列表
	rows, err := h.db.Query(`
		SELECT id, url, status, progress, created_at
		FROM tasks
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, claims.UserID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询任务列表失败")
		return
	}
	defer rows.Close()

	type TaskData struct {
		ID        string  `json:"id"`
		URL       string  `json:"url"`
		Status    string  `json:"status"`
		Progress  float64 `json:"progress"`
		CreatedAt string  `json:"created_at"`
	}

	var tasks []TaskData
	for rows.Next() {
		var task TaskData
		var createdAt time.Time
		if err := rows.Scan(&task.ID, &task.URL, &task.Status, &task.Progress, &createdAt); err != nil {
			log.Printf("Failed to scan task: %v", err)
			continue
		}
		task.CreatedAt = createdAt.Format(time.RFC3339)
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务数据失败")
		return
	}

	// 如果任务列表为空，返回空数组而不是 null
	if tasks == nil {
		tasks = []TaskData{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    tasks,
	})
}

// GetTaskStats 获取任务统计
// GET /api/v1/tasks/stats
func (h *TaskHandler) GetTaskStats(w http.ResponseWriter, r *http.Request) {
	// 获取用户信息
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}

	// 查询统计数据 - 使用单独的查询避免 SQLite SUM 返回 NULL
	var totalTasks int
	err := h.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE user_id = ?`, claims.UserID).Scan(&totalTasks)
	if err != nil {
		log.Printf("查询总任务数失败：%v", err)
		writeError(w, http.StatusInternalServerError, "查询总任务数失败")
		return
	}

	var completedTasks int
	err = h.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE user_id = ? AND status = 'completed'`, claims.UserID).Scan(&completedTasks)
	if err != nil {
		log.Printf("查询已完成任务失败：%v", err)
		writeError(w, http.StatusInternalServerError, "查询已完成任务失败")
		return
	}

	var pendingTasks int
	err = h.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE user_id = ? AND status IN ('queued', 'downloading')`, claims.UserID).Scan(&pendingTasks)
	if err != nil {
		log.Printf("查询进行中任务失败：%v", err)
		writeError(w, http.StatusInternalServerError, "查询进行中任务失败")
		return
	}

	var failedTasks int
	err = h.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE user_id = ? AND status = 'failed'`, claims.UserID).Scan(&failedTasks)
	if err != nil {
		log.Printf("查询失败任务失败：%v", err)
		writeError(w, http.StatusInternalServerError, "查询失败任务失败")
		return
	}

	var downloadingTasks int
	err = h.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE user_id = ? AND status = 'downloading'`, claims.UserID).Scan(&downloadingTasks)
	if err != nil {
		log.Printf("查询下载中任务失败：%v", err)
		writeError(w, http.StatusInternalServerError, "查询下载中任务失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]int{
			"totalTasks":       totalTasks,
			"completedTasks":   completedTasks,
			"pendingTasks":     pendingTasks,
			"failedTasks":      failedTasks,
			"downloadingTasks": downloadingTasks,
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
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("取消任务失败：%v", err))
			return
		}

		// 清理临时文件
		if filePath.Valid && filePath.String != "" {
			if err := cleanupTempFiles(filePath.String); err != nil {
				fmt.Printf("清理临时文件失败：%v\n", err)
			}
		}

		// 更新数据库状态
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

		writeJSON(w, http.StatusOK, TaskCancelResponse{
			Success: true,
			Message: "任务已取消",
		})
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

	// 打开文件，如果失败则尝试从 downloads 目录查找
	file, err := os.Open(filePath.String)
	if err != nil {
		// 文件路径可能因编码问题导致乱码，尝试从 downloads 目录查找
		log.Printf("文件路径打开失败：%s, 错误：%v", filePath.String, err)
		
		// 获取任务 ID 对应的文件（通过遍历 downloads 目录）
		fixedPath, findErr := h.findFileInDownloads(taskID, h.outputDir)
		if findErr != nil {
			writeError(w, http.StatusInternalServerError, "打开文件失败")
			return
		}
		
		file, err = os.Open(fixedPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "打开文件失败")
			return
		}
		
		// 更新数据库中的文件路径为正确的路径
		_, _ = h.db.Exec(`UPDATE tasks SET file_path = ? WHERE id = ?`, fixedPath, taskID)
		log.Printf("已修复文件路径：%s -> %s", filePath.String, fixedPath)
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

// findFileInDownloads 在 downloads 目录中查找任务对应的文件
// 用于修复因编码问题导致的文件路径错误
func (h *TaskHandler) findFileInDownloads(taskID, downloadsDir string) (string, error) {
	// 首先查询数据库中的 file_path，用于匹配文件名
	var filePath sql.NullString
	err := h.db.QueryRow(`SELECT file_path FROM tasks WHERE id = ?`, taskID).Scan(&filePath)
	if err != nil {
		return "", fmt.Errorf("查询任务信息失败：%w", err)
	}

	// 遍历 downloads 目录，查找匹配的视频文件
	entries, err := os.ReadDir(downloadsDir)
	if err != nil {
		return "", fmt.Errorf("读取 downloads 目录失败：%w", err)
	}

	// 支持的视频文件扩展名
	videoExts := map[string]bool{
		".mp4":  true,
		".mkv":  true,
		".webm": true,
		".avi":  true,
		".mov":  true,
		".flv":  true,
		".wmv":  true,
		".m4v":  true,
	}

	// 收集所有视频文件
	var videoFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !videoExts[ext] {
			continue
		}
		videoFiles = append(videoFiles, name)
	}

	// 如果数据库中只有单个视频文件，直接返回（最常见场景）
	if len(videoFiles) == 1 {
		return filepath.Join(downloadsDir, videoFiles[0]), nil
	}

	// 如果有多个视频文件，尝试从 file_path 中提取文件名进行匹配
	if filePath.Valid && filePath.String != "" {
		dbBaseName := filepath.Base(filePath.String)
		
		// 提取文件名中的数字和日期特征
		dbNumbers := extractNumbers(dbBaseName)
		
		for _, name := range videoFiles {
			// 尝试直接匹配文件名（处理可能的编码问题）
			if name == dbBaseName {
				return filepath.Join(downloadsDir, name), nil
			}
			
			// 提取实际文件名的数字特征
			realNumbers := extractNumbers(name)
			
			// 如果数字特征高度匹配（相同数字数量>=3 个），认为是同一文件
			if matchNumbers(dbNumbers, realNumbers) {
				return filepath.Join(downloadsDir, name), nil
			}
		}
	}

	// 如果没有任何匹配，返回第一个视频文件（适用于单任务场景）
	if len(videoFiles) > 0 {
		return filepath.Join(downloadsDir, videoFiles[0]), nil
	}

	// 如果没有找到任何视频文件，返回错误
	return "", fmt.Errorf("未找到任务 %s 对应的文件", taskID)
}

// extractNumbers 从字符串中提取连续的数字序列
func extractNumbers(s string) []string {
	var numbers []string
	var current string
	
	for _, r := range s {
		if r >= '0' && r <= '9' {
			current += string(r)
		} else {
			if current != "" {
				numbers = append(numbers, current)
				current = ""
			}
		}
	}
	if current != "" {
		numbers = append(numbers, current)
	}
	return numbers
}

// matchNumbers 比较两个数字序列是否高度匹配
func matchNumbers(nums1, nums2 []string) bool {
	if len(nums1) == 0 || len(nums2) == 0 {
		return false
	}
	
	// 统计相同的数字数量
	matchCount := 0
	for _, n1 := range nums1 {
		for _, n2 := range nums2 {
			if n1 == n2 {
				matchCount++
				break
			}
		}
	}
	
	// 如果至少有 2 个数字匹配，或者匹配了所有数字，认为是同一文件
	minLen := len(nums1)
	if len(nums2) < minLen {
		minLen = len(nums2)
	}
	
	return matchCount >= 2 || matchCount >= minLen
}
