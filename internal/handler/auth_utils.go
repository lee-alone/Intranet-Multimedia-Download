package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/auth"
)

// contextKey 是上下文中存储 claims 的键类型
type contextKey string

// ClaimsContextKey 是上下文中存储 claims 的键
const ClaimsContextKey contextKey = "claims"

// AuthMiddleware JWT 认证中间件
func AuthMiddleware(jwtMgr *auth.JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 放行 OPTIONS 预检请求
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			var tokenString string

			// 优先从 Authorization Header 读取
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				if len(authHeader) < 7 || authHeader[:7] != "Bearer " {
					log.Printf("🚨 [Middleware] Authorization Header 格式错误: %s", authHeader)
					writeError(w, http.StatusUnauthorized, "Invalid authorization header format")
					return
				}
				tokenString = authHeader[7:]
			} else {
				// 降级：从 URL 参数读取
				tokenString = r.URL.Query().Get("token")
				if tokenString == "" {
					writeError(w, http.StatusUnauthorized, "Missing authorization token")
					return
				}
			}

			claims, err := jwtMgr.ValidateToken(tokenString)
			if err != nil {
				log.Printf("🚨 [Middleware] Token 验证失败: %v", err)
				writeError(w, http.StatusUnauthorized, "Invalid or expired token")
				return
			}

			// 将用户信息添加到请求上下文
			ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetClaimsFromContext 从请求上下文中获取用户信息
func GetClaimsFromContext(ctx context.Context) (*auth.Claims, bool) {
	claims, ok := ctx.Value(ClaimsContextKey).(*auth.Claims)
	return claims, ok
}

// writeJSON 写入 JSON 响应
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("🚨 Failed to encode JSON response: %v", err)
	}
}

// writeError 写入错误响应
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{
		"success": false,
		"error":   message,
	})
}

// getClientIP 从请求中获取客户端真实 IP 地址
// 优先级：X-Real-IP > X-Forwarded-For（第一个 IP）> RemoteAddr
func getClientIP(r *http.Request) string {
	// 首先检查 X-Real-IP（通常由反向代理设置）
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}

	// 检查 X-Forwarded-For（可能包含多个 IP，第一个是真实 IP）
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For 格式：client, proxy1, proxy2
		// 取第一个 IP 作为真实 IP
		if idx := len(xff); idx > 0 {
			for i, c := range xff {
				if c == ',' {
					return xff[:i]
				}
			}
			return xff
		}
	}

	// 最后使用 RemoteAddr
	return r.RemoteAddr
}

// int64Ptr 将 int 转换为 *int64
func int64Ptr(i int) *int64 {
	v := int64(i)
	return &v
}

// resourceTypePtr 将 audit.ResourceType 转换为 *audit.ResourceType
func resourceTypePtr(rt audit.ResourceType) *audit.ResourceType {
	return &rt
}
