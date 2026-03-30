// Package handler 提供 HTTP 请求处理器
package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/campus/collector/internal/auth"
	"github.com/campus/collector/internal/engine"
	"github.com/gorilla/websocket"
)

// WSMessage WebSocket 消息结构
type WSMessage struct {
	Type      string      `json:"type"`           // 消息类型：task_update, batch_update, error, ping
	TaskID    string      `json:"task_id"`        // 任务 ID
	BatchID   string      `json:"batch_id"`       // 批量 ID
	Status    string      `json:"status"`         // 任务状态
	Progress  float64     `json:"progress"`       // 进度百分比
	Message   string      `json:"message"`        // 消息内容
	Timestamp time.Time   `json:"timestamp"`      // 时间戳
	Data      interface{} `json:"data,omitempty"` // 额外数据
}

// ProgressHub 进度推送中心
type ProgressHub struct {
	mu         sync.RWMutex
	clients    map[*Client]bool
	taskSubs   map[string]map[*Client]bool // task_id -> clients
	batchSubs  map[string]map[*Client]bool // batch_id -> clients
	broadcast  chan WSMessage
	register   chan *Client
	unregister chan *Client
}

// Client WebSocket 客户端连接
type Client struct {
	mu      sync.Mutex
	hub     *ProgressHub
	conn    *websocket.Conn
	userID  int
	taskID  string
	batchID string
	send    chan WSMessage
	stop    chan struct{}
}

// 全局进度推送中心实例
var progressHub *ProgressHub
var progressHubOnce sync.Once

// GetProgressHub 获取或创建全局进度推送中心
func GetProgressHub() *ProgressHub {
	progressHubOnce.Do(func() {
		progressHub = &ProgressHub{
			clients:    make(map[*Client]bool),
			taskSubs:   make(map[string]map[*Client]bool),
			batchSubs:  make(map[string]map[*Client]bool),
			broadcast:  make(chan WSMessage, 256),
			register:   make(chan *Client),
			unregister: make(chan *Client),
		}
		go progressHub.run()
	})
	return progressHub
}

// run 运行推送中心
func (h *ProgressHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			// 订阅任务或批量任务
			if client.taskID != "" {
				if h.taskSubs[client.taskID] == nil {
					h.taskSubs[client.taskID] = make(map[*Client]bool)
				}
				h.taskSubs[client.taskID][client] = true
			}
			if client.batchID != "" {
				if h.batchSubs[client.batchID] == nil {
					h.batchSubs[client.batchID] = make(map[*Client]bool)
				}
				h.batchSubs[client.batchID][client] = true
			}
			h.mu.Unlock()
			log.Printf("WebSocket client connected: user=%d, task=%s, batch=%s", client.userID, client.taskID, client.batchID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			// 清理订阅
			if client.taskID != "" && h.taskSubs[client.taskID] != nil {
				delete(h.taskSubs[client.taskID], client)
			}
			if client.batchID != "" && h.batchSubs[client.batchID] != nil {
				delete(h.batchSubs[client.batchID], client)
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			h.broadcastMessage(msg)
		}
	}
}

// broadcastMessage 广播消息给相关订阅者
func (h *ProgressHub) broadcastMessage(msg WSMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 发送到任务订阅者
	if clients, ok := h.taskSubs[msg.TaskID]; ok {
		for client := range clients {
			select {
			case client.send <- msg:
			default:
				// 客户端缓冲区已满，断开连接并记录日志
				log.Printf("WebSocket client buffer full, disconnecting: user=%d, task=%s", client.userID, msg.TaskID)
				close(client.send)
				delete(h.clients, client)
			}
		}
	}

	// 发送到批量任务订阅者
	if clients, ok := h.batchSubs[msg.BatchID]; ok {
		for client := range clients {
			select {
			case client.send <- msg:
			default:
				// 客户端缓冲区已满，断开连接并记录日志
				log.Printf("WebSocket client buffer full, disconnecting: user=%d, batch=%s", client.userID, msg.BatchID)
				close(client.send)
				delete(h.clients, client)
			}
		}
	}
}

