package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 固定的 User-Agent（最新 Chrome Windows 10 Standard UA）
// 用于与导出 Cookie 时的浏览器 UA 保持一致，降低风控风险
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// YtdlpEngine 是 yt-dlp 下载引擎的实现
type YtdlpEngine struct {
	execPath       string
	outputDir      string
	tempDir        string
	status         EngineStatus
	lastError      error
	commandRunner  CommandRunner
	lastProgressMu sync.Mutex
}

// YtdlpEngine 配置
type YtdlpConfig struct {
	ExecPath   string
	OutputDir  string
	TempDir    string
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
		outputDir:     config.OutputDir,
		tempDir:       config.TempDir,
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
// 职责：仅负责调度，参数构建和解析交给专门模块
func (e *YtdlpEngine) Download(ctx context.Context, url string, options DownloadOptions) <-chan DownloadProgress {
	progressChan := make(chan DownloadProgress, 100)

	e.status = EngineStatusRunning

	go func() {
		defer close(progressChan)

		var result DownloadResult

		// 根据 UseCookies 选项决定使用认证模式还是纯净模式
		useAuth := options.UseCookies

		// 执行下载
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
// 职责：进程调度、进度推送、文件重命名
func (e *YtdlpEngine) runYtdlp(
	ctx context.Context,
	url string,
	options DownloadOptions,
	useAuth bool,
	progressChan chan DownloadProgress,
) *ytdlpExecutionResult {
	result := &ytdlpExecutionResult{}

	// 注入引擎级别的目录配置（如果 options 中未设置）
	if options.OutputDir == "" && e.outputDir != "" {
		options.OutputDir = e.outputDir
	}
	if options.TempDir == "" && e.tempDir != "" {
		options.TempDir = e.tempDir
	}

	// === 第一步：获取视频标题（在下载前） ===
	titleArgs := buildTitleArgs(url, options, useAuth)
	titleCmd := buildTitleCmd(ctx, e.execPath, titleArgs)
	titleOutput, err := titleCmd.Output()
	if err == nil {
		result.VideoTitle = strings.TrimSpace(string(titleOutput))
	}

	// === 第二步：构建下载命令参数 ===
	args := buildYtdlpArgs(url, options, useAuth, e.execPath)
	// 调试日志：输出完整的命令行参数
	log.Printf("[DEBUG] yt-dlp 命令: %s %v", e.execPath, args)
	cmd := exec.CommandContext(ctx, e.execPath, args...)

	// === 第三步：启动进程并获取管道 ===
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		result.Error = fmt.Errorf("failed to get stdout: %w", err)
		safeSendProgress(progressChan, DownloadProgress{Status: result.Error.Error()})
		return result
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		result.Error = fmt.Errorf("failed to get stderr: %w", err)
		safeSendProgress(progressChan, DownloadProgress{Status: result.Error.Error()})
		return result
	}

	if err := cmd.Start(); err != nil {
		result.Error = fmt.Errorf("failed to start command: %w", err)
		safeSendProgress(progressChan, DownloadProgress{Status: result.Error.Error()})
		return result
	}

	// === 第四步：异步解析输出 ===
	var errorOutput strings.Builder
	var lastProgressMu sync.Mutex
	var errorSent bool

	// 解析 stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			e.processOutputLine(line, &result, &lastProgressMu, &errorOutput, &errorSent, progressChan, options)
		}
	}()

	// 解析 stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			errorOutput.WriteString(line + "\n")
			e.processOutputLine(line, &result, &lastProgressMu, &errorOutput, &errorSent, progressChan, options)
		}
	}()

	// === 第五步：等待命令完成 ===
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

	// === 第六步：下载完成后，重命名文件（如果指定了 TaskID） ===
	e.renameFileIfNeeded(&result, options, &lastProgressMu)

	// === 第七步：发送最终进度 ===
	e.sendFinalProgress(&result, options, url, &lastProgressMu, errorSent, progressChan)

	// 处理结果
	if err != nil {
		result.Error = fmt.Errorf("download failed: %w", err)
	} else {
		result.Success = true
	}

	return result
}

// processOutputLine 处理单行输出（解析进度、提取文件路径、检测错误）
func (e *YtdlpEngine) processOutputLine(
	line string,
	result **ytdlpExecutionResult,
	lastProgressMu *sync.Mutex,
	errorOutput *strings.Builder,
	errorSent *bool,
	progressChan chan DownloadProgress,
	options DownloadOptions,
) {
	if prog, ok := parseProgress(line); ok {
		prog.Status = line
		safeSendProgress(progressChan, *prog)
		lastProgressMu.Lock()
		(*result).LastProgress = prog
		lastProgressMu.Unlock()
	} else if strings.Contains(strings.ToLower(line), "error") {
		errorOutput.WriteString(line)
		lastProgressMu.Lock()
		pct := 0.0
		if (*result).LastProgress != nil {
			pct = (*result).LastProgress.Percent
		}
		errProg := &DownloadProgress{
			Percent: pct,
			Status:  "error: " + line,
		}
		safeSendProgress(progressChan, *errProg)
		*errorSent = true
		lastProgressMu.Unlock()
	}

	// 解析文件路径
	e.extractFilePath(line, result, lastProgressMu, options)
}

