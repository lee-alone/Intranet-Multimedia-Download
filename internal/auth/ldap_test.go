// Package auth 提供 LDAP 认证功能的测试
package auth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNewLDAPClient(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		bindDN   string
		password string
		baseDN   string
		timeout  int
		enabled  bool
	}{
		{
			name:     "enabled client",
			url:      "ldap://localhost:389",
			bindDN:   "cn=admin,dc=example,dc=com",
			password: "password",
			baseDN:   "ou=users,dc=example,dc=com",
			timeout:  10,
			enabled:  true,
		},
		{
			name:     "disabled client",
			url:      "",
			bindDN:   "",
			password: "",
			baseDN:   "",
			timeout:  0,
			enabled:  false,
		},
		{
			name:     "ldaps client",
			url:      "ldaps://localhost:636",
			bindDN:   "cn=admin,dc=example,dc=com",
			password: "password",
			baseDN:   "ou=users,dc=example,dc=com",
			timeout:  30,
			enabled:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLDAPClient(tt.url, tt.bindDN, tt.password, tt.baseDN, tt.timeout, tt.enabled)

			if client.URL != tt.url {
				t.Errorf("URL = %v, want %v", client.URL, tt.url)
			}
			if client.BindDN != tt.bindDN {
				t.Errorf("BindDN = %v, want %v", client.BindDN, tt.bindDN)
			}
			if client.Password != tt.password {
				t.Errorf("Password = %v, want %v", client.Password, tt.password)
			}
			if client.BaseDN != tt.baseDN {
				t.Errorf("BaseDN = %v, want %v", client.BaseDN, tt.baseDN)
			}
			if client.Timeout != time.Duration(tt.timeout)*time.Second {
				t.Errorf("Timeout = %v, want %v", client.Timeout, time.Duration(tt.timeout)*time.Second)
			}
			if client.Enabled != tt.enabled {
				t.Errorf("Enabled = %v, want %v", client.Enabled, tt.enabled)
			}
		})
	}
}

func TestLDAPClient_Authenticate_Disabled(t *testing.T) {
	client := NewLDAPClient("", "", "", "", 0, false)

	result := client.Authenticate("testuser", "testpass")

	if result.Success {
		t.Error("Authenticate should fail when LDAP is disabled")
	}
	if result.Error == nil {
		t.Error("Error should be set when LDAP is disabled")
	}
	if result.Error.Error() != "LDAP is not enabled" {
		t.Errorf("Error message = %v, want 'LDAP is not enabled'", result.Error.Error())
	}
}

func TestLDAPClient_TestConnection_Disabled(t *testing.T) {
	client := NewLDAPClient("", "", "", "", 0, false)

	err := client.TestConnection()

	if err == nil {
		t.Error("TestConnection should fail when LDAP is disabled")
	}
	if err.Error() != "LDAP is not enabled" {
		t.Errorf("Error message = %v, want 'LDAP is not enabled'", err.Error())
	}
}

func TestLDAPAuthResult_Structure(t *testing.T) {
	result := &LDAPAuthResult{
		Success: true,
		UID:     "testuser",
		Email:   "test@example.com",
		CN:      "Test User",
		Error:   nil,
	}

	if !result.Success {
		t.Error("Success should be true")
	}
	if result.UID != "testuser" {
		t.Errorf("UID = %v, want testuser", result.UID)
	}
	if result.Email != "test@example.com" {
		t.Errorf("Email = %v, want test@example.com", result.Email)
	}
	if result.CN != "Test User" {
		t.Errorf("CN = %v, want 'Test User'", result.CN)
	}
	if result.Error != nil {
		t.Errorf("Error should be nil, got %v", result.Error)
	}
}

func TestLDAPAuthHandler_New(t *testing.T) {
	client := NewLDAPClient("ldap://localhost:389", "cn=admin", "pass", "ou=users", 10, true)

	// 创建测试用的 JWT 管理器
	manager, err := NewJWTManager(
		filepath.Join(testKeyDir, "private.pem"),
		filepath.Join(testKeyDir, "public.pem"),
		60,
		1440,
	)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	handler := NewLDAPAuthHandler(client, manager, true)

	if handler.client != client {
		t.Error("Client not set correctly")
	}
	if handler.jwtManager != manager {
		t.Error("JWTManager not set correctly")
	}
	if !handler.fallbackLocal {
		t.Error("FallbackLocal should be true")
	}
}

func TestLDAPAuthHandler_New_NoFallback(t *testing.T) {
	client := NewLDAPClient("ldap://localhost:389", "cn=admin", "pass", "ou=users", 10, true)

	manager, err := NewJWTManager(
		filepath.Join(testKeyDir, "private.pem"),
		filepath.Join(testKeyDir, "public.pem"),
		60,
		1440,
	)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	handler := NewLDAPAuthHandler(client, manager, false)

	if handler.fallbackLocal {
		t.Error("FallbackLocal should be false")
	}
}

// MockLDAPClient 是用于测试的 Mock LDAP 客户端
type MockLDAPClient struct {
	Enabled       bool
	AuthResult    *LDAPAuthResult
	ConnectError  error
	TestConnError error
}

// Authenticate 模拟 LDAP 认证
func (m *MockLDAPClient) Authenticate(username, password string) *LDAPAuthResult {
	if m.AuthResult != nil {
		return m.AuthResult
	}
	return &LDAPAuthResult{
		Success: false,
		Error:   ErrInvalidToken,
	}
}

// TestConnection 模拟连接测试
func (m *MockLDAPClient) TestConnection() error {
	return m.TestConnError
}

func TestLDAPClient_Timeout(t *testing.T) {
	// 测试超时设置
	client := NewLDAPClient(
		"ldap://localhost:389",
		"cn=admin,dc=example,dc=com",
		"password",
		"ou=users,dc=example,dc=com",
		30, // 30 秒超时
		true,
	)

	expectedTimeout := 30 * time.Second
	if client.Timeout != expectedTimeout {
		t.Errorf("Timeout = %v, want %v", client.Timeout, expectedTimeout)
	}
}

func TestLDAPClient_ZeroTimeout(t *testing.T) {
	// 测试零超时
	client := NewLDAPClient(
		"ldap://localhost:389",
		"cn=admin,dc=example,dc=com",
		"password",
		"ou=users,dc=example,dc=com",
		0, // 无超时
		true,
	)

	if client.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0", client.Timeout)
	}
}