// BroadcastToTask 向特定任务广播消息
func (h *ProgressHub) BroadcastToTask(taskID string, msg WSMessage) {
	msg.TaskID = taskID
	h.broadcast <- msg
}

// WebSocket 处理器
type WebSocketHandler struct {
	hub      *ProgressHub
	jwtMgr   *auth.JWTManager
	upgrader websocket.Upgrader
	db       *sql.DB
}

// NewWebSocketHandler 创建 WebSocket 处理器
func NewWebSocketHandler(db *sql.DB, jwtMgr *auth.JWTManager) *WebSocketHandler {
	return &WebSocketHandler{
		hub:    GetProgressHub(),
		jwtMgr: jwtMgr,
		db:     db,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// 限制来源（生产环境）
				origin := r.Header.Get("Origin")
				allowedOrigins := []string{
					"http://localhost:5173",
					"http://localhost",
					"http://127.0.0.1:5173",
					"http://127.0.0.1",
				}
				for _, o := range allowedOrigins {
					if origin == o {
						return true
					}
				}
				// 生产环境白名单（可从配置读取）
				if origin == "https://your-domain.com" {
					return true
				}
				return false
			},
		},
	}
}

// HandleWebSocket 处理 WebSocket 连接请求
func (h *WebSocketHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 验证 Token
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing token parameter", http.StatusUnauthorized)
		return
	}

	// 验证 JWT Token
	claims, err := h.jwtMgr.ValidateToken(token)
	if err != nil {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	// 获取订阅的任务 ID 或批量 ID
	taskID := r.URL.Query().Get("task_id")
	batchID := r.URL.Query().Get("batch_id")

	if taskID == "" && batchID == "" {
		http.Error(w, "Missing task_id or batch_id parameter", http.StatusBadRequest)
		return
	}

	// 验证用户权限（用户只能订阅自己的任务）
	if taskID != "" {
		if !h.hasTaskPermission(claims.UserID, taskID) {
			http.Error(w, "No permission to access this task", http.StatusForbidden)
			return
		}
	}
	if batchID != "" {
		if !h.hasBatchPermission(claims.UserID, batchID) {
			http.Error(w, "No permission to access this batch", http.StatusForbidden)
			return
		}
	}

	// 升级为 WebSocket 连接
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// 创建客户端
	client := &Client{
		hub:     h.hub,
		conn:    conn,
		userID:  claims.UserID,
		taskID:  taskID,
		batchID: batchID,
		send:    make(chan WSMessage, 256),
		stop:    make(chan struct{}),
	}

	// 注册客户端
	h.hub.register <- client

	// 启动写入协程
	go client.writePump()

	// 启动读取协程（处理心跳）
	go client.readPump()
}

// writePump 从通道读取消息并写入 WebSocket
func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		if c.conn != nil {
			c.conn.Close()
		}
	}()

	// 如果连接为 nil，只消费消息但不写入
	if c.conn == nil {
		for range c.send {
			// 消费消息但不处理
		}
		return
	}

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			c.mu.Lock()
			if err := c.conn.WriteJSON(msg); err != nil {
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()
		case <-ticker.C:
			// 发送心跳
			c.mu.Lock()
			if err := c.conn.WriteJSON(WSMessage{Type: "ping", Timestamp: time.Now()}); err != nil {
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()
		case <-c.stop:
			return
		}
	}
}

// readPump 从 WebSocket 读取消息（处理心跳响应）
func (c *Client) readPump() {
	// 如果没有连接，直接返回
	if c.conn == nil {
		return
	}

	defer func() {
		c.hub.unregister <- c
		if c.conn != nil {
			c.conn.Close()
		}
	}()

	for {
		select {
		case <-c.stop:
			return
		default:
			c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				return
			}
			// 处理心跳响应
			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err == nil {
				if msg.Type == "pong" {
					// 心跳响应，更新最后活动时间
				}
			}
		}
	}
}

