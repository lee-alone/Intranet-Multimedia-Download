// Package handler 提供 HTTP 请求处理器
package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/campus/collector/internal/auth"
)

// CookieHandler Cookie 管理处理器
type CookieHandler struct {
	db     *sql.DB
	jwtMgr *auth.JWTManager
}

// NewCookieHandler 创建 Cookie 处理器
func NewCookieHandler(db *sql.DB, jwtMgr *auth.JWTManager) *CookieHandler {
	return &CookieHandler{
		db:     db,
		jwtMgr: jwtMgr,
	}
}

// PublicKeyData 公钥数据
type PublicKeyData struct {
	PubKey string `json:"pubkey"`
}

// GetPublicKeyResponse 获取公钥响应
type GetPublicKeyResponse struct {
	Success bool           `json:"success"`
	Data    *PublicKeyData `json:"data,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// SaveCookieRequest 保存 Cookie 请求
type SaveCookieRequest struct {
	EncryptedData string `json:"encrypted_data"` // 前端加密后的数据（信封加密）
	Domain        string `json:"domain"`         // Cookie 所属域名
	IsShared      bool   `json:"is_shared"`      // 是否共享（仅管理员可设置）
}

// SaveCookieResponse 保存 Cookie 响应
type SaveCookieResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// GetCookieResponse 获取 Cookie 响应
type GetCookieResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// CookieInfo Cookie 信息（不包含内容）
type CookieInfo struct {
	Domain    string    `json:"domain"`
	UpdatedAt time.Time `json:"updated_at"`
	IsShared  bool      `json:"is_shared"`
}

// ListCookiesResponse 列出 Cookie 响应
type ListCookiesResponse struct {
	Success bool         `json:"success"`
	Data    []CookieInfo `json:"data,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// GetPublicKey 返回 RSA 公钥
func (h *CookieHandler) GetPublicKey(w http.ResponseWriter, r *http.Request) {
	// 🚩 防崩溃保护
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🚨 [GetPublicKey] 捕获到 Panic: %v", r)
			writeJSON(w, http.StatusInternalServerError, GetPublicKeyResponse{
				Success: false,
				Error:   fmt.Sprintf("服务器内部错误: %v", r),
			})
		}
	}()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pubKeyPEM, err := h.jwtMgr.GetPublicKeyPEM()
	if err != nil {
		log.Printf("Failed to get public key: %v", err)
		writeJSON(w, http.StatusInternalServerError, GetPublicKeyResponse{
			Success: false,
			Error:   "Failed to retrieve public key",
		})
		return
	}

	writeJSON(w, http.StatusOK, GetPublicKeyResponse{
		Success: true,
		Data:    &PublicKeyData{PubKey: pubKeyPEM},
	})
}

// SaveCookie 保存加密的 Cookie
func (h *CookieHandler) SaveCookie(w http.ResponseWriter, r *http.Request) {
	// 🚩🚩🚩 【防崩溃保护】捕获任何可能的 Panic，防止连接断开
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🚨🚨🚨 [SaveCookie] 捕获到 Panic! 错误详情: %v", r)
			// 尝试写入错误响应（如果还没写入）
			writeJSON(w, http.StatusInternalServerError, SaveCookieResponse{
				Success: false,
				Error:   fmt.Sprintf("服务器内部错误: %v", r),
			})
		}
	}()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 从 context 获取用户信息（由 AuthMiddleware 注入）
	userID, role, ok := getUserFromContext(r)
	if !ok {
		return
	}

	var req SaveCookieRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SaveCookieResponse{
			Success: false,
			Error:   "Invalid request body",
		})
		return
	}

	// 验证域名
	if !isValidDomain(req.Domain) {
		writeJSON(w, http.StatusBadRequest, SaveCookieResponse{
			Success: false,
			Error:   "Invalid domain format",
		})
		return
	}

	// 解密数据（信封加密）
	decryptedContent, err := h.decryptEnvelope(req.EncryptedData)
	if err != nil {
		log.Printf("Failed to decrypt cookie: %v", err)
		writeJSON(w, http.StatusBadRequest, SaveCookieResponse{
			Success: false,
			Error:   "Failed to decrypt cookie data",
		})
		return
	}

	// 验证 Cookie 格式（Netscape 格式）
	if err := validateCookieFormat(decryptedContent); err != nil {
		writeJSON(w, http.StatusBadRequest, SaveCookieResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid cookie format: %v", err),
		})
		return
	}

	// 判断是否共享（仅管理员可设置共享）
	isShared := false
	if req.IsShared {
		if role != "admin" {
			// 非管理员尝试设置共享，返回 403
			writeJSON(w, http.StatusForbidden, SaveCookieResponse{
				Success: false,
				Error:   "Permission denied: only administrators can set shared cookies",
			})
			return
		}
		// 管理员设置共享
		isShared = true
	}

	// 🚩 详细日志：保存到数据库前的数据检查
	log.Printf("🔍 [SaveCookie] 用户ID: %d, 角色: %s, 域名: %s, 共享: %t, 解密后内容长度: %d 字节",
		userID, role, req.Domain, isShared, len(decryptedContent))

	// 保存到数据库
	result, err := h.db.Exec(`
		INSERT INTO user_cookies (user_id, domain, content, is_shared, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id, domain)
		DO UPDATE SET content = excluded.content, updated_at = CURRENT_TIMESTAMP
	`, userID, req.Domain, decryptedContent, isShared)

	if err != nil {
		log.Printf("🚨 [SaveCookie] 数据库执行失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, SaveCookieResponse{
			Success: false,
			Error:   fmt.Sprintf("数据库保存失败: %v", err),
		})
		return
	}

	// 🚩 验证 SQL 执行结果
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("🚨 [SaveCookie] 获取影响行数失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, SaveCookieResponse{
			Success: false,
			Error:   "保存验证失败",
		})
		return
	}
	log.Printf("✅ [SaveCookie] 保存成功, 影响行数: %d", rowsAffected)

	writeJSON(w, http.StatusOK, SaveCookieResponse{
		Success: true,
		Message: "Cookie saved successfully",
	})
}

