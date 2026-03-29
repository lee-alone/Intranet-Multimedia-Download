// Package auth 提供 SSO 认证功能
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// SSOProvider SSO 提供者类型
type SSOProvider string

const (
	// CASProvider CAS 认证提供者
	CASProvider SSOProvider = "cas"
	// OAuth2Provider OAuth2 认证提供者
	OAuth2Provider SSOProvider = "oauth2"
	// LocalProvider 本地认证提供者
	LocalProvider SSOProvider = "local"
)

// SSOConfig SSO 配置
type SSOConfig struct {
	Provider     SSOProvider   `yaml:"provider"`
	Enabled      bool          `yaml:"enabled"`
	CASURL       string        `yaml:"cas_url"`
	CASService   string        `yaml:"cas_service"`
	OAuth2Config *OAuth2Config `yaml:"oauth2"`
}

// OAuth2Config OAuth2 配置
type OAuth2Config struct {
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	AuthURL      string   `yaml:"auth_url"`
	TokenURL     string   `yaml:"token_url"`
	UserInfoURL  string   `yaml:"user_info_url"`
	Scopes       []string `yaml:"scopes"`
	RedirectURL  string   `yaml:"redirect_url"`
}

// SSOClient SSO 客户端
type SSOClient struct {
	config          *SSOConfig
	httpClient      *http.Client
	mu              sync.RWMutex
	degraded        bool
	degradedAt      time.Time
	degradedTimeout time.Duration
	probeInProgress bool
	db              *sql.DB // 用于存储 state 到 sso_sessions 表
}

// SSOResult SSO 认证结果
type SSOResult struct {
	Success   bool
	UserID    string
	Username  string
	Email     string
	Provider  string
	ErrorCode string
	ErrorMsg  string
}

