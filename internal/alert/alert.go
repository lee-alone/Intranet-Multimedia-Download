// Package alert 提供系统告警功能，包括磁盘监控、Webhook 和邮件告警
package alert

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// AlertType 告警类型
type AlertType string

const (
	AlertTypeDisk      AlertType = "disk"       // 磁盘空间告警
	AlertTypeMemory    AlertType = "memory"     // 内存使用告警
	AlertTypeEngine    AlertType = "engine"     // 引擎故障告警
	AlertTypeDownload  AlertType = "download"   // 下载失败告警
	AlertTypeSystem    AlertType = "system"     // 系统告警
	AlertTypeLogRotate AlertType = "log_rotate" // 日志轮转告警
)

// AlertLevel 告警级别
type AlertLevel string

const (
	AlertLevelInfo     AlertLevel = "info"     // 信息
	AlertLevelWarning  AlertLevel = "warning"  // 警告
	AlertLevelError    AlertLevel = "error"    // 错误
	AlertLevelCritical AlertLevel = "critical" // 严重
)

// Alert 告警信息
type Alert struct {
	Type      AlertType      `json:"type"`
	Level     AlertLevel     `json:"level"`
	Title     string         `json:"title"`
	Message   string         `json:"message"`
	Timestamp time.Time      `json:"timestamp"`
	Details   map[string]any `json:"details,omitempty"`
}

// Config 告警配置
type Config struct {
	EnableDiskAlert   bool          `yaml:"enable_disk_alert"`   // 启用磁盘告警
	DiskThreshold     float64       `yaml:"disk_threshold"`      // 磁盘使用率阈值 (0-1)
	CheckInterval     time.Duration `yaml:"check_interval"`      // 检查间隔
	EnableWebhook     bool          `yaml:"enable_webhook"`      // 启用 Webhook
	WebhookURL        string        `yaml:"webhook_url"`         // Webhook URL
	WebhookType       string        `yaml:"webhook_type"`        // Webhook 类型：dingtalk, wechat, feishu
	EnableEmail       bool          `yaml:"enable_email"`        // 启用邮件告警
	EmailSMTPServer   string        `yaml:"email_smtp_server"`   // SMTP 服务器
	EmailSMTPPort     int           `yaml:"email_smtp_port"`     // SMTP 端口
	EmailFrom         string        `yaml:"email_from"`          // 发件人邮箱
	EmailPassword     string        `yaml:"email_password"`      // 邮箱密码/授权码
	EmailTo           []string      `yaml:"email_to"`            // 收件人列表
	EmailUseTLS       bool          `yaml:"email_use_tls"`       // 使用 TLS
	EmailAuthType     string        `yaml:"email_auth_type"`     // 认证类型：LOGIN, PLAIN
	EnableLogAlert    bool          `yaml:"enable_log_alert"`    // 启用日志告警
	LogAlertThreshold int           `yaml:"log_alert_threshold"` // 日志告警阈值（条/分钟）
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		EnableDiskAlert:   true,
		DiskThreshold:     0.8, // 80%
		CheckInterval:     5 * time.Minute,
		EnableWebhook:     false,
		WebhookType:       "dingtalk",
		EnableEmail:       false,
		EmailSMTPPort:     25,
		EmailUseTLS:       true,
		EmailAuthType:     "PLAIN",
		EnableLogAlert:    false,
		LogAlertThreshold: 100,
	}
}

// AlertManager 告警管理器
type AlertManager struct {
	mu            sync.RWMutex
	config        Config
	callbacks     []func(alert *Alert)
	httpClient    *http.Client
	stopChan      chan struct{}
	started       bool
	lastDiskAlert time.Time
	alertMutex    sync.Mutex
}

// NewAlertManager 创建新的告警管理器
func NewAlertManager(config Config) *AlertManager {
	am := &AlertManager{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		stopChan: make(chan struct{}),
	}

	return am
}

// RegisterCallback 注册告警回调
func (am *AlertManager) RegisterCallback(callback func(alert *Alert)) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.callbacks = append(am.callbacks, callback)
}

// Start 启动告警管理器
func (am *AlertManager) Start() {
	am.mu.Lock()
	if am.started {
		am.mu.Unlock()
		return
	}
	am.started = true
	am.mu.Unlock()

	// 启动磁盘检查协程
	if am.config.EnableDiskAlert {
		go am.diskCheckLoop()
	}
}

// Stop 停止告警管理器
func (am *AlertManager) Stop() {
	am.mu.Lock()
	if !am.started {
		am.mu.Unlock()
		return
	}
	am.started = false

	// 关闭停止通道（只能关闭一次）
	close(am.stopChan)
	am.mu.Unlock()
}

