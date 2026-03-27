// Package auth 提供 Refresh Token 持久化存储功能
package auth

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

var (
	// ErrTokenNotFound 表示令牌未找到
	ErrTokenNotFound = errors.New("token not found")
	// ErrTokenRevoked 表示令牌已被撤销
	ErrTokenRevoked = errors.New("token has been revoked")
)

// TokenStore 管理 Refresh Token 的持久化存储
type TokenStore struct {
	db *sql.DB
}

// NewTokenStore 创建新的令牌存储
func NewTokenStore(db *sql.DB) *TokenStore {
	return &TokenStore{db: db}
}

// StoreRefreshToken 存储刷新令牌
func (s *TokenStore) StoreRefreshToken(userID int, tokenString string, expiresAt time.Time) error {
	// 对令牌进行哈希处理，避免明文存储
	tokenHash := hashToken(tokenString)

	_, err := s.db.Exec(
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		userID, tokenHash, expiresAt,
	)

	return err
}

// ValidateRefreshToken 验证刷新令牌是否有效
func (s *TokenStore) ValidateRefreshToken(userID int, tokenString string) error {
	tokenHash := hashToken(tokenString)

	var expiresAt time.Time
	err := s.db.QueryRow(
		`SELECT expires_at FROM refresh_tokens WHERE user_id = ? AND token_hash = ?`,
		userID, tokenHash,
	).Scan(&expiresAt)

	if err == sql.ErrNoRows {
		return ErrTokenNotFound
	}
	if err != nil {
		return err
	}

	// 检查令牌是否过期
	if time.Now().After(expiresAt) {
		return ErrExpiredToken
	}

	return nil
}

// RevokeRefreshToken 撤销刷新令牌
func (s *TokenStore) RevokeRefreshToken(userID int, tokenString string) error {
	tokenHash := hashToken(tokenString)

	result, err := s.db.Exec(
		`DELETE FROM refresh_tokens WHERE user_id = ? AND token_hash = ?`,
		userID, tokenHash,
	)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrTokenNotFound
	}

	return nil
}

// RevokeAllUserTokens 撤销用户的所有刷新令牌
func (s *TokenStore) RevokeAllUserTokens(userID int) error {
	_, err := s.db.Exec(`DELETE FROM refresh_tokens WHERE user_id = ?`, userID)
	return err
}

// CleanupExpiredTokens 清理过期的令牌
func (s *TokenStore) CleanupExpiredTokens() (int64, error) {
	result, err := s.db.Exec(`DELETE FROM refresh_tokens WHERE expires_at < ?`, time.Now())
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// hashToken 对令牌进行 SHA-256 哈希
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
