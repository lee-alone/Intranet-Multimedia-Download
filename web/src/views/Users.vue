<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { get, del, post } from '@/api'

interface User {
  id: number
  username: string
  email: string
  role: string
  created_at: string
  updated_at: string
}

const users = ref<User[]>([])
const loading = ref(false)
const error = ref('')
const isAdmin = ref(false)
const showResetPassword = ref(false)
const selectedUserId = ref<number>(0)
const newPassword = ref('')

onMounted(async () => {
  await checkAdmin()
  if (isAdmin.value) {
    await loadUsers()
  }
})

async function checkAdmin() {
  try {
    const response = await get('/user/me')
    if (response.success && response.data) {
      const data = response.data as any
      isAdmin.value = data.role === 'admin'
    }
  } catch (e) {
    console.error('Failed to get user info:', e)
  }
}

async function loadUsers() {
  loading.value = true
  error.value = ''
  try {
    const response = await get('/users')
    if (response.success && response.data) {
      const data = response as any
      users.value = data.data || []
    }
  } catch (e: any) {
    console.error('Failed to load users:', e)
    error.value = e.response?.data?.message || '加载用户列表失败'
  } finally {
    loading.value = false
  }
}

async function deleteUser(user: User) {
  if (!confirm(`确定要删除用户 "${user.username}" 吗？此操作不可恢复。`)) {
    return
  }

  try {
    const response = await del('/users', {
      data: { id: user.id }
    })
    if (response.success) {
      await loadUsers()
      if ((window as any).toast) {
        (window as any).toast.success('用户已删除')
      }
    }
  } catch (e: any) {
    console.error('Failed to delete user:', e)
    if ((window as any).toast) {
      (window as any).toast.error(e.response?.data?.message || '删除用户失败')
    }
  }
}

function openResetPassword(userId: number) {
  selectedUserId.value = userId
  newPassword.value = ''
  showResetPassword.value = true
}

async function resetPassword() {
  if (newPassword.value.length < 6) {
    if ((window as any).toast) {
      (window as any).toast.error('密码长度至少为 6 位')
    }
    return
  }

  try {
    const response = await post('/admin/users/reset-password', {
      user_id: selectedUserId.value,
      new_password: newPassword.value
    })
    if (response.success) {
      showResetPassword.value = false
      if ((window as any).toast) {
        (window as any).toast.success('密码已重置')
      }
    }
  } catch (e: any) {
    console.error('Failed to reset password:', e)
    if ((window as any).toast) {
      (window as any).toast.error(e.response?.data?.message || '重置密码失败')
    }
  }
}

function formatTime(dateStr: string) {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN')
}

function getRoleClass(role: string) {
  return role === 'admin' ? 'bg-red-100 text-red-800' : 'bg-blue-100 text-blue-800'
}

function getRoleText(role: string) {
  return role === 'admin' ? '管理员' : '普通用户'
}
</script>

<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-900 mb-6">用户管理</h1>

    <!-- 权限提示 -->
    <div v-if="!isAdmin" class="mb-4 p-4 bg-yellow-50 border border-yellow-200 rounded-lg">
      <div class="flex items-center">
        <svg class="w-5 h-5 text-yellow-500 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
        </svg>
        <span class="text-yellow-700">此页面仅限管理员访问</span>
      </div>
    </div>

    <!-- 错误提示 -->
    <div v-else-if="error" class="mb-4 p-4 bg-red-50 border border-red-200 rounded-lg">
      <div class="flex items-center">
        <svg class="w-5 h-5 text-red-500 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span class="text-red-700">{{ error }}</span>
      </div>
    </div>

    <!-- 用户列表 -->
    <div v-if="isAdmin" class="bg-white rounded-lg shadow overflow-hidden">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-lg font-medium text-gray-900">用户列表</h2>
      </div>

      <div v-if="loading" class="text-center py-8">
        <svg class="animate-spin h-8 w-8 text-primary-600 mx-auto" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
        <p class="mt-2 text-gray-600">加载中...</p>
      </div>

      <div v-else-if="users.length === 0" class="text-center py-8 text-gray-500">
        暂无用户数据
      </div>

      <table v-else class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">ID</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">用户名</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">邮箱</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">角色</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">创建时间</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">操作</th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="user in users" :key="user.id">
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ user.id }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">{{ user.username }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ user.email || '-' }}</td>
            <td class="px-6 py-4 whitespace-nowrap">
              <span :class="['px-2 py-1 text-xs rounded-full', getRoleClass(user.role)]">
                {{ getRoleText(user.role) }}
              </span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ formatTime(user.created_at) }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
              <button
                @click="openResetPassword(user.id)"
                class="text-primary-600 hover:text-primary-800 mr-3"
              >
                重置密码
              </button>
              <button
                v-if="user.id !== 1"
                @click="deleteUser(user)"
                class="text-red-600 hover:text-red-800"
              >
                删除
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 重置密码弹窗 -->
    <div v-if="showResetPassword" class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div class="bg-white rounded-lg p-6 w-full max-w-md">
        <h3 class="text-lg font-medium text-gray-900 mb-4">重置密码</h3>
        <p class="text-sm text-gray-500 mb-4">重置后用户需要重新登录</p>
        <input
          v-model="newPassword"
          type="password"
          placeholder="请输入新密码（至少 6 位）"
          class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500 mb-4"
          @keyup.enter="resetPassword"
        />
        <div class="flex justify-end space-x-3">
          <button
            @click="showResetPassword = false"
            class="px-4 py-2 border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50"
          >
            取消
          </button>
          <button
            @click="resetPassword"
            class="px-4 py-2 bg-primary-600 text-white rounded-lg hover:bg-primary-700"
          >
            确认重置
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