// HandleProgressStream 处理进度流请求（Server-Sent Events 备用方案）
func (h *WebSocketHandler) HandleProgressStream(w http.ResponseWriter, r *http.Request) {
	// 验证 Token
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing token parameter", http.StatusUnauthorized)
		return
	}

	// 验证 JWT Token
	claims, err := h.jwtMgr.ValidateToken(token)
	if err != nil {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	// 获取订阅的任务 ID 或批量 ID
	taskID := r.URL.Query().Get("task_id")
	batchID := r.URL.Query().Get("batch_id")

	if taskID == "" && batchID == "" {
		http.Error(w, "Missing task_id or batch_id parameter", http.StatusBadRequest)
		return
	}

	// 验证用户权限（用户只能订阅自己的任务）
	if taskID != "" {
		if !h.hasTaskPermission(claims.UserID, taskID) {
			http.Error(w, "No permission to access this task", http.StatusForbidden)
			return
		}
	}
	if batchID != "" {
		if !h.hasBatchPermission(claims.UserID, batchID) {
			http.Error(w, "No permission to access this batch", http.StatusForbidden)
			return
		}
	}

	// 设置 SSE 头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// 订阅任务更新
	ch := h.hub.Subscribe()
	defer h.hub.Unsubscribe(ch)

	// 发送初始连接消息
	initialMsg := WSMessage{
		Type:      "connected",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"user_id":  claims.UserID,
			"task_id":  taskID,
			"batch_id": batchID,
		},
	}
	fmt.Fprintf(w, "data: %s\n\n", toJSON(initialMsg))
	w.(http.Flusher).Flush()

	// 获取客户端关闭通道
	clientGone := r.Context().Done()

	// 发送心跳
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-clientGone:
			return
		case <-ticker.C:
			// 发送心跳
			fmt.Fprintf(w, "data: {\"type\":\"ping\",\"timestamp\":\"%s\"}\n\n", time.Now().Format(time.RFC3339))
			w.(http.Flusher).Flush()
		case msg := <-ch:
			// 过滤消息，只发送相关的任务更新
			if taskID != "" && msg.TaskID != taskID {
				continue
			}
			if batchID != "" && msg.BatchID != batchID {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", toJSON(msg))
			w.(http.Flusher).Flush()
		}
	}
}

// Subscribe 订阅所有任务更新（SSE 方案）
func (h *ProgressHub) Subscribe() chan WSMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan WSMessage, 100)
	// 使用一个虚拟客户端来跟踪订阅
	client := &Client{send: ch}
	h.clients[client] = true
	return ch
}

// Unsubscribe 取消订阅（SSE 方案）- 防止重复调用导致 panic
func (h *ProgressHub) Unsubscribe(ch chan WSMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	client := &Client{send: ch}
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		select {
		case <-ch:
			// 通道已关闭
		default:
			close(ch)
		}
	}
}

// hasTaskPermission 检查用户是否有权限访问任务
func (h *WebSocketHandler) hasTaskPermission(userID int, taskID string) bool {
	if h.db == nil {
		return false
	}
	var ownerID int
	err := h.db.QueryRow("SELECT user_id FROM tasks WHERE id = ?", taskID).Scan(&ownerID)
	if err != nil {
		return false
	}
	return ownerID == userID
}

// hasBatchPermission 检查用户是否有权限访问批量任务
func (h *WebSocketHandler) hasBatchPermission(userID int, batchID string) bool {
	if h.db == nil {
		return false
	}
	var ownerID int
	err := h.db.QueryRow("SELECT user_id FROM batch_tasks WHERE id = ?", batchID).Scan(&ownerID)
	if err != nil {
		return false
	}
	return ownerID == userID
}

// NotifyTaskUpdate 通知任务更新
func NotifyTaskUpdate(task *engine.Task) {
	hub := GetProgressHub()
	if hub == nil {
		return
	}

	msg := WSMessage{
		Type:      "task_update",
		TaskID:    task.ID,
		BatchID:   task.BatchID,
		Status:    string(task.Status),
		Progress:  task.Progress.Percent,
		Timestamp: time.Now(),
	}

	hub.BroadcastToTask(task.ID, msg)
}

// toJSON 将对象转换为 JSON 字符串
func toJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	return string(data)
}
