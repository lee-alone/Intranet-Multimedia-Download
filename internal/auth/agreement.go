// Package auth 提供用户协议管理功能
package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// contextKey 是上下文中存储 claims 的键类型（与 handler 包保持一致）
type contextKey string

// ClaimsContextKey 是上下文中存储 claims 的键（与 handler 包保持一致）
const ClaimsContextKey contextKey = "claims"

// AgreementManager 协议管理器
type AgreementManager struct {
	db *sql.DB
}

// AgreementRecord 协议记录
type AgreementRecord struct {
	ID               int64     `json:"id"`
	UserID           int64     `json:"user_id"`
	AgreementVersion string    `json:"agreement_version"`
	AgreedAt         time.Time `json:"agreed_at"`
	IPAddress        string    `json:"ip_address"`
	UserAgent        string    `json:"user_agent"`
}

// NewAgreementManager 创建协议管理器
func NewAgreementManager(db *sql.DB) *AgreementManager {
	return &AgreementManager{
		db: db,
	}
}

// GetAgreementVersion 获取当前协议版本
func (am *AgreementManager) GetAgreementVersion() (string, error) {
	var version string
	err := am.db.QueryRow(
		"SELECT value FROM system_config WHERE key = 'agreement_version'",
	).Scan(&version)
	if err != nil {
		return "1.0", err
	}
	return version, nil
}

// UpdateAgreementVersion 更新协议版本
func (am *AgreementManager) UpdateAgreementVersion(version string) error {
	_, err := am.db.Exec(
		"INSERT OR REPLACE INTO system_config (key, value, updated_at) VALUES ('agreement_version', ?, CURRENT_TIMESTAMP)",
		version,
	)
	return err
}

// HasAgreed 检查用户是否已同意当前版本协议
func (am *AgreementManager) HasAgreed(userID int64) (bool, error) {
	version, err := am.GetAgreementVersion()
	if err != nil {
		return false, err
	}

	var count int
	err = am.db.QueryRow(
		"SELECT COUNT(*) FROM user_agreements WHERE user_id = ? AND agreement_version = ?",
		userID, version,
	).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// HasAgreedVersion 检查用户是否已同意指定版本协议
func (am *AgreementManager) HasAgreedVersion(userID int64, version string) (bool, error) {
	var count int
	err := am.db.QueryRow(
		"SELECT COUNT(*) FROM user_agreements WHERE user_id = ? AND agreement_version = ?",
		userID, version,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// RecordAgreement 记录用户同意协议
func (am *AgreementManager) RecordAgreement(userID int64, version, ipAddress, userAgent string) error {
	_, err := am.db.Exec(`
		INSERT INTO user_agreements (user_id, agreement_version, ip_address, user_agent, agreed_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, userID, version, ipAddress, userAgent)
	return err
}

// GetUserAgreements 获取用户的协议同意记录
func (am *AgreementManager) GetUserAgreements(userID int64, limit, offset int) ([]AgreementRecord, error) {
	rows, err := am.db.Query(`
		SELECT id, user_id, agreement_version, agreed_at, ip_address, user_agent
		FROM user_agreements
		WHERE user_id = ?
		ORDER BY agreed_at DESC
		LIMIT ? OFFSET ?
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []AgreementRecord
	for rows.Next() {
		var record AgreementRecord
		err := rows.Scan(
			&record.ID,
			&record.UserID,
			&record.AgreementVersion,
			&record.AgreedAt,
			&record.IPAddress,
			&record.UserAgent,
		)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

// GetAgreementStatusResponse 协议状态响应
type GetAgreementStatusResponse struct {
	Success        bool   `json:"success"`
	CurrentVersion string `json:"current_version"`
	HasAgreed      bool   `json:"has_agreed"`
	NeedsUpdate    bool   `json:"needs_update"` // 是否需要重新同意（协议更新）
}

// GetAgreementStatusHandler 获取协议状态处理器
func (am *AgreementManager) GetAgreementStatusHandler(w http.ResponseWriter, r *http.Request) {
	// 从上下文获取用户 ID
	claims, ok := r.Context().Value(ClaimsContextKey).(*Claims)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	currentVersion, err := am.GetAgreementVersion()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to get agreement version")
		return
	}

	hasAgreed, err := am.HasAgreed(int64(claims.UserID))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to check agreement status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"current_version": currentVersion,
		"has_agreed":      hasAgreed,
		"needs_update":    false,
	})
}

// AgreeRequest 同意协议请求
type AgreeRequest struct {
	Version string `json:"version"`
}

// AgreeHandler 同意协议处理器
func (am *AgreementManager) AgreeHandler(w http.ResponseWriter, r *http.Request) {
	// 从上下文获取用户 ID
	claims, ok := r.Context().Value(ClaimsContextKey).(*Claims)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req AgreeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 如果未提供版本，使用当前版本
	version := req.Version
	if version == "" {
		var err error
		version, err = am.GetAgreementVersion()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "Failed to get agreement version")
			return
		}
	}

	// 记录协议同意
	ipAddr := r.RemoteAddr
	if forwarded := r.Header.Get("X-Real-IP"); forwarded != "" {
		ipAddr = forwarded
	} else if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		// 取第一个 IP
		for i, c := range forwarded {
			if c == ',' {
				ipAddr = forwarded[:i]
				break
			}
		}
	}

	err := am.RecordAgreement(int64(claims.UserID), version, ipAddr, r.UserAgent())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to record agreement")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Agreement accepted",
	})
}

