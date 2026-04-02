<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { get, post, put } from '@/api'

interface User {
  id: number
  username: string
  email: string
  role: string
}

const user = ref<User | null>(null)
const loading = ref(false)
const editMode = ref(false)
const email = ref('')
const showChangePassword = ref(false)
const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')

onMounted(async () => {
  await loadUserProfile()
})

async function loadUserProfile() {
  loading.value = true
  try {
    const response = await get('/user/me')
    if (response.success && response.data) {
      user.value = response.data as User
      email.value = (response.data as any).email || ''
    }
  } catch (e: any) {
    console.error('Failed to load user profile:', e)
    if ((window as any).toast) {
      (window as any).toast.error('加载用户信息失败')
    }
  } finally {
    loading.value = false
  }
}

async function updateProfile() {
  try {
    const response = await put('/users', {
      id: user.value?.id,
      email: email.value
    })
    if (response.success) {
      if (user.value) {
        user.value = { ...user.value, email: email.value }
      }
      editMode.value = false
      if ((window as any).toast) {
        (window as any).toast.success('个人信息已更新')
      }
    }
  } catch (e: any) {
    console.error('Failed to update profile:', e)
    if ((window as any).toast) {
      (window as any).toast.error(e.response?.data?.message || '更新失败')
    }
  }
}

function openChangePassword() {
  oldPassword.value = ''
  newPassword.value = ''
  confirmPassword.value = ''
  showChangePassword.value = true
}

async function changePassword() {
  if (newPassword.value.length < 6) {
    if ((window as any).toast) {
      (window as any).toast.error('密码长度至少为 6 位')
    }
    return
  }

  if (newPassword.value !== confirmPassword.value) {
    if ((window as any).toast) {
      (window as any).toast.error('两次输入的新密码不一致')
    }
    return
  }

  try {
    const response = await post('/user/change-password', {
      old_password: oldPassword.value,
      new_password: newPassword.value
    })
    if (response.success) {
      showChangePassword.value = false
      if ((window as any).toast) {
        (window as any).toast.success('密码已修改，请重新登录')
      }
      // 清除 token 并跳转到登录页
      localStorage.removeItem('token')
      localStorage.removeItem('refreshToken')
      setTimeout(() => {
        window.location.href = '/login'
      }, 1000)
    }
  } catch (e: any) {
    console.error('Failed to change password:', e)
    if ((window as any).toast) {
      (window as any).toast.error(e.response?.data?.message || '修改密码失败')
    }
  }
}

function getRoleText(role: string) {
  return role === 'admin' ? '管理员' : '普通用户'
}

function getRoleClass(role: string) {
  return role === 'admin' ? 'bg-red-100 text-red-800' : 'bg-blue-100 text-blue-800'
}
</script>

<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-900 mb-6">个人中心</h1>

    <!-- 加载状态 -->
    <div v-if="loading" class="text-center py-12">
      <svg class="animate-spin h-8 w-8 text-primary-600 mx-auto" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
      </svg>
      <p class="mt-2 text-gray-500">加载中...</p>
    </div>

    <div v-else-if="user" class="max-w-2xl">
      <!-- 用户信息卡片 -->
      <div class="bg-white rounded-lg shadow p-6 mb-6">
        <div class="flex items-center justify-between mb-6">
          <h2 class="text-lg font-medium text-gray-900">基本信息</h2>
          <button
            v-if="!editMode"
            @click="editMode = true"
            class="text-sm text-primary-600 hover:text-primary-800"
          >
            编辑
          </button>
          <div v-else class="space-x-2">
            <button
              @click="editMode = false"
              class="text-sm text-gray-600 hover:text-gray-800"
            >
              取消
            </button>
            <button
              @click="updateProfile"
              class="text-sm text-primary-600 hover:text-primary-800"
            >
              保存
            </button>
          </div>
        </div>

        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-500 mb-1">用户 ID</label>
            <div class="text-gray-900">{{ user.id }}</div>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-500 mb-1">用户名</label>
            <div class="text-gray-900">{{ user.username }}</div>
          </div>

          <div v-if="!editMode">
            <label class="block text-sm font-medium text-gray-500 mb-1">邮箱</label>
            <div class="text-gray-900">{{ email || '未设置' }}</div>
          </div>
          <div v-else>
            <label class="block text-sm font-medium text-gray-500 mb-1">邮箱</label>
            <input
              v-model="email"
              type="email"
              placeholder="请输入邮箱"
              class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-500 mb-1">角色</label>
            <span :class="['px-2 py-1 text-xs rounded-full', getRoleClass(user.role)]">
              {{ getRoleText(user.role) }}
            </span>
          </div>
        </div>
      </div>

      <!-- 安全设置卡片 -->
      <div class="bg-white rounded-lg shadow p-6">
        <h2 class="text-lg font-medium text-gray-900 mb-4">安全设置</h2>
        <div class="space-y-4">
          <div class="flex items-center justify-between">
            <div>
              <div class="font-medium text-gray-900">登录密码</div>
              <div class="text-sm text-gray-500">定期修改密码可以提高账户安全性</div>
            </div>
            <button
              @click="openChangePassword"
              class="px-4 py-2 border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50"
            >
              修改密码
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- 修改密码弹窗 -->
    <div v-if="showChangePassword" class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div class="bg-white rounded-lg p-6 w-full max-w-md">
        <h3 class="text-lg font-medium text-gray-900 mb-4">修改密码</h3>
        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">原密码</label>
            <input
              v-model="oldPassword"
              type="password"
              placeholder="请输入原密码"
              class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500"
              @keyup.enter="changePassword"
            />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">新密码</label>
            <input
              v-model="newPassword"
              type="password"
              placeholder="请输入新密码（至少 6 位）"
              class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500"
              @keyup.enter="changePassword"
            />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">确认新密码</label>
            <input
              v-model="confirmPassword"
              type="password"
              placeholder="请再次输入新密码"
              class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500"
              @keyup.enter="changePassword"
            />
          </div>
        </div>
        <div class="flex justify-end space-x-3 mt-6">
          <button
            @click="showChangePassword = false"
            class="px-4 py-2 border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50"
          >
            取消
          </button>
          <button
            @click="changePassword"
            class="px-4 py-2 bg-primary-600 text-white rounded-lg hover:bg-primary-700"
          >
            确认修改
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
