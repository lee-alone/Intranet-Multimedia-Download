// Package audit 提供审计日志功能的测试
package audit

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/campus/collector/internal/database"
)

var (
	testLogger *Logger
	testDB     *sql.DB
)

// TestMain 设置测试环境
func TestMain(m *testing.M) {
	// 创建临时数据库文件
	tempDir, err := os.MkdirTemp("", "audit_test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")

	// 初始化数据库
	cfg := &database.Config{
		Path:     dbPath,
		WALMode:  true,
		MaxConns: 5,
	}
	if err := database.Init(cfg); err != nil {
		panic(err)
	}
	testDB = database.Get()

	// 创建审计日志表（禁用外键约束用于测试）
	_, err = testDB.Exec("PRAGMA foreign_keys=OFF")
	if err != nil {
		panic(err)
	}

	_, err = testDB.Exec(`
		CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			action TEXT NOT NULL,
			resource_type TEXT,
			resource_id INTEGER,
			ip_address TEXT,
			user_agent TEXT,
			detail TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		panic(err)
	}

	// 创建测试日志记录器
	testLogger, err = NewLogger(tempDir, true)
	if err != nil {
		panic(err)
	}

	// 运行测试
	code := m.Run()

	// 清理
	testLogger.Close()
	database.Close()

	os.Exit(code)
}

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name       string
		logDir     string
		enableFile bool
		wantErr    bool
	}{
		{
			name:       "valid logger with file",
			logDir:     os.TempDir(),
			enableFile: true,
			wantErr:    false,
		},
		{
			name:       "valid logger without file",
			logDir:     os.TempDir(),
			enableFile: false,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := NewLogger(tt.logDir, tt.enableFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLogger() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if logger == nil {
					t.Error("NewLogger() returned nil logger")
				}
				logger.Close()
			}
		})
	}
}

func TestLogger_Log(t *testing.T) {
	tests := []struct {
		name    string
		log     *AuditLog
		wantErr bool
	}{
		{
			name: "valid log with user",
			log: &AuditLog{
				UserID:       int64Ptr(1),
				Action:       ActionLogin,
				ResourceType: resourceTypePtr(ResourceTypeUser),
				ResourceID:   int64Ptr(1),
				IPAddress:    "192.168.1.1",
				UserAgent:    "Mozilla/5.0",
				Detail: map[string]interface{}{
					"username": "testuser",
				},
			},
			wantErr: false,
		},
		{
			name: "valid log without user",
			log: &AuditLog{
				Action:    ActionLogin,
				IPAddress: "192.168.1.1",
				UserAgent: "Mozilla/5.0",
			},
			wantErr: false,
		},
		{
			name: "log with URL in detail",
			log: &AuditLog{
				UserID:    int64Ptr(1),
				Action:    ActionCreateTask,
				IPAddress: "192.168.1.1",
				Detail: map[string]interface{}{
					"url": "https://example.com/video?token=secret123",
				},
			},
			wantErr: false,
		},
		{
			name: "log with sensitive data",
			log: &AuditLog{
				UserID:    int64Ptr(1),
				Action:    ActionLogin,
				IPAddress: "192.168.1.1",
				Detail: map[string]interface{}{
					"username": "testuser",
					"password": "secret123",
					"api_key":  "abc123def456",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := testLogger.Log(tt.log)
			if (err != nil) != tt.wantErr {
				t.Errorf("Logger.Log() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// 验证日志已写入数据库
				logs, err := testLogger.Query(nil, nil, nil, 1, 0)
				if err != nil {
					t.Errorf("Failed to query logs: %v", err)
				}
				if len(logs) == 0 {
					t.Error("Log was not written to database")
				}
			}
		})
	}
}

func TestSanitizeDetail(t *testing.T) {
	tests := []struct {
		name     string
		detail   map[string]interface{}
		wantMask bool
		maskKey  string
	}{
		{
			name: "sanitize URL with token",
			detail: map[string]interface{}{
				"url": "https://example.com/video?token=secret123&session=abc",
			},
			wantMask: true,
			maskKey:  "url",
		},
		{
			name: "sanitize password",
			detail: map[string]interface{}{
				"username": "testuser",
				"password": "secret123",
			},
			wantMask: true,
			maskKey:  "password",
		},
		{
			name: "sanitize api_key",
			detail: map[string]interface{}{
				"api_key": "abc123def456",
			},
			wantMask: true,
			maskKey:  "api_key",
		},
		{
			name: "no sensitive data",
			detail: map[string]interface{}{
				"username": "testuser",
				"email":    "test@example.com",
			},
			wantMask: false,
		},
		{
			name: "nested sensitive data",
			detail: map[string]interface{}{
				"user": map[string]interface{}{
					"username": "testuser",
					"password": "secret123",
				},
			},
			wantMask: true,
			maskKey:  "user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeDetail(tt.detail)

			if tt.wantMask {
				// 检查敏感数据是否被掩码
				if tt.maskKey != "" {
					if val, ok := result[tt.maskKey]; ok {
						if strVal, ok := val.(string); ok {
							if strVal == tt.detail[tt.maskKey] {
								t.Errorf("Sensitive data was not masked: %s", strVal)
							}
						}
					}
				}
			} else {
				// 检查非敏感数据是否保持不变
				for key, value := range tt.detail {
					if result[key] != value {
						t.Errorf("Non-sensitive data was modified: key=%s", key)
					}
				}
			}
		})
	}
}

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantMask bool
	}{
		{
			name:     "URL with token parameter",
			url:      "https://example.com/video?token=secret123",
			wantMask: true,
		},
		{
			name:     "URL with password parameter",
			url:      "https://example.com/api?password=secret",
			wantMask: true,
		},
		{
			name:     "URL with api_key parameter",
			url:      "https://example.com/api?api_key=abc123",
			wantMask: true,
		},
		{
			name:     "URL without sensitive parameters",
			url:      "https://example.com/video?id=123",
			wantMask: false,
		},
		{
			name:     "URL with mixed parameters",
			url:      "https://example.com/video?id=123&token=secret&format=mp4",
			wantMask: true,
		},
		{
			name:     "invalid URL",
			url:      "not a url",
			wantMask: false,
		},
		{
			name:     "URL with session parameter",
			url:      "https://example.com/api?session=abc123",
			wantMask: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeURL(tt.url)

			if tt.wantMask {
				// 检查敏感参数是否被掩码
				if result == tt.url {
					t.Error("URL was not sanitized")
				}
				// 检查是否包含 token=%2A%2A%2A 或 password=%2A%2A%2A 或 api_key=%2A%2A%2A (URL 编码的 ***)
				if !contains(result, "token=%2A%2A%2A") && !contains(result, "password=%2A%2A%2A") && !contains(result, "api_key=%2A%2A%2A") {
					t.Errorf("Sanitized URL does not contain mask: %s", result)
				}
				// 检查原始敏感值是否被替换
				if contains(result, "secret123") || contains(result, "secret") || contains(result, "abc123") {
					t.Errorf("Sensitive value was not masked: %s", result)
				}
			} else {
				// 对于无效 URL，检查是否返回原始值
				if tt.url == "not a url" {
					if result != tt.url {
						t.Errorf("Invalid URL was modified: %s -> %s", tt.url, result)
					}
				} else {
					// 对于有效 URL，检查是否保持不变（除了可能的规范化）
					// 检查敏感参数是否被保留
					if contains(tt.url, "session=abc123") && !contains(result, "session=abc123") {
						t.Errorf("Non-sensitive parameter was modified: %s -> %s", tt.url, result)
					}
				}
			}
		})
	}
}

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"password", "password", true},
		{"passwd", "passwd", true},
		{"pwd", "pwd", true},
		{"secret", "secret", true},
		{"token", "token", true},
		{"api_key", "api_key", true},
		{"apikey", "apikey", true},
		{"access_token", "access_token", true},
		{"refresh_token", "refresh_token", true},
		{"authorization", "authorization", true},
		{"auth", "auth", true},
		{"credential", "credential", true},
		{"credentials", "credentials", true},
		{"username", "username", false},
		{"email", "email", false},
		{"id", "id", false},
		{"name", "name", false},
		{"Password", "Password", true},
		{"PASSWORD", "PASSWORD", true},
		{"user_password", "user_password", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSensitiveKey(tt.key); got != tt.want {
				t.Errorf("isSensitiveKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMaskSensitiveValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "short value",
			value: "ab",
			want:  "***",
		},
		{
			name:  "medium value",
			value: "secret",
			want:  "se***et",
		},
		{
			name:  "long value",
			value: "verylongsecretvalue",
			want:  "ve***ue",
		},
		{
			name:  "exactly 4 chars",
			value: "abcd",
			want:  "***",
		},
		{
			name:  "empty string",
			value: "",
			want:  "***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskSensitiveValue(tt.value); got != tt.want {
				t.Errorf("maskSensitiveValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogger_Query(t *testing.T) {
	// 清空测试数据
	testDB.Exec("DELETE FROM audit_logs")

	// 插入测试数据
	userID1 := int64(1)
	userID2 := int64(2)
	resourceID := int64(100)

	logs := []*AuditLog{
		{
			UserID:       &userID1,
			Action:       ActionLogin,
			ResourceType: resourceTypePtr(ResourceTypeUser),
			ResourceID:   &userID1,
			IPAddress:    "192.168.1.1",
			UserAgent:    "Mozilla/5.0",
			Detail:       map[string]interface{}{"username": "user1"},
		},
		{
			UserID:       &userID1,
			Action:       ActionCreateTask,
			ResourceType: resourceTypePtr(ResourceTypeTask),
			ResourceID:   &resourceID,
			IPAddress:    "192.168.1.1",
			UserAgent:    "Mozilla/5.0",
			Detail:       map[string]interface{}{"url": "https://example.com/video"},
		},
		{
			UserID:       &userID2,
			Action:       ActionLogin,
			ResourceType: resourceTypePtr(ResourceTypeUser),
			ResourceID:   &userID2,
			IPAddress:    "192.168.1.2",
			UserAgent:    "Chrome/1.0",
			Detail:       map[string]interface{}{"username": "user2"},
		},
	}

	for _, log := range logs {
		if err := testLogger.Log(log); err != nil {
			t.Fatalf("Failed to insert test log: %v", err)
		}
	}

	tests := []struct {
		name         string
		userID       *int64
		action       *ActionType
		resourceType *ResourceType
		limit        int
		offset       int
		wantCount    int
	}{
		{
			name:         "query all",
			userID:       nil,
			action:       nil,
			resourceType: nil,
			limit:        10,
			offset:       0,
			wantCount:    3,
		},
		{
			name:         "query by user",
			userID:       &userID1,
			action:       nil,
			resourceType: nil,
			limit:        10,
			offset:       0,
			wantCount:    2,
		},
		{
			name:         "query by action",
			userID:       nil,
			action:       actionPtr(ActionLogin),
			resourceType: nil,
			limit:        10,
			offset:       0,
			wantCount:    2,
		},
		{
			name:         "query by resource type",
			userID:       nil,
			action:       nil,
			resourceType: resourceTypePtr(ResourceTypeUser),
			limit:        10,
			offset:       0,
			wantCount:    2,
		},
		{
			name:         "query with limit",
			userID:       nil,
			action:       nil,
			resourceType: nil,
			limit:        2,
			offset:       0,
			wantCount:    2,
		},
		{
			name:         "query with offset",
			userID:       nil,
			action:       nil,
			resourceType: nil,
			limit:        10,
			offset:       1,
			wantCount:    2,
		},
		{
			name:         "query with multiple filters",
			userID:       &userID1,
			action:       actionPtr(ActionLogin),
			resourceType: nil,
			limit:        10,
			offset:       0,
			wantCount:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := testLogger.Query(tt.userID, tt.action, tt.resourceType, tt.limit, tt.offset)
			if err != nil {
				t.Errorf("Logger.Query() error = %v", err)
				return
			}
			if len(result) != tt.wantCount {
				t.Errorf("Logger.Query() got %d logs, want %d", len(result), tt.wantCount)
			}
		})
	}
}

func TestFileLogger_Rotate(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "file_logger_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	fl, err := NewFileLogger(tempDir)
	if err != nil {
		t.Fatalf("Failed to create file logger: %v", err)
	}

	// 记录初始日期
	initialDay := fl.currentDay

	// 写入一些日志
	log := &AuditLog{
		Action:    ActionLogin,
		IPAddress: "192.168.1.1",
	}
	for i := 0; i < 5; i++ {
		if err := fl.Write(log); err != nil {
			t.Fatalf("Failed to write log: %v", err)
		}
	}

	// 检查文件是否存在
	filename := filepath.Join(tempDir, "audit_"+initialDay+".log")
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Error("Log file was not created")
	}

	// 关闭文件日志记录器
	fl.Close()

	// 创建新的文件日志记录器，模拟日期变更
	fl2, err := NewFileLogger(tempDir)
	if err != nil {
		t.Fatalf("Failed to create second file logger: %v", err)
	}
	defer fl2.Close()

	// 新文件应该被创建（因为日期相同，但文件被重新打开）
	newFilename := filepath.Join(tempDir, "audit_"+fl2.currentDay+".log")
	if _, err := os.Stat(newFilename); os.IsNotExist(err) {
		t.Error("New log file was not created after rotation")
	}

	// 检查旧文件仍然存在
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Error("Old log file was deleted after rotation")
	}
}

func TestLogger_LogWithFileRotation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "log_rotation_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	logger, err := NewLogger(tempDir, true)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// 写入日志
	log := &AuditLog{
		UserID:    int64Ptr(1),
		Action:    ActionLogin,
		IPAddress: "192.168.1.1",
	}

	if err := logger.Log(log); err != nil {
		t.Fatalf("Failed to log: %v", err)
	}

	// 检查日志文件是否创建
	files, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("Failed to read log directory: %v", err)
	}

	if len(files) == 0 {
		t.Error("No log file was created")
	}

	// 检查文件名格式
	for _, file := range files {
		if !contains(file.Name(), "audit_") || !contains(file.Name(), ".log") {
			t.Errorf("Invalid log file name: %s", file.Name())
		}
	}
}

// 辅助函数

func int64Ptr(i int64) *int64 {
	return &i
}

func resourceTypePtr(rt ResourceType) *ResourceType {
	return &rt
}

func actionPtr(at ActionType) *ActionType {
	return &at
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestLogger_ConcurrentWrite 测试并发写入
func TestLogger_ConcurrentWrite(t *testing.T) {
	// 清空测试数据
	testDB.Exec("DELETE FROM audit_logs")

	const numGoroutines = 10
	const logsPerGoroutine = 10

	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			userID := int64(id + 1)
			for j := 0; j < logsPerGoroutine; j++ {
				log := &AuditLog{
					UserID:    &userID,
					Action:    ActionLogin,
					IPAddress: "192.168.1.1",
					Detail:    map[string]interface{}{"iteration": j},
				}
				if err := testLogger.Log(log); err != nil {
					t.Errorf("Failed to log in goroutine %d: %v", id, err)
				}
			}
			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// 验证日志数量
	logs, err := testLogger.Query(nil, nil, nil, 0, 0)
	if err != nil {
		t.Fatalf("Failed to query logs: %v", err)
	}

	expectedCount := numGoroutines * logsPerGoroutine
	if len(logs) != expectedCount {
		t.Errorf("Expected %d logs, got %d", expectedCount, len(logs))
	}
}

// TestLogger_LogWithNilDetail 测试 nil detail 处理
func TestLogger_LogWithNilDetail(t *testing.T) {
	log := &AuditLog{
		UserID:    int64Ptr(1),
		Action:    ActionLogin,
		IPAddress: "192.168.1.1",
		Detail:    nil,
	}

	if err := testLogger.Log(log); err != nil {
		t.Errorf("Logger.Log() with nil detail error = %v", err)
	}

	// 验证日志已写入
	logs, err := testLogger.Query(nil, nil, nil, 1, 0)
	if err != nil {
		t.Errorf("Failed to query logs: %v", err)
	}
	if len(logs) == 0 {
		t.Error("Log with nil detail was not written")
	}
}

// TestLogger_LogWithEmptyDetail 测试空 detail 处理
func TestLogger_LogWithEmptyDetail(t *testing.T) {
	log := &AuditLog{
		UserID:    int64Ptr(1),
		Action:    ActionLogin,
		IPAddress: "192.168.1.1",
		Detail:    map[string]interface{}{},
	}

	if err := testLogger.Log(log); err != nil {
		t.Errorf("Logger.Log() with empty detail error = %v", err)
	}

	// 验证日志已写入
	logs, err := testLogger.Query(nil, nil, nil, 1, 0)
	if err != nil {
		t.Errorf("Failed to query logs: %v", err)
	}
	if len(logs) == 0 {
		t.Error("Log with empty detail was not written")
	}
}

// TestLogger_LogWithTimestamp 测试自定义时间戳
func TestLogger_LogWithTimestamp(t *testing.T) {
	// 清空测试数据
	testDB.Exec("DELETE FROM audit_logs")

	customTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	log := &AuditLog{
		UserID:    int64Ptr(1),
		Action:    ActionLogin,
		IPAddress: "192.168.1.1",
		CreatedAt: customTime,
	}

	if err := testLogger.Log(log); err != nil {
		t.Errorf("Logger.Log() with custom timestamp error = %v", err)
	}

	// 验证时间戳
	logs, err := testLogger.Query(nil, nil, nil, 1, 0)
	if err != nil {
		t.Errorf("Failed to query logs: %v", err)
	}
	if len(logs) > 0 {
		// SQLite 存储时间时会丢失纳秒精度，所以比较到秒级别
		expected := customTime.Truncate(time.Second)
		got := logs[0].CreatedAt.Truncate(time.Second)
		if !got.Equal(expected) {
			t.Errorf("Expected timestamp %v, got %v", expected, got)
		}
	}
}
