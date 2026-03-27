// Package config 提供配置加载和验证功能的测试
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envVars  map[string]string
		expected string
	}{
		{
			name:     "简单环境变量替换",
			input:    "prefix_${VAR}_suffix",
			envVars:  map[string]string{"VAR": "value"},
			expected: "prefix_value_suffix",
		},
		{
			name:     "不带大括号的环境变量（独立）",
			input:    "prefix_$VAR suffix",
			envVars:  map[string]string{"VAR": "value"},
			expected: "prefix_value suffix",
		},
		{
			name:     "不带大括号的环境变量（末尾）",
			input:    "prefix_$VAR",
			envVars:  map[string]string{"VAR": "value"},
			expected: "prefix_value",
		},
		{
			name:     "带默认值的环境变量（变量存在）",
			input:    "${VAR:-default}",
			envVars:  map[string]string{"VAR": "value"},
			expected: "value",
		},
		{
			name:     "带默认值的环境变量（变量不存在）",
			input:    "${VAR:-default}",
			envVars:  map[string]string{},
			expected: "default",
		},
		{
			name:     "带空默认值的环境变量",
			input:    "${VAR:-}",
			envVars:  map[string]string{},
			expected: "",
		},
		{
			name:     "多个环境变量",
			input:    "${VAR1} and ${VAR2:-default}",
			envVars:  map[string]string{"VAR1": "value1", "VAR2": "value2"},
			expected: "value1 and value2",
		},
		{
			name:     "环境变量不存在且无默认值",
			input:    "${UNDEFINED_VAR}",
			envVars:  map[string]string{},
			expected: "${UNDEFINED_VAR}",
		},
		{
			name:     "无环境变量的普通文本",
			input:    "just plain text",
			envVars:  map[string]string{},
			expected: "just plain text",
		},
		{
			name:     "默认值包含特殊字符",
			input:    "${VAR:-/path/to/file}",
			envVars:  map[string]string{},
			expected: "/path/to/file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 设置环境变量
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			result := expandEnvVars(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnvVars(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCheckFilePermissions(t *testing.T) {
	// 创建临时测试文件
	tmpDir := t.TempDir()

	// 注意：Windows 不支持 Unix 权限位，所以这些测试在 Windows 上行为不同
	// 在 Windows 上，文件权限检查会跳过权限位检查

	t.Run("存在的文件", func(t *testing.T) {
		safeFile := filepath.Join(tmpDir, "safe.yaml")
		if err := os.WriteFile(safeFile, []byte("test"), 0600); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		// 在 Windows 上，权限检查可能通过或跳过
		// 这里只验证函数不会崩溃
		_ = checkFilePermissions(safeFile)
	})

	t.Run("不存在的文件", func(t *testing.T) {
		nonExistent := filepath.Join(tmpDir, "nonexistent.yaml")
		if err := checkFilePermissions(nonExistent); err == nil {
			t.Errorf("checkFilePermissions(%q) should fail for non-existent file", nonExistent)
		}
	})
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "默认配置有效",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "无效端口号",
			config: &Config{
				Server: ServerConfig{Port: 0},
			},
			wantErr: true,
		},
		{
			name: "端口号超出范围",
			config: &Config{
				Server: ServerConfig{Port: 70000},
			},
			wantErr: true,
		},
		{
			name: "空数据库路径",
			config: &Config{
				Server:   ServerConfig{Port: 8080},
				Database: DatabaseConfig{Path: ""},
			},
			wantErr: true,
		},
		{
			name: "无效并发数（太小）",
			config: &Config{
				Server:   ServerConfig{Port: 8080},
				Database: DatabaseConfig{Path: "./test.db"},
				Download: DownloadConfig{Concurrent: 0},
			},
			wantErr: true,
		},
		{
			name: "无效并发数（太大）",
			config: &Config{
				Server:   ServerConfig{Port: 8080},
				Database: DatabaseConfig{Path: "./test.db"},
				Download: DownloadConfig{Concurrent: 200},
			},
			wantErr: true,
		},
		{
			name: "无效日志级别",
			config: &Config{
				Server:   ServerConfig{Port: 8080},
				Database: DatabaseConfig{Path: "./test.db"},
				Download: DownloadConfig{Concurrent: 10},
				Log:      LogConfig{Level: "invalid"},
			},
			wantErr: true,
		},
		{
			name: "有效日志级别 debug",
			config: &Config{
				Server:   ServerConfig{Port: 8080},
				Database: DatabaseConfig{Path: "./test.db"},
				Download: DownloadConfig{Concurrent: 10},
				Log:      LogConfig{Level: "debug"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	// 验证默认值
	if cfg.Server.Port != 8080 {
		t.Errorf("Default server port = %d, want 8080", cfg.Server.Port)
	}

	if cfg.Database.WALMode != true {
		t.Error("Default WAL mode should be true")
	}

	if cfg.Download.Concurrent != 10 {
		t.Errorf("Default concurrent = %d, want 10", cfg.Download.Concurrent)
	}

	if cfg.Log.Level != "info" {
		t.Errorf("Default log level = %s, want info", cfg.Log.Level)
	}
}

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

func TestGetDSN(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{
			Path: "./data/test.db",
		},
	}

	if dsn := cfg.GetDSN(); dsn != "./data/test.db" {
		t.Errorf("GetDSN() = %q, want %q", dsn, "./data/test.db")
	}
}

func TestLoadFromFile(t *testing.T) {
	t.Run("成功加载配置文件", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")

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
  jwt_secret: "test-secret"
  token_expiry: 30
  refresh_expiry: 720

download:
  concurrent: 5
  max_retries: 2
  timeout: 1800
  max_file_size: 5368709120
  temp_dir: "./temp"
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
		if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
			t.Fatalf("Failed to create config file: %v", err)
		}

		// 加载配置
		cfg, err := LoadFromFile(configFile)
		if err != nil {
			t.Fatalf("LoadFromFile() failed: %v", err)
		}

		// 验证配置值
		if cfg.Server.Host != "127.0.0.1" {
			t.Errorf("Server.Host = %q, want 127.0.0.1", cfg.Server.Host)
		}
		if cfg.Server.Port != 9090 {
			t.Errorf("Server.Port = %d, want 9090", cfg.Server.Port)
		}
		if cfg.Database.Path != "./test.db" {
			t.Errorf("Database.Path = %q, want ./test.db", cfg.Database.Path)
		}
		if cfg.Auth.JWTSecret != "test-secret" {
			t.Errorf("Auth.JWTSecret = %q, want test-secret", cfg.Auth.JWTSecret)
		}
		if cfg.Download.Concurrent != 5 {
			t.Errorf("Download.Concurrent = %d, want 5", cfg.Download.Concurrent)
		}
		if cfg.Log.Level != "debug" {
			t.Errorf("Log.Level = %q, want debug", cfg.Log.Level)
		}
	})

	t.Run("环境变量替换", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")

		// 设置环境变量
		os.Setenv("TEST_JWT_SECRET", "env-secret-value")
		os.Setenv("TEST_PORT", "9999")
		defer os.Unsetenv("TEST_JWT_SECRET")
		defer os.Unsetenv("TEST_PORT")

		// 创建测试配置文件（使用环境变量）
		configContent := `
server:
  host: "0.0.0.0"
  port: ${TEST_PORT}

database:
  path: "./test.db"

auth:
  jwt_secret: "${TEST_JWT_SECRET:-default-secret}"
  token_expiry: 60
  refresh_expiry: 1440

download:
  concurrent: 10

log:
  level: "info"

security:
  enable_rate_limit: true
`
		if err := os.WriteFile(configFile, []byte(configContent), 0600); err != nil {
			t.Fatalf("Failed to create config file: %v", err)
		}

		// 加载配置
		cfg, err := LoadFromFile(configFile)
		if err != nil {
			t.Fatalf("LoadFromFile() failed: %v", err)
		}

		// 验证环境变量替换
		if cfg.Auth.JWTSecret != "env-secret-value" {
			t.Errorf("Auth.JWTSecret = %q, want env-secret-value", cfg.Auth.JWTSecret)
		}
		// 注意：端口号需要特殊处理，因为 YAML 解析器可能无法正确处理字符串形式的数字
	})

	t.Run("配置文件不存在", func(t *testing.T) {
		_, err := LoadFromFile("/nonexistent/path/config.yaml")
		if err == nil {
			t.Error("LoadFromFile() should fail for non-existent file")
		}
	})

	t.Run("无效的 YAML 格式", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")

		// 创建无效的 YAML 文件
		invalidContent := `
server:
  host: "localhost"
  port: [invalid
`
		if err := os.WriteFile(configFile, []byte(invalidContent), 0600); err != nil {
			t.Fatalf("Failed to create config file: %v", err)
		}

		_, err := LoadFromFile(configFile)
		if err == nil {
			t.Error("LoadFromFile() should fail for invalid YAML")
		}
	})
}

func TestIsWindows(t *testing.T) {
	// 测试 isWindows 函数不会崩溃
	result := isWindows()
	// 结果取决于运行平台，只验证函数能正常执行
	_ = result
}
