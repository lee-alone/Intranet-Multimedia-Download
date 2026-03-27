// Package main 是校园资源采集系统的入口点
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/campus/collector/internal/config"
	"github.com/campus/collector/internal/database"
	"github.com/campus/collector/internal/engine"
	"github.com/campus/collector/internal/server"
)

func main() {
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

	// 创建任务调度器
	schedulerConfig := engine.DefaultSchedulerConfig()
	scheduler := engine.NewTaskScheduler(nil, schedulerConfig) // engine 为 nil，后续会设置

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
