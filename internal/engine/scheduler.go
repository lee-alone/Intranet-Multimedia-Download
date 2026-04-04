// Package engine 提供视频下载引擎的统一接口和实现
package engine

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/campus/collector/internal/audit"
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusQueued      TaskStatus = "queued"      // 排队中
	TaskStatusDownloading TaskStatus = "downloading" // 下载中
	TaskStatusMerging     TaskStatus = "merging"     // 合并中
	TaskStatusCompleted   TaskStatus = "completed"   // 已完成
	TaskStatusFailed      TaskStatus = "failed"      // 失败
	TaskStatusCancelled   TaskStatus = "cancelled"   // 已取消
)

// ValidTransitions 定义任务状态的有效转换
var ValidTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusQueued:      {TaskStatusDownloading, TaskStatusCancelled},
	TaskStatusDownloading: {TaskStatusMerging, TaskStatusFailed, TaskStatusCancelled},
	TaskStatusMerging:     {TaskStatusCompleted, TaskStatusFailed},
	TaskStatusCompleted:   {},
	TaskStatusFailed:      {},
	TaskStatusCancelled:   {},
}

// CanTransitionTo 检查是否可以转换到目标状态
func (s TaskStatus) CanTransitionTo(target TaskStatus) bool {
	allowedTargets, ok := ValidTransitions[s]
	if !ok {
		return false
	}
	for _, t := range allowedTargets {
		if t == target {
			return true
		}
	}
	return false
}

// IsTerminal 检查是否为终态
func (s TaskStatus) IsTerminal() bool {
	return s == TaskStatusCompleted || s == TaskStatusFailed || s == TaskStatusCancelled
}

// String 返回状态的中文名称
func (s TaskStatus) String() string {
	switch s {
	case TaskStatusQueued:
		return "排队中"
	case TaskStatusDownloading:
		return "下载中"
	case TaskStatusMerging:
		return "合并中"
	case TaskStatusCompleted:
		return "已完成"
	case TaskStatusFailed:
		return "失败"
	case TaskStatusCancelled:
		return "已取消"
	default:
		return "未知"
	}
}

// TaskPriority 任务优先级
type TaskPriority int

const (
	PriorityLow    TaskPriority = 0
	PriorityNormal TaskPriority = 1
	PriorityHigh   TaskPriority = 2
	PriorityUrgent TaskPriority = 3
)

// Task 下载任务
type Task struct {
	ID          string
	URL         string
	Options     DownloadOptions
	Priority    TaskPriority
	Status      TaskStatus
	Progress    DownloadProgress
	Engine      string
	BatchID     string
	Title       string    // 视频标题
	FilePath    string    // 文件路径
	CreatedAt   time.Time
	StartedAt   time.Time
	CompletedAt time.Time
	Error       error
	mu          sync.RWMutex
}

// GetProgress 获取任务进度
func (t *Task) GetProgress() DownloadProgress {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Progress
}

// SetProgress 设置任务进度
func (t *Task) SetProgress(p DownloadProgress) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Progress = p
}

// GetStatus 获取任务状态
func (t *Task) GetStatus() TaskStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status
}

// SetStatus 设置任务状态
func (t *Task) SetStatus(status TaskStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Status = status
}

// TransitionStatus 安全地转换任务状态
func (t *Task) TransitionStatus(target TaskStatus) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.Status.CanTransitionTo(target) {
		return fmt.Errorf("无效的状态转换：%s -> %s", t.Status, target)
	}
	t.Status = target
	return nil
}

// SchedulerConfig 调度器配置
type SchedulerConfig struct {
	MaxConcurrent int
	QueueSize     int
}

// DefaultSchedulerConfig 默认调度器配置
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		MaxConcurrent: 10,
		QueueSize:     100,
	}
}

// TaskScheduler 任务调度器
type TaskScheduler struct {
	mu               sync.RWMutex
	config           SchedulerConfig
	tasks            map[string]*Task
	queue            []*Task
	activeCount      int32
	ctx              context.Context
	cancel           context.CancelFunc
	cancels          map[string]context.CancelFunc // 存储每个活跃任务的取消函数
	engine           Engine
	onTaskUpdate     func(task *Task)
	onProgressUpdate func(task *Task)
	semaphore        chan struct{}
	taskChan         chan *Task
	running          int32
	auditLogger      *audit.Logger // 审计日志记录器
	cookieGetter     CookieGetter  // Cookie 获取接口（可选）
	tempFiles        []string      // 临时文件列表（用于清理）
	tempFilesMu      sync.Mutex    // 临时文件互斥锁
}

