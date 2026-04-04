package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// MockEngine 用于测试的模拟引擎
type MockEngine struct {
	name         string
	status       EngineStatus
	canHandle    bool
	version      string
	available    bool
	failNext     bool
	downloadFunc func(ctx context.Context, url string, options DownloadOptions) <-chan DownloadProgress
	mu           sync.RWMutex
}

func NewMockEngine(name string, canHandle, available bool) *MockEngine {
	return &MockEngine{
		name:      name,
		status:    EngineStatusIdle,
		canHandle: canHandle,
		available: available,
		version:   "1.0.0",
	}
}

func (m *MockEngine) Name() string {
	return m.name
}

func (m *MockEngine) Status() EngineStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *MockEngine) SetStatus(status EngineStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = status
}

func (m *MockEngine) CanHandle(url string) bool {
	return m.canHandle
}

func (m *MockEngine) Download(ctx context.Context, url string, options DownloadOptions) <-chan DownloadProgress {
	m.mu.Lock()
	m.status = EngineStatusRunning
	m.mu.Unlock()

	progressChan := make(chan DownloadProgress, 10)

	go func() {
		defer func() {
			// 安全关闭 channel
			close(progressChan)
		}()

		// 辅助函数：安全发送进度
		sendProgress := func(p DownloadProgress) bool {
			select {
			case progressChan <- p:
				return true
			default:
				// channel 已满或已关闭
				return false
			}
		}

		if m.failNext {
			sendProgress(DownloadProgress{
				Status: "error: download failed",
			})
			m.mu.Lock()
			m.status = EngineStatusError
			m.mu.Unlock()
			return
		}

		// 模拟下载进度
		for i := 0; i <= 100; i += 25 {
			select {
			case <-ctx.Done():
				return
			default:
				if !sendProgress(DownloadProgress{
					Percent: float64(i),
					Status:  fmt.Sprintf("downloading: %d%%", i),
				}) {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}

		m.mu.Lock()
		m.status = EngineStatusIdle
		m.mu.Unlock()
	}()

	return progressChan
}

func (m *MockEngine) GetVersion() (string, error) {
	return m.version, nil
}

func (m *MockEngine) IsAvailable() bool {
	return m.available
}

func (m *MockEngine) SetFailNext(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = fail
}

// TestFailoverEngine_Creation 测试故障转移引擎创建
func TestFailoverEngine_Creation(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, true)

	config := DefaultFailoverConfig()
	fe := NewFailoverEngine(primary, backup, config)

	if fe == nil {
		t.Fatal("FailoverEngine 创建失败")
	}

	if fe.Name() != "yt-dlp" {
		t.Errorf("期望主引擎名称为 yt-dlp, 得到 %s", fe.Name())
	}
}

// TestFailoverEngine_SwitchEngine 测试引擎切换
func TestFailoverEngine_SwitchEngine(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, true)

	config := FailoverConfig{
		MaxFailures:      2,
		FailureWindow:    1 * time.Minute,
		CooldownTime:     100 * time.Millisecond,
		EnableAutoSwitch: true,
		EnableAlert:      false,
	}

	fe := NewFailoverEngine(primary, backup, config)

	if fe.IsSwitched() {
		t.Error("初始状态不应切换")
	}

	// 模拟失败触发切换
	for i := 0; i < config.MaxFailures; i++ {
		fe.recordFailure("http://test.com", fmt.Errorf("test error"), "yt-dlp")
	}

	// 检查是否切换
	if !fe.IsSwitched() {
		t.Error("应该在失败次数达到阈值后切换引擎")
	}
}

// TestFailoverEngine_FailureCount 测试失败计数
func TestFailoverEngine_FailureCount(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, true)

	config := DefaultFailoverConfig()
	config.MaxFailures = 5
	config.FailureWindow = 1 * time.Minute

	fe := NewFailoverEngine(primary, backup, config)

	// 记录多次失败
	for i := 0; i < 3; i++ {
		fe.recordFailure("http://test.com", fmt.Errorf("error %d", i), "yt-dlp")
	}

	count := fe.GetFailureCount()
	if count != 3 {
		t.Errorf("期望失败计数为 3, 得到 %d", count)
	}
}