// extractFilePath 从输出行中提取文件路径
func (e *YtdlpEngine) extractFilePath(
	line string,
	result **ytdlpExecutionResult,
	lastProgressMu *sync.Mutex,
	options DownloadOptions,
) {
	if strings.Contains(line, "[download] Destination:") {
		filePath := strings.TrimSpace(strings.SplitN(line, "Destination:", 2)[1])
		if !filepath.IsAbs(filePath) && options.OutputDir != "" {
			filePath = filepath.Join(options.OutputDir, filePath)
		}
		lastProgressMu.Lock()
		if (*result).LastProgress != nil {
			(*result).LastProgress.FilePath = filePath
			(*result).FilePath = filePath
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
		if (*result).LastProgress != nil {
			(*result).LastProgress.FilePath = filePath
			(*result).FilePath = filePath
		}
		lastProgressMu.Unlock()
	}
}

// renameFileIfNeeded 如果指定了 TaskID 但文件名不匹配，则重命名文件
func (e *YtdlpEngine) renameFileIfNeeded(
	result **ytdlpExecutionResult,
	options DownloadOptions,
	lastProgressMu *sync.Mutex,
) {
	lastProgressMu.Lock()
	defer lastProgressMu.Unlock()

	if options.TaskID == "" || (*result).LastProgress == nil || (*result).LastProgress.FilePath == "" {
		return
	}

	dir := filepath.Dir((*result).LastProgress.FilePath)
	baseName := filepath.Base((*result).LastProgress.FilePath)
	ext := filepath.Ext(baseName)

	if strings.HasPrefix(baseName, options.TaskID) {
		return // 文件名已匹配，无需重命名
	}

	newFileName := options.TaskID + ext
	newFilePath := filepath.Join(dir, newFileName)

	if _, statErr := os.Stat(newFilePath); os.IsNotExist(statErr) {
		if renameErr := os.Rename((*result).LastProgress.FilePath, newFilePath); renameErr == nil {
			(*result).LastProgress.FilePath = newFilePath
			(*result).FilePath = newFilePath
		}
	} else if statErr == nil {
		os.Remove((*result).LastProgress.FilePath)
		(*result).LastProgress.FilePath = newFilePath
		(*result).FilePath = newFilePath
	}
}

// sendFinalProgress 发送最终进度
func (e *YtdlpEngine) sendFinalProgress(
	result **ytdlpExecutionResult,
	options DownloadOptions,
	url string,
	lastProgressMu *sync.Mutex,
	errorSent bool,
	progressChan chan DownloadProgress,
) {
	lastProgressMu.Lock()
	if (*result).LastProgress == nil {
		lastProgressMu.Unlock()
		return
	}

	// 如果进度达到 100% 但文件路径为空，尝试查找文件
	if (*result).LastProgress.Percent >= 100 && (*result).LastProgress.FilePath == "" &&
		options.TaskID != "" && options.OutputDir != "" && !errorSent {
		e.findCompletedFile(result, options, url)
	}

	// 填充标题和绝对路径
	if (*result).VideoTitle != "" {
		(*result).LastProgress.Title = (*result).VideoTitle
	}
	if (*result).LastProgress.FilePath != "" && !filepath.IsAbs((*result).LastProgress.FilePath) {
		if options.OutputDir != "" {
			(*result).LastProgress.FilePath = filepath.Join(options.OutputDir, (*result).LastProgress.FilePath)
			(*result).FilePath = filepath.Join(options.OutputDir, (*result).FilePath)
		} else {
			if absPath, absErr := filepath.Abs((*result).LastProgress.FilePath); absErr == nil {
				(*result).LastProgress.FilePath = absPath
				(*result).FilePath = absPath
			}
		}
	}

	finalProgress := *(*result).LastProgress
	lastProgressMu.Unlock()

	if !errorSent {
		safeSendProgress(progressChan, finalProgress)
	}
}

// findCompletedFile 在输出目录中查找已完成的文件
func (e *YtdlpEngine) findCompletedFile(
	result **ytdlpExecutionResult,
	options DownloadOptions,
	url string,
) {
	videoExts := []string{".mp4", ".mkv", ".webm", ".avi", ".mov", ".flv", ".wmv", ".m4v"}
	for _, ext := range videoExts {
		testPath := filepath.Join(options.OutputDir, options.TaskID+ext)
		if _, statErr := os.Stat(testPath); statErr == nil {
			(*result).LastProgress.FilePath = testPath
			(*result).FilePath = testPath
			return
		}
	}

	// 如果仍然找不到，尝试模拟查询
	if options.OutputDir != "" {
		cmd := exec.Command(e.execPath, "--simulate", "--print", "filename", url)
		output, getErr := cmd.Output()
		if getErr == nil {
			filename := strings.TrimSpace(string(output))
			if filename != "" {
				(*result).LastProgress.FilePath = filepath.Join(options.OutputDir, filename)
				(*result).FilePath = filepath.Join(options.OutputDir, filename)
			}
		}
	}
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