// CookieGetter 获取 Cookie 的接口
type CookieGetter interface {
	// GetCookieForDownload 根据用户ID、角色和URL域名获取 Cookie 内容
	GetCookieForDownload(userID int, role string, urlDomain string) (string, error)
}

// SetCookieGetter 设置 Cookie 获取器
func (s *TaskScheduler) SetCookieGetter(cg CookieGetter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cookieGetter = cg
}

// NewTaskScheduler 创建任务调度器
func NewTaskScheduler(engine Engine, config SchedulerConfig) *TaskScheduler {
	if config.MaxConcurrent <= 0 {
		config = DefaultSchedulerConfig()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &TaskScheduler{
		config:    config,
		tasks:     make(map[string]*Task),
		queue:     make([]*Task, 0),
		cancels:   make(map[string]context.CancelFunc),
		semaphore: make(chan struct{}, config.MaxConcurrent),
		taskChan:  make(chan *Task, config.QueueSize),
		ctx:       ctx,
		cancel:    cancel,
		engine:    engine,
		running:   1,
	}
	go s.consumerLoop()
	return s
}

// SetEngine 设置下载引擎
func (s *TaskScheduler) SetEngine(engine Engine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.engine = engine
}

// SetOnTaskUpdate 设置任务状态更新回调
func (s *TaskScheduler) SetOnTaskUpdate(fn func(task *Task)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTaskUpdate = fn
}

// SetOnProgressUpdate 设置进度更新回调
func (s *TaskScheduler) SetOnProgressUpdate(fn func(task *Task)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onProgressUpdate = fn
}

// SetAuditLogger 设置审计日志记录器
func (s *TaskScheduler) SetAuditLogger(logger *audit.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditLogger = logger
}

func (s *TaskScheduler) consumerLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case task, ok := <-s.taskChan:
			if !ok {
				return
			}
			s.semaphore <- struct{}{}
			go s.executeTask(task)
		}
	}
}

func (s *TaskScheduler) SetTaskUpdateCallback(callback func(task *Task)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTaskUpdate = callback
}

// SetProgressUpdateCallback 设置进度更新回调（用于 WebSocket 推送）
func (s *TaskScheduler) SetProgressUpdateCallback(callback func(task *Task)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onProgressUpdate = callback
}

func (s *TaskScheduler) AddTask(task *Task) error {
	if task == nil {
		return fmt.Errorf("任务不能为空")
	}
	if task.Status == "" {
		task.Status = TaskStatusQueued
	}
	task.CreatedAt = time.Now()
	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()
	select {
	case s.taskChan <- task:
	default:
		return fmt.Errorf("任务队列已满")
	}
	return nil
}

