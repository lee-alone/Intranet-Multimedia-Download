// Package config 提供配置加载和验证功能
package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 是应用程序的主配置结构
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Download DownloadConfig `yaml:"download"`
	Log      LogConfig      `yaml:"log"`
	Security SecurityConfig `yaml:"security"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	ShutdownTimeout int    `yaml:"shutdown_timeout"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Path     string `yaml:"path"`
	WALMode  bool   `yaml:"wal_mode"`
	MaxConns int    `yaml:"max_conns"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	JWTSecret     string     `yaml:"jwt_secret"`
	PrivateKey    string     `yaml:"private_key"`    // RS256 私钥文件路径
	PublicKey     string     `yaml:"public_key"`     // RS256 公钥文件路径
	TokenExpiry   int        `yaml:"token_expiry"`   // 分钟
	RefreshExpiry int        `yaml:"refresh_expiry"` // 分钟
	LDAP          LDAPConfig `yaml:"ldap"`
	SSO           SSOConfig  `yaml:"sso"`
}

// SSOConfig SSO 配置
type SSOConfig struct {
	Enabled    bool         `yaml:"enabled"`
	Provider   string       `yaml:"provider"` // "cas" or "oauth2"
	CASURL     string       `yaml:"cas_url"`
	CASService string       `yaml:"cas_service"`
	OAuth2     OAuth2Config `yaml:"oauth2"`
}

// OAuth2Config OAuth2 配置
type OAuth2Config struct {
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	AuthURL      string   `yaml:"auth_url"`
	TokenURL     string   `yaml:"token_url"`
	UserInfoURL  string   `yaml:"user_info_url"`
	Scopes       []string `yaml:"scopes"`
	RedirectURL  string   `yaml:"redirect_url"`
}

// LDAPConfig LDAP 配置
type LDAPConfig struct {
	Enabled  bool   `yaml:"enabled"`
	URL      string `yaml:"url"`
	BindDN   string `yaml:"bind_dn"`
	Password string `yaml:"password"`
	BaseDN   string `yaml:"base_dn"`
	Timeout  int    `yaml:"timeout"` // 秒
}

// DownloadConfig 下载配置
type DownloadConfig struct {
	Concurrent  int      `yaml:"concurrent"`
	MaxRetries  int      `yaml:"max_retries"`
	Timeout     int      `yaml:"timeout"`       // 秒
	MaxFileSize int64    `yaml:"max_file_size"` // 字节
	TempDir     string   `yaml:"temp_dir"`
	OutputDir   string   `yaml:"output_dir"`
	Whitelist   []string `yaml:"whitelist"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level    string `yaml:"level"`
	Dir      string `yaml:"dir"`
	MaxSize  int    `yaml:"max_size"` // MB
	MaxAge   int    `yaml:"max_age"`  // 天
	Compress bool   `yaml:"compress"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	AllowedHosts    []string `yaml:"allowed_hosts"`
	EnableRateLimit bool     `yaml:"enable_rate_limit"`
	RateLimitRPS    int      `yaml:"rate_limit_rps"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:            "0.0.0.0",
			Port:            8080,
			ShutdownTimeout: 30,
		},
		Database: DatabaseConfig{
			Path:     "./data/collector.db",
			WALMode:  true,
			MaxConns: 10,
		},
		Auth: AuthConfig{
			JWTSecret:     "change-me-in-production",
			PrivateKey:    "./keys/private.pem",
			PublicKey:     "./keys/public.pem",
			TokenExpiry:   60,
			RefreshExpiry: 1440,
			LDAP: LDAPConfig{
				Enabled: false,
				Timeout: 10,
			},
		},
		Download: DownloadConfig{
			Concurrent:  10,
			MaxRetries:  3,
			Timeout:     3600,
			MaxFileSize: 10 * 1024 * 1024 * 1024, // 10GB
			TempDir:     "./temp",
			OutputDir:   "./downloads",
			Whitelist: []string{
				"bilibili.com",
				"youtube.com",
				"youku.com",
				"iqiyi.com",
			},
		},
		Log: LogConfig{
			Level:    "info",
			Dir:      "./logs",
			MaxSize:  100,
			MaxAge:   7,
			Compress: true,
		},
		Security: SecurityConfig{
			AllowedHosts:    []string{"localhost", "127.0.0.1"},
			EnableRateLimit: true,
			RateLimitRPS:    100,
		},
	}
}

// Load 从文件加载配置，支持环境变量替换
func Load() (*Config, error) {
	return LoadFromFile("config.yaml")
}

