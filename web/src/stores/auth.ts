import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { post, get, type ApiResponse } from '@/api'

// 认证类型
export type AuthType = 'local' | 'ldap'

// 用户信息类型
export interface User {
  id: number
  username: string
  email: string
  role: string
  mfaEnabled: boolean
}

// 登录请求参数
export interface LoginParams {
  username: string
  password: string
  authType?: AuthType
}

// 登录响应（后端返回的 token 数据）
interface TokenData {
  access_token: string
  refresh_token: string
  expires_in: number
}

// 密码强度检查结果
export interface PasswordStrength {
  valid: boolean
  tooShort: boolean
  noUppercase: boolean
  noLowercase: boolean
  noNumber: boolean
}

// 密码强度检查
export function checkPasswordStrength(password: string): PasswordStrength {
  const result: PasswordStrength = {
    valid: true,
    tooShort: false,
    noUppercase: false,
    noLowercase: false,
    noNumber: false,
  }

  if (password.length < 8) {
    result.tooShort = true
    result.valid = false
  }
  if (!/[A-Z]/.test(password)) {
    result.noUppercase = true
    result.valid = false
  }
  if (!/[a-z]/.test(password)) {
    result.noLowercase = true
    result.valid = false
  }
  if (!/[0-9]/.test(password)) {
    result.noNumber = true
    result.valid = false
  }

  return result
}

export const useAuthStore = defineStore('auth', () => {
  // 状态 - 统一使用 localStorage 避免刷新丢失
  const token = ref<string | null>(localStorage.getItem('token'))
  const refreshToken = ref<string | null>(localStorage.getItem('refreshToken'))
  const user = ref<User | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  const authType = ref<AuthType>('local')

  // 计算属性
  const isAuthenticated = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  // 存储 token（统一使用 localStorage）
  function saveToken(access_token: string, refresh_token: string, _useSession: boolean = true): void {
    token.value = access_token
    refreshToken.value = refresh_token
    // 统一使用 localStorage 持久化
    localStorage.setItem('token', access_token)
    localStorage.setItem('refreshToken', refresh_token)
  }

  // 登录
  async function login(username: string, password: string, type: AuthType = 'local'): Promise<boolean> {
    loading.value = true
    error.value = null
    authType.value = type

    try {
      // 构建登录请求体
      const loginData = {
        username,
        password,
        auth_type: type,
      }

      const response = await post<TokenData>('/login', loginData)

      // 检查响应：支持 code=0 或 success=true 两种格式
      const isSuccess = response.code === 0 || response.success === true

      if (isSuccess && response.data) {
        const { access_token, refresh_token } = response.data

        // 使用 localStorage 存储 token（避免刷新丢失）
        saveToken(access_token, refresh_token, false)

        // 获取用户信息
        try {
          await fetchUserInfo()
        } catch (e) {
          // 获取用户信息失败，清除 token 并返回错误
          console.error('Failed to fetch user info:', e)
          clearAuth()
          error.value = '登录成功但获取用户信息失败，请重试'
          return false
        }
        return true
      } else {
        error.value = response.message || response.error || '登录失败'
        return false
      }
    } catch (e: any) {
      // 根据错误类型提供友好提示
      if (e.response?.status === 401) {
        error.value = '用户名或密码错误'
      } else if (e.code === 'ECONNREFUSED' || e.code === 'ERR_NETWORK') {
        error.value = '服务器连接失败，请联系管理员'
      } else {
        error.value = '登录失败，请稍后重试'
      }
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
      const response: ApiResponse = await post('/register', { username, password, email })

      // 检查响应：支持 code=0 或 success=true 两种格式
      const isSuccess = response.code === 0 || response.success === true
      
      if (isSuccess) {
        return true
      } else {
        error.value = response.message || response.error || '注册失败'
        return false
      }
    } catch (e: any) {
      if (e.response?.status === 400) {
        error.value = e.response.data?.message || e.response.data?.error || '注册失败，用户名可能已存在'
      } else {
        error.value = '注册失败，请检查网络连接'
      }
      return false
    } finally {
      loading.value = false
    }
  }

  // 登出
  async function logout(): Promise<void> {
    try {
      await post('/auth/logout')
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
      const response = await get<User>('/user/me')
      if (response.data) {
        // 后端返回 mfa_enabled（蛇形命名），需要转换为 mfaEnabled（驼峰命名）
        const userData = response.data as any
        const mfaEnabled = userData.mfa_enabled !== undefined ? userData.mfa_enabled : userData.mfaEnabled
        user.value = {
          id: userData.id,
          username: userData.username,
          email: userData.email,
          role: userData.role,
          mfaEnabled: mfaEnabled
        }
        // 同步 MFA 状态到 localStorage（供路由守卫使用）
        if (mfaEnabled) {
          localStorage.setItem('mfaEnabled', 'true')
        } else {
          localStorage.removeItem('mfaEnabled')
        }
      }
    } catch (e) {
      // 忽略错误，由拦截器处理
    }
  }

  // 刷新令牌
  async function refreshAccessToken(): Promise<boolean> {
    if (!refreshToken.value) {
      return false
    }

    try {
      const response = await post<TokenData>('/auth/token/refresh', {
        refresh_token: refreshToken.value,
      })

      if (response.code === 0 && response.data) {
        const { access_token, refresh_token } = response.data
        // 保持原有存储方式
        const useSession = !!sessionStorage.getItem('token')
        saveToken(access_token, refresh_token, useSession)
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
    authType,
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
    saveToken,
  }
})
