// Package engine 提供视频下载引擎的统一接口和实现
package engine

import (
	"context"
	"fmt"
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
// key: 当前状态, value: 允许转换到的状态列表
var ValidTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusQueued:      {TaskStatusDownloading, TaskStatusCancelled},
	TaskStatusDownloading: {TaskStatusMerging, TaskStatusFailed, TaskStatusCancelled},
	TaskStatusMerging:     {TaskStatusCompleted, TaskStatusFailed},
	TaskStatusCompleted:   {}, // 终态，不能再转换
	TaskStatusFailed:      {}, // 终态，不能再转换
	TaskStatusCancelled:   {}, // 终态，不能再转换
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
	BatchID     string // 批量任务 ID
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

// SetStatus 设置任务状态（不验证转换）
func (t *Task) SetStatus(status TaskStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Status = status
}

// TransitionStatus 安全地转换任务状态（验证状态转换）
func (t *Task) TransitionStatus(target TaskStatus) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.Status.CanTransitionTo(target) {
		return fmt.Errorf("无效的状态转换: %s -> %s", t.Status, target)
	}

	t.Status = target
	return nil
}

// SchedulerConfig 调度器配置
type SchedulerConfig struct {
	MaxConcurrent int // 最大并发数
	QueueSize     int // 队列大小
}

// DefaultSchedulerConfig 默认调度器配置
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		MaxConcurrent: 10,  // 默认 10 个并发
		QueueSize:     100, // 队列大小 100
	}
}

// TaskScheduler 任务调度器
// 使用 Go Channel 生产消费者模型实现任务调度
type TaskScheduler struct {
	mu           sync.RWMutex
	config       SchedulerConfig
	tasks        map[string]*Task
	queue        []*Task
	activeCount  int32 // 使用原子操作计数
	ctx          context.Context
	cancel       context.CancelFunc
	engine       Engine
	onTaskUpdate func(task *Task)
	semaphore    chan struct{} // 并发控制信号量
	taskChan     chan *Task    // 任务生产通道
	running      int32         // 调度器运行状态
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
		running:   1, // 默认启动状态
	}

	// 启动消费者协程
	go s.consumerLoop()

	return s
}

// consumerLoop 消费者循环 - 从 taskChan 中消费任务并执行
func (s *TaskScheduler) consumerLoop() {
	for {
		select {
		case <-s.ctx.Done():
			// 调度器关闭，退出循环
			return
		case task, ok := <-s.taskChan:
			if !ok {
				// 通道关闭，退出循环
				return
			}
			// 获取信号量，控制并发
			s.semaphore <- struct{}{}
			// 执行任务
			go s.executeTaskWithSemaphore(task)
		}
	}
}

// executeTaskWithSemaphore 执行任务并在完成后释放信号量
func (s *TaskScheduler) executeTaskWithSemaphore(task *Task) {
	defer func() {
		<-s.semaphore
		atomic.AddInt32(&s.activeCount, -1)
	}()

	atomic.AddInt32(&s.activeCount, 1)
	s.executeTask(task)
}

// Start 启动调度器（已弃用，调度器在创建时自动启动）
// Deprecated: 调度器在 NewTaskScheduler 中已自动启动
func (s *TaskScheduler) Start() {
	// 调度器已在 NewTaskScheduler 中启动，此方法保留用于兼容
}

// Stop 停止调度器
func (s *TaskScheduler) Stop() {
	if atomic.CompareAndSwapInt32(&s.running, 1, 0) {
		s.cancel()
	}
}

// SetTaskUpdateCallback 设置任务更新回调
func (s *TaskScheduler) SetTaskUpdateCallback(callback func(task *Task)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTaskUpdate = callback
}

// AddTask 添加任务到队列
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

	// 向 taskChan 发送任务（生产者）
	select {
	case s.taskChan <- task:
		// 任务已发送到通道
	default:
		// 通道已满，返回错误
		return fmt.Errorf("任务队列已满")
	}

	return nil
}

