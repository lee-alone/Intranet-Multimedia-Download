// Package main 提供 JWT RS256 密钥对生成工具
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// 默认密钥大小（位）
	defaultKeySize = 2048
	// 默认输出目录
	defaultOutputDir = "./keys"
	// 默认私钥文件名
	defaultPrivateKeyFile = "private.pem"
	// 默认公钥文件名
	defaultPublicKeyFile = "public.pem"
)

func main() {
	fmt.Println("=== JWT RS256 密钥对生成工具 ===")
	fmt.Println()

	// 解析命令行参数
	outputDir := defaultOutputDir
	privateKeyFile := defaultPrivateKeyFile
	publicKeyFile := defaultPublicKeyFile
	keySize := defaultKeySize

	// 检查命令行参数
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o", "--output":
			if i+1 < len(args) {
				outputDir = args[i+1]
				i++
			}
		case "-s", "--size":
			if i+1 < len(args) {
				size, err := parseInt(args[i+1])
				if err != nil {
					fmt.Printf("错误: 无效的密钥大小: %s\n", args[i+1])
					os.Exit(1)
				}
				if size < 2048 {
					fmt.Println("警告: 密钥大小小于 2048 位可能不安全，已自动调整为 2048 位")
					size = 2048
				}
				keySize = size
				i++
			}
		case "-h", "--help":
			printUsage()
			os.Exit(0)
		}
	}

	// 创建输出目录
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		fmt.Printf("错误: 无法创建输出目录: %v\n", err)
		os.Exit(1)
	}

	// 生成 RSA 密钥对
	fmt.Printf("正在生成 %d 位 RSA 密钥对...\n", keySize)
	privateKey, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		fmt.Printf("错误: 无法生成密钥对: %v\n", err)
		os.Exit(1)
	}

	// 导出私钥
	privateKeyPath := filepath.Join(outputDir, privateKeyFile)
	if err := exportPrivateKey(privateKey, privateKeyPath); err != nil {
		fmt.Printf("错误: 无法导出私钥: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("私钥已保存至: %s\n", privateKeyPath)

	// 导出公钥
	publicKeyPath := filepath.Join(outputDir, publicKeyFile)
	if err := exportPublicKey(&privateKey.PublicKey, publicKeyPath); err != nil {
		fmt.Printf("错误: 无法导出公钥: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("公钥已保存至: %s\n", publicKeyPath)

	// 设置文件权限（仅所有者可读写）
	if err := os.Chmod(privateKeyPath, 0600); err != nil {
		fmt.Printf("警告: 无法设置私钥文件权限: %v\n", err)
	}
	if err := os.Chmod(publicKeyPath, 0644); err != nil {
		fmt.Printf("警告: 无法设置公钥文件权限: %v\n", err)
	}

	fmt.Println()
	fmt.Println("=== 密钥对生成完成 ===")
	fmt.Println()
	fmt.Println("重要提示:")
	fmt.Println("1. 请妥善保管私钥文件，不要泄露或提交到版本控制系统")
	fmt.Println("2. 公钥文件可以分发给需要验证 JWT 的服务")
	fmt.Println("3. 在 config.yaml 中配置密钥文件路径:")
	fmt.Printf("   auth:\n")
	fmt.Printf("     private_key: \"%s\"\n", privateKeyPath)
	fmt.Printf("     public_key: \"%s\"\n", publicKeyPath)
	fmt.Println()
	fmt.Println("安全建议:")
	fmt.Println("- 生产环境建议使用 4096 位密钥")
	fmt.Println("- 定期轮换密钥对")
	fmt.Println("- 将私钥文件添加到 .gitignore")
}

// exportPrivateKey 导出 RSA 私钥到 PEM 格式文件
func exportPrivateKey(privateKey *rsa.PrivateKey, filename string) error {
	// 将私钥序列化为 PKCS#1 DER 格式
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)

	// 创建 PEM 块
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}

	// 创建文件
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("无法创建文件: %w", err)
	}
	defer file.Close()

	// 写入 PEM 数据
	if err := pem.Encode(file, privateKeyPEM); err != nil {
		return fmt.Errorf("无法写入 PEM 数据: %w", err)
	}

	return nil
}

// exportPublicKey 导出 RSA 公钥到 PEM 格式文件
func exportPublicKey(publicKey *rsa.PublicKey, filename string) error {
	// 将公钥序列化为 PKIX DER 格式
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return fmt.Errorf("无法序列化公钥: %w", err)
	}

	// 创建 PEM 块
	publicKeyPEM := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}

	// 创建文件
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("无法创建文件: %w", err)
	}
	defer file.Close()

	// 写入 PEM 数据
	if err := pem.Encode(file, publicKeyPEM); err != nil {
		return fmt.Errorf("无法写入 PEM 数据: %w", err)
	}

	return nil
}

// parseInt 解析整数
func parseInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

// printUsage 打印使用说明
func printUsage() {
	fmt.Println("用法: keygen [选项]")
	fmt.Println()
	fmt.Println("选项:")
	fmt.Println("  -o, --output <目录>   输出目录 (默认: ./keys)")
	fmt.Println("  -s, --size <位数>     密钥大小，最小 2048 (默认: 2048)")
	fmt.Println("  -h, --help            显示帮助信息")
	fmt.Println()
	fmt.Println("示例:")
	fmt.Println("  keygen                          # 使用默认设置生成密钥对")
	fmt.Println("  keygen -o /etc/app/keys         # 指定输出目录")
	fmt.Println("  keygen -s 4096                  # 生成 4096 位密钥对")
	fmt.Println("  keygen -o ./keys -s 4096        # 组合使用")
}
