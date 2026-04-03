/**
 * 任务操作 Composable
 * 提供任务取消、删除、下载等操作的封装
 */

import { del, get, post } from '@/api'

// 任务状态类型
export type TaskStatus = 'queued' | 'downloading' | 'merging' | 'completed' | 'failed' | 'cancelled'

// 任务接口
export interface Task {
  id: string
  status: TaskStatus
  [key: string]: any
}

// API 响应接口
export interface ApiResponse<T = any> {
  code?: number
  success?: boolean
  data?: T
  message?: string
  error?: string
}

/**
 * 取消或删除任务（后端会根据任务状态自动判断）
 * @param taskId 任务 ID
 * @returns 操作结果
 */
export async function cancelOrDeleteTask(taskId: string): Promise<ApiResponse> {
  try {
    const response = await del<ApiResponse>(`/tasks/${taskId}`)
    return response
  } catch (e: any) {
    return {
      success: false,
      error: e.message || '操作失败',
    }
  }
}

/**
 * 取消任务
 * @param taskId 任务 ID
 * @returns 操作结果
 */
export async function cancelTask(taskId: string): Promise<ApiResponse> {
  return cancelOrDeleteTask(taskId)
}

/**
 * 重新执行任务
 * @param taskId 任务 ID
 * @returns 操作结果（包含新任务 ID）
 */
export async function retryTask(taskId: string): Promise<ApiResponse> {
  try {
    const response = await post<ApiResponse>(`/tasks/${taskId}/retry`)
    return response
  } catch (e: any) {
    return {
      success: false,
      error: e.message || '重新执行失败',
    }
  }
}

/**
 * 删除任务
 * @param taskId 任务 ID
 * @returns 操作结果
 */
export async function deleteTask(taskId: string): Promise<ApiResponse> {
  return cancelOrDeleteTask(taskId)
}

/**
 * 下载任务文件
 * @param taskId 任务 ID
 * @param token 认证 token
 * @returns 是否成功
 */
export async function downloadTask(taskId: string, token?: string | null): Promise<boolean> {
  const authToken = token ?? localStorage.getItem('token')

  try {
    const response = await fetch(`/api/v1/tasks/${taskId}/download`, {
      headers: {
        Authorization: `Bearer ${authToken}`,
      },
    })

    if (!response.ok) {
      const error = await response.json().catch(() => ({ error: '下载失败' }))
      throw new Error(error.error || `HTTP ${response.status}`)
    }

    // 获取文件名：优先从 content-disposition 提取，如果失败使用默认名
    const disposition = response.headers.get('Content-Disposition')
    let filename = `【教学引用】${taskId}.mp4`

    if (disposition) {
      // 尝试匹配 filename*=UTF-8'' (更标准)
      const utf8Match = disposition.match(/filename\*=UTF-8''([^;]+)/i)
      if (utf8Match && utf8Match[1]) {
        filename = decodeURIComponent(utf8Match[1])
      } else {
        // 降级匹配普通 filename=
        const normalMatch = disposition.match(/filename="?([^";\n]+)"?/i)
        if (normalMatch && normalMatch[1]) {
          filename = decodeURIComponent(normalMatch[1])
        }
      }
    }

    // 创建 Blob 并触发下载
    const blob = await response.blob()
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    window.URL.revokeObjectURL(url)

    return true
  } catch (e: any) {
    console.error('下载失败:', e)
    throw e
  }
}

/**
 * 获取任务列表
 * @returns 任务列表
 */
export async function fetchTasks(): Promise<Task[]> {
  const response = await get<ApiResponse<Task[]>>('/tasks')

  if ((response.code === 0 || response.success === true) && response.data) {
    return response.data as unknown as Task[]
  }

  return []
}

/**
 * 任务操作 Composable 主函数
 */
export function useTaskActions() {
  /**
   * 处理取消/删除操作，带确认提示
   */
  async function handleCancelOrDelete(taskId: string): Promise<boolean> {
    if (!confirm('确定要操作这个任务吗？')) {
      return false
    }

    const result = await cancelOrDeleteTask(taskId)

    if (result.code === 0 || result.success === true) {
      return true
    } else {
      alert(result.message || result.error || '操作失败')
      return false
    }
  }

  /**
   * 处理下载操作，带错误提示
   */
  async function handleDownload(taskId: string): Promise<boolean> {
    try {
      await downloadTask(taskId)
      return true
    } catch (e: any) {
      alert(e.message || '下载失败，请稍后重试')
      return false
    }
  }

  /**
   * 处理重新执行操作，带提示
   */
  async function handleRetry(taskId: string): Promise<boolean> {
    const result = await retryTask(taskId)

    if (result.code === 0 || result.success === true) {
      return true
    } else {
      alert(result.message || result.error || '重新执行失败')
      return false
    }
  }

  return {
    cancelOrDeleteTask,
    cancelTask,
    deleteTask,
    retryTask,
    downloadTask,
    fetchTasks,
    handleCancelOrDelete,
    handleDownload,
    handleRetry,
  }
}
