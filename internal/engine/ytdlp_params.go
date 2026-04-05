package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// isYouTubeURL 判断 URL 是否属于 YouTube 站点
func isYouTubeURL(url string) bool {
	lower := strings.ToLower(url)
	return strings.Contains(lower, "youtube.com") || strings.Contains(lower, "youtu.be")
}

// isBilibiliURL 判断 URL 是否属于 Bilibili 站点
func isBilibiliURL(url string) bool {
	lower := strings.ToLower(url)
	return strings.Contains(lower, "bilibili.com") || strings.Contains(lower, "b23.tv")
}

// buildTitleArgs 构建获取视频标题的命令参数
// useAuth: 是否使用认证模式
func buildTitleArgs(url string, options DownloadOptions, useAuth bool) []string {
	isYT := isYouTubeURL(url)

	var args []string
	// 基础参数：UTF-8 编码 + 打印标题
	args = append(args, "--encoding", "UTF-8", "--print", "title")

	// YouTube 特殊处理：不使用 Cookie，避免触发反爬虫机制
	// YouTube 对 Cookie 检测非常严格，使用 Cookie 反而容易触发 429 错误
	if useAuth && options.CookieFile != "" && !isYT {
		// 认证模式：携带 Cookie（仅非 YouTube 站点）
		args = append(args, "--cookies", options.CookieFile)
		userAgent := options.UserAgent
		if userAgent == "" {
			userAgent = DefaultUserAgent
		}
		args = append(args, "--user-agent", userAgent)
	}
	// 纯净模式：不添加认证相关参数

	return append(args, url)
}

// buildYtdlpArgs 构建 yt-dlp 下载命令参数
// useAuth: 是否使用认证模式（Cookies、UA、ExtractorArgs）
// execPath: yt-dlp 可执行文件路径（用于定位同目录下的 ffmpeg）
// 返回值：完整的命令参数列表
func buildYtdlpArgs(url string, options DownloadOptions, useAuth bool, execPath string) []string {
	isYT := isYouTubeURL(url)

	// === 基础参数（所有模式通用） ===
	args := []string{
		"--no-color",
		"--newline",
		"--progress",
		"--encoding", "UTF-8",
	}

	// 指定 ffmpeg 路径（与 yt-dlp 在同一目录下）
	ffmpegPath := filepath.Join(filepath.Dir(execPath), "ffmpeg.exe")
	if _, statErr := os.Stat(ffmpegPath); statErr == nil {
		args = append(args, "--ffmpeg-location", ffmpegPath)
	}

	// === 输出路径 ===
	args = append(args, buildOutputArgs(options)...)

	// === 临时路径 ===
	args = append(args, buildPathsArgs(options)...)

	// === 画质选择：极简策略 ===
	args = append(args, buildQualityArgs(options.Quality, isYT)...)

	// === 认证模式 vs 纯净模式 的分水岭 ===
	// YouTube 特殊处理：不使用 Cookie，避免触发反爬虫机制
	// YouTube 对 Cookie 检测非常严格，使用 Cookie 反而容易触发 429 错误
	if useAuth && !isYT {
		args = append(args, buildAuthArgs(url, options)...)
	}
	// 纯净模式：不添加认证相关参数

	// === 续传属性：所有模式统一追加 ===
	args = append(args, "--continue")
	args = append(args, "--retries", "10")
	args = append(args, "--fragment-retries", "10")

	// === 超时设置 ===
	if options.Timeout > 0 {
		args = append(args, "--socket-timeout", strconv.Itoa(int(options.Timeout.Seconds())))
	}

	// === 代理设置 ===
	if options.Proxy != "" {
		args = append(args, "--proxy", options.Proxy)
	}

	// 添加 URL
	args = append(args, url)

	return args
}

