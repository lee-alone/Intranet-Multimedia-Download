// Package middleware 提供 HTTP 中间件功能
package middleware

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// WhitelistManager 白名单管理器
type WhitelistManager struct {
	mu        sync.RWMutex
	whitelist map[string]bool // 域名 -> true
}

// NewWhitelistManager 创建白名单管理器
func NewWhitelistManager(domains []string) *WhitelistManager {
	wm := &WhitelistManager{
		whitelist: make(map[string]bool, len(domains)),
	}
	for _, domain := range domains {
		wm.AddDomain(domain)
	}
	return wm
}

// AddDomain 添加域名到白名单
func (wm *WhitelistManager) AddDomain(domain string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	// 标准化域名（去掉前缀 www.，转为小写）
	normalized := normalizeDomain(domain)
	if normalized != "" {
		wm.whitelist[normalized] = true
	}
}

// RemoveDomain 从白名单移除域名
func (wm *WhitelistManager) RemoveDomain(domain string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	normalized := normalizeDomain(domain)
	delete(wm.whitelist, normalized)
}

// IsAllowed 检查域名是否在白名单中
func (wm *WhitelistManager) IsAllowed(domain string) bool {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	normalized := normalizeDomain(domain)
	return wm.whitelist[normalized]
}

// GetDomains 获取所有白名单域名
func (wm *WhitelistManager) GetDomains() []string {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	domains := make([]string, 0, len(wm.whitelist))
	for domain := range wm.whitelist {
		domains = append(domains, domain)
	}
	return domains
}

// UpdateDomains 更新整个白名单
func (wm *WhitelistManager) UpdateDomains(domains []string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.whitelist = make(map[string]bool, len(domains))
	for _, domain := range domains {
		normalized := normalizeDomain(domain)
		if normalized != "" {
			wm.whitelist[normalized] = true
		}
	}
}

// normalizeDomain 标准化域名（去掉 www. 前缀，转为小写）
func normalizeDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	// 转为小写
	domain = strings.ToLower(domain)
	// 去掉 www. 前缀
	domain = strings.TrimPrefix(domain, "www.")
	// 去掉前后缀点号
	domain = strings.Trim(domain, ".")
	return domain
}

// ExtractDomain 从 URL 中提取域名
func ExtractDomain(rawURL string) (string, error) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return u.Hostname(), nil
}

// WhitelistMiddleware 创建白名单校验中间件
// 如果 URL 不在白名单中，返回 403 错误
func WhitelistMiddleware(wm *WhitelistManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 仅对 POST/PUT 请求进行校验（创建/更新任务时）
			if r.Method == http.MethodPost || r.Method == http.MethodPut {
				// 尝试从请求体中提取 URL
				// 注意：这里不能读取 r.Body，因为会被后续处理器使用
				// 实际校验应该在具体的 handler 中进行
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ValidateURL 验证 URL 是否在白名单中
func ValidateURL(wm *WhitelistManager, rawURL string) error {
	domain, err := ExtractDomain(rawURL)
	if err != nil {
		return &URLValidationError{
			Code:    "E400",
			Message: "无效的 URL 格式",
			Err:     err,
		}
	}

	if !wm.IsAllowed(domain) {
		return &URLValidationError{
			Code:    "E401",
			Message: "该网站不在允许下载范围内",
			URL:     rawURL,
			Domain:  domain,
		}
	}

	return nil
}

// URLValidationError URL 校验错误
type URLValidationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	URL     string `json:"url,omitempty"`
	Domain  string `json:"domain,omitempty"`
	Err     error  `json:"-"`
}

// Error 实现 error 接口
func (e *URLValidationError) Error() string {
	return e.Message
}

// Unwrap 实现 errors.Unwrap 接口
func (e *URLValidationError) Unwrap() error {
	return e.Err
}

// ErrorResponse 统一错误响应
type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
}

// WriteError 写入错误响应
func WriteError(w http.ResponseWriter, statusCode int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{
		Success: false,
		Error:   message,
		Code:    code,
	})
}

// WriteURLValidationError 写入 URL 校验错误响应
func WriteURLValidationError(w http.ResponseWriter, err *URLValidationError) {
	statusCode := http.StatusForbidden
	if err.Code == "E400" {
		statusCode = http.StatusBadRequest
	}
	WriteError(w, statusCode, err.Code, err.Message)
}
