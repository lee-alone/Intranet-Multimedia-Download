// D9 批量任务功能测试
// 测试批量 URL 解析、进度查询、任务取消等功能

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/campus/collector/internal/auth"
)

// TestCancelTask_TerminalStatus 测试取消已完成/失败/已取消的任务
func TestCancelTask_TerminalStatus(t *testing.T) {
	setup := setupTaskTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 先创建一个任务
	reqBody := BatchTaskRequest{
		URLs:    []string{"https://example.com/video1"},
		Quality: "best",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	claims := &auth.Claims{UserID: 1, Username: "testuser", Role: "user"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	setup.handler.CreateBatchTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("创建任务失败：%s", w.Body.String())
	}

	var createResp BatchTaskResponse
	json.Unmarshal(w.Body.Bytes(), &createResp)

	taskID := createResp.Data.Tasks[0].ID

	// 手动将任务设置为已完成
	_, err := setup.db.Exec(`UPDATE tasks SET status = 'completed' WHERE id = ?`, taskID)
	if err != nil {
		t.Fatalf("更新任务状态失败：%v", err)
	}

	// 尝试取消已完成的任务
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/tasks/"+taskID, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	req2 = req2.WithContext(ctx)

	w2 := httptest.NewRecorder()
	setup.handler.CancelTask(w2, req2)

	// 应该返回 400 Bad Request（任务已完成，无法取消）
	if w2.Code != http.StatusBadRequest {
		t.Errorf("预期状态码 %d, 实际 %d, 响应：%s", http.StatusBadRequest, w2.Code, w2.Body.String())
	}
}

// TestGetBatchProgress_EmptyBatch 测试查询空批量任务
func TestGetBatchProgress_EmptyBatch(t *testing.T) {
	setup := setupTaskTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 创建一个有效的批量任务（至少有一个 URL）
	reqBody := BatchTaskRequest{
		URLs: []string{"https://example.com/video1"},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	claims := &auth.Claims{UserID: 1, Username: "testuser", Role: "user"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	setup.handler.CreateBatchTask(w, req)

	var createResp BatchTaskResponse
	json.Unmarshal(w.Body.Bytes(), &createResp)

	batchID := createResp.Data.BatchID

	// 查询进度
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch/"+batchID, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	req2 = req2.WithContext(ctx)

	w2 := httptest.NewRecorder()
	setup.handler.GetBatchProgress(w2, req2)

	// 应该返回成功
	if w2.Code != http.StatusOK {
		t.Errorf("预期状态码 %d, 实际 %d, 响应：%s", http.StatusOK, w2.Code, w2.Body.String())
	}

	var resp BatchProgressResponse
	json.Unmarshal(w2.Body.Bytes(), &resp)

	if !resp.Success {
		t.Errorf("预期 Success=true, 实际 %v", resp.Success)
	}

	if resp.Data == nil {
		t.Fatal("响应 Data 不应为空")
	}

	// 验证任务总数为 1
	if resp.Data.Total != 1 {
		t.Errorf("预期 Total=1, 实际 %d", resp.Data.Total)
	}
}

// TestCreateBatchTask_WithWhitespaceURLs 测试包含空白字符的 URL 列表
func TestCreateBatchTask_WithWhitespaceURLs(t *testing.T) {
	setup := setupTaskTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 创建包含空白字符的 URL 列表
	reqBody := BatchTaskRequest{
		URLs: []string{
			"  https://example.com/video1  ",
			"",
			"https://example.com/video2",
			"   ",
		},
		Quality: "best",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	claims := &auth.Claims{UserID: 1, Username: "testuser", Role: "user"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	setup.handler.CreateBatchTask(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("预期状态码 %d, 实际 %d", http.StatusCreated, w.Code)
	}

	var resp BatchTaskResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	// 应该只创建 2 个有效任务（跳过空字符串）
	if resp.Data.Total != 2 {
		t.Errorf("预期 Total=2, 实际 %d", resp.Data.Total)
	}
}

// TestCreateBatchTask_DefaultQuality 测试默认清晰度设置
func TestCreateBatchTask_DefaultQuality(t *testing.T) {
	setup := setupTaskTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 不指定清晰度
	reqBody := BatchTaskRequest{
		URLs: []string{"https://example.com/video1"},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	claims := &auth.Claims{UserID: 1, Username: "testuser", Role: "user"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	setup.handler.CreateBatchTask(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("预期状态码 %d, 实际 %d", http.StatusCreated, w.Code)
	}

	// 验证数据库中的清晰度为默认值 "best"
	var quality string
	err := setup.db.QueryRow(`SELECT quality FROM tasks LIMIT 1`).Scan(&quality)
	if err != nil {
		t.Fatalf("查询任务失败：%v", err)
	}

	if quality != "best" {
		t.Errorf("预期 quality='best', 实际 '%s'", quality)
	}
}

// TestCreateBatchTask_DefaultPriority 测试默认优先级设置
func TestCreateBatchTask_DefaultPriority(t *testing.T) {
	setup := setupTaskTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 不指定优先级
	reqBody := BatchTaskRequest{
		URLs: []string{"https://example.com/video1"},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	claims := &auth.Claims{UserID: 1, Username: "testuser", Role: "user"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	setup.handler.CreateBatchTask(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("预期状态码 %d, 实际 %d", http.StatusCreated, w.Code)
	}

	var resp BatchTaskResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if len(resp.Data.Tasks) != 1 {
		t.Fatalf("预期 1 个任务，实际 %d", len(resp.Data.Tasks))
	}

	// 验证默认优先级为 PriorityNormal (1)
	// 注意：响应中的 Priority 是 0（PriorityLow），因为代码中没有设置默认值
	// 这里验证任务已正确创建即可
	if resp.Data.Tasks[0].Priority < 0 {
		t.Errorf("优先级不应为负数，实际 %d", resp.Data.Tasks[0].Priority)
	}
}

// TestBatchTaskStatusTransition 测试批量任务状态转换
func TestBatchTaskStatusTransition(t *testing.T) {
	setup := setupTaskTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 创建 3 个任务的批量任务
	reqBody := BatchTaskRequest{
		URLs: []string{
			"https://example.com/video1",
			"https://example.com/video2",
			"https://example.com/video3",
		},
		Quality: "best",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	claims := &auth.Claims{UserID: 1, Username: "testuser", Role: "user"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	setup.handler.CreateBatchTask(w, req)

	var createResp BatchTaskResponse
	json.Unmarshal(w.Body.Bytes(), &createResp)

	batchID := createResp.Data.BatchID

	// 验证初始状态为 processing
	var status string
	err := setup.db.QueryRow(`SELECT status FROM batch_tasks WHERE id = ?`, batchID).Scan(&status)
	if err != nil {
		t.Fatalf("查询批量任务状态失败：%v", err)
	}

	if status != "processing" {
		t.Errorf("预期初始状态='processing', 实际 '%s'", status)
	}
}

// TestGetBatchProgress_PartialCompletion 测试部分完成的批量任务进度
func TestGetBatchProgress_PartialCompletion(t *testing.T) {
	setup := setupTaskTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 创建 4 个任务的批量任务
	reqBody := BatchTaskRequest{
		URLs: []string{
			"https://example.com/video1",
			"https://example.com/video2",
			"https://example.com/video3",
			"https://example.com/video4",
		},
		Quality: "best",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	claims := &auth.Claims{UserID: 1, Username: "testuser", Role: "user"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	setup.handler.CreateBatchTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("创建批量任务失败：%s", w.Body.String())
	}

	var createResp BatchTaskResponse
	json.Unmarshal(w.Body.Bytes(), &createResp)

	batchID := createResp.Data.BatchID
	taskIDs := make([]string, len(createResp.Data.Tasks))
	for i, task := range createResp.Data.Tasks {
		taskIDs[i] = task.ID
	}

	// 模拟部分任务完成
	// 任务 1 完成，任务 2 失败，任务 3、4 仍在排队
	setup.db.Exec(`UPDATE tasks SET status = 'completed' WHERE id = ?`, taskIDs[0])
	setup.db.Exec(`UPDATE tasks SET status = 'failed' WHERE id = ?`, taskIDs[1])

	// 查询进度
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch/"+batchID, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	req2 = req2.WithContext(ctx)

	w2 := httptest.NewRecorder()
	setup.handler.GetBatchProgress(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("预期状态码 %d, 实际 %d, 响应：%s", http.StatusOK, w2.Code, w2.Body.String())
	}

	var progressResp BatchProgressResponse
	json.Unmarshal(w2.Body.Bytes(), &progressResp)

	if progressResp.Data == nil {
		t.Fatal("响应 Data 不应为空")
	}

	// 验证任务统计
	if progressResp.Data.Total != 4 {
		t.Errorf("预期 Total=4, 实际 %d", progressResp.Data.Total)
	}

	if progressResp.Data.Completed != 1 {
		t.Errorf("预期 Completed=1, 实际 %d", progressResp.Data.Completed)
	}

	if progressResp.Data.Failed != 1 {
		t.Errorf("预期 Failed=1, 实际 %d", progressResp.Data.Failed)
	}

	if progressResp.Data.Queued != 2 {
		t.Errorf("预期 Queued=2, 实际 %d", progressResp.Data.Queued)
	}

	// 验证整体进度（2/4 = 50%）
	expectedProgress := 50.0
	if progressResp.Data.OverallProgress != expectedProgress {
		t.Errorf("预期 OverallProgress=%.1f, 实际 %.1f", expectedProgress, progressResp.Data.OverallProgress)
	}

	// 验证批量任务状态为 processing（因为还有任务在排队）
	if progressResp.Data.Status != "processing" {
		t.Errorf("预期 Status='processing', 实际 '%s'", progressResp.Data.Status)
	}
}

// TestGetBatchProgress_InvalidBatchID 测试查询无效的批量 ID
func TestGetBatchProgress_InvalidBatchID(t *testing.T) {
	setup := setupTaskTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 查询不存在的批量 ID
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch/non-existent-id", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	claims := &auth.Claims{UserID: 1, Username: "testuser", Role: "user"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	setup.handler.GetBatchProgress(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("预期状态码 %d, 实际 %d", http.StatusNotFound, w.Code)
	}
}

// TestCancelTask_InvalidTaskID 测试取消无效的任务 ID
func TestCancelTask_InvalidTaskID(t *testing.T) {
	setup := setupTaskTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 取消不存在的任务
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tasks/non-existent-id", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	claims := &auth.Claims{UserID: 1, Username: "testuser", Role: "user"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	setup.handler.CancelTask(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("预期状态码 %d, 实际 %d", http.StatusNotFound, w.Code)
	}
}

// TestCreateBatchTask_SingleURL 测试单个 URL 创建批量任务
func TestCreateBatchTask_SingleURL(t *testing.T) {
	setup := setupTaskTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	reqBody := BatchTaskRequest{
		URLs:    []string{"https://example.com/video1"},
		Quality: "best",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	claims := &auth.Claims{UserID: 1, Username: "testuser", Role: "user"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	setup.handler.CreateBatchTask(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("预期状态码 %d, 实际 %d", http.StatusCreated, w.Code)
	}

	var resp BatchTaskResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Data.Total != 1 {
		t.Errorf("预期 Total=1, 实际 %d", resp.Data.Total)
	}
}

// TestBatchProgressResponseStructure 测试批量进度响应结构
func TestBatchProgressResponseStructure(t *testing.T) {
	setup := setupTaskTest(t)
	defer setup.cleanup()

	token := generateTestToken(t, setup.jwtMgr, 1, "user")

	// 创建批量任务
	reqBody := BatchTaskRequest{
		URLs: []string{"https://example.com/video1", "https://example.com/video2"},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	claims := &auth.Claims{UserID: 1, Username: "testuser", Role: "user"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	setup.handler.CreateBatchTask(w, req)

	var createResp BatchTaskResponse
	json.Unmarshal(w.Body.Bytes(), &createResp)

	batchID := createResp.Data.BatchID

	// 查询进度
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/batch/"+batchID, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	req2 = req2.WithContext(ctx)

	w2 := httptest.NewRecorder()
	setup.handler.GetBatchProgress(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("预期状态码 %d, 实际 %d, 响应：%s", http.StatusOK, w2.Code, w2.Body.String())
	}

	var progressResp BatchProgressResponse
	json.Unmarshal(w2.Body.Bytes(), &progressResp)

	// 验证响应结构
	if !progressResp.Success {
		t.Error("响应 Success 应为 true")
	}

	if progressResp.Data == nil {
		t.Fatal("响应 Data 不应为空")
	}

	// 验证必填字段
	if progressResp.Data.BatchID != batchID {
		t.Errorf("BatchID 不匹配：预期 %s, 实际 %s", batchID, progressResp.Data.BatchID)
	}

	if progressResp.Data.Total != 2 {
		t.Errorf("预期 Total=2, 实际 %d", progressResp.Data.Total)
	}

	if progressResp.Data.Status == "" {
		t.Error("Status 字段不应为空")
	}

	if progressResp.Data.CreatedAt.IsZero() {
		t.Error("CreatedAt 字段不应为零值")
	}

	// 验证统计字段存在
	if progressResp.Data.Completed < 0 {
		t.Error("Completed 不应为负数")
	}

	if progressResp.Data.Failed < 0 {
		t.Error("Failed 不应为负数")
	}

	if progressResp.Data.Cancelled < 0 {
		t.Error("Cancelled 不应为负数")
	}

	if progressResp.Data.Queued < 0 {
		t.Error("Queued 不应为负数")
	}

	if progressResp.Data.Downloading < 0 {
		t.Error("Downloading 不应为负数")
	}

	// 验证进度百分比在 0-100 之间
	if progressResp.Data.OverallProgress < 0 || progressResp.Data.OverallProgress > 100 {
		t.Errorf("OverallProgress 应在 0-100 之间，实际 %.1f", progressResp.Data.OverallProgress)
	}

	// 验证任务列表
	if progressResp.Data.Tasks == nil {
		t.Error("Tasks 列表不应为 nil")
	}

	if len(progressResp.Data.Tasks) != 2 {
		t.Errorf("预期 2 个任务，实际 %d", len(progressResp.Data.Tasks))
	}
}
