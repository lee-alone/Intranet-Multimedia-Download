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

	"github.com/campus/collector/internal/auth"
	"github.com/campus/collector/internal/config"
	"github.com/campus/collector/internal/database"
	"github.com/campus/collector/internal/engine"
	"github.com/campus/collector/internal/server"
)

func main() {
	// 获取程序运行目录
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}
	execDir := filepath.Dir(execPath)
	log.Printf("Program running directory: %s", execDir)

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
	log.Println("Database initialized successfully")

	// 运行数据库迁移
	if err := database.RunMigrations("./migrations"); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Database migrations completed")

	// 获取数据库连接
	db := database.Get()

	// 初始化默认管理员账号
	log.Println("Initializing default admin user...")
	adminCfg := cfg.Auth.DefaultAdmin
	if adminCfg.Enabled {
		if err := auth.InitDefaultAdmin(db, adminCfg.Username, adminCfg.Password, adminCfg.Email); err != nil {
			log.Printf("警告：创建默认管理员账号失败：%v", err)
		}
	} else {
		log.Println("默认管理员账号功能已禁用")
	}

	// 确保下载目录存在（相对于程序运行目录）
	downloadDir := filepath.Join(execDir, "downloads")
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		log.Fatalf("Failed to create downloads directory: %v", err)
	}
	log.Printf("Downloads directory: %s", downloadDir)

	// 确保临时目录存在
	tempDir := filepath.Join(execDir, "temp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Fatalf("Failed to create temp directory: %v", err)
	}
	log.Printf("Temp directory: %s", tempDir)

	// 创建任务调度器
	schedulerConfig := engine.DefaultSchedulerConfig()
	schedulerConfig.MaxConcurrent = cfg.Download.Concurrent
	scheduler := engine.NewTaskScheduler(nil, schedulerConfig)

	// 创建下载引擎（使用相对于程序运行目录的路径）
	ytdlp := engine.NewYtdlpEngine(engine.YtdlpConfig{
		ExecPath:   filepath.Join(execDir, "runtime", "yt-dlp.exe"),
		OutputDir:  downloadDir,
		Timeout:    time.Duration(cfg.Download.Timeout) * time.Second,
		MaxRetries: 3,
	})

	lux := engine.NewLuxEngine(engine.LuxConfig{
		ExecPath:  filepath.Join(execDir, "runtime", "lux.exe"),
		OutputDir: downloadDir,
	})

	// 创建故障转移引擎
	failoverConfig := engine.DefaultFailoverConfig()
	failoverConfig.MaxFailures = 3
	failoverConfig.CooldownTime = 10 * time.Minute
	engineWrapper := engine.NewFailoverEngine(ytdlp, lux, failoverConfig)

	// 将引擎注入调度器
	scheduler.SetEngine(engineWrapper)

	// 创建服务器
	srv, err := server.New(cfg, db, scheduler)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// 启动服务器（在 goroutine 中）
	go func() {
		log.Printf("Server starting on %s", cfg.GetAddress())
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