// diskCheckLoop 磁盘检查循环
func (am *AlertManager) diskCheckLoop() {
	// 立即检查一次
	am.checkDiskUsage()

	ticker := time.NewTicker(am.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			am.checkDiskUsage()
		case <-am.stopChan:
			return
		}
	}
}

// checkDiskUsage 检查磁盘使用率
func (am *AlertManager) checkDiskUsage() {
	am.alertMutex.Lock()
	defer am.alertMutex.Unlock()

	// 获取工作目录
	workDir, err := os.Getwd()
	if err != nil {
		return
	}

	// 获取磁盘使用情况
	usage, err := getDiskUsage(workDir)
	if err != nil {
		return
	}

	// 检查是否超过阈值
	if usage.UsedPercent >= am.config.DiskThreshold {
		// 防止告警风暴，10 分钟内只告警一次
		if time.Since(am.lastDiskAlert) < 10*time.Minute {
			return
		}
		am.lastDiskAlert = time.Now()

		alert := &Alert{
			Type:      AlertTypeDisk,
			Level:     AlertLevelWarning,
			Title:     "磁盘空间不足告警",
			Message:   fmt.Sprintf("磁盘使用率已达到 %.1f%%，超过阈值 %.0f%%", usage.UsedPercent*100, am.config.DiskThreshold*100),
			Timestamp: time.Now(),
			Details: map[string]any{
				"total":       formatSize(usage.Total),
				"used":        formatSize(usage.Used),
				"free":        formatSize(usage.Free),
				"usedPercent": fmt.Sprintf("%.1f%%", usage.UsedPercent*100),
				"path":        workDir,
			},
		}

		am.sendAlert(alert)
	}
}

// getDiskUsage 获取磁盘使用情况（跨平台）
func getDiskUsage(path string) (*diskUsage, error) {
	if runtime.GOOS == "windows" {
		return getDiskUsageWindows(path)
	}
	return getDiskUsageUnix(path)
}

// diskUsage 磁盘使用情况
type diskUsage struct {
	Total       uint64  // 总大小
	Used        uint64  // 已使用
	Free        uint64  // 剩余
	UsedPercent float64 // 使用率
}

// SendAlert 发送告警
func (am *AlertManager) SendAlert(alertType AlertType, level AlertLevel, title, message string, details map[string]any) {
	alert := &Alert{
		Type:      alertType,
		Level:     level,
		Title:     title,
		Message:   message,
		Timestamp: time.Now(),
		Details:   details,
	}
	am.sendAlert(alert)
}

// sendAlert 发送告警（内部方法）
func (am *AlertManager) sendAlert(alert *Alert) {
	am.mu.RLock()
	callbacks := make([]func(alert *Alert), len(am.callbacks))
	copy(callbacks, am.callbacks)
	am.mu.RUnlock()

	// 调用回调
	for _, callback := range callbacks {
		callback(alert)
	}

	// 发送 Webhook
	if am.config.EnableWebhook && am.config.WebhookURL != "" {
		go am.sendWebhook(alert)
	}

	// 发送邮件
	if am.config.EnableEmail && len(am.config.EmailTo) > 0 {
		go am.sendEmail(alert)
	}
}

// sendWebhook 发送 Webhook 告警
func (am *AlertManager) sendWebhook(alert *Alert) error {
	var payload map[string]any
	var url string

	switch am.config.WebhookType {
	case "dingtalk":
		// 钉钉格式
		payload = map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]any{
				"title": alert.Title,
				"text": fmt.Sprintf("## %s\n\n**级别**: %s\n\n%s\n\n**时间**: %s\n\n**详情**: %v",
					alert.Title, alert.Level, alert.Message,
					alert.Timestamp.Format("2006-01-02 15:04:05"),
					formatDetails(alert.Details)),
			},
		}
		url = am.config.WebhookURL
	case "wechat":
		// 企业微信格式
		payload = map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]any{
				"content": fmt.Sprintf("## %s\n> **级别**: %s\n> **时间**: %s\n> **详情**: %s\n> %s",
					alert.Title, alert.Level,
					alert.Timestamp.Format("2006-01-02 15:04:05"),
					alert.Message,
					formatDetails(alert.Details)),
			},
		}
		url = am.config.WebhookURL
	case "feishu":
		// 飞书格式
		payload = map[string]any{
			"msg_type": "interactive",
			"card": map[string]any{
				"header": map[string]any{
					"title": map[string]any{
						"tag":     "plain_text",
						"content": alert.Title,
					},
				},
				"elements": []map[string]any{
					{
						"tag": "div",
						"text": map[string]any{
							"tag": "lark_md",
							"content": fmt.Sprintf("**级别**: %s\n**时间**: %s\n**详情**: %s\n%s",
								alert.Level,
								alert.Timestamp.Format("2006-01-02 15:04:05"),
								alert.Message,
								formatDetails(alert.Details)),
						},
					},
				},
			},
		}
		url = am.config.WebhookURL
	default:
		// 通用 JSON 格式
		payload = map[string]any{
			"type":      alert.Type,
			"level":     alert.Level,
			"title":     alert.Title,
			"message":   alert.Message,
			"timestamp": alert.Timestamp.Format(time.RFC3339),
			"details":   alert.Details,
		}
		url = am.config.WebhookURL
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化 Webhook 数据失败：%w", err)
	}

	resp, err := am.httpClient.Post(url, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("发送 Webhook 失败：%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Webhook 返回错误状态码：%d", resp.StatusCode)
	}

	return nil
}

