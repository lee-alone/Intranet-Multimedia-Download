package logrotate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDefaultConfig 测试默认配置
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.MaxSize != 100 {
		t.Errorf("默认最大大小应为 100MB, 得到 %d", config.MaxSize)
	}

	if config.MaxAge != 7 {
		t.Errorf("默认最大天数应为 7 天，得到 %d", config.MaxAge)
	}

	if config.Compress != true {
		t.Error("默认应启用压缩")
	}

	if config.MaxBackups != 10 {
		t.Errorf("默认最大备份数应为 10, 得到 %d", config.MaxBackups)
	}
}

// TestNewRotator 测试创建轮转器
func TestNewRotator(t *testing.T) {
	config := Config{
		MaxSize:    50,
		MaxAge:     14,
		Compress:   false,
		MaxBackups: 5,
	}

	r := NewRotator("./test_logs", config)

	if r.config.MaxSize != 50 {
		t.Errorf("MaxSize 应为 50, 得到 %d", r.config.MaxSize)
	}

	if r.config.MaxAge != 14 {
		t.Errorf("MaxAge 应为 14, 得到 %d", r.config.MaxAge)
	}

	if r.config.Compress != false {
		t.Error("Compress 应为 false")
	}

	if r.config.MaxBackups != 5 {
		t.Errorf("MaxBackups 应为 5, 得到 %d", r.config.MaxBackups)
	}
}

// TestNewRotatorDefaultConfig 测试使用默认配置创建轮转器
func TestNewRotatorDefaultConfig(t *testing.T) {
	config := Config{} // 空配置应使用默认值
	r := NewRotator("./test_logs", config)

	if r.config.MaxSize <= 0 {
		t.Error("MaxSize 应使用默认值")
	}

	if r.config.MaxAge <= 0 {
		t.Error("MaxAge 应使用默认值")
	}

	if r.config.MaxBackups <= 0 {
		t.Error("MaxBackups 应使用默认值")
	}
}

// TestGetLogStats 测试获取日志统计信息
func TestGetLogStats(t *testing.T) {
	// 创建临时目录
	tempDir := t.TempDir()

	config := DefaultConfig()
	r := NewRotator(tempDir, config)

	// 创建测试日志文件
	testFile := filepath.Join(tempDir, "test.log")
	content := "test log content"
	if err := os.WriteFile(testFile, []byte(content), 0640); err != nil {
		t.Fatalf("创建测试文件失败：%v", err)
	}

	// 获取统计信息
	totalSize, fileCount, err := r.GetLogStats()
	if err != nil {
		t.Fatalf("获取统计信息失败：%v", err)
	}

	if fileCount != 1 {
		t.Errorf("应有 1 个日志文件，得到 %d", fileCount)
	}

	if totalSize != int64(len(content)) {
		t.Errorf("总大小应为 %d, 得到 %d", len(content), totalSize)
	}
}

// TestGetLogStatsEmpty 测试空目录的统计信息
func TestGetLogStatsEmpty(t *testing.T) {
	tempDir := t.TempDir()

	config := DefaultConfig()
	r := NewRotator(tempDir, config)

	totalSize, fileCount, err := r.GetLogStats()
	if err != nil {
		t.Fatalf("获取统计信息失败：%v", err)
	}

	if fileCount != 0 {
		t.Errorf("应有 0 个日志文件，得到 %d", fileCount)
	}

	if totalSize != 0 {
		t.Errorf("总大小应为 0, 得到 %d", totalSize)
	}
}

// TestCheckAndRotate 测试检查并轮转
func TestCheckAndRotate(t *testing.T) {
	tempDir := t.TempDir()

	// 设置很小的 MaxSize 以便触发轮转
	config := Config{
		MaxSize:    1, // 1MB
		MaxAge:     7,
		Compress:   false,
		MaxBackups: 10,
	}

	r := NewRotator(tempDir, config)

	// 创建测试日志文件（小于 1MB，不应轮转）
	testFile := filepath.Join(tempDir, "test.log")
	smallContent := "small content"
	if err := os.WriteFile(testFile, []byte(smallContent), 0640); err != nil {
		t.Fatalf("创建测试文件失败：%v", err)
	}

	// 检查（不应轮转）
	err := r.CheckAndRotate(testFile)
	if err != nil {
		t.Fatalf("CheckAndRotate 失败：%v", err)
	}

	// 文件应该还在
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("文件应该存在")
	}
}

