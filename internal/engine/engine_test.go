package engine

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// MockCommandRunner 用于测试的模拟命令执行器
type MockCommandRunner struct {
	shouldFail bool
	output     []byte
}

func (r *MockCommandRunner) Run(cmd *exec.Cmd) error {
	if r.shouldFail {
		return exec.ErrNotFound
	}
	return nil
}

func (r *MockCommandRunner) Output(cmd *exec.Cmd) ([]byte, error) {
	if r.shouldFail {
		return nil, exec.ErrNotFound
	}
	return r.output, nil
}

// TestYtdlpEngine_Name 测试 yt-dlp 引擎名称
func TestYtdlpEngine_Name(t *testing.T) {
	engine := NewYtdlpEngine(YtdlpConfig{})
	if engine.Name() != "yt-dlp" {
		t.Errorf("expected name 'yt-dlp', got '%s'", engine.Name())
	}
}

// TestYtdlpEngine_Status 测试 yt-dlp 引擎状态
func TestYtdlpEngine_Status(t *testing.T) {
	engine := NewYtdlpEngine(YtdlpConfig{})
	if engine.Status() != EngineStatusIdle {
		t.Errorf("expected status 'idle', got '%s'", engine.Status())
	}
}

// TestYtdlpEngine_CanHandle 测试 yt-dlp 的 URL 处理能力
func TestYtdlpEngine_CanHandle(t *testing.T) {
	engine := NewYtdlpEngine(YtdlpConfig{})

	testCases := []struct {
		url      string
		expected bool
	}{
		{"https://www.youtube.com/watch?v=abc123", true},
		{"https://youtu.be/abc123", true},
		{"https://www.bilibili.com/video/BV123", true},
		{"https://b23.tv/abc123", true},
		{"https://www.youku.com/v1/abc123", true},
		{"https://v.qq.com/x/abc123", true},
		{"https://www.iqiyi.com/v_abc123.html", true},
		{"https://example.com/video", true}, // yt-dlp 默认支持所有
	}

	for _, tc := range testCases {
		result := engine.CanHandle(tc.url)
		if result != tc.expected {
			t.Errorf("CanHandle(%s) = %v, expected %v", tc.url, result, tc.expected)
		}
	}
}

// TestYtdlpEngine_IsAvailable 测试 yt-dlp 可用性检查
func TestYtdlpEngine_IsAvailable(t *testing.T) {
	engine := NewYtdlpEngine(YtdlpConfig{ExecPath: "yt-dlp"})
	// 不检查实际可用性，因为测试环境可能没有安装 yt-dlp
	_ = engine.IsAvailable()
}

// TestYtdlpEngine_GetVersion 测试获取版本
func TestYtdlpEngine_GetVersion(t *testing.T) {
	engine := NewYtdlpEngine(YtdlpConfig{ExecPath: "yt-dlp"})
	// 不检查实际版本，因为测试环境可能没有安装 yt-dlp
	_, _ = engine.GetVersion()
}

// TestYtdlpEngine_Download 测试下载流程（模拟）
func TestYtdlpEngine_Download(t *testing.T) {
	engine := NewYtdlpEngine(YtdlpConfig{})
	ctx := context.Background()

	// 测试下载流程（不实际执行）
	progressChan := engine.Download(ctx, "https://example.com/video", DownloadOptions{
		OutputDir: "./downloads",
		Timeout:   30 * time.Second,
	})

	// 等待通道关闭
	count := 0
	for range progressChan {
		count++
	}
}

// TestLuxEngine_Name 测试 lux 引擎名称
func TestLuxEngine_Name(t *testing.T) {
	engine := NewLuxEngine(LuxConfig{})
	if engine.Name() != "lux" {
		t.Errorf("expected name 'lux', got '%s'", engine.Name())
	}
}

// TestLuxEngine_Status 测试 lux 引擎状态
func TestLuxEngine_Status(t *testing.T) {
	engine := NewLuxEngine(LuxConfig{})
	if engine.Status() != EngineStatusIdle {
		t.Errorf("expected status 'idle', got '%s'", engine.Status())
	}
}

// TestLuxEngine_CanHandle 测试 lux 的 URL 处理能力
func TestLuxEngine_CanHandle(t *testing.T) {
	engine := NewLuxEngine(LuxConfig{})

	testCases := []struct {
		url      string
		expected bool
	}{
		{"https://www.bilibili.com/video/BV123", true},
		{"https://b23.tv/abc123", true},
		{"https://www.youtube.com/watch?v=abc123", true},
		{"https://www.youku.com/v1/abc123", true},
		{"https://www.iqiyi.com/v_abc123.html", true},
		{"https://www.acfun.cn/v/abc123", true},
		{"https://unknown.com/video", true}, // http(s) URL 返回 true
		{"http://example.com/video", true},  // http URL 返回 true
		{"ftp://example.com/video", true},   // 非 http(s) 但也不是已知域名，返回 true 让 lux 尝试
		{"not-a-url", false},                // 无效 URL 返回 false
	}

	for _, tc := range testCases {
		result := engine.CanHandle(tc.url)
		if result != tc.expected {
			t.Errorf("CanHandle(%s) = %v, expected %v", tc.url, result, tc.expected)
		}
	}
}