// TestFailoverEngine_ResetEngine 测试重置引擎
func TestFailoverEngine_ResetEngine(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, true)

	config := FailoverConfig{
		MaxFailures:      2,
		FailureWindow:    1 * time.Minute,
		CooldownTime:     100 * time.Millisecond,
		EnableAutoSwitch: true,
		EnableAlert:      false,
	}

	fe := NewFailoverEngine(primary, backup, config)

	// 触发切换
	for i := 0; i < config.MaxFailures; i++ {
		fe.recordFailure("http://test.com", fmt.Errorf("test error"), "yt-dlp")
	}

	if !fe.IsSwitched() {
		t.Fatal("应该已切换")
	}

	// 重置
	fe.ResetEngine()

	if fe.IsSwitched() {
		t.Error("重置后不应处于切换状态")
	}
}

// TestFailoverEngine_AlertCallback 测试告警回调
func TestFailoverEngine_AlertCallback(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, true)

	config := FailoverConfig{
		MaxFailures:      2,
		FailureWindow:    1 * time.Minute,
		CooldownTime:     100 * time.Millisecond,
		EnableAutoSwitch: true,
		EnableAlert:      true,
	}

	fe := NewFailoverEngine(primary, backup, config)

	alertReceived := false
	var alertType string

	fe.SetAlertCallback(func(atype, message string) {
		alertReceived = true
		alertType = atype
		_ = message // 使用变量避免编译错误
	})

	// 触发切换以产生告警
	for i := 0; i < config.MaxFailures; i++ {
		fe.recordFailure("http://test.com", fmt.Errorf("test error"), "yt-dlp")
	}

	if !alertReceived {
		t.Error("应该收到告警")
	}

	if alertType != "failover" {
		t.Errorf("期望告警类型为 failover, 得到 %s", alertType)
	}
}

// TestFailoverEngine_HealthStatus 测试健康状态
func TestFailoverEngine_HealthStatus(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, true)

	config := DefaultFailoverConfig()
	fe := NewFailoverEngine(primary, backup, config)

	health := fe.GetEngineHealth("yt-dlp")
	if health == nil {
		t.Fatal("健康状态不应为空")
	}

	if health.Name != "yt-dlp" {
		t.Errorf("期望引擎名称为 yt-dlp, 得到 %s", health.Name)
	}
}

// TestFailoverEngine_Download 测试下载流程
func TestFailoverEngine_Download(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, true)

	config := DefaultFailoverConfig()
	config.EnableAutoSwitch = false // 禁用自动切换以简化测试
	fe := NewFailoverEngine(primary, backup, config)

	ctx := context.Background()
	options := DownloadOptions{
		OutputDir: "/tmp",
	}

	progressChan := fe.Download(ctx, "http://test.com/video", options)

	receivedProgress := false
	for p := range progressChan {
		receivedProgress = true
		if p.Percent < 0 || p.Percent > 100 {
			t.Errorf("进度百分比应在 0-100 之间，得到 %f", p.Percent)
		}
	}

	if !receivedProgress {
		t.Error("应该收到进度更新")
	}
}

// TestFailoverEngine_VersionCheck 测试版本检查
func TestFailoverEngine_VersionCheck(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, true)

	config := DefaultFailoverConfig()
	fe := NewFailoverEngine(primary, backup, config)

	version, err := fe.CheckVersion("yt-dlp")
	if err != nil {
		t.Fatalf("检查版本失败：%v", err)
	}

	if version != "1.0.0" {
		t.Errorf("期望版本为 1.0.0, 得到 %s", version)
	}
}

// TestFailoverEngine_CanHandle 测试 URL 处理能力
func TestFailoverEngine_CanHandle(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", false, true)

	config := DefaultFailoverConfig()
	fe := NewFailoverEngine(primary, backup, config)

	if !fe.CanHandle("http://test.com") {
		t.Error("应该能处理 URL")
	}
}

// TestFailoverEngine_IsAvailable 测试可用性检查
func TestFailoverEngine_IsAvailable(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, false) // 不可用

	config := DefaultFailoverConfig()
	fe := NewFailoverEngine(primary, backup, config)

	// 当前引擎可用
	if !fe.IsAvailable() {
		t.Error("应该可用")
	}
}

