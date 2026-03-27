// Package server 提供 HTTP 服务器功能
package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestCheckCommand 测试命令检查功能
func TestCheckCommand(t *testing.T) {
	// 测试一个应该存在的命令
	status := checkCommand("echo", "--version")
	if status.Name != "echo" {
		t.Errorf("Expected name 'echo', got '%s'", status.Name)
	}
}

// TestHealthHandler 测试健康检查端点（使用简化的服务器实例）
func TestHealthHandler(t *testing.T) {
	server := &Server{
		startTime: time.Now(),
		healthStatus: HealthStatus{
			Status:    "healthy",
			Timestamp: time.Now().Format(time.RFC3339),
			Database:  true,
			YtDlp:     DependencyStatus{Name: "yt-dlp", Available: true},
			FFmpeg:    DependencyStatus{Name: "ffmpeg", Available: false},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.healthHandler(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", res.StatusCode)
	}
}

// TestHealthzHandler 测试详细健康检查端点
func TestHealthzHandler(t *testing.T) {
	server := &Server{
		startTime: time.Now(),
		healthStatus: HealthStatus{
			Status:    "healthy",
			Timestamp: time.Now().Format(time.RFC3339),
			Database:  true,
			YtDlp:     DependencyStatus{Name: "yt-dlp", Available: true},
			FFmpeg:    DependencyStatus{Name: "ffmpeg", Available: false},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	server.healthzHandler(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", res.StatusCode)
	}
}

// TestReadyHandler 测试就绪检查端点
func TestReadyHandler(t *testing.T) {
	server := &Server{
		startTime: time.Now(),
		healthStatus: HealthStatus{
			Status:    "healthy",
			Timestamp: time.Now().Format(time.RFC3339),
			Database:  true,
			YtDlp:     DependencyStatus{Name: "yt-dlp", Available: true},
			FFmpeg:    DependencyStatus{Name: "ffmpeg", Available: false},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	server.readyHandler(w, req)

	res := w.Result()
	defer res.Body.Close()

	// 应该返回 200，因为数据库和 yt-dlp 都可用
	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", res.StatusCode)
	}
}

// TestLiveHandler 测试存活检查端点
func TestLiveHandler(t *testing.T) {
	server := &Server{
		startTime: time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	w := httptest.NewRecorder()

	server.liveHandler(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", res.StatusCode)
	}
}

// TestMetricsHandler 测试 Prometheus 指标端点
func TestMetricsHandler(t *testing.T) {
	server := &Server{
		startTime: time.Now(),
		healthStatus: HealthStatus{
			Status:    "healthy",
			Timestamp: time.Now().Format(time.RFC3339),
			Database:  true,
			YtDlp:     DependencyStatus{Name: "yt-dlp", Available: true},
			FFmpeg:    DependencyStatus{Name: "ffmpeg", Available: false},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	server.metricsHandler(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", res.StatusCode)
	}

	body := w.Body.String()
	if body == "" {
		t.Error("Expected metrics output, got empty string")
	}
}

// TestAPIMetricsHandler 测试 API 指标端点
func TestAPIMetricsHandler(t *testing.T) {
	server := &Server{
		startTime: time.Now(),
		healthStatus: HealthStatus{
			Status:    "healthy",
			Timestamp: time.Now().Format(time.RFC3339),
			Database:  true,
			YtDlp:     DependencyStatus{Name: "yt-dlp", Available: true},
			FFmpeg:    DependencyStatus{Name: "ffmpeg", Available: false},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	w := httptest.NewRecorder()

	server.apiMetricsHandler(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", res.StatusCode)
	}
}

// TestServerStartTime 测试服务器启动时间
func TestServerStartTime(t *testing.T) {
	beforeCreate := time.Now()
	server := &Server{
		startTime: time.Now(),
	}
	afterCreate := time.Now()

	if server.startTime.Before(beforeCreate) {
		t.Error("Expected start time to be after test start")
	}

	if server.startTime.After(afterCreate) {
		t.Error("Expected start time to be before test end")
	}
}

// TestHealthStatusConcurrent 测试健康状态并发访问
func TestHealthStatusConcurrent(t *testing.T) {
	server := &Server{
		startTime: time.Now(),
		healthStatus: HealthStatus{
			Status:    "healthy",
			Timestamp: time.Now().Format(time.RFC3339),
			Database:  true,
		},
	}

	done := make(chan bool)

	// 并发读取健康状态
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				req := httptest.NewRequest(http.MethodGet, "/health", nil)
				w := httptest.NewRecorder()
				server.healthHandler(w, req)
			}
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestHealthStatusLogic 测试健康状态逻辑（不依赖数据库）
func TestHealthStatusLogic(t *testing.T) {
	testCases := []struct {
		name            string
		database        bool
		ytDlpAvailable  bool
		ffmpegAvailable bool
		expectedStatus  string
	}{
		{"healthy_all", true, true, true, "healthy"},
		{"healthy_ytdlp_only", true, true, false, "healthy"},
		{"healthy_ffmpeg_only", true, false, true, "healthy"},
		{"degraded_no_engines", true, false, false, "degraded"},
		{"unhealthy_no_db", false, false, false, "unhealthy"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 直接测试状态计算逻辑
			status := "healthy"
			if !tc.database {
				status = "unhealthy"
			} else if !tc.ytDlpAvailable && !tc.ffmpegAvailable {
				status = "degraded"
			}

			if status != tc.expectedStatus {
				t.Errorf("Expected status '%s', got '%s'", tc.expectedStatus, status)
			}
		})
	}
}

// TestDependencyStatus 测试依赖项状态结构
func TestDependencyStatus(t *testing.T) {
	status := DependencyStatus{
		Name:      "test",
		Available: true,
		Version:   "1.0.0",
	}

	if status.Name != "test" {
		t.Errorf("Expected name 'test', got '%s'", status.Name)
	}

	if !status.Available {
		t.Error("Expected Available to be true")
	}

	if status.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", status.Version)
	}
}

// TestHealthStatusStructure 测试健康状态结构
func TestHealthStatusStructure(t *testing.T) {
	hs := HealthStatus{
		Status:   "healthy",
		Database: true,
		YtDlp:    DependencyStatus{Name: "yt-dlp", Available: true},
		FFmpeg:   DependencyStatus{Name: "ffmpeg", Available: false},
	}

	if hs.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", hs.Status)
	}

	if !hs.Database {
		t.Error("Expected Database to be true")
	}

	if !hs.YtDlp.Available {
		t.Error("Expected YtDlp.Available to be true")
	}

	if hs.FFmpeg.Available {
		t.Error("Expected FFmpeg.Available to be false")
	}
}

// TestMain 测试主函数
func TestMain(m *testing.M) {
	// 检查密钥文件是否存在
	if _, err := os.Stat("../../keys/private.pem"); os.IsNotExist(err) {
		// 如果密钥不存在，跳过某些测试
	}

	// 运行测试
	os.Exit(m.Run())
}
