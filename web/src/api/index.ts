import axios, { type AxiosInstance, type AxiosRequestConfig, type AxiosResponse } from 'axios'
import { getErrorMessage, getNetworkErrorMessage } from '@/utils/errorMap'

// 扩展 Window 接口以包含 toast
declare global {
  interface Window {
    toast?: {
      success: (message: string, duration?: number) => void
      error: (message: string, duration?: number) => void
      warning: (message: string, duration?: number) => void
      info: (message: string, duration?: number) => void
    }
  }
}

// API 响应类型定义（与后端 API 规范一致）
export interface ApiResponse<T = unknown> {
  code?: number     // 错误码：0 表示成功（兼容字段）
  success?: boolean // 是否成功（后端使用）
  message?: string  // 消息
  error?: string    // 错误信息（兼容字段）
  data?: T          // 数据
}

// 分页响应
export interface PaginatedResponse<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

// 扩展 AxiosResponse 类型，包含 ApiResponse
type ApiAxiosResponse<T = unknown> = Omit<AxiosResponse<ApiResponse<T>>, 'data'> & {
  data: ApiResponse<T>
}

// 创建 axios 实例
const api: AxiosInstance = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// 请求拦截器 - 添加认证 token
api.interceptors.request.use(
  (config) => {
    // 统一从 localStorage 获取 token
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
      // 🚩 详细打印 Token 信息，用于排查认证问题
      console.log('🚀 [API 请求] 发送请求:', {
        url: config.url,
        method: config.method,
        tokenPrefix: token.substring(0, 15) + '...',
        tokenLength: token.length,
      })
    } else {
      console.warn('⚠️ [API 请求] 发送请求时发现无 Token!', {
        url: config.url,
        method: config.method,
      })
    }
    return config
  },
  (error) => {
    console.error('❌ [API 请求] 请求拦截器错误:', error)
    return Promise.reject(error)
  }
)

// 响应拦截器 - 统一错误处理
api.interceptors.response.use(
  (response: ApiAxiosResponse) => {
    // 🚩 暴力打印原始响应详情，用于排查"看不见的错误"
    console.log('🚩 [API 拦截器] 原始响应详情:', {
      status: response.status,
      statusText: response.statusText,
      data: response.data,
      headers: response.headers,
    })
    return response
  },
  (error) => {
    if (error.response) {
      const status = error.response.status
      const backendMessage = error.response.data?.message || ''

      switch (status) {
      case 401:
        // 未授权，清除所有 token
        const oldToken = localStorage.getItem('token')
        // 只在有旧 token 时才清除并跳转
        if (oldToken) {
          localStorage.removeItem('token')
          localStorage.removeItem('refreshToken')
          // 避免在审计日志等页面因 401 循环跳转
          const currentPath = window.location.pathname
          if (!currentPath.includes('/login') && !currentPath.includes('/register')) {
            // 使用 replace 避免循环
            window.location.replace('/login')
          }
        }
        break
      case 403:
        // 禁止访问（如 MFA 验证失败）
        const forbiddenMsg = getErrorMessage(backendMessage || '禁止访问')
        if (window.toast) {
          window.toast.error(forbiddenMsg)
        }
        break
      case 500:
        // 服务器错误
        const serverErrorMsg = getErrorMessage(backendMessage || '服务器内部错误')
        if (window.toast) {
          window.toast.error(serverErrorMsg)
        }
        break
      default:
        // 其他错误使用 errorMap 映射
        const defaultMsg = getErrorMessage(backendMessage || status.toString())
        if (window.toast) {
          window.toast.error(defaultMsg)
        }
      }
    } else if (error.code) {
      // 网络错误
      const networkMsg = getNetworkErrorMessage(error.code)
      if (window.toast) {
        window.toast.error(networkMsg)
      }
    }
    return Promise.reject(error)
  }
)

