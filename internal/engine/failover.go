// Package engine 提供视频下载引擎的统一接口和实现
package engine

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// FailoverConfig 故障转移配置
type FailoverConfig struct {
	MaxFailures      int           // 最大失败次数阈值
	FailureWindow    time.Duration // 失败时间窗口
	CooldownTime     time.Duration // 冷却时间
	EnableAutoSwitch bool          // 是否启用自动切换
	EnableAlert      bool          // 是否启用告警
}

// DefaultFailoverConfig 默认故障转移配置
func DefaultFailoverConfig() FailoverConfig {
	return FailoverConfig{
		MaxFailures:      3,               // 连续失败 3 次触发切换
		FailureWindow:    5 * time.Minute, // 5 分钟内的失败计数
		CooldownTime:     1 * time.Minute, // 冷却 1 分钟
		EnableAutoSwitch: true,            // 默认启用自动切换
		EnableAlert:      true,            // 默认启用告警
	}
}

// FailureRecord 失败记录
type FailureRecord struct {
	Timestamp time.Time
	URL       string
	Error     error
	Engine    string
}

// EngineHealth 引擎健康状态
type EngineHealth struct {
	Name           string
	Status         EngineStatus
	FailureCount   int
	LastFailure    time.Time
	LastSuccess    time.Time
	IsHealthy      bool
	CooldownUntil  time.Time
	Version        string
	VersionChecked time.Time
}

// FailoverEngine 支持故障转移的下载引擎包装器
type FailoverEngine struct {
	mu             sync.RWMutex
	primary        Engine
	backup         Engine
	config         FailoverConfig
	failures       []FailureRecord
	currentEngine  Engine
	isSwitched     bool
	alertCallback  func(alertType string, message string)
	versionCache   map[string]*versionInfo
	onHealthChange func(engineName string, health *EngineHealth)
}

// versionInfo 版本信息缓存
type versionInfo struct {
	version string
	checked time.Time
}

// NewFailoverEngine 创建故障转移引擎
func NewFailoverEngine(primary, backup Engine, config FailoverConfig) *FailoverEngine {
	if config.MaxFailures <= 0 {
		config = DefaultFailoverConfig()
	}

	fe := &FailoverEngine{
		primary:       primary,
		backup:        backup,
		config:        config,
		currentEngine: primary,
		isSwitched:    false,
		versionCache:  make(map[string]*versionInfo),
	}

	return fe
}

// SetAlertCallback 设置告警回调
func (fe *FailoverEngine) SetAlertCallback(callback func(alertType string, message string)) {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	fe.alertCallback = callback
}

// SetHealthChangeCallback 设置健康状态变化回调
func (fe *FailoverEngine) SetHealthChangeCallback(callback func(engineName string, health *EngineHealth)) {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	fe.onHealthChange = callback
}

// CurrentEngine 返回当前使用的引擎
func (fe *FailoverEngine) CurrentEngine() Engine {
	fe.mu.RLock()
	defer fe.mu.RUnlock()
	return fe.currentEngine
}

// IsSwitched 是否已切换到备用引擎
func (fe *FailoverEngine) IsSwitched() bool {
	fe.mu.RLock()
	defer fe.mu.RUnlock()
	return fe.isSwitched
}

// recordFailure 记录失败
func (fe *FailoverEngine) recordFailure(url string, err error, engineName string) {
	fe.mu.Lock()
	defer fe.mu.Unlock()

	now := time.Now()
	fe.failures = append(fe.failures, FailureRecord{
		Timestamp: now,
		URL:       url,
		Error:     err,
		Engine:    engineName,
	})

	// 清理过期记录
	windowStart := now.Add(-fe.config.FailureWindow)
	validFailures := make([]FailureRecord, 0)
	for _, f := range fe.failures {
		if f.Timestamp.After(windowStart) {
			validFailures = append(validFailures, f)
		}
	}
	fe.failures = validFailures

	// 触发健康状态变化通知
	if fe.onHealthChange != nil {
		health := fe.getEngineHealthLocked(engineName)
		fe.onHealthChange(engineName, health)
	}

	// 检查是否需要切换引擎（使用内部版本，避免死锁）
	if fe.config.EnableAutoSwitch && len(fe.failures) >= fe.config.MaxFailures {
		fe.switchEngineLocked()
	}
}

// recordSuccess 记录成功
func (fe *FailoverEngine) recordSuccess(engineName string) {
	fe.mu.Lock()
	defer fe.mu.Unlock()

	now := time.Now()

	// 更新引擎健康状态
	if fe.primary != nil && fe.primary.Name() == engineName {
		// 清除主引擎的冷却状态
		// (实际实现在 getEngineHealth 中处理)
	}
	if fe.backup != nil && fe.backup.Name() == engineName {
		// 清除备用引擎的冷却状态
	}

	// 触发健康状态变化通知
	if fe.onHealthChange != nil {
		health := &EngineHealth{
			Name:        engineName,
			IsHealthy:   true,
			LastSuccess: now,
		}
		fe.onHealthChange(engineName, health)
	}
}