// insertTaskByPriority 按优先级插入任务到内部队列（高优先级在前）
//
// 注意：当前使用 taskChan 进行任务调度，此方法暂未被调用。
// 保留此方法用于未来可能的优先级重排序功能扩展。
//
// 使用线性插入保证稳定排序，相同优先级按添加顺序排列（FIFO）。
//
// 未来可能的用途：
//   - 当需要实现优先级队列时，可在 consumerLoop 中使用此方法
//   - 当需要动态调整任务优先级顺序时使用
func (s *TaskScheduler) insertTaskByPriority(task *Task) {
	// 找到第一个优先级小于当前任务的位置
	// 高优先级在前，相同优先级按添加顺序（FIFO）
	insertPos := len(s.queue)
	for i := 0; i < len(s.queue); i++ {
		if s.queue[i].Priority < task.Priority {
			insertPos = i
			break
		}
	}

	// 在指定位置插入
	s.queue = append(s.queue, nil)
	copy(s.queue[insertPos+1:], s.queue[insertPos:])
	s.queue[insertPos] = task
}

// UpdateTaskPriority 更新任务优先级
//
// 注意：由于使用 taskChan 进行调度，此方法有以下限制：
//   - 只更新任务元数据中的优先级字段
//   - 已发送到 taskChan 的任务无法更改执行顺序
//   - 只有状态为 TaskStatusQueued 的任务可以更新优先级
//
// 参数：
//   - taskID: 任务 ID
//   - newPriority: 新的优先级
//
// 返回：
//   - error: 任务不存在或状态不允许时返回错误
func (s *TaskScheduler) UpdateTaskPriority(taskID string, newPriority TaskPriority) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("任务不存在：%s", taskID)
	}

	// 检查任务状态，只有排队中的任务可以更新优先级
	if task.Status != TaskStatusQueued {
		return fmt.Errorf("只有排队中的任务可以更新优先级")
	}

	// 更新优先级
	task.Priority = newPriority

	return nil
}

// GetQueuePosition 获取任务在队列中的位置
//
// 已弃用：由于使用 taskChan 进行任务调度，无法确定任务在通道中的具体位置。
// 此方法保留用于 API 兼容性，始终返回 -1。
//
// 参数：
//   - taskID: 任务 ID
//
// 返回：
//   - int: 始终返回 -1（无法确定位置）
//   - error: 任务不存在时返回错误
//
// 替代方案：使用 GetTask() 获取任务状态，或使用 GetActiveCount() 获取当前活动任务数。
func (s *TaskScheduler) GetQueuePosition(taskID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 检查任务是否存在
	if _, ok := s.tasks[taskID]; !ok {
		return -1, fmt.Errorf("任务不存在：%s", taskID)
	}

	// 由于使用 taskChan，无法确定任务在通道中的位置
	return -1, nil
}

// executeTask 执行单个任务
func (s *TaskScheduler) executeTask(task *Task) {
	task.SetStatus(TaskStatusDownloading)
	task.StartedAt = time.Now()
	s.notifyTaskUpdate(task)

	// 创建下载上下文
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	// 开始下载
	progressChan := s.engine.Download(ctx, task.URL, task.Options)

	var lastProgress DownloadProgress
	hasError := false

	for p := range progressChan {
		lastProgress = p
		task.SetProgress(p)
		s.notifyTaskUpdate(task)

		// 检查是否有错误
		if p.Status != "" && (p.Status == "error" || containsIgnoreCase(p.Status, "error")) {
			hasError = true
		}
	}

	// 处理结果
	if hasError {
		task.SetStatus(TaskStatusFailed)
		task.Error = fmt.Errorf("下载失败：%s", lastProgress.Status)
	} else {
		task.SetStatus(TaskStatusCompleted)
		task.CompletedAt = time.Now()
	}

	s.notifyTaskUpdate(task)
}

// containsIgnoreCase 不区分大小写包含
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
			// 转换为小写比较
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

// notifyTaskUpdateLocked 通知任务更新（内部版本，假设调用者已持有锁）
func (s *TaskScheduler) notifyTaskUpdateLocked(task *Task) {
	callback := s.onTaskUpdate
	if callback != nil {
		callback(task)
	}
}

// notifyTaskUpdate 通知任务更新（公开版本）
func (s *TaskScheduler) notifyTaskUpdate(task *Task) {
	s.mu.RLock()
	callback := s.onTaskUpdate
	s.mu.RUnlock()

	if callback != nil {
		callback(task)
	}
}

