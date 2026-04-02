package handler

import (
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

// UserListResponse 用户列表响应
type UserListResponse struct {
	Success bool       `json:"success"`
	Data    []UserData `json:"data"`
	Total   int        `json:"total"`
}

// UserData 用户数据
type UserData struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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

// GetUsers 获取用户列表（仅管理员）
func (h *AuthHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 检查管理员权限
	if claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "需要管理员权限")
		return
	}

	// 解析查询参数
	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		_, _ = fmt.Sscanf(l, "%d", &limit)
		if limit > 1000 {
			limit = 1000
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		_, _ = fmt.Sscanf(o, "%d", &offset)
	}

	// 查询用户列表
	rows, err := h.db.Query(`
		SELECT id, username, email, role, created_at, updated_at
		FROM users
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询用户列表失败")
		return
	}
	defer rows.Close()

	var users []UserData
	for rows.Next() {
		var user UserData
		if err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.CreatedAt, &user.UpdatedAt); err != nil {
			continue
		}
		users = append(users, user)
	}

	if users == nil {
		users = []UserData{}
	}

	// 查询总数
	var total int
	h.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&total)

	writeJSON(w, http.StatusOK, UserListResponse{
		Success: true,
		Data:    users,
		Total:   total,
	})
}

// DeleteUserRequest 删除用户请求
type DeleteUserRequest struct {
	ID int `json:"id"`
}

// DeleteUser 删除用户（仅管理员）
func (h *AuthHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 检查管理员权限
	if claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "需要管理员权限")
		return
	}

	// 解析请求
	var req DeleteUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求体")
		return
	}

	if req.ID <= 0 {
		writeError(w, http.StatusBadRequest, "无效的用户 ID")
		return
	}

	// 不能删除自己
	if req.ID == claims.UserID {
		writeError(w, http.StatusBadRequest, "不能删除自己的账号")
		return
	}

	// 检查用户是否存在
	var exists int
	err := h.db.QueryRow("SELECT 1 FROM users WHERE id = ?", req.ID).Scan(&exists)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询用户失败")
		return
	}

	// 删除用户（先删除相关的 refresh tokens）
	_, err = h.db.Exec("DELETE FROM refresh_tokens WHERE user_id = ?", req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除用户令牌失败")
		return
	}

	_, err = h.db.Exec("DELETE FROM users WHERE id = ?", req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除用户失败")
		return
	}

	// 记录审计日志
	userIDVal := int64(claims.UserID)
	h.auditLogger.Log(&audit.AuditLog{
		UserID:    &userIDVal,
		Action:    "delete_user",
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
		Detail: map[string]interface{}{
			"deleted_user_id": req.ID,
			"status":          "success",
		},
		CreatedAt: time.Now(),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "用户已删除",
	})
}

// UpdateUserRequest 更新用户信息请求
type UpdateUserRequest struct {
	ID       int    `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username,omitempty"`
}

// UpdateUser 更新用户信息（管理员或用户自己）
func (h *AuthHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 解析请求
	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求体")
		return
	}

	if req.ID <= 0 {
		writeError(w, http.StatusBadRequest, "无效的用户 ID")
		return
	}

	// 检查权限：只能更新自己或管理员更新其他用户
	if req.ID != claims.UserID && claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "无权更新此用户")
		return
	}

	// 更新用户信息
	_, err := h.db.Exec(`
		UPDATE users
		SET email = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, req.Email, req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新用户信息失败")
		return
	}

	// 记录审计日志
	userIDVal := int64(claims.UserID)
	h.auditLogger.Log(&audit.AuditLog{
		UserID:    &userIDVal,
		Action:    "update_user",
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
		Detail: map[string]interface{}{
			"target_user_id": req.ID,
			"email":          req.Email,
			"status":         "success",
		},
		CreatedAt: time.Now(),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "用户信息已更新",
	})
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangePassword 修改密码（用户自己）
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 解析请求
	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求体")
		return
	}

	if req.OldPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "密码不能为空")
		return
	}

	if len(req.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "新密码长度至少为 6 位")
		return
	}

	// 查询当前密码哈希
	var currentHash string
	err := h.db.QueryRow("SELECT password_hash FROM users WHERE id = ?", claims.UserID).Scan(&currentHash)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询用户失败")
		return
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.OldPassword)); err != nil {
		writeError(w, http.StatusBadRequest, "原密码错误")
		return
	}

	// 加密新密码
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}

	// 更新密码
	_, err = h.db.Exec(`
		UPDATE users
		SET password_hash = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, string(newHash), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新密码失败")
		return
	}

	// 删除该用户的所有 refresh tokens（强制重新登录）
	_, err = h.db.Exec("DELETE FROM refresh_tokens WHERE user_id = ?", claims.UserID)
	if err != nil {
		log.Printf("删除用户 refresh tokens 失败：%v", err)
	}

	// 记录审计日志
	userIDVal := int64(claims.UserID)
	h.auditLogger.Log(&audit.AuditLog{
		UserID:    &userIDVal,
		Action:    "change_password",
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
		Detail: map[string]interface{}{
			"status": "success",
		},
		CreatedAt: time.Now(),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "密码已修改，请重新登录",
	})
}

// AdminChangePasswordRequest 管理员重置密码请求
type AdminChangePasswordRequest struct {
	UserID      int    `json:"user_id"`
	NewPassword string `json:"new_password"`
}

// AdminChangePassword 管理员重置用户密码
func (h *AuthHandler) AdminChangePassword(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 检查管理员权限
	if claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "需要管理员权限")
		return
	}

	// 解析请求
	var req AdminChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求体")
		return
	}

	if req.UserID <= 0 {
		writeError(w, http.StatusBadRequest, "无效的用户 ID")
		return
	}

	if req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "密码不能为空")
		return
	}

	if len(req.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "新密码长度至少为 6 位")
		return
	}

	// 检查用户是否存在
	var exists int
	err := h.db.QueryRow("SELECT 1 FROM users WHERE id = ?", req.UserID).Scan(&exists)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询用户失败")
		return
	}

	// 加密新密码
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}

	// 更新密码
	_, err = h.db.Exec(`
		UPDATE users
		SET password_hash = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, string(newHash), req.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新密码失败")
		return
	}

	// 删除该用户的所有 refresh tokens（强制重新登录）
	_, err = h.db.Exec("DELETE FROM refresh_tokens WHERE user_id = ?", req.UserID)
	if err != nil {
		log.Printf("删除用户 refresh tokens 失败：%v", err)
	}

	// 记录审计日志
	userIDVal := int64(claims.UserID)
	h.auditLogger.Log(&audit.AuditLog{
		UserID:    &userIDVal,
		Action:    "admin_reset_password",
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
		Detail: map[string]interface{}{
			"target_user_id": req.UserID,
			"status":         "success",
		},
		CreatedAt: time.Now(),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "密码已重置",
	})
}
