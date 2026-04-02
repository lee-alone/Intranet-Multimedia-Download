// Package server 提供 HTTP 服务器功能
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	collector "github.com/campus/collector"
	"github.com/campus/collector/internal/alert"
	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/auth"
	"github.com/campus/collector/internal/config"
	"github.com/campus/collector/internal/engine"
	"github.com/campus/collector/internal/handler"
	"github.com/campus/collector/internal/logrotate"
	"github.com/campus/collector/internal/middleware"
)

// WebFS 嵌入的前端资源文件系统
// 从根包导入
var WebFS = collector.WebFS

// Server 表示 HTTP 服务器
type Server struct {
	cfg          *config.Config
	db           *sql.DB
	jwtMgr       *auth.JWTManager
	ldap         *auth.LDAPClient
	auditLogger  *audit.Logger
	server       *http.Server
	mux          *http.ServeMux
	scheduler    *engine.TaskScheduler
	whitelistMgr *middleware.WhitelistManager
	alertManager *alert.AlertManager
	logRotator   *logrotate.Rotator
	outputDir    string // 下载目录

	// 健康检查状态
	healthMutex  sync.RWMutex
	healthStatus HealthStatus
	startTime    time.Time // 服务器启动时间
}

// DependencyStatus 表示依赖项状态
type DependencyStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// HealthStatus 表示健康检查状态
type HealthStatus struct {
	Status    string           `json:"status"` // "healthy", "degraded", "unhealthy"
	Timestamp string           `json:"timestamp"`
	Database  bool             `json:"database"`
	YtDlp     DependencyStatus `json:"yt_dlp"`
	FFmpeg    DependencyStatus `json:"ffmpeg"`
	Start     time.Time        `json:"-"` // 服务器启动时间
}

// Metrics 表示 Prometheus 指标
type Metrics struct {
	TotalRequests int64     `json:"total_requests"`
	ActiveUsers   int64     `json:"active_users"`
	QueueSize     int64     `json:"queue_size"`
	Uptime        string    `json:"uptime"`
	StartTime     time.Time `json:"-"`
}

