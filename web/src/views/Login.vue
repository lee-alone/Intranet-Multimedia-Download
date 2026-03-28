<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore, checkPasswordStrength, type PasswordStrength } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const username = ref('')
const password = ref('')
const useLDAP = ref(false)
const loading = ref(false)
const error = ref('')
const showPassword = ref(false)
const showPasswordStrength = ref(false)

// 密码强度检查结果
const passwordStrength = ref<PasswordStrength | null>(null)

// 记住用户名
const savedUsername = localStorage.getItem('rememberedUsername')
if (savedUsername) {
  username.value = savedUsername
}

// 检查重定向 URL 是否安全（防止开放重定向漏洞）
function isValidRedirectUrl(url: string): boolean {
  if (!url) return false
  // 只允许内部路由（以 / 开头且不以 // 开头）
  return url.startsWith('/') && !url.startsWith('//')
}

// 监听密码输入，实时检查强度
function onPasswordInput() {
  if (password.value.length > 0) {
    passwordStrength.value = checkPasswordStrength(password.value)
    showPasswordStrength.value = true
  } else {
    passwordStrength.value = null
    showPasswordStrength.value = false
  }
}

// 密码强度提示文本
const strengthMessage = computed(() => {
  if (!passwordStrength.value) return ''
  const messages: string[] = []
  if (passwordStrength.value.tooShort) messages.push('长度至少 8 位')
  if (passwordStrength.value.noUppercase) messages.push('包含大写字母')
  if (passwordStrength.value.noLowercase) messages.push('包含小写字母')
  if (passwordStrength.value.noNumber) messages.push('包含数字')
  return messages.join('、')
})

async function handleLogin() {
  loading.value = true
  error.value = ''

  if (!username.value.trim() || !password.value) {
    error.value = '请输入用户名和密码'
    loading.value = false
    return
  }

  try {
    // 保存用户名到本地存储
    if (useLDAP.value) {
      localStorage.setItem('rememberedUsername', username.value)
    }

    // 登录
    const loginMethod = useLDAP.value ? 'ldap' : 'local'
    const success = await authStore.login(username.value, password.value, loginMethod)

    if (success) {
      // 登录成功，安全检查后跳转
      const redirect = route.query.redirect as string
      if (isValidRedirectUrl(redirect)) {
        router.push(redirect)
      } else {
        router.push('/')
      }
    } else {
      error.value = authStore.error || '登录失败，请检查用户名和密码'
    }
  } catch (e: any) {
    // 根据错误类型显示友好提示
    if (e.response?.status === 401) {
      error.value = '用户名或密码错误'
    } else if (e.code === 'ECONNREFUSED' || e.code === 'ERR_NETWORK') {
      error.value = '服务器连接失败，请联系管理员'
    } else {
      error.value = '登录失败，请稍后重试'
    }
    console.error('Login error:', e)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-gradient-to-br from-primary-50 via-blue-50 to-indigo-100">
    <div class="max-w-md w-full bg-white rounded-2xl shadow-xl p-8 mx-4">
      <!-- Logo 和标题 -->
      <div class="text-center mb-8">
        <div class="flex justify-center mb-4">
          <div class="w-16 h-16 bg-primary-600 rounded-xl flex items-center justify-center shadow-lg">
            <svg class="w-10 h-10 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" />
            </svg>
          </div>
        </div>
        <h1 class="text-2xl font-bold text-gray-900">校园资源采集系统</h1>
        <p class="mt-2 text-gray-500 text-sm">安全、高效的多媒体资源采集平台</p>
      </div>

      <form @submit.prevent="handleLogin" class="space-y-5">
        <!-- 用户名输入 -->
        <div>
          <label for="username" class="block text-sm font-medium text-gray-700">用户名</label>
          <input
            id="username"
            v-model="username"
            type="text"
            required
            class="mt-1 block w-full px-4 py-2.5 border border-gray-300 rounded-lg shadow-sm focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-primary-500 transition-colors"
            placeholder="请输入用户名"
            :disabled="loading"
          />
        </div>

        <!-- 密码输入 -->
        <div>
          <label for="password" class="block text-sm font-medium text-gray-700">密码</label>
          <div class="relative mt-1">
            <input
              id="password"
              v-model="password"
              @input="onPasswordInput"
              :type="showPassword ? 'text' : 'password'"
              required
              class="block w-full px-4 py-2.5 border border-gray-300 rounded-lg shadow-sm focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-primary-500 transition-colors"
              placeholder="请输入密码"
              :disabled="loading"
            />
            <button
              type="button"
              @click="showPassword = !showPassword"
              class="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
              tabindex="-1"
            >
              <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <!-- 显示密码时的图标（眼睛睁开） -->
                <path v-if="showPassword" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                <!-- 隐藏密码时的图标（眼睛闭合） -->
                <path v-else stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.418 0-8-3.582-8-8 0-1.81.625-3.473 1.688-4.793m13.563 0A9.953 9.953 0 0120 11c0 4.418-3.582 8-8 8-1.344 0-2.628-.325-3.781-.907m-8.437-6.907A9.953 9.953 0 014 13c0 4.418 3.582 8 8 8 1.344 0 2.628-.325 3.781-.907m-8.437-6.907A10.05 10.05 0 0112 5c1.688 0 3.281.375 4.719 1.031m0 0A9.953 9.953 0 0118 11c0 1.81-.625 3.473-1.688 4.793m-13.563 0" />
              </svg>
            </button>
          </div>
          <!-- 密码强度提示 -->
          <div v-if="showPasswordStrength && password" class="mt-2 text-xs">
            <div :class="passwordStrength?.valid ? 'text-green-600' : 'text-red-500'">
              {{ passwordStrength?.valid ? '密码强度符合要求' : '密码要求：' + strengthMessage }}
            </div>
          </div>
        </div>

        <!-- LDAP 选项 -->
        <div class="flex items-center">
          <input
            id="ldap"
            v-model="useLDAP"
            type="checkbox"
            class="h-4 w-4 text-primary-600 focus:ring-primary-500 border-gray-300 rounded cursor-pointer"
            :disabled="loading"
          />
          <label for="ldap" class="ml-2 block text-sm text-gray-700 cursor-pointer">
            使用 LDAP 登录
          </label>
        </div>

        <!-- 错误提示 -->
        <div v-if="error" class="p-3 bg-red-50 border border-red-200 rounded-lg">
          <div class="flex items-center">
            <svg class="w-5 h-5 text-red-500 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span class="text-red-600 text-sm">{{ error }}</span>
          </div>
        </div>

        <!-- 登录按钮 -->
        <button
          type="submit"
          :disabled="loading"
          class="w-full flex justify-center items-center py-2.5 px-4 border border-transparent rounded-lg shadow-md text-sm font-medium text-white bg-primary-600 hover:bg-primary-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-primary-500 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          <svg v-if="loading" class="animate-spin -ml-1 mr-2 h-5 w-5 text-white" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          {{ loading ? '登录中...' : '登录' }}
        </button>
      </form>

      <!-- 底部信息 -->
      <div class="mt-6 text-center text-xs text-gray-400">
        <p>© 2026 校园资源采集系统 v4.0</p>
      </div>
    </div>
  </div>
</template>
