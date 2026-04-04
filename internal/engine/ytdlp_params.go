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

	if useAuth && options.CookieFile != "" {
		// 认证模式：携带 Cookie
		args = append(args, "--cookies", options.CookieFile)
		// 关键点：对于 YouTube，只传 Cookie，不传 UA（避免 UA 不匹配触发风控）
		if !isYT {
			userAgent := options.UserAgent
			if userAgent == "" {
				userAgent = DefaultUserAgent
			}
			args = append(args, "--user-agent", userAgent)
		}
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
		"--merge-output-format", "mp4",
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
	if useAuth {
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
		// 默认最强画质：音视频分离后合并
		return []string{"-f", "bestvideo+bestaudio/best"}
	}

	// 用户指定了具体分辨率（如 1080p）
	height := strings.TrimSuffix(quality, "p")

	// 策略：使用 -S 限制分辨率上限，-f 保证获取合并流
	// -S res:1080 会优先选择不超过 1080p 的最清晰流
	// -f bestvideo+bestaudio/best 确保拿到分离的音视频并合并
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

	// User-Agent 和提取器参数：仅当真正上传了 Cookies 且非 YouTube 时才注入
	if options.CookieFile != "" && !isYT {
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

	return args
}

// buildTitleCmd 构建获取标题的命令对象
func buildTitleCmd(ctx context.Context, execPath string, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, execPath, args...)
}
