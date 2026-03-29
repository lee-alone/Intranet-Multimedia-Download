<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { get } from '@/api'

interface AuditLog {
  id: number
  user_id?: number
  action: string
  resource_type?: string
  resource_id?: number
  ip_address: string
  user_agent: string
  detail: string
  created_at: string
}

const logs = ref<AuditLog[]>([])
const loading = ref(true)
const error = ref('')
const showMFA = ref(true)
const mfaCode = ref('')
const mfaRequired = ref(false)
const mfaEnabled = ref(false)

onMounted(async () => {
  await checkMFAStatus()
})

async function checkMFAStatus() {
  loading.value = true
  try {
    const response = await get('/mfa/status')
    const data = response.data as any
    mfaEnabled.value = data?.mfaEnabled || false
    
    if (mfaEnabled.value) {
      // 已启用 MFA，显示验证界面
      showMFA.value = true
      mfaRequired.value = true
    } else {
      // 未启用 MFA，提示用户先启用
      showMFA.value = true
      mfaRequired.value = false
    }
  } catch (e: any) {
    error.value = '获取 MFA 状态失败'
    console.error(e)
  } finally {
    loading.value = false
  }
}

async function verifyMFA() {
  if (!mfaCode.value || mfaCode.value.length !== 6) {
    error.value = '请输入 6 位验证码'
    return
  }

  loading.value = true
  error.value = ''

  try {
    const response = await get(`/audit/logs?code=${mfaCode.value}`)
    if (response.data) {
      const data = response.data as any
      if (data.success) {
        logs.value = data.data || []
        showMFA.value = false
      } else {
        error.value = data.message || '验证失败'
      }
    }
  } catch (e: any) {
    error.value = '验证失败或无权访问'
    console.error(e)
  } finally {
    loading.value = false
  }
}

function getActionClass(action: string) {
  const classes: Record<string, string> = {
    login: 'bg-green-100 text-green-800',
    logout: 'bg-gray-100 text-gray-800',
    download: 'bg-blue-100 text-blue-800',
    error: 'bg-red-100 text-red-800',
    create_task: 'bg-purple-100 text-purple-800',
    cancel_task: 'bg-yellow-100 text-yellow-800',
    mfa_enable: 'bg-indigo-100 text-indigo-800',
    mfa_disable: 'bg-pink-100 text-pink-800',
  }
  return classes[action] || 'bg-gray-100 text-gray-800'
}

function getActionText(action: string) {
  const texts: Record<string, string> = {
    login: '登录',
    logout: '登出',
    download: '下载',
    error: '错误',
    create_task: '创建任务',
    cancel_task: '取消任务',
    mfa_enable: '启用 MFA',
    mfa_disable: '禁用 MFA',
  }
  return texts[action] || action
}

function formatTime(dateStr: string) {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN')
}

function formatDetail(detail: string) {
  try {
    const parsed = JSON.parse(detail)
    return parsed.detail || detail
  } catch {
    return detail
  }
}
</script>

<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-900 mb-6">审计日志</h1>

    <!-- MFA 验证弹窗 -->
    <div v-if="showMFA" class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div class="bg-white rounded-lg shadow-xl p-6 max-w-sm w-full mx-4">
        <h2 class="text-lg font-medium text-gray-900 mb-4">二次验证</h2>
        
        <div v-if="!mfaRequired" class="text-center">
          <p class="text-sm text-gray-600 mb-4">访问审计日志需要先启用 MFA</p>
          <button
            @click="$router.push('/mfa-bind')"
            class="px-4 py-2 bg-primary-600 text-white rounded-lg hover:bg-primary-700"
          >
            前往启用 MFA
          </button>
        </div>
        
        <template v-else>
          <p class="text-sm text-gray-600 mb-4">请输入 MFA 验证码以访问审计日志</p>
          <input
            v-model="mfaCode"
            type="text"
            maxlength="6"
            class="w-full px-3 py-2 border border-gray-300 rounded-lg text-center text-2xl tracking-widest mb-4"
            placeholder="000000"
          />
          <button
            @click="verifyMFA"
            :disabled="loading || mfaCode.length !== 6"
            class="w-full px-4 py-2 bg-primary-600 text-white rounded-lg hover:bg-primary-700 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {{ loading ? '验证中...' : '验证' }}
          </button>
        </template>
      </div>
    </div>

    <!-- 错误提示 -->
    <div v-if="error && !showMFA" class="mb-4 p-4 bg-red-50 border border-red-200 rounded-lg">
      <div class="flex items-center">
        <svg class="w-5 h-5 text-red-500 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span class="text-red-700">{{ error }}</span>
      </div>
    </div>

    <!-- 审计日志列表 -->
    <div v-if="!showMFA" class="bg-white rounded-lg shadow overflow-hidden">
      <div v-if="loading" class="text-center py-8">
        <svg class="animate-spin h-8 w-8 text-primary-600 mx-auto" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
        <p class="mt-2 text-gray-600">加载中...</p>
      </div>

      <div v-else-if="logs.length === 0" class="text-center py-8 text-gray-500">
        暂无审计日志
      </div>

      <table v-else class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">时间</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">操作</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">IP 地址</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">详情</th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="log in logs" :key="log.id">
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ formatTime(log.created_at) }}</td>
            <td class="px-6 py-4 whitespace-nowrap">
              <span :class="['px-2 py-1 text-xs rounded-full', getActionClass(log.action)]">
                {{ getActionText(log.action) }}
              </span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ log.ip_address }}</td>
            <td class="px-6 py-4 text-sm text-gray-500">{{ formatDetail(log.detail) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
