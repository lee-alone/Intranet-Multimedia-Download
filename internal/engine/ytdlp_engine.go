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

// YtdlpEngine 是 yt-dlp 下载引擎的实现
type YtdlpEngine struct {
	execPath      string
	status        EngineStatus
	lastError     error
	commandRunner CommandRunner
}

// YtdlpEngine 配置
type YtdlpConfig struct {
	ExecPath   string
	OutputDir  string
	Quality    string
	Timeout    time.Duration
	MaxRetries int
	CookieFile string
	UserAgent  string
	Proxy      string
}

// NewYtdlpEngine 创建 yt-dlp 引擎
func NewYtdlpEngine(config YtdlpConfig) *YtdlpEngine {
	execPath := config.ExecPath
	if execPath == "" {
		execPath = "yt-dlp" // 默认在 PATH 中查找
	}

	return &YtdlpEngine{
		execPath:      execPath,
		status:        EngineStatusIdle,
		commandRunner: &DefaultCommandRunner{},
	}
}

// Name 返回引擎名称
func (e *YtdlpEngine) Name() string {
	return "yt-dlp"
}

// Status 返回引擎状态
func (e *YtdlpEngine) Status() EngineStatus {
	return e.status
}

// CanHandle 判断 yt-dlp 是否可以处理给定的 URL
func (e *YtdlpEngine) CanHandle(url string) bool {
	// yt-dlp 支持众多网站，这里列出常见的
	supportedDomains := []string{
		"youtube.com",
		"youtu.be",
		"bilibili.com",
		"b23.tv",
		"youku.com",
		"v.qq.com",
		"iqiyi.com",
		"mgtv.com",
		"sohu.com",
		"acfun.cn",
		"douyin.com",
		"tiktok.com",
		"twitter.com",
		"x.com",
		"facebook.com",
		"instagram.com",
		"vimeo.com",
		"dailymotion.com",
		"twitch.tv",
	}

	urlLower := strings.ToLower(url)
	for _, domain := range supportedDomains {
		if strings.Contains(urlLower, domain) {
			return true
		}
	}

	// 如果没有匹配到特定域名，yt-dlp 仍然可能支持
	// 这里返回 true 让 yt-dlp 自己尝试处理
	return true
}

// safeSendProgress 安全发送进度（避免 channel 关闭后发送）
func safeSendProgress(ch chan<- DownloadProgress, p DownloadProgress) bool {
	select {
	case ch <- p:
		return true
	default:
		return false
	}
}

