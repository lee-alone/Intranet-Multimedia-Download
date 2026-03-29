// Package logrotate 提供日志轮转功能
package logrotate

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config 日志轮转配置
type Config struct {
	MaxSize    int64 // 最大文件大小 (MB)
	MaxAge     int   // 最大保存天数
	Compress   bool  // 是否压缩
	MaxBackups int   // 最大备份数量
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		MaxSize:    100, // 100MB
		MaxAge:     7,   // 7 天
		Compress:   true,
		MaxBackups: 10,
	}
}

// Rotator 日志轮转器
type Rotator struct {
	mu      sync.Mutex
	config  Config
	logDir  string
	fileExt string
}

// NewRotator 创建新的日志轮转器
func NewRotator(logDir string, config Config) *Rotator {
	if config.MaxSize <= 0 {
		config.MaxSize = DefaultConfig().MaxSize
	}
	if config.MaxAge <= 0 {
		config.MaxAge = DefaultConfig().MaxAge
	}
	if config.MaxBackups <= 0 {
		config.MaxBackups = DefaultConfig().MaxBackups
	}

	return &Rotator{
		config:  config,
		logDir:  logDir,
		fileExt: ".log",
	}
}

// Rotate 执行日志轮转
func (r *Rotator) Rotate() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 获取所有日志文件
	files, err := r.getLogFiles()
	if err != nil {
		return fmt.Errorf("获取日志文件失败：%w", err)
	}

	// 按时间排序
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	// 处理需要轮转的文件
	for _, f := range files {
		// 检查是否需要轮转
		if f.size >= r.config.MaxSize*1024*1024 {
			if err := r.rotateFile(f.path); err != nil {
				return fmt.Errorf("轮转文件 %s 失败：%w", f.path, err)
			}
		}
	}

	// 清理旧文件
	if err := r.cleanupOldFiles(); err != nil {
		return fmt.Errorf("清理旧文件失败：%w", err)
	}

	return nil
}

// logFileInfo 日志文件信息
type logFileInfo struct {
	path    string
	modTime time.Time
	size    int64
}

// getLogFiles 获取所有日志文件
func (r *Rotator) getLogFiles() ([]logFileInfo, error) {
	entries, err := os.ReadDir(r.logDir)
	if err != nil {
		return nil, err
	}

	var files []logFileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// 识别 .log 文件、.log.gz 文件以及轮转后的文件（如 app.log.2026-01-02_15-04-05）
		if !strings.HasSuffix(name, r.fileExt) &&
			!strings.HasSuffix(name, r.fileExt+".gz") &&
			!strings.Contains(name, r.fileExt+".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, logFileInfo{
			path:    filepath.Join(r.logDir, name),
			modTime: info.ModTime(),
			size:    info.Size(),
		})
	}

	return files, nil
}

// rotateFile 轮转单个文件
func (r *Rotator) rotateFile(filePath string) error {
	// 生成新的文件名（带时间戳）
	timestamp := time.Now().Format(".2006-01-02_15-04-05")
	baseName := filepath.Base(filePath)
	dirName := filepath.Dir(filePath)
	newName := filepath.Join(dirName, baseName+timestamp)

	// 移动文件
	if err := os.Rename(filePath, newName); err != nil {
		return fmt.Errorf("重命名文件失败：%w", err)
	}

	// 压缩文件
	if r.config.Compress {
		if err := r.compressFile(newName); err != nil {
			// 压缩失败不影响主流程
			fmt.Printf("警告：压缩文件 %s 失败：%v\n", newName, err)
		}
	}

	// 创建新的空文件
	if err := os.WriteFile(filePath, []byte(""), 0640); err != nil {
		return fmt.Errorf("创建新日志文件失败：%w", err)
	}

	return nil
}

// compressFile 压缩文件
func (r *Rotator) compressFile(filePath string) error {
	// 打开源文件
	srcFile, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开源文件失败：%w", err)
	}

	// 创建目标文件
	dstFile, err := os.Create(filePath + ".gz")
	if err != nil {
		srcFile.Close()
		return fmt.Errorf("创建目标文件失败：%w", err)
	}

	// 创建 gzip 写入器
	gzWriter := gzip.NewWriter(dstFile)

	// 复制内容
	_, copyErr := io.Copy(gzWriter, srcFile)

	// 关闭写入器
	if err := gzWriter.Close(); err != nil {
		srcFile.Close()
		dstFile.Close()
		return fmt.Errorf("关闭 gzip 写入器失败：%w", err)
	}

	// 关闭源文件
	if err := srcFile.Close(); err != nil {
		dstFile.Close()
		return fmt.Errorf("关闭源文件失败：%w", err)
	}

	// 关闭目标文件
	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("关闭目标文件失败：%w", err)
	}

	if copyErr != nil {
		return fmt.Errorf("压缩文件失败：%w", copyErr)
	}

	// 删除原文件（在 Windows 上需要关闭所有句柄后才能删除）
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("删除原文件失败：%w", err)
	}

	return nil
}

// cleanupOldFiles 清理旧文件
func (r *Rotator) cleanupOldFiles() error {
	files, err := r.getLogFiles()
	if err != nil {
		return err
	}

	now := time.Now()
	cutoff := now.Add(-time.Duration(r.config.MaxAge) * 24 * time.Hour)

	// 删除超过保存期限的文件
	for _, f := range files {
		if f.modTime.Before(cutoff) {
			if err := os.Remove(f.path); err != nil {
				return fmt.Errorf("删除旧文件 %s 失败：%w", f.path, err)
			}
		}
	}

	// 如果备份数量超过限制，删除最旧的
	if len(files) > r.config.MaxBackups {
		// 按时间排序
		sort.Slice(files, func(i, j int) bool {
			return files[i].modTime.Before(files[j].modTime)
		})

		// 删除多余的旧文件
		for i := 0; i < len(files)-r.config.MaxBackups; i++ {
			if err := os.Remove(files[i].path); err != nil {
				return fmt.Errorf("删除多余文件 %s 失败：%w", files[i].path, err)
			}
		}
	}

	return nil
}

// GetLogStats 获取日志统计信息
func (r *Rotator) GetLogStats() (totalSize int64, fileCount int, err error) {
	files, err := r.getLogFiles()
	if err != nil {
		return 0, 0, err
	}

	for _, f := range files {
		totalSize += f.size
		fileCount++
	}

	return totalSize, fileCount, nil
}

// CheckAndRotate 检查并在需要时轮转
func (r *Rotator) CheckAndRotate(filePath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	if info.Size() >= r.config.MaxSize*1024*1024 {
		return r.rotateFile(filePath)
	}

	return nil
}