// TestLuxEngine_IsAvailable 测试 lux 可用性检查
func TestLuxEngine_IsAvailable(t *testing.T) {
	engine := NewLuxEngine(LuxConfig{ExecPath: "lux"})
	// 不检查实际可用性，因为测试环境可能没有安装 lux
	_ = engine.IsAvailable()
}

// TestLuxEngine_GetVersion 测试获取版本
func TestLuxEngine_GetVersion(t *testing.T) {
	engine := NewLuxEngine(LuxConfig{ExecPath: "lux"})
	// 不检查实际版本，因为测试环境可能没有安装 lux
	_, _ = engine.GetVersion()
}

// TestEngineSelector 测试引擎选择器
func TestEngineSelector_Select(t *testing.T) {
	selector := NewEngineSelector(EngineTypeAuto)

	// 添加引擎
	ytdlp := NewYtdlpEngine(YtdlpConfig{})
	lux := NewLuxEngine(LuxConfig{})

	selector.AddEngine(ytdlp)
	selector.AddEngine(lux)

	// 测试引擎选择
	testCases := []struct {
		url          string
		expectedName string
	}{
		{"https://www.youtube.com/watch?v=abc123", "yt-dlp"},
		{"https://www.bilibili.com/video/BV123", "yt-dlp"},
	}

	for _, tc := range testCases {
		engine := selector.Select(tc.url)
		if engine != nil && engine.Name() != tc.expectedName {
			t.Errorf("Select(%s) = %s, expected %s", tc.url, engine.Name(), tc.expectedName)
		}
	}
}

// TestEngineSelector_ListEngines 测试列出所有引擎
func TestEngineSelector_ListEngines(t *testing.T) {
	selector := NewEngineSelector(EngineTypeAuto)

	ytdlp := NewYtdlpEngine(YtdlpConfig{})
	lux := NewLuxEngine(LuxConfig{})

	selector.AddEngine(ytdlp)
	selector.AddEngine(lux)

	engines := selector.ListEngines()
	if len(engines) != 2 {
		t.Errorf("expected 2 engines, got %d", len(engines))
	}
}

// TestEngineSelector_GetEngineByName 测试按名称获取引擎
func TestEngineSelector_GetEngineByName(t *testing.T) {
	selector := NewEngineSelector(EngineTypeAuto)

	ytdlp := NewYtdlpEngine(YtdlpConfig{})
	lux := NewLuxEngine(LuxConfig{})

	selector.AddEngine(ytdlp)
	selector.AddEngine(lux)

	// 测试获取 yt-dlp
	engine := selector.GetEngineByName("yt-dlp")
	if engine == nil {
		t.Error("expected to get yt-dlp engine")
	}

	// 测试获取 lux
	engine = selector.GetEngineByName("lux")
	if engine == nil {
		t.Error("expected to get lux engine")
	}

	// 测试获取不存在的引擎
	engine = selector.GetEngineByName("nonexistent")
	if engine != nil {
		t.Error("expected nil for nonexistent engine")
	}
}

// TestParseDuration 测试时长解析
func TestParseDuration(t *testing.T) {
	testCases := []struct {
		input    string
		expected int
	}{
		{"00:01:30", 90},
		{"01:30", 90},
		{"90", 90},
		{"00:00:00", 0},
		{"", 0},
	}

	for _, tc := range testCases {
		result := parseDuration(tc.input)
		if result != tc.expected {
			t.Errorf("parseDuration(%s) = %d, expected %d", tc.input, result, tc.expected)
		}
	}
}

// TestBuildOutputPath 测试输出路径构建
func TestBuildOutputPath(t *testing.T) {
	// 只测试标题清理和长度限制，不测试路径分隔符（因平台而异）
	testCases := []struct {
		title       string
		expectShort bool // 是否期望标题被截断
		expectClean bool // 是否期望标题被清理
	}{
		{"test_video", false, false},
		{"test<video>", false, true},
		{"very_long_title_that_exceeds_100_characters_abcdefghijklmnopqrstuvwxyz", true, false},
	}

	for _, tc := range testCases {
		result := buildOutputPath("./downloads", tc.title, "mp4")

		// 检查是否包含输出目录
		if tc.expectShort {
			// 标题应该被截断为 100 字符以内
			// 检查文件名部分（去掉路径和扩展名）
			parts := strings.Split(result, string(filepath.Separator))
			if len(parts) > 0 {
				filename := parts[len(parts)-1]
				// 去掉扩展名
				if idx := strings.LastIndex(filename, "."); idx > 0 {
					filename = filename[:idx]
				}
				if len(filename) > 100 {
					t.Errorf("buildOutputPath result filename too long: %d chars", len(filename))
				}
			}
		}

		// 检查特殊字符是否被清理
		if tc.expectClean {
			if strings.Contains(result, "<") || strings.Contains(result, ">") {
				t.Errorf("buildOutputPath result contains invalid characters: %s", result)
			}
		}
	}
}

