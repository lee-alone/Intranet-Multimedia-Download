// Package collector 提供前端资源嵌入功能
// 使用 go:embed 指令将前端构建产物嵌入到 Go 二进制中
//
// 注意：在编译此包之前，需要先构建前端资源
// 运行：npm run build (在 web 目录下)
// 或者运行构建脚本：scripts/build.bat (Windows) 或 scripts/build.sh (Linux)
package collector

import "embed"

// WebFS 嵌入的前端资源文件系统
// 包含构建后的前端静态资源
//
// 注意：web/dist 目录必须存在且包含前端构建产物
// 如果编译失败，请先运行前端构建命令
//go:embed web/dist/index.html
//go:embed web/dist/assets/*
var WebFS embed.FS