// buildOutputArgs 构建输出路径参数
func buildOutputArgs(options DownloadOptions) []string {
	if options.OutputDir != "" {
		if options.TaskID != "" {
			return []string{"-o", filepath.Join(options.OutputDir, options.TaskID+".%(ext)s")}
		}
		return []string{"-o", filepath.Join(options.OutputDir, "%(id)s.%(ext)s")}
	}
	if options.TaskID != "" {
		return []string{"-o", options.TaskID + ".%(ext)s"}
	}
	return []string{"-o", "%(id)s.%(ext)s"}
}

// buildPathsArgs 构建 --paths 参数，指定 home 和 temp 路径
func buildPathsArgs(options DownloadOptions) []string {
	var args []string

	if options.OutputDir != "" || options.TempDir != "" {
		// 构建 --paths 参数
		paths := make([]string, 0)

		// 设置 home 路径（用于最终文件输出）
		if options.OutputDir != "" {
			paths = append(paths, fmt.Sprintf("home:%s", options.OutputDir))
		}

		// 设置 temp 路径（用于临时文件、合并过程）
		if options.TempDir != "" {
			paths = append(paths, fmt.Sprintf("temp:%s", options.TempDir))
		}

		if len(paths) > 0 {
			args = append(args, "--paths", strings.Join(paths, ","))
		}
	}

	return args
}

// buildQualityArgs 构建画质选择参数
// 使用 -S (Sort) 排序策略替代死板的 -f 过滤，更鲁棒
func buildQualityArgs(quality string, isYouTube bool) []string {
	if quality == "" || quality == "best" {
		// 默认最强画质：使用更灵活的格式选择策略
		// 优先尝试音视频分离，回退到单文件，最后使用任何可用格式
		// 对于 YouTube，添加额外的格式选择参数来解锁更多选项
		if isYouTube {
			// YouTube 特殊处理：使用最宽松的格式选择策略
			// 不限制容器格式，让 yt-dlp 自动选择最佳可用格式
			// 回退链：分离流 -> 合并流 -> 任意最佳格式
			return []string{
				"-f", "bestvideo+bestaudio/best",
			}
		}
		// 非 YouTube 站点：使用标准策略
		return []string{"-f", "bestvideo+bestaudio/best"}
	}

	// 用户指定了具体分辨率（如 1080p）
	height := strings.TrimSuffix(quality, "p")

	// 策略：使用 -S 限制分辨率上限，-f 保证获取合并流
	// -S res:1080 会优先选择不超过 1080p 的最清晰流
	// -f bestvideo+bestaudio/best 确保拿到分离的音视频并合并
	if isYouTube {
		// YouTube 使用更宽松的格式选择，不限制容器格式
		return []string{
			"-f", fmt.Sprintf("bestvideo[height<=%s]+bestaudio/best[height<=%s]/best", height, height),
		}
	}
	return []string{
		"-S", fmt.Sprintf("res:%s", height),
		"-f", "bestvideo+bestaudio/best",
	}
}

// buildAuthArgs 构建认证模式相关参数
func buildAuthArgs(url string, options DownloadOptions) []string {
	var args []string
	isYT := isYouTubeURL(url)

	// Cookie 文件
	if options.CookieFile != "" {
		args = append(args, "--cookies", options.CookieFile)
	}

	// User-Agent 和提取器参数
	if options.CookieFile != "" {
		// 对于非 YouTube 站点，注入 User-Agent
		if !isYT {
			userAgent := options.UserAgent
			if userAgent == "" {
				userAgent = DefaultUserAgent
			}
			args = append(args, "--user-agent", userAgent)

			// Bilibili 提取器参数
			if isBilibiliURL(url) {
				args = append(args, "--extractor-args", "bilibili:prefer_multi_flv=true")
			}
		}

		// YouTube 提取器参数：不再指定 player-client
		// 让 yt-dlp 自动选择最佳客户端，避免 Cookie 与客户端类型不匹配导致反爬虫
		// 注意：当前代码已对 YouTube 禁用 Cookie，此分支不会执行
	}

	return args
}

// buildTitleCmd 构建获取标题的命令对象
func buildTitleCmd(ctx context.Context, execPath string, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, execPath, args...)
}