// SSOSession SSO 会话
type SSOSession struct {
	ID          string    `json:"id"`
	UserID      int       `json:"user_id,omitempty"`
	Provider    string    `json:"provider"`
	SSOUserID   string    `json:"sso_user_id,omitempty"`
	SSOEmail    string    `json:"sso_email,omitempty"`
	SSOUsername string    `json:"sso_username,omitempty"`
	State       string    `json:"state"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// NewSSOClient 创建 SSO 客户端
func NewSSOClient(config *SSOConfig) *SSOClient {
	return &SSOClient{
		config: config,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		degraded:        false,
		degradedTimeout: 5 * time.Minute,
	}
}

// SetDB 设置数据库连接（用于存储 state）
func (c *SSOClient) SetDB(db *sql.DB) {
	c.db = db
}

// generateState 生成 OAuth2/CAS state 参数（防 CSRF）
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// StoreState 存储 State 到数据库
func (c *SSOClient) StoreState(state, provider string) error {
	if c.db == nil {
		return nil // 数据库未初始化时跳过
	}

	sessionID := fmt.Sprintf("sso_%s_%s", provider, state[:8])
	expiresAt := time.Now().Add(10 * time.Minute)

	_, err := c.db.Exec(`
		INSERT INTO sso_sessions (id, state, expires_at, created_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET state=excluded.state, expires_at=excluded.expires_at
	`, sessionID, state, expiresAt)

	return err
}

// ValidateState 验证 State 参数
func (c *SSOClient) ValidateState(state string) bool {
	if c.db == nil {
		return false
	}

	var count int
	err := c.db.QueryRow(`
		SELECT COUNT(*) FROM sso_sessions 
		WHERE state = ? AND expires_at > CURRENT_TIMESTAMP
	`, state).Scan(&count)

	if err != nil {
		return false
	}

	// 验证成功后删除该 state（防止重放攻击）
	if count > 0 {
		c.db.Exec("DELETE FROM sso_sessions WHERE state = ?", state)
	}

	return count > 0
}

// GetCASLoginURL 获取 CAS 登录 URL
func (c *SSOClient) GetCASLoginURL() (string, error) {
	if c.config.Provider != CASProvider || !c.config.Enabled {
		return "", fmt.Errorf("CAS provider not enabled")
	}

	state, err := generateState()
	if err != nil {
		return "", err
	}

	// 存储 state 到数据库
	if err := c.StoreState(state, "cas"); err != nil {
		// 记录日志但不阻止流程
	}

	// 构建 CAS 登录 URL
	casURL := c.config.CASURL
	if c.config.CASService != "" {
		casURL = fmt.Sprintf("%s/login?service=%s&state=%s",
			c.config.CASURL,
			url.QueryEscape(c.config.CASService),
			url.QueryEscape(state))
	} else {
		casURL = fmt.Sprintf("%s/login?state=%s", c.config.CASURL, url.QueryEscape(state))
	}

	return casURL, nil
}

// ValidateCASTicket 验证 CAS Ticket 并获取用户信息
func (c *SSOClient) ValidateCASTicket(ticket, state string) (*SSOResult, error) {
	// 验证 State 参数
	if state != "" && !c.ValidateState(state) {
		return &SSOResult{
			Success:   false,
			ErrorCode: "invalid_state",
			ErrorMsg:  "Invalid or expired state parameter",
		}, nil
	}

	// 检查降级状态并尝试恢复
	if !c.TryRecoverFromDegraded() {
		// 仍在降级中
		return &SSOResult{
			Success:   false,
			ErrorCode: "sso_degraded",
			ErrorMsg:  "SSO service is temporarily unavailable, please try again later",
		}, nil
	}

	// CAS 1.0/2.0 协议验证
	validateURL := fmt.Sprintf("%s/validate?ticket=%s&service=%s",
		c.config.CASURL,
		url.QueryEscape(ticket),
		url.QueryEscape(c.config.CASService),
	)

	resp, err := c.httpClient.Get(validateURL)
	if err != nil {
		c.setDegraded()
		return &SSOResult{
			Success:   false,
			ErrorCode: "sso_unavailable",
			ErrorMsg:  "CAS service is unavailable",
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.setDegraded()
		return &SSOResult{
			Success:   false,
			ErrorCode: "sso_error",
			ErrorMsg:  "Failed to read CAS response",
		}, nil
	}

	// CAS 协议响应：第一行是 yes/no
	response := string(body)
	if len(response) < 4 || response[:3] != "yes" {
		return &SSOResult{
			Success:   false,
			ErrorCode: "invalid_ticket",
			ErrorMsg:  "Invalid CAS ticket",
		}, nil
	}

	// 解析用户名（第二行）
	lines := splitLines(response)
	username := ""
	if len(lines) >= 2 {
		username = lines[1]
	}

	return &SSOResult{
		Success:  true,
		UserID:   username,
		Username: username,
		Provider: string(CASProvider),
	}, nil
}

// GetOAuth2LoginURL 获取 OAuth2 登录 URL
func (c *SSOClient) GetOAuth2LoginURL() (string, string, error) {
	if c.config.Provider != OAuth2Provider || !c.config.Enabled {
		return "", "", fmt.Errorf("OAuth2 provider not enabled")
	}

	if c.config.OAuth2Config == nil {
		return "", "", fmt.Errorf("OAuth2 config is nil")
	}

	state, err := generateState()
	if err != nil {
		return "", "", err
	}

	// 存储 state 到数据库
	if err := c.StoreState(state, "oauth2"); err != nil {
		// 记录日志但不阻止流程
	}

	// 构建 OAuth2 授权 URL
	authURL := c.config.OAuth2Config.AuthURL
	authURL += fmt.Sprintf("?client_id=%s", url.QueryEscape(c.config.OAuth2Config.ClientID))
	authURL += fmt.Sprintf("&redirect_uri=%s", url.QueryEscape(c.config.OAuth2Config.RedirectURL))
	authURL += "&response_type=code"
	authURL += fmt.Sprintf("&scope=%s", url.QueryEscape(joinStrings(c.config.OAuth2Config.Scopes, " ")))
	authURL += fmt.Sprintf("&state=%s", url.QueryEscape(state))

	return authURL, state, nil
}

// ExchangeOAuth2Code 交换 OAuth2 Code 获取 Token
func (c *SSOClient) ExchangeOAuth2Code(code string) (string, error) {
	if c.config.OAuth2Config == nil {
		return "", fmt.Errorf("OAuth2 config is nil")
	}

	data := url.Values{}
	data.Set("client_id", c.config.OAuth2Config.ClientID)
	data.Set("client_secret", c.config.OAuth2Config.ClientSecret)
	data.Set("code", code)
	data.Set("grant_type", "authorization_code")
	data.Set("redirect_uri", c.config.OAuth2Config.RedirectURL)

	resp, err := c.httpClient.PostForm(c.config.OAuth2Config.TokenURL, data)
	if err != nil {
		c.setDegraded()
		return "", fmt.Errorf("failed to exchange code: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	if result.Error != "" {
		return "", fmt.Errorf("token exchange error: %s", result.Error)
	}

	return result.AccessToken, nil
}

// GetOAuth2UserInfo 获取 OAuth2 用户信息
func (c *SSOClient) GetOAuth2UserInfo(accessToken string) (*SSOResult, error) {
	if c.config.OAuth2Config == nil {
		return nil, fmt.Errorf("OAuth2 config is nil")
	}

	req, err := http.NewRequest("GET", c.config.OAuth2Config.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.setDegraded()
		return &SSOResult{
			Success:   false,
			ErrorCode: "sso_unavailable",
			ErrorMsg:  "OAuth2 service is unavailable",
		}, nil
	}
	defer resp.Body.Close()

	var userInfo struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Email    string `json:"email"`
		Name     string `json:"name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}

	username := userInfo.Username
	if username == "" {
		username = userInfo.Name
	}

	return &SSOResult{
		Success:  true,
		UserID:   userInfo.ID,
		Username: username,
		Email:    userInfo.Email,
		Provider: string(OAuth2Provider),
	}, nil
}

// setDegraded 设置降级状态
func (c *SSOClient) setDegraded() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.degraded {
		c.degraded = true
		c.degradedAt = time.Now()
	}
}