// New 创建新的服务器实例
func New(cfg *config.Config, db *sql.DB, scheduler *engine.TaskScheduler, outputDir string) (*Server, error) {
	// 创建 JWT 管理器
	jwtMgr, err := auth.NewJWTManager(
		cfg.Auth.PrivateKey,
		cfg.Auth.PublicKey,
		cfg.Auth.TokenExpiry,
		cfg.Auth.RefreshExpiry,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT manager: %w", err)
	}

	// 创建 LDAP 客户端
	ldapClient := auth.NewLDAPClient(
		cfg.Auth.LDAP.URL,
		cfg.Auth.LDAP.BindDN,
		cfg.Auth.LDAP.Password,
		cfg.Auth.LDAP.BaseDN,
		cfg.Auth.LDAP.Timeout,
		cfg.Auth.LDAP.Enabled,
	)

	// 创建 SSO 客户端
	var ssoClient *auth.SSOClient
	if cfg.Auth.SSO.Enabled {
		ssoConfig := &auth.SSOConfig{
			Provider:   auth.SSOProvider(cfg.Auth.SSO.Provider),
			Enabled:    cfg.Auth.SSO.Enabled,
			CASURL:     cfg.Auth.SSO.CASURL,
			CASService: cfg.Auth.SSO.CASService,
		}
		if cfg.Auth.SSO.OAuth2.ClientID != "" {
			ssoConfig.OAuth2Config = &auth.OAuth2Config{
				ClientID:     cfg.Auth.SSO.OAuth2.ClientID,
				ClientSecret: cfg.Auth.SSO.OAuth2.ClientSecret,
				AuthURL:      cfg.Auth.SSO.OAuth2.AuthURL,
				TokenURL:     cfg.Auth.SSO.OAuth2.TokenURL,
				UserInfoURL:  cfg.Auth.SSO.OAuth2.UserInfoURL,
				Scopes:       cfg.Auth.SSO.OAuth2.Scopes,
				RedirectURL:  cfg.Auth.SSO.OAuth2.RedirectURL,
			}
		}
		ssoClient = auth.NewSSOClient(ssoConfig)
	}

	// 保存 SSO 客户端到 server 结构体（在后面的代码中使用）
	_ = ssoClient

	// 创建审计日志记录器
	auditLogger, err := audit.NewLogger(cfg.Log.Dir, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit logger: %w", err)
	}

	// 创建白名单管理器
	whitelistMgr := middleware.NewWhitelistManager(cfg.Download.Whitelist)

	// 创建告警管理器
	alertConfig := alert.Config{
		EnableDiskAlert:   cfg.Alert.EnableDiskAlert,
		DiskThreshold:     cfg.Alert.DiskThreshold,
		CheckInterval:     time.Duration(cfg.Alert.CheckInterval) * time.Minute,
		EnableWebhook:     cfg.Alert.EnableWebhook,
		WebhookURL:        cfg.Alert.WebhookURL,
		WebhookType:       cfg.Alert.WebhookType,
		EnableEmail:       cfg.Alert.EnableEmail,
		EmailSMTPServer:   cfg.Alert.EmailSMTPServer,
		EmailSMTPPort:     cfg.Alert.EmailSMTPPort,
		EmailFrom:         cfg.Alert.EmailFrom,
		EmailPassword:     cfg.Alert.EmailPassword,
		EmailTo:           cfg.Alert.EmailTo,
		EmailUseTLS:       cfg.Alert.EmailUseTLS,
		EmailAuthType:     cfg.Alert.EmailAuthType,
		EnableLogAlert:    cfg.Alert.EnableLogAlert,
		LogAlertThreshold: cfg.Alert.LogAlertThreshold,
	}
	alertManager := alert.NewAlertManager(alertConfig)

	// 创建日志轮转器
	logRotator := logrotate.NewRotator(cfg.Log.Dir, logrotate.Config{
		MaxSize:    int64(cfg.Log.MaxSize),
		MaxAge:     cfg.Log.MaxAge,
		Compress:   cfg.Log.Compress,
		MaxBackups: 10,
	})

	mux := http.NewServeMux()

	startTime := time.Now()

	s := &Server{
		cfg:          cfg,
		db:           db,
		jwtMgr:       jwtMgr,
		ldap:         ldapClient,
		auditLogger:  auditLogger,
		mux:          mux,
		scheduler:    scheduler,
		whitelistMgr: whitelistMgr,
		alertManager: alertManager,
		logRotator:   logRotator,
		outputDir:    outputDir,
		server: &http.Server{
			Addr:         cfg.GetAddress(),
			Handler:      mux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		healthStatus: HealthStatus{
			Status:   "initializing",
			Database: false,
			YtDlp:    DependencyStatus{Name: "yt-dlp"},
			FFmpeg:   DependencyStatus{Name: "ffmpeg"},
		},
		startTime: startTime,
	}

	// 启动时初始化健康状态
	s.updateHealthStatus()

	return s, nil
}

// Start 启动服务器
func (s *Server) Start() error {
	// 设置调度器回调（必须在启动前设置）
	s.setupSchedulerCallbacks()

	// 注册路由
	s.registerRoutes()

	// 启动告警管理器
	if s.alertManager != nil {
		s.alertManager.Start()
	}

	// 启动日志轮转定时任务
	if s.logRotator != nil {
		go s.logRotateLoop()
	}

	// 启动服务器
	log.Printf("Server starting on %s", s.cfg.GetAddress())

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
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

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Server shutting down...")

	// 关闭告警管理器
	if s.alertManager != nil {
		s.alertManager.Stop()
	}

	// 关闭审计日志记录器
	if s.auditLogger != nil {
		s.auditLogger.Close()
	}

	return s.server.Shutdown(ctx)
}

// registerRoutes 注册所有路由
func (s *Server) registerRoutes() {
	// 注册静态文件服务（前端资源）
	s.registerStaticFiles()

	// 创建认证处理器（传入审计日志记录器）
	authHandler := handler.NewAuthHandler(s.db, s.jwtMgr, s.ldap, s.auditLogger)

	// 设置 SSO 客户端（如果已配置）
	if s.cfg.Auth.SSO.Enabled {
		ssoConfig := &auth.SSOConfig{
			Provider:   auth.SSOProvider(s.cfg.Auth.SSO.Provider),
			Enabled:    s.cfg.Auth.SSO.Enabled,
			CASURL:     s.cfg.Auth.SSO.CASURL,
			CASService: s.cfg.Auth.SSO.CASService,
		}
		if s.cfg.Auth.SSO.OAuth2.ClientID != "" {
			ssoConfig.OAuth2Config = &auth.OAuth2Config{
				ClientID:     s.cfg.Auth.SSO.OAuth2.ClientID,
				ClientSecret: s.cfg.Auth.SSO.OAuth2.ClientSecret,
				AuthURL:      s.cfg.Auth.SSO.OAuth2.AuthURL,
				TokenURL:     s.cfg.Auth.SSO.OAuth2.TokenURL,
				UserInfoURL:  s.cfg.Auth.SSO.OAuth2.UserInfoURL,
				Scopes:       s.cfg.Auth.SSO.OAuth2.Scopes,
				RedirectURL:  s.cfg.Auth.SSO.OAuth2.RedirectURL,
			}
		}
		authHandler.SetSSOClient(auth.NewSSOClient(ssoConfig))
	}

	// 创建任务处理器（传入白名单管理器、审计日志记录器和下载目录）
	taskHandler := handler.NewTaskHandler(s.db, s.scheduler, s.jwtMgr, s.whitelistMgr, s.auditLogger, s.outputDir)

	// 创建 WebSocket/进度流处理器（传入 db 用于权限验证）
	wsHandler := handler.NewWebSocketHandler(s.db, s.jwtMgr)

	// 用于跟踪每个任务的最后推送进度（用于数据库更新）
	type taskProgressState struct {
		lastDbProgress int // 上次写入数据库的进度（整数）
		lastStatus     string
	}
	progressStates := make(map[string]*taskProgressState)
	var progressStatesMu sync.Mutex

	// 设置任务调度器的进度更新回调，用于 WebSocket 推送和数据库更新
	s.scheduler.SetProgressUpdateCallback(func(task *engine.Task) {
		currentStatus := string(task.Status)
		currentProgressInt := int(task.Progress.Percent) // 取整数部分
		
		// 获取或创建状态跟踪
		progressStatesMu.Lock()
		state, exists := progressStates[task.ID]
		if !exists {
			state = &taskProgressState{}
			progressStates[task.ID] = state
		}
		progressStatesMu.Unlock()
		
		// 当任务完成时，保存 file_path 和 title 到数据库，进度强制设为 100%
		if task.Status == engine.TaskStatusCompleted && task.FilePath != "" {
			_, err := s.db.Exec(`UPDATE tasks SET file_path = ?, title = ?, status = 'completed', completed_at = ?, progress = 100 WHERE id = ?`,
				task.FilePath, task.Title, time.Now(), task.ID)
			if err != nil {
				log.Printf("保存文件路径和标题到数据库失败：%v", err)
			}
			// 数据库更新后推送，确保前端收到完成状态
			handler.NotifyTaskUpdate(task)
			// 清理状态跟踪
			progressStatesMu.Lock()
			delete(progressStates, task.ID)
			progressStatesMu.Unlock()
		} else if task.Status == engine.TaskStatusDownloading {
			// 下载中：只有进度整数部分变化或状态变化时才写数据库（减少 IO）
			shouldUpdateDB := false
			if state.lastStatus != currentStatus {
				shouldUpdateDB = true
			} else if currentProgressInt != state.lastDbProgress {
				shouldUpdateDB = true
			}
			
			if shouldUpdateDB {
				_, err := s.db.Exec(`UPDATE tasks SET progress = ?, status = ? WHERE id = ?`, currentProgressInt, currentStatus, task.ID)
				if err != nil {
					log.Printf("更新任务进度到数据库失败：%v", err)
				}
				// 更新状态跟踪
				progressStatesMu.Lock()
				state.lastDbProgress = currentProgressInt
				state.lastStatus = currentStatus
				progressStatesMu.Unlock()
			}
			
			// 推送 SSE（前端可能还在使用）
			handler.NotifyTaskUpdate(task)
		} else if task.Status == engine.TaskStatusFailed || task.Status == engine.TaskStatusCancelled {
			// 失败/取消：更新数据库
			_, err := s.db.Exec(`UPDATE tasks SET status = ?, progress = ? WHERE id = ?`, currentStatus, currentProgressInt, task.ID)
			if err != nil {
				log.Printf("更新任务状态到数据库失败：%v", err)
			}
			handler.NotifyTaskUpdate(task)
			// 清理状态跟踪
			progressStatesMu.Lock()
			delete(progressStates, task.ID)
			progressStatesMu.Unlock()
		}
	})

	// 健康检查端点
	s.mux.HandleFunc("/health", s.healthHandler)

	// 详细健康检查端点
	s.mux.HandleFunc("/healthz", s.healthzHandler)

	// 就绪检查端点
	s.mux.HandleFunc("/ready", s.readyHandler)

	// 存活检查端点
	s.mux.HandleFunc("/live", s.liveHandler)

	// 指标暴露端点（Prometheus 格式）
	s.mux.HandleFunc("/metrics", s.metricsHandler)

	// 基础指标端点（JSON 格式）
	s.mux.HandleFunc("/api/v1/metrics", s.apiMetricsHandler)

	// 进度流端点（Server-Sent Events，用于实时推送任务进度）
	s.mux.HandleFunc("/api/v1/progress", wsHandler.HandleProgressStream)

	// WebSocket 端点（真正的 WebSocket 协议）
	s.mux.HandleFunc("/api/v1/ws", wsHandler.HandleWebSocket)

	// API v1 路由
	s.mux.HandleFunc("/api/v1/login", authHandler.Login)
	s.mux.HandleFunc("/api/v1/register", authHandler.Register)
	s.mux.HandleFunc("/api/v1/token/refresh", authHandler.RefreshToken)

	// SSO 登录路由
	s.mux.HandleFunc("/api/v1/sso/login", authHandler.HandleSSOLogin)
	s.mux.HandleFunc("/api/v1/sso/callback", authHandler.HandleSSOCallback)
	s.mux.HandleFunc("/api/v1/sso/status", authHandler.GetSSOStatus)
	s.mux.HandleFunc("/api/v1/sso/login-url", authHandler.GetSSOLoginURL)

	// 协议相关路由
	s.mux.HandleFunc("/api/v1/agreement/status", authHandler.GetAgreementStatus)
	s.mux.HandleFunc("/api/v1/agreement/text", authHandler.GetAgreementText)

	// 需要认证的路由中间件
	authMiddleware := handler.AuthMiddleware(s.jwtMgr)

	// 用户信息路由
	s.mux.Handle("/api/v1/user/me", authMiddleware(http.HandlerFunc(authHandler.GetCurrentUser)))

	// 任务创建和列表路由
	s.mux.Handle("/api/v1/tasks", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			// POST: 创建单个任务
			taskHandler.CreateTask(w, r)
		case http.MethodGet:
			// GET: 获取任务列表
			taskHandler.GetTasks(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})))

	// 任务统计路由
	s.mux.Handle("/api/v1/tasks/stats", authMiddleware(http.HandlerFunc(taskHandler.GetTaskStats)))

	// 审计日志路由（移除 MFA 验证）
	s.mux.Handle("/api/v1/audit/logs", authMiddleware(http.HandlerFunc(authHandler.GetAuditLogs)))

	// 需要认证的路由
	s.mux.Handle("/api/v1/logout", authMiddleware(http.HandlerFunc(authHandler.Logout)))

	// 协议同意路由（需要认证）
	s.mux.Handle("/api/v1/agreement/agree", authMiddleware(http.HandlerFunc(authHandler.AgreeToAgreement)))

	// 协议同意中间件（可选：用于需要协议同意的路由）
	// 使用方式：agreementMiddleware := authHandler.AgreementManager().CheckAgreementMiddleware("/api/v1/agreement/*")
	// 当前实现：在 handler 中手动检查 HasAgreed()
	// 未来可以：使用 agreementMiddleware 包装需要协议同意的路由，例如：
	// agreementMiddleware := authHandler.AgreementManager().CheckAgreementMiddleware("/api/v1/agreement/status", "/api/v1/agreement/text", "/api/v1/agreement/agree")
	// s.mux.Handle("/api/v1/tasks/batch", agreementMiddleware(authMiddleware(http.HandlerFunc(taskHandler.CreateBatchTask))))

	// 批量任务路由
	s.mux.Handle("/api/v1/tasks/batch", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			taskHandler.CreateBatchTask(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})))

	// 批量任务进度查询路由
	s.mux.Handle("/api/v1/tasks/batch/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查是否是批量任务进度查询
		path := r.URL.Path
		parts := strings.Split(path, "/")
		if strings.HasPrefix(path, "/api/v1/tasks/batch/") && len(parts) == 6 {
			taskHandler.GetBatchProgress(w, r)
			return
		}
		http.NotFound(w, r)
	})))

	// 单任务取消/删除/下载路由
	s.mux.Handle("/api/v1/tasks/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查是否是单任务操作
		path := r.URL.Path
		parts := strings.Split(path, "/")
		// 路径格式：/api/v1/tasks/{id}/download
		// parts: ["", "api", "v1", "tasks", "{id}", "download"] -> len=6
		if len(parts) == 6 && parts[5] == "download" && r.Method == http.MethodGet {
			taskHandler.DownloadFile(w, r)
			return
		}
		// 路径格式：/api/v1/tasks/{id}
		// parts: ["", "api", "v1", "tasks", "{id}"] -> len=5
		if len(parts) == 5 && parts[4] != "" {
			if r.Method == http.MethodDelete {
				// DELETE 请求：根据任务状态自动判断是取消还是删除
				// 为了保持向后兼容，统一使用 CancelTask 处理
				// 前端应该根据任务状态调用不同的 API
				taskHandler.CancelOrDeleteTask(w, r)
				return
			}
		}
		// 如果是 /api/v1/tasks 或 /api/v1/tasks/ 且是 GET 请求，返回任务列表
		// parts: ["", "api", "v1", "tasks"] -> len=4 (不带尾随斜杠)
		// parts: ["", "api", "v1", "tasks", ""] -> len=5 (带尾随斜杠)
		if r.Method == http.MethodGet && (len(parts) == 4 || (len(parts) == 5 && parts[4] == "")) {
			taskHandler.GetTasks(w, r)
			return
		}
		// 其他方法返回 404
		http.NotFound(w, r)
	})))
}

