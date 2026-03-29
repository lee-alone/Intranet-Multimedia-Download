// Package collector 提供前端资源嵌入功能
// 使用 go:embed 指令将前端构建产物嵌入到 Go 二进制中
package collector

import "embed"

// WebFS 嵌入的前端资源文件系统
//go:embed web/dist/*
//go:embed web/dist/**
var WebFS embed.FS
