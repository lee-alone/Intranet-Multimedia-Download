// Package audit 提供审计日志功能
package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/campus/collector/internal/database"
)

// ActionType 定义审计日志的操作类型
type ActionType string

const (
	// ActionLogin 用户登录
	ActionLogin ActionType = "login"
	// ActionLogout 用户登出
	ActionLogout ActionType = "logout"
	// ActionRegister 用户注册
	ActionRegister ActionType = "register"
	// ActionCreateTask 创建任务
	ActionCreateTask ActionType = "create_task"
	// ActionDeleteTask 删除任务
	ActionDeleteTask ActionType = "delete_task"
	// ActionCancelTask 取消任务
	ActionCancelTask ActionType = "cancel_task"
	// ActionDownload 下载文件
	ActionDownload ActionType = "download"
	// ActionViewAudit 查看审计日志
	ActionViewAudit ActionType = "view_audit"
	// ActionMFAEnable 启用 MFA
	ActionMFAEnable ActionType = "mfa_enable"
	// ActionMFADisable 禁用 MFA
	ActionMFADisable ActionType = "mfa_disable"
)

// ResourceType 定义资源类型
type ResourceType string

const (
	// ResourceTypeUser 用户资源
	ResourceTypeUser ResourceType = "user"
	// ResourceTypeTask 任务资源
	ResourceTypeTask ResourceType = "task"
	// ResourceTypeBatchTask 批量任务资源
	ResourceTypeBatchTask ResourceType = "batch_task"
)

// AuditLog 审计日志记录
type AuditLog struct {
	ID           int64
	UserID       *int64
	Action       ActionType
	ResourceType *ResourceType
	ResourceID   *int64
	IPAddress    string
	UserAgent    string
	Detail       map[string]interface{}
	CreatedAt    time.Time
}

// Logger 审计日志记录器
type Logger struct {
	db         *sql.DB
	fileLogger *FileLogger
	mu         sync.RWMutex
	enableFile bool
}

// FileLogger 文件日志记录器
type FileLogger struct {
	baseDir    string
	currentDay string
	file       *os.File
	mu         sync.Mutex
}

// NewLogger 创建新的审计日志记录器
func NewLogger(logDir string, enableFile bool) (*Logger, error) {
	db := database.Get()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	logger := &Logger{
		db:         db,
		enableFile: enableFile,
	}

	if enableFile {
		fileLogger, err := NewFileLogger(logDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create file logger: %w", err)
		}
		logger.fileLogger = fileLogger
	}

	return logger, nil
}

// NewFileLogger 创建新的文件日志记录器
func NewFileLogger(baseDir string) (*FileLogger, error) {
	// 确保日志目录存在
	if err := os.MkdirAll(baseDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	fl := &FileLogger{
		baseDir: baseDir,
	}

	// 初始化当前日志文件
	if err := fl.rotate(); err != nil {
		return nil, fmt.Errorf("failed to initialize log file: %w", err)
	}

	return fl, nil
}

// rotate 执行日志轮转
func (fl *FileLogger) rotate() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	// 关闭当前文件
	if fl.file != nil {
		if err := fl.file.Close(); err != nil {
			return fmt.Errorf("failed to close log file: %w", err)
		}
		fl.file = nil
	}

	// 获取当前日期
	currentDay := time.Now().Format("2006-01-02")
	fl.currentDay = currentDay

	// 创建新的日志文件
	filename := filepath.Join(fl.baseDir, fmt.Sprintf("audit_%s.log", currentDay))
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	fl.file = file
	return nil
}

// Write 写入日志到文件
func (fl *FileLogger) Write(log *AuditLog) error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	// 检查是否需要轮转
	currentDay := time.Now().Format("2006-01-02")
	if currentDay != fl.currentDay {
		if err := fl.rotate(); err != nil {
			return err
		}
	}

	// 序列化日志
	data, err := json.Marshal(log)
	if err != nil {
		return fmt.Errorf("failed to marshal log: %w", err)
	}

	// 写入文件
	if _, err := fl.file.WriteString(string(data) + "\n"); err != nil {
		return fmt.Errorf("failed to write log: %w", err)
	}

	return nil
}

// Close 关闭文件日志记录器
func (fl *FileLogger) Close() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	if fl.file != nil {
		err := fl.file.Close()
		fl.file = nil
		return err
	}
	return nil
}

// Log 记录审计日志
func (l *Logger) Log(log *AuditLog) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 脱敏处理
	if log.Detail != nil {
		log.Detail = sanitizeDetail(log.Detail)
	}

	// 设置创建时间（在脱敏之后，避免修改原始对象）
	createdAt := log.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	// 写入数据库
	if err := l.writeToDBWithTime(log, createdAt); err != nil {
		return fmt.Errorf("failed to write to database: %w", err)
	}

	// 写入文件
	if l.enableFile && l.fileLogger != nil {
		// 创建副本用于文件写入
		fileLog := *log
		fileLog.CreatedAt = createdAt
		if err := l.fileLogger.Write(&fileLog); err != nil {
			// 文件写入失败不影响主流程，仅记录错误
			fmt.Printf("warning: failed to write audit log to file: %v\n", err)
		}
	}

	return nil
}

