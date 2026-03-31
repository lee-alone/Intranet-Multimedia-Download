// Package config 提供配置加载功能
package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config 系统配置
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Download DownloadConfig `yaml:"download"`
	Log      LogConfig      `yaml:"log"`
	Security SecurityConfig `yaml:"security"`
	Alert    AlertConfig    `yaml:"alert"`
	execDir  string // 二进制文件所在目录
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
	PrivateKey    string             `yaml:"private_key"`
	PublicKey     string             `yaml:"public_key"`
	TokenExpiry   int                `yaml:"token_expiry"`
	RefreshExpiry int                `yaml:"refresh_expiry"`
	LDAP          LDAPConfig         `yaml:"ldap"`
	SSO           SSOConfig          `yaml:"sso"`
	DefaultAdmin  DefaultAdminConfig `yaml:"default_admin"`
}

// DefaultAdminConfig 默认管理员配置
type DefaultAdminConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Email    string `yaml:"email"`
}

// DownloadConfig 下载配置
type DownloadConfig struct {
	Concurrent int      `yaml:"concurrent"`
	MaxRetries int      `yaml:"max_retries"`
	Timeout    int      `yaml:"timeout"`
	MaxSize    int64    `yaml:"max_file_size"`
	TempDir    string   `yaml:"temp_dir"`
	OutputDir  string   `yaml:"output_dir"`
	Whitelist  []string `yaml:"whitelist"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level    string `yaml:"level"`
	Dir      string `yaml:"dir"`
	MaxSize  int    `yaml:"max_size"`
	MaxAge   int    `yaml:"max_age"`
	Compress bool   `yaml:"compress"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	AllowedHosts    []string `yaml:"allowed_hosts"`
	EnableRateLimit bool     `yaml:"enable_rate_limit"`
	RateLimitRPS    int      `yaml:"rate_limit_rps"`
}

// AlertConfig 告警配置
type AlertConfig struct {
	EnableDiskAlert   bool     `yaml:"enable_disk_alert"`
	DiskThreshold     float64  `yaml:"disk_threshold"`
	CheckInterval     int      `yaml:"check_interval"`
	EnableWebhook     bool     `yaml:"enable_webhook"`
	WebhookURL        string   `yaml:"webhook_url"`
	WebhookType       string   `yaml:"webhook_type"`
	EnableEmail       bool     `yaml:"enable_email"`
	EmailSMTPServer   string   `yaml:"email_smtp_server"`
	EmailSMTPPort     int      `yaml:"email_smtp_port"`
	EmailFrom         string   `yaml:"email_from"`
	EmailPassword     string   `yaml:"email_password"`
	EmailTo           []string `yaml:"email_to"`
	EmailUseTLS       bool     `yaml:"email_use_tls"`
	EmailAuthType     string   `yaml:"email_auth_type"`
	EnableLogAlert    bool     `yaml:"enable_log_alert"`
	LogAlertThreshold int      `yaml:"log_alert_threshold"`
}

// LDAPConfig LDAP 配置
type LDAPConfig struct {
	Enabled  bool   `yaml:"enabled"`
	URL      string `yaml:"url"`
	BindDN   string `yaml:"bind_dn"`
	Password string `yaml:"password"`
	BaseDN   string `yaml:"base_dn"`
	Timeout  int    `yaml:"timeout"`
}

// SSOConfig SSO 配置
type SSOConfig struct {
	Enabled    bool         `yaml:"enabled"`
	Provider   string       `yaml:"provider"`
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

// AlertEmailConfig 告警邮件配置
type AlertEmailConfig struct {
	Enabled    bool     `yaml:"enabled"`
	SMTPServer string   `yaml:"smtp_server"`
	SMTPPort   int      `yaml:"smtp_port"`
	From       string   `yaml:"from"`
	Password   string   `yaml:"password"`
	To         []string `yaml:"to"`
}

// GetAddress 获取服务器地址
func (c *Config) GetAddress() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// convertPathsToAbsolute 将相对路径转换为绝对路径（相对于二进制文件目录）
func (c *Config) convertPathsToAbsolute() {
	if c.execDir == "" {
		return
	}

	log.Printf("Config execDir: %s", c.execDir)

	// 转换认证相关路径
	if !filepath.IsAbs(c.Auth.PrivateKey) {
		oldPath := c.Auth.PrivateKey
		c.Auth.PrivateKey = filepath.Join(c.execDir, c.Auth.PrivateKey)
		log.Printf("Converted private key path: %s -> %s", oldPath, c.Auth.PrivateKey)
	}
	if !filepath.IsAbs(c.Auth.PublicKey) {
		oldPath := c.Auth.PublicKey
		c.Auth.PublicKey = filepath.Join(c.execDir, c.Auth.PublicKey)
		log.Printf("Converted public key path: %s -> %s", oldPath, c.Auth.PublicKey)
	}

	// 转换数据库路径
	if !filepath.IsAbs(c.Database.Path) {
		oldPath := c.Database.Path
		c.Database.Path = filepath.Join(c.execDir, c.Database.Path)
		log.Printf("Converted database path: %s -> %s", oldPath, c.Database.Path)
	}

	// 转换日志目录
	if !filepath.IsAbs(c.Log.Dir) {
		oldPath := c.Log.Dir
		c.Log.Dir = filepath.Join(c.execDir, c.Log.Dir)
		log.Printf("Converted log dir: %s -> %s", oldPath, c.Log.Dir)
	}

	// 转换下载相关路径
	if !filepath.IsAbs(c.Download.TempDir) {
		oldPath := c.Download.TempDir
		c.Download.TempDir = filepath.Join(c.execDir, c.Download.TempDir)
		log.Printf("Converted temp dir: %s -> %s", oldPath, c.Download.TempDir)
	}
	if !filepath.IsAbs(c.Download.OutputDir) {
		oldPath := c.Download.OutputDir
		c.Download.OutputDir = filepath.Join(c.execDir, c.Download.OutputDir)
		log.Printf("Converted output dir: %s -> %s", oldPath, c.Download.OutputDir)
	}
}

// Load 加载配置文件
func Load() (*Config, error) {
	// 获取二进制文件所在目录
	execPath, err := os.Executable()
	if err != nil {
		execPath, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get executable path: %w", err)
		}
	}
	execDir := filepath.Dir(execPath)

	configPath := "config.yaml"

	// 检查配置文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 尝试使用示例配置
		examplePath := "config.yaml.example"
		if _, err := os.Stat(examplePath); err == nil {
			configPath = examplePath
		}
	}

	// 读取配置文件
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// 解析配置
	var cfg Config
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// 保存执行目录
	cfg.execDir = execDir

	// 设置默认值
	setDefaults(&cfg)

	// 将相对路径转换为绝对路径（相对于二进制文件目录）
	cfg.convertPathsToAbsolute()

	return &cfg, nil
}

