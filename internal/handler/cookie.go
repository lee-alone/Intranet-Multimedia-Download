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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🚨 [SaveCookie] 捕获到 Panic: %v", r)
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

	userID, role, ok := getUserFromContext(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, SaveCookieResponse{
			Success: false,
			Error:   "Missing user context",
		})
		return
	}

	var req SaveCookieRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("🚨 [SaveCookie] 请求体解析失败: %v", err)
		writeJSON(w, http.StatusBadRequest, SaveCookieResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid request body: %v", err),
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

	// 统一处理：清洗、验证、域名一致性校验、注入标准头部
	processedContent, err := processAndValidateCookie(decryptedContent, req.Domain)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, SaveCookieResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// 判断是否共享（仅管理员可设置共享）
	isShared := false
	if req.IsShared {
		if role != "admin" {
			writeJSON(w, http.StatusForbidden, SaveCookieResponse{
				Success: false,
				Error:   "Permission denied: only administrators can set shared cookies",
			})
			return
		}
		isShared = true
	}

	// 保存到数据库
	result, err := h.db.Exec(`
		INSERT INTO user_cookies (user_id, domain, content, is_shared, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id, domain)
		DO UPDATE SET content = excluded.content, updated_at = CURRENT_TIMESTAMP
	`, userID, req.Domain, processedContent, isShared)

	if err != nil {
		log.Printf("🚨 [SaveCookie] 数据库执行失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, SaveCookieResponse{
			Success: false,
			Error:   fmt.Sprintf("数据库保存失败: %v", err),
		})
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("🚨 [SaveCookie] 获取影响行数失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, SaveCookieResponse{
			Success: false,
			Error:   "保存验证失败",
		})
		return
	}
	log.Printf("✅ [SaveCookie] 保存成功, UserID: %d, Domain: %s, 影响行数: %d", userID, req.Domain, rowsAffected)

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
			log.Printf("🚨 [ListCookies] Scan 失败: %v", err)
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
	// 清洗 Base64 字符串中的空白字符
	cleanData := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, encryptedDataBase64)

	encryptedData, err := base64.StdEncoding.DecodeString(cleanData)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	dataLen := len(encryptedData)

	if dataLen < 2 {
		return "", fmt.Errorf("密文数据过短，无法解析长度字段")
	}

	// 读取 RSA 加密的 AES Key 长度
	aesKeyLen := int(encryptedData[0])<<8 | int(encryptedData[1])

	if aesKeyLen <= 0 || aesKeyLen > 512 {
		return "", fmt.Errorf("密文格式错误: AES Key 长度 %d 超出合理范围", aesKeyLen)
	}

	minRequiredLen := 2 + aesKeyLen + 12 + 16
	if dataLen < minRequiredLen {
		return "", fmt.Errorf("密文数据不完整: 需要至少 %d 字节，实际 %d 字节", minRequiredLen, dataLen)
	}

	if 2+aesKeyLen > dataLen {
		return "", fmt.Errorf("密文数据损坏，无法安全提取 AES Key")
	}

	encryptedAESKey := encryptedData[2 : 2+aesKeyLen]
	aesGCMData := encryptedData[2+aesKeyLen:]

	// 使用 RSA 私钥解密 AES Key (OAEP with SHA-256)
	aesKey, err := rsa.DecryptOAEP(
		sha256.New(),
		rand.Reader,
		h.jwtMgr.GetPrivateKey(),
		encryptedAESKey,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt AES key: %w", err)
	}

	// 使用 AES-GCM 解密内容
	decryptedContent, err := decryptAESGCM(aesKey, aesGCMData)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt content with AES-GCM: %w", err)
	}

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
	claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	if !ok {
		return 0, "", false
	}
	return claims.UserID, claims.Role, true
}

