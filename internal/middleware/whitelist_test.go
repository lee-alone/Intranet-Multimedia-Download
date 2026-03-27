package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewWhitelistManager(t *testing.T) {
	domains := []string{"bilibili.com", "youtube.com", "www.qq.com"}
	wm := NewWhitelistManager(domains)

	if wm == nil {
		t.Fatal("NewWhitelistManager returned nil")
	}

	if len(wm.whitelist) != 3 {
		t.Errorf("Expected 3 domains, got %d", len(wm.whitelist))
	}
}

func TestWhitelistManager_AddDomain(t *testing.T) {
	wm := NewWhitelistManager([]string{})
	wm.AddDomain("bilibili.com")

	if !wm.IsAllowed("bilibili.com") {
		t.Error("Expected bilibili.com to be allowed")
	}
}

func TestWhitelistManager_AddDomain_Normalizes(t *testing.T) {
	wm := NewWhitelistManager([]string{})
	
	// 测试 www. 前缀处理
	wm.AddDomain("www.bilibili.com")
	if !wm.IsAllowed("bilibili.com") {
		t.Error("Expected bilibili.com to be allowed after adding www.bilibili.com")
	}

	// 测试大小写处理
	wm.AddDomain("YouTube.COM")
	if !wm.IsAllowed("youtube.com") {
		t.Error("Expected youtube.com to be allowed after adding YouTube.COM")
	}
}

func TestWhitelistManager_RemoveDomain(t *testing.T) {
	wm := NewWhitelistManager([]string{"bilibili.com", "youtube.com"})
	wm.RemoveDomain("bilibili.com")

	if wm.IsAllowed("bilibili.com") {
		t.Error("Expected bilibili.com to be removed from whitelist")
	}

	if !wm.IsAllowed("youtube.com") {
		t.Error("Expected youtube.com to still be in whitelist")
	}
}

func TestWhitelistManager_IsAllowed(t *testing.T) {
	domains := []string{"bilibili.com", "youtube.com", "v.qq.com"}
	wm := NewWhitelistManager(domains)

	tests := []struct {
		domain   string
		expected bool
	}{
		{"bilibili.com", true},
		{"www.bilibili.com", true},
		{"BILIBILI.COM", true},
		{"youtube.com", true},
		{"v.qq.com", true},
		// qq.com 不在白名单中，因为 v.qq.com 和 qq.com 是不同的域名
		{"qq.com", false},
		{"google.com", false},
		{"example.com", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			result := wm.IsAllowed(tt.domain)
			if result != tt.expected {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.domain, result, tt.expected)
			}
		})
	}
}

func TestWhitelistManager_GetDomains(t *testing.T) {
	domains := []string{"bilibili.com", "youtube.com"}
	wm := NewWhitelistManager(domains)

	result := wm.GetDomains()
	if len(result) != 2 {
		t.Errorf("Expected 2 domains, got %d", len(result))
	}

	// 检查域名是否存在（不考虑顺序）
	domainMap := make(map[string]bool)
	for _, d := range result {
		domainMap[d] = true
	}

	if !domainMap["bilibili.com"] {
		t.Error("Expected bilibili.com in result")
	}
	if !domainMap["youtube.com"] {
		t.Error("Expected youtube.com in result")
	}
}

