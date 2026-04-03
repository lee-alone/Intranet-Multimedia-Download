// Package config 提供配置加载和验证功能的测试
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetAddress(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
	}

	expected := "127.0.0.1:8080"
	if addr := cfg.GetAddress(); addr != expected {
		t.Errorf("GetAddress() = %q, want %q", addr, expected)
	}
}

func TestLoad(t *testing.T) {
	t.Run("成功加载配置文件", func(t *testing.T) {
		// 保存当前工作目录
		originalWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Failed to get working directory: %v", err)
		}
		defer os.Chdir(originalWd)

		// 创建临时目录
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change to temp directory: %v", err)
		}

		// 创建测试配置文件
		configContent := `
server:
  host: "127.0.0.1"
  port: 9090

database:
  path: "./test.db"
  wal_mode: false
  max_conns: 5

auth:
  private_key: "./keys/private.pem"
  public_key: "./keys/public.pem"
  token_expiry: 30
  refresh_expiry: 720

download:
  concurrent: 5
  max_retries: 2
  timeout: 1800
  max_file_size: 5368709120
  output_dir: "./output"
  whitelist:
    - "example.com"

log:
  level: "debug"
  dir: "./logs"
  max_size: 50
  max_age: 3
  compress: false

security:
  allowed_hosts:
    - "localhost"
  enable_rate_limit: false
  rate_limit_rps: 50
`
		if err := os.WriteFile("config.yaml", []byte(configContent), 0600); err != nil {
			t.Fatalf("Failed to create config file: %v", err)
		}

		// 加载配置
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() failed: %v", err)
		}

		// 验证配置值
		if cfg.Server.Host != "127.0.0.1" {
			t.Errorf("Server.Host = %q, want 127.0.0.1", cfg.Server.Host)
		}
		if cfg.Server.Port != 9090 {
			t.Errorf("Server.Port = %d, want 9090", cfg.Server.Port)
		}
		if cfg.Database.Path == "" {
			t.Error("Database.Path should not be empty")
		}
		if cfg.Download.Concurrent != 5 {
			t.Errorf("Download.Concurrent = %d, want 5", cfg.Download.Concurrent)
		}
		if cfg.Log.Level != "debug" {
			t.Errorf("Log.Level = %q, want debug", cfg.Log.Level)
		}
	})

	t.Run("配置文件不存在时使用示例配置", func(t *testing.T) {
		// 保存当前工作目录
		originalWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Failed to get working directory: %v", err)
		}
		defer os.Chdir(originalWd)

		// 创建临时目录
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change to temp directory: %v", err)
		}

		// 创建示例配置文件
		configContent := `
server:
  host: "0.0.0.0"
  port: 8080

database:
  path: "./data/collector.db"

auth:
  private_key: "./keys/private.pem"
  public_key: "./keys/public.pem"

download:
  concurrent: 10

log:
  level: "info"
`
		if err := os.WriteFile("config.yaml.example", []byte(configContent), 0600); err != nil {
			t.Fatalf("Failed to create example config file: %v", err)
		}

		// 加载配置（应该使用示例配置）
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() failed: %v", err)
		}

		// 验证默认值已设置
		if cfg.Server.Port != 8080 {
			t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
		}
	})

	t.Run("无效的 YAML 格式", func(t *testing.T) {
		// 保存当前工作目录
		originalWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Failed to get working directory: %v", err)
		}
		defer os.Chdir(originalWd)

		// 创建临时目录
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change to temp directory: %v", err)
		}

		// 创建无效的 YAML 文件
		invalidContent := `
server:
  host: "localhost"
  port: [invalid
`
		if err := os.WriteFile("config.yaml", []byte(invalidContent), 0600); err != nil {
			t.Fatalf("Failed to create config file: %v", err)
		}

		_, err = Load()
		if err == nil {
			t.Error("Load() should fail for invalid YAML")
		}
	})
}