// updateHealthStatus 更新健康状态
func (s *Server) updateHealthStatus() {
	s.healthMutex.Lock()
	defer s.healthMutex.Unlock()

	// 检查数据库连接
	dbHealthy := false
	if s.db != nil {
		err := s.db.Ping()
		dbHealthy = (err == nil)
	}

	// 检查 yt-dlp
	ytDlpStatus := checkCommand("yt-dlp", "--version")

	// 检查 ffmpeg
	ffmpegStatus := checkCommand("ffmpeg", "-version")

	// 确定整体状态
	status := "healthy"
	if !dbHealthy {
		status = "unhealthy"
	} else if !ytDlpStatus.Available && !ffmpegStatus.Available {
		status = "degraded"
	}

	s.healthStatus = HealthStatus{
		Status:    status,
		Timestamp: time.Now().Format(time.RFC3339),
		Database:  dbHealthy,
		YtDlp:     ytDlpStatus,
		FFmpeg:    ffmpegStatus,
	}
}

// checkCommand 检查命令是否可用并获取版本
func checkCommand(name, arg string) DependencyStatus {
	status := DependencyStatus{Name: name, Available: false}

	cmd := exec.Command(name, arg)
	output, err := cmd.Output()
	if err != nil {
		status.Error = err.Error()
		return status
	}

	status.Available = true
	status.Version = string(output[:len(output)-1]) // 去掉换行符
	return status
}

