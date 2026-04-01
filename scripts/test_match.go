package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

func sanitizeFilename(filename string) string {
	filename = filepath.Base(filename)
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		filename = filename[:idx]
	}

	filename = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return '_'
		}
		if strings.ContainsRune("<>:\"/\\|？*", r) {
			return '_'
		}
		return r
	}, filename)

	return strings.TrimSpace(filename)
}

func main() {
	// 数据库中的乱码文件名
	dbFilePath := `F:\У԰Դɼϵͳ\release\collector_1.0.0_windows_amd64\downloads\2Կڴ˫˫նԿձ324.mp4`
	
	// 实际文件名
	realFileName := "2 月显卡和内存出货量双双腰斩？显卡日报 3 月 24 日.mp4"
	
	dbBaseName := filepath.Base(dbFilePath)
	dbNameWithoutExt := strings.TrimSuffix(dbBaseName, filepath.Ext(dbBaseName))
	
	realNameWithoutExt := strings.TrimSuffix(realFileName, filepath.Ext(realFileName))
	
	cleanDbName := sanitizeFilename(dbNameWithoutExt)
	cleanRealName := sanitizeFilename(realNameWithoutExt)
	
	fmt.Printf("DB filename: %s\n", dbBaseName)
	fmt.Printf("DB filename (clean): %s\n", cleanDbName)
	fmt.Printf("DB filename (bytes): %v\n", []byte(dbNameWithoutExt))
	fmt.Println("---")
	fmt.Printf("Real filename: %s\n", realFileName)
	fmt.Printf("Real filename (clean): %s\n", cleanRealName)
	fmt.Printf("Real filename (bytes): %v\n", []byte(realNameWithoutExt))
	fmt.Println("---")
	
	// 尝试匹配
	fmt.Printf("Direct match: %v\n", cleanRealName == cleanDbName)
	fmt.Printf("Contains (real contains db): %v\n", strings.Contains(cleanRealName, cleanDbName))
	fmt.Printf("Contains (db contains real): %v\n", strings.Contains(cleanDbName, cleanRealName))
	
	// 尝试部分匹配
	if len(cleanRealName) >= 10 && len(cleanDbName) >= 10 {
		shortReal := cleanRealName[:10]
		shortDb := cleanDbName[:10]
		fmt.Printf("Short match (first 10): %v\n", shortReal == shortDb)
		fmt.Printf("Short real: '%s'\n", shortReal)
		fmt.Printf("Short db: '%s'\n", shortDb)
	}
}