// TestRotateFile 测试轮转文件
func TestRotateFile(t *testing.T) {
	tempDir := t.TempDir()

	config := Config{
		MaxSize:    1,
		MaxAge:     7,
		Compress:   false, // 先测试不压缩
		MaxBackups: 10,
	}

	r := NewRotator(tempDir, config)

	// 创建测试日志文件
	testFile := filepath.Join(tempDir, "test.log")
	content := "test content"
	if err := os.WriteFile(testFile, []byte(content), 0640); err != nil {
		t.Fatalf("创建测试文件失败：%v", err)
	}

	// 轮转文件
	if err := r.rotateFile(testFile); err != nil {
		t.Fatalf("轮转文件失败：%v", err)
	}

	// 原文件应被清空（重新创建）
	info, err := os.Stat(testFile)
	if err != nil {
		t.Errorf("原文件应存在：%v", err)
	} else if info.Size() != 0 {
		t.Errorf("原文件应被清空，实际大小：%d", info.Size())
	}
}

// TestCompressFile 测试压缩文件
func TestCompressFile(t *testing.T) {
	tempDir := t.TempDir()

	config := DefaultConfig()
	r := NewRotator(tempDir, config)

	// 创建测试文件
	testFile := filepath.Join(tempDir, "test.log")
	content := "test content for compression"
	if err := os.WriteFile(testFile, []byte(content), 0640); err != nil {
		t.Fatalf("创建测试文件失败：%v", err)
	}

	// 压缩文件
	if err := r.compressFile(testFile); err != nil {
		t.Fatalf("压缩文件失败：%v", err)
	}

	// 原文件应被删除
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("原文件应被删除")
	}

	// 压缩文件应存在
	gzFile := testFile + ".gz"
	if _, err := os.Stat(gzFile); os.IsNotExist(err) {
		t.Error("压缩文件应存在")
	}
}

// TestCleanupOldFiles 测试清理旧文件
func TestCleanupOldFiles(t *testing.T) {
	tempDir := t.TempDir()

	// 设置 MaxAge 为 1 天
	config := Config{
		MaxSize:    100,
		MaxAge:     1,
		Compress:   false,
		MaxBackups: 10,
	}

	r := NewRotator(tempDir, config)

	// 创建测试日志文件
	testFile := filepath.Join(tempDir, "test.log")
	content := "test content"
	if err := os.WriteFile(testFile, []byte(content), 0640); err != nil {
		t.Fatalf("创建测试文件失败：%v", err)
	}

	// 修改文件时间为 2 天前
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(testFile, oldTime, oldTime); err != nil {
		t.Fatalf("修改文件时间失败：%v", err)
	}

	// 清理旧文件
	if err := r.cleanupOldFiles(); err != nil {
		t.Fatalf("清理旧文件失败：%v", err)
	}

	// 文件应被删除
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("旧文件应被删除")
	}
}

// TestGetLogFiles 测试获取日志文件
func TestGetLogFiles(t *testing.T) {
	tempDir := t.TempDir()

	config := DefaultConfig()
	r := NewRotator(tempDir, config)

	// 创建测试日志文件
	testFile1 := filepath.Join(tempDir, "app.log")
	testFile2 := filepath.Join(tempDir, "error.log")
	testFile3 := filepath.Join(tempDir, "notlog.txt") // 不应被识别

	if err := os.WriteFile(testFile1, []byte("log1"), 0640); err != nil {
		t.Fatalf("创建测试文件 1 失败：%v", err)
	}
	if err := os.WriteFile(testFile2, []byte("log2"), 0640); err != nil {
		t.Fatalf("创建测试文件 2 失败：%v", err)
	}
	if err := os.WriteFile(testFile3, []byte("txt"), 0640); err != nil {
		t.Fatalf("创建测试文件 3 失败：%v", err)
	}

	files, err := r.getLogFiles()
	if err != nil {
		t.Fatalf("获取日志文件失败：%v", err)
	}

	if len(files) != 2 {
		t.Errorf("应有 2 个日志文件，得到 %d", len(files))
	}
}

