// Package handler 提供 HTTP 请求处理器
package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// OrphanResourceResponse 孤儿资源响应
type OrphanResourceResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message,omitempty"`
	Data    *OrphanResourceData `json:"data,omitempty"`
}

// OrphanResourceData 孤儿资源数据
type OrphanResourceData struct {
	TotalFiles   int                `json:"total_files"`
	TotalSize    int64              `json:"total_size"`
	TotalSizeMB  float64            `json:"total_size_mb"`
	Resources    []OrphanResourceItem `json:"resources"`
	ScannedPaths []string           `json:"scanned_paths"`
}

// OrphanResourceItem 孤儿资源项
type OrphanResourceItem struct {
	FilePath  string  `json:"file_path"`
	FileName  string  `json:"file_name"`
	FileSize  int64   `json:"file_size"`
	FileSizeMB float64 `json:"file_size_mb"`
	CreatedAt string  `json:"created_at"`
	Reason    string  `json:"reason"`
}

// DeleteOrphanResourceRequest 删除孤儿资源请求
type DeleteOrphanResourceRequest struct {
	FilePaths []string `json:"file_paths"`
}

// DeleteOrphanResourceResponse 删除孤儿资源响应
type DeleteOrphanResourceResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message,omitempty"`
	Data    *DeleteResult `json:"data,omitempty"`
}

// DeleteResult 删除结果
type DeleteResult struct {
	DeletedCount int      `json:"deleted_count"`
	DeletedSize  int64    `json:"deleted_size"`
	DeletedSizeMB float64 `json:"deleted_size_mb"`
	FailedFiles  []FailedFile `json:"failed_files,omitempty"`
}

// FailedFile 删除失败的文件
type FailedFile struct {
	FilePath string `json:"file_path"`
	Reason   string `json:"reason"`
}

// CleanupAllOrphanResourcesResponse 清理所有孤儿资源响应
type CleanupAllOrphanResourcesResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message,omitempty"`
	Data    *DeleteResult `json:"data,omitempty"`
}