// TestFailoverEngine_GetFailures 测试获取失败记录
func TestFailoverEngine_GetFailures(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, true)

	config := DefaultFailoverConfig()
	fe := NewFailoverEngine(primary, backup, config)

	// 记录失败
	fe.recordFailure("http://test1.com", fmt.Errorf("error1"), "yt-dlp")
	fe.recordFailure("http://test2.com", fmt.Errorf("error2"), "yt-dlp")

	failures := fe.GetFailures()
	if len(failures) != 2 {
		t.Errorf("期望 2 条失败记录，得到 %d", len(failures))
	}
}

// TestFailoverEngine_ConcurrentAccess 测试并发访问
func TestFailoverEngine_ConcurrentAccess(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, true)

	config := DefaultFailoverConfig()
	fe := NewFailoverEngine(primary, backup, config)

	var wg sync.WaitGroup

	// 并发记录失败
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			fe.recordFailure("http://test.com", fmt.Errorf("error %d", id), "yt-dlp")
		}(i)
	}

	wg.Wait()

	failures := fe.GetFailures()
	if len(failures) != 10 {
		t.Errorf("期望 10 条失败记录，得到 %d", len(failures))
	}
}

// TestEngineHealth_StatusString 测试引擎状态字符串转换
func TestEngineHealth_StatusString(t *testing.T) {
	tests := []struct {
		status   EngineStatus
		expected string
	}{
		{EngineStatusIdle, "idle"},
		{EngineStatusRunning, "running"},
		{EngineStatusError, "error"},
	}

	for _, test := range tests {
		result := test.status.String()
		if result != test.expected {
			t.Errorf("期望 %s, 得到 %s", test.expected, result)
		}
	}
}

// TestFailoverEngine_ImmediateFailover 测试即时故障转移功能
func TestFailoverEngine_ImmediateFailover(t *testing.T) {
	// 创建主引擎(会失败)
	primary := NewMockEngine("yt-dlp", true, true)
	primary.SetFailNext(true)

	// 创建备用引擎(会成功)
	backup := NewMockEngine("lux", true, true)
	backup.SetFailNext(false)

	config := FailoverConfig{
		MaxFailures:          10, // 设置较大的阈值,避免触发全局切换
		FailureWindow:        1 * time.Minute,
		CooldownTime:         100 * time.Millisecond,
		EnableAutoSwitch:     false, // 禁用全局切换
		EnableAlert:          false,
		EnableImmediateRetry: true, // 启用即时重试
	}

	fe := NewFailoverEngine(primary, backup, config)

	ctx := context.Background()
	options := DownloadOptions{
		OutputDir: "/tmp",
	}

	progressChan := fe.Download(ctx, "http://test.com/video", options)

	receivedProgress := false
	lastStatus := ""
	for p := range progressChan {
		receivedProgress = true
		lastStatus = p.Status
		if p.Percent < 0 || p.Percent > 100 {
			t.Errorf("进度百分比应在 0-100 之间，得到 %f", p.Percent)
		}
	}

	if !receivedProgress {
		t.Fatal("应该收到进度更新")
	}

	// 验证最终没有错误(因为备用引擎成功了)
	if strings.Contains(strings.ToLower(lastStatus), "error") {
		t.Errorf("不应该有错误状态,得到: %s", lastStatus)
	}
}

// TestFailoverEngine_ImmediateFailover_Disabled 测试禁用即时重试的情况
func TestFailoverEngine_ImmediateFailover_Disabled(t *testing.T) {
	// 创建主引擎(会失败)
	primary := NewMockEngine("yt-dlp", true, true)
	primary.SetFailNext(true)

	// 创建备用引擎(会成功)
	backup := NewMockEngine("lux", true, true)

	config := FailoverConfig{
		MaxFailures:          10,
		FailureWindow:        1 * time.Minute,
		CooldownTime:         100 * time.Millisecond,
		EnableAutoSwitch:     false,
		EnableAlert:          false,
		EnableImmediateRetry: false, // 禁用即时重试
	}

	fe := NewFailoverEngine(primary, backup, config)

	ctx := context.Background()
	options := DownloadOptions{
		OutputDir: "/tmp",
	}

	progressChan := fe.Download(ctx, "http://test.com/video", options)

	hasError := false
	for p := range progressChan {
		if strings.Contains(strings.ToLower(p.Status), "error") {
			hasError = true
		}
	}

	// 验证有错误(因为没有即时重试)
	if !hasError {
		t.Error("禁用即时重试时应该收到错误")
	}
}