// sendEmail 发送邮件告警
func (am *AlertManager) sendEmail(alert *Alert) error {
	// 构建邮件内容
	subject := fmt.Sprintf("[%s] %s - %s", alert.Level, alert.Type, alert.Title)

	body := fmt.Sprintf(`告警通知

级别：%s
类型：%s
标题：%s
时间：%s

详情:
%s

%s
`,
		alert.Level,
		alert.Type,
		alert.Title,
		alert.Timestamp.Format("2006-01-02 15:04:05"),
		formatDetails(alert.Details),
		alert.Message,
	)

	// 构建邮件
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", am.config.EmailFrom))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(am.config.EmailTo, ", ")))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	// 根据认证类型选择认证方式
	var auth smtp.Auth
	addr := fmt.Sprintf("%s:%d", am.config.EmailSMTPServer, am.config.EmailSMTPPort)

	switch strings.ToUpper(am.config.EmailAuthType) {
	case "LOGIN":
		// 使用 LOGIN 认证（自定义实现）
		auth = newLoginAuth(am.config.EmailFrom, am.config.EmailPassword)
	case "PLAIN", "":
		// 使用 PLAIN 认证
		auth = smtp.PlainAuth("", am.config.EmailFrom, am.config.EmailPassword, am.config.EmailSMTPServer)
	default:
		// 默认使用 PLAIN
		auth = smtp.PlainAuth("", am.config.EmailFrom, am.config.EmailPassword, am.config.EmailSMTPServer)
	}

	// 发送邮件
	var err error
	// 使用 smtp.SendMail 发送邮件（自动处理 TLS）
	err = smtp.SendMail(addr, auth, am.config.EmailFrom, am.config.EmailTo, []byte(msg.String()))

	if err != nil {
		return fmt.Errorf("发送邮件失败：%w", err)
	}

	return nil
}

// newLoginAuth 创建 LOGIN 认证器
func newLoginAuth(username, password string) smtp.Auth {
	return &loginAuth{username, password}
}

// loginAuth 实现 SMTP LOGIN 认证
type loginAuth struct {
	username string
	password string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", []byte(a.username), nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		switch strings.ToLower(string(fromServer)) {
		case "username:":
			return []byte(a.username), nil
		case "password:":
			return []byte(a.password), nil
		default:
			// 如果是 Base64 编码的挑战
			decoded, err := base64.StdEncoding.DecodeString(string(fromServer))
			if err != nil {
				return nil, fmt.Errorf("无法解码挑战：%w", err)
			}
			switch strings.ToLower(string(decoded)) {
			case "username:":
				return []byte(a.username), nil
			case "password:":
				return []byte(a.password), nil
			}
		}
	}
	return nil, nil
}

// formatDetails 格式化详情
func formatDetails(details map[string]any) string {
	if details == nil {
		return ""
	}

	var sb strings.Builder
	for k, v := range details {
		sb.WriteString(fmt.Sprintf("- %s: %v\n", k, v))
	}
	return sb.String()
}

// formatSize 格式化大小为人类可读格式
func formatSize(bytes uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// GetSystemInfo 获取系统信息
func (am *AlertManager) GetSystemInfo() map[string]any {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return map[string]any{
		"go_version":    runtime.Version(),
		"os":            runtime.GOOS,
		"arch":          runtime.GOARCH,
		"num_cpu":       runtime.NumCPU(),
		"num_goroutine": runtime.NumGoroutine(),
		"mem_alloc":     formatSize(memStats.Alloc),
		"mem_total":     formatSize(memStats.TotalAlloc),
		"mem_sys":       formatSize(memStats.Sys),
	}
}

// CheckDiskAlert 手动触发磁盘检查
func (am *AlertManager) CheckDiskAlert() {
	am.checkDiskUsage()
}
