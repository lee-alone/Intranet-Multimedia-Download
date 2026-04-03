// Package handler 提供 HTTP 请求处理器
package handler

import (
	"database/sql"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/auth"
	"github.com/campus/collector/internal/engine"
	"github.com/campus/collector/internal/middleware"
)

// TaskHandler 任务处理器
type TaskHandler struct {
	db           *sql.DB
	scheduler    *engine.TaskScheduler
	jwtMgr       *auth.JWTManager
	whitelistMgr *middleware.WhitelistManager
	auditLogger  *audit.Logger
	outputDir    string
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(db *sql.DB, scheduler *engine.TaskScheduler, jwtMgr *auth.JWTManager, whitelistMgr *middleware.WhitelistManager, auditLogger *audit.Logger, outputDir string) *TaskHandler {
	// 使用外部传入的 outputDir（基于 os.Executable() 计算）
	// 确保目录存在
	os.MkdirAll(outputDir, 0755)

	return &TaskHandler{
		db:           db,
		scheduler:    scheduler,
		jwtMgr:       jwtMgr,
		whitelistMgr: whitelistMgr,
		auditLogger:  auditLogger,
		outputDir:    outputDir,
	}
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
			continue
		}
		// 从调度器获取实时进度（仅对非终态任务）
		if !engine.TaskStatus(task.Status).IsTerminal() {
			if activeTask, err := h.scheduler.GetTask(task.ID); err == nil {
				task.Progress = activeTask.GetProgress().Percent
			}
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
		writeError(w, http.StatusInternalServerError, "查询总任务数失败")
		return
	}

	var completedTasks int
	err = h.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE user_id = ? AND status = 'completed'`, claims.UserID).Scan(&completedTasks)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询已完成任务失败")
		return
	}

	var pendingTasks int
	err = h.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE user_id = ? AND status IN ('queued', 'downloading')`, claims.UserID).Scan(&pendingTasks)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询进行中任务失败")
		return
	}

	var failedTasks int
	err = h.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE user_id = ? AND status = 'failed'`, claims.UserID).Scan(&failedTasks)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询失败任务失败")
		return
	}

	var downloadingTasks int
	err = h.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE user_id = ? AND status = 'downloading'`, claims.UserID).Scan(&downloadingTasks)
	if err != nil {
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
