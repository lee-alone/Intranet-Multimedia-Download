package main

import (
	"fmt"
	"strings"
)

func extractNumbers(s string) []string {
	var numbers []string
	var current string
	
	for _, r := range s {
		if r >= '0' && r <= '9' {
			current += string(r)
		} else {
			if current != "" {
				numbers = append(numbers, current)
				current = ""
			}
		}
	}
	if current != "" {
		numbers = append(numbers, current)
	}
	return numbers
}

func matchNumbers(nums1, nums2 []string) bool {
	if len(nums1) == 0 || len(nums2) == 0 {
		return false
	}
	
	matchCount := 0
	for _, n1 := range nums1 {
		for _, n2 := range nums2 {
			if n1 == n2 {
				matchCount++
				break
			}
		}
	}
	
	minLen := len(nums1)
	if len(nums2) < minLen {
		minLen = len(nums2)
	}
	
	return matchCount >= 2 || matchCount >= minLen
}

func main() {
	// 数据库中的乱码文件名
	dbFilePath := `F:\У԰Դɼϵͳ\release\collector_1.0.0_windows_amd64\downloads\2Կڴ˫˫նԿձ324.mp4`
	
	// 实际文件名
	realFileName := "2 月显卡和内存出货量双双腰斩？显卡日报 3 月 24 日.mp4"
	
	dbBaseName := dbFilePath[strings.LastIndex(dbFilePath, `\`)+1:]
	
	dbNumbers := extractNumbers(dbBaseName)
	realNumbers := extractNumbers(realFileName)
	
	fmt.Printf("DB filename: %s\n", dbBaseName)
	fmt.Printf("DB numbers: %v\n", dbNumbers)
	fmt.Println("---")
	fmt.Printf("Real filename: %s\n", realFileName)
	fmt.Printf("Real numbers: %v\n", realNumbers)
	fmt.Println("---")
	fmt.Printf("Match result: %v\n", matchNumbers(dbNumbers, realNumbers))
}
