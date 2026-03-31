package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LuxEngine 是 lux 下载引擎的实现
// lux (原 annie) 是一个支持多个视频网站的下载工具
type LuxEngine struct {
	execPath      string
	status        EngineStatus
	lastError     error
	commandRunner CommandRunner
}

// LuxConfig Lux 引擎配置
type LuxConfig struct {
	ExecPath   string
	OutputDir  string
	Quality    string
	Timeout    time.Duration
	MaxRetries int
	CookieFile string
	UserAgent  string
	Proxy      string
}

// NewLuxEngine 创建 lux 引擎
func NewLuxEngine(config LuxConfig) *LuxEngine {
	execPath := config.ExecPath
	if execPath == "" {
		execPath = "lux" // 默认在 PATH 中查找
	}

	return &LuxEngine{
		execPath:      execPath,
		status:        EngineStatusIdle,
		commandRunner: &DefaultCommandRunner{},
	}
}

// Name 返回引擎名称
func (e *LuxEngine) Name() string {
	return "lux"
}

// Status 返回引擎状态
func (e *LuxEngine) Status() EngineStatus {
	return e.status
}

// CanHandle 判断 lux 是否可以处理给定的 URL
func (e *LuxEngine) CanHandle(url string) bool {
	// lux 支持的网站列表
	supportedDomains := []string{
		"bilibili.com",
		"b23.tv",
		"youtube.com",
		"youtu.be",
		"youku.com",
		"iqiyi.com",
		"mgtv.com",
		"sohu.com",
		"acfun.cn",
		"ixigua.com",
		"le.com",
		"163.com",
		"m.miluo.com",
		"new.qq.com",
		"zhihu.com",
		"douyin.com",
		"huoshan.com",
	}

	urlLower := strings.ToLower(url)
	for _, domain := range supportedDomains {
		if strings.Contains(urlLower, domain) {
			return true
		}
	}

	// 如果不匹配已知域名，检查是否是有效的 HTTP(S) URL
	// 让 lux 自己尝试处理未知网站
	return strings.HasPrefix(urlLower, "http://") || strings.HasPrefix(urlLower, "https://")
}

// Download 执行下载
func (e *LuxEngine) Download(ctx context.Context, url string, options DownloadOptions) <-chan DownloadProgress {
	progressChan := make(chan DownloadProgress, 100)

	e.status = EngineStatusRunning

	go func() {
		defer close(progressChan)

		var result DownloadResult
		var lastProgress *DownloadProgress

		// 构建 lux 命令参数
		args := []string{
			"--info", // 输出详细信息
		}

		// 输出目录
		if options.OutputDir != "" {
			args = append(args, "-o", options.OutputDir)
		}

		// 画质选择
		if options.Quality != "" && options.Quality != "best" {
			// lux 使用 -p 参数选择画质
			args = append(args, "-p", options.Quality)
		}

		// 超时设置
		if options.Timeout > 0 {
			// lux 没有直接的超时参数，依赖上下文控制
		}

		// 代理
		if options.Proxy != "" {
			args = append(args, "-proxy", options.Proxy)
		}

		// 添加 URL
		args = append(args, url)

		// 创建命令
		cmd := exec.CommandContext(ctx, e.execPath, args...)

		// 获取标准输出用于解析进度
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			result = DownloadResult{
				Success: false,
				Error:   fmt.Errorf("failed to get stdout: %w", err),
			}
			safeSendProgress(progressChan, DownloadProgress{Status: result.Error.Error()})
			return
		}

		// 获取标准错误用于解析进度
		stderr, err := cmd.StderrPipe()
		if err != nil {
			result = DownloadResult{
				Success: false,
				Error:   fmt.Errorf("failed to get stderr: %w", err),
			}
			safeSendProgress(progressChan, DownloadProgress{Status: result.Error.Error()})
			return
		}

		// 启动命令
		if err := cmd.Start(); err != nil {
			result = DownloadResult{
				Success: false,
				Error:   fmt.Errorf("failed to start command: %w", err),
			}
			safeSendProgress(progressChan, DownloadProgress{Status: result.Error.Error()})
			return
		}

		// 解析进度输出
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				line := scanner.Text()
				if prog, ok := e.parseProgress(line); ok {
					prog.Status = line
					safeSendProgress(progressChan, *prog)
					lastProgress = prog
				}
				// 解析文件路径 - lux 输出格式：Saving to: /path/to/file
				if strings.Contains(line, "Saving to:") {
					filePath := strings.TrimSpace(strings.SplitN(line, "Saving to:", 2)[1])
					// 如果是相对路径，转换为绝对路径
					if !filepath.IsAbs(filePath) && options.OutputDir != "" {
						filePath = filepath.Join(options.OutputDir, filePath)
					}
					if lastProgress != nil {
						lastProgress.FilePath = filePath
					}
				}
			}
		}()

		// 解析 stderr 中的进度
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				if prog, ok := e.parseProgress(line); ok {
					prog.Status = line
					safeSendProgress(progressChan, *prog)
					lastProgress = prog
				}
				// 解析文件路径 - lux 输出格式：Saving to: /path/to/file
				if strings.Contains(line, "Saving to:") {
					filePath := strings.TrimSpace(strings.SplitN(line, "Saving to:", 2)[1])
					// 如果是相对路径，转换为绝对路径
					if !filepath.IsAbs(filePath) && options.OutputDir != "" {
						filePath = filepath.Join(options.OutputDir, filePath)
					}
					if lastProgress != nil {
						lastProgress.FilePath = filePath
					}
				}
			}
		}()

		// 等待命令完成
		err = cmd.Wait()

		// 发送最终进度（包含文件路径）
		if lastProgress != nil {
			if lastProgress.Percent >= 100 && lastProgress.FilePath == "" {
				// 如果下载完成但没有文件路径，尝试从输出目录构建
				if options.OutputDir != "" {
					// lux 默认使用当前目录，这里假设文件在输出目录中
					// 实际文件名需要从标题推断
					lastProgress.FilePath = filepath.Join(options.OutputDir, "video.mp4")
				}
			}
			// 确保文件路径是绝对路径
			if lastProgress.FilePath != "" && !filepath.IsAbs(lastProgress.FilePath) {
				if options.OutputDir != "" {
					lastProgress.FilePath = filepath.Join(options.OutputDir, lastProgress.FilePath)
				} else {
					if absPath, err := filepath.Abs(lastProgress.FilePath); err == nil {
						lastProgress.FilePath = absPath
					}
				}
			}
			safeSendProgress(progressChan, *lastProgress)
		}

		// 处理结果
		if err != nil {
			result = DownloadResult{
				Success: false,
				Error:   fmt.Errorf("download failed: %w", err),
			}
			e.status = EngineStatusError
			e.lastError = result.Error
		} else {
			result = DownloadResult{
				Success:    true,
				OutputPath: lastProgress.Status,
			}
			e.status = EngineStatusIdle
		}
	}()

	return progressChan
}