// writeToDBWithTime 将日志写入数据库（使用指定时间）
func (l *Logger) writeToDBWithTime(log *AuditLog, createdAt time.Time) error {
	var detailJSON []byte
	var err error

	if log.Detail != nil {
		detailJSON, err = json.Marshal(log.Detail)
		if err != nil {
			return fmt.Errorf("failed to marshal detail: %w", err)
		}
	}

	query := `
		INSERT INTO audit_logs (user_id, action, resource_type, resource_id, ip_address, user_agent, detail, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	var resourceTypeStr *string
	if log.ResourceType != nil {
		s := string(*log.ResourceType)
		resourceTypeStr = &s
	}

	_, err = l.db.Exec(query,
		log.UserID,
		string(log.Action),
		resourceTypeStr,
		log.ResourceID,
		log.IPAddress,
		log.UserAgent,
		string(detailJSON),
		createdAt,
	)

	return err
}

// sanitizeDetail 对日志详情进行脱敏处理
func sanitizeDetail(detail map[string]interface{}) map[string]interface{} {
	sanitized := make(map[string]interface{})

	for key, value := range detail {
		switch v := value.(type) {
		case string:
			// 检查是否是 URL
			if isURL(v) {
				sanitized[key] = sanitizeURL(v)
			} else if isSensitiveKey(key) {
				sanitized[key] = maskSensitiveValue(v)
			} else {
				sanitized[key] = v
			}
		case map[string]interface{}:
			sanitized[key] = sanitizeDetail(v)
		default:
			sanitized[key] = v
		}
	}

	return sanitized
}

// isURL 检查字符串是否是 URL
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// sanitizeURL 对 URL 进行脱敏处理
func sanitizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		// 无效 URL，直接返回原始值
		return rawURL
	}

	// 脱敏查询参数
	if u.RawQuery != "" {
		queryParams := u.Query()
		hasSensitive := false
		for key := range queryParams {
			if isSensitiveKey(key) {
				queryParams.Set(key, "***")
				hasSensitive = true
			}
		}
		if hasSensitive {
			u.RawQuery = queryParams.Encode()
		}
	}

	// 使用 url.URL 的 String() 方法重建 URL
	return u.String()
}

// isSensitiveKey 检查键名是否敏感
func isSensitiveKey(key string) bool {
	sensitiveKeys := []string{
		"password", "passwd", "pwd", "secret", "token",
		"api_key", "apikey", "access_token", "refresh_token",
		"authorization", "auth", "credential", "credentials",
	}

	lowerKey := strings.ToLower(key)
	for _, sk := range sensitiveKeys {
		if strings.Contains(lowerKey, sk) {
			return true
		}
	}

	return false
}

// maskSensitiveValue 对敏感值进行掩码处理
func maskSensitiveValue(value string) string {
	if len(value) <= 4 {
		return "***"
	}
	return value[:2] + "***" + value[len(value)-2:]
}

// Query 查询审计日志
func (l *Logger) Query(userID *int64, action *ActionType, resourceType *ResourceType, limit, offset int) ([]AuditLog, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	query := `
		SELECT id, user_id, action, resource_type, resource_id, ip_address, user_agent, detail, created_at
		FROM audit_logs
		WHERE 1=1
	`
	args := []interface{}{}
	argPos := 1

	if userID != nil {
		query += fmt.Sprintf(" AND user_id = $%d", argPos)
		args = append(args, *userID)
		argPos++
	}

	if action != nil {
		query += fmt.Sprintf(" AND action = $%d", argPos)
		args = append(args, string(*action))
		argPos++
	}

	if resourceType != nil {
		query += fmt.Sprintf(" AND resource_type = $%d", argPos)
		args = append(args, string(*resourceType))
		argPos++
	}

	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, limit)
		argPos++
	}
	if offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argPos)
		args = append(args, offset)
	}

	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var log AuditLog
		var resourceTypeStr sql.NullString
		var detailJSON sql.NullString

		err := rows.Scan(
			&log.ID,
			&log.UserID,
			&log.Action,
			&resourceTypeStr,
			&log.ResourceID,
			&log.IPAddress,
			&log.UserAgent,
			&detailJSON,
			&log.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}

		if resourceTypeStr.Valid {
			rt := ResourceType(resourceTypeStr.String)
			log.ResourceType = &rt
		}

		if detailJSON.Valid && detailJSON.String != "" {
			if err := json.Unmarshal([]byte(detailJSON.String), &log.Detail); err != nil {
				log.Detail = nil
			}
		}

		logs = append(logs, log)
	}

	return logs, nil
}

// Close 关闭审计日志记录器
func (l *Logger) Close() error {
	if l.fileLogger != nil {
		return l.fileLogger.Close()
	}
	return nil
}
