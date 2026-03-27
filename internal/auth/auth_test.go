// Package auth 提供 JWT 认证和授权功能的测试
package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 测试密钥目录
var testKeyDir string

// TestMain 设置测试环境
func TestMain(m *testing.M) {
	// 创建临时密钥目录
	testKeyDir, _ = os.MkdirTemp("", "auth_test_keys")

	// 生成测试密钥对
	generateTestKeys(testKeyDir)

	// 运行测试
	code := m.Run()

	// 清理
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

func TestNewJWTManager(t *testing.T) {
	tests := []struct {
		name           string
		privateKeyPath string
		publicKeyPath  string
		expiry         int
		refreshExpiry  int
		wantErr        bool
	}{
		{
			name:           "valid keys",
			privateKeyPath: filepath.Join(testKeyDir, "private.pem"),
			publicKeyPath:  filepath.Join(testKeyDir, "public.pem"),
			expiry:         60,
			refreshExpiry:  1440,
			wantErr:        false,
		},
		{
			name:           "invalid private key path",
			privateKeyPath: "/nonexistent/private.pem",
			publicKeyPath:  filepath.Join(testKeyDir, "public.pem"),
			expiry:         60,
			refreshExpiry:  1440,
			wantErr:        true,
		},
		{
			name:           "invalid public key path",
			privateKeyPath: filepath.Join(testKeyDir, "private.pem"),
			publicKeyPath:  "/nonexistent/public.pem",
			expiry:         60,
			refreshExpiry:  1440,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewJWTManager(tt.privateKeyPath, tt.publicKeyPath, tt.expiry, tt.refreshExpiry)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewJWTManager() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestJWTManager_GenerateToken(t *testing.T) {
	manager, err := NewJWTManager(
		filepath.Join(testKeyDir, "private.pem"),
		filepath.Join(testKeyDir, "public.pem"),
		60,
		1440,
	)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	tests := []struct {
		name     string
		userID   int
		username string
		role     string
		wantErr  bool
	}{
		{
			name:     "valid user",
			userID:   1,
			username: "testuser",
			role:     "user",
			wantErr:  false,
		},
		{
			name:     "admin user",
			userID:   2,
			username: "admin",
			role:     "admin",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := manager.GenerateToken(tt.userID, tt.username, tt.role)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if tokens.AccessToken == "" {
					t.Error("AccessToken is empty")
				}
				if tokens.RefreshToken == "" {
					t.Error("RefreshToken is empty")
				}
				if tokens.ExpiresIn <= 0 {
					t.Error("ExpiresIn should be positive")
				}
			}
		})
	}
}

func TestJWTManager_ValidateToken(t *testing.T) {
	manager, err := NewJWTManager(
		filepath.Join(testKeyDir, "private.pem"),
		filepath.Join(testKeyDir, "public.pem"),
		60,
		1440,
	)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	// 生成有效令牌
	tokens, err := manager.GenerateToken(1, "testuser", "user")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	tests := []struct {
		name       string
		token      string
		wantErr    error
		wantUserID int
	}{
		{
			name:       "valid access token",
			token:      tokens.AccessToken,
			wantErr:    nil,
			wantUserID: 1,
		},
		{
			name:    "invalid token",
			token:   "invalid.token.here",
			wantErr: ErrInvalidToken,
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: ErrInvalidToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := manager.ValidateToken(tt.token)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("ValidateToken() expected error %v, got nil", tt.wantErr)
					return
				}
				if err != tt.wantErr {
					t.Errorf("ValidateToken() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("ValidateToken() unexpected error: %v", err)
				return
			}

			if claims.UserID != tt.wantUserID {
				t.Errorf("ValidateToken() userID = %v, want %v", claims.UserID, tt.wantUserID)
			}
		})
	}
}

func TestJWTManager_RefreshAccessToken(t *testing.T) {
	manager, err := NewJWTManager(
		filepath.Join(testKeyDir, "private.pem"),
		filepath.Join(testKeyDir, "public.pem"),
		60,
		1440,
	)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	// 生成令牌
	tokens, err := manager.GenerateToken(1, "testuser", "user")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// 使用刷新令牌获取新令牌
	newTokens, err := manager.RefreshAccessToken(tokens.RefreshToken)
	if err != nil {
		t.Errorf("RefreshAccessToken() error = %v", err)
		return
	}

	if newTokens.AccessToken == "" {
		t.Error("New AccessToken is empty")
	}

	if newTokens.RefreshToken == "" {
		t.Error("New RefreshToken is empty")
	}

	// 验证新令牌
	claims, err := manager.ValidateToken(newTokens.AccessToken)
	if err != nil {
		t.Errorf("Failed to validate new access token: %v", err)
		return
	}

	if claims.UserID != 1 {
		t.Errorf("Claims UserID = %v, want 1", claims.UserID)
	}

	if claims.Username != "testuser" {
		t.Errorf("Claims Username = %v, want testuser", claims.Username)
	}
}

func TestJWTManager_TokenExpiry(t *testing.T) {
	// 创建一个短期令牌管理器（1秒过期）
	manager, err := NewJWTManager(
		filepath.Join(testKeyDir, "private.pem"),
		filepath.Join(testKeyDir, "public.pem"),
		1, // 1 分钟过期
		2, // 2 分钟刷新令牌过期
	)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	tokens, err := manager.GenerateToken(1, "testuser", "user")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// 验证令牌
	claims, err := manager.ValidateToken(tokens.AccessToken)
	if err != nil {
		t.Errorf("Token should be valid, got error: %v", err)
	}

	// 检查过期时间设置正确
	expectedExpiry := time.Now().Add(time.Minute).Unix()
	if claims.ExpiresAt.Time.Unix() > expectedExpiry+5 || claims.ExpiresAt.Time.Unix() < expectedExpiry-5 {
		t.Errorf("Token expiry time not set correctly")
	}
}

func TestClaims_Structure(t *testing.T) {
	claims := Claims{
		UserID:   1,
		Username: "testuser",
		Role:     "admin",
	}

	if claims.UserID != 1 {
		t.Errorf("Claims.UserID = %v, want 1", claims.UserID)
	}

	if claims.Username != "testuser" {
		t.Errorf("Claims.Username = %v, want testuser", claims.Username)
	}

	if claims.Role != "admin" {
		t.Errorf("Claims.Role = %v, want admin", claims.Role)
	}
}

func TestTokenPair_Structure(t *testing.T) {
	pair := TokenPair{
		AccessToken:  "access_token_value",
		RefreshToken: "refresh_token_value",
		ExpiresIn:    3600,
	}

	if pair.AccessToken != "access_token_value" {
		t.Errorf("TokenPair.AccessToken = %v, want access_token_value", pair.AccessToken)
	}

	if pair.RefreshToken != "refresh_token_value" {
		t.Errorf("TokenPair.RefreshToken = %v, want refresh_token_value", pair.RefreshToken)
	}

	if pair.ExpiresIn != 3600 {
		t.Errorf("TokenPair.ExpiresIn = %v, want 3600", pair.ExpiresIn)
	}
}

func TestErrorDefinitions(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{
			name: "ErrInvalidToken",
			err:  ErrInvalidToken,
			msg:  "invalid token",
		},
		{
			name: "ErrExpiredToken",
			err:  ErrExpiredToken,
			msg:  "token expired",
		},
		{
			name: "ErrMissingKey",
			err:  ErrMissingKey,
			msg:  "missing signing key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.msg {
				t.Errorf("%s error message = %v, want %v", tt.name, tt.err.Error(), tt.msg)
			}
		})
	}
}