// Download 执行下载
func (e *YtdlpEngine) Download(ctx context.Context, url string, options DownloadOptions) <-chan DownloadProgress {
	progressChan := make(chan DownloadProgress, 100)

	e.status = EngineStatusRunning

	go func() {
		defer close(progressChan)

		var result DownloadResult
		var lastProgress *DownloadProgress

		// 构建 yt-dlp 命令参数
		args := []string{
			"--no-color",
			"--newline",
			"--progress",
		}

		// 输出目录和格式
		outputTemplate := ""
		if options.OutputDir != "" {
			outputTemplate = filepath.Join(options.OutputDir, "%(title)s.%(ext)s")
			args = append(args, "-o", outputTemplate)
		} else {
			args = append(args, "-o", "%(title)s.%(ext)s")
		}

		// 画质选择
		if options.Quality != "" && options.Quality != "best" {
			args = append(args, "-f", "bestvideo[height<="+options.Quality+"]+bestaudio/best")
		} else {
			args = append(args, "-f", "best")
		}

		// 超时设置
		if options.Timeout > 0 {
			args = append(args, "--socket-timeout", strconv.Itoa(int(options.Timeout.Seconds())))
		}

		// Cookie 文件
		if options.CookieFile != "" {
			args = append(args, "--cookies", options.CookieFile)
		}

		// User-Agent
		if options.UserAgent != "" {
			args = append(args, "--user-agent", options.UserAgent)
		}

		// 代理
		if options.Proxy != "" {
			args = append(args, "--proxy", options.Proxy)
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
				// 解析文件路径 [download] Destination: /path/to/file
				if strings.Contains(line, "[download] Destination:") {
					filePath := strings.TrimSpace(strings.SplitN(line, "Destination:", 2)[1])
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
				// 解析文件路径 [download] Destination: /path/to/file
				if strings.Contains(line, "[download] Destination:") {
					filePath := strings.TrimSpace(strings.SplitN(line, "Destination:", 2)[1])
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
				// 如果下载完成但没有文件路径，尝试从输出目录和标题构建
				if options.OutputDir != "" {
					// 使用 yt-dlp 获取实际文件名
					cmd := exec.Command(e.execPath, "--simulate", "--print", "filename", url)
					output, err := cmd.Output()
					if err == nil {
						filename := strings.TrimSpace(string(output))
						if filename != "" {
							lastProgress.FilePath = filepath.Join(options.OutputDir, filename)
						}
					}
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

// parseProgress 解析 yt-dlp 进度输出
func (e *YtdlpEngine) parseProgress(line string) (*DownloadProgress, bool) {
	// yt-dlp 进度格式：
	// [download] 50.0% of 10.00MiB in 00:10
	// [download] 100% of 10.00MiB in 00:10
	// [download] 50.0% of 10.00MiB at 1.50MiB/s ETA 0:00:10

	// 匹配进度百分比 - 支持多种格式
	// 格式 1: [download] 50.0%
	// 格式 2: 50.0% (无前缀)
	percentRe := regexp.MustCompile(`(?:\[download\]\s*)?(\d+\.?\d*)%`)
	matches := percentRe.FindStringSubmatch(line)
	if len(matches) < 2 {
		return nil, false
	}

	percent, _ := strconv.ParseFloat(matches[1], 64)

	progress := &DownloadProgress{
		Percent: percent,
		Status:  line,
	}

	// 匹配已下载大小
	sizeRe := regexp.MustCompile(`of\s+([\d.]+)([KMGT]?B)`)
	if matches := sizeRe.FindStringSubmatch(line); len(matches) >= 3 {
		size, _ := strconv.ParseFloat(matches[1], 64)
		unit := matches[2]
		// 转换为字节
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
	speedRe := regexp.MustCompile(`at\s+([\d.]+)([KMGT]?B/s)`)
	if matches := speedRe.FindStringSubmatch(line); len(matches) >= 3 {
		speed, _ := strconv.ParseFloat(matches[1], 64)
		unit := matches[2]
		switch unit {
		case "KB/s":
			progress.Speed = speed * 1024
		case "MB/s":
			progress.Speed = speed * 1024 * 1024
		case "GB/s":
			progress.Speed = speed * 1024 * 1024 * 1024
		default:
			progress.Speed = speed
		}
	}

	// 匹配 ETA
	etaRe := regexp.MustCompile(`ETA\s+(\d+):(\d+):(\d+)`)
	if matches := etaRe.FindStringSubmatch(line); len(matches) >= 4 {
		hours, _ := strconv.Atoi(matches[1])
		mins, _ := strconv.Atoi(matches[2])
		secs, _ := strconv.Atoi(matches[3])
		progress.ETA = hours*3600 + mins*60 + secs
	}

	return progress, true
}

// GetVersion 获取 yt-dlp 版本
func (e *YtdlpEngine) GetVersion() (string, error) {
	cmd := exec.Command(e.execPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get yt-dlp version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// IsAvailable 检查 yt-dlp 是否可用
func (e *YtdlpEngine) IsAvailable() bool {
	cmd := exec.Command(e.execPath, "--version")
	err := cmd.Run()
	return err == nil
}

// YtdlpInfo 视频信息结构
type YtdlpInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Duration int    `json:"duration"` // 秒
	Formats  []struct {
		FormatID  string  `json:"format_id"`
		Height    int     `json:"height"`
		FPS       float64 `json:"fps"`
		TBR       float64 `json:"tbr"` // 码率 kbps
		FileSize  int64   `json:"filesize"`
		Format    string  `json:"format"`
		Extension string  `json:"ext"`
	} `json:"formats"`
	Thumbnail  string `json:"thumbnail"`
	Uploader   string `json:"uploader"`
	UploadDate string `json:"upload_date"`
	ViewCount  int64  `json:"view_count"`
	LikeCount  int64  `json:"like_count"`
}

// GetVideoInfo 获取视频信息
func (e *YtdlpEngine) GetVideoInfo(url string) (*YtdlpInfo, error) {
	cmd := exec.Command(e.execPath, "-J", url)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get video info: %w", err)
	}

	var info YtdlpInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("failed to parse video info: %w", err)
	}

	return &info, nil
}
