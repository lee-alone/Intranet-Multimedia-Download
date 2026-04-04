package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 固定的 User-Agent（最新 Chrome Windows 10 Standard UA）
// 用于与导出 Cookie 时的浏览器 UA 保持一致，降低风控风险
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// YtdlpEngine 是 yt-dlp 下载引擎的实现
type YtdlpEngine struct {
	execPath      string
	status        EngineStatus
	lastError     error
	commandRunner CommandRunner
	lastProgressMu sync.Mutex
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

		// 根据 UseCookies 选项决定使用认证模式还是纯净模式
		// useAuth 直接由 options.UseCookies 决定，不再有重试回滚逻辑
		useAuth := options.UseCookies

		// 执行单一模式下载
		dlResult := e.runYtdlp(ctx, url, options, useAuth, progressChan)

		// 处理结果
		if !dlResult.Success {
			result = DownloadResult{
				Success: false,
				Error:   dlResult.Error,
			}
			e.status = EngineStatusError
			e.lastError = result.Error
		} else {
			result = DownloadResult{
				Success:    true,
				OutputPath: dlResult.FilePath,
				Title:      dlResult.VideoTitle,
			}
			e.status = EngineStatusIdle
		}
	}()

	return progressChan
}

// isAuthRelatedError 判断错误是否由认证相关原因导致
// 用于触发回滚机制，从"认证模式"降级到"纯净模式"
func isAuthRelatedError(errMsg string) bool {
	// 统一弯撇号：将弯撇号（' U+2019）替换为直撇号（' U+0027）
	// 确保无论 yt-dlp 输出使用哪种撇号都能匹配
	errLower := strings.ToLower(strings.ReplaceAll(errMsg, "'", "'"))

	// 认证/风控相关错误特征
	authErrorPatterns := []string{
		"sign in to confirm you're not a bot",  // 机器人验证
		"sign in to confirm you are not a bot", // 变体（直撇号）
		"sign in to confirm your age",          // 年龄验证
		"requested format is not available",    // 格式不可用（可能是认证限制）
		"private video",                        // 私有视频（需要认证）
		"members-only",                         // 会员专属（需要认证）
		"this video is unavailable",            // 视频不可用（可能被限制）
		"account has been terminated",          // 账号被封
		"verify it's you",                      // 身份验证
		"verify it's you",                      // 变体（直撇号）
		"consent cookie",                       // Cookie 同意
		"age-restricted",                       // 年龄限制
		"login required",                       // 需要登录
		"authentication required",              // 需要认证
	}

	for _, pattern := range authErrorPatterns {
		if strings.Contains(errLower, pattern) {
			return true
		}
	}

	return false
}

// ytdlpExecutionResult yt-dlp 执行结果
type ytdlpExecutionResult struct {
	Success      bool
	Error        error
	LastProgress *DownloadProgress
	VideoTitle   string
	FilePath     string
	ErrorOutput  string // 完整的错误输出
}