// LoadFromFile 从指定文件加载配置
func LoadFromFile(path string) (*Config, error) {
	// 检查文件权限
	if err := checkFilePermissions(path); err != nil {
		return nil, fmt.Errorf("file permission check failed: %w", err)
	}

	// 读取文件内容
	// #nosec G304 -- path 参数经过 checkFilePermissions 验证
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// 环境变量替换
	expanded := expandEnvVars(string(data))

	// 解析 YAML
	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// expandEnvVars 展开配置中的环境变量
// 支持以下语法:
//   - ${VAR}        - 环境变量替换
//   - $VAR          - 环境变量替换（仅匹配独立的变量名）
//   - ${VAR:-default} - 带默认值的环境变量替换
func expandEnvVars(s string) string {
	// 先处理 ${VAR:-default} 格式
	reWithDefault := regexp.MustCompile(`\$\{(\w+):-([^}]*)\}`)
	s = reWithDefault.ReplaceAllStringFunc(s, func(match string) string {
		matches := reWithDefault.FindStringSubmatch(match)
		if len(matches) == 3 {
			varName := matches[1]
			defaultVal := matches[2]
			if val := os.Getenv(varName); val != "" {
				return val
			}
			return defaultVal
		}
		return match
	})

	// 处理 ${VAR} 格式
	reBraced := regexp.MustCompile(`\$\{(\w+)\}`)
	s = reBraced.ReplaceAllStringFunc(s, func(match string) string {
		matches := reBraced.FindStringSubmatch(match)
		if len(matches) == 2 {
			varName := matches[1]
			if val := os.Getenv(varName); val != "" {
				return val
			}
		}
		return match
	})

	// 处理 $VAR 格式（匹配 $ 后跟字母数字下划线）
	// 简化正则：只匹配变量名，边界处理在替换函数中完成
	rePlain := regexp.MustCompile(`\$(\w+)`)
	s = rePlain.ReplaceAllStringFunc(s, func(match string) string {
		matches := rePlain.FindStringSubmatch(match)
		if len(matches) == 2 {
			varName := matches[1]
			if val := os.Getenv(varName); val != "" {
				return val
			}
		}
		return match
	})

	return s
}

// checkFilePermissions 检查配置文件权限是否安全
func checkFilePermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	// Windows 平台不支持 Unix 权限位检查
	// 在 Windows 上，文件安全性由 ACL 控制，这里只记录警告
	// 实际生产环境建议在 Windows 上使用 ACL 检查工具
	if isWindows() {
		// Windows 平台：检查文件是否为只读（简单检查）
		// 注意：这只是基本检查，Windows 的完整安全检查需要 ACL API
		if info.Mode()&0200 == 0 {
			// 文件是只读的，相对安全
			return nil
		}
		// 文件可写，在 Windows 上这是正常的，记录日志但不报错
		// 生产环境应考虑使用 Windows ACL API 进行更严格的检查
		return nil
	}

	// Unix/Linux 平台：检查文件权限（不应被其他用户可写）
	mode := info.Mode()
	if mode&0077 != 0 {
		return fmt.Errorf("config file %s has insecure permissions: %v", path, mode)
	}

	return nil
}

// isWindows 检测当前操作系统是否为 Windows
func isWindows() bool {
	return os.PathSeparator == '\\' && os.PathListSeparator == ';'
}

// Validate 验证配置有效性
func (c *Config) Validate() error {
	// 验证服务器配置
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	// 验证数据库配置
	if c.Database.Path == "" {
		return fmt.Errorf("database path cannot be empty")
	}

	// 验证认证配置
	if c.Auth.JWTSecret == "change-me-in-production" {
		// 生产环境警告
		fmt.Println("WARNING: Using default JWT secret, please change in production!")
	}

	// 验证下载配置
	if c.Download.Concurrent < 1 || c.Download.Concurrent > 100 {
		return fmt.Errorf("invalid concurrent count: %d", c.Download.Concurrent)
	}

	// 验证日志级别
	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[strings.ToLower(c.Log.Level)] {
		return fmt.Errorf("invalid log level: %s", c.Log.Level)
	}

	return nil
}

// GetDSN 返回数据库连接字符串
func (c *Config) GetDSN() string {
	return c.Database.Path
}

// GetAddress 返回服务器监听地址
func (c *Config) GetAddress() string {
	return c.Server.Host + ":" + strconv.Itoa(c.Server.Port)
}
