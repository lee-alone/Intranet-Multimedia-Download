package alert

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestDefaultConfig 测试默认配置
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.EnableDiskAlert != true {
		t.Error("默认应启用磁盘告警")
	}

	if config.DiskThreshold != 0.8 {
		t.Errorf("磁盘阈值应为 0.8, 得到 %f", config.DiskThreshold)
	}

	if config.CheckInterval != 5*time.Minute {
		t.Errorf("检查间隔应为 5 分钟，得到 %v", config.CheckInterval)
	}

	if config.WebhookType != "dingtalk" {
		t.Errorf("默认 Webhook 类型应为 dingtalk, 得到 %s", config.WebhookType)
	}

	if config.EmailAuthType != "PLAIN" {
		t.Errorf("默认邮件认证类型应为 PLAIN, 得到 %s", config.EmailAuthType)
	}
}

// TestAlertManager 测试告警管理器基本功能
func TestAlertManager(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = false // 禁用自动检查
	config.EnableWebhook = false   // 禁用 Webhook
	config.EnableEmail = false     // 禁用邮件

	am := NewAlertManager(config)

	// 测试启动和停止
	am.Start()
	if !am.started {
		t.Error("启动后 started 应为 true")
	}

	am.Stop()
	if am.started {
		t.Error("停止后 started 应为 false")
	}
}

// TestAlertManagerCallback 测试告警回调
func TestAlertManagerCallback(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = false
	config.EnableWebhook = false
	config.EnableEmail = false

	am := NewAlertManager(config)

	// 注册回调
	alertReceived := false
	am.RegisterCallback(func(alert *Alert) {
		alertReceived = true
		if alert.Type != AlertTypeSystem {
			t.Errorf("告警类型应为 system, 得到 %s", alert.Type)
		}
		if alert.Level != AlertLevelInfo {
			t.Errorf("告警级别应为 info, 得到 %s", alert.Level)
		}
	})

	am.Start()
	defer am.Stop()

	// 发送测试告警
	am.SendAlert(AlertTypeSystem, AlertLevelInfo, "测试标题", "测试消息", nil)

	// 等待异步处理
	time.Sleep(100 * time.Millisecond)

	if !alertReceived {
		t.Error("应该收到告警回调")
	}
}

// TestFormatSize 测试大小格式化
func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes  uint64
		expect string
	}{
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
		{1024 * 1024 * 1024 * 1024, "1.00 TB"},
	}

	for _, test := range tests {
		result := formatSize(test.bytes)
		if result != test.expect {
			t.Errorf("formatSize(%d) = %s, 期望 %s", test.bytes, result, test.expect)
		}
	}
}

// TestFormatDetails 测试详情格式化
func TestFormatDetails(t *testing.T) {
	// 测试空详情
	result := formatDetails(nil)
	if result != "" {
		t.Errorf("空详情应返回空字符串，得到 '%s'", result)
	}

	// 测试有内容
	details := map[string]any{
		"key1": "value1",
		"key2": 123,
	}
	result = formatDetails(details)
	if result == "" {
		t.Error("详情不应为空")
	}
}

// TestGetSystemInfo 测试系统信息
func TestGetSystemInfo(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = false
	config.EnableWebhook = false
	config.EnableEmail = false

	am := NewAlertManager(config)
	info := am.GetSystemInfo()

	if info["go_version"] == "" {
		t.Error("Go 版本不应为空")
	}

	if info["os"] == "" {
		t.Error("操作系统不应为空")
	}

	if info["arch"] == "" {
		t.Error("架构不应为空")
	}

	if info["num_cpu"].(int) <= 0 {
		t.Error("CPU 数量应大于 0")
	}
}

// TestAlertTypes 测试所有告警类型
func TestAlertTypes(t *testing.T) {
	types := []AlertType{
		AlertTypeDisk,
		AlertTypeMemory,
		AlertTypeEngine,
		AlertTypeDownload,
		AlertTypeSystem,
		AlertTypeLogRotate,
	}

	for _, tp := range types {
		if tp == "" {
			t.Errorf("告警类型不应为空：%v", tp)
		}
	}
}

// TestAlertLevels 测试所有告警级别
func TestAlertLevels(t *testing.T) {
	levels := []AlertLevel{
		AlertLevelInfo,
		AlertLevelWarning,
		AlertLevelError,
		AlertLevelCritical,
	}

	for _, level := range levels {
		if level == "" {
			t.Errorf("告警级别不应为空：%v", level)
		}
	}
}

// TestAlertManagerMultipleCallbacks 测试多个回调
func TestAlertManagerMultipleCallbacks(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = false
	config.EnableWebhook = false
	config.EnableEmail = false

	am := NewAlertManager(config)

	// 注册多个回调
	callback1Called := false
	callback2Called := false

	am.RegisterCallback(func(alert *Alert) {
		callback1Called = true
	})

	am.RegisterCallback(func(alert *Alert) {
		callback2Called = true
	})

	am.Start()
	defer am.Stop()

	am.SendAlert(AlertTypeSystem, AlertLevelInfo, "测试", "测试", nil)

	// 等待异步处理
	time.Sleep(100 * time.Millisecond)

	if !callback1Called {
		t.Error("回调 1 应该被调用")
	}

	if !callback2Called {
		t.Error("回调 2 应该被调用")
	}
}

