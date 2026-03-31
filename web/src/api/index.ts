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
    }
    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

// 响应拦截器 - 统一错误处理
api.interceptors.response.use(
  (response: ApiAxiosResponse) => {
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
        localStorage.removeItem('token')
        localStorage.removeItem('refreshToken')
        // 只在有旧 token 且当前不在登录页时跳转
        if (oldToken && !window.location.pathname.includes('/login') && !window.location.pathname.includes('/register')) {
          window.location.href = '/login'
        }
        break
      case 403:
        // 禁止访问
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
  const token = localStorage.getItem('token')
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

export default api