func TestSetDefaults(t *testing.T) {
	// 测试默认值设置
	cfg := &Config{}
	setDefaults(cfg)

	// 验证服务器默认值
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Default server host = %q, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Default server port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Server.ShutdownTimeout != 30 {
		t.Errorf("Default shutdown timeout = %d, want 30", cfg.Server.ShutdownTimeout)
	}

	// 验证数据库默认值
	if cfg.Database.Path != "./data/collector.db" {
		t.Errorf("Default database path = %q, want ./data/collector.db", cfg.Database.Path)
	}
	if cfg.Database.MaxConns != 10 {
		t.Errorf("Default max connections = %d, want 10", cfg.Database.MaxConns)
	}

	// 验证认证默认值
	if cfg.Auth.PrivateKey != "./keys/private.pem" {
		t.Errorf("Default private key = %q, want ./keys/private.pem", cfg.Auth.PrivateKey)
	}
	if cfg.Auth.PublicKey != "./keys/public.pem" {
		t.Errorf("Default public key = %q, want ./keys/public.pem", cfg.Auth.PublicKey)
	}
	if cfg.Auth.TokenExpiry != 60 {
		t.Errorf("Default token expiry = %d, want 60", cfg.Auth.TokenExpiry)
	}
	if cfg.Auth.RefreshExpiry != 1440 {
		t.Errorf("Default refresh expiry = %d, want 1440", cfg.Auth.RefreshExpiry)
	}

	// 验证下载默认值
	if cfg.Download.Concurrent != 10 {
		t.Errorf("Default concurrent = %d, want 10", cfg.Download.Concurrent)
	}
	if cfg.Download.MaxRetries != 3 {
		t.Errorf("Default max retries = %d, want 3", cfg.Download.MaxRetries)
	}
	if cfg.Download.Timeout != 3600 {
		t.Errorf("Default timeout = %d, want 3600", cfg.Download.Timeout)
	}
	if cfg.Download.MaxSize != 10737418240 {
		t.Errorf("Default max size = %d, want 10737418240", cfg.Download.MaxSize)
	}
	if cfg.Download.OutputDir != "./downloads" {
		t.Errorf("Default output dir = %q, want ./downloads", cfg.Download.OutputDir)
	}

	// 验证日志默认值
	if cfg.Log.Level != "info" {
		t.Errorf("Default log level = %q, want info", cfg.Log.Level)
	}
	if cfg.Log.Dir != "./logs" {
		t.Errorf("Default log dir = %q, want ./logs", cfg.Log.Dir)
	}
	if cfg.Log.MaxSize != 100 {
		t.Errorf("Default log max size = %d, want 100", cfg.Log.MaxSize)
	}
	if cfg.Log.MaxAge != 7 {
		t.Errorf("Default log max age = %d, want 7", cfg.Log.MaxAge)
	}

	// 验证告警默认值
	if cfg.Alert.CheckInterval != 5 {
		t.Errorf("Default alert check interval = %d, want 5", cfg.Alert.CheckInterval)
	}
	if cfg.Alert.DiskThreshold != 90 {
		t.Errorf("Default disk threshold = %f, want 90", cfg.Alert.DiskThreshold)
	}
}

func TestConvertPathsToAbsolute(t *testing.T) {
	execDir := "/app"
	cfg := &Config{
		execDir: execDir,
		Database: DatabaseConfig{
			Path: "./data/test.db",
		},
		Auth: AuthConfig{
			PrivateKey: "./keys/private.pem",
			PublicKey:  "./keys/public.pem",
		},
		Log: LogConfig{
			Dir: "./logs",
		},
		Download: DownloadConfig{
			OutputDir: "./downloads",
		},
	}

	cfg.convertPathsToAbsolute()

	// 验证路径已转换为绝对路径（使用 filepath.Join 以兼容不同操作系统）
	if cfg.Database.Path != filepath.Join(execDir, "data", "test.db") {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, filepath.Join(execDir, "data", "test.db"))
	}
	if cfg.Auth.PrivateKey != filepath.Join(execDir, "keys", "private.pem") {
		t.Errorf("Auth.PrivateKey = %q, want %q", cfg.Auth.PrivateKey, filepath.Join(execDir, "keys", "private.pem"))
	}
	if cfg.Auth.PublicKey != filepath.Join(execDir, "keys", "public.pem") {
		t.Errorf("Auth.PublicKey = %q, want %q", cfg.Auth.PublicKey, filepath.Join(execDir, "keys", "public.pem"))
	}
	if cfg.Log.Dir != filepath.Join(execDir, "logs") {
		t.Errorf("Log.Dir = %q, want %q", cfg.Log.Dir, filepath.Join(execDir, "logs"))
	}
	if cfg.Download.OutputDir != filepath.Join(execDir, "downloads") {
		t.Errorf("Download.OutputDir = %q, want %q", cfg.Download.OutputDir, filepath.Join(execDir, "downloads"))
	}
}

