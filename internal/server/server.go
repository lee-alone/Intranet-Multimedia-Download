// Package server 提供 HTTP 服务器功能
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/auth"
	"github.com/campus/collector/internal/config"
	"github.com/campus/collector/internal/engine"
	"github.com/campus/collector/internal/handler"
	"github.com/campus/collector/internal/middleware"
)

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
func New(cfg *config.Config, db *sql.DB, scheduler *engine.TaskScheduler) (*Server, error) {
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

	// 创建审计日志记录器
	auditLogger, err := audit.NewLogger(cfg.Log.Dir, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit logger: %w", err)
	}

	// 创建白名单管理器
	whitelistMgr := middleware.NewWhitelistManager(cfg.Download.Whitelist)

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
	// 注册路由
	s.registerRoutes()

	// 启动服务器
	log.Printf("Server starting on %s", s.cfg.GetAddress())

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Server shutting down...")

	// 关闭审计日志记录器
	if s.auditLogger != nil {
		s.auditLogger.Close()
	}

	return s.server.Shutdown(ctx)
}

// registerRoutes 注册所有路由
func (s *Server) registerRoutes() {
	// 创建认证处理器（传入审计日志记录器）
	authHandler := handler.NewAuthHandler(s.db, s.jwtMgr, s.ldap, s.auditLogger)

	// 创建任务处理器（传入白名单管理器）
	taskHandler := handler.NewTaskHandler(s.db, s.scheduler, s.jwtMgr, s.whitelistMgr)

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

	// API v1 路由
	s.mux.HandleFunc("/api/v1/login", authHandler.Login)
	s.mux.HandleFunc("/api/v1/register", authHandler.Register)
	s.mux.HandleFunc("/api/v1/token/refresh", authHandler.RefreshToken)

	// 需要认证的路由
	s.mux.Handle("/api/v1/logout", handler.AuthMiddleware(s.jwtMgr)(http.HandlerFunc(authHandler.Logout)))

	// 任务相关路由（需要认证）
	authMiddleware := handler.AuthMiddleware(s.jwtMgr)

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
		if strings.HasPrefix(path, "/api/v1/tasks/batch/") && len(strings.Split(path, "/")) == 5 {
			taskHandler.GetBatchProgress(w, r)
			return
		}
		http.NotFound(w, r)
	})))

	// 单任务取消路由
	s.mux.Handle("/api/v1/tasks/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查是否是单任务操作
		path := r.URL.Path
		parts := strings.Split(path, "/")
		// 路径格式: /api/v1/tasks/{id}
		if len(parts) == 4 && parts[3] != "" && r.Method == http.MethodDelete {
			taskHandler.CancelTask(w, r)
			return
		}
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
