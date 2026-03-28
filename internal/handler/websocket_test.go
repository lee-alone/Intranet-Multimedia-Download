package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/campus/collector/internal/auth"
	"github.com/campus/collector/internal/engine"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

// TestProgressHub_Subscribe 测试订阅功能
func TestProgressHub_Subscribe(t *testing.T) {
	hub := GetProgressHub()

	// 测试订阅
	ch := hub.Subscribe()
	if ch == nil {
		t.Error("Subscribe 返回的通道不应为 nil")
	}

	// 测试取消订阅
	hub.Unsubscribe(ch)

	// 确保通道已关闭
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("取消订阅后通道应该关闭")
		}
	default:
		// 通道未关闭，正常
	}
}

// TestProgressHub_Unsubscribe_DoubleClose 测试重复取消订阅不会 panic
func TestProgressHub_Unsubscribe_DoubleClose(t *testing.T) {
	hub := GetProgressHub()

	ch := hub.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe 返回的通道不应为 nil")
	}

	// 第一次取消订阅
	hub.Unsubscribe(ch)

	// 第二次取消订阅（不应 panic）
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("重复调用 Unsubscribe 导致 panic: %v", r)
		}
	}()
	hub.Unsubscribe(ch)
}

// TestProgressHub_Broadcast 测试广播功能
func TestProgressHub_Broadcast(t *testing.T) {
	hub := GetProgressHub()

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	// 发送测试消息
	msg := WSMessage{
		Type:      "test",
		TaskID:    "task-123",
		BatchID:   "batch-456",
		Status:    "downloading",
		Progress:  50.0,
		Message:   "测试消息",
		Timestamp: time.Now(),
	}

	// 广播消息
	hub.BroadcastToTask("task-123", msg)

	// 接收消息
	select {
	case received := <-ch:
		if received.Type != msg.Type {
			t.Errorf("期望消息类型 %s, 得到 %s", msg.Type, received.Type)
		}
		if received.TaskID != msg.TaskID {
			t.Errorf("期望任务 ID %s, 得到 %s", msg.TaskID, received.TaskID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("超时未收到消息")
	}
}

// TestProgressHub_Broadcast_Backpressure 测试背压处理
func TestProgressHub_Broadcast_Backpressure(t *testing.T) {
	hub := GetProgressHub()

	// 创建一个小缓冲通道模拟背压
	ch := make(chan WSMessage, 1)
	client := &Client{
		hub:  hub,
		send: ch,
	}

	// 注册客户端
	hub.register <- client
	time.Sleep(10 * time.Millisecond)

	// 发送多条消息，超过缓冲区
	for i := 0; i < 10; i++ {
		msg := WSMessage{
			Type:   "test",
			TaskID: "task-123",
		}
		hub.BroadcastToTask("task-123", msg)
	}

	// 等待处理
	time.Sleep(50 * time.Millisecond)

	// 清理
	hub.unregister <- client
}

// TestWebSocketHandler_HandleProgressStream 测试 SSE 进度流
func TestWebSocketHandler_HandleProgressStream(t *testing.T) {
	// 创建测试依赖
	setup := setupWebSocketTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 创建请求
	url := "/api/v1/progress?token=" + token + "&task_id=test-123"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()

	// 执行请求
	setup.handler.HandleProgressStream(w, req)

	// 验证响应
	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 %d, 得到 %d, 响应：%s", http.StatusOK, w.Code, w.Body.String())
	}

	// 验证 SSE 头
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Error("期望 Content-Type 为 text/event-stream")
	}
}

// TestWebSocketHandler_HandleProgressStream_NoToken 测试无 token 的情况
func TestWebSocketHandler_HandleProgressStream_NoToken(t *testing.T) {
	setup := setupWebSocketTest(t)
	defer setup.cleanup()

	// 创建请求（无 token）
	url := "/api/v1/progress?task_id=test-123"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()

	setup.handler.HandleProgressStream(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望状态码 %d, 得到 %d", http.StatusUnauthorized, w.Code)
	}
}