// healthHandler 处理健康检查请求（简化版）
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	s.healthMutex.RLock()
	status := s.healthStatus
	s.healthMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	switch status.Status {
	case "healthy":
		w.WriteHeader(http.StatusOK)
	case "degraded":
		w.WriteHeader(http.StatusOK) // 降级状态仍返回 200
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	fmt.Fprintf(w, `{"status":"%s","timestamp":"%s"}`, status.Status, status.Timestamp)
}

// healthzHandler 处理详细健康检查请求
func (s *Server) healthzHandler(w http.ResponseWriter, r *http.Request) {
	s.healthMutex.RLock()
	status := s.healthStatus
	s.healthMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	var httpStatus int
	switch status.Status {
	case "healthy":
		httpStatus = http.StatusOK
	case "degraded":
		httpStatus = http.StatusOK
	default:
		httpStatus = http.StatusServiceUnavailable
	}
	w.WriteHeader(httpStatus)

	json.NewEncoder(w).Encode(status)
}

// readyHandler 处理就绪检查请求
func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	s.healthMutex.RLock()
	status := s.healthStatus
	s.healthMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	// 数据库和至少一个下载器可用才算就绪
	ready := status.Database && (status.YtDlp.Available || status.FFmpeg.Available)

	if ready {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	fmt.Fprintf(w, `{"ready":%t,"timestamp":"%s","database":%t,"yt_dlp":%t,"ffmpeg":%t}`,
		ready, status.Timestamp, status.Database, status.YtDlp.Available, status.FFmpeg.Available)
}

