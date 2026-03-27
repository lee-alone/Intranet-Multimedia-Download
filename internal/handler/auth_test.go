// Package handler 提供 HTTP 请求处理器的测试
package handler

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/auth"
	"github.com/campus/collector/internal/database"
	_ "github.com/mattn/go-sqlite3"
)

var (
	testDB          *sql.DB
	testJWTMgr      *auth.JWTManager
	testKeyDir      string
	testLDAP        *auth.LDAPClient
	testAuditLogger *audit.Logger
)

// TestMain 设置测试环境
func TestMain(m *testing.M) {
	// 创建临时密钥目录
	testKeyDir, _ = os.MkdirTemp("", "handler_test_keys")
	generateTestKeys(testKeyDir)

	// 初始化数据库（使用 database 包）
	dbCfg := &database.Config{
		Path:     ":memory:",
		WALMode:  false,
		MaxConns: 5,
	}
	if err := database.Init(dbCfg); err != nil {
		panic(err)
	}
	testDB = database.Get()

	// 创建测试表
	createTestTables(testDB)

	// 创建 JWT 管理器
	var err error
	testJWTMgr, err = auth.NewJWTManager(
		filepath.Join(testKeyDir, "private.pem"),
		filepath.Join(testKeyDir, "public.pem"),
		60,
		1440,
	)
	if err != nil {
		panic(err)
	}

	// 创建 LDAP 客户端（禁用状态）
	testLDAP = auth.NewLDAPClient("", "", "", "", 0, false)

	// 创建审计日志记录器（禁用文件日志）
	testAuditLogger, err = audit.NewLogger(os.TempDir(), false)
	if err != nil {
		panic(err)
	}

	// 运行测试
	code := m.Run()

	// 清理
	testAuditLogger.Close()
	database.Close()
	os.RemoveAll(testKeyDir)

	os.Exit(code)
}

// generateTestKeys 生成测试用的 RSA 密钥对
func generateTestKeys(dir string) {
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)

	// 保存私钥
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateKeyBytes}
	privateKeyFile, _ := os.Create(filepath.Join(dir, "private.pem"))
	pem.Encode(privateKeyFile, privateKeyPEM)
	privateKeyFile.Close()

	// 保存公钥
	publicKeyBytes, _ := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	publicKeyPEM := &pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyBytes}
	publicKeyFile, _ := os.Create(filepath.Join(dir, "public.pem"))
	pem.Encode(publicKeyFile, publicKeyPEM)
	publicKeyFile.Close()
}

