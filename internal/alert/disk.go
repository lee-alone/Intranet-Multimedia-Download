package alert

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// getDiskUsageUnix 获取 Unix 系统磁盘使用情况（使用 df 命令）
func getDiskUsageUnix(path string) (*diskUsage, error) {
	// 使用 df 命令获取磁盘使用情况
	cmd := exec.Command("df", "-P", path)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("执行 df 命令失败：%w", err)
	}

	// 解析 df 输出
	// Filesystem     1024-blocks     Used Available Capacity  Mounted on
	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("df 输出格式异常")
	}

	// 解析第二行（数据行）
	parts := regexp.MustCompile(`\s+`).Split(strings.TrimSpace(lines[1]), -1)
	if len(parts) < 5 {
		return nil, fmt.Errorf("df 输出解析失败")
	}

	// 转换为字节（df 默认使用 1KB 块）
	blockSize := uint64(1024)
	total, _ := strconv.ParseUint(parts[1], 10, 64)
	total *= blockSize
	used, _ := strconv.ParseUint(parts[2], 10, 64)
	used *= blockSize
	free, _ := strconv.ParseUint(parts[3], 10, 64)
	free *= blockSize
	usedPercent, _ := strconv.ParseFloat(strings.TrimSuffix(parts[4], "%"), 64)
	usedPercent /= 100.0

	return &diskUsage{
		Total:       total,
		Used:        used,
		Free:        free,
		UsedPercent: usedPercent,
	}, nil
}

// getDiskUsageWindows 获取 Windows 系统磁盘使用情况
func getDiskUsageWindows(path string) (*diskUsage, error) {
	// 获取驱动器字母
	drive := "C:"
	if len(path) > 0 {
		if len(path) >= 2 && path[1] == ':' {
			drive = path[:2]
		} else if runtime.GOOS == "windows" {
			// 使用 C: 作为默认驱动器
			drive = "C:"
		}
	}

	// 使用 wmic 获取磁盘使用情况
	cmd := exec.Command("wmic", "logicaldisk", "where", fmt.Sprintf("DeviceID='%s'", drive), "get", "Size,FreeSpace")
	output, err := cmd.Output()
	if err != nil {
		// 如果 wmic 失败，尝试使用 PowerShell
		return getDiskUsageWindowsPS(drive)
	}

	// 解析输出
	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("wmic 输出格式异常")
	}

	// 解析数据行
	parts := regexp.MustCompile(`\s+`).Split(strings.TrimSpace(lines[1]), -1)
	if len(parts) < 2 {
		return nil, fmt.Errorf("wmic 输出解析失败")
	}

	total, _ := strconv.ParseUint(parts[0], 10, 64)
	free, _ := strconv.ParseUint(parts[1], 10, 64)
	used := total - free
	usedPercent := float64(0)
	if total > 0 {
		usedPercent = float64(used) / float64(total)
	}

	return &diskUsage{
		Total:       total,
		Used:        used,
		Free:        free,
		UsedPercent: usedPercent,
	}, nil
}

// getDiskUsageWindowsPS 使用 PowerShell 获取 Windows 磁盘使用情况
func getDiskUsageWindowsPS(drive string) (*diskUsage, error) {
	psCmd := fmt.Sprintf(`
		$disk = Get-PSDrive -Name "%s" | Select-Object -First 1
		$total = $disk.Total
		$free = $disk.Free
		$used = $total - $free
		$percent = $used / $total
		Write-Output "$total $used $free $percent"
	`, strings.TrimSuffix(drive, ":"))

	cmd := exec.Command("powershell", "-Command", psCmd)
	output, err := cmd.Output()
	if err != nil {
		// 如果 PowerShell 也失败，返回错误
		return nil, fmt.Errorf("获取磁盘信息失败：%w", err)
	}

	parts := regexp.MustCompile(`\s+`).Split(strings.TrimSpace(string(output)), -1)
	if len(parts) < 4 {
		return nil, fmt.Errorf("PowerShell 输出解析失败")
	}

	total, _ := strconv.ParseUint(parts[0], 10, 64)
	used, _ := strconv.ParseUint(parts[1], 10, 64)
	free, _ := strconv.ParseUint(parts[2], 10, 64)
	usedPercent, _ := strconv.ParseFloat(parts[3], 64)

	return &diskUsage{
		Total:       total,
		Used:        used,
		Free:        free,
		UsedPercent: usedPercent,
	}, nil
}

// GetDiskUsage 公开函数，获取指定路径的磁盘使用情况
func GetDiskUsage(path string) (*diskUsage, error) {
	if runtime.GOOS == "windows" {
		return getDiskUsageWindows(path)
	}
	return getDiskUsageUnix(path)
}

// CheckDiskSpace 检查磁盘空间并返回是否超过阈值
func CheckDiskSpace(path string, threshold float64) (bool, *diskUsage, error) {
	usage, err := GetDiskUsage(path)
	if err != nil {
		return false, nil, err
	}

	return usage.UsedPercent >= threshold, usage, nil
}

// GetPathDrive 获取路径对应的驱动器（Windows）
func GetPathDrive(path string) string {
	if runtime.GOOS == "windows" {
		if len(path) >= 2 && path[1] == ':' {
			return path[:2]
		}
		return "C:"
	}
	return "/"
}

// GetTempDrive 获取临时文件所在驱动器
func GetTempDrive() string {
	tempDir := os.TempDir()
	return GetPathDrive(tempDir)
}