// 封装 GET 请求
export async function get<T>(url: string, config?: AxiosRequestConfig): Promise<ApiResponse<T>> {
  const response = await api.get<ApiResponse<T>>(url, config)
  return response.data
}

// 封装 POST 请求
export async function post<T>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<ApiResponse<T>> {
  const response = await api.post<ApiResponse<T>>(url, data, config)
  return response.data
}

// 封装 PUT 请求
export async function put<T>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<ApiResponse<T>> {
  const response = await api.put<ApiResponse<T>>(url, data, config)
  return response.data
}

// 封装 DELETE 请求
export async function del<T>(url: string, config?: AxiosRequestConfig): Promise<ApiResponse<T>> {
  const response = await api.delete<ApiResponse<T>>(url, config)
  return response.data
}

// WebSocket 连接工具函数（带 token 认证）
export function createWebSocketUrl(path: string): string {
  const token = localStorage.getItem('token') || ''
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.host || 'localhost:8080'

  // 构建带 token 的 WebSocket URL
  const wsUrl = `${protocol}//${host}${path}?token=${token}`
  return wsUrl
}

// 创建带认证的 WebSocket 连接
export function createAuthenticatedWebSocket(path: string): WebSocket {
  const wsUrl = createWebSocketUrl(path)
  return new WebSocket(wsUrl)
}

// 检查是否有有效的 token
export function hasValidToken(): boolean {
  return !!localStorage.getItem('token')
}

// ==================== Cookie 管理 API ====================

export interface CookieInfo {
  domain: string
  updated_at: string
  is_shared: boolean
}

export interface CookieData {
  content: string
  domain: string
}

/**
 * 获取 RSA 公钥
 */
export async function getPublicKey(): Promise<string> {
  const response = await get<{ pubkey: string }>('/crypto/pubkey')
  if (response.success && response.data) {
    return response.data.pubkey
  }
  throw new Error('获取公钥失败')
}

/**
 * 保存 Cookie（加密后）
 */
export async function saveCookie(domain: string, encryptedData: string, isShared = false): Promise<void> {
  try {
    const response = await post('/user/cookies', {
      domain,
      encrypted_data: encryptedData,
      is_shared: isShared,
    })
    // 🔍 暴力打印后端原始响应，彻底杜绝信息被吞
    console.log('[API] saveCookie 原始响应:', response)
    
    if (!response.success) {
      const errMsg = response.error || response.message || '未知业务错误'
      alert(`[API 业务错误] ${errMsg}\n\n完整响应:\n${JSON.stringify(response, null, 2)}`)
      const error = new Error(errMsg)
      ;(error as any).response = { data: response }
      throw error
    }
  } catch (err: any) {
    // 🔍 如果是网络层错误（如 400/500），Axios 会直接 reject 到这里
    const responseData = err.response?.data
    alert(`[API 网络/系统异常] ${err.message}\n\nHTTP 状态: ${err.response?.status}\n完整响应:\n${JSON.stringify(responseData, null, 2)}`)
    throw err
  }
}

/**
 * 获取 Cookie 列表
 */
export async function getCookies(): Promise<CookieInfo[]> {
  const response = await get<{ data: CookieInfo[] }>('/user/cookies')
  if (response.success && response.data) {
    return response.data.data || []
  }
  throw new Error('获取 Cookie 列表失败')
}

/**
 * 获取指定域名的 Cookie
 */
export async function getCookie(domain: string): Promise<CookieData> {
  const response = await get<{ data: CookieData }>(`/user/cookie?domain=${encodeURIComponent(domain)}`)
  if (response.success && response.data) {
    return response.data.data
  }
  throw new Error('获取 Cookie 失败')
}

/**
 * 删除指定域名的 Cookie
 */
export async function deleteCookie(domain: string): Promise<void> {
  const response = await del(`/user/cookie?domain=${encodeURIComponent(domain)}`)
  if (!response.success) {
    throw new Error(response.error || '删除 Cookie 失败')
  }
}