// createTestTables 创建测试数据库表
func createTestTables(db *sql.DB) {
	// 创建用户表
	db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			email TEXT,
			role TEXT DEFAULT 'user',
			auth_type TEXT DEFAULT 'local',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)

	// 创建审计日志表
	db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			action TEXT NOT NULL,
			resource_type TEXT,
			resource_id INTEGER,
			ip_address TEXT,
			user_agent TEXT,
			detail TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)

	// 创建刷新令牌表
	db.Exec(`
		CREATE TABLE IF NOT EXISTS refresh_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
}

func TestNewAuthHandler(t *testing.T) {
	handler := NewAuthHandler(testDB, testJWTMgr, testLDAP, testAuditLogger)

	if handler == nil {
		t.Error("NewAuthHandler should not return nil")
	}

	if handler.db != testDB {
		t.Error("DB not set correctly")
	}

	if handler.jwtMgr != testJWTMgr {
		t.Error("JWTManager not set correctly")
	}
}

func TestAuthHandler_Register(t *testing.T) {
	handler := NewAuthHandler(testDB, testJWTMgr, testLDAP, testAuditLogger)

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
	}{
		{
			name: "valid registration",
			body: map[string]interface{}{
				"username": "testuser1",
				"password": "password123",
				"email":    "test1@example.com",
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "short username",
			body: map[string]interface{}{
				"username": "ab",
				"password": "password123",
				"email":    "test2@example.com",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "short password",
			body: map[string]interface{}{
				"username": "testuser3",
				"password": "short",
				"email":    "test3@example.com",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "duplicate username",
			body: map[string]interface{}{
				"username": "testuser1",
				"password": "password123",
				"email":    "test4@example.com",
			},
			wantStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/register", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.Register(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Register() status = %v, want %v", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestAuthHandler_Login(t *testing.T) {
	handler := NewAuthHandler(testDB, testJWTMgr, testLDAP, testAuditLogger)

	// 先注册一个用户
	registerBody := map[string]interface{}{
		"username": "loginuser",
		"password": "password123",
		"email":    "login@example.com",
	}
	bodyBytes, _ := json.Marshal(registerBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/register", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.Register(rec, req)

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
	}{
		{
			name: "valid login",
			body: map[string]interface{}{
				"username": "loginuser",
				"password": "password123",
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "invalid password",
			body: map[string]interface{}{
				"username": "loginuser",
				"password": "wrongpassword",
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "nonexistent user",
			body: map[string]interface{}{
				"username": "nonexistent",
				"password": "password123",
			},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/login", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.Login(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Login() status = %v, want %v", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var response LoginResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Errorf("Failed to parse response: %v", err)
					return
				}
				if !response.Success {
					t.Error("Login should succeed")
				}
				if response.Data == nil {
					t.Error("Token data should not be nil")
				}
			}
		})
	}
}

func TestAuthHandler_RefreshToken(t *testing.T) {
	handler := NewAuthHandler(testDB, testJWTMgr, testLDAP, testAuditLogger)

	// 注册并登录获取令牌
	registerBody := map[string]interface{}{
		"username": "refreshuser",
		"password": "password123",
		"email":    "refresh@example.com",
	}
	bodyBytes, _ := json.Marshal(registerBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/register", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.Register(rec, req)

	// 登录
	loginBody := map[string]interface{}{
		"username": "refreshuser",
		"password": "password123",
	}
	bodyBytes, _ = json.Marshal(loginBody)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/login", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.Login(rec, req)

	var loginResponse LoginResponse
	json.Unmarshal(rec.Body.Bytes(), &loginResponse)

	tests := []struct {
		name         string
		refreshToken string
		wantStatus   int
	}{
		{
			name:         "valid refresh token",
			refreshToken: loginResponse.Data.RefreshToken,
			wantStatus:   http.StatusOK,
		},
		{
			name:         "invalid refresh token",
			refreshToken: "invalid.token.here",
			wantStatus:   http.StatusUnauthorized,
		},
		{
			name:         "empty refresh token",
			refreshToken: "",
			wantStatus:   http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]interface{}{
				"refresh_token": tt.refreshToken,
			}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/token/refresh", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.RefreshToken(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("RefreshToken() status = %v, want %v", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestAuthHandler_Logout(t *testing.T) {
	handler := NewAuthHandler(testDB, testJWTMgr, testLDAP, testAuditLogger)

	// 注册并登录
	registerBody := map[string]interface{}{
		"username": "logoutuser",
		"password": "password123",
		"email":    "logout@example.com",
	}
	bodyBytes, _ := json.Marshal(registerBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/register", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.Register(rec, req)

	// 登录
	loginBody := map[string]interface{}{
		"username": "logoutuser",
		"password": "password123",
	}
	bodyBytes, _ = json.Marshal(loginBody)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/login", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.Login(rec, req)

	var loginResponse LoginResponse
	json.Unmarshal(rec.Body.Bytes(), &loginResponse)

	// 使用认证中间件测试登出
	req = httptest.NewRequest(http.MethodPost, "/api/v1/logout", nil)
	req.Header.Set("Authorization", "Bearer "+loginResponse.Data.AccessToken)
	rec = httptest.NewRecorder()

	// 使用中间件
	middleware := AuthMiddleware(testJWTMgr)
	middleware(http.HandlerFunc(handler.Logout)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Logout() status = %v, want %v", rec.Code, http.StatusOK)
	}
}

func TestAuthMiddleware(t *testing.T) {
	// 生成测试令牌
	tokens, err := testJWTMgr.GenerateToken(1, "testuser", "user")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "valid token",
			authHeader: "Bearer " + tokens.AccessToken,
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing authorization header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid authorization format",
			authHeader: "InvalidFormat " + tokens.AccessToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid token",
			authHeader: "Bearer invalid.token.here",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建一个简单的处理器来测试中间件
			finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"success":true}`))
			})

			middleware := AuthMiddleware(testJWTMgr)
			handler := middleware(finalHandler)

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("AuthMiddleware() status = %v, want %v", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	data := map[string]string{"message": "test"}

	writeJSON(rec, http.StatusOK, data)

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Error("Content-Type should be application/json")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %v, want %v", rec.Code, http.StatusOK)
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()

	writeError(rec, http.StatusBadRequest, "test error")

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Error("Content-Type should be application/json")
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %v, want %v", rec.Code, http.StatusBadRequest)
	}

	var response map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &response)

	if response["success"] != false {
		t.Error("success should be false")
	}

	if response["error"] != "test error" {
		t.Errorf("error = %v, want 'test error'", response["error"])
	}
}
