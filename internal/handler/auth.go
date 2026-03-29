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
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// contextKey 是上下文中存储 claims 的键类型
type contextKey string

// ClaimsContextKey 是上下文中存储 claims 的键
const ClaimsContextKey contextKey = "claims"

// AuthHandler 认证处理器
type AuthHandler struct {
	db          *sql.DB
	jwtMgr      *auth.JWTManager
	ldap        *auth.LDAPClient
	tokenStore  *auth.TokenStore
	auditLogger *audit.Logger
}

// NewAuthHandler 创建认证处理器
func NewAuthHandler(db *sql.DB, jwtMgr *auth.JWTManager, ldap *auth.LDAPClient, auditLogger *audit.Logger) *AuthHandler {
	return &AuthHandler{
		db:          db,
		jwtMgr:      jwtMgr,
		ldap:        ldap,
		tokenStore:  auth.NewTokenStore(db),
		auditLogger: auditLogger,
	}
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
		writeError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

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
	claims, ok := r.Context().Value("claims").(*auth.Claims)
	if ok {
		h.auditLog(r, claims.UserID, "logout", "user", claims.UserID, "Logout successful")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Logged out successfully",
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

// GenerateMFA 生成 MFA 配置
func (h *AuthHandler) GenerateMFA(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value("claims").(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 获取用户信息
	var userID int
	var mfaEnabled bool
	var mfaSecret sql.NullString
	err := h.db.QueryRow(
		"SELECT id, mfa_enabled, mfa_secret FROM users WHERE id = ?",
		claims.UserID,
	).Scan(&userID, &mfaEnabled, &mfaSecret)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get user info")
		return
	}

	// 如果已启用 MFA，返回错误
	if mfaEnabled {
		writeJSON(w, http.StatusOK, MFAResponse{
			Success: false,
			Message: "MFA already enabled",
		})
		return
	}

	// 生成新的 MFA 密钥
	// 使用 totp.Generate 生成密钥
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Campus Collector",
		AccountName: claims.Username,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to generate MFA secret")
		return
	}

	writeJSON(w, http.StatusOK, MFAResponse{
		Success: true,
		Secret:  key.Secret(),
		URI:     key.URL(),
	})
}

// VerifyMFA 验证 MFA 代码
func (h *AuthHandler) VerifyMFA(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value("claims").(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req MFARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 获取用户信息
	var userID int
	var mfaEnabled bool
	var mfaSecret sql.NullString
	var username string
	err := h.db.QueryRow(
		"SELECT id, username, mfa_enabled, mfa_secret FROM users WHERE id = ?",
		claims.UserID,
	).Scan(&userID, &username, &mfaEnabled, &mfaSecret)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get user info")
		return
	}

	if req.Enabled {
		// 启用 MFA - 需要验证代码
		if req.Code == "" {
			writeJSON(w, http.StatusOK, MFAResponse{
				Success: false,
				Message: "Verification code required",
			})
			return
		}

		// 验证 TOTP 代码
		if !totp.Validate(req.Code, mfaSecret.String) {
			writeJSON(w, http.StatusOK, MFAResponse{
				Success: false,
				Message: "Invalid verification code",
			})
			return
		}

		// 更新数据库
		_, err = h.db.Exec(
			"UPDATE users SET mfa_enabled = 1, mfa_secret = ? WHERE id = ?",
			mfaSecret.String,
			userID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to enable MFA")
			return
		}

		// 记录审计日志
		h.auditLog(r, userID, "mfa_enable", "user", userID, "MFA enabled")

		writeJSON(w, http.StatusOK, MFAResponse{
			Success: true,
			Message: "MFA enabled successfully",
		})
	} else {
		// 禁用 MFA - 如果已启用且提供了代码，需要验证
		if mfaEnabled {
			if req.Code == "" || !totp.Validate(req.Code, mfaSecret.String) {
				writeJSON(w, http.StatusOK, MFAResponse{
					Success: false,
					Message: "Invalid verification code",
				})
				return
			}
		}

		// 更新数据库
		_, err = h.db.Exec(
			"UPDATE users SET mfa_enabled = 0, mfa_secret = NULL WHERE id = ?",
			userID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to disable MFA")
			return
		}

		// 记录审计日志
		h.auditLog(r, userID, "mfa_disable", "user", userID, "MFA disabled")

		writeJSON(w, http.StatusOK, MFAResponse{
			Success: true,
			Message: "MFA disabled successfully",
		})
	}
}

// GetMFAStatus 获取 MFA 状态
func (h *AuthHandler) GetMFAStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value("claims").(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var mfaEnabled bool
	err := h.db.QueryRow(
		"SELECT mfa_enabled FROM users WHERE id = ?",
		claims.UserID,
	).Scan(&mfaEnabled)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get MFA status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"mfaEnabled": mfaEnabled,
	})
}

// GetAuditLogs 获取审计日志（需要 MFA 验证）
func (h *AuthHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value("claims").(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 获取 MFA 验证码
	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"success": false,
			"message": "MFA verification code required",
		})
		return
	}

	// 获取用户的 MFA 密钥
	var mfaEnabled bool
	var mfaSecret sql.NullString
	err := h.db.QueryRow(
		"SELECT mfa_enabled, mfa_secret FROM users WHERE id = ?",
		claims.UserID,
	).Scan(&mfaEnabled, &mfaSecret)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get user info")
		return
	}

	// 验证 MFA 代码
	if !mfaEnabled || !totp.Validate(code, mfaSecret.String) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"success": false,
			"message": "Invalid MFA verification code",
		})
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
					writeError(w, http.StatusUnauthorized, "Invalid authorization header format")
					return
				}
				tokenString = authHeader[7:]
			} else {
				// 降级：从 URL 参数读取（仅用于下载等特定场景）
				tokenString = r.URL.Query().Get("token")
				if tokenString == "" {
					writeError(w, http.StatusUnauthorized, "Missing authorization token")
					return
				}
			}

			claims, err := jwtMgr.ValidateToken(tokenString)
			if err != nil {
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