// switchEngine 切换引擎（公开版本，会获取锁）
func (fe *FailoverEngine) switchEngine() {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	fe.switchEngineLocked()
}

// switchEngineLocked 切换引擎（内部版本，假设调用者已持有锁）
func (fe *FailoverEngine) switchEngineLocked() {
	if fe.backup == nil {
		fe.sendAlert("failover", "备用引擎不可用，无法切换")
		return
	}

	// 检查备用引擎是否在冷却中
	now := time.Now()
	for _, f := range fe.failures {
		if f.Engine == fe.backup.Name() && now.Sub(f.Timestamp) < fe.config.CooldownTime {
			// 备用引擎还在冷却中
			return
		}
	}

	// 切换引擎
	fe.currentEngine = fe.backup
	fe.isSwitched = true

	fe.sendAlert("failover", fmt.Sprintf("已切换到备用引擎：%s", fe.backup.Name()))
}

// ResetEngine 重置引擎状态（手动切换回主引擎）
func (fe *FailoverEngine) ResetEngine() {
	fe.mu.Lock()
	defer fe.mu.Unlock()

	if fe.primary != nil {
		fe.currentEngine = fe.primary
		fe.isSwitched = false
		fe.failures = make([]FailureRecord, 0)

		fe.sendAlert("reset", "已重置引擎状态")
	}
}

// sendAlert 发送告警
func (fe *FailoverEngine) sendAlert(alertType, message string) {
	if !fe.config.EnableAlert {
		return
	}

	if fe.alertCallback != nil {
		fe.alertCallback(alertType, message)
	}
}

// GetFailureCount 获取当前失败计数
func (fe *FailoverEngine) GetFailureCount() int {
	fe.mu.RLock()
	defer fe.mu.RUnlock()
	return len(fe.failures)
}

// GetFailures 获取失败记录列表
func (fe *FailoverEngine) GetFailures() []FailureRecord {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	result := make([]FailureRecord, len(fe.failures))
	copy(result, fe.failures)
	return result
}

// GetEngineHealth 获取引擎健康状态（公开版本，会获取读锁）
func (fe *FailoverEngine) GetEngineHealth(engineName string) *EngineHealth {
	fe.mu.RLock()
	defer fe.mu.RUnlock()
	return fe.getEngineHealthLocked(engineName)
}

// getEngineHealthLocked 内部方法获取引擎健康状态（假设调用者已持有锁）
func (fe *FailoverEngine) getEngineHealthLocked(engineName string) *EngineHealth {
	now := time.Now()
	windowStart := now.Add(-fe.config.FailureWindow)

	// 统计失败次数
	failureCount := 0
	var lastFailure time.Time
	var lastSuccess time.Time

	for _, f := range fe.failures {
		if f.Engine == engineName && f.Timestamp.After(windowStart) {
			failureCount++
			lastFailure = f.Timestamp
		}
	}

	// 检查引擎状态
	var status EngineStatus = EngineStatusIdle
	var isHealthy bool

	if fe.primary != nil && fe.primary.Name() == engineName {
		status = fe.primary.Status()
		isHealthy = status != EngineStatusError && failureCount < fe.config.MaxFailures
	}
	if fe.backup != nil && fe.backup.Name() == engineName {
		status = fe.backup.Status()
		isHealthy = status != EngineStatusError && failureCount < fe.config.MaxFailures
	}

	// 检查冷却状态
	cooldownUntil := time.Time{}
	if lastFailure.IsZero() {
		cooldownUntil = time.Time{}
	} else if lastFailure.Add(fe.config.CooldownTime).After(now) {
		cooldownUntil = lastFailure.Add(fe.config.CooldownTime)
	}

	return &EngineHealth{
		Name:          engineName,
		Status:        status,
		FailureCount:  failureCount,
		LastFailure:   lastFailure,
		LastSuccess:   lastSuccess,
		IsHealthy:     isHealthy,
		CooldownUntil: cooldownUntil,
	}
}

// CheckVersion 检查引擎版本
func (fe *FailoverEngine) CheckVersion(engineName string) (string, error) {
	fe.mu.Lock()
	defer fe.mu.Unlock()

	now := time.Now()

	// 检查缓存
	if info, ok := fe.versionCache[engineName]; ok {
		if now.Sub(info.checked) < 5*time.Minute {
			return info.version, nil
		}
	}

	// 获取版本
	var version string
	var err error

	if fe.primary != nil && fe.primary.Name() == engineName {
		version, err = fe.primary.GetVersion()
	} else if fe.backup != nil && fe.backup.Name() == engineName {
		version, err = fe.backup.GetVersion()
	} else {
		return "", fmt.Errorf("未找到引擎：%s", engineName)
	}

	if err != nil {
		return "", err
	}

	// 更新缓存
	fe.versionCache[engineName] = &versionInfo{
		version: version,
		checked: now,
	}

	return version, nil
}