// GetOrphanResources 获取孤儿资源列表
// GET /api/v1/resources/orphan
// 孤儿资源定义：存在于 outputDir 中，但在 tasks 表中没有对应 file_path 记录的文件
func (h *TaskHandler) GetOrphanResources(w http.ResponseWriter, r *http.Request) {
	// 仅允许管理员访问
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	if claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "仅管理员可访问")
		return
	}

	// 查询数据库中所有已记录的文件路径
	rows, err := h.db.Query(`SELECT DISTINCT file_path FROM tasks WHERE file_path IS NOT NULL AND file_path != ''`)
	if err != nil {
		log.Printf("查询任务文件路径失败：%v", err)
		writeError(w, http.StatusInternalServerError, "查询数据库失败")
		return
	}
	defer rows.Close()

	recordedFiles := make(map[string]bool)
	for rows.Next() {
		var filePath string
		if err := rows.Scan(&filePath); err != nil {
			continue
		}
		recordedFiles[filePath] = true
	}

	if err := rows.Err(); err != nil {
		log.Printf("读取任务文件路径失败：%v", err)
		writeError(w, http.StatusInternalServerError, "读取数据失败")
		return
	}

	// 扫描 outputDir 目录
	scannedPaths := []string{h.outputDir}
	orphanResources := make([]OrphanResourceItem, 0)
	var totalSize int64 = 0

	// 支持扫描多个子目录
	dirsToScan := []string{h.outputDir}

	for _, scanDir := range dirsToScan {
		err := filepath.Walk(scanDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // 忽略访问错误，继续扫描
			}

			// 跳过目录
			if info.IsDir() {
				return nil
			}

			// 检查文件是否在数据库记录中
			absPath, err := filepath.Abs(path)
			if err != nil {
				absPath = path
			}

			if recordedFiles[absPath] || recordedFiles[path] {
				return nil // 文件已记录，跳过
			}

			// 检查是否是视频文件或相关临时文件
			ext := filepath.Ext(path)
			isVideoFile := false
			isTempFile := false

			videoExts := map[string]bool{
				".mp4":  true,
				".mkv":  true,
				".webm": true,
				".avi":  true,
				".mov":  true,
				".flv":  true,
				".wmv":  true,
				".m4v":  true,
				".mp3":  true,
				".wav":  true,
			}

			if videoExts[ext] {
				isVideoFile = true
			}

			// 检查临时文件
			baseName := filepath.Base(path)
			if filepath.Ext(path) == ".part" || 
			   filepath.Ext(path) == ".temp" ||
			   len(baseName) > 5 && baseName[len(baseName)-5:] == ".part" ||
			   len(baseName) > 5 && baseName[len(baseName)-5:] == ".temp" {
				isTempFile = true
			}

			// 只收集视频文件和临时文件
			if !isVideoFile && !isTempFile {
				return nil
			}

			// 确定孤儿原因
			reason := "未在数据库中记录"
			if isTempFile {
				reason = "临时文件残留"
			}

			fileInfo := OrphanResourceItem{
				FilePath:   absPath,
				FileName:   filepath.Base(path),
				FileSize:   info.Size(),
				FileSizeMB: float64(info.Size()) / 1024.0 / 1024.0,
				CreatedAt:  info.ModTime().Format(time.RFC3339),
				Reason:     reason,
			}

			orphanResources = append(orphanResources, fileInfo)
			totalSize += info.Size()

			return nil
		})

		if err != nil {
			log.Printf("扫描目录 %s 失败：%v", scanDir, err)
		}
	}

	// 返回结果
	response := OrphanResourceResponse{
		Success: true,
		Data: &OrphanResourceData{
			TotalFiles:   len(orphanResources),
			TotalSize:    totalSize,
			TotalSizeMB:  float64(totalSize) / 1024.0 / 1024.0,
			Resources:    orphanResources,
			ScannedPaths: scannedPaths,
		},
	}

	// 如果没有找到孤儿资源，返回空数组而不是 null
	if response.Data.Resources == nil {
		response.Data.Resources = []OrphanResourceItem{}
	}

	writeJSON(w, http.StatusOK, response)
}

// DeleteOrphanResource 删除单个孤儿资源
// DELETE /api/v1/resources/orphan
func (h *TaskHandler) DeleteOrphanResource(w http.ResponseWriter, r *http.Request) {
	// 仅允许管理员访问
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	if claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "仅管理员可访问")
		return
	}

	// 解析请求
	var req DeleteOrphanResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求体")
		return
	}

	if len(req.FilePaths) == 0 {
		writeError(w, http.StatusBadRequest, "文件路径列表不能为空")
		return
	}

	// 执行删除
	var deletedCount int
	var deletedSize int64
	failedFiles := make([]FailedFile, 0)

	for _, filePath := range req.FilePaths {
		// 安全检查：确保路径在 outputDir 内
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			failedFiles = append(failedFiles, FailedFile{
				FilePath: filePath,
				Reason:   "无效的文件路径",
			})
			continue
		}

		// 防止路径遍历攻击
		if !filepath.IsLocal(absPath) && len(absPath) >= len(h.outputDir) {
			if absPath[:len(h.outputDir)] != h.outputDir {
				failedFiles = append(failedFiles, FailedFile{
					FilePath: filePath,
					Reason:   "文件不在允许的目录内",
				})
				continue
			}
		}

		// 获取文件信息
		fileInfo, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				// 文件已不存在，视为删除成功
				deletedCount++
				continue
			}
			failedFiles = append(failedFiles, FailedFile{
				FilePath: filePath,
				Reason:   "无法获取文件信息：" + err.Error(),
			})
			continue
		}

		// 删除文件
		if err := os.Remove(absPath); err != nil {
			failedFiles = append(failedFiles, FailedFile{
				FilePath: filePath,
				Reason:   "删除失败：" + err.Error(),
			})
			continue
		}

		deletedCount++
		deletedSize += fileInfo.Size()
	}

	// 返回结果
	response := DeleteOrphanResourceResponse{
		Success: true,
		Message: "删除完成",
		Data: &DeleteResult{
			DeletedCount:  deletedCount,
			DeletedSize:   deletedSize,
			DeletedSizeMB: float64(deletedSize) / 1024.0 / 1024.0,
		},
	}

	if len(failedFiles) > 0 {
		response.Data.FailedFiles = failedFiles
		response.Message = "部分文件删除失败"
	}

	writeJSON(w, http.StatusOK, response)
}

