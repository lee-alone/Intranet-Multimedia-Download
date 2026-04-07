// Package main 是校园资源采集系统的入口点
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/auth"
	"github.com/campus/collector/internal/config"
	"github.com/campus/collector/internal/database"
	"github.com/campus/collector/internal/engine"
	"github.com/campus/collector/internal/server"
)

func main() {
	// 获取程序根目录（基于 os.Executable()，不受 CWD 影响）
	execDir := config.GetBaseDir()

	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化数据库
	dbCfg := &database.Config{
		Path:     cfg.Database.Path,
		WALMode:  cfg.Database.WALMode,
		MaxConns: cfg.Database.MaxConns,
	}
	if err := database.Init(dbCfg); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// 运行数据库迁移
	if err := database.RunMigrations("./migrations"); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// 获取数据库连接
	db := database.Get()

	// 初始化默认管理员账号
	adminCfg := cfg.Auth.DefaultAdmin
	if adminCfg.Enabled {
		if err := auth.InitUser(db, adminCfg.Username, adminCfg.Password, adminCfg.Email, "admin"); err != nil {
			log.Printf("警告：创建默认管理员账号失败：%v", err)
		}
	}

	// 初始化默认测试账号
	userCfg := cfg.Auth.DefaultUser
	if userCfg.Enabled {
		if err := auth.InitUser(db, userCfg.Username, userCfg.Password, userCfg.Email, "user"); err != nil {
			log.Printf("警告：创建默认测试账号失败：%v", err)
		}
	}

	// 确保下载目录存在（相对于程序根目录）
	downloadDir := cfg.Download.OutputDir
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		log.Fatalf("Failed to create downloads directory: %v", err)
	}

	// 确保临时目录存在
	tempDir := cfg.Download.TempDir
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Fatalf("Failed to create temp directory: %v", err)
	}

	// 创建任务调度器
	schedulerConfig := engine.DefaultSchedulerConfig()
	schedulerConfig.MaxConcurrent = cfg.Download.Concurrent
	scheduler := engine.NewTaskScheduler(nil, schedulerConfig)

	// 创建 yt-dlp 下载引擎（使用相对于程序根目录的路径）
	ytdlp := engine.NewYtdlpEngine(engine.YtdlpConfig{
		ExecPath:   filepath.Join(execDir, "runtime", "yt-dlp.exe"),
		OutputDir:  downloadDir,
		TempDir:    tempDir,
		Timeout:    time.Duration(cfg.Download.Timeout) * time.Second,
		MaxRetries: 3,
	})

	// 直接将 yt-dlp 注入调度器（单引擎架构，简化维护）
	scheduler.SetEngine(ytdlp)

	// 设置临时文件目录
	scheduler.SetTempDir(tempDir)

	// 创建审计日志记录器（如果启用）
	if cfg.Audit.Enabled {
		auditLogger, err := audit.NewLogger(cfg.Audit.LogDir, cfg.Audit.FileEnabled)
		if err != nil {
			log.Printf("警告：创建审计日志记录器失败：%v", err)
		} else {
			scheduler.SetAuditLogger(auditLogger)
			log.Printf("审计日志已启用，日志目录: %s", cfg.Audit.LogDir)
		}
	}

	// 创建服务器（传入 downloadDir 和 tempDir 用于 TaskHandler）
	srv, err := server.New(cfg, db, scheduler, downloadDir, tempDir)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// 启动服务器（在 goroutine 中）
	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// 等待中断信号进行优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// 给服务器 30 秒时间完成正在处理的请求
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
