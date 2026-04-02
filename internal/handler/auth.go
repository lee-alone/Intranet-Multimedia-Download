// Package handler 提供 HTTP 请求处理器
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

// contextKey 是上下文中存储 claims 的键类型
type contextKey string

// ClaimsContextKey 是上下文中存储 claims 的键
const ClaimsContextKey contextKey = "claims"

// AuthHandler 认证处理器
type AuthHandler struct {
	db           *sql.DB
	jwtMgr       *auth.JWTManager
	ldap         *auth.LDAPClient
	tokenStore   *auth.TokenStore
	auditLogger  *audit.Logger
	ssoClient    *auth.SSOClient
	agreementMgr *auth.AgreementManager
}

// NewAuthHandler 创建认证处理器
func NewAuthHandler(db *sql.DB, jwtMgr *auth.JWTManager, ldap *auth.LDAPClient, auditLogger *audit.Logger) *AuthHandler {
	return &AuthHandler{
		db:           db,
		jwtMgr:       jwtMgr,
		ldap:         ldap,
		tokenStore:   auth.NewTokenStore(db),
		auditLogger:  auditLogger,
		ssoClient:    nil, // 在 server 中初始化
		agreementMgr: auth.NewAgreementManager(db),
	}
}

// SetSSOClient 设置 SSO 客户端
func (h *AuthHandler) SetSSOClient(client *auth.SSOClient) {
	h.ssoClient = client
}