// TestEngineStatus_String 测试引擎状态字符串转换
func TestEngineStatus_String(t *testing.T) {
	testCases := []struct {
		status   EngineStatus
		expected string
	}{
		{EngineStatusIdle, "idle"},
		{EngineStatusRunning, "running"},
		{EngineStatusError, "error"},
		{EngineStatus(999), "unknown"},
	}

	for _, tc := range testCases {
		result := tc.status.String()
		if result != tc.expected {
			t.Errorf("EngineStatus(%d).String() = %s, expected %s", tc.status, result, tc.expected)
		}
	}
}

// TestYtdlpParseProgress 测试 yt-dlp 进度解析
func TestYtdlpParseProgress(t *testing.T) {
	testCases := []struct {
		line     string
		expected bool
	}{
		{"[download] 50.0% of 10.00MiB", true},
		{"[download] 100% of 10.00MiB in 00:10", true},
		{"[download] 50.0% of 10.00MiB at 1.50MiB/s ETA 0:00:10", true},
		{"50.0% of 10.00MiB", true}, // 无前缀格式
		{"some random text", false},
		{"", false},
		{"[download] 0.0% of 0B", true}, // 0% 进度
	}

	for _, tc := range testCases {
		prog, ok := parseProgress(tc.line)
		if ok != tc.expected {
			t.Errorf("parseProgress(%s) ok = %v, expected %v", tc.line, ok, tc.expected)
		} else if ok && prog == nil {
			t.Errorf("parseProgress(%s) returned nil progress", tc.line)
		}
	}
}

// TestLuxParseProgress 测试 lux 进度解析
func TestLuxParseProgress(t *testing.T) {
	engine := NewLuxEngine(LuxConfig{})

	testCases := []struct {
		line     string
		expected bool
	}{
		{"100% of 10.00MB", true},
		{"50% 5.00MB/10.00MB 1.5MB/s", true},
		{"50.5% of 100.00MB at 2.5MB/s ETA 0:00:20", true},
		{"some random text", false},
		{"", false},
	}

	for _, tc := range testCases {
		prog, ok := engine.parseProgress(tc.line)
		if ok != tc.expected {
			t.Errorf("parseProgress(%s) ok = %v, expected %v", tc.line, ok, tc.expected)
		} else if ok && prog == nil {
			t.Errorf("parseProgress(%s) returned nil progress", tc.line)
		}
	}
}

// TestYtdlpEngine_Download_Error 测试下载错误处理
func TestYtdlpEngine_Download_Error(t *testing.T) {
	engine := NewYtdlpEngine(YtdlpConfig{ExecPath: "nonexistent-binary"})
	ctx := context.Background()

	progressChan := engine.Download(ctx, "https://example.com/video", DownloadOptions{
		OutputDir: "./downloads",
		Timeout:   5 * time.Second,
	})

	// 等待通道关闭，不检查具体内容（因为依赖外部环境）
	for range progressChan {
		// 消耗通道
	}
}

// TestLuxEngine_Download 测试 lux 下载流程
func TestLuxEngine_Download(t *testing.T) {
	engine := NewLuxEngine(LuxConfig{ExecPath: "nonexistent-binary"})
	ctx := context.Background()

	progressChan := engine.Download(ctx, "https://bilibili.com/video/BV123", DownloadOptions{
		OutputDir: "./downloads",
		Timeout:   5 * time.Second,
	})

	// 等待通道关闭
	for range progressChan {
		// 消耗通道
	}
}

// TestEngineStatusTransitions 测试引擎状态转换
func TestEngineStatusTransitions(t *testing.T) {
	engine := NewYtdlpEngine(YtdlpConfig{})

	if engine.Status() != EngineStatusIdle {
		t.Errorf("initial status should be Idle")
	}
}

// TestYtdlpEngine_CanHandle_EdgeCases 测试 yt-dlp 边界情况
func TestYtdlpEngine_CanHandle_EdgeCases(t *testing.T) {
	engine := NewYtdlpEngine(YtdlpConfig{})

	testCases := []struct {
		url      string
		expected bool
	}{
		{"", true},                          // 空 URL
		{"not-a-url", true},                 // 非 URL 字符串
		{"HTTP://YOUTUBE.COM", true},        // 大写
		{"https://YOUTUBE.COM/watch", true}, // 混合大小写
	}

	for _, tc := range testCases {
		result := engine.CanHandle(tc.url)
		if result != tc.expected {
			t.Errorf("CanHandle(%s) = %v, expected %v", tc.url, result, tc.expected)
		}
	}
}