// setDefaults 设置默认值
func setDefaults(cfg *Config) {
	// 服务器配置
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.ShutdownTimeout == 0 {
		cfg.Server.ShutdownTimeout = 30
	}

	// 数据库配置
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./data/collector.db"
	}
	if cfg.Database.MaxConns == 0 {
		cfg.Database.MaxConns = 10
	}

	// 认证配置
	if cfg.Auth.PrivateKey == "" {
		cfg.Auth.PrivateKey = "./keys/private.pem"
	}
	if cfg.Auth.PublicKey == "" {
		cfg.Auth.PublicKey = "./keys/public.pem"
	}
	if cfg.Auth.TokenExpiry == 0 {
		cfg.Auth.TokenExpiry = 60
	}
	if cfg.Auth.RefreshExpiry == 0 {
		cfg.Auth.RefreshExpiry = 1440
	}

	// 默认管理员配置
	if !cfg.Auth.DefaultAdmin.Enabled {
		cfg.Auth.DefaultAdmin.Enabled = true
	}
	if cfg.Auth.DefaultAdmin.Username == "" {
		cfg.Auth.DefaultAdmin.Username = "admin"
	}
	if cfg.Auth.DefaultAdmin.Password == "" {
		cfg.Auth.DefaultAdmin.Password = "admin123"
	}
	if cfg.Auth.DefaultAdmin.Email == "" {
		cfg.Auth.DefaultAdmin.Email = "admin@localhost"
	}

	// 下载配置
	if cfg.Download.Concurrent == 0 {
		cfg.Download.Concurrent = 10
	}
	if cfg.Download.MaxRetries == 0 {
		cfg.Download.MaxRetries = 3
	}
	if cfg.Download.Timeout == 0 {
		cfg.Download.Timeout = 3600
	}
	if cfg.Download.MaxSize == 0 {
		cfg.Download.MaxSize = 10737418240 // 10GB
	}
	if cfg.Download.TempDir == "" {
		cfg.Download.TempDir = "./temp"
	}
	if cfg.Download.OutputDir == "" {
		cfg.Download.OutputDir = "./downloads"
	}

	// 日志配置
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Dir == "" {
		cfg.Log.Dir = "./logs"
	}
	if cfg.Log.MaxSize == 0 {
		cfg.Log.MaxSize = 100
	}
	if cfg.Log.MaxAge == 0 {
		cfg.Log.MaxAge = 7
	}

	// 安全配置
	if cfg.Security.AllowedHosts == nil {
		cfg.Security.AllowedHosts = []string{"localhost", "127.0.0.1"}
	}
	if cfg.Security.EnableRateLimit {
		if cfg.Security.RateLimitRPS == 0 {
			cfg.Security.RateLimitRPS = 100
		}
	}

	// 告警配置
	if cfg.Alert.CheckInterval == 0 {
		cfg.Alert.CheckInterval = 5
	}
	if cfg.Alert.DiskThreshold == 0 {
		cfg.Alert.DiskThreshold = 90
	}
}

// EnsureConfigFile 确保配置文件存在
func EnsureConfigFile() error {
	configPath := "config.yaml"

	// 检查配置文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 复制示例配置
		examplePath := "config.yaml.example"
		if _, err := os.Stat(examplePath); err == nil {
			content, err := os.ReadFile(examplePath)
			if err != nil {
				return fmt.Errorf("failed to read example config: %w", err)
			}
			if err := os.WriteFile(configPath, content, 0644); err != nil {
				return fmt.Errorf("failed to create config file: %w", err)
			}
		}
	}

	return nil
}

// GetConfigPath 获取配置文件路径
func GetConfigPath() string {
	configPath := "config.yaml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		examplePath := "config.yaml.example"
		if _, err := os.Stat(examplePath); err == nil {
			return examplePath
		}
	}
	return configPath
}

// GetDataDir 获取数据目录
func GetDataDir() string {
	cfg, err := Load()
	if err != nil {
		return "./data"
	}
	return filepath.Dir(cfg.Database.Path)
}
