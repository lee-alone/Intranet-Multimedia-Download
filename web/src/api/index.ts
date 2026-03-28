import axios, { type AxiosInstance, type AxiosRequestConfig, type AxiosResponse } from 'axios'

// API 响应类型定义（与后端 API 规范一致）
export interface ApiResponse<T = unknown> {
  code: number      // 错误码：0 表示成功
  message: string   // 消息
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
    // 优先从 sessionStorage 获取 token（更安全）
    const token = sessionStorage.getItem('token') || localStorage.getItem('token')
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
      switch (error.response.status) {
        case 401:
          // 未授权，清除 token 并跳转登录
          sessionStorage.removeItem('token')
          localStorage.removeItem('token')
          window.location.href = '/login'
          break
        case 403:
          // 禁止访问
          console.error('Access denied')
          break
        case 500:
          // 服务器错误
          console.error('Server error')
          break
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
  const token = sessionStorage.getItem('token') || localStorage.getItem('token')
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