// TestFailoverEngine_ImmediateFailover_BackupUnavailable 测试备用引擎不可用的情况
func TestFailoverEngine_ImmediateFailover_BackupUnavailable(t *testing.T) {
	// 创建主引擎(会失败)
	primary := NewMockEngine("yt-dlp", true, true)
	primary.SetFailNext(true)

	// 创建备用引擎(不可用)
	backup := NewMockEngine("lux", true, false)

	config := FailoverConfig{
		MaxFailures:          10,
		FailureWindow:        1 * time.Minute,
		CooldownTime:         100 * time.Millisecond,
		EnableAutoSwitch:     false,
		EnableAlert:          false,
		EnableImmediateRetry: true,
	}

	fe := NewFailoverEngine(primary, backup, config)

	ctx := context.Background()
	options := DownloadOptions{
		OutputDir: "/tmp",
	}

	progressChan := fe.Download(ctx, "http://test.com/video", options)

	hasError := false
	for p := range progressChan {
		if strings.Contains(strings.ToLower(p.Status), "error") {
			hasError = true
		}
	}

	// 验证有错误(因为备用引擎不可用)
	if !hasError {
		t.Error("备用引擎不可用时应该收到错误")
	}
}

// TestFailoverEngine_ImmediateFailover_BackupCannotHandle 测试备用引擎无法处理 URL 的情况
func TestFailoverEngine_ImmediateFailover_BackupCannotHandle(t *testing.T) {
	// 创建主引擎(会失败,但可以处理 URL)
	primary := NewMockEngine("yt-dlp", true, true)
	primary.SetFailNext(true)

	// 创建备用引擎(可以成功,但不能处理该 URL)
	backup := NewMockEngine("lux", false, true)

	config := FailoverConfig{
		MaxFailures:          10,
		FailureWindow:        1 * time.Minute,
		CooldownTime:         100 * time.Millisecond,
		EnableAutoSwitch:     false,
		EnableAlert:          false,
		EnableImmediateRetry: true,
	}

	fe := NewFailoverEngine(primary, backup, config)

	ctx := context.Background()
	options := DownloadOptions{
		OutputDir: "/tmp",
	}

	progressChan := fe.Download(ctx, "http://test.com/video", options)

	hasError := false
	for p := range progressChan {
		if strings.Contains(strings.ToLower(p.Status), "error") {
			hasError = true
		}
	}

	// 验证有错误(因为备用引擎无法处理该 URL)
	if !hasError {
		t.Error("备用引擎无法处理 URL 时应该收到错误")
	}
}

// TestFailoverEngine_GetPreferredEngineForURL 测试根据 URL 获取推荐引擎
func TestFailoverEngine_GetPreferredEngineForURL(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	backup := NewMockEngine("lux", true, true)

	config := DefaultFailoverConfig()
	fe := NewFailoverEngine(primary, backup, config)

	tests := []struct {
		name            string
		url             string
		expectedEngine  string
	}{
		{
			name:           "Bilibili URL 应该推荐 lux",
			url:            "https://www.bilibili.com/video/BV1xx411c7BF",
			expectedEngine: "lux",
		},
		{
			name:           "爱奇艺 URL 应该推荐 lux",
			url:            "https://www.iqiyi.com/v_123456.html",
			expectedEngine: "lux",
		},
		{
			name:           "YouTube URL 应该推荐当前引擎",
			url:            "https://www.youtube.com/watch?v=xxxxx",
			expectedEngine: "yt-dlp",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := fe.GetPreferredEngineForURL(test.url)
			if engine == nil {
				t.Errorf("URL %s 应该返回推荐引擎", test.url)
				return
			}
			if engine.Name() != test.expectedEngine {
				t.Errorf("URL %s 期望引擎为 %s, 得到 %s", test.url, test.expectedEngine, engine.Name())
			}
		})
	}
}