// liveHandler 处理存活检查请求
func (s *Server) liveHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"alive":true,"timestamp":"%s"}`, time.Now().Format(time.RFC3339))
}

// metricsHandler 处理 Prometheus 格式指标请求
func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	s.healthMutex.RLock()
	status := s.healthStatus
	s.healthMutex.RUnlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	// Prometheus 格式指标
	fmt.Fprintf(w, "# HELP server_health 服务器健康状态 (1=healthy, 0=unhealthy)\n")
	fmt.Fprintf(w, "# TYPE server_health gauge\n")
	if status.Status == "healthy" {
		fmt.Fprintf(w, "server_health 1\n")
	} else {
		fmt.Fprintf(w, "server_health 0\n")
	}

	fmt.Fprintf(w, "# HELP server_database_health 数据库健康状态\n")
	fmt.Fprintf(w, "# TYPE server_database_health gauge\n")
	if status.Database {
		fmt.Fprintf(w, "server_database_health 1\n")
	} else {
		fmt.Fprintf(w, "server_database_health 0\n")
	}

	fmt.Fprintf(w, "# HELP server_ytdlp_available yt-dlp 是否可用\n")
	fmt.Fprintf(w, "# TYPE server_ytdlp_available gauge\n")
	if status.YtDlp.Available {
		fmt.Fprintf(w, "server_ytdlp_available 1\n")
	} else {
		fmt.Fprintf(w, "server_ytdlp_available 0\n")
	}

	fmt.Fprintf(w, "# HELP server_ffmpeg_available ffmpeg 是否可用\n")
	fmt.Fprintf(w, "# TYPE server_ffmpeg_available gauge\n")
	if status.FFmpeg.Available {
		fmt.Fprintf(w, "server_ffmpeg_available 1\n")
	} else {
		fmt.Fprintf(w, "server_ffmpeg_available 0\n")
	}

	fmt.Fprintf(w, "# HELP server_uptime_seconds 服务器运行时间（秒）\n")
	fmt.Fprintf(w, "# TYPE server_uptime_seconds counter\n")
	fmt.Fprintf(w, "server_uptime_seconds %.0f\n", time.Since(s.startTime).Seconds())
}

// apiMetricsHandler 处理 API 格式指标请求
func (s *Server) apiMetricsHandler(w http.ResponseWriter, r *http.Request) {
	s.healthMutex.RLock()
	status := s.healthStatus
	s.healthMutex.RUnlock()

	metrics := map[string]interface{}{
		"health": map[string]interface{}{
			"status":   status.Status,
			"database": status.Database,
			"yt_dlp":   status.YtDlp.Available,
			"ffmpeg":   status.FFmpeg.Available,
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// registerStaticFiles 注册静态文件服务（前端资源）
func (s *Server) registerStaticFiles() {
	// 使用嵌入的 WebFS 提供前端资源
	// 从 embed.FS 获取子目录 web/dist
	webFS, err := fs.Sub(WebFS, "web/dist")
	if err != nil {
		// 如果嵌入失败，使用本地文件系统作为后备
		webFS = nil
	}

	// 如果嵌入文件系统可用，使用它
	if webFS != nil {
		// 注册静态文件处理器
		fileServer := http.FileServer(http.FS(webFS))

		// 根路径 - 提供前端资源
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// 如果是 API 路径，跳过静态文件处理
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}

			// 如果是根路径，直接交给 fileServer 处理 (它会自动服务 index.html)
			if r.URL.Path == "/" || r.URL.Path == "" {
				fileServer.ServeHTTP(w, r)
				return
			}

			// 获取去掉了开头斜杠的路径，用于 fs.Stat 检查文件是否存在
			path := strings.TrimPrefix(r.URL.Path, "/")

			// 检查文件是否存在
			if _, err := fs.Stat(webFS, path); err != nil {
				// 文件不存在，很可能是 SPA (单页应用) 路由
				// 此时应当返回 index.html 的内容，但保持当前的浏览地址不变
				// 我们通过克隆请求并将路径改为 "/" 来实现，这样 fileServer 内部会服务 index.html 且不会触发重定向
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}

			// 文件存在，直接服务
			fileServer.ServeHTTP(w, r)
		})
	}
}
