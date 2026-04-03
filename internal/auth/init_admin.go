// Package auth 提供默认管理员账号初始化功能
package auth

import (
	"database/sql"
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
)

// InitDefaultAdmin 初始化默认管理员账号
// 如果数据库中不存在 admin 用户，则创建
func InitDefaultAdmin(db *sql.DB, username, password, email string) error {
	// 检查是否已存在 admin 用户
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check admin user: %w", err)
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

	// 插入管理员账号
	result, err := db.Exec(`
		INSERT INTO users (username, password_hash, email, role, is_initialized)
		VALUES (?, ?, ?, 'admin', 1)
	`, username, string(hashedPassword), email)
	if err != nil {
		return fmt.Errorf("failed to insert admin user: %w", err)
	}

	// 获取插入的 ID
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}

	log.Printf("默认管理员账号创建成功：username=%s, id=%d", username, id)
	log.Printf("警告：请尽快修改默认密码！")

	return nil
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