// CheckAgreementMiddleware 协议同意检查中间件
func (am *AgreementManager) CheckAgreementMiddleware(excludePaths ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 排除特定路径
			for _, path := range excludePaths {
				if r.URL.Path == path {
					next.ServeHTTP(w, r)
					return
				}
			}

			// 从上下文获取用户 ID
			claims, ok := r.Context().Value(ClaimsContextKey).(*Claims)
			if !ok {
				// 未认证用户，跳过检查
				next.ServeHTTP(w, r)
				return
			}

			// 检查协议同意状态
			hasAgreed, err := am.HasAgreed(int64(claims.UserID))
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "Failed to check agreement status")
				return
			}

			if !hasAgreed {
				// 未同意协议，返回需要同意协议的错误
				writeJSON(w, http.StatusForbidden, map[string]interface{}{
					"success": false,
					"error":   "agreement_required",
					"message": "Please agree to the user agreement first",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// 辅助函数
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{
		"success": false,
		"error":   message,
	})
}

// GetClientIP 获取客户端 IP
func GetClientIP(r *http.Request) string {
	// 检查 X-Real-IP
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}

	// 检查 X-Forwarded-For
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i, c := range xff {
			if c == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	return r.RemoteAddr
}

// FormatAgreementText 格式化协议文本（示例）
func FormatAgreementText(version string) string {
	return fmt.Sprintf(`校园资源采集系统用户协议
版本：%s

一、总则
1.1 本协议是您（以下简称"用户"）与校园资源采集系统（以下简称"本系统"）运营方之间关于使用本系统服务所订立的协议。
1.2 用户在使用本系统服务前，应当认真阅读并充分理解本协议内容。

二、服务内容
2.1 本系统提供校园资源采集、下载、管理等服务。
2.2 用户应当遵守国家相关法律法规，不得利用本系统从事违法活动。

三、用户义务
3.1 用户应当保证提供的信息真实、准确、完整。
3.2 用户应当妥善保管账号信息，不得将账号转借他人使用。
3.3 用户应当尊重知识产权，不得下载、传播侵权内容。

四、免责声明
4.1 本系统不对用户下载内容的合法性承担责任。
4.2 因网络原因导致的服务中断，本系统不承担责任。

五、协议变更
5.1 本系统有权根据需要修改本协议内容。
5.2 协议变更后，用户继续使用服务视为接受新协议。

六、其他
6.1 本协议自用户点击"同意"按钮时生效。
6.2 本协议的解释、效力及纠纷解决适用中华人民共和国法律。
`, version)
}
