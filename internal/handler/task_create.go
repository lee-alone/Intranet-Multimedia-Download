// Package handler 提供 HTTP 请求处理器
package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/engine"
	"github.com/campus/collector/internal/middleware"
	"github.com/google/uuid"
)

// URL 验证正则表达式
var urlRegex = regexp.MustCompile(`^https?://[^\s]+$`)

// CreateTaskRequest 创建任务请求
type CreateTaskRequest struct {
	URL        string `json:"url"`
	Quality    string `json:"quality,omitempty"`
	Priority   int    `json:"priority,omitempty"`
	UseCookies bool   `json:"use_cookies,omitempty"`
}

// CreateTaskResponse 创建任务响应
type CreateTaskResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    *TaskCreateData `json:"data,omitempty"`
}

// TaskCreateData 任务创建数据
type TaskCreateData struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Status string `json:"status"`
}

// BatchTaskRequest 批量任务请求
type BatchTaskRequest struct {
	URLs       []string `json:"urls"`
	Quality    string   `json:"quality,omitempty"`
	Priority   int      `json:"priority,omitempty"`
	UseCookies bool     `json:"use_cookies,omitempty"`
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
	var req CreateTaskRequest
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
			Quality:    quality,
			OutputDir:  h.outputDir,
			TempDir:    h.tempDir,
			Timeout:    time.Duration(3600) * time.Second, // 默认 1 小时超时
			TaskID:     taskID,                            // 传递 TaskID 用于生成确定的文件名
			UserID:     int(claims.UserID),                // 注入用户身份
			UserRole:   claims.Role,                       // 注入用户角色
			UserAgent:  r.UserAgent(),                     // 注入浏览器 User-Agent
			UseCookies: req.UseCookies,                    // 是否使用 Cookie
		},
		CreatedAt: time.Now(),
	}

	// 保存到数据库
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	useCookiesInt := 0
	if req.UseCookies {
		useCookiesInt = 1
	}
	_, err := h.db.Exec(`
		INSERT INTO tasks (id, user_id, url, status, quality, engine, use_cookies, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, taskID, int64(claims.UserID), req.URL, string(engine.TaskStatusQueued), quality, "", useCookiesInt, nowStr)
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

	// 立即推送任务创建通知（让前端能立刻收到新任务）
	NotifyTaskUpdate(task)

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

	writeJSON(w, http.StatusCreated, CreateTaskResponse{
		Success: true,
		Message: "任务创建成功",
		Data: &TaskCreateData{
			ID:     taskID,
			URL:    req.URL,
			Status: string(engine.TaskStatusQueued),
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
			writeError(w, http.StatusBadRequest, fmt.Sprintf("无效的 URL 格式：%s", rawURL))
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
	useCookiesInt := 0
	if req.UseCookies {
		useCookiesInt = 1
	}
	for _, url := range validURLs {
		taskID := uuid.New().String()

		// 创建任务对象
		task := &engine.Task{
			ID:       taskID,
			URL:      url,
			Priority: priority,
			Status:   engine.TaskStatusQueued,
			Options: engine.DownloadOptions{
				Quality:    quality,
				OutputDir:  h.outputDir,
				TempDir:    h.tempDir,
				Timeout:    time.Duration(3600) * time.Second,
				TaskID:     taskID, // 传递 TaskID 用于生成确定的文件名
				UserID:     int(claims.UserID),                // 注入用户身份
				UserRole:   claims.Role,                       // 注入用户角色
				UserAgent:  r.UserAgent(),                     // 注入浏览器 User-Agent
				UseCookies: req.UseCookies,                    // 是否使用 Cookie
			},
			BatchID:   batchID,
			CreatedAt: time.Now(),
		}

		// 保存到数据库
		nowStr := time.Now().Format("2006-01-02 15:04:05")
		_, err := tx.Exec(`
			INSERT INTO tasks (id, user_id, url, status, quality, engine, batch_id, use_cookies, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, taskID, int64(claims.UserID), url, string(engine.TaskStatusQueued), quality, "", batchID, useCookiesInt, nowStr)
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
		// 立即推送任务创建通知（让前端能立刻收到新任务）
		NotifyTaskUpdate(task)
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