func (s *TaskScheduler) executeTask(task *Task) {
	defer func() { <-s.semaphore }() // 释放信号量

	// 使用 TransitionStatus 进行状态转换
	if err := task.TransitionStatus(TaskStatusDownloading); err != nil {
		log.Printf("任务 %s 状态转换失败：%v", task.ID, err)
		return
	}
	s.notifyTaskUpdate(task)
	log.Printf("开始执行任务 %s: URL=%s", task.ID, task.URL)
	task.StartedAt = time.Now()
	s.notifyTaskUpdate(task)

	// 创建任务级别的取消函数，并注册到 cancels map
	taskCtx, taskCancel := context.WithCancel(s.ctx)
	s.mu.Lock()
	s.cancels[task.ID] = taskCancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.cancels, task.ID)
		s.mu.Unlock()
		taskCancel() // 确保释放资源
	}()

	// 处理 Cookie 文件（如果配置了 CookieGetter）
	var tempCookieFile string
	var cookieFileDeleted bool
	if s.cookieGetter != nil && task.Options.UserID > 0 {
		// 从 URL 中提取域名
		domain := extractDomainFromURL(task.URL)
		if domain != "" {
			cookieContent, err := s.cookieGetter.GetCookieForDownload(task.Options.UserID, task.Options.UserRole, domain)
			if err == nil && cookieContent != "" {
				// 创建临时 Cookie 文件
				tempCookieFile, err = s.createTempCookieFile(cookieContent)
				if err != nil {
					log.Printf("警告：创建临时 Cookie 文件失败：%v", err)
				} else {
					// 更新下载选项中的 Cookie 文件路径
					task.Options.CookieFile = tempCookieFile
					log.Printf("任务 %s 使用用户 Cookie，域名：%s", task.ID, domain)
				}
			}
		}
	}

	// 确保在所有退出路径下都清理临时 Cookie 文件
	defer func() {
		if tempCookieFile != "" && !cookieFileDeleted {
			if err := os.Remove(tempCookieFile); err != nil {
				// 文件可能已被 yt-dlp 启动后删除，忽略不存在的错误
				if !os.IsNotExist(err) {
					log.Printf("警告：清理临时 Cookie 文件失败：%s, 错误：%v", tempCookieFile, err)
				}
			} else {
				log.Printf("已清理临时 Cookie 文件：%s", tempCookieFile)
			}
		}
	}()

	if s.engine == nil {
		log.Printf("错误：任务调度器未配置下载引擎")
		task.TransitionStatus(TaskStatusFailed)
		task.Error = fmt.Errorf("未配置下载引擎")
		s.notifyTaskUpdate(task)
		return
	}

	progressChan := s.engine.Download(taskCtx, task.URL, task.Options)
	var lastProgress DownloadProgress
	hasError := false
	for p := range progressChan {
		lastProgress = p
		task.SetProgress(p)
		// 同步标题信息
		if p.Title != "" {
			task.Title = p.Title
		}
		// 同步文件路径
		if p.FilePath != "" {
			task.FilePath = p.FilePath
		}
		s.notifyTaskUpdate(task)
		// 检测错误：如果状态包含 error 关键字，标记为错误
		if p.Status != "" && (strings.HasPrefix(p.Status, "error") || containsIgnoreCase(p.Status, "error")) {
			hasError = true
		}
	}
	if hasError {
		// 关键增强：将错误详细信息输出到系统控制台
		errMsg := lastProgress.Status
		log.Printf("[FAILED] 任务 %s (UE-HIDDEN: %s) 失败！ 使用引擎: %s | URL: %s | 错误详情: %s",
			task.ID, task.ID[:min(6, len(task.ID))], task.Engine, task.URL, errMsg)

		task.TransitionStatus(TaskStatusFailed)
		task.Error = fmt.Errorf("下载失败：%s", errMsg)
		s.notifyTaskUpdate(task)

		// 记录审计日志
		s.logTaskFailure(task, errMsg)
	} else {
		// 验证：只有在进度接近或达到 100% 时才能标记为完成
		// 如果最后一次进度小于 90%，说明可能异常退出
		if lastProgress.Percent < 90 {
			// 关键增强：输出异常退出的详细信息
			log.Printf("[FAILED] 任务 %s (UE-HIDDEN: %s) 异常退出！ 使用引擎: %s | URL: %s | 最终进度: %.1f%% | 状态: %s",
				task.ID, task.ID[:min(6, len(task.ID))], task.Engine, task.URL, lastProgress.Percent, lastProgress.Status)

			task.TransitionStatus(TaskStatusFailed)
			task.Error = fmt.Errorf("下载异常退出，进度仅 %.1f%%", lastProgress.Percent)
			s.notifyTaskUpdate(task)

			// 记录审计日志
			s.logTaskFailure(task, fmt.Sprintf("下载异常退出，进度仅 %.1f%%", lastProgress.Percent))
		} else {
			// 先转换到 Merging 状态，然后再转换到 Completed
			task.TransitionStatus(TaskStatusMerging)
			s.notifyTaskUpdate(task)
			// 使用 DownloadProgress 中的 FilePath 和 Title 字段
			if lastProgress.FilePath != "" {
				task.FilePath = lastProgress.FilePath
			}
			if lastProgress.Title != "" {
				task.Title = lastProgress.Title
			}
			task.CompletedAt = time.Now()
			// 任务完成时强制设置进度为 100%
			task.SetProgress(DownloadProgress{Percent: 100, Status: "completed"})
			task.TransitionStatus(TaskStatusCompleted)
			s.notifyTaskUpdate(task)
		}
	}
	s.notifyTaskUpdate(task)
}

func containsIgnoreCase(s, substr string) bool {
	return containsCI(s, substr)
}

func containsCI(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			c1 := s[i+j]
			c2 := substr[j]
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 32
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 32
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func (s *TaskScheduler) notifyTaskUpdate(task *Task) {
	s.mu.RLock()
	onUpdate := s.onTaskUpdate
	onProgress := s.onProgressUpdate
	s.mu.RUnlock()
	if onUpdate != nil {
		onUpdate(task)
	}
	if onProgress != nil {
		onProgress(task)
	}
}

func (s *TaskScheduler) GetTask(taskID string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("任务不存在：%s", taskID)
	}
	return task, nil
}

