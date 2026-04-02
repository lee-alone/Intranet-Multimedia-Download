<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { get } from '@/api'

interface AuditLog {
  id: number
  user_id?: number
  username?: string
  action: string
  resource_type?: string
  resource_id?: number
  ip_address: string
  user_agent: string
  detail: string
  created_at: string
}

const logs = ref<AuditLog[]>([])
const loading = ref(false)
const error = ref('')

onMounted(async () => {
  await loadAuditLogs()
})

async function loadAuditLogs() {
  loading.value = true
  error.value = ''
  try {
    const response = await get('/audit/logs')
    // response 是 ApiResponse 类型，直接访问 data 字段
    logs.value = (response.data as any) || []
  } catch (e: any) {
    console.error('Failed to load audit logs:', e)
    error.value = e.response?.data?.message || '加载审计日志失败'
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
    delete_task: 'bg-red-100 text-red-800',
    change_password: 'bg-orange-100 text-orange-800',
    update_user: 'bg-indigo-100 text-indigo-800',
    delete_user: 'bg-red-100 text-red-800',
    admin_reset_password: 'bg-orange-100 text-orange-800',
    agree_agreement: 'bg-cyan-100 text-cyan-800',
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
    delete_task: '删除任务',
    change_password: '修改密码',
    update_user: '更新用户',
    delete_user: '删除用户',
    admin_reset_password: '重置密码',
    agree_agreement: '同意协议',
  }
  return texts[action] || action
}

function formatTime(dateStr: string) {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN')
}

function formatDetail(detail: string, action: string) {
  try {
    const parsed = JSON.parse(detail)
    // 根据不同操作类型显示不同的详情
    switch (action) {
      case 'login':
        return parsed.detail || '登录成功'
      case 'logout':
        return '退出登录'
      case 'create_task':
        if (parsed.url) {
          // 缩短 URL 显示
          const url = parsed.url
          return url.length > 50 ? url.substring(0, 50) + '...' : url
        }
        return parsed.detail || '创建任务'
      case 'delete_task':
        return `删除任务 ID: ${parsed.task_id || '未知'}`
      case 'cancel_task':
        return `取消任务 ID: ${parsed.task_id || '未知'}`
      case 'download':
        return parsed.url ? (parsed.url.length > 50 ? parsed.url.substring(0, 50) + '...' : parsed.url) : '下载文件'
      case 'change_password':
        return '用户修改了登录密码'
      case 'update_user':
        return `更新邮箱：${parsed.email || '未知'}`
      case 'delete_user':
        return `删除用户 ID: ${parsed.deleted_user_id || '未知'}`
      case 'admin_reset_password':
        return `重置用户 ID: ${parsed.target_user_id || '未知'} 的密码`
      case 'agree_agreement':
        return `同意协议版本：${parsed.version || '未知'}`
      default:
        return parsed.detail || detail
    }
  } catch {
    return detail
  }
}
</script>

<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-900 mb-6">审计日志</h1>

    <!-- 错误提示 -->
    <div v-if="error" class="mb-4 p-4 bg-red-50 border border-red-200 rounded-lg">
      <div class="flex items-center">
        <svg class="w-5 h-5 text-red-500 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span class="text-red-700">{{ error }}</span>
      </div>
    </div>

    <!-- 审计日志列表 -->
    <div class="bg-white rounded-lg shadow overflow-hidden">
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
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">用户</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">操作</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">IP 地址</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">详情</th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="log in logs" :key="log.id">
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ formatTime(log.created_at) }}</td>
            <td class="px-6 py-4 whitespace-nowrap">
              <div class="flex items-center">
                <div class="w-8 h-8 bg-primary-100 rounded-full flex items-center justify-center mr-2">
                  <svg class="w-4 h-4 text-primary-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
                  </svg>
                </div>
                <div>
                  <div class="text-sm font-medium text-gray-900">{{ log.username || `用户${log.user_id}` }}</div>
                  <div class="text-xs text-gray-500">ID: {{ log.user_id }}</div>
                </div>
              </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
              <span :class="['px-2 py-1 text-xs rounded-full', getActionClass(log.action)]">
                {{ getActionText(log.action) }}
              </span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ log.ip_address }}</td>
            <td class="px-6 py-4 text-sm text-gray-500 max-w-md truncate">
              {{ formatDetail(log.detail, log.action) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