// GetTask 获取任务
func (s *TaskScheduler) GetTask(taskID string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("任务不存在：%s", taskID)
	}
	return task, nil
}

// CancelTask 取消任务
func (s *TaskScheduler) CancelTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("任务不存在：%s", taskID)
	}

	// 检查任务状态
	if task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed || task.Status == TaskStatusCancelled {
		return fmt.Errorf("任务已完成/失败/已取消，无法取消")
	}

	task.SetStatus(TaskStatusCancelled)
	task.CompletedAt = time.Now()

	// 从队列中移除
	for i, t := range s.queue {
		if t.ID == taskID {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			break
		}
	}

	s.notifyTaskUpdateLocked(task)
	return nil
}

// GetQueue 获取队列中的任务
func (s *TaskScheduler) GetQueue() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Task, len(s.queue))
	copy(result, s.queue)
	return result
}

// GetActiveCount 获取活动任务数
func (s *TaskScheduler) GetActiveCount() int {
	return int(atomic.LoadInt32(&s.activeCount))
}

// GetTaskCount 获取总任务数
func (s *TaskScheduler) GetTaskCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tasks)
}

// ClearCompletedTasks 清理已完成的任务
func (s *TaskScheduler) ClearCompletedTasks() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for id, task := range s.tasks {
		if task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed || task.Status == TaskStatusCancelled {
			delete(s.tasks, id)
			count++
		}
	}

	return count
}

// Shutdown 关闭调度器
func (s *TaskScheduler) Shutdown() {
	s.cancel()

	// 等待所有任务完成
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

// BatchScheduler 批量任务调度器
type BatchScheduler struct {
	mu            sync.RWMutex
	batchID       string
	tasks         []*Task
	completed     int
	failed        int
	cancelled     int
	total         int
	onBatchUpdate func(batch *BatchScheduler)
}

// NewBatchScheduler 创建批量任务调度器
func NewBatchScheduler(batchID string) *BatchScheduler {
	return &BatchScheduler{
		batchID: batchID,
		tasks:   make([]*Task, 0),
	}
}

// SetBatchUpdateCallback 设置批量更新回调
func (bs *BatchScheduler) SetBatchUpdateCallback(callback func(batch *BatchScheduler)) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.onBatchUpdate = callback
}

// AddTask 添加任务到批量
func (bs *BatchScheduler) AddTask(task *Task) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	task.BatchID = bs.batchID
	bs.tasks = append(bs.tasks, task)
	bs.total = len(bs.tasks)
}

// GetProgress 获取批量任务进度
func (bs *BatchScheduler) GetProgress() (completed, failed, cancelled, total int) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return bs.completed, bs.failed, bs.cancelled, bs.total
}

// GetBatchID 获取批量 ID
func (bs *BatchScheduler) GetBatchID() string {
	return bs.batchID
}

// GetTasks 获取所有任务
func (bs *BatchScheduler) GetTasks() []*Task {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	result := make([]*Task, len(bs.tasks))
	copy(result, bs.tasks)
	return result
}

// UpdateTaskStatus 更新任务状态
func (bs *BatchScheduler) UpdateTaskStatus(taskID string, status TaskStatus) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	for _, task := range bs.tasks {
		if task.ID == taskID {
			task.SetStatus(status)

			switch status {
			case TaskStatusCompleted:
				bs.completed++
			case TaskStatusFailed:
				bs.failed++
			case TaskStatusCancelled:
				bs.cancelled++
			}

			if bs.onBatchUpdate != nil {
				bs.onBatchUpdate(bs)
			}
			return
		}
	}
}

// GetOverallProgress 获取整体进度百分比
func (bs *BatchScheduler) GetOverallProgress() float64 {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	if bs.total == 0 {
		return 0
	}

	return float64(bs.completed+bs.failed+bs.cancelled) / float64(bs.total) * 100
}

// GetStatus 获取批量任务状态
func (bs *BatchScheduler) GetStatus() string {
	completed, failed, cancelled, total := bs.GetProgress()
	return fmt.Sprintf("完成：%d, 失败：%d, 取消：%d, 总计：%d, 进度：%.1f%%",
		completed, failed, cancelled, total, bs.GetOverallProgress())
}
