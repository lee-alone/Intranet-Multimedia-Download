package handler

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/auth"
)

// AuditLogResponse 审计日志响应
type AuditLogResponse struct {
	ID           int64     `json:"id"`
	UserID       *int64    `json:"user_id,omitempty"`
	Username     *string   `json:"username,omitempty"`
	Action       string    `json:"action"`
	ResourceType *string   `json:"resource_type,omitempty"`
	ResourceID   *int64    `json:"resource_id,omitempty"`
	IPAddress    string    `json:"ip_address"`
	UserAgent    string    `json:"user_agent"`
	Detail       string    `json:"detail"`
	CreatedAt    time.Time `json:"created_at"`
}

// GetAuditLogs 获取审计日志（仅管理员）
func (h *AuthHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
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

	// 查询审计日志（关联查询用户名）
	// 使用 SQL 查询而不是 auditLogger.Query，以便获取用户名
	query := `
		SELECT a.id, a.user_id, a.action, a.resource_type, a.resource_id,
		       a.ip_address, a.user_agent, a.detail, a.created_at,
		       u.username
		FROM audit_logs a
		LEFT JOIN users u ON a.user_id = u.id
		ORDER BY a.created_at DESC
		LIMIT ? OFFSET ?
	`
	rows, err := h.db.Query(query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to query audit logs")
		return
	}
	defer rows.Close()

	var response []AuditLogResponse
	for rows.Next() {
		var auditLog AuditLogResponse
		var username sql.NullString
		var resourceTypeStr sql.NullString
		var resourceID sql.NullInt64
		var detailJSON sql.NullString

		err := rows.Scan(
			&auditLog.ID,
			&auditLog.UserID,
			&auditLog.Action,
			&resourceTypeStr,
			&resourceID,
			&auditLog.IPAddress,
			&auditLog.UserAgent,
			&detailJSON,
			&auditLog.CreatedAt,
			&username,
		)
		if err != nil {
			log.Printf("Failed to scan audit log: %v", err)
			continue
		}

		if username.Valid {
			auditLog.Username = &username.String
		}

		if resourceTypeStr.Valid {
			rt := resourceTypeStr.String
			auditLog.ResourceType = &rt
		}

		if resourceID.Valid {
			auditLog.ResourceID = &resourceID.Int64
		}

		if detailJSON.Valid && detailJSON.String != "" {
			auditLog.Detail = detailJSON.String
		} else {
			auditLog.Detail = "{}"
		}

		response = append(response, auditLog)
	}

	if response == nil {
		response = []AuditLogResponse{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    response,
		"total":   len(response),
	})
}

// auditLog 记录审计日志（异步模式）
func (h *AuthHandler) auditLog(r *http.Request, userID int, action, resourceType string, resourceID int, detail string) {
	// 关键：在进入协程前，先从请求中提取需要的数据，因为协程运行期间请求上下文可能已销毁
	ip := getClientIP(r)
	ua := r.UserAgent()
	
	// 开启后台协程处理耗时的数据库/文件写入
	go func() {
		auditLog := &audit.AuditLog{
			UserID:       int64Ptr(userID),
			Action:       audit.ActionType(action),
			ResourceType: resourceTypePtr(audit.ResourceType(resourceType)),
			ResourceID:   int64Ptr(resourceID),
			IPAddress:    ip,
			UserAgent:    ua,
			Detail:       map[string]interface{}{"detail": detail},
			CreatedAt:    time.Now(),
		}
		if err := h.auditLogger.Log(auditLog); err != nil {
			// 后台记录失败仅打印日志，不阻塞用户
			log.Printf("🚨 [AsyncAudit] 异步写入审计日志失败: %v", err)
		}
	}()
}
