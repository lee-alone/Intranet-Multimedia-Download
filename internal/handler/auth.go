// Package handler 提供 HTTP 请求处理器
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
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
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, "Missing authorization header")
				return
			}

			if len(authHeader) < 7 || authHeader[:7] != "Bearer " {
				writeError(w, http.StatusUnauthorized, "Invalid authorization header format")
				return
			}

			tokenString := authHeader[7:]
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
