// Package auth 提供 Token Store 的测试
package auth

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestTokenStore(t *testing.T) {
	// 创建内存数据库
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// 创建测试表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS refresh_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	store := NewTokenStore(db)

	t.Run("StoreRefreshToken", func(t *testing.T) {
		err := store.StoreRefreshToken(1, "test-token-1", time.Now().Add(time.Hour))
		if err != nil {
			t.Errorf("StoreRefreshToken() error = %v", err)
		}
	})

	t.Run("ValidateRefreshToken_Valid", func(t *testing.T) {
		// 先存储一个令牌
		err := store.StoreRefreshToken(2, "test-token-2", time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Failed to store token: %v", err)
		}

		// 验证令牌
		err = store.ValidateRefreshToken(2, "test-token-2")
		if err != nil {
			t.Errorf("ValidateRefreshToken() error = %v", err)
		}
	})

	t.Run("ValidateRefreshToken_NotFound", func(t *testing.T) {
		err := store.ValidateRefreshToken(999, "nonexistent-token")
		if err != ErrTokenNotFound {
			t.Errorf("ValidateRefreshToken() error = %v, want %v", err, ErrTokenNotFound)
		}
	})

	t.Run("ValidateRefreshToken_Expired", func(t *testing.T) {
		// 存储一个已过期的令牌
		err := store.StoreRefreshToken(3, "expired-token", time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatalf("Failed to store token: %v", err)
		}

		err = store.ValidateRefreshToken(3, "expired-token")
		if err != ErrExpiredToken {
			t.Errorf("ValidateRefreshToken() error = %v, want %v", err, ErrExpiredToken)
		}
	})

	t.Run("RevokeRefreshToken", func(t *testing.T) {
		// 先存储一个令牌
		err := store.StoreRefreshToken(4, "revoke-token", time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Failed to store token: %v", err)
		}

		// 撤销令牌
		err = store.RevokeRefreshToken(4, "revoke-token")
		if err != nil {
			t.Errorf("RevokeRefreshToken() error = %v", err)
		}

		// 验证令牌已被撤销
		err = store.ValidateRefreshToken(4, "revoke-token")
		if err != ErrTokenNotFound {
			t.Errorf("ValidateRefreshToken() after revoke error = %v, want %v", err, ErrTokenNotFound)
		}
	})

	t.Run("RevokeRefreshToken_NotFound", func(t *testing.T) {
		err := store.RevokeRefreshToken(999, "nonexistent-token")
		if err != ErrTokenNotFound {
			t.Errorf("RevokeRefreshToken() error = %v, want %v", err, ErrTokenNotFound)
		}
	})

	t.Run("RevokeAllUserTokens", func(t *testing.T) {
		// 存储多个令牌
		err := store.StoreRefreshToken(5, "token-1", time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Failed to store token: %v", err)
		}
		err = store.StoreRefreshToken(5, "token-2", time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Failed to store token: %v", err)
		}

		// 撤销所有令牌
		err = store.RevokeAllUserTokens(5)
		if err != nil {
			t.Errorf("RevokeAllUserTokens() error = %v", err)
		}

		// 验证令牌已被撤销
		err = store.ValidateRefreshToken(5, "token-1")
		if err != ErrTokenNotFound {
			t.Errorf("ValidateRefreshToken() after revoke all error = %v, want %v", err, ErrTokenNotFound)
		}
	})

	t.Run("CleanupExpiredTokens", func(t *testing.T) {
		// 使用新的用户ID避免与之前的测试冲突
		userID := 100

		// 存储一些过期和未过期的令牌
		store.StoreRefreshToken(userID, "expired-1", time.Now().Add(-time.Hour))
		store.StoreRefreshToken(userID, "expired-2", time.Now().Add(-time.Hour))
		store.StoreRefreshToken(userID, "valid-1", time.Now().Add(time.Hour))

		// 清理过期令牌
		count, err := store.CleanupExpiredTokens()
		if err != nil {
			t.Errorf("CleanupExpiredTokens() error = %v", err)
		}
		// 注意：清理会删除所有过期的令牌，包括之前测试中的
		if count < 2 {
			t.Errorf("CleanupExpiredTokens() count = %d, want at least 2", count)
		}

		// 验证有效令牌仍然存在
		err = store.ValidateRefreshToken(userID, "valid-1")
		if err != nil {
			t.Errorf("ValidateRefreshToken() for valid token error = %v", err)
		}
	})
}

func TestHashToken(t *testing.T) {
	token := "test-token"
	hash1 := hashToken(token)
	hash2 := hashToken(token)

	// 相同令牌应该产生相同的哈希
	if hash1 != hash2 {
		t.Error("hashToken() should produce consistent hashes")
	}

	// 不同令牌应该产生不同的哈希
	hash3 := hashToken("different-token")
	if hash1 == hash3 {
		t.Error("hashToken() should produce different hashes for different tokens")
	}

	// 哈希应该是 64 字符的十六进制字符串（SHA-256）
	if len(hash1) != 64 {
		t.Errorf("hashToken() hash length = %d, want 64", len(hash1))
	}
}

func TestErrorDefinitions_TokenStore(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{
			name: "ErrTokenNotFound",
			err:  ErrTokenNotFound,
			msg:  "token not found",
		},
		{
			name: "ErrTokenRevoked",
			err:  ErrTokenRevoked,
			msg:  "token has been revoked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.msg {
				t.Errorf("%s error message = %v, want %v", tt.name, tt.err.Error(), tt.msg)
			}
		})
	}
}
