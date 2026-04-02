<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { get } from '@/api'
import { createAuthenticatedWebSocket } from '@/api'

// 统计信息
interface Stats {
  totalTasks: number
  completedTasks: number
  pendingTasks: number
  failedTasks: number
  downloadingTasks: number
}

// 任务信息
interface Task {
  id: string
  url: string
  status: string
  progress: number
  createdAt?: string
}

const stats = ref<Stats>({
  totalTasks: 0,
  completedTasks: 0,
  pendingTasks: 0,
  failedTasks: 0,
  downloadingTasks: 0,
})

const recentTasks = ref<Task[]>([])
const loading = ref(true)
const ws = ref<WebSocket | null>(null)

// 获取统计数据
async function fetchStats() {
  try {
    const response = await get<Stats>('/tasks/stats')
    // 兼容 code=0 或 success=true 两种格式
    if ((response.code === 0 || response.success === true) && response.data) {
      stats.value = response.data as any
    }
  } catch (e: any) {
    console.error('获取统计数据失败:', e)
    // 使用模拟数据用于演示
    stats.value = {
      totalTasks: 25,
      completedTasks: 18,
      pendingTasks: 5,
      failedTasks: 2,
      downloadingTasks: 2,
    }
  }
}

// 获取最近任务
async function fetchRecentTasks() {
  try {
    const response = await get<Task[]>('/tasks?limit=5')
    console.log('Dashboard fetchRecentTasks response:', response)
    // 兼容 code=0 或 success=true 两种格式
    // response 是 ApiResponse 类型，data 字段是实际的任务数组
    if (response.success === true && response.data) {
      recentTasks.value = response.data as any
    } else if (response.code === 0 && response.data) {
      recentTasks.value = response.data as any
    } else if (Array.isArray(response)) {
      // 如果响应直接是数组
      recentTasks.value = response
    } else {
      recentTasks.value = []
    }
    console.log('recentTasks:', recentTasks.value)
  } catch (e: any) {
    console.error('获取最近任务失败:', e)
    // 失败时显示空列表
    recentTasks.value = []
  } finally {
    loading.value = false
  }
}

// 连接 WebSocket 获取实时更新
function connectWebSocket() {
  // 检查是否有有效的 token，没有则不连接
  const token = localStorage.getItem('token')
  if (!token) {
    console.log('WebSocket 连接跳过：未登录')
    return
  }

  try {
    // 使用 /api/v1/ws 进行 WebSocket 连接，订阅所有任务更新
    const socket = createAuthenticatedWebSocket('/api/v1/ws')

    socket.onopen = () => {
      console.log('Dashboard WebSocket 连接成功')
    }

    socket.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        // 更新统计数据
        if (data.type === 'stats') {
          stats.value = data.data
        }
        // 更新任务进度
        if (data.type === 'task_update') {
          const index = recentTasks.value.findIndex(t => t.id === data.task.id)
          if (index !== -1) {
            recentTasks.value[index] = { ...recentTasks.value[index], ...data.task }
          }
        }
      } catch (e) {
        console.error('解析 WebSocket 消息失败:', e)
      }
    }

    socket.onerror = (error) => {
      console.error('Dashboard WebSocket 错误:', error)
    }

    socket.onclose = () => {
      console.log('Dashboard WebSocket 连接关闭')
      // 5 秒后尝试重连
      setTimeout(() => {
        connectWebSocket()
      }, 5000)
    }

    ws.value = socket
  } catch (e) {
    console.error('WebSocket 连接失败:', e)
  }
}

// 格式化 URL
function formatUrl(url: string): string {
  if (url.length <= 40) return url
  return url.substring(0, 37) + '...'
}

// 获取状态样式
function getStatusClass(status: string): string {
  const styles: Record<string, string> = {
    completed: 'text-green-600',
    downloading: 'text-blue-600',
    queued: 'text-gray-500',
    failed: 'text-red-600',
  }
  return styles[status] || 'text-gray-500'
}

// 获取状态文本
function getStatusText(status: string): string {
  const texts: Record<string, string> = {
    completed: '已完成',
    downloading: '下载中',
    queued: '排队中',
    failed: '失败',
    merging: '合并中',
  }
  return texts[status] || status
}

onMounted(() => {
  fetchStats()
  fetchRecentTasks()
  connectWebSocket()
})

import { onBeforeUnmount } from 'vue'
onBeforeUnmount(() => {
  if (ws.value) {
    ws.value.close()
  }
})
</script>