func TestConvertPathsToAbsoluteWithEmptyExecDir(t *testing.T) {
	cfg := &Config{
		execDir: "", // 空的 execDir
		Database: DatabaseConfig{
			Path: "./data/test.db",
		},
	}

	originalPath := cfg.Database.Path
	cfg.convertPathsToAbsolute()

	// 当 execDir 为空时，路径不应改变
	if cfg.Database.Path != originalPath {
		t.Errorf("Database.Path should not change when execDir is empty, got %q, want %q", cfg.Database.Path, originalPath)
	}
}

func TestConvertPathsToAbsoluteWithAbsolutePath(t *testing.T) {
	// 在 Windows 上，绝对路径需要驱动器字母（如 C:\）
	// 在 Unix 上，绝对路径以 / 开头
	// 为了跨平台兼容，我们跳过此测试，只验证相对路径的转换
	// 这个测试主要验证当路径已经是绝对路径时，不会被重复转换

	// 创建一个临时目录作为 execDir
	tmpDir := t.TempDir()

	// 构建一个绝对路径（使用临时目录确保路径存在）
	absPath := filepath.Join(tmpDir, "test.db")

	cfg := &Config{
		execDir: "/app",
		Database: DatabaseConfig{
			Path: absPath,
		},
	}

	cfg.convertPathsToAbsolute()

	// 绝对路径应保持不变
	if cfg.Database.Path != absPath {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, absPath)
	}
}

func TestGetBaseDir(t *testing.T) {
	baseDir := GetBaseDir()
	if baseDir == "" {
		t.Error("GetBaseDir() returned empty string")
	}
	// 多次调用应返回相同结果（sync.Once）
	baseDir2 := GetBaseDir()
	if baseDir != baseDir2 {
		t.Errorf("GetBaseDir() returned different values: %q and %q", baseDir, baseDir2)
	}
}

func TestEnsureConfigFile(t *testing.T) {
	t.Run("配置文件已存在", func(t *testing.T) {
		// 保存当前工作目录
		originalWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Failed to get working directory: %v", err)
		}
		defer os.Chdir(originalWd)

		// 创建临时目录
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change to temp directory: %v", err)
		}

		// 创建配置文件
		if err := os.WriteFile("config.yaml", []byte("test: value"), 0644); err != nil {
			t.Fatalf("Failed to create config file: %v", err)
		}

		err = EnsureConfigFile()
		if err != nil {
			t.Errorf("EnsureConfigFile() should not fail when config exists: %v", err)
		}
	})

	t.Run("从示例配置创建", func(t *testing.T) {
		// 保存当前工作目录
		originalWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Failed to get working directory: %v", err)
		}
		defer os.Chdir(originalWd)

		// 创建临时目录
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change to temp directory: %v", err)
		}

		// 创建示例配置文件
		if err := os.WriteFile("config.yaml.example", []byte("test: value"), 0644); err != nil {
			t.Fatalf("Failed to create example config file: %v", err)
		}

		err = EnsureConfigFile()
		if err != nil {
			t.Errorf("EnsureConfigFile() failed: %v", err)
		}

		// 验证配置文件已创建
		if _, err := os.Stat("config.yaml"); os.IsNotExist(err) {
			t.Error("config.yaml should have been created from example")
		}
	})
}

func TestGetConfigPath(t *testing.T) {
	t.Run("配置文件存在", func(t *testing.T) {
		// 保存当前工作目录
		originalWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Failed to get working directory: %v", err)
		}
		defer os.Chdir(originalWd)

		// 创建临时目录
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change to temp directory: %v", err)
		}

		// 创建配置文件
		if err := os.WriteFile("config.yaml", []byte("test: value"), 0644); err != nil {
			t.Fatalf("Failed to create config file: %v", err)
		}

		path := GetConfigPath()
		if path != "config.yaml" {
			t.Errorf("GetConfigPath() = %q, want config.yaml", path)
		}
	})

	t.Run("使用示例配置", func(t *testing.T) {
		// 保存当前工作目录
		originalWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Failed to get working directory: %v", err)
		}
		defer os.Chdir(originalWd)

		// 创建临时目录
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change to temp directory: %v", err)
		}

		// 只创建示例配置文件
		if err := os.WriteFile("config.yaml.example", []byte("test: value"), 0644); err != nil {
			t.Fatalf("Failed to create example config file: %v", err)
		}

		path := GetConfigPath()
		if path != "config.yaml.example" {
			t.Errorf("GetConfigPath() = %q, want config.yaml.example", path)
		}
	})
}
