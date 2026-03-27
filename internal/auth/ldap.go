// Package auth 提供 LDAP 认证功能
package auth

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// LDAPClient 提供 LDAP 认证客户端
type LDAPClient struct {
	URL      string
	BindDN   string
	Password string
	BaseDN   string
	Timeout  time.Duration
	Enabled  bool
}

// NewLDAPClient 创建新的 LDAP 客户端
func NewLDAPClient(url, bindDN, password, baseDN string, timeout int, enabled bool) *LDAPClient {
	return &LDAPClient{
		URL:      url,
		BindDN:   bindDN,
		Password: password,
		BaseDN:   baseDN,
		Timeout:  time.Duration(timeout) * time.Second,
		Enabled:  enabled,
	}
}

// LDAPAuthResult LDAP 认证结果
type LDAPAuthResult struct {
	Success bool
	UID     string
	Email   string
	CN      string
	Error   error
}

// Authenticate 使用 LDAP 进行用户认证
func (c *LDAPClient) Authenticate(username, password string) *LDAPAuthResult {
	result := &LDAPAuthResult{}

	if !c.Enabled {
		result.Error = errors.New("LDAP is not enabled")
		return result
	}

	// 连接 LDAP 服务器
	conn, err := c.connect()
	if err != nil {
		result.Error = fmt.Errorf("failed to connect to LDAP server: %w", err)
		return result
	}
	defer conn.Close()

	// 绑定管理员账号进行搜索
	if err := conn.Bind(c.BindDN, c.Password); err != nil {
		result.Error = fmt.Errorf("failed to bind admin account: %w", err)
		return result
	}

	// 搜索用户
	searchRequest := ldap.NewSearchRequest(
		c.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, int(c.Timeout.Seconds()), false,
		fmt.Sprintf("(&(objectClass=posixAccount)(uid=%s))", ldap.EscapeFilter(username)),
		[]string{"uid", "cn", "mail", "dn"},
		nil,
	)

	searchResult, err := conn.Search(searchRequest)
	if err != nil {
		result.Error = fmt.Errorf("failed to search user: %w", err)
		return result
	}

	if len(searchResult.Entries) == 0 {
		result.Error = errors.New("user not found")
		return result
	}

	if len(searchResult.Entries) > 1 {
		result.Error = errors.New("multiple users found")
		return result
	}

	userEntry := searchResult.Entries[0]
	userDN := userEntry.DN

	// 使用用户凭据绑定验证密码
	if err := conn.Bind(userDN, password); err != nil {
		result.Error = fmt.Errorf("invalid credentials: %w", err)
		return result
	}

	// 认证成功，提取用户信息
	result.Success = true
	result.UID = userEntry.GetAttributeValue("uid")
	result.CN = userEntry.GetAttributeValue("cn")
	result.Email = userEntry.GetAttributeValue("mail")

	return result
}

// connect 连接到 LDAP 服务器
func (c *LDAPClient) connect() (*ldap.Conn, error) {
	// 解析 URL 确定是否使用 TLS
	var conn *ldap.Conn
	var err error

	// 使用 DialURL 连接，超时通过连接后设置控制
	conn, err = ldap.DialURL(c.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to dial LDAP URL: %w", err)
	}

	// 设置连接超时
	if c.Timeout > 0 {
		conn.SetTimeout(c.Timeout)
	}

	return conn, nil
}

// TestConnection 测试 LDAP 连接
func (c *LDAPClient) TestConnection() error {
	if !c.Enabled {
		return errors.New("LDAP is not enabled")
	}

	conn, err := c.connect()
	if err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}
	defer conn.Close()

	// 尝试绑定管理员账号
	if err := conn.Bind(c.BindDN, c.Password); err != nil {
		return fmt.Errorf("bind test failed: %w", err)
	}

	return nil
}

// LDAPAuthHandler LDAP 认证处理器
type LDAPAuthHandler struct {
	client        *LDAPClient
	jwtManager    *JWTManager
	fallbackLocal bool // 是否在 LDAP 失败时降级到本地认证
}

// NewLDAPAuthHandler 创建 LDAP 认证处理器
func NewLDAPAuthHandler(client *LDAPClient, jwtManager *JWTManager, fallbackLocal bool) *LDAPAuthHandler {
	return &LDAPAuthHandler{
		client:        client,
		jwtManager:    jwtManager,
		fallbackLocal: fallbackLocal,
	}
}

// AuthenticateWithFallback 使用 LDAP 认证，失败时可降级到本地认证
func (h *LDAPAuthHandler) AuthenticateWithFallback(username, password string, localAuthFunc func(string, string) (int, string, error)) (*TokenPair, error) {
	// 尝试 LDAP 认证
	if h.client.Enabled {
		result := h.client.Authenticate(username, password)
		if result.Success {
			// LDAP 认证成功，生成令牌
			// 注意：LDAP 用户可能不存在于本地数据库，需要创建或更新
			return h.jwtManager.GenerateToken(0, result.UID, "user")
		}

		// LDAP 认证失败
		log.Printf("LDAP authentication failed: %v", result.Error)

		// 如果不降级，直接返回错误
		if !h.fallbackLocal {
			return nil, fmt.Errorf("LDAP authentication failed: %w", result.Error)
		}

		// 降级到本地认证
		log.Printf("Falling back to local authentication for user: %s", username)
	}

	// 使用本地认证
	userID, role, err := localAuthFunc(username, password)
	if err != nil {
		return nil, err
	}

	return h.jwtManager.GenerateToken(userID, username, role)
}

// StartTLS 启用 TLS 连接
func (c *LDAPClient) StartTLS(conn *ldap.Conn) error {
	// 配置 TLS
	tlsConfig := &tls.Config{
		InsecureSkipVerify: false, // 生产环境应验证证书
	}

	if err := conn.StartTLS(tlsConfig); err != nil {
		return fmt.Errorf("failed to start TLS: %w", err)
	}

	return nil
}