// IsDegraded 检查是否处于降级状态
func (c *SSOClient) IsDegraded() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.degraded
}

// TryRecoverFromDegraded 尝试从降级状态恢复
func (c *SSOClient) TryRecoverFromDegraded() bool {
	c.mu.Lock()
	if !c.degraded {
		c.mu.Unlock()
		return true
	}

	if time.Since(c.degradedAt) < c.degradedTimeout {
		c.mu.Unlock()
		return false
	}

	if c.probeInProgress {
		c.mu.Unlock()
		return false
	}
	c.probeInProgress = true
	c.mu.Unlock()

	recovered := false
	switch c.config.Provider {
	case CASProvider:
		recovered = c.probeCAS()
	case OAuth2Provider:
		recovered = c.probeOAuth2()
	}

	c.mu.Lock()
	c.probeInProgress = false
	if recovered {
		c.degraded = false
	}
	c.mu.Unlock()

	return recovered
}

// probeCAS 探测 CAS 服务
func (c *SSOClient) probeCAS() bool {
	if c.config.CASURL == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.config.CASURL, nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode < 500
}

// probeOAuth2 探测 OAuth2 服务
func (c *SSOClient) probeOAuth2() bool {
	if c.config.OAuth2Config == nil || c.config.OAuth2Config.AuthURL == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.config.OAuth2Config.AuthURL, nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode < 500
}

// GetStatus 获取 SSO 状态
func (c *SSOClient) GetStatus() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := map[string]interface{}{
		"enabled":  c.config.Enabled,
		"provider": c.config.Provider,
	}

	if c.degraded {
		status["degraded"] = true
		status["degraded_since"] = c.degradedAt
		status["degraded_timeout"] = c.degradedTimeout
	}

	return status
}

// 辅助函数
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