// TestFailoverEngine_ImmediateFailover_AlertCallback 测试即时故障转移的告警回调
func TestFailoverEngine_ImmediateFailover_AlertCallback(t *testing.T) {
	primary := NewMockEngine("yt-dlp", true, true)
	primary.SetFailNext(true)

	backup := NewMockEngine("lux", true, true)

	config := FailoverConfig{
		MaxFailures:          10,
		FailureWindow:        1 * time.Minute,
		CooldownTime:         100 * time.Millisecond,
		EnableAutoSwitch:     false,
		EnableAlert:          true,
		EnableImmediateRetry: true,
	}

	fe := NewFailoverEngine(primary, backup, config)

	alertReceived := false
	var alertType string
	var alertMessage string

	fe.SetAlertCallback(func(atype, message string) {
		alertReceived = true
		alertType = atype
		alertMessage = message
	})

	ctx := context.Background()
	options := DownloadOptions{
		OutputDir: "/tmp",
	}

	progressChan := fe.Download(ctx, "http://test.com/video", options)

	// 消费所有进度
	for range progressChan {
	}

	// 验证收到了即时重试告警
	if !alertReceived {
		t.Fatal("应该收到告警")
	}

	if alertType != "immediate_retry" {
		t.Errorf("期望告警类型为 immediate_retry, 得到 %s", alertType)
	}

	if !strings.Contains(alertMessage, "立即尝试备用引擎") {
		t.Errorf("告警消息应包含'立即尝试备用引擎', 得到: %s", alertMessage)
	}
}

// TestFailoverEngine_SmartEngineSelection_Bilibili 测试 Bilibili URL 智能选择 lux
func TestFailoverEngine_SmartEngineSelection_Bilibili(t *testing.T) {
	// 主引擎 yt-dlp(会失败)
	primary := NewMockEngine("yt-dlp", true, true)
	primary.SetFailNext(true)

	// 备用引擎 lux(会成功)
	backup := NewMockEngine("lux", true, true)
	backup.SetFailNext(false)

	config := FailoverConfig{
		MaxFailures:          10,
		FailureWindow:        1 * time.Minute,
		CooldownTime:         100 * time.Millisecond,
		EnableAutoSwitch:     false,
		EnableAlert:          false,
		EnableImmediateRetry: true,
	}

	fe := NewFailoverEngine(primary, backup, config)

	ctx := context.Background()
	options := DownloadOptions{
		OutputDir: "/tmp",
	}

	// 测试 Bilibili URL
	bilibiliURL := "https://www.bilibili.com/video/BV1xx411c7BF"
	progressChan := fe.Download(ctx, bilibiliURL, options)

	hasError := false
	receivedProgress := false
	for p := range progressChan {
		receivedProgress = true
		if strings.Contains(strings.ToLower(p.Status), "error") {
			hasError = true
		}
	}

	if !receivedProgress {
		t.Fatal("应该收到进度更新")
	}

	// Bilibili URL 应该优先使用 lux,lux 成功了所以不应该有错误
	if hasError {
		t.Error("Bilibili URL 应该通过 lux 引擎成功下载,不应有错误")
	}
}

// TestFailoverEngine_SmartEngineSelection_YouTube 测试 YouTube URL 使用默认引擎
func TestFailoverEngine_SmartEngineSelection_YouTube(t *testing.T) {
	// 主引擎 yt-dlp(会成功)
	primary := NewMockEngine("yt-dlp", true, true)
	primary.SetFailNext(false)

	// 备用引擎 lux(会失败)
	backup := NewMockEngine("lux", true, true)
	backup.SetFailNext(true)

	config := FailoverConfig{
		MaxFailures:          10,
		FailureWindow:        1 * time.Minute,
		CooldownTime:         100 * time.Millisecond,
		EnableAutoSwitch:     false,
		EnableAlert:          false,
		EnableImmediateRetry: true,
	}

	fe := NewFailoverEngine(primary, backup, config)

	ctx := context.Background()
	options := DownloadOptions{
		OutputDir: "/tmp",
	}

	// 测试 YouTube URL(应该使用默认的 yt-dlp)
	youtubeURL := "https://www.youtube.com/watch?v=xxxxx"
	progressChan := fe.Download(ctx, youtubeURL, options)

	hasError := false
	receivedProgress := false
	for p := range progressChan {
		receivedProgress = true
		if strings.Contains(strings.ToLower(p.Status), "error") {
			hasError = true
		}
	}

	if !receivedProgress {
		t.Fatal("应该收到进度更新")
	}

	// YouTube URL 应该使用 yt-dlp(当前引擎),yt-dlp 成功了所以不应该有错误
	if hasError {
		t.Error("YouTube URL 应该通过 yt-dlp 引擎成功下载,不应有错误")
	}
}
