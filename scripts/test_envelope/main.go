package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log"
)

// 这个脚本用于测试信封加密/解密流程，帮助定位问题
func main() {
	log.Println("🔍 开始测试信封加密/解密流程...")

	// 1. 生成 RSA 密钥对（仅用于测试）
	log.Println("📝 生成 RSA-2048 密钥对...")
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("❌ 生成 RSA 私钥失败: %v", err)
	}
	publicKey := &privateKey.PublicKey

	// 2. 准备测试数据（模拟 Cookie 内容）
	plaintext := `# Netscape HTTP Cookie File
.baidu.com	TRUE	/	FALSE	1743638400	BAIDUID	ABC123DEF456
.baidu.com	TRUE	/	FALSE	1743638400	BDUSS	XYZ789`

	log.Printf("📝 测试明文数据长度: %d 字节", len(plaintext))

	// 3. 前端加密流程（模拟）
	log.Println("🔐 开始前端加密流程...")

	// 生成随机 AES-256 Key
	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		log.Fatalf("❌ 生成 AES Key 失败: %v", err)
	}
	log.Printf("📝 生成 AES-256 Key: %d 字节", len(aesKey))

	// 使用 AES-GCM 加密明文
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		log.Fatalf("❌ 创建 AES Cipher 失败: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		log.Fatalf("❌ 创建 GCM 失败: %v", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		log.Fatalf("❌ 生成 Nonce 失败: %v", err)
	}

	// GCM 加密：[Nonce][Ciphertext][Auth Tag]
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	aesGCMData := append(nonce, ciphertext...)
	log.Printf("📝 AES-GCM 加密完成，密文长度: %d 字节", len(aesGCMData))

	// 使用 RSA 公钥加密 AES Key (OAEP with SHA-256)
	encryptedAESKey, err := rsa.EncryptOAEP(
		sha256.New(),
		rand.Reader,
		publicKey,
		aesKey,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ RSA 加密 AES Key 失败: %v", err)
	}
	log.Printf("📝 RSA 加密完成，密文长度: %d 字节", len(encryptedAESKey))

	// 组合信封数据：[2字节长度][RSA 密文][AES-GCM 数据]
	envelope := make([]byte, 0, 2+len(encryptedAESKey)+len(aesGCMData))
	envelope = append(envelope, byte(len(encryptedAESKey)>>8))
	envelope = append(envelope, byte(len(encryptedAESKey)&0xFF))
	envelope = append(envelope, encryptedAESKey...)
	envelope = append(envelope, aesGCMData...)

	log.Printf("📝 信封组合完成，总长度: %d 字节", len(envelope))

	// Base64 编码
	encodedData := base64.StdEncoding.EncodeToString(envelope)
	log.Printf("📝 Base64 编码完成，长度: %d 字节", len(encodedData))

	// 4. 后端解密流程（模拟）
	log.Println("\n🔓 开始后端解密流程...")

	// Base64 解码
	decodedData, err := base64.StdEncoding.DecodeString(encodedData)
	if err != nil {
		log.Fatalf("❌ Base64 解码失败: %v", err)
	}
	log.Printf("✅ Base64 解码成功，长度: %d 字节", len(decodedData))

	// 检查长度
	if len(decodedData) < 2 {
		log.Fatalf("❌ 数据过短，无法读取长度字段")
	}

	// 读取 AES Key 长度
	aesKeyLen := int(decodedData[0])<<8 | int(decodedData[1])
	log.Printf("📝 从数据中读取 AES Key 长度: %d", aesKeyLen)

	// 完整性检查
	minRequiredLen := 2 + aesKeyLen + 12 + 16 // 2字节长度 + RSA密文 + GCM Nonce + GCM Tag
	if len(decodedData) < minRequiredLen {
		log.Fatalf("❌ 密文完整性校验失败: 需要至少 %d 字节，实际 %d 字节",
			minRequiredLen, len(decodedData))
	}

	// 提取数据
	extractedAESKey := decodedData[2 : 2+aesKeyLen]
	extractedGCMData := decodedData[2+aesKeyLen:]
	log.Printf("✅ 提取数据成功: encryptedAESKey=%d 字节, aesGCMData=%d 字节",
		len(extractedAESKey), len(extractedGCMData))

	// RSA 解密 AES Key
	decryptedAESKey, err := rsa.DecryptOAEP(
		sha256.New(),
		rand.Reader,
		privateKey,
		extractedAESKey,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ RSA 解密 AES Key 失败: %v", err)
	}
	log.Printf("✅ RSA 解密成功，AES Key 长度: %d", len(decryptedAESKey))

	// AES-GCM 解密
	block2, err := aes.NewCipher(decryptedAESKey)
	if err != nil {
		log.Fatalf("❌ 创建 AES Cipher 失败: %v", err)
	}

	gcm2, err := cipher.NewGCM(block2)
	if err != nil {
		log.Fatalf("❌ 创建 GCM 失败: %v", err)
	}

	nonceSize := gcm2.NonceSize()
	if len(extractedGCMData) < nonceSize {
		log.Fatalf("❌ 密文过短: 需要至少 %d 字节，实际 %d 字节", nonceSize, len(extractedGCMData))
	}

	nonce2, ciphertext2 := extractedGCMData[:nonceSize], extractedGCMData[nonceSize:]
	decryptedPlaintext, err := gcm2.Open(nil, nonce2, ciphertext2, nil)
	if err != nil {
		log.Fatalf("❌ AES-GCM 解密失败: %v", err)
	}

	log.Printf("✅ AES-GCM 解密成功，明文长度: %d", len(decryptedPlaintext))

	// 5. 验证解密结果
	log.Println("\n🔍 验证解密结果...")
	if string(decryptedPlaintext) == plaintext {
		log.Println("✅✅✅ 解密成功！明文数据与原始数据完全一致！")
		log.Println("\n📋 解密后的内容预览:")
		fmt.Println(string(decryptedPlaintext))
	} else {
		log.Println("❌ 解密失败！明文数据与原始数据不一致")
		log.Printf("   原始长度: %d, 解密长度: %d", len(plaintext), len(decryptedPlaintext))
	}

	// 6. 输出公钥（用于前端测试）
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		log.Fatalf("❌ 序列化公钥失败: %v", err)
	}

	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	log.Println("\n📋 测试用公钥 (PEM 格式):")
	fmt.Println(string(pubKeyPEM))

	log.Println("\n✅ 所有测试完成！")
}