// GetCookie 获取用户的 Cookie（用于下载引擎）
func (h *CookieHandler) GetCookie(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, ok := getUserFromContext(r)
	if !ok {
		return
	}

	domain := r.URL.Query().Get("domain")
	if domain == "" {
		writeJSON(w, http.StatusBadRequest, GetCookieResponse{
			Success: false,
			Error:   "Missing domain parameter",
		})
		return
	}

	// 精确匹配域名（防止 abilibili.com 匹配到 bilibili.com）
	content, err := h.getCookieByDomain(userID, domain)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, GetCookieResponse{
				Success: false,
				Error:   "Cookie not found for this domain",
			})
			return
		}
		log.Printf("Failed to get cookie: %v", err)
		writeJSON(w, http.StatusInternalServerError, GetCookieResponse{
			Success: false,
			Error:   "Failed to retrieve cookie",
		})
		return
	}

	writeJSON(w, http.StatusOK, GetCookieResponse{
		Success: true,
		Data:    map[string]string{"content": content, "domain": domain},
	})
}

// ListCookies 列出用户的所有 Cookie
func (h *CookieHandler) ListCookies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, ok := getUserFromContext(r)
	if !ok {
		return
	}

	rows, err := h.db.Query(`
		SELECT domain, is_shared, updated_at 
		FROM user_cookies 
		WHERE user_id = ? 
		ORDER BY updated_at DESC
	`, userID)

	if err != nil {
		log.Printf("Failed to list cookies: %v", err)
		writeJSON(w, http.StatusInternalServerError, ListCookiesResponse{
			Success: false,
			Error:   "Failed to retrieve cookies",
		})
		return
	}
	defer rows.Close()

	var cookies []CookieInfo
	for rows.Next() {
		var c CookieInfo
		if err := rows.Scan(&c.Domain, &c.IsShared, &c.UpdatedAt); err != nil {
			log.Printf("Failed to scan cookie row: %v", err)
			continue
		}
		cookies = append(cookies, c)
	}

	writeJSON(w, http.StatusOK, ListCookiesResponse{
		Success: true,
		Data:    cookies,
	})
}

// DeleteCookie 删除 Cookie
func (h *CookieHandler) DeleteCookie(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, ok := getUserFromContext(r)
	if !ok {
		return
	}

	domain := r.URL.Query().Get("domain")
	if domain == "" {
		writeJSON(w, http.StatusBadRequest, SaveCookieResponse{
			Success: false,
			Error:   "Missing domain parameter",
		})
		return
	}

	result, err := h.db.Exec(
		"DELETE FROM user_cookies WHERE user_id = ? AND domain = ?",
		userID, domain,
	)

	if err != nil {
		log.Printf("Failed to delete cookie: %v", err)
		writeJSON(w, http.StatusInternalServerError, SaveCookieResponse{
			Success: false,
			Error:   "Failed to delete cookie",
		})
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeJSON(w, http.StatusNotFound, SaveCookieResponse{
			Success: false,
			Error:   "Cookie not found",
		})
		return
	}

	writeJSON(w, http.StatusOK, SaveCookieResponse{
		Success: true,
		Message: "Cookie deleted successfully",
	})
}