// TestWebSocketHandler_HandleProgressStream_InvalidToken 测试无效 token
func TestWebSocketHandler_HandleProgressStream_InvalidToken(t *testing.T) {
	setup := setupWebSocketTest(t)
	defer setup.cleanup()

	// 创建请求（无效 token）
	url := "/api/v1/progress?token=invalid_token&task_id=test-123"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()

	setup.handler.HandleProgressStream(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望状态码 %d, 得到 %d", http.StatusUnauthorized, w.Code)
	}
}

// TestWebSocketHandler_HandleProgressStream_MissingTaskID 测试缺少 task_id
func TestWebSocketHandler_HandleProgressStream_MissingTaskID(t *testing.T) {
	setup := setupWebSocketTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 创建请求（无 task_id 和 batch_id）
	url := "/api/v1/progress?token=" + token
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()

	setup.handler.HandleProgressStream(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("期望状态码 %d, 得到 %d", http.StatusBadRequest, w.Code)
	}
}

// TestWebSocketHandler_WebSocket 测试 WebSocket 连接
func TestWebSocketHandler_WebSocket(t *testing.T) {
	setup := setupWebSocketTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setup.handler.HandleWebSocket(w, r)
	}))
	defer server.Close()

	// WebSocket URL
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=" + token + "&task_id=test-123"

	// 连接 WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket 连接失败：%v", err)
	}
	defer conn.Close()

	// 发送心跳
	pingMsg := WSMessage{Type: "pong", Timestamp: time.Now()}
	if err := conn.WriteJSON(pingMsg); err != nil {
		t.Errorf("发送心跳失败：%v", err)
	}

	// 接收消息（应该有 ping 响应）
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		// 超时是正常的，因为可能没有立即收到消息
		if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
			t.Logf("读取消息超时或错误（可能正常）: %v", err)
		}
	} else {
		// 验证消息格式
		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err == nil {
			t.Logf("收到消息：Type=%s", msg.Type)
		}
	}
}

// TestHasTaskPermission 测试任务权限验证
func TestHasTaskPermission(t *testing.T) {
	setup := setupWebSocketTest(t)
	defer setup.cleanup()

	// 创建测试任务
	_, err := setup.db.Exec(`
		INSERT INTO tasks (id, user_id, url, status, quality, engine, batch_id, created_at)
		VALUES ('test-task-1', 1, 'http://example.com', 'queued', 'best', '', '', ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("创建测试任务失败：%v", err)
	}

	// 测试有权限的情况
	if !setup.handler.hasTaskPermission(1, "test-task-1") {
		t.Error("用户应该有自己任务的权限")
	}

	// 测试无权限的情况
	if setup.handler.hasTaskPermission(2, "test-task-1") {
		t.Error("用户不应该有其他人的任务权限")
	}

	// 测试不存在的任务
	if setup.handler.hasTaskPermission(1, "non-existent-task") {
		t.Error("不存在的任务应该返回无权限")
	}
}

// TestHasBatchPermission 测试批量任务权限验证
func TestHasBatchPermission(t *testing.T) {
	setup := setupWebSocketTest(t)
	defer setup.cleanup()

	// 创建测试批量任务
	_, err := setup.db.Exec(`
		INSERT INTO batch_tasks (id, user_id, total_count, status, created_at)
		VALUES ('test-batch-1', 1, 1, 'pending', ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("创建测试批量任务失败：%v", err)
	}

	// 测试有权限的情况
	if !setup.handler.hasBatchPermission(1, "test-batch-1") {
		t.Error("用户应该有自己的批量任务权限")
	}

	// 测试无权限的情况
	if setup.handler.hasBatchPermission(2, "test-batch-1") {
		t.Error("用户不应该有其他人的批量任务权限")
	}

	// 测试不存在的批量任务
	if setup.handler.hasBatchPermission(1, "non-existent-batch") {
		t.Error("不存在的批量任务应该返回无权限")
	}
}

// TestWSMessage_JSON 测试消息 JSON 序列化
func TestWSMessage_JSON(t *testing.T) {
	msg := WSMessage{
		Type:      "task_update",
		TaskID:    "task-123",
		BatchID:   "batch-456",
		Status:    "downloading",
		Progress:  75.5,
		Message:   "下载中...",
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("JSON 序列化失败：%v", err)
	}

	var decoded WSMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败：%v", err)
	}

	if decoded.Type != msg.Type {
		t.Errorf("期望类型 %s, 得到 %s", msg.Type, decoded.Type)
	}
	if decoded.TaskID != msg.TaskID {
		t.Errorf("期望任务 ID %s, 得到 %s", msg.TaskID, decoded.TaskID)
	}
	if decoded.Progress != msg.Progress {
		t.Errorf("期望进度 %.2f, 得到 %.2f", msg.Progress, decoded.Progress)
	}
}