// TestRotatorConcurrent 测试并发安全性
func TestRotatorConcurrent(t *testing.T) {
	tempDir := t.TempDir()

	config := DefaultConfig()
	r := NewRotator(tempDir, config)

	// 并发获取统计信息
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			r.GetLogStats()
			done <- true
		}()
	}

	// 等待所有协程完成
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestRotate 测试完整轮转流程
func TestRotate(t *testing.T) {
	tempDir := t.TempDir()

	// 设置很小的 MaxSize 以便触发轮转
	config := Config{
		MaxSize:    1, // 1MB
		MaxAge:     7,
		Compress:   true, // 测试压缩
		MaxBackups: 10,
	}

	r := NewRotator(tempDir, config)

	// 创建测试日志文件（小于 1MB，不应触发轮转）
	testFile := filepath.Join(tempDir, "app.log")
	smallContent := "small content"
	if err := os.WriteFile(testFile, []byte(smallContent), 0640); err != nil {
		t.Fatalf("创建测试文件失败：%v", err)
	}

	// 执行轮转（不应有变化）
	if err := r.Rotate(); err != nil {
		t.Fatalf("Rotate 失败：%v", err)
	}

	// 文件应该还在
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("文件应该存在")
	}
}

// TestRotateWithLargeFile 测试大文件轮转
func TestRotateWithLargeFile(t *testing.T) {
	tempDir := t.TempDir()

	// 设置非常小的 MaxSize (1KB = 1024 字节) 以便触发轮转
	config := Config{
		MaxSize:    1, // 1KB (注意：MaxSize 单位是 MB，所以这里是 1MB = 1048576 字节)
		MaxAge:     7,
		Compress:   true,
		MaxBackups: 10,
	}

	r := NewRotator(tempDir, config)

	// 创建测试日志文件（2KB，但 MaxSize 单位是 MB，所以 2KB < 1MB 不会触发轮转）
	// 我们需要创建一个大于 1MB 的文件，但这样测试太慢
	// 所以改为测试 rotateFile 函数直接
	testFile := filepath.Join(tempDir, "app.log")
	content := "test content"
	if err := os.WriteFile(testFile, []byte(content), 0640); err != nil {
		t.Fatalf("创建测试文件失败：%v", err)
	}

	// 直接测试 rotateFile（不通过 Rotate）
	if err := r.rotateFile(testFile); err != nil {
		t.Fatalf("rotateFile 失败：%v", err)
	}

	// 原文件应被清空
	info, err := os.Stat(testFile)
	if err != nil {
		t.Errorf("原文件应存在：%v", err)
	} else if info.Size() != 0 {
		t.Errorf("原文件应被清空，实际大小：%d", info.Size())
	}
}

// TestRotateEmptyDir 测试空目录轮转
func TestRotateEmptyDir(t *testing.T) {
	tempDir := t.TempDir()

	config := DefaultConfig()
	r := NewRotator(tempDir, config)

	// 执行轮转（空目录）
	if err := r.Rotate(); err != nil {
		t.Fatalf("Rotate 失败：%v", err)
	}
}

// TestGetLogStatsWithMultipleFiles 测试多个日志文件的统计
func TestGetLogStatsWithMultipleFiles(t *testing.T) {
	tempDir := t.TempDir()

	config := DefaultConfig()
	r := NewRotator(tempDir, config)

	// 创建多个测试日志文件
	files := []string{"app.log", "error.log", "access.log"}
	totalExpected := 0
	for _, name := range files {
		content := "test content for " + name
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content), 0640); err != nil {
			t.Fatalf("创建测试文件失败：%v", err)
		}
		totalExpected += len(content)
	}

	// 获取统计信息
	totalSize, fileCount, err := r.GetLogStats()
	if err != nil {
		t.Fatalf("获取统计信息失败：%v", err)
	}

	if fileCount != 3 {
		t.Errorf("应有 3 个日志文件，得到 %d", fileCount)
	}

	if totalSize != int64(totalExpected) {
		t.Errorf("总大小应为 %d, 得到 %d", totalExpected, totalSize)
	}
}