// decryptEnvelope 解密信封加密的数据
// 前端使用 RSA 公钥加密 AES Key，用 AES-GCM 加密内容
// 后端使用 RSA 私钥解密 AES Key，再用 AES-GCM 解密内容
// 数据格式：[RSA 加密的 AES Key 长度 (2 字节)][RSA 加密的 AES Key][AES-GCM Nonce][AES-GCM 加密的内容]
func (h *CookieHandler) decryptEnvelope(encryptedDataBase64 string) (string, error) {
	// 🚩 调试日志
	log.Printf("🔍 [decryptEnvelope] 输入数据长度: %d", len(encryptedDataBase64))

	// 清洗 Base64 字符串中的空白字符（换行符、空格等），防止 StdEncoding 报错
	cleanData := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1 // 删除该字符
		}
		return r
	}, encryptedDataBase64)

	// 🚩 调试日志
	log.Printf("🔍 [decryptEnvelope] 清洗后数据长度: %d", len(cleanData))

	// 使用清洗后的数据解码
	encryptedData, err := base64.StdEncoding.DecodeString(cleanData)
	if err != nil {
		log.Printf("🚨 [decryptEnvelope] Base64 解码失败: %v", err)
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	// 🚩 调试日志
	log.Printf("🔍 [decryptEnvelope] Base64 解码成功, 二进制数据长度: %d", len(encryptedData))

	// 🚩🚩🚩 【防崩溃保护】严格的最小长度检查
	// 信封格式：[2字节 AES Key 长度][RSA 加密的 AES Key][AES-GCM 数据]
	// AES-GCM 数据格式：[12字节 Nonce][加密内容][16字节 Auth Tag]
	// 最小安全长度：2(长度字段) + 256(RSA-2048 加密后的 AES Key) + 12(GCM Nonce) + 16(GCM Tag) = 286 字节
	// 但我们使用动态检查，不硬编码 RSA 密文长度

	dataLen := len(encryptedData)

	// 🚩 检查 1: 数据必须至少有 2 字节用于读取长度字段
	if dataLen < 2 {
		log.Printf("🚨 [decryptEnvelope] 数据过短，无法读取长度字段: %d 字节", dataLen)
		return "", fmt.Errorf("密文数据过短，无法解析长度字段")
	}

	// 读取 RSA 加密的 AES Key 长度
	aesKeyLen := int(encryptedData[0])<<8 | int(encryptedData[1])

	log.Printf("🔍 [decryptEnvelope] AES Key 长度(从数据读取): %d", aesKeyLen)

	// 🚩 检查 2: AES Key 长度必须合理（AES-128: 16字节, AES-256: 32字节）
	// RSA-2048 加密后的密文长度固定为 256 字节
	if aesKeyLen <= 0 || aesKeyLen > 512 {
		log.Printf("🚨 [decryptEnvelope] AES Key 长度异常: %d (预期 16-256 字节)", aesKeyLen)
		return "", fmt.Errorf("密文格式错误: AES Key 长度 %d 超出合理范围", aesKeyLen)
	}

	// 🚩 检查 3: 总数据长度必须足够
	// 2(长度字段) + aesKeyLen(RSA 密文) + 12(GCM Nonce) + 16(GCM Tag) = 最小 30 字节
	minRequiredLen := 2 + aesKeyLen + 12 + 16
	if dataLen < minRequiredLen {
		log.Printf("🚨 [decryptEnvelope] 密文完整性校验失败: 需要至少 %d 字节，实际 %d 字节",
			minRequiredLen, dataLen)
		return "", fmt.Errorf("密文数据不完整: 需要至少 %d 字节，实际 %d 字节", minRequiredLen, dataLen)
	}

	// 🚩 检查 4: 防止切片越界 - 再次验证
	if 2+aesKeyLen > dataLen {
		log.Printf("🚨 [decryptEnvelope] 切片越界保护: 2+%d > %d", aesKeyLen, dataLen)
		return "", fmt.Errorf("密文数据损坏，无法安全提取 AES Key")
	}

	// ✅ 所有检查通过，现在可以安全地进行切片操作
	encryptedAESKey := encryptedData[2 : 2+aesKeyLen]
	aesGCMData := encryptedData[2+aesKeyLen:]

	log.Printf("🔍 [decryptEnvelope] 提取到 encryptedAESKey 长度: %d, aesGCMData 长度: %d",
		len(encryptedAESKey), len(aesGCMData))

	// 使用 RSA 私钥解密 AES Key (OAEP with SHA-256)
	aesKey, err := rsa.DecryptOAEP(
		sha256.New(),
		rand.Reader,
		h.jwtMgr.GetPrivateKey(),
		encryptedAESKey,
		nil,
	)
	if err != nil {
		log.Printf("🚨 [decryptEnvelope] RSA 解密 AES Key 失败: %v", err)
		return "", fmt.Errorf("failed to decrypt AES key: %w", err)
	}

	log.Printf("🔍 [decryptEnvelope] RSA 解密成功, AES Key 长度: %d", len(aesKey))

	// 使用 AES-GCM 解密内容
	// AES-GCM 格式：[Nonce (12 字节)][加密内容][Auth Tag (16 字节)]
	decryptedContent, err := decryptAESGCM(aesKey, aesGCMData)
	if err != nil {
		log.Printf("🚨 [decryptEnvelope] AES-GCM 解密失败: %v", err)
		return "", fmt.Errorf("failed to decrypt content with AES-GCM: %w", err)
	}

	log.Printf("🔍 [decryptEnvelope] AES-GCM 解密成功, 明文长度: %d", len(decryptedContent))

	return string(decryptedContent), nil
}

