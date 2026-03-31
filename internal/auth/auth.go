// Package auth 提供 JWT 认证和授权功能
package auth

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrInvalidToken 表示令牌无效
	ErrInvalidToken = errors.New("invalid token")
	// ErrExpiredToken 表示令牌已过期
	ErrExpiredToken = errors.New("token expired")
	// ErrMissingKey 表示缺少密钥
	ErrMissingKey = errors.New("missing signing key")
)

// Claims 表示 JWT 令牌的声明
type Claims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// TokenPair 表示访问令牌和刷新令牌对
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // 秒
}

// JWTManager 管理 JWT 令牌
type JWTManager struct {
	privateKey    *rsa.PrivateKey
	publicKey     *rsa.PublicKey
	expiry        time.Duration
	refreshExpiry time.Duration
}

// NewJWTManager 创建新的 JWT 管理器
func NewJWTManager(privateKeyPath, publicKeyPath string, expiry, refreshExpiry int) (*JWTManager, error) {
	log.Printf("Loading JWT keys: private_key=%s, public_key=%s", privateKeyPath, publicKeyPath)
	
	// 读取私钥
	privateKeyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}
	log.Printf("Private key loaded, size: %d bytes", len(privateKeyData))

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// 读取公钥
	publicKeyData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key: %w", err)
	}
	log.Printf("Public key loaded, size: %d bytes", len(publicKeyData))

	publicKey, err := jwt.ParseRSAPublicKeyFromPEM(publicKeyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	log.Printf("JWT keys loaded successfully")
	return &JWTManager{
		privateKey:    privateKey,
		publicKey:     publicKey,
		expiry:        time.Duration(expiry) * time.Minute,
		refreshExpiry: time.Duration(refreshExpiry) * time.Minute,
	}, nil
}

// GenerateToken 生成访问令牌和刷新令牌
func (m *JWTManager) GenerateToken(userID int, username, role string) (*TokenPair, error) {
	now := time.Now()

	// 生成访问令牌
	accessClaims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(m.expiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "campus-collector",
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodRS256, accessClaims)
	accessStr, err := accessToken.SignedString(m.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// 生成刷新令牌
	refreshClaims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(m.refreshExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "campus-collector",
		},
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodRS256, refreshClaims)
	refreshStr, err := refreshToken.SignedString(m.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		ExpiresIn:    int64(m.expiry.Seconds()),
	}, nil
}

// ValidateToken 验证访问令牌
func (m *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.publicKey, nil
	})

	if err != nil {
		// 输出详细错误信息用于调试
		log.Printf("JWT parse error: %v", err)
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		log.Printf("JWT claims error: ok=%v, valid=%v", ok, token.Valid)
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// RefreshAccessToken 使用刷新令牌生成新的访问令牌
func (m *JWTManager) RefreshAccessToken(refreshTokenString string) (*TokenPair, error) {
	claims, err := m.ValidateToken(refreshTokenString)
	if err != nil {
		return nil, err
	}

	return m.GenerateToken(claims.UserID, claims.Username, claims.Role)
}

// GetRefreshExpiry 获取刷新令牌过期时间（分钟）
func (m *JWTManager) GetRefreshExpiry() int {
	return int(m.refreshExpiry.Minutes())
}

// GetExpiry 获取访问令牌过期时间（分钟）
func (m *JWTManager) GetExpiry() int {
	return int(m.expiry.Minutes())
}