func (s *TaskScheduler) CancelTask(taskID string) error {
	s.mu.Lock()
	task, ok := s.tasks[taskID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("任务不存在：%s", taskID)
	}
	if task.Status.IsTerminal() {
		s.mu.Unlock()
		return fmt.Errorf("任务已完成/失败/已取消，无法取消")
	}
	// 修改状态
	if err := task.TransitionStatus(TaskStatusCancelled); err != nil {
		s.mu.Unlock()
		log.Printf("任务 %s 状态转换失败：%v", taskID, err)
		return err
	}
	task.CompletedAt = time.Now()
	// 从队列中移除
	for i, t := range s.queue {
		if t.ID == taskID {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			break
		}
	}
	// 取出取消函数
	cancelFunc, hasCancel := s.cancels[taskID]

	// 关键：在执行耗时或可能触发后续锁的操作前，先释放调度器锁
	s.mu.Unlock()

	log.Printf("正在取消任务 %s...", taskID)
	// 执行物理取消 (此时不持有锁)
	if hasCancel {
		cancelFunc()
	}
	// 执行更新通知 (此时不持有锁)
	s.notifyTaskUpdate(task)

	log.Printf("任务 %s 已成功标记取消并终止进程\n", taskID)
	return nil
}

func (s *TaskScheduler) GetActiveCount() int {
	return int(atomic.LoadInt32(&s.activeCount))
}

func (s *TaskScheduler) GetTaskCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tasks)
}

func (s *TaskScheduler) Shutdown() {
	s.cancel()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case <-timeout:
			return
		default:
			s.mu.RLock()
			if s.activeCount == 0 {
				s.mu.RUnlock()
				return
			}
			s.mu.RUnlock()
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// logTaskFailure 记录任务失败的审计日志
func (s *TaskScheduler) logTaskFailure(task *Task, errorMsg string) {
	s.mu.RLock()
	logger := s.auditLogger
	s.mu.RUnlock()

	if logger == nil {
		return
	}

	// 构建审计日志
	resourceType := audit.ResourceTypeTask
	auditLog := &audit.AuditLog{
		Action:       audit.ActionRetryTask, // 使用 retry_task 表示任务异常
		ResourceType: &resourceType,
		Detail: map[string]interface{}{
			"task_id":    task.ID,
			"url":        task.URL,
			"engine":     task.Engine,
			"error":      errorMsg,
			"progress":   task.Progress.Percent,
			"event_type": "task_failure",
		},
		CreatedAt: time.Now(),
	}

	// 异步记录，不阻塞主流程
	go func() {
		if err := logger.Log(auditLog); err != nil {
			log.Printf("[AUDIT] 记录任务失败日志错误: %v", err)
		}
	}()
}

// extractDomainFromURL 从 URL 中提取域名
func extractDomainFromURL(url string) string {
	// 简单的域名提取，去除协议和路径
	// 例如：https://www.bilibili.com/video/xxx -> bilibili.com
	url = strings.ToLower(url)
	
	// 去除协议
	if idx := strings.Index(url, "://"); idx >= 0 {
		url = url[idx+3:]
	}
	
	// 去除路径
	if idx := strings.Index(url, "/"); idx >= 0 {
		url = url[:idx]
	}
	
	// 去除端口号
	if idx := strings.Index(url, ":"); idx >= 0 {
		url = url[:idx]
	}
	
	// 去除 www. 前缀
	url = strings.TrimPrefix(url, "www.")
	
	// 去除 m. 前缀（移动端）
	url = strings.TrimPrefix(url, "m.")
	
	return url
}

// createTempCookieFile 创建临时 Cookie 文件
func (s *TaskScheduler) createTempCookieFile(content string) (string, error) {
	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "cookie_*.txt")
	if err != nil {
		return "", fmt.Errorf("创建临时 Cookie 文件失败：%w", err)
	}
	
	tempPath := tmpFile.Name()
	
	// 写入 Cookie 内容
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tempPath)
		return "", fmt.Errorf("写入 Cookie 内容失败：%w", err)
	}
	
	// 关闭文件
	if err := tmpFile.Close(); err != nil {
		os.Remove(tempPath)
		return "", fmt.Errorf("关闭 Cookie 文件失败：%w", err)
	}
	
	// 设置文件权限（仅所有者可读写）
	if err := os.Chmod(tempPath, 0600); err != nil {
		log.Printf("警告：设置 Cookie 文件权限失败：%v", err)
	}
	
	// 记录临时文件（用于清理）
	s.tempFilesMu.Lock()
	s.tempFiles = append(s.tempFiles, tempPath)
	s.tempFilesMu.Unlock()
	
	return tempPath, nil
}
