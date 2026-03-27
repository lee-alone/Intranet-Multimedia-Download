<script setup lang="ts">
import { ref, onMounted } from 'vue'

const logs = ref<any[]>([])
const loading = ref(true)
const showMFA = ref(true)
const mfaCode = ref('')

onMounted(async () => {
  // TODO: 检查 MFA 验证状态
  // 如果已验证，加载审计日志
})

function verifyMFA() {
  // TODO: 验证 MFA 代码
  showMFA.value = false
  loadLogs()
}

async function loadLogs() {
  loading.value = true
  // TODO: 从 API 获取审计日志
  logs.value = [
    { id: 1, action: 'login', user: 'admin', ip: '192.168.1.1', time: '2026-03-25 10:00:00', detail: '登录成功' },
    { id: 2, action: 'download', user: 'admin', ip: '192.168.1.1', time: '2026-03-25 10:05:00', detail: '下载任务创建: bilibili.com' },
    { id: 3, action: 'logout', user: 'admin', ip: '192.168.1.1', time: '2026-03-25 11:00:00', detail: '退出登录' },
  ]
  loading.value = false
}

function getActionClass(action: string) {
  const classes: Record<string, string> = {
    login: 'bg-green-100 text-green-800',
    logout: 'bg-gray-100 text-gray-800',
    download: 'bg-blue-100 text-blue-800',
    error: 'bg-red-100 text-red-800',
  }
  return classes[action] || 'bg-gray-100 text-gray-800'
}

function getActionText(action: string) {
  const texts: Record<string, string> = {
    login: '登录',
    logout: '登出',
    download: '下载',
    error: '错误',
  }
  return texts[action] || action
}
</script>

<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-900 mb-6">审计日志</h1>
    
    <!-- MFA 验证弹窗 -->
    <div v-if="showMFA" class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div class="bg-white rounded-lg shadow-xl p-6 max-w-sm w-full mx-4">
        <h2 class="text-lg font-medium text-gray-900 mb-4">二次验证</h2>
        <p class="text-sm text-gray-600 mb-4">请输入 MFA 验证码以访问审计日志</p>
        <input
          v-model="mfaCode"
          type="text"
          maxlength="6"
          class="w-full px-3 py-2 border border-gray-300 rounded-lg text-center text-2xl tracking-widest"
          placeholder="000000"
        />
        <button
          @click="verifyMFA"
          class="mt-4 w-full px-4 py-2 bg-primary-600 text-white rounded-lg hover:bg-primary-700"
        >
          验证
        </button>
      </div>
    </div>
    
    <!-- 审计日志列表 -->
    <div v-if="!showMFA" class="bg-white rounded-lg shadow overflow-hidden">
      <div v-if="loading" class="text-center py-8">
        加载中...
      </div>
      
      <table v-else class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">时间</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">操作</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">用户</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">IP</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">详情</th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="log in logs" :key="log.id">
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ log.time }}</td>
            <td class="px-6 py-4 whitespace-nowrap">
              <span :class="['px-2 py-1 text-xs rounded-full', getActionClass(log.action)]">
                {{ getActionText(log.action) }}
              </span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-900">{{ log.user }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ log.ip }}</td>
            <td class="px-6 py-4 text-sm text-gray-500">{{ log.detail }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