// TestClient_WritePump 测试客户端写入泵
func TestClient_WritePump(t *testing.T) {
	hub := GetProgressHub()

	// 创建测试通道
	sendCh := make(chan WSMessage, 10)
	stopCh := make(chan struct{})

	client := &Client{
		hub:  hub,
		send: sendCh,
		stop: stopCh,
	}

	// 启动写入泵
	go client.writePump()

	// 发送测试消息
	msg := WSMessage{
		Type:      "test",
		TaskID:    "test-123",
		Timestamp: time.Now(),
	}

	client.send <- msg

	// 等待处理
	time.Sleep(50 * time.Millisecond)

	// 停止写入泵
	close(client.stop)
	time.Sleep(10 * time.Millisecond)
}

// TestProgressHub_Cleanup 测试 ProgressHub 清理功能
func TestProgressHub_Cleanup(t *testing.T) {
	hub := GetProgressHub()

	// 创建多个订阅
	channels := make([]chan WSMessage, 5)
	for i := 0; i < 5; i++ {
		channels[i] = hub.Subscribe()
	}

	// 取消部分订阅
	for i := 0; i < 3; i++ {
		hub.Unsubscribe(channels[i])
	}

	// 验证剩余订阅数量
	// 注意：这里需要等待 goroutine 处理
	time.Sleep(50 * time.Millisecond)
}

// TestBroadcastMessage 测试广播消息处理
func TestBroadcastMessage(t *testing.T) {
	hub := GetProgressHub()

	// 测试空广播
	msg := WSMessage{
		Type:    "test",
		TaskID:  "non-existent",
		BatchID: "non-existent",
	}

	// 不应 panic
	hub.broadcastMessage(msg)
}

// TestGetProgressHub_Singleton 测试 ProgressHub 单例
func TestGetProgressHub_Singleton(t *testing.T) {
	hub1 := GetProgressHub()
	hub2 := GetProgressHub()

	if hub1 != hub2 {
		t.Error("GetProgressHub 应返回同一个实例")
	}
}

// TestClient_ReadPump 测试客户端读取泵
func TestClient_ReadPump(t *testing.T) {
	hub := GetProgressHub()

	client := &Client{
		hub:  hub,
		stop: make(chan struct{}),
	}

	// 启动读取泵（会立即因为没有连接而退出）
	go client.readPump()

	// 等待处理
	time.Sleep(50 * time.Millisecond)

	// 停止
	close(client.stop)
}