// decryptAESGCM 使用 AES-GCM 解密数据
func decryptAESGCM(key []byte, ciphertext []byte) ([]byte, error) {
	// 🚩 防崩溃保护
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🚨 [decryptAESGCM] 捕获到 Panic: %v", r)
		}
	}()

	// 创建 AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// 创建 GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Nonce 大小为 12 字节（GCM 标准）
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short: need at least %d bytes, got %d", nonceSize, len(ciphertext))
	}

	// 提取 nonce 和密文
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// 解密
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

// validateCookieFormat 验证 Cookie 是否符合 Netscape 格式
func validateCookieFormat(content string) error {
	lines := strings.Split(content, "\n")
	validLines := 0

	for i, line := range lines {
		line = strings.TrimSpace(line)

		// 跳过空行和注释行
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Netscape 格式：7 个字段由 Tab 分隔
		// domain flag path secure expiration name value
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			return fmt.Errorf("line %d: expected 7 tab-separated fields, got %d", i+1, len(fields))
		}

		// 验证 domain 字段不为空
		if fields[0] == "" {
			return fmt.Errorf("line %d: empty domain field", i+1)
		}

		// 验证 expiration 字段是数字
		if _, err := parseUint64(fields[4]); err != nil {
			return fmt.Errorf("line %d: invalid expiration timestamp: %w", i+1, err)
		}

		validLines++
	}

	if validLines == 0 {
		return fmt.Errorf("no valid cookie entries found")
	}

	return nil
}

// isValidDomain 验证域名格式是否正确
func isValidDomain(domain string) bool {
	// 域名正则：仅允许字母、数字、点、连字符
	domainRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?)*$`)
	return domainRegex.MatchString(domain) && len(domain) <= 253
}

// getCookieByDomain 精确匹配获取 Cookie
func (h *CookieHandler) getCookieByDomain(userID int, domain string) (string, error) {
	var content string
	err := h.db.QueryRow(
		"SELECT content FROM user_cookies WHERE user_id = ? AND domain = ?",
		userID, domain,
	).Scan(&content)

	return content, err
}

// GetCookieForDownload 为下载任务获取 Cookie（支持管理员共享）
func (h *CookieHandler) GetCookieForDownload(userID int, role string, urlDomain string) (string, error) {
	var content string

	// 首先尝试获取用户自己的 Cookie
	err := h.db.QueryRow(
		"SELECT content FROM user_cookies WHERE user_id = ? AND domain = ?",
		userID, urlDomain,
	).Scan(&content)

	if err == nil {
		return content, nil
	}

	if err != sql.ErrNoRows {
		return "", err
	}

	// 如果没有，且用户是管理员，尝试获取共享的 Cookie
	if role == "admin" {
		err = h.db.QueryRow(
			"SELECT content FROM user_cookies WHERE is_shared = 1 AND domain = ? LIMIT 1",
			urlDomain,
		).Scan(&content)

		if err == nil {
			return content, nil
		}
	}

	return "", err
}

// 辅助函数

func parseUint64(s string) (uint64, error) {
	val, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid unsigned integer: %w", err)
	}
	return val, nil
}

// getUserFromContext 从 context 获取用户信息
func getUserFromContext(r *http.Request) (int, string, bool) {
	claims, ok := r.Context().Value("claims").(*auth.Claims)
	if !ok {
		return 0, "", false
	}
	return claims.UserID, claims.Role, true
}
