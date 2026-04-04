package engine

import (
	"regexp"
	"strconv"
)

// yt-dlp 进度正则表达式（预编译，提升性能）
var (
	// 匹配进度百分比：支持 "[download] 50.0%" 和 "50.0%" 两种格式
	percentRe = regexp.MustCompile(`(?:\[download\]\s*)?(\d+\.?\d*)%`)

	// 匹配已下载大小：如 "of 10.00MiB"
	sizeRe = regexp.MustCompile(`of\s+([\d.]+)([KMGT]?B)`)

	// 匹配下载速度：如 "at 1.50MiB/s"
	speedRe = regexp.MustCompile(`at\s+([\d.]+)([KMGT]?B/s)`)

	// 匹配预计剩余时间：如 "ETA 0:00:10"
	etaRe = regexp.MustCompile(`ETA\s+(\d+):(\d+):(\d+)`)
)

// parseProgress 解析 yt-dlp 进度输出
// 将 yt-dlp 打印的字符串转换为结构化的 DownloadProgress 对象
func parseProgress(line string) (*DownloadProgress, bool) {
	// yt-dlp 进度格式示例：
	// [download] 50.0% of 10.00MiB in 00:10
	// [download] 100% of 10.00MiB in 00:10
	// [download] 50.0% of 10.00MiB at 1.50MiB/s ETA 0:00:10

	// 匹配进度百分比
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

	// 匹配下载速度
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

	// 匹配 ETA（预计剩余时间）
	if matches := etaRe.FindStringSubmatch(line); len(matches) >= 4 {
		hours, _ := strconv.Atoi(matches[1])
		mins, _ := strconv.Atoi(matches[2])
		secs, _ := strconv.Atoi(matches[3])
		progress.ETA = hours*3600 + mins*60 + secs
	}

	return progress, true
}
