// Package database 提供数据库连接和操作功能
package database

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/campus/collector"
	_ "github.com/mattn/go-sqlite3"
)

var (
	db   *sql.DB
	once sync.Once
)

// Config 数据库配置
type Config struct {
	Path     string
	WALMode  bool
	MaxConns int
}

// Init 初始化数据库连接
func Init(cfg *Config) error {
	var err error
	once.Do(func() {
		err = initDB(cfg)
	})
	return err
}

func initDB(cfg *Config) (err error) {
	// 确保数据目录存在
	dir := filepath.Dir(cfg.Path)
	// #nosec G301 -- 目录权限 0750 用于允许所有者读写执行，组用户读执行
	if err = os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	// 打开数据库连接
	db, err = sql.Open("sqlite3", cfg.Path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// 设置连接池
	db.SetMaxOpenConns(cfg.MaxConns)
	// 确保 MaxIdleConns 至少为 1，避免连接频繁创建销毁
	maxIdleConns := cfg.MaxConns / 2
	if maxIdleConns < 1 {
		maxIdleConns = 1
	}
	db.SetMaxIdleConns(maxIdleConns)

	// 启用 WAL 模式
	if cfg.WALMode {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			return fmt.Errorf("failed to enable WAL mode: %w", err)
		}
	}

	// 设置其他 PRAGMA
	pragmas := []string{
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("failed to set pragma %s: %w", pragma, err)
		}
	}

	return nil
}

// Get 获取数据库连接
func Get() *sql.DB {
	return db
}

// Close 关闭数据库连接
func Close() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

// isValidMigrationFilename 验证迁移文件名是否有效
func isValidMigrationFilename(filename string) bool {
	// 检查文件扩展名
	if len(filename) < 5 || filename[len(filename)-4:] != ".sql" {
		return false
	}
	// 检查文件名格式：YYYYMMDDHHMMSS_description.sql
	name := filename[:len(filename)-4]
	if len(name) < 15 {
		return false
	}
	// 检查时间戳部分是否为数字
	for i := 0; i < 14; i++ {
		if name[i] < '0' || name[i] > '9' {
			return false
		}
	}
	// 检查不包含路径遍历字符
	if containsPathTraversal(filename) {
		return false
	}
	return true
}

// containsPathTraversal 检查是否包含路径遍历字符
func containsPathTraversal(s string) bool {
	return len(s) > 0 && (s[0] == '/' || s[0] == '\\' ||
		(len(s) > 1 && s[0] == '.' && (s[1] == '.' || s[1] == '/' || s[1] == '\\')))
}

// RunMigrations 运行数据库迁移
// 迁移文件已嵌入到二进制中，不需要外部 migrations 目录
func RunMigrations(_ string) error {
	// 创建迁移记录表
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// 从嵌入的文件系统中读取迁移文件
	entries, err := fs.ReadDir(collector.MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to read embedded migrations directory: %w", err)
	}

	// 执行每个迁移文件
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		// 安全检查：确保文件名只包含有效字符
		if !isValidMigrationFilename(filename) {
			continue
		}
		version := filename[:len(filename)-4] // 移除 .sql 扩展名

		// 检查是否已应用
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}
		if count > 0 {
			continue
		}

		// 从嵌入文件系统中读取迁移文件
		// 注意：embed.FS 始终使用正斜杠 / 作为路径分隔符，不能使用 filepath.Join
		content, err := fs.ReadFile(collector.MigrationsFS, "migrations/"+filename)
		if err != nil {
			return fmt.Errorf("failed to read embedded migration file %s: %w", filename, err)
		}

		// 执行迁移
		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", filename, err)
		}

		// 记录迁移
		if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			return fmt.Errorf("failed to record migration: %w", err)
		}
	}

	return nil
}
