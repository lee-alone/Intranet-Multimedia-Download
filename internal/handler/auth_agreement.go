package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/campus/collector/internal/auth"
)

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
	ipAddr := getClientIP(r)
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