// runYtdlp 执行 yt-dlp 下载命令
// useAuth: 是否使用认证相关参数（Cookies、UA、ExtractorArgs）
func (e *YtdlpEngine) runYtdlp(
	ctx context.Context,
	url string,
	options DownloadOptions,
	useAuth bool,
	progressChan chan DownloadProgress,
) *ytdlpExecutionResult {
	result := &ytdlpExecutionResult{}

	// 获取视频标题（在下载前）
	// 使用 --encoding UTF-8 强制输出 UTF-8 编码，避免 Windows GBK 乱码
	isYouTubeURL := strings.Contains(strings.ToLower(url), "youtube.com") || strings.Contains(strings.ToLower(url), "youtu.be")

	var titleArgs []string
	if useAuth && options.CookieFile != "" {
		// 认证模式：携带 Cookie 获取标题
		titleArgs = []string{"--encoding", "UTF-8", "--print", "title", "--cookies", options.CookieFile}
		// 关键点：对于 YouTube，只传 Cookie，不传 UA（避免 UA 不匹配触发风控）
		if !isYouTubeURL {
			userAgent := options.UserAgent
			if userAgent == "" {
				userAgent = DefaultUserAgent
			}
			titleArgs = append(titleArgs, "--user-agent", userAgent)
		}
	} else {
		// 纯净模式：不携带 Cookie
		titleArgs = []string{"--encoding", "UTF-8", "--print", "title"}
	}
	titleArgs = append(titleArgs, url)

	titleCmd := exec.CommandContext(ctx, e.execPath, titleArgs...)
	titleOutput, err := titleCmd.Output()
	if err == nil {
		result.VideoTitle = strings.TrimSpace(string(titleOutput))
	}

	// 构建 yt-dlp 命令参数
	args := []string{
		"--no-color",
		"--newline",
		"--progress",
		"--encoding", "UTF-8",
		"--merge-output-format", "mp4",
	}

	// 指定 ffmpeg 路径
	ffmpegPath := filepath.Join(filepath.Dir(e.execPath), "ffmpeg.exe")
	if _, statErr := os.Stat(ffmpegPath); statErr == nil {
		args = append(args, "--ffmpeg-location", ffmpegPath)
	}

	// 输出目录和格式
	if options.OutputDir != "" {
		if options.TaskID != "" {
			args = append(args, "-o", filepath.Join(options.OutputDir, options.TaskID+".%(ext)s"))
		} else {
			args = append(args, "-o", filepath.Join(options.OutputDir, "%(id)s.%(ext)s"))
		}
	} else {
		if options.TaskID != "" {
			args = append(args, "-o", options.TaskID+".%(ext)s")
		} else {
			args = append(args, "-o", "%(id)s.%(ext)s")
		}
	}

	// === 画质选择：极简策略 ===
	// 对于 YouTube 站点，尽量不加干预，让它走官方默认 4K 逻辑
	// isYouTubeURL 已在上方获取

	if options.Quality == "" || options.Quality == "best" {
		// 最高画质：对于 YouTube，完全不传 -f 参数，让它自动匹配最强 4K 合并方案
		// 对于其他站点，使用官方推荐的标准格式
		if !isYouTubeURL {
			args = append(args, "-f", "bestvideo*+bestaudio/best")
		}
		// YouTube 走默认逻辑，不干预
	} else {
		// 用户指定了具体分辨率（如 1080p），才做计算
		height := strings.TrimSuffix(options.Quality, "p")
		args = append(args, "-f", "bestvideo[height<="+height+"][vcodec!=none]+bestaudio/best[height<="+height+"]/best")
	}

	// === 认证模式 vs 纯净模式 的分水岭 ===
	if useAuth {
		// Cookie 文件
		if options.CookieFile != "" {
			args = append(args, "--cookies", options.CookieFile)
		}

		// User-Agent：仅当真正上传了 Cookies 时，才注入配套的 User-Agent
		// 关键点：对于 YouTube，只传 Cookie，不传 Extractor Args 和 UA
		if options.CookieFile != "" && !isYouTubeURL {
			// 非 YouTube 站点：注入 UA 和提取器参数
			userAgent := options.UserAgent
			if userAgent == "" {
				userAgent = DefaultUserAgent
			}
			args = append(args, "--user-agent", userAgent)

			// Bilibili 提取器参数
			if strings.Contains(strings.ToLower(url), "bilibili.com") || strings.Contains(strings.ToLower(url), "b23.tv") {
				args = append(args, "--extractor-args", "bilibili:prefer_multi_flv=true")
			}
		}
		// 移除 --force-ipv4：让 yt-dlp 自行决定使用哪个 IP 栈，这才是最正宗的官方默认
	}
	// 纯净模式：不添加上述认证相关参数

	// === 续传属性：所有模式统一追加 ===
	args = append(args, "--continue")
	args = append(args, "--retries", "10")
	args = append(args, "--fragment-retries", "10")

	// 超时设置（两种模式都添加）
	if options.Timeout > 0 {
		args = append(args, "--socket-timeout", strconv.Itoa(int(options.Timeout.Seconds())))
	}

	// 代理（两种模式都添加）
	if options.Proxy != "" {
		args = append(args, "--proxy", options.Proxy)
	}

	// 添加 URL
	args = append(args, url)

	// 创建命令
	cmd := exec.CommandContext(ctx, e.execPath, args...)

	// 获取标准输出
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		result.Error = fmt.Errorf("failed to get stdout: %w", err)
		safeSendProgress(progressChan, DownloadProgress{Status: result.Error.Error()})
		return result
	}

	// 获取标准错误
	stderr, err := cmd.StderrPipe()
	if err != nil {
		result.Error = fmt.Errorf("failed to get stderr: %w", err)
		safeSendProgress(progressChan, DownloadProgress{Status: result.Error.Error()})
		return result
	}

	// 启动命令
	if err := cmd.Start(); err != nil {
		result.Error = fmt.Errorf("failed to start command: %w", err)
		safeSendProgress(progressChan, DownloadProgress{Status: result.Error.Error()})
		return result
	}

	// 用于收集错误输出
	var errorOutput strings.Builder
	var lastProgressMu sync.Mutex
	var errorSent bool

	// 解析 stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if prog, ok := e.parseProgress(line); ok {
				prog.Status = line
				safeSendProgress(progressChan, *prog)
				lastProgressMu.Lock()
				result.LastProgress = prog
				lastProgressMu.Unlock()
			} else if strings.Contains(strings.ToLower(line), "error") {
				errorOutput.WriteString(line)
				lastProgressMu.Lock()
				pct := 0.0
				if result.LastProgress != nil {
					pct = result.LastProgress.Percent
				}
				errProg := &DownloadProgress{
					Percent: pct,
					Status:  "error: " + line,
				}
				safeSendProgress(progressChan, *errProg)
				errorSent = true
				lastProgressMu.Unlock()
			}
			// 解析文件路径
			if strings.Contains(line, "[download] Destination:") {
				filePath := strings.TrimSpace(strings.SplitN(line, "Destination:", 2)[1])
				if !filepath.IsAbs(filePath) && options.OutputDir != "" {
					filePath = filepath.Join(options.OutputDir, filePath)
				}
				lastProgressMu.Lock()
				if result.LastProgress != nil {
					result.LastProgress.FilePath = filePath
					result.FilePath = filePath
				}
				lastProgressMu.Unlock()
			}
			if strings.Contains(line, "[Merger] Merging formats into") {
				filePath := strings.TrimSpace(strings.SplitN(line, "Merging formats into", 2)[1])
				filePath = strings.Trim(filePath, "\"")
				if !filepath.IsAbs(filePath) && options.OutputDir != "" {
					filePath = filepath.Join(options.OutputDir, filePath)
				}
				lastProgressMu.Lock()
				if result.LastProgress != nil {
					result.LastProgress.FilePath = filePath
					result.FilePath = filePath
				}
				lastProgressMu.Unlock()
			}
		}
	}()

	// 解析 stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			errorOutput.WriteString(line + "\n") // 收集错误输出用于后续分析

			if prog, ok := e.parseProgress(line); ok {
				prog.Status = line
				safeSendProgress(progressChan, *prog)
				lastProgressMu.Lock()
				result.LastProgress = prog
				lastProgressMu.Unlock()
			} else if strings.Contains(strings.ToLower(line), "error") {
				lastProgressMu.Lock()
				pct := 0.0
				if result.LastProgress != nil {
					pct = result.LastProgress.Percent
				}
				errProg := &DownloadProgress{
					Percent: pct,
					Status:  "error: " + line,
				}
				safeSendProgress(progressChan, *errProg)
				errorSent = true
				lastProgressMu.Unlock()
			}
			// 解析文件路径
			if strings.Contains(line, "[download] Destination:") {
				filePath := strings.TrimSpace(strings.SplitN(line, "Destination:", 2)[1])
				if !filepath.IsAbs(filePath) && options.OutputDir != "" {
					filePath = filepath.Join(options.OutputDir, filePath)
				}
				lastProgressMu.Lock()
				if result.LastProgress != nil {
					result.LastProgress.FilePath = filePath
					result.FilePath = filePath
				}
				lastProgressMu.Unlock()
			}
			if strings.Contains(line, "[Merger] Merging formats into") {
				filePath := strings.TrimSpace(strings.SplitN(line, "Merging formats into", 2)[1])
				filePath = strings.Trim(filePath, "\"")
				if !filepath.IsAbs(filePath) && options.OutputDir != "" {
					filePath = filepath.Join(options.OutputDir, filePath)
				}
				lastProgressMu.Lock()
				if result.LastProgress != nil {
					result.LastProgress.FilePath = filePath
					result.FilePath = filePath
				}
				lastProgressMu.Unlock()
			}
		}
	}()

	// 等待命令完成
	err = cmd.Wait()
	result.ErrorOutput = errorOutput.String()

	// 如果进程异常退出且未发送过错误信息，构造错误进度发送
	if err != nil && !errorSent {
		lastProgressMu.Lock()
		pct := 0.0
		var fp, title string
		if result.LastProgress != nil {
			pct = result.LastProgress.Percent
			fp = result.LastProgress.FilePath
			title = result.LastProgress.Title
		}
		errProg := DownloadProgress{
			Percent:  pct,
			Status:   "error: download failed: " + err.Error(),
			FilePath: fp,
			Title:    title,
		}
		safeSendProgress(progressChan, errProg)
		errorSent = true
		lastProgressMu.Unlock()
	}

	// 下载完成后，如果指定了 TaskID 但文件路径不是 TaskID，则重命名文件
	lastProgressMu.Lock()
	if options.TaskID != "" && result.LastProgress != nil && result.LastProgress.FilePath != "" {
		dir := filepath.Dir(result.LastProgress.FilePath)
		baseName := filepath.Base(result.LastProgress.FilePath)
		ext := filepath.Ext(baseName)

		if !strings.HasPrefix(baseName, options.TaskID) {
			newFileName := options.TaskID + ext
			newFilePath := filepath.Join(dir, newFileName)

			if _, statErr := os.Stat(newFilePath); os.IsNotExist(statErr) {
				if renameErr := os.Rename(result.LastProgress.FilePath, newFilePath); renameErr == nil {
					result.LastProgress.FilePath = newFilePath
					result.FilePath = newFilePath
				}
			} else if statErr == nil {
				os.Remove(result.LastProgress.FilePath)
				result.LastProgress.FilePath = newFilePath
				result.FilePath = newFilePath
			}
		}
	}
	lastProgressMu.Unlock()

	// 发送最终进度
	lastProgressMu.Lock()
	if result.LastProgress != nil {
		if result.LastProgress.Percent >= 100 && result.LastProgress.FilePath == "" && options.TaskID != "" && options.OutputDir != "" && !errorSent {
			videoExts := []string{".mp4", ".mkv", ".webm", ".avi", ".mov", ".flv", ".wmv", ".m4v"}
			for _, ext := range videoExts {
				testPath := filepath.Join(options.OutputDir, options.TaskID+ext)
				if _, statErr := os.Stat(testPath); statErr == nil {
					result.LastProgress.FilePath = testPath
					result.FilePath = testPath
					break
				}
			}

			if result.LastProgress.FilePath == "" {
				if options.OutputDir != "" {
					cmd := exec.Command(e.execPath, "--simulate", "--print", "filename", url)
					output, getErr := cmd.Output()
					if getErr == nil {
						filename := strings.TrimSpace(string(output))
						if filename != "" {
							result.LastProgress.FilePath = filepath.Join(options.OutputDir, filename)
							result.FilePath = filepath.Join(options.OutputDir, filename)
						}
					}
				}
			}
		}

		if result.VideoTitle != "" {
			result.LastProgress.Title = result.VideoTitle
		}
		if result.LastProgress.FilePath != "" && !filepath.IsAbs(result.LastProgress.FilePath) {
			if options.OutputDir != "" {
				result.LastProgress.FilePath = filepath.Join(options.OutputDir, result.LastProgress.FilePath)
				result.FilePath = filepath.Join(options.OutputDir, result.FilePath)
			} else {
				if absPath, absErr := filepath.Abs(result.LastProgress.FilePath); absErr == nil {
					result.LastProgress.FilePath = absPath
					result.FilePath = absPath
				}
			}
		}

		finalProgress := *result.LastProgress
		lastProgressMu.Unlock()
		if !errorSent {
			safeSendProgress(progressChan, finalProgress)
		}
	} else {
		lastProgressMu.Unlock()
	}

	// 处理结果
	if err != nil {
		result.Error = fmt.Errorf("download failed: %w", err)
	} else {
		result.Success = true
	}

	return result
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