// parseProgress 解析 lux 进度输出
func (e *LuxEngine) parseProgress(line string) (*DownloadProgress, bool) {
	// lux 进度格式示例：
	// [download] 100% of 10.00MB
	// 或：100% 10.00MB/10.00MB 1.5MB/s 0s

	// 匹配百分比进度
	percentRe := regexp.MustCompile(`(\d+\.?\d*)%`)
	matches := percentRe.FindStringSubmatch(line)
	if len(matches) < 2 {
		return nil, false
	}

	percent, _ := strconv.ParseFloat(matches[1], 64)

	progress := &DownloadProgress{
		Percent: percent,
		Status:  line,
	}

	// 匹配大小信息
	sizeRe := regexp.MustCompile(`([\d.]+)([KMGT]?B)`)
	if matches := sizeRe.FindStringSubmatch(line); len(matches) >= 3 {
		size, _ := strconv.ParseFloat(matches[1], 64)
		unit := matches[2]
		switch unit {
		case "KB":
			progress.Downloaded = int64(size * 1024)
		case "MB":
			progress.Downloaded = int64(size * 1024 * 1024)
		case "GB":
			progress.Downloaded = int64(size * 1024 * 1024 * 1024)
		case "TB":
			progress.Downloaded = int64(size * 1024 * 1024 * 1024 * 1024)
		default:
			progress.Downloaded = int64(size)
		}
	}

	// 匹配速度
	speedRe := regexp.MustCompile(`([\d.]+)([KMGT]?B)/s`)
	if matches := speedRe.FindStringSubmatch(line); len(matches) >= 3 {
		speed, _ := strconv.ParseFloat(matches[1], 64)
		unit := matches[2]
		switch unit {
		case "KB":
			progress.Speed = speed * 1024
		case "MB":
			progress.Speed = speed * 1024 * 1024
		case "GB":
			progress.Speed = speed * 1024 * 1024 * 1024
		default:
			progress.Speed = speed
		}
	}

	return progress, true
}

// GetVersion 获取 lux 版本
func (e *LuxEngine) GetVersion() (string, error) {
	cmd := exec.Command(e.execPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get lux version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// IsAvailable 检查 lux 是否可用
func (e *LuxEngine) IsAvailable() bool {
	cmd := exec.Command(e.execPath, "--version")
	err := cmd.Run()
	return err == nil
}

// LuxInfo 视频信息结构
type LuxInfo struct {
	Title    string `json:"title"`
	Duration int    `json:"duration"`
	Streams  map[string]struct {
		Size    int64    `json:"size"`
		Quality string   `json:"quality"`
		URLs    []string `json:"urls"`
	} `json:"streams"`
}

// GetVideoInfo 获取视频信息
func (e *LuxEngine) GetVideoInfo(url string) (*LuxInfo, error) {
	cmd := exec.Command(e.execPath, "--json", url)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get video info: %w", err)
	}

	var info LuxInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("failed to parse video info: %w", err)
	}

	return &info, nil
}
