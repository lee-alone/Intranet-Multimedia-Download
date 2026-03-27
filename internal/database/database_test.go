// Package database 提供数据库连接和操作功能的测试
package database

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestIsValidMigrationFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{
			name:     "有效文件名",
			filename: "20260325100000_init.sql",
			expected: true,
		},
		{
			name:     "有效文件名带下划线",
			filename: "20260325100000_create_users_table.sql",
			expected: true,
		},
		{
			name:     "无效文件名 - 无扩展名",
			filename: "20260325100000_init",
			expected: false,
		},
		{
			name:     "无效文件名 - 时间戳太短",
			filename: "20260325_init.sql",
			expected: false,
		},
		{
			name:     "无效文件名 - 非数字时间戳",
			filename: "aaaaaaaaaaaaaa_init.sql",
			expected: false,
		},
		{
			name:     "无效文件名 - 错误扩展名",
			filename: "20260325100000_init.txt",
			expected: false,
		},
		{
			name:     "无效文件名 - 路径遍历",
			filename: "../20260325100000_init.sql",
			expected: false,
		},
		{
			name:     "无效文件名 - 绝对路径",
			filename: "/20260325100000_init.sql",
			expected: false,
		},
		{
			name:     "空文件名",
			filename: "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidMigrationFilename(tt.filename)
			if result != tt.expected {
				t.Errorf("isValidMigrationFilename(%q) = %v, want %v", tt.filename, result, tt.expected)
			}
		})
	}
}

func TestContainsPathTraversal(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "正常文件名",
			input:    "20260325100000_init.sql",
			expected: false,
		},
		{
			name:     "路径遍历 ../",
			input:    "../init.sql",
			expected: true,
		},
		{
			name:     "绝对路径 /",
			input:    "/path/to/file.sql",
			expected: true,
		},
		{
			name:     "Windows 绝对路径 \\",
			input:    "\\path\\to\\file.sql",
			expected: true,
		},
		{
			name:     "当前目录 ./",
			input:    "./init.sql",
			expected: true,
		},
		{
			name:     "空字符串",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsPathTraversal(tt.input)
			if result != tt.expected {
				t.Errorf("containsPathTraversal(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// resetDatabase 重置全局数据库状态（仅用于测试）
func resetDatabase() {
	db = nil
	once = sync.Once{}
}

func TestInitAndMigrations(t *testing.T) {
	// 使用单个测试来避免 sync.Once 的问题
	t.Run("数据库初始化和迁移", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")
		migrationsDir := filepath.Join(tmpDir, "migrations")

		// 重置全局状态
		resetDatabase()

		// 创建迁移目录和文件
		if err := os.MkdirAll(migrationsDir, 0750); err != nil {
			t.Fatalf("Failed to create migrations directory: %v", err)
		}

		migrationContent := `
CREATE TABLE IF NOT EXISTS test_table (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL
);
`
		migrationFile := filepath.Join(migrationsDir, "20260325100000_test.sql")
		if err := os.WriteFile(migrationFile, []byte(migrationContent), 0600); err != nil {
			t.Fatalf("Failed to create migration file: %v", err)
		}

		// 初始化数据库
		cfg := &Config{
			Path:     dbPath,
			WALMode:  true,
			MaxConns: 5,
		}
		if err := Init(cfg); err != nil {
			t.Fatalf("Init() failed: %v", err)
		}

		// 验证数据库连接
		testDB := Get()
		if testDB == nil {
			t.Fatal("Get() returned nil")
		}

		// 验证 WAL 模式已启用
		var journalMode string
		if err := testDB.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
			t.Fatalf("Failed to query journal mode: %v", err)
		}
		if journalMode != "wal" {
			t.Errorf("Journal mode = %q, want wal", journalMode)
		}

		// 验证外键约束已启用
		var foreignKeys int
		if err := testDB.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("Failed to query foreign_keys: %v", err)
		}
		if foreignKeys != 1 {
			t.Errorf("Foreign keys = %d, want 1", foreignKeys)
		}

		// 运行迁移
		if err := RunMigrations(migrationsDir); err != nil {
			t.Fatalf("RunMigrations() failed: %v", err)
		}

		// 验证迁移记录
		var count int
		if err := testDB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = '20260325100000_test'").Scan(&count); err != nil {
			t.Fatalf("Failed to query migrations: %v", err)
		}
		if count != 1 {
			t.Errorf("Migration count = %d, want 1", count)
		}

		// 验证表已创建
		var tableName string
		if err := testDB.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='test_table'").Scan(&tableName); err != nil {
			t.Fatalf("Failed to query table: %v", err)
		}
		if tableName != "test_table" {
			t.Errorf("Table name = %q, want test_table", tableName)
		}

		// 再次运行迁移（应该跳过已应用的）
		if err := RunMigrations(migrationsDir); err != nil {
			t.Fatalf("Second RunMigrations() failed: %v", err)
		}

		// 验证迁移只运行了一次
		if err := testDB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
			t.Fatalf("Failed to query migrations: %v", err)
		}
		if count != 1 {
			t.Errorf("Migration count after second run = %d, want 1", count)
		}

		// 关闭数据库
		if err := Close(); err != nil {
			t.Fatalf("Close() failed: %v", err)
		}
	})

	t.Run("跳过无效迁移文件", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")
		migrationsDir := filepath.Join(tmpDir, "migrations")

		// 重置全局状态
		resetDatabase()

		// 创建迁移目录
		if err := os.MkdirAll(migrationsDir, 0750); err != nil {
			t.Fatalf("Failed to create migrations directory: %v", err)
		}

		// 创建无效文件名
		invalidFile := filepath.Join(migrationsDir, "invalid.sql")
		if err := os.WriteFile(invalidFile, []byte("invalid"), 0600); err != nil {
			t.Fatalf("Failed to create invalid file: %v", err)
		}

		// 初始化数据库
		cfg := &Config{
			Path:     dbPath,
			WALMode:  false,
			MaxConns: 5,
		}
		if err := Init(cfg); err != nil {
			t.Fatalf("Init() failed: %v", err)
		}

		// 运行迁移（应该跳过无效文件）
		if err := RunMigrations(migrationsDir); err != nil {
			t.Fatalf("RunMigrations() failed: %v", err)
		}

		// 关闭数据库
		if err := Close(); err != nil {
			t.Fatalf("Close() failed: %v", err)
		}
	})

	t.Run("关闭空连接", func(t *testing.T) {
		resetDatabase()
		if err := Close(); err != nil {
			t.Errorf("Close() on nil db should not fail: %v", err)
		}
	})
}