// UpdateEngine 更新引擎（热更新）
func (fe *FailoverEngine) UpdateEngine(engineName string, binaryPath string) error {
	fe.mu.Lock()
	defer fe.mu.Unlock()

	// 检查是否是当前使用的引擎
	if fe.currentEngine != nil && fe.currentEngine.Name() == engineName {
		// 需要重新初始化引擎
		// 这里可以根据新的 binaryPath 重新创建引擎实例
		// 由于引擎接口限制，这里仅发送通知
		fe.sendAlert("update", fmt.Sprintf("引擎 %s 已更新，路径：%s", engineName, binaryPath))
	}

	// 清除版本缓存
	delete(fe.versionCache, engineName)

	return nil
}

// Download 执行下载（带故障转移）
func (fe *FailoverEngine) Download(ctx context.Context, url string, options DownloadOptions) <-chan DownloadProgress {
	progressChan := make(chan DownloadProgress, 100)

	go func() {
		defer close(progressChan)

		// 尝试使用当前引擎下载
		engine := fe.CurrentEngine()
		if engine == nil {
			select {
			case progressChan <- DownloadProgress{
				Status: "错误：无可用引擎",
			}:
			default:
			}
			return
		}

		// 执行下载
		resultChan := engine.Download(ctx, url, options)

		var lastProgress DownloadProgress
		hasError := false

		for p := range resultChan {
			lastProgress = p
			if p.Status != "" {
				select {
				case progressChan <- p:
				default:
				}
			}

			// 检查状态中是否包含错误标识
			if p.Status != "" && strings.Contains(strings.ToLower(p.Status), "error") {
				hasError = true
			}
		}

		// 处理结果
		if hasError {
			// 记录失败（会自动触发切换逻辑）
			fe.recordFailure(url, fmt.Errorf("下载失败：%s", lastProgress.Status), engine.Name())

			// 如果已切换到备用引擎，使用备用引擎重新下载
			if fe.config.EnableAutoSwitch && fe.IsSwitched() {
				backupEngine := fe.CurrentEngine()
				if backupEngine != nil && backupEngine != engine {
					// 重新执行下载
					resultChan := backupEngine.Download(ctx, url, options)
					for p := range resultChan {
						select {
						case progressChan <- p:
						default:
						}
					}
				}
			}
		} else {
			// 记录成功
			fe.recordSuccess(engine.Name())
		}
	}()

	return progressChan
}

// Name 返回引擎名称
func (fe *FailoverEngine) Name() string {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	if fe.currentEngine != nil {
		return fe.currentEngine.Name()
	}
	return "failover"
}

// Status 返回引擎状态
func (fe *FailoverEngine) Status() EngineStatus {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	if fe.currentEngine != nil {
		return fe.currentEngine.Status()
	}
	return EngineStatusError
}

// CanHandle 判断是否可以处理给定的 URL
func (fe *FailoverEngine) CanHandle(url string) bool {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	if fe.currentEngine != nil {
		return fe.currentEngine.CanHandle(url)
	}
	return false
}

// GetVersion 获取引擎版本
func (fe *FailoverEngine) GetVersion() (string, error) {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	if fe.currentEngine != nil {
		return fe.currentEngine.GetVersion()
	}
	return "", fmt.Errorf("无可用引擎")
}

// IsAvailable 检查引擎是否可用
func (fe *FailoverEngine) IsAvailable() bool {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	if fe.currentEngine != nil {
		return fe.currentEngine.IsAvailable()
	}
	return false
}

// GetPrimaryEngine 获取主引擎
func (fe *FailoverEngine) GetPrimaryEngine() Engine {
	fe.mu.RLock()
	defer fe.mu.RUnlock()
	return fe.primary
}

// GetBackupEngine 获取备用引擎
func (fe *FailoverEngine) GetBackupEngine() Engine {
	fe.mu.RLock()
	defer fe.mu.RUnlock()
	return fe.backup
}

// CheckAndUpdateVersion 检查并更新引擎版本
func (fe *FailoverEngine) CheckAndUpdateVersion(engineName string, checkFunc func() (string, error)) (string, bool, error) {
	currentVersion, err := fe.CheckVersion(engineName)
	if err != nil {
		return "", false, err
	}

	// 调用外部检查函数获取最新版本
	latestVersion, err := checkFunc()
	if err != nil {
		return currentVersion, false, err
	}

	// 比较版本
	if latestVersion != currentVersion {
		// 需要更新
		return latestVersion, true, nil
	}

	return currentVersion, false, nil
}

// CommandRunner 用于执行外部命令的接口
type VersionCommandRunner interface {
	Run(cmd *exec.Cmd) error
	Output(cmd *exec.Cmd) ([]byte, error)
}

// DefaultVersionCommandRunner 默认的命令执行器
type DefaultVersionCommandRunner struct{}

func (r *DefaultVersionCommandRunner) Run(cmd *exec.Cmd) error {
	return cmd.Run()
}

func (r *DefaultVersionCommandRunner) Output(cmd *exec.Cmd) ([]byte, error) {
	return cmd.Output()
}