// AgreementManager 获取协议管理器
func (h *AuthHandler) AgreementManager() *auth.AgreementManager {
	return h.agreementMgr
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	UseLDAP  bool   `json:"use_ldap"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	Success bool            `json:"success"`
	Data    *auth.TokenPair `json:"data,omitempty"`
	Message string          `json:"message,omitempty"`
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

// RegisterResponse 注册响应
type RegisterResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Login 处理登录请求
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 如果请求使用 LDAP 认证
	if req.UseLDAP && h.ldap != nil && h.ldap.Enabled {
		h.loginWithLDAP(w, r, &req)
		return
	}

	// 本地认证
	h.loginWithLocal(w, r, &req)
}

// loginWithLDAP 使用 LDAP 进行认证
func (h *AuthHandler) loginWithLDAP(w http.ResponseWriter, r *http.Request, req *LoginRequest) {
	result := h.ldap.Authenticate(req.Username, req.Password)
	if result.Success {
		// LDAP 认证成功，查找或创建本地用户
		var userID int
		var role string

		err := h.db.QueryRow("SELECT id, role FROM users WHERE username = ?", req.Username).Scan(&userID, &role)
		if err == sql.ErrNoRows {
			// 用户不存在，创建新用户
			// 注意：LDAP 用户的密码存储为特殊标记，表示只能通过 LDAP 认证
			// 使用随机生成的不可用密码哈希，防止本地登录
			ldapOnlyHash := "$ldap$only$" // 特殊标记，表示仅 LDAP 用户

			result2, err := h.db.Exec(
				"INSERT INTO users (username, password_hash, email, role, auth_type) VALUES (?, ?, ?, ?, ?)",
				req.Username, ldapOnlyHash, result.Email, "user", "ldap",
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to create user")
				return
			}
			id, _ := result2.LastInsertId()
			userID = int(id)
			role = "user"
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "Database error")
			return
		}

		// 生成令牌
		tokens, err := h.jwtMgr.GenerateToken(userID, req.Username, role)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to generate token")
			return
		}

		// 存储刷新令牌
		refreshExpiry := time.Now().Add(time.Duration(h.jwtMgr.GetRefreshExpiry()) * time.Minute)
		if err := h.tokenStore.StoreRefreshToken(userID, tokens.RefreshToken, refreshExpiry); err != nil {
			log.Printf("Failed to store refresh token: %v", err)
			// 不阻止登录，只记录日志
		}

		// 记录审计日志
		h.auditLog(r, userID, "login", "user", userID, "LDAP login successful")

		writeJSON(w, http.StatusOK, LoginResponse{
			Success: true,
			Data:    tokens,
		})
		return
	}

	// LDAP 认证失败，记录日志并返回错误
	log.Printf("LDAP authentication failed for user %s: %v", req.Username, result.Error)
	writeJSON(w, http.StatusUnauthorized, LoginResponse{
		Success: false,
		Message: "LDAP authentication failed",
	})
}

// loginWithLocal 使用本地数据库进行认证
func (h *AuthHandler) loginWithLocal(w http.ResponseWriter, r *http.Request, req *LoginRequest) {
	// 查询用户
	var user struct {
		ID           int
		Username     string
		PasswordHash string
		Role         string
	}

	err := h.db.QueryRow(
		"SELECT id, username, password_hash, role FROM users WHERE username = ?",
		req.Username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role)

	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusUnauthorized, LoginResponse{
			Success: false,
			Message: "Invalid username or password",
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Database error")
		return
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte(req.Password),
	); err != nil {
		writeJSON(w, http.StatusUnauthorized, LoginResponse{
			Success: false,
			Message: "Invalid username or password",
		})
		return
	}

	// 生成令牌
	tokens, err := h.jwtMgr.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		log.Printf("Failed to generate token: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}
	log.Printf("Login successful for user %s, token prefix: %s...", user.Username, tokens.AccessToken[:20])

	// 存储刷新令牌
	refreshExpiry := time.Now().Add(time.Duration(h.jwtMgr.GetRefreshExpiry()) * time.Minute)
	if err := h.tokenStore.StoreRefreshToken(user.ID, tokens.RefreshToken, refreshExpiry); err != nil {
		log.Printf("Failed to store refresh token: %v", err)
		// 不阻止登录，只记录日志
	}

	// 记录审计日志
	h.auditLog(r, user.ID, "login", "user", user.ID, "Local login successful")

	writeJSON(w, http.StatusOK, LoginResponse{
		Success: true,
		Data:    tokens,
	})
}

// Register 处理注册请求
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 验证输入
	if len(req.Username) < 3 || len(req.Username) > 50 {
		writeError(w, http.StatusBadRequest, "Username must be between 3 and 50 characters")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	// 哈希密码
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	// 插入用户
	result, err := h.db.Exec(
		"INSERT INTO users (username, password_hash, email, role) VALUES (?, ?, ?, 'user')",
		req.Username, string(passwordHash), req.Email,
	)
	if err != nil {
		if err.Error() == "UNIQUE constraint failed: users.username" {
			writeError(w, http.StatusConflict, "Username already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "Database error")
		return
	}

	userID, _ := result.LastInsertId()

	// 记录审计日志
	h.auditLog(r, int(userID), "register", "user", int(userID), "User registered")

	writeJSON(w, http.StatusCreated, RegisterResponse{
		Success: true,
		Message: "User registered successfully",
	})
}

// RefreshToken 刷新令牌
func (h *AuthHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	tokens, err := h.jwtMgr.RefreshAccessToken(req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Invalid or expired refresh token")
		return
	}

	writeJSON(w, http.StatusOK, LoginResponse{
		Success: true,
		Data:    tokens,
	})
}

// Logout 处理登出请求
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// 从上下文获取用户信息（由中间件设置）
	claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	if ok {
		h.auditLog(r, claims.UserID, "logout", "user", claims.UserID, "Logout successful")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Logged out successfully",
	})
}

// GetCurrentUser 获取当前用户信息
func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 查询用户详细信息
	var user struct {
		ID       int
		Username string
		Email    sql.NullString
		Role     string
	}

	err := h.db.QueryRow(
		"SELECT id, username, email, role FROM users WHERE id = ?",
		claims.UserID,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Role)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get user info")
		return
	}

	email := ""
	if user.Email.Valid {
		email = user.Email.String
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
			"email":    email,
			"role":     user.Role,
		},
	})
}

// MFARequest MFA 请求
type MFARequest struct {
	Enabled bool   `json:"enabled"`
	Code    string `json:"code,omitempty"`
}

// MFAResponse MFA 响应
type MFAResponse struct {
	Success bool   `json:"success"`
	Secret  string `json:"secret,omitempty"`
	URI     string `json:"uri,omitempty"`
	Message string `json:"message,omitempty"`
}

// GetAuditLogs 获取审计日志（移除 MFA 验证）
func (h *AuthHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 获取查询参数（带错误处理和限制）
	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if _, err := fmt.Sscanf(l, "%d", &limit); err != nil {
			limit = 100
		}
		// 限制最大值
		if limit > 1000 {
			limit = 1000
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if _, err := fmt.Sscanf(o, "%d", &offset); err != nil {
			offset = 0
		}
	}

	// 查询审计日志
	userID := int64(claims.UserID)
	logs, err := h.auditLogger.Query(&userID, nil, nil, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to query audit logs")
		return
	}

	// 转换日志格式
	type AuditLogResponse struct {
		ID           int64     `json:"id"`
		UserID       *int64    `json:"user_id,omitempty"`
		Action       string    `json:"action"`
		ResourceType *string   `json:"resource_type,omitempty"`
		ResourceID   *int64    `json:"resource_id,omitempty"`
		IPAddress    string    `json:"ip_address"`
		UserAgent    string    `json:"user_agent"`
		Detail       string    `json:"detail"`
		CreatedAt    time.Time `json:"created_at"`
	}

	var response []AuditLogResponse
	for _, log := range logs {
		detailJSON := "{}"
		if log.Detail != nil {
			detailBytes, _ := json.Marshal(log.Detail)
			detailJSON = string(detailBytes)
		}

		var rtStr *string
		if log.ResourceType != nil {
			s := string(*log.ResourceType)
			rtStr = &s
		}

		response = append(response, AuditLogResponse{
			ID:           log.ID,
			UserID:       log.UserID,
			Action:       string(log.Action),
			ResourceType: rtStr,
			ResourceID:   log.ResourceID,
			IPAddress:    log.IPAddress,
			UserAgent:    log.UserAgent,
			Detail:       detailJSON,
			CreatedAt:    log.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    response,
		"total":   len(response),
	})
}

// auditLog 记录审计日志
func (h *AuthHandler) auditLog(r *http.Request, userID int, action, resourceType string, resourceID int, detail string) {
	ip := getClientIP(r)

	// 构建审计日志
	auditLog := &audit.AuditLog{
		UserID:       int64Ptr(userID),
		Action:       audit.ActionType(action),
		ResourceType: resourceTypePtr(audit.ResourceType(resourceType)),
		ResourceID:   int64Ptr(resourceID),
		IPAddress:    ip,
		UserAgent:    r.UserAgent(),
		Detail:       map[string]interface{}{"detail": detail},
	}

	if err := h.auditLogger.Log(auditLog); err != nil {
		log.Printf("Failed to write audit log: %v", err)
	}
}

// 辅助函数
func int64Ptr(i int) *int64 {
	v := int64(i)
	return &v
}

func resourceTypePtr(rt audit.ResourceType) *audit.ResourceType {
	return &rt
}

// getClientIP 从请求中获取客户端真实 IP 地址
// 优先级：X-Real-IP > X-Forwarded-For（第一个 IP）> RemoteAddr
func getClientIP(r *http.Request) string {
	// 首先检查 X-Real-IP（通常由反向代理设置）
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}

	// 检查 X-Forwarded-For（可能包含多个 IP，第一个是真实 IP）
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For 格式：client, proxy1, proxy2
		// 取第一个 IP 作为真实 IP
		if idx := len(xff); idx > 0 {
			for i, c := range xff {
				if c == ',' {
					return xff[:i]
				}
			}
			return xff
		}
	}

	// 最后使用 RemoteAddr
	return r.RemoteAddr
}

// writeJSON 写入 JSON 响应
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError 写入错误响应
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{
		"success": false,
		"error":   message,
	})
}

// AuthMiddleware JWT 认证中间件
func AuthMiddleware(jwtMgr *auth.JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tokenString string
			var err error

			// 优先从 Authorization Header 读取
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				if len(authHeader) < 7 || authHeader[:7] != "Bearer " {
					log.Printf("AuthMiddleware: Invalid authorization header format: %s", authHeader)
					writeError(w, http.StatusUnauthorized, "Invalid authorization header format")
					return
				}
				tokenString = authHeader[7:]
			} else {
				// 降级：从 URL 参数读取（仅用于下载等特定场景）
				tokenString = r.URL.Query().Get("token")
				if tokenString == "" {
					log.Printf("AuthMiddleware: Missing authorization token")
					writeError(w, http.StatusUnauthorized, "Missing authorization token")
					return
				}
			}

			claims, err := jwtMgr.ValidateToken(tokenString)
			if err != nil {
				log.Printf("AuthMiddleware: Token validation failed: %v", err)
				writeError(w, http.StatusUnauthorized, "Invalid or expired token")
				return
			}

			// 将用户信息添加到请求上下文
			ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetClaimsFromContext 从请求上下文中获取用户信息
func GetClaimsFromContext(ctx context.Context) (*auth.Claims, bool) {
	claims, ok := ctx.Value(ClaimsContextKey).(*auth.Claims)
	return claims, ok
}

// =====================
// SSO 相关处理器
// =====================

// SSOLoginRequest SSO 登录请求
type SSOLoginRequest struct {
	Provider string `json:"provider"`         // "cas" or "oauth2"
	Ticket   string `json:"ticket,omitempty"` // CAS ticket
	Code     string `json:"code,omitempty"`   // OAuth2 code
	State    string `json:"state,omitempty"`  // OAuth2/CAS state
}

// SSOLoginResponse SSO 登录响应
type SSOLoginResponse struct {
	Success          bool            `json:"success"`
	Data             *auth.TokenPair `json:"data,omitempty"`
	Message          string          `json:"message,omitempty"`
	NeedsAgreement   bool            `json:"needs_agreement,omitempty"` // 是否需要同意协议
	AgreementVersion string          `json:"agreement_version,omitempty"`
}

// HandleSSOLogin 处理 SSO 登录
func (h *AuthHandler) HandleSSOLogin(w http.ResponseWriter, r *http.Request) {
	if h.ssoClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, SSOLoginResponse{
			Success: false,
			Message: "SSO is not configured",
		})
		return
	}

	var req SSOLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	var result *auth.SSOResult
	var err error

	// 根据提供者处理登录
	switch req.Provider {
	case "cas":
		result, err = h.ssoClient.ValidateCASTicket(req.Ticket, req.State)
	case "oauth2":
		// 先交换 code 获取 token
		accessToken, err := h.ssoClient.ExchangeOAuth2Code(req.Code)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, SSOLoginResponse{
				Success: false,
				Message: "Failed to exchange OAuth2 code",
			})
			return
		}
		// 获取用户信息
		result, err = h.ssoClient.GetOAuth2UserInfo(accessToken)
	default:
		writeJSON(w, http.StatusBadRequest, SSOLoginResponse{
			Success: false,
			Message: "Unsupported SSO provider",
		})
		return
	}

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SSOLoginResponse{
			Success: false,
			Message: "SSO authentication failed",
		})
		return
	}

	if !result.Success {
		// SSO 失败，检查是否需要降级到本地认证
		if h.ssoClient.IsDegraded() {
			writeJSON(w, http.StatusServiceUnavailable, SSOLoginResponse{
				Success: false,
				Message: "SSO service temporarily unavailable, please try again later",
			})
		} else {
			writeJSON(w, http.StatusUnauthorized, SSOLoginResponse{
				Success: false,
				Message: result.ErrorMsg,
			})
		}
		return
	}

	// SSO 认证成功，查找或创建本地用户
	var userID int
	var role string
	var username string

	err = h.db.QueryRow("SELECT id, username, role FROM users WHERE sso_provider = ? AND sso_user_id = ?", req.Provider, result.UserID).Scan(&userID, &username, &role)
	if err == sql.ErrNoRows {
		// 用户不存在，创建新用户
		username = result.Username
		if username == "" {
			username = result.UserID
		}

		// 使用特殊密码标记表示仅 SSO 登录（防止本地登录）
		ssoOnlyPassword := "$sso$only$" + result.UserID

		result2, err := h.db.Exec(
			"INSERT INTO users (username, password_hash, email, role, sso_provider, sso_user_id, sso_email, auth_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			username, ssoOnlyPassword, result.Email, "user", req.Provider, result.UserID, result.Email, "sso",
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to create user")
			return
		}
		id, _ := result2.LastInsertId()
		userID = int(id)
		role = "user"
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "Database error")
		return
	}

	// 更新最后登录时间
	_, err = h.db.Exec("UPDATE users SET last_login_at = CURRENT_TIMESTAMP, last_login_provider = ? WHERE id = ?", req.Provider, userID)
	if err != nil {
		log.Printf("Failed to update last login: %v", err)
	}

	// 生成令牌
	tokens, err := h.jwtMgr.GenerateToken(userID, username, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	// 存储刷新令牌
	refreshExpiry := time.Now().Add(time.Duration(h.jwtMgr.GetRefreshExpiry()) * time.Minute)
	if err := h.tokenStore.StoreRefreshToken(userID, tokens.RefreshToken, refreshExpiry); err != nil {
		log.Printf("Failed to store refresh token: %v", err)
	}

	// 检查协议同意状态
	needsAgreement := false
	agreementVersion := ""
	if h.agreementMgr != nil {
		hasAgreed, err := h.agreementMgr.HasAgreed(int64(userID))
		if err != nil {
			log.Printf("Failed to check agreement status: %v", err)
		}
		if !hasAgreed {
			needsAgreement = true
			agreementVersion, _ = h.agreementMgr.GetAgreementVersion()
		}
	}

	// 记录审计日志
	h.auditLog(r, userID, "sso_login", "user", userID, fmt.Sprintf("SSO login successful via %s", req.Provider))

	writeJSON(w, http.StatusOK, SSOLoginResponse{
		Success:          true,
		Data:             tokens,
		NeedsAgreement:   needsAgreement,
		AgreementVersion: agreementVersion,
	})
}

// GetSSOStatus 获取 SSO 状态
func (h *AuthHandler) GetSSOStatus(w http.ResponseWriter, r *http.Request) {
	if h.ssoClient == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"enabled":  false,
			"provider": "",
		})
		return
	}

	status := h.ssoClient.GetStatus()
	writeJSON(w, http.StatusOK, status)
}

// HandleSSOCallback 处理 SSO 回调（CAS/OAuth2 重定向后的回调）
func (h *AuthHandler) HandleSSOCallback(w http.ResponseWriter, r *http.Request) {
	if h.ssoClient == nil {
		writeError(w, http.StatusServiceUnavailable, "SSO is not configured")
		return
	}

	// 获取回调参数
	ticket := r.URL.Query().Get("ticket")
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	provider := r.URL.Query().Get("provider")

	if provider == "" {
		provider = "cas"
	}

	var result *auth.SSOResult
	var err error

	// 根据提供者处理回调
	switch provider {
	case "cas":
		// CAS 回调：验证 ticket 和 state
		result, err = h.ssoClient.ValidateCASTicket(ticket, state)
	case "oauth2":
		// OAuth2 回调：先验证 state，再交换 code
		if state != "" {
			if !h.ssoClient.ValidateState(state) {
				writeJSON(w, http.StatusUnauthorized, SSOLoginResponse{
					Success: false,
					Message: "Invalid state parameter",
				})
				return
			}
		}
		accessToken, err := h.ssoClient.ExchangeOAuth2Code(code)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, SSOLoginResponse{
				Success: false,
				Message: "Failed to exchange OAuth2 code",
			})
			return
		}
		result, err = h.ssoClient.GetOAuth2UserInfo(accessToken)
	default:
		writeJSON(w, http.StatusBadRequest, SSOLoginResponse{
			Success: false,
			Message: "Unsupported SSO provider",
		})
		return
	}

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SSOLoginResponse{
			Success: false,
			Message: "SSO authentication failed",
		})
		return
	}

	if !result.Success {
		writeJSON(w, http.StatusUnauthorized, SSOLoginResponse{
			Success: false,
			Message: result.ErrorMsg,
		})
		return
	}

	// SSO 认证成功，查找或创建本地用户
	var userID int
	var role string
	var username string

	err = h.db.QueryRow("SELECT id, username, role FROM users WHERE sso_provider = ? AND sso_user_id = ?", provider, result.UserID).Scan(&userID, &username, &role)
	if err == sql.ErrNoRows {
		// 用户不存在，创建新用户
		username = result.Username
		if username == "" {
			username = result.UserID
		}

		// 使用特殊密码标记表示仅 SSO 登录
		ssoOnlyPassword := "$sso$only$" + result.UserID

		result2, err := h.db.Exec(
			"INSERT INTO users (username, password_hash, email, role, sso_provider, sso_user_id, sso_email, auth_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			username, ssoOnlyPassword, result.Email, "user", provider, result.UserID, result.Email, "sso",
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to create user")
			return
		}
		id, _ := result2.LastInsertId()
		userID = int(id)
		role = "user"
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "Database error")
		return
	}

	// 更新最后登录时间
	_, err = h.db.Exec("UPDATE users SET last_login_at = CURRENT_TIMESTAMP, last_login_provider = ? WHERE id = ?", provider, userID)
	if err != nil {
		log.Printf("Failed to update last login: %v", err)
	}

	// 生成令牌
	tokens, err := h.jwtMgr.GenerateToken(userID, username, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	// 存储刷新令牌
	refreshExpiry := time.Now().Add(time.Duration(h.jwtMgr.GetRefreshExpiry()) * time.Minute)
	if err := h.tokenStore.StoreRefreshToken(userID, tokens.RefreshToken, refreshExpiry); err != nil {
		log.Printf("Failed to store refresh token: %v", err)
	}

	// 检查协议同意状态
	needsAgreement := false
	agreementVersion := ""
	if h.agreementMgr != nil {
		hasAgreed, err := h.agreementMgr.HasAgreed(int64(userID))
		if err != nil {
			log.Printf("Failed to check agreement status: %v", err)
		}
		if !hasAgreed {
			needsAgreement = true
			agreementVersion, _ = h.agreementMgr.GetAgreementVersion()
		}
	}

	// 记录审计日志
	h.auditLog(r, userID, "sso_login", "user", userID, fmt.Sprintf("SSO callback login successful via %s", provider))

	// 返回成功响应，前端可以根据需要进行重定向
	writeJSON(w, http.StatusOK, SSOLoginResponse{
		Success:          true,
		Data:             tokens,
		NeedsAgreement:   needsAgreement,
		AgreementVersion: agreementVersion,
		Message:          "SSO login successful",
	})
}

// GetSSOLoginURL 获取 SSO 登录 URL（用于 OAuth2/CAS 重定向）
func (h *AuthHandler) GetSSOLoginURL(w http.ResponseWriter, r *http.Request) {
	if h.ssoClient == nil {
		writeError(w, http.StatusServiceUnavailable, "SSO is not configured")
		return
	}

	provider := r.URL.Query().Get("provider")
	if provider == "" {
		provider = "cas"
	}

	var loginURL string
	var err error

	switch provider {
	case "cas":
		loginURL, err = h.ssoClient.GetCASLoginURL()
	case "oauth2":
		loginURL, _, err = h.ssoClient.GetOAuth2LoginURL()
	default:
		writeError(w, http.StatusBadRequest, "Unsupported provider")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to generate login URL")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"login_url": loginURL,
	})
}

// =====================
// 协议相关处理器
// =====================

// GetAgreementStatus 获取协议状态
func (h *AuthHandler) GetAgreementStatus(w http.ResponseWriter, r *http.Request) {
	if h.agreementMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Agreement manager not initialized")
		return
	}
	h.agreementMgr.GetAgreementStatusHandler(w, r)
}

// AgreeToAgreement 同意协议
func (h *AuthHandler) AgreeToAgreement(w http.ResponseWriter, r *http.Request) {
	if h.agreementMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Agreement manager not initialized")
		return
	}

	// 从上下文获取用户 ID
	claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req auth.AgreeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 如果未提供版本，使用当前版本
	version := req.Version
	if version == "" {
		var err error
		version, err = h.agreementMgr.GetAgreementVersion()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to get agreement version")
			return
		}
	}

	// 记录协议同意
	ipAddr := auth.GetClientIP(r)
	err := h.agreementMgr.RecordAgreement(int64(claims.UserID), version, ipAddr, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to record agreement")
		return
	}

	// 记录审计日志
	h.auditLog(r, claims.UserID, "agree_agreement", "agreement", int(claims.UserID), fmt.Sprintf("User agreed to agreement version %s", version))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Agreement accepted",
	})
}

// GetAgreementText 获取协议文本
func (h *AuthHandler) GetAgreementText(w http.ResponseWriter, r *http.Request) {
	version := r.URL.Query().Get("version")
	if version == "" {
		if h.agreementMgr != nil {
			version, _ = h.agreementMgr.GetAgreementVersion()
		}
		if version == "" {
			version = "1.0"
		}
	}

	text := auth.FormatAgreementText(version)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"version": version,
		"text":    text,
	})
}
