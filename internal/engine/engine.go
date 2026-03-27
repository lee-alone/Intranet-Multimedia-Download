// Package engine 提供视频下载引擎的统一接口和实现
package engine

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// EngineStatus 引擎状态
type EngineStatus int

const (
	EngineStatusIdle EngineStatus = iota
	EngineStatusRunning
	EngineStatusError
)

func (s EngineStatus) String() string {
	switch s {
	case EngineStatusIdle:
		return "idle"
	case EngineStatusRunning:
		return "running"
	case EngineStatusError:
		return "error"
	default:
		return "unknown"
	}
}

// DownloadProgress 下载进度
type DownloadProgress struct {
	Percent    float64 // 进度百分比 0-100
	Downloaded int64   // 已下载字节
	Total      int64   // 总字节
	Speed      float64 // 下载速度 (bytes/s)
	ETA        int     // 预计剩余时间 (秒)
	Status     string  // 当前状态描述
}

// DownloadResult 下载结果
type DownloadResult struct {
	Success    bool
	OutputPath string
	Title      string
	Duration   int // 视频时长 (秒)
	FileSize   int64
	Format     string
	Error      error
}

// Engine 是下载引擎的统一接口
type Engine interface {
	// Name 返回引擎名称
	Name() string

	// Status 返回引擎状态
	Status() EngineStatus

	// CanHandle 判断是否可以处理给定的 URL
	CanHandle(url string) bool

	// Download 执行下载
	Download(ctx context.Context, url string, options DownloadOptions) <-chan DownloadProgress

	// GetVersion 获取引擎版本
	GetVersion() (string, error)

	// IsAvailable 检查引擎是否可用
	IsAvailable() bool
}

// DownloadOptions 下载选项
type DownloadOptions struct {
	OutputDir    string        // 输出目录
	OutputFormat string        // 输出格式 (mp4, mkv 等)
	Quality      string        // 画质选择 (best, 1080p, 720p 等)
	Timeout      time.Duration // 超时时间
	MaxRetries   int           // 最大重试次数
	CookieFile   string        // Cookie 文件路径
	UserAgent    string        // User-Agent
	Proxy        string        // 代理地址
}

// EngineType 引擎类型
type EngineType string

const (
	EngineTypeYtdlp EngineType = "yt-dlp"
	EngineTypeLux   EngineType = "lux"
	EngineTypeAuto  EngineType = "auto"
)

// EngineSelector 引擎选择器
type EngineSelector struct {
	engines       []Engine
	defaultEngine EngineType
}

// NewEngineSelector 创建引擎选择器
func NewEngineSelector(defaultEngine EngineType) *EngineSelector {
	return &EngineSelector{
		engines:       make([]Engine, 0, 2),
		defaultEngine: defaultEngine,
	}
}

// AddEngine 添加引擎
func (s *EngineSelector) AddEngine(engine Engine) {
	s.engines = append(s.engines, engine)
}

// Select 根据 URL 选择合适的引擎
func (s *EngineSelector) Select(url string) Engine {
	// 遍历所有引擎，找到第一个可以处理该 URL 的引擎
	for _, engine := range s.engines {
		if engine.CanHandle(url) {
			return engine
		}
	}
	// 如果没有引擎可以处理，返回 nil
	return nil
}

// GetDefaultEngine 获取默认引擎
func (s *EngineSelector) GetDefaultEngine() Engine {
	if len(s.engines) == 0 {
		return nil
	}
	return s.engines[0]
}

// GetEngineByName 根据名称获取引擎
func (s *EngineSelector) GetEngineByName(name string) Engine {
	for _, engine := range s.engines {
		if engine.Name() == name {
			return engine
		}
	}
	return nil
}

// ListEngines 列出所有可用引擎
func (s *EngineSelector) ListEngines() []string {
	names := make([]string, 0, len(s.engines))
	for _, engine := range s.engines {
		names = append(names, engine.Name())
	}
	return names
}

// buildOutputPath 构建输出文件路径
func buildOutputPath(outputDir, title, format string) string {
	// 清理标题中的非法字符
	cleanTitle := strings.Map(func(r rune) rune {
		invalidChars := "<>:\"/\\|？*"
		if strings.ContainsRune(invalidChars, r) {
			return '_'
		}
		return r
	}, title)

	// 限制标题长度
	if len(cleanTitle) > 100 {
		cleanTitle = cleanTitle[:100]
	}

	ext := format
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	return filepath.Join(outputDir, cleanTitle+ext)
}

// parseDuration 解析时长字符串为秒数
func parseDuration(durationStr string) int {
	// 支持格式：HH:MM:SS, MM:SS, SS
	parts := strings.Split(durationStr, ":")
	if len(parts) == 0 {
		return 0
	}

	var seconds int
	multiplier := 1

	// 从后往前解析
	for i := len(parts) - 1; i >= 0; i-- {
		var val int
		fmt.Sscanf(parts[i], "%d", &val)
		seconds += val * multiplier
		multiplier *= 60
	}

	return seconds
}

// CommandRunner 用于执行外部命令的接口（便于测试）
type CommandRunner interface {
	Run(cmd *exec.Cmd) error
	Output(cmd *exec.Cmd) ([]byte, error)
}

// DefaultCommandRunner 默认的命令执行器
type DefaultCommandRunner struct{}

func (r *DefaultCommandRunner) Run(cmd *exec.Cmd) error {
	return cmd.Run()
}

func (r *DefaultCommandRunner) Output(cmd *exec.Cmd) ([]byte, error) {
	return cmd.Output()
}