// CleanupAllOrphanResources 清理所有孤儿资源
// POST /api/v1/resources/orphan/cleanup
func (h *TaskHandler) CleanupAllOrphanResources(w http.ResponseWriter, r *http.Request) {
	// 仅允许管理员访问
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	if claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "仅管理员可访问")
		return
	}

	// 查询数据库中所有已记录的文件路径
	rows, err := h.db.Query(`SELECT DISTINCT file_path FROM tasks WHERE file_path IS NOT NULL AND file_path != ''`)
	if err != nil {
		log.Printf("查询任务文件路径失败：%v", err)
		writeError(w, http.StatusInternalServerError, "查询数据库失败")
		return
	}
	defer rows.Close()

	recordedFiles := make(map[string]bool)
	for rows.Next() {
		var filePath string
		if err := rows.Scan(&filePath); err != nil {
			continue
		}
		recordedFiles[filePath] = true
	}

	if err := rows.Err(); err != nil {
		log.Printf("读取任务文件路径失败：%v", err)
		writeError(w, http.StatusInternalServerError, "读取数据失败")
		return
	}

	// 扫描并删除孤儿资源
	var deletedCount int
	var deletedSize int64
	failedFiles := make([]FailedFile, 0)

	dirsToScan := []string{h.outputDir}

	for _, scanDir := range dirsToScan {
		err := filepath.Walk(scanDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}

			// 跳过目录
			if info.IsDir() {
				return nil
			}

			// 检查文件是否在数据库记录中
			absPath, err := filepath.Abs(path)
			if err != nil {
				absPath = path
			}

			if recordedFiles[absPath] || recordedFiles[path] {
				return nil
			}

			// 检查是否是视频文件或相关临时文件
			ext := filepath.Ext(path)
			isVideoFile := false
			isTempFile := false

			videoExts := map[string]bool{
				".mp4":  true,
				".mkv":  true,
				".webm": true,
				".avi":  true,
				".mov":  true,
				".flv":  true,
				".wmv":  true,
				".m4v":  true,
				".mp3":  true,
				".wav":  true,
			}

			if videoExts[ext] {
				isVideoFile = true
			}

			baseName := filepath.Base(path)
			if filepath.Ext(path) == ".part" ||
			   filepath.Ext(path) == ".temp" ||
			   len(baseName) > 5 && baseName[len(baseName)-5:] == ".part" ||
			   len(baseName) > 5 && baseName[len(baseName)-5:] == ".temp" {
				isTempFile = true
			}

			if !isVideoFile && !isTempFile {
				return nil
			}

			// 删除文件
			if err := os.Remove(path); err != nil {
				failedFiles = append(failedFiles, FailedFile{
					FilePath: path,
					Reason:   "删除失败：" + err.Error(),
				})
				return nil
			}

			deletedCount++
			deletedSize += info.Size()

			return nil
		})

		if err != nil {
			log.Printf("扫描目录 %s 失败：%v", scanDir, err)
		}
	}

	// 返回结果
	response := CleanupAllOrphanResourcesResponse{
		Success: true,
		Message: "清理完成",
		Data: &DeleteResult{
			DeletedCount:  deletedCount,
			DeletedSize:   deletedSize,
			DeletedSizeMB: float64(deletedSize) / 1024.0 / 1024.0,
		},
	}

	if len(failedFiles) > 0 {
		response.Data.FailedFiles = failedFiles
		response.Message = "部分文件删除失败"
	}

	writeJSON(w, http.StatusOK, response)
}
