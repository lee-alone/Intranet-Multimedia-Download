package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"
)

// DependencyStatus 表示依赖项状态
type DependencyStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// HealthStatus 表示健康检查状态
type HealthStatus struct {
	Status    string           `json:"status"` // "healthy", "degraded", "unhealthy"
	Timestamp string           `json:"timestamp"`
	Database  bool             `json:"database"`
	YtDlp     DependencyStatus `json:"yt_dlp"`
	FFmpeg    DependencyStatus `json:"ffmpeg"`
	Start     time.Time        `json:"-"` // 服务器启动时间
}

// Metrics 表示 Prometheus 指标
type Metrics struct {
	TotalRequests int64     `json:"total_requests"`
	ActiveUsers   int64     `json:"active_users"`
	QueueSize     int64     `json:"queue_size"`
	Uptime        string    `json:"uptime"`
	StartTime     time.Time `json:"-"`
}

// updateHealthStatus 更新健康状态
func (s *Server) updateHealthStatus() {
	s.healthMutex.Lock()
	defer s.healthMutex.Unlock()

	// 检查数据库连接
	dbHealthy := false
	if s.db != nil {
		err := s.db.Ping()
		dbHealthy = (err == nil)
	}

	// 检查 yt-dlp
	ytDlpStatus := checkCommand("yt-dlp", "--version")

	// 检查 ffmpeg
	ffmpegStatus := checkCommand("ffmpeg", "-version")

	// 确定整体状态
	status := "healthy"
	if !dbHealthy {
		status = "unhealthy"
	} else if !ytDlpStatus.Available && !ffmpegStatus.Available {
		status = "degraded"
	}

	s.healthStatus = HealthStatus{
		Status:    status,
		Timestamp: time.Now().Format(time.RFC3339),
		Database:  dbHealthy,
		YtDlp:     ytDlpStatus,
		FFmpeg:    ffmpegStatus,
	}
}

// checkCommand 检查命令是否可用并获取版本
func checkCommand(name, arg string) DependencyStatus {
	status := DependencyStatus{Name: name, Available: false}

	cmd := exec.Command(name, arg)
	output, err := cmd.Output()
	if err != nil {
		status.Error = err.Error()
		return status
	}

	status.Available = true
	status.Version = string(output[:len(output)-1]) // 去掉换行符
	return status
}

// healthHandler 处理健康检查请求（简化版）
func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	s.healthMutex.RLock()
	status := s.healthStatus
	s.healthMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	switch status.Status {
	case "healthy":
		w.WriteHeader(http.StatusOK)
	case "degraded":
		w.WriteHeader(http.StatusOK) // 降级状态仍返回 200
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	fmt.Fprintf(w, `{"status":"%s","timestamp":"%s"}`, status.Status, status.Timestamp)
}

// healthzHandler 处理详细健康检查请求
func (s *Server) healthzHandler(w http.ResponseWriter, _ *http.Request) {
	s.healthMutex.RLock()
	status := s.healthStatus
	s.healthMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	var httpStatus int
	switch status.Status {
	case "healthy":
		httpStatus = http.StatusOK
	case "degraded":
		httpStatus = http.StatusOK
	default:
		httpStatus = http.StatusServiceUnavailable
	}
	w.WriteHeader(httpStatus)

	json.NewEncoder(w).Encode(status)
}

// readyHandler 处理就绪检查请求
func (s *Server) readyHandler(w http.ResponseWriter, _ *http.Request) {
	s.healthMutex.RLock()
	status := s.healthStatus
	s.healthMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	// 数据库和至少一个下载器可用才算就绪
	ready := status.Database && (status.YtDlp.Available || status.FFmpeg.Available)

	if ready {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	fmt.Fprintf(w, `{"ready":%t,"timestamp":"%s","database":%t,"yt_dlp":%t,"ffmpeg":%t}`,
		ready, status.Timestamp, status.Database, status.YtDlp.Available, status.FFmpeg.Available)
}

// liveHandler 处理存活检查请求
func (s *Server) liveHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"alive":true,"timestamp":"%s"}`, time.Now().Format(time.RFC3339))
}

// metricsHandler 处理 Prometheus 格式指标请求
func (s *Server) metricsHandler(w http.ResponseWriter, _ *http.Request) {
	s.healthMutex.RLock()
	status := s.healthStatus
	s.healthMutex.RUnlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	// Prometheus 格式指标
	fmt.Fprintf(w, "# HELP server_health 服务器健康状态 (1=healthy, 0=unhealthy)\n")
	fmt.Fprintf(w, "# TYPE server_health gauge\n")
	if status.Status == "healthy" {
		fmt.Fprintf(w, "server_health 1\n")
	} else {
		fmt.Fprintf(w, "server_health 0\n")
	}

	fmt.Fprintf(w, "# HELP server_database_health 数据库健康状态\n")
	fmt.Fprintf(w, "# TYPE server_database_health gauge\n")
	if status.Database {
		fmt.Fprintf(w, "server_database_health 1\n")
	} else {
		fmt.Fprintf(w, "server_database_health 0\n")
	}

	fmt.Fprintf(w, "# HELP server_ytdlp_available yt-dlp 是否可用\n")
	fmt.Fprintf(w, "# TYPE server_ytdlp_available gauge\n")
	if status.YtDlp.Available {
		fmt.Fprintf(w, "server_ytdlp_available 1\n")
	} else {
		fmt.Fprintf(w, "server_ytdlp_available 0\n")
	}

	fmt.Fprintf(w, "# HELP server_ffmpeg_available ffmpeg 是否可用\n")
	fmt.Fprintf(w, "# TYPE server_ffmpeg_available gauge\n")
	if status.FFmpeg.Available {
		fmt.Fprintf(w, "server_ffmpeg_available 1\n")
	} else {
		fmt.Fprintf(w, "server_ffmpeg_available 0\n")
	}

	fmt.Fprintf(w, "# HELP server_uptime_seconds 服务器运行时间（秒）\n")
	fmt.Fprintf(w, "# TYPE server_uptime_seconds counter\n")
	fmt.Fprintf(w, "server_uptime_seconds %.0f\n", time.Since(s.startTime).Seconds())
}

// apiMetricsHandler 处理 API 格式指标请求
func (s *Server) apiMetricsHandler(w http.ResponseWriter, r *http.Request) {
	s.healthMutex.RLock()
	status := s.healthStatus
	s.healthMutex.RUnlock()

	metrics := map[string]interface{}{
		"health": map[string]interface{}{
			"status":   status.Status,
			"database": status.Database,
			"yt_dlp":   status.YtDlp.Available,
			"ffmpeg":   status.FFmpeg.Available,
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}
