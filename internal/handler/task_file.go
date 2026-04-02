// Package handler 提供 HTTP 请求处理器
package handler

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/campus/collector/internal/audit"
	"github.com/campus/collector/internal/engine"
)

// DownloadFile 处理文件下载请求（支持断点续传）
// GET /api/v1/tasks/:id/download
func (h *TaskHandler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	// 获取用户信息
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未授权访问")
		return
	}

	// 从 URL 路径中提取 task_id
	taskID, err := h.extractIDFromPath(r.URL.Path, 3)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求路径或 ID")
		return
	}

	if taskID == "" {
		writeError(w, http.StatusBadRequest, "任务 ID 不能为空")
		return
	}

	// 查询任务信息
	var userID int64
	var filePath sql.NullString
	var title sql.NullString
	var status string

	err = h.db.QueryRow(`
		SELECT user_id, file_path, title, status FROM tasks WHERE id = ?
		`, taskID).Scan(&userID, &filePath, &title, &status)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询任务失败")
		return
	}

	// 验证用户权限
	if int(userID) != claims.UserID && claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "无权下载此文件")
		return
	}

	// 检查任务状态
	if status != string(engine.TaskStatusCompleted) {
		writeError(w, http.StatusBadRequest, "任务尚未完成，无法下载")
		return
	}

	// 检查文件路径 - 支持文件搬迁后的兼容性
	var actualFilePath string
	if filePath.Valid && filePath.String != "" {
		dbFilePath := filePath.String
		// 如果数据库存的是绝对路径但文件不存在（用户移动了文件夹），尝试在当前 outputDir 下寻找
		if filepath.IsAbs(dbFilePath) {
			if _, err := os.Stat(dbFilePath); os.IsNotExist(err) {
				// 文件不在数据库记录的位置，尝试在当前程序的 downloads 目录下寻找
				actualFilePath = filepath.Join(h.outputDir, filepath.Base(dbFilePath))
				log.Printf("文件搬迁检测：原路径 %s 不存在，使用新路径 %s", dbFilePath, actualFilePath)
			} else {
				actualFilePath = dbFilePath
			}
		} else {
			// 相对路径，直接拼接
			actualFilePath = filepath.Join(h.outputDir, dbFilePath)
		}
	} else {
		// 兜底方案：TaskID.mp4
		actualFilePath = filepath.Join(h.outputDir, taskID+".mp4")
	}

	// 打开文件（优先使用 task_id 路径）
	file, err := os.Open(actualFilePath)
	if err != nil {
		// 尝试其他视频格式（.mkv, .webm 等）
		videoExts := []string{".mkv", ".webm", ".avi", ".mov", ".flv", ".wmv", ".m4v"}
		basePath := strings.TrimSuffix(actualFilePath, filepath.Ext(actualFilePath))
		for _, ext := range videoExts {
			testPath := basePath + ext
			if testPath == actualFilePath {
				continue // 跳过已尝试的扩展名
			}
			file, err = os.Open(testPath)
			if err == nil {
				actualFilePath = testPath
				break
			}
		}
		if err != nil {
			log.Printf("打开文件失败：%s, 错误：%v", actualFilePath, err)
			writeError(w, http.StatusNotFound, "文件不存在")
			return
		}
	}
	defer file.Close()

	// 更新数据库中的文件路径（如果之前为空或文件已搬迁）
	if !filePath.Valid || filePath.String == "" || filePath.String != actualFilePath {
		_, _ = h.db.Exec(`UPDATE tasks SET file_path = ? WHERE id = ?`, actualFilePath, taskID)
	}

	// 获取文件信息
	fileInfo, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取文件信息失败")
		return
	}
	fileSize := fileInfo.Size()

	// 获取文件名（带【教学引用】前缀）
	var rawTitle string
	if title.Valid && title.String != "" {
		// 数据库中有标题，使用标题
		rawTitle = title.String
	} else {
		// 兜底：如果数据库没存标题，从物理文件名获取（排除后缀）
		base := filepath.Base(actualFilePath)
		ext := filepath.Ext(base)
		rawTitle = strings.TrimSuffix(base, ext)
	}
	// 先清理标题中的非法字符，再添加前缀
	cleanTitle := sanitizeFilename(rawTitle)
	filename := cleanTitle + ".mp4"
	// 添加【教学引用】前缀
	displayFilename := "【教学引用】" + filename

	// 解析 Range 头（断点续传支持）
	rangeHeader := r.Header.Get("Range")
	var start, end int64 = 0, fileSize - 1
	var contentRange string
	var statusCode int

	if rangeHeader != "" {
		// 解析 Range: bytes=start-end
		parts := strings.TrimPrefix(rangeHeader, "bytes=")
		rangeParts := strings.Split(parts, "-")
		if len(rangeParts) == 2 {
			if rangeParts[0] != "" {
				var parseErr error
				start, parseErr = strconv.ParseInt(rangeParts[0], 10, 64)
				if parseErr != nil {
					writeError(w, http.StatusBadRequest, "无效的 Range 格式")
					return
				}
			}
			if rangeParts[1] != "" {
				var parseErr error
				end, parseErr = strconv.ParseInt(rangeParts[1], 10, 64)
				if parseErr != nil {
					writeError(w, http.StatusBadRequest, "无效的 Range 格式")
					return
				}
			}
		}

		// 验证范围
		if start < 0 || start >= fileSize || end < start || end >= fileSize {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		// 设置 Content-Range 头
		contentRange = fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize)
		statusCode = http.StatusPartialContent

		// 定位到起始位置
		if _, err := file.Seek(start, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, "文件定位失败")
			return
		}
	} else {
		// 无 Range 头，从头开始
		statusCode = http.StatusOK
	}

	// 计算内容长度
	contentLength := end - start + 1

	// 根据文件扩展名动态设置 Content-Type
	ext := strings.ToLower(filepath.Ext(actualFilePath))
	contentType := "application/octet-stream"
	switch ext {
	case ".mp4", ".m4v":
		contentType = "video/mp4"
	case ".webm":
		contentType = "video/webm"
	case ".mov":
		contentType = "video/quicktime"
	case ".avi":
		contentType = "video/x-msvideo"
	case ".mp3":
		contentType = "audio/mpeg"
	case ".wav":
		contentType = "audio/wav"
	case ".pdf":
		contentType = "application/pdf"
	case ".doc", ".docx":
		contentType = "application/msword"
	case ".txt":
		contentType = "text/plain"
	}

	// 设置响应头
	// 使用 RFC 6266 标准处理中文文件名
	// 注意：第一个 filename="..." 只能包含 ASCII 字符，真正的中文名必须放在 filename* 中
	encodedName := url.PathEscape(displayFilename)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"download.mp4\"; filename*=UTF-8''%s", encodedName))
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "public, max-age=31536000")

	if contentRange != "" {
		w.Header().Set("Content-Range", contentRange)
	}

	// 写入状态码
	w.WriteHeader(statusCode)

	// 记录审计日志（开始下载）
	{
		userID := int64(claims.UserID)
		auditLog := &audit.AuditLog{
			UserID:    &userID,
			Action:    audit.ActionDownload,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
			Detail: map[string]interface{}{
				"task_id":   taskID,
				"filename":  displayFilename,
				"file_size": fileSize,
				"range":     rangeHeader,
				"status":    "success",
			},
			CreatedAt: time.Now(),
		}
		if err := h.auditLogger.Log(auditLog); err != nil {
			log.Printf("审计日志记录失败：%v", err)
		}
	}

	// 使用 io.CopyN 精确传输请求的字节数
	_, err = io.CopyN(w, file, contentLength)
	if err != nil {
		log.Printf("文件传输中断：%v", err)
		// 记录审计日志（下载失败）
		userID := int64(claims.UserID)
		failedLog := &audit.AuditLog{
			UserID:    &userID,
			Action:    audit.ActionDownload,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
			Detail: map[string]interface{}{
				"task_id":  taskID,
				"filename": displayFilename,
				"status":   "failed",
				"error":    err.Error(),
			},
			CreatedAt: time.Now(),
		}
		if err := h.auditLogger.Log(failedLog); err != nil {
			log.Printf("审计日志记录失败：%v", err)
		}
	}
}

// sanitizeFilename 清理文件名中的非法字符
func sanitizeFilename(filename string) string {
	// 安全获取文件名（防止路径遍历）
	filename = filepath.Base(filename)

	// 移除扩展名
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		filename = filename[:idx]
	}

	// 替换非法字符（包括 ASCII 和 Unicode 控制字符）
	filename = strings.Map(func(r rune) rune {
		// ASCII 控制字符
		if r < 32 || r == 127 {
			return '_'
		}
		// Windows/Unix 非法字符
		if strings.ContainsRune("<>:\"/\\|？*", r) {
			return '_'
		}
		return r
	}, filename)

	// Windows 保留名称检查
	reservedNames := map[string]bool{
		"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"COM1": true, "COM2": true, "COM3": true, "COM4": true,
		"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
		"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
		"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	}
	upperName := strings.ToUpper(filename)
	if reservedNames[upperName] {
		filename = "_" + filename
	}

	// 限制长度（使用 rune 计数，避免截断 UTF-8）
	runes := []rune(filename)
	if len(runes) > 100 {
		filename = string(runes[:100])
	}

	return strings.TrimSpace(filename)
}