<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-900 mb-6">仪表盘</h1>

    <!-- 加载状态 -->
    <div v-if="loading" class="text-center py-12">
      <svg class="animate-spin h-8 w-8 text-primary-600 mx-auto" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
      </svg>
      <p class="mt-2 text-gray-500">加载中...</p>
    </div>

    <div v-else>
      <!-- 统计卡片 -->
      <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <div class="bg-white rounded-lg shadow p-6 border-l-4 border-blue-500">
          <div class="flex items-center justify-between">
            <div>
              <div class="text-sm text-gray-500">总任务数</div>
              <div class="text-3xl font-bold text-gray-900">{{ stats.totalTasks }}</div>
            </div>
            <div class="p-3 bg-blue-100 rounded-full">
              <svg class="w-6 h-6 text-blue-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4"/>
              </svg>
            </div>
          </div>
        </div>
        <div class="bg-white rounded-lg shadow p-6 border-l-4 border-green-500">
          <div class="flex items-center justify-between">
            <div>
              <div class="text-sm text-gray-500">已完成</div>
              <div class="text-3xl font-bold text-green-600">{{ stats.completedTasks }}</div>
            </div>
            <div class="p-3 bg-green-100 rounded-full">
              <svg class="w-6 h-6 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/>
              </svg>
            </div>
          </div>
        </div>
        <div class="bg-white rounded-lg shadow p-6 border-l-4 border-yellow-500">
          <div class="flex items-center justify-between">
            <div>
              <div class="text-sm text-gray-500">进行中</div>
              <div class="text-3xl font-bold text-yellow-600">{{ stats.pendingTasks + stats.downloadingTasks }}</div>
            </div>
            <div class="p-3 bg-yellow-100 rounded-full">
              <svg class="w-6 h-6 text-yellow-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/>
              </svg>
            </div>
          </div>
        </div>
        <div class="bg-white rounded-lg shadow p-6 border-l-4 border-red-500">
          <div class="flex items-center justify-between">
            <div>
              <div class="text-sm text-gray-500">失败</div>
              <div class="text-3xl font-bold text-red-600">{{ stats.failedTasks }}</div>
            </div>
            <div class="p-3 bg-red-100 rounded-full">
              <svg class="w-6 h-6 text-red-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/>
              </svg>
            </div>
          </div>
        </div>
      </div>

      <!-- 快捷操作 -->
      <div class="bg-white rounded-lg shadow p-6 mb-8">
        <h2 class="text-lg font-medium text-gray-900 mb-4">快捷操作</h2>
        <div class="flex flex-wrap gap-4">
          <router-link
            to="/tasks/new"
            class="inline-flex items-center px-4 py-2 bg-primary-600 text-white rounded-lg hover:bg-primary-700 transition-colors"
          >
            <svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"/>
            </svg>
            新建任务
          </router-link>
          <router-link
            to="/tasks"
            class="inline-flex items-center px-4 py-2 border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 transition-colors"
          >
            <svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 10h16M4 14h16M4 18h16"/>
            </svg>
            任务列表
          </router-link>
          <a
            href="/api/v1/docs"
            target="_blank"
            class="inline-flex items-center px-4 py-2 border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 transition-colors"
          >
            <svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/>
            </svg>
            API 文档
          </a>
        </div>
      </div>

      <!-- 最近任务 -->
      <div class="bg-white rounded-lg shadow">
        <div class="px-6 py-4 border-b border-gray-200 flex justify-between items-center">
          <h2 class="text-lg font-medium text-gray-900">最近任务</h2>
          <router-link to="/tasks" class="text-sm text-primary-600 hover:text-primary-800">
            查看全部 →
          </router-link>
        </div>
        <div class="divide-y divide-gray-200">
          <div
            v-for="task in recentTasks"
            :key="task.id"
            class="px-6 py-4 flex items-center justify-between hover:bg-gray-50"
          >
            <div class="flex-1 min-w-0">
              <div class="flex items-center">
                <a :href="task.url" target="_blank" class="text-sm font-medium text-primary-600 hover:text-primary-800 hover:underline truncate">
                  {{ formatUrl(task.url) }}
                </a>
              </div>
              <div class="text-sm text-gray-500 mt-1">
                <span :class="getStatusClass(task.status)">
                  {{ getStatusText(task.status) }}
                </span>
              </div>
            </div>
            <div class="w-32 ml-4">
              <div class="flex items-center justify-end">
                <div class="w-20 h-2 bg-gray-200 rounded-full overflow-hidden">
                  <div
                    :class="[
                      'h-full transition-all duration-300',
                      task.status === 'completed' ? 'bg-green-500' :
                      task.status === 'failed' ? 'bg-red-500' :
                      task.status === 'downloading' ? 'bg-blue-500' :
                      'bg-primary-500'
                    ]"
                    :style="{ width: task.progress + '%' }"
                  ></div>
                </div>
                <div class="text-xs text-gray-500 text-right ml-2 w-10">{{ task.progress }}%</div>
              </div>
            </div>
          </div>
          <div v-if="recentTasks.length === 0" class="px-6 py-8 text-center text-gray-500">
            暂无任务数据
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
