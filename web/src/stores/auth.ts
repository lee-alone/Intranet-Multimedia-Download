import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import api, { type ApiResponse } from '@/api'

// 用户信息类型
export interface User {
  id: number
  username: string
  email: string
  role: string
  mfaEnabled: boolean
}

// 登录响应（后端返回的 token 数据）
interface TokenData {
  access_token: string
  refresh_token: string
  expires_in: number
}

export const useAuthStore = defineStore('auth', () => {
  // 状态
  const token = ref<string | null>(localStorage.getItem('token'))
  const refreshToken = ref<string | null>(localStorage.getItem('refreshToken'))
  const user = ref<User | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  // 计算属性
  const isAuthenticated = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  // 登录
  async function login(username: string, password: string): Promise<boolean> {
    loading.value = true
    error.value = null

    try {
      const res = await api.post<TokenData>('/login', { username, password })
      const response = res as unknown as ApiResponse<TokenData>

      if (response.data && response.data.access_token) {
        token.value = response.data.access_token
        refreshToken.value = response.data.refresh_token

        localStorage.setItem('token', response.data.access_token)
        localStorage.setItem('refreshToken', response.data.refresh_token)

        // 获取用户信息
        await fetchUserInfo()
        return true
      } else {
        error.value = response.message || '登录失败'
        return false
      }
    } catch (e) {
      error.value = '登录失败，请检查网络连接'
      return false
    } finally {
      loading.value = false
    }
  }

  // 注册
  async function register(username: string, password: string, email: string): Promise<boolean> {
    loading.value = true
    error.value = null

    try {
      const response: ApiResponse = await api.post('/register', { username, password, email })

      if (response.success) {
        return true
      } else {
        error.value = response.message || '注册失败'
        return false
      }
    } catch (e) {
      error.value = '注册失败，请检查网络连接'
      return false
    } finally {
      loading.value = false
    }
  }

  // 登出
  async function logout(): Promise<void> {
    try {
      await api.post('/logout')
    } catch (e) {
      // 忽略登出错误
    } finally {
      clearAuth()
    }
  }

  // 清除认证信息
  function clearAuth(): void {
    token.value = null
    refreshToken.value = null
    user.value = null
    localStorage.removeItem('token')
    localStorage.removeItem('refreshToken')
  }

  // 获取用户信息
  async function fetchUserInfo(): Promise<void> {
    try {
      const response = await api.get<User>('/user/me')
      if (response.data) {
        user.value = response.data
      }
    } catch (e) {
      console.error('Failed to fetch user info:', e)
    }
  }

  // 刷新令牌
  async function refreshAccessToken(): Promise<boolean> {
    if (!refreshToken.value) {
      return false
    }

    try {
      const response = await api.post<TokenData>('/token/refresh', {
        refresh_token: refreshToken.value,
      })

      if (response.data && response.data.access_token) {
        token.value = response.data.access_token
        refreshToken.value = response.data.refresh_token
        localStorage.setItem('token', response.data.access_token)
        localStorage.setItem('refreshToken', response.data.refresh_token)
        return true
      }
      return false
    } catch (e) {
      clearAuth()
      return false
    }
  }

  return {
    // 状态
    token,
    refreshToken,
    user,
    loading,
    error,
    // 计算属性
    isAuthenticated,
    isAdmin,
    // 方法
    login,
    register,
    logout,
    clearAuth,
    fetchUserInfo,
    refreshAccessToken,
  }
})
