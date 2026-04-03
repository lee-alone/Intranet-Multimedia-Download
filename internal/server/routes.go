package server

import (
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/campus/collector/internal/auth"
	"github.com/campus/collector/internal/engine"
	"github.com/campus/collector/internal/handler"
)

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

	// 设置任务调度器的进度更新回调，用于 WebSocket 推送和数据库更新
	s.setupProgressUpdateCallback()

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

	// 用户管理路由（仅管理员）
	s.mux.Handle("/api/v1/users", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			authHandler.GetUsers(w, r)
		case http.MethodDelete:
			authHandler.DeleteUser(w, r)
		case http.MethodPut:
			authHandler.UpdateUser(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})))

	// 修改密码路由
	s.mux.Handle("/api/v1/user/change-password", authMiddleware(http.HandlerFunc(authHandler.ChangePassword)))

	// 管理员重置密码路由
	s.mux.Handle("/api/v1/admin/users/reset-password", authMiddleware(http.HandlerFunc(authHandler.AdminChangePassword)))

	// 任务创建和列表路由
	s.mux.Handle("/api/v1/tasks", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			taskHandler.CreateTask(w, r)
		case http.MethodGet:
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
		path := r.URL.Path
		parts := strings.Split(path, "/")
		if strings.HasPrefix(path, "/api/v1/tasks/batch/") && len(parts) == 6 {
			taskHandler.GetBatchProgress(w, r)
			return
		}
		http.NotFound(w, r)
	})))

	// 单任务取消/删除/下载/重试路由
	s.mux.Handle("/api/v1/tasks/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		parts := strings.Split(path, "/")

		// POST /api/v1/tasks/{id}/retry
		if len(parts) == 6 && parts[5] == "retry" && r.Method == http.MethodPost {
			taskHandler.RetryTask(w, r)
			return
		}
		// GET /api/v1/tasks/{id}/download
		if len(parts) == 6 && parts[5] == "download" && r.Method == http.MethodGet {
			taskHandler.DownloadFile(w, r)
			return
		}
		// DELETE /api/v1/tasks/{id} - 取消或删除任务
		if len(parts) == 5 && parts[4] != "" {
			if r.Method == http.MethodDelete {
				taskHandler.CancelOrDeleteTask(w, r)
				return
			}
		}
		// GET /api/v1/tasks 或 /api/v1/tasks/
		if r.Method == http.MethodGet && (len(parts) == 4 || (len(parts) == 5 && parts[4] == "")) {
			taskHandler.GetTasks(w, r)
			return
		}
		http.NotFound(w, r)
	})))
}

// taskProgressState 用于跟踪每个任务的最后推送进度
type taskProgressState struct {
	lastDbProgress int
	lastStatus     string
}

// setupProgressUpdateCallback 设置任务调度器的进度更新回调
func (s *Server) setupProgressUpdateCallback() {
	progressStates := make(map[string]*taskProgressState)
	var progressStatesMu sync.Mutex

	s.scheduler.SetProgressUpdateCallback(func(task *engine.Task) {
		currentStatus := string(task.Status)
		currentProgressInt := int(task.Progress.Percent)

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
				//log.Printf("保存文件路径和标题到数据库失败：%v", err)
			}
			handler.NotifyTaskUpdate(task)
			progressStatesMu.Lock()
			delete(progressStates, task.ID)
			progressStatesMu.Unlock()
		} else if task.Status == engine.TaskStatusDownloading {
			shouldUpdateDB := false
			if state.lastStatus != currentStatus {
				shouldUpdateDB = true
			} else if currentProgressInt != state.lastDbProgress {
				shouldUpdateDB = true
			}

			if shouldUpdateDB {
				_, err := s.db.Exec(`UPDATE tasks SET progress = ?, status = ? WHERE id = ?`, currentProgressInt, currentStatus, task.ID)
				if err != nil {
					//log.Printf("更新任务进度到数据库失败：%v", err)
				}
				progressStatesMu.Lock()
				state.lastDbProgress = currentProgressInt
				state.lastStatus = currentStatus
				progressStatesMu.Unlock()
			}

			handler.NotifyTaskUpdate(task)
		} else if task.Status == engine.TaskStatusFailed || task.Status == engine.TaskStatusCancelled {
			_, err := s.db.Exec(`UPDATE tasks SET status = ?, progress = ? WHERE id = ?`, currentStatus, currentProgressInt, task.ID)
			if err != nil {
				//log.Printf("更新任务状态到数据库失败：%v", err)
			}
			handler.NotifyTaskUpdate(task)
			progressStatesMu.Lock()
			delete(progressStates, task.ID)
			progressStatesMu.Unlock()
		}
	})
}

// registerStaticFiles 注册静态文件服务（前端资源）
func (s *Server) registerStaticFiles() {
	webFS, err := fs.Sub(WebFS, "web/dist")
	if err != nil {
		webFS = nil
	}

	if webFS != nil {
		fileServer := http.FileServer(http.FS(webFS))

		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}

			if r.URL.Path == "/" || r.URL.Path == "" {
				fileServer.ServeHTTP(w, r)
				return
			}

			path := strings.TrimPrefix(r.URL.Path, "/")

			if _, err := fs.Stat(webFS, path); err != nil {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}

			fileServer.ServeHTTP(w, r)
		})
	}
}
