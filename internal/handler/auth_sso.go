package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/campus/collector/internal/auth"
)

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