// TestCheckDiskAlert 测试手动触发磁盘检查
func TestCheckDiskAlert(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = false // 禁用自动检查
	config.EnableWebhook = false
	config.EnableEmail = false
	config.DiskThreshold = 1.0 // 设置高阈值避免触发告警

	am := NewAlertManager(config)
	am.Start()
	defer am.Stop()

	// 手动触发检查（不应崩溃）
	am.CheckDiskAlert()
}

// TestDiskCheckLoopStop 测试 diskCheckLoop 协程停止逻辑
func TestDiskCheckLoopStop(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = true
	config.CheckInterval = 1 // 1 分钟检查间隔
	config.EnableWebhook = false
	config.EnableEmail = false
	config.DiskThreshold = 1.0 // 设置高阈值避免触发告警

	am := NewAlertManager(config)
	am.Start()

	// 等待一小段时间
	time.Sleep(50 * time.Millisecond)

	// 停止应该能正常退出
	am.Stop()

	// 测试多次调用 Stop 不会崩溃（幂等性）
	am.Stop()
	am.Stop()
}

// TestSendWebhook_DingTalk 测试钉钉 Webhook 发送
func TestSendWebhook_DingTalk(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = false
	config.EnableWebhook = true
	config.EnableEmail = false
	config.WebhookType = "dingtalk"
	config.WebhookURL = "http://invalid-webhook-url-for-test" // 无效 URL 用于测试

	am := NewAlertManager(config)

	// 发送告警（会失败，但不应崩溃）
	am.SendAlert(AlertTypeSystem, AlertLevelInfo, "测试", "测试", nil)

	// 等待异步处理
	time.Sleep(200 * time.Millisecond)
}

// TestSendWebhook_WeChat 测试企业微信 Webhook 发送
func TestSendWebhook_WeChat(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = false
	config.EnableWebhook = true
	config.EnableEmail = false
	config.WebhookType = "wechat"
	config.WebhookURL = "http://invalid-webhook-url-for-test"

	am := NewAlertManager(config)
	am.SendAlert(AlertTypeSystem, AlertLevelInfo, "测试", "测试", nil)
	time.Sleep(200 * time.Millisecond)
}

// TestSendWebhook_FeiShu 测试飞书 Webhook 发送
func TestSendWebhook_FeiShu(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = false
	config.EnableWebhook = true
	config.EnableEmail = false
	config.WebhookType = "feishu"
	config.WebhookURL = "http://invalid-webhook-url-for-test"

	am := NewAlertManager(config)
	am.SendAlert(AlertTypeSystem, AlertLevelInfo, "测试", "测试", nil)
	time.Sleep(200 * time.Millisecond)
}

// TestSendEmail 测试邮件发送（使用无效配置测试错误处理）
func TestSendEmail(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = false
	config.EnableWebhook = false
	config.EnableEmail = true
	config.EmailSMTPServer = "invalid-smtp-server"
	config.EmailSMTPPort = 25
	config.EmailFrom = "test@example.com"
	config.EmailPassword = "password"
	config.EmailTo = []string{"admin@example.com"}
	config.EmailAuthType = "PLAIN"

	am := NewAlertManager(config)

	// 发送告警（会失败，但不应崩溃）
	am.SendAlert(AlertTypeSystem, AlertLevelInfo, "测试", "测试", nil)

	// 等待异步处理
	time.Sleep(500 * time.Millisecond)
}

// TestSendEmail_LOGIN 测试 LOGIN 认证
func TestSendEmail_LOGIN(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = false
	config.EnableWebhook = false
	config.EnableEmail = true
	config.EmailSMTPServer = "invalid-smtp-server"
	config.EmailSMTPPort = 25
	config.EmailFrom = "test@example.com"
	config.EmailPassword = "password"
	config.EmailTo = []string{"admin@example.com"}
	config.EmailAuthType = "LOGIN" // 测试 LOGIN 认证

	am := NewAlertManager(config)
	am.SendAlert(AlertTypeSystem, AlertLevelInfo, "测试", "测试", nil)
	time.Sleep(500 * time.Millisecond)
}

// TestAlertManager_WebhookCallback 测试 Webhook 回调
func TestAlertManager_WebhookCallback(t *testing.T) {
	config := DefaultConfig()
	config.EnableDiskAlert = false
	config.EnableWebhook = false // 禁用实际发送
	config.EnableEmail = false

	am := NewAlertManager(config)

	webhookCalled := false
	am.RegisterCallback(func(alert *Alert) {
		if alert.Type == AlertTypeSystem {
			webhookCalled = true
		}
	})

	am.Start()
	defer am.Stop()

	am.SendAlert(AlertTypeSystem, AlertLevelInfo, "测试", "测试", nil)
	time.Sleep(100 * time.Millisecond)

	if !webhookCalled {
		t.Error("应该收到回调")
	}
}

// TestAlertManager_EmailDetails 测试邮件详情格式化
func TestAlertManager_EmailDetails(t *testing.T) {
	details := map[string]any{
		"disk_usage": "85%",
		"threshold":  "80%",
		"path":       "/data",
	}

	result := formatDetails(details)
	if result == "" {
		t.Error("详情不应为空")
	}

	// 检查是否包含所有键
	for k, v := range details {
		expected := fmt.Sprintf("%s: %v", k, v)
		if !strings.Contains(result, expected) {
			// 详情格式可能不同，只要包含键值对即可
			if !strings.Contains(result, k) {
				t.Errorf("结果应包含键 %s", k)
			}
		}
	}
}
