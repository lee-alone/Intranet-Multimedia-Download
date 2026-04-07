// Package auth 提供默认用户账号初始化功能
package auth

import (
	"database/sql"
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
)

// InitUser 初始化默认用户账号
// 如果指定用户名的用户不存在，则创建
func InitUser(db *sql.DB, username, password, email, role string) error {
	// 检查是否已存在该用户
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check user: %w", err)
	}

	// 如果已存在，跳过创建
	if count > 0 {
		return nil
	}

	// 生成密码哈希
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// 插入用户账号
	result, err := db.Exec(`
		INSERT INTO users (username, password_hash, email, role, is_initialized)
		VALUES (?, ?, ?, ?, 1)
	`, username, string(hashedPassword), email, role)
	if err != nil {
		return fmt.Errorf("failed to insert user: %w", err)
	}

	// 获取插入的 ID
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}

	log.Printf("默认用户账号创建成功：username=%s, role=%s, id=%d", username, role, id)
	if role == "admin" {
		log.Printf("警告：请尽快修改默认管理员密码！")
	}

	return nil
}

// InitDefaultAdmin 初始化默认管理员账号（向后兼容）
// 如果数据库中不存在 admin 用户，则创建
func InitDefaultAdmin(db *sql.DB, username, password, email string) error {
	return InitUser(db, username, password, email, "admin")
}

// CheckAdminExists 检查管理员账号是否存在
func CheckAdminExists(db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