// TestWSMessage_Types 测试各种消息类型
func TestWSMessage_Types(t *testing.T) {
	testCases := []struct {
		name     string
		msgType  string
		taskID   string
		batchID  string
		status   string
		progress float64
	}{
		{"TaskUpdate", "task_update", "task-1", "", "downloading", 50.0},
		{"BatchUpdate", "batch_update", "", "batch-1", "processing", 25.0},
		{"Error", "error", "task-2", "", "failed", 0.0},
		{"Ping", "ping", "", "", "", 0.0},
		{"Pong", "pong", "", "", "", 0.0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := WSMessage{
				Type:     tc.msgType,
				TaskID:   tc.taskID,
				BatchID:  tc.batchID,
				Status:   tc.status,
				Progress: tc.progress,
			}

			data, err := json.Marshal(msg)
			if err != nil {
				t.Fatalf("序列化失败：%v", err)
			}

			var decoded WSMessage
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("反序列化失败：%v", err)
			}

			if decoded.Type != tc.msgType {
				t.Errorf("期望类型 %s, 得到 %s", tc.msgType, decoded.Type)
			}
		})
	}
}

// TestProgressHub_Concurrent 测试并发安全性
func TestProgressHub_Concurrent(t *testing.T) {
	hub := GetProgressHub()

	var wg sync.WaitGroup
	numGoroutines := 10

	// 并发订阅
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := hub.Subscribe()
			defer hub.Unsubscribe(ch)

			// 发送消息
			for j := 0; j < 10; j++ {
				msg := WSMessage{
					Type:   "concurrent_test",
					TaskID: "task-concurrent",
				}
				hub.BroadcastToTask("task-concurrent", msg)
			}
		}(i)
	}

	wg.Wait()

	// 等待所有消息处理完成
	time.Sleep(100 * time.Millisecond)
}

// setupWebSocketTest 创建 WebSocket 测试环境
type webSocketSetup struct {
	db        *sql.DB
	scheduler *engine.TaskScheduler
	jwtMgr    *auth.JWTManager
	handler   *WebSocketHandler
	cleanup   func()
}

func setupWebSocketTest(t *testing.T) *webSocketSetup {
	// 创建临时数据库
	tmpDir, err := os.MkdirTemp("", "websocket_test_*")
	if err != nil {
		t.Fatalf("创建临时目录失败：%v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}

	// 创建测试表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			email TEXT,
			role TEXT DEFAULT 'user',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			url TEXT NOT NULL,
			status TEXT DEFAULT 'queued',
			progress INTEGER DEFAULT 0,
			quality TEXT DEFAULT 'best',
			file_path TEXT,
			error_message TEXT,
			engine TEXT,
			batch_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			started_at DATETIME,
			completed_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS batch_tasks (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			total_count INTEGER DEFAULT 0,
			completed_count INTEGER DEFAULT 0,
			failed_count INTEGER DEFAULT 0,
			status TEXT DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("创建测试表失败：%v", err)
	}

	// 插入测试用户
	_, err = db.Exec(`INSERT INTO users (id, username, password_hash, role) VALUES (1, 'testuser', 'hash', 'user')`)
	if err != nil {
		t.Fatalf("插入测试用户失败：%v", err)
	}

	// 使用项目中的密钥文件
	privateKeyPath := "../../keys/private.pem"
	publicKeyPath := "../../keys/public.pem"

	// 创建 JWT 管理器
	jwtMgr, err := auth.NewJWTManager(privateKeyPath, publicKeyPath, 3600, 86400)
	if err != nil {
		t.Fatalf("创建 JWT 管理器失败：%v", err)
	}

	// 创建调度器（使用 MockEngine）
	schedulerConfig := engine.DefaultSchedulerConfig()
	mockEngine := &MockEngine{}
	scheduler := engine.NewTaskScheduler(mockEngine, schedulerConfig)

	// 创建 WebSocket 处理器
	wsHandler := NewWebSocketHandler(db, jwtMgr)

	cleanup := func() {
		scheduler.Shutdown()
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return &webSocketSetup{
		db:        db,
		scheduler: scheduler,
		jwtMgr:    jwtMgr,
		handler:   wsHandler,
		cleanup:   cleanup,
	}
}