func TestWhitelistManager_UpdateDomains(t *testing.T) {
	wm := NewWhitelistManager([]string{"bilibili.com", "youtube.com"})
	
	// 更新为新的域名列表
	newDomains := []string{"v.qq.com", "iqiyi.com"}
	wm.UpdateDomains(newDomains)

	if len(wm.whitelist) != 2 {
		t.Errorf("Expected 2 domains after update, got %d", len(wm.whitelist))
	}

	if wm.IsAllowed("bilibili.com") {
		t.Error("Expected bilibili.com to be removed after update")
	}

	if !wm.IsAllowed("v.qq.com") {
		t.Error("Expected v.qq.com to be added after update")
	}
}

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"bilibili.com", "bilibili.com"},
		{"www.bilibili.com", "bilibili.com"},
		{"BILIBILI.COM", "bilibili.com"},
		{"WWW.YouTube.COM", "youtube.com"},
		{"  bilibili.com  ", "bilibili.com"},
		{".bilibili.com.", "bilibili.com"},
		{"", ""},
		{"   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeDomain(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeDomain(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name:    "https with www",
			url:     "https://www.bilibili.com/video/123",
			want:    "www.bilibili.com",
			wantErr: false,
		},
		{
			name:    "https without www",
			url:     "https://youtube.com/watch?v=abc",
			want:    "youtube.com",
			wantErr: false,
		},
		{
			name:    "http protocol",
			url:     "http://v.qq.com/x/cover/123",
			want:    "v.qq.com",
			wantErr: false,
		},
		{
			name:    "without protocol",
			url:     "bilibili.com/video/123",
			want:    "bilibili.com",
			wantErr: false,
		},
		{
			name:    "invalid URL",
			url:     "not-a-valid-url-at-all",
			want:    "not-a-valid-url-at-all",
			wantErr: false, // url.Parse 会尝试解析，不一定报错
		},
		{
			name:    "URL with port",
			url:     "https://localhost:8080/api",
			want:    "localhost",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractDomain(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractDomain() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ExtractDomain() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	wm := NewWhitelistManager([]string{"bilibili.com", "youtube.com", "v.qq.com"})

	tests := []struct {
		name    string
		url     string
		wantErr bool
		errCode string
	}{
		{
			name:    "valid bilibili",
			url:     "https://www.bilibili.com/video/BV123",
			wantErr: false,
			errCode: "",
		},
		{
			name:    "valid youtube",
			url:     "https://youtube.com/watch?v=abc123",
			wantErr: false,
			errCode: "",
		},
		{
			name:    "valid qq",
			url:     "https://v.qq.com/x/cover/xyz",
			wantErr: false,
			errCode: "",
		},
		{
			name:    "invalid domain",
			url:     "https://google.com/video",
			wantErr: true,
			errCode: "E401",
		},
		{
			name:    "invalid URL format",
			url:     "not-a-url",
			wantErr: true,
			errCode: "E401", // 无法解析域名时返回 E401
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(wm, tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				urlErr, ok := err.(*URLValidationError)
				if !ok {
					t.Errorf("Expected URLValidationError, got %T", err)
					return
				}
				if urlErr.Code != tt.errCode {
					t.Errorf("Expected error code %q, got %q", tt.errCode, urlErr.Code)
				}
			}
		})
	}
}

func TestURLValidationError_Error(t *testing.T) {
	err := &URLValidationError{
		Code:    "E401",
		Message: "该网站不在允许下载范围内",
	}

	expected := "该网站不在允许下载范围内"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestWriteError(t *testing.T) {
	recorder := httptest.NewRecorder()
	
	WriteError(recorder, http.StatusForbidden, "E401", "该网站不在允许下载范围内")

	if recorder.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}

	body := recorder.Body.String()
	expectedSubstrings := []string{`"success":false`, `"error":"该网站不在允许下载范围内"`, `"code":"E401"`}
	for _, substr := range expectedSubstrings {
		if !contains(body, substr) {
			t.Errorf("Expected body to contain %q, got %q", substr, body)
		}
	}
}

func TestWriteURLValidationError(t *testing.T) {
	recorder := httptest.NewRecorder()
	
	err := &URLValidationError{
		Code:    "E401",
		Message: "该网站不在允许下载范围内",
		Domain:  "google.com",
	}

	WriteURLValidationError(recorder, err)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}

	body := recorder.Body.String()
	if !contains(body, `"code":"E401"`) {
		t.Errorf("Expected body to contain error code, got %q", body)
	}
}

func TestWhitelistMiddleware(t *testing.T) {
	wm := NewWhitelistManager([]string{"bilibili.com"})
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := WhitelistMiddleware(wm)(handler)
	recorder := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	middleware.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", recorder.Code)
	}
}

// contains 检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
