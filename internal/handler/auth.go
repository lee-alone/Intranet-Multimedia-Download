// Package handler 提供 HTTP 请求处理器
package handler

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

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

// RefreshToken 刷新令牌