// processAndValidateCookie 统一处理 Cookie 内容：
// 1. 清洗注释行和空行
// 2. 验证 Netscape 格式（7 字段制表符分隔）
// 3. 域名一致性校验（确保上传内容与请求域名匹配）
// 4. 注入标准头部 # Netscape HTTP Cookie File
// 5. 规范化换行符（\r\n -> \n）
func processAndValidateCookie(content, expectedDomain string) (string, error) {
	// 规范化换行符
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	lines := strings.Split(content, "\n")
	var validLines []string
	var domainCounts map[string]int
	domainCounts = make(map[string]int)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 跳过空行和注释行
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// 验证 7 字段结构
		fields := strings.Split(trimmed, "\t")
		if len(fields) != 7 {
			return "", fmt.Errorf("格式错误：应为 7 个制表符分隔字段，实际 %d 个", len(fields))
		}

		// 提取并验证域名
		domain := strings.TrimSpace(fields[0])
		if domain == "" {
			return "", fmt.Errorf("格式错误：域名为空")
		}

		// 验证过期时间
		if _, err := strconv.ParseInt(strings.TrimSpace(fields[4]), 10, 64); err != nil {
			return "", fmt.Errorf("格式错误：第 %d 行过期时间无效", len(validLines)+1)
		}

		// 记录域名分布
		cleanDomain := strings.TrimPrefix(domain, ".")
		domainCounts[cleanDomain]++

		validLines = append(validLines, trimmed)
	}

	if len(validLines) == 0 {
		return "", fmt.Errorf("未找到有效的 Cookie 条目")
	}

	// 域名一致性校验：检查域名是否与请求域名匹配（支持双向匹配）
	expectedClean := strings.TrimPrefix(strings.ToLower(expectedDomain), ".")
	var matchedCount int
	for domain, count := range domainCounts {
		if isDomainRelated(domain, expectedClean) {
			matchedCount += count
		}
	}

	totalCount := len(validLines)
	if matchedCount == 0 {
		return "", fmt.Errorf("域名不匹配：上传内容中未找到与 %q 相关的 Cookie 条目", expectedDomain)
	}

	// 如果匹配的条目不足 50%，发出警告
	if matchedCount < totalCount/2 {
		domainSummary := make([]string, 0, len(domainCounts))
		for d, c := range domainCounts {
			domainSummary = append(domainSummary, fmt.Sprintf("%s (%d 条)", d, c))
		}
		return "", fmt.Errorf("域名不匹配：上传内容中仅 %d/%d 条 Cookie 与 %q 相关，可能包含其他站点数据 (%s)",
			matchedCount, totalCount, expectedDomain, strings.Join(domainSummary, ", "))
	}

	// 过滤：仅保留与请求域名相关的条目
	filteredLines := make([]string, 0, matchedCount)
	for _, line := range validLines {
		fields := strings.Split(line, "\t")
		domain := strings.TrimPrefix(strings.TrimSpace(fields[0]), ".")
		if isDomainRelated(domain, expectedClean) {
			filteredLines = append(filteredLines, line)
		}
	}

	// 注入标准头部并拼接
	result := "# Netscape HTTP Cookie File\n" + strings.Join(filteredLines, "\n")

	return result, nil
}

// isDomainRelated 判断两个域名是否相关（支持双向匹配）
// 场景说明：
// 1. 精确匹配：bilibili.com == bilibili.com
// 2. 子域名 → 父域名：www.bilibili.com 匹配 bilibili.com
// 3. 父域名 → 子域名：bilibili.com 匹配 www.bilibili.com
//    （父域名的 Cookie 通常对所有子域名有效）
func isDomainRelated(domain, expected string) bool {
	if domain == expected {
		return true
	}
	// 子域名匹配父域名（如 www.bilibili.com 匹配 bilibili.com）
	if strings.HasSuffix(domain, "."+expected) {
		return true
	}
	// 父域名匹配子域名（如 bilibili.com 匹配 www.bilibili.com）
	// 注意：仅当 expected 是 domain 的子域名时才匹配
	if strings.HasSuffix(expected, "."+domain) {
		return true
	}
	return false
}
