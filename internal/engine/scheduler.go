// Package engine 提供视频下载引擎的统一接口和实现
package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
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
	engine           Engine
	onTaskUpdate     func(task *Task)
	onProgressUpdate func(task *Task)
	semaphore        chan struct{}
	taskChan         chan *Task
	running          int32
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
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	if s.engine == nil {
		log.Printf("错误：任务调度器未配置下载引擎")
		task.TransitionStatus(TaskStatusFailed)
		task.Error = fmt.Errorf("未配置下载引擎")
		s.notifyTaskUpdate(task)
		return
	}

	progressChan := s.engine.Download(ctx, task.URL, task.Options)
	var lastProgress DownloadProgress
	hasError := false
	for p := range progressChan {
		lastProgress = p
		task.SetProgress(p)
		// 同步标题信息
		if p.Title != "" {
			task.Title = p.Title
		}
		s.notifyTaskUpdate(task)
		if p.Status != "" && (p.Status == "error" || containsIgnoreCase(p.Status, "error")) {
			hasError = true
		}
	}
	if hasError {
		task.TransitionStatus(TaskStatusFailed)
		task.Error = fmt.Errorf("下载失败：%s", lastProgress.Status)
	} else {
		// 先转换到 Merging 状态，然后再转换到 Completed
		task.TransitionStatus(TaskStatusMerging)
		s.notifyTaskUpdate(task)
		// 使用 DownloadProgress 中的 FilePath 和 Title 字段
		task.FilePath = lastProgress.FilePath
		if lastProgress.Title != "" {
			task.Title = lastProgress.Title
		}
		task.CompletedAt = time.Now()
		// 任务完成时强制设置进度为 100%（避免卡在 merging 前的进度）
		task.SetProgress(DownloadProgress{Percent: 100, Status: "completed"})
		task.TransitionStatus(TaskStatusCompleted)
		s.notifyTaskUpdate(task)
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
	callback := s.onTaskUpdate
	s.mu.RUnlock()
	if callback != nil {
		callback(task)
	}
	if s.onProgressUpdate != nil {
		s.onProgressUpdate(task)
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
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("任务不存在：%s", taskID)
	}
	if task.Status.IsTerminal() {
		return fmt.Errorf("任务已完成/失败/已取消，无法取消")
	}
	// 使用 TransitionStatus 进行状态转换
	if err := task.TransitionStatus(TaskStatusCancelled); err != nil {
		log.Printf("任务 %s 状态转换失败：%v", taskID, err)
		return err
	}
	task.CompletedAt = time.Now()
	for i, t := range s.queue {
		if t.ID == taskID {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			break
		}
	}
	s.notifyTaskUpdate(task)
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
