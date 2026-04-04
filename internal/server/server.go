// Package server 提供 HTTP 服务器功能
package server

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
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
	cookieHandler *handler.CookieHandler // Cookie 处理器

	// 健康检查状态
	healthMutex  sync.RWMutex
	healthStatus HealthStatus
	startTime    time.Time // 服务器启动时间
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

	// 创建 Cookie 处理器
	cookieHandler := handler.NewCookieHandler(db, jwtMgr)

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
		cookieHandler: cookieHandler,
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

	// 设置调度器的 CookieGetter（使下载引擎能够获取用户 Cookie）
	if s.cookieHandler != nil {
		s.scheduler.SetCookieGetter(s.cookieHandler)
	}

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
	log.Printf("🚀 Campus Collector server started on %s", s.cfg.GetAddress())

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
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
