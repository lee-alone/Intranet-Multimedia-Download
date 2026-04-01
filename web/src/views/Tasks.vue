<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { get, del } from '@/api'

// 任务类型
interface Task {
  id: string
  url: string
  status: 'queued' | 'downloading' | 'merging' | 'completed' | 'failed' | 'cancelled'
  progress: number
  priority: 'low' | 'normal' | 'high'
  createdAt: string
  updatedAt?: string
  message?: string
  batchId?: string
}

// 平滑进度数据
interface SmoothedProgress {
  smoothed: number // 加权平滑后的进度
  history: number[] // 最近几次的进度历史
  weights: number[] // 权重
  lastUpdated?: number // 最后更新时间戳
}

// SSE 已禁用：OfflineMessage 接口已删除

// 状态
const tasks = ref<Task[]>([])
const loading = ref(true)
const error = ref('')
const showBatchView = ref(false)
// const eventSource = ref<EventSource | null>(null)  // SSE 已禁用
// const usePolling = ref(true) // 始终使用轮询模式
let pollTimer: ReturnType<typeof setInterval> | null = null
// const reconnectAttempts = ref(0)  // SSE 已禁用
// const maxReconnectAttempts = 5  // SSE 已禁用
const lastProgressCache = ref<Map<string, SmoothedProgress>>(new Map()) // 离线缓存
// const offlineMessages = ref<OfflineMessage[]>([])  // SSE 已禁用
// const isOffline = ref(false)  // SSE 已禁用
const cacheCleanupInterval = ref<ReturnType<typeof setInterval> | null>(null) // 缓存清理定时器

// 状态样式映射
const statusStyles: Record<string, string> = {
  queued: 'bg-gray-100 text-gray-800',
  downloading: 'bg-blue-100 text-blue-800',
  merging: 'bg-purple-100 text-purple-800',
  completed: 'bg-green-100 text-green-800',
  failed: 'bg-red-100 text-red-800',
  cancelled: 'bg-gray-100 text-gray-600',
}

// 状态文本映射
const statusTexts: Record<string, string> = {
  queued: '排队中',
  downloading: '下载中',
  merging: '合并中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
}

// 优先级样式
const priorityStyles: Record<string, string> = {
  low: 'bg-gray-100 text-gray-600',
  normal: 'bg-blue-100 text-blue-600',
  high: 'bg-red-100 text-red-600',
}

// 优先级文本
const priorityTexts: Record<string, string> = {
  low: '低',
  normal: '普通',
  high: '高',
}

// 获取状态样式
function getStatusClass(status: string): string {
  return statusStyles[status] || 'bg-gray-100 text-gray-800'
}

// 获取状态文本
function getStatusText(status: string): string {
  return statusTexts[status] || status
}

// 获取优先级样式
function getPriorityClass(priority: string): string {
  return priorityStyles[priority] || 'bg-gray-100 text-gray-600'
}

// 获取优先级文本
function getPriorityText(priority: string): string {
  return priorityTexts[priority] || priority
}

// 获取进度条颜色
function getProgressColor(status: string, progress: number): string {
  if (status === 'completed') return 'bg-green-500'
  if (status === 'failed') return 'bg-red-500'
  if (status === 'downloading' || status === 'merging') return 'bg-blue-500'
  if (progress > 0) return 'bg-primary-500'
  return 'bg-gray-400'
}

// 格式化 URL 显示（截断过长部分）
function formatUrl(url: string): string {
  if (url.length <= 50) return url
  return url.substring(0, 47) + '...'
}

// 格式化时间
function formatTime(dateStr: string): string {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// 清理已完成的缓存（防止内存泄漏）
function cleanupFinishedTasks() {
  const now = Date.now()
  const cache = lastProgressCache.value
  const completedTaskIds = new Set<string>()

  // 获取所有已完成的任务 ID
  tasks.value.forEach(task => {
    if (task.status === 'completed' || task.status === 'failed' || task.status === 'cancelled') {
      completedTaskIds.add(task.id)
    }
  })

  // 清理已完成任务的缓存
  completedTaskIds.forEach(id => {
    cache.delete(id)
  })

  // 清理超过 5 分钟未更新的数据
  for (const [taskId, data] of cache.entries()) {
    if (data.lastUpdated && now - data.lastUpdated > 5 * 60 * 1000) {
      cache.delete(taskId)
    }
  }
}

// 加权平滑算法 - 避免进度跳动
function smoothProgress(taskId: string, newProgress: number): number {
  const cache = lastProgressCache.value
  const defaultWeights = [0.4, 0.3, 0.2, 0.1] // 权重总和为 1

  if (!cache.has(taskId)) {
    cache.set(taskId, {
      smoothed: newProgress,
      history: [newProgress],
      weights: defaultWeights,
      lastUpdated: Date.now()
    })
    return newProgress
  }

  const data = cache.get(taskId)!
  const historySize = data.weights.length

  // 更新历史
  data.history.push(newProgress)
  if (data.history.length > historySize) {
    data.history.shift()
  }
  data.lastUpdated = Date.now()

  // 计算加权平均
  let weightedSum = 0
  let weightTotal = 0

  for (let i = 0; i < data.history.length; i++) {
    const weightIndex = Math.max(0, data.weights.length - (data.history.length - i))
    const weight = data.weights[weightIndex] || 0.1
    weightedSum += data.history[i] * weight
    weightTotal += weight
  }

  data.smoothed = weightedSum / weightTotal
  cache.set(taskId, data)

  return Math.round(data.smoothed * 100) / 100 // 保留两位小数
}

// 获取平滑后的进度
function getSmoothedProgress(taskId: string, progress: number): number {
  const smoothed = smoothProgress(taskId, progress)
  return smoothed
}

// 获取任务列表
async function fetchTasks() {
  try {
    const response = await get<Task[]>('/tasks')
    // 兼容 code=0 或 success=true 两种格式
    if ((response.code === 0 || response.success === true) && response.data) {
      const newTasks = response.data.map(task => {
        // 终态任务（completed/failed/cancelled）跳过平滑算法，直接使用后端返回的进度
        const isTerminalStatus = ['completed', 'failed', 'cancelled'].includes(task.status)
        const progress = isTerminalStatus ? task.progress : getSmoothedProgress(task.id, task.progress)
        return {
          ...task,
          progress
        }
      })
      
      // 检测是否有任务刚完成（之前是 downloading，现在是 completed）
      const hasNewCompleted = newTasks.some(newTask => {
        const oldTask = tasks.value.find(t => t.id === newTask.id)
        return oldTask &&
               (oldTask.status === 'downloading' || oldTask.status === 'merging') &&
               (newTask.status === 'completed' || newTask.status === 'failed' || newTask.status === 'cancelled')
      })
      
      tasks.value = newTasks

      // 智能控制轮询启停
      updatePolling()
      
      // 如果有新完成的任务，立即再刷新一次（确保 UI 立即更新）
      if (hasNewCompleted) {
        console.log('检测到任务完成，立即刷新')
        setTimeout(() => fetchTasks(), 500)
      }
    }
  } catch (e: any) {
    if (e.response?.status === 401) {
      error.value = '未授权，请重新登录'
    } else {
      // API 请求失败时显示空列表
      tasks.value = []
    }
  } finally {
    loading.value = false
  }
}

// 取消/删除任务（后端会根据任务状态自动判断）
async function cancelOrDeleteTask(taskId: string) {
  if (!confirm('确定要操作这个任务吗？')) return

  try {
    const response = await del(`/tasks/${taskId}`)
    // 支持 code=0 或 success=true 两种格式
    if (response.code === 0 || response.success === true) {
      // 从列表中移除
      tasks.value = tasks.value.filter(t => t.id !== taskId)
    } else {
      alert(response.message || response.error || '操作失败')
    }
  } catch (e: any) {
    alert('操作任务失败，请稍后重试')
  }
}

// 取消任务（调用取消/删除函数）
async function cancelTask(taskId: string) {
  await cancelOrDeleteTask(taskId)
}

// 删除任务（调用取消/删除函数）
async function deleteTask(taskId: string) {
  await cancelOrDeleteTask(taskId)
}

    // 下载文件
    async function downloadTask(taskId: string) {
      const token = localStorage.getItem('token')

      try {
    		const response = await fetch(`/api/v1/tasks/${taskId}/download`, {
    			headers: {
    				'Authorization': `Bearer ${token}`
    			}
    		})

    		if (!response.ok) {
    			const error = await response.json().catch(() => ({ error: '下载失败' }))
    			throw new Error(error.error || `HTTP ${response.status}`)
    		}

    		// 获取文件名：优先从 content-disposition 提取，如果失败使用默认名
    		const disposition = response.headers.get('Content-Disposition')
    		let filename = `【教学引用】${taskId}.mp4`
    		if (disposition) {
    		  // 尝试匹配 filename*=UTF-8'' (更标准)
    		  const utf8Match = disposition.match(/filename\*=UTF-8''([^;]+)/i)
    		  if (utf8Match && utf8Match[1]) {
    		    filename = decodeURIComponent(utf8Match[1])
    		  } else {
    		    // 降级匹配普通 filename=
    		    const normalMatch = disposition.match(/filename="?([^";\n]+)"?/i)
    		    if (normalMatch && normalMatch[1]) {
    		      filename = decodeURIComponent(normalMatch[1])
    		    }
    		  }
    		}

    		// 创建 Blob 并触发下载
    		const blob = await response.blob()
    		const url = window.URL.createObjectURL(blob)
    		const a = document.createElement('a')
    		a.href = url
    		a.download = filename
    		document.body.appendChild(a)
    		a.click()
    		document.body.removeChild(a)
    		window.URL.revokeObjectURL(url)
    	} catch (e: any) {
    	  alert(e.message || '下载失败，请稍后重试')
    	}
    }

    // 调整优先级（上移）
function moveUp(taskId: string) {
  const index = tasks.value.findIndex(t => t.id === taskId)
  if (index > 0) {
    const temp = tasks.value[index]
    tasks.value[index] = tasks.value[index - 1]
    tasks.value[index - 1] = temp
    // TODO: 调用 API 更新优先级
  }
}

// 调整优先级（下移）
function moveDown(taskId: string) {
  const index = tasks.value.findIndex(t => t.id === taskId)
  if (index !== -1 && index < tasks.value.length - 1) {
    const temp = tasks.value[index]
    tasks.value[index] = tasks.value[index + 1]
    tasks.value[index + 1] = temp
    // TODO: 调用 API 更新优先级
  }
}

// 轮询：智能控制（有任务进行时才轮询）
function startPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
  }
  pollTimer = setInterval(() => {
    fetchTasks()
  }, 3000) // 3 秒轮询一次，避免用户焦虑
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

// 检查是否有活跃任务（需要继续轮询）
function hasActiveTasks(): boolean {
  return tasks.value.some(t => ['queued', 'downloading', 'merging'].includes(t.status))
}

// 智能轮询控制：根据任务状态自动启停
function updatePolling() {
  const active = hasActiveTasks()
  if (active && !pollTimer) {
    // 有活跃任务且未轮询，启动轮询
    console.log('检测到活跃任务，启动轮询')
    startPolling()
  } else if (!active && pollTimer) {
    // 无活跃任务且在轮询，停止轮询（节约资源）
    console.log('无活跃任务，停止轮询')
    stopPolling()
  }
}

// 生命周期
onMounted(() => {
  // 加载初始任务列表（会自动触发轮询控制）
  fetchTasks()

  // 定期清理缓存（每 5 分钟）
  cacheCleanupInterval.value = setInterval(() => {
    cleanupFinishedTasks()
  }, 5 * 60 * 1000)
})

// 清理
onBeforeUnmount(() => {
  stopPolling()
  if (cacheCleanupInterval.value) {
    clearInterval(cacheCleanupInterval.value)
  }
})
</script>

<template>
  <div>
    <div class="flex justify-between items-center mb-6">
      <div class="flex items-center space-x-3">
        <h1 class="text-2xl font-bold text-gray-900">任务列表</h1>
        <!-- 轮询状态指示器 -->
        <span
          v-if="!loading && pollTimer"
          class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800"
          title="每 3 秒自动刷新"
        >
          <span class="w-2 h-2 rounded-full mr-1.5 bg-blue-500 animate-pulse"></span>
          轮询中
        </span>
      </div>
      <div class="flex space-x-3">
        <button
          @click="showBatchView = !showBatchView"
          class="px-4 py-2 text-sm border border-gray-300 rounded-lg hover:bg-gray-50"
        >
          {{ showBatchView ? '查看单任务' : '查看批量任务' }}
        </button>
        <router-link
          to="/tasks/new"
          class="px-4 py-2 bg-primary-600 text-white rounded-lg hover:bg-primary-700 flex items-center"
        >
          <svg class="w-4 h-4 mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"/>
          </svg>
          新建任务
        </router-link>
      </div>
    </div>

    <!-- 错误提示 -->
    <div v-if="error" class="mb-4 p-4 bg-red-50 border border-red-200 rounded-lg">
      <div class="flex items-center">
        <svg class="w-5 h-5 text-red-500 mr-2" fill="currentColor" viewBox="0 0 20 20">
          <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd"/>
        </svg>
        <span class="text-red-800">{{ error }}</span>
      </div>
    </div>

    <!-- 加载状态 -->
    <div v-if="loading" class="text-center py-12">
      <svg class="animate-spin h-8 w-8 text-primary-600 mx-auto" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
      </svg>
      <p class="mt-2 text-gray-500">加载中...</p>
    </div>

    <!-- 单任务列表 -->
    <div v-else-if="!showBatchView && tasks.length > 0" class="bg-white rounded-lg shadow overflow-hidden">
      <table class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">URL</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">状态</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">优先级</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">进度</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">创建时间</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">操作</th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="task in tasks" :key="task.id" class="hover:bg-gray-50">
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-900 max-w-xs truncate">
              <a :href="task.url" target="_blank" class="text-primary-600 hover:text-primary-800 hover:underline">
                {{ formatUrl(task.url) }}
              </a>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
              <span :class="['px-2 py-1 text-xs rounded-full', getStatusClass(task.status)]">
                {{ getStatusText(task.status) }}
              </span>
              <span v-if="task.message" class="ml-2 text-xs text-red-600">{{ task.message }}</span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
              <span :class="['px-2 py-1 text-xs rounded-full', getPriorityClass(task.priority)]">
                {{ getPriorityText(task.priority) }}
              </span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
              <div class="flex items-center">
                <div class="w-24 h-2 bg-gray-200 rounded-full overflow-hidden">
                  <div
                    :class="['h-full transition-all duration-300', getProgressColor(task.status, task.progress)]"
                    :style="{ width: task.progress + '%' }"
                  ></div>
                </div>
                <span class="ml-2 text-sm text-gray-500">{{ task.progress }}%</span>
              </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
              {{ formatTime(task.createdAt) }}
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm">
              <div class="flex space-x-2">
                <!-- 上移 -->
                <button
                  v-if="tasks.indexOf(task) > 0"
                  @click="moveUp(task.id)"
                  class="text-gray-400 hover:text-gray-600"
                  title="上移"
                >
                  <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 15l7-7 7 7"/>
                  </svg>
                </button>
                <!-- 下移 -->
                <button
                  v-if="tasks.indexOf(task) < tasks.length - 1"
                  @click="moveDown(task.id)"
                  class="text-gray-400 hover:text-gray-600"
                  title="下移"
                >
                  <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
                  </svg>
                </button>
                <!-- 取消 -->
                <button
                  v-if="task.status === 'queued' || task.status === 'downloading'"
                  @click="cancelTask(task.id)"
                  class="text-yellow-600 hover:text-yellow-900"
                  title="取消"
                >
                  取消
                </button>
                <!-- 下载 -->
                		<button
                		v-if="task.status === 'completed'"
                		@click="downloadTask(task.id)"
                		class="text-green-600 hover:text-green-900 flex items-center"
                		title="下载"
                		>
                		<svg class="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                		<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/>
                		</svg>
                		下载
                		</button>
                		<!-- 删除 -->
                		<button
                		v-if="task.status === 'completed' || task.status === 'failed' || task.status === 'cancelled'"
                		@click="deleteTask(task.id)"
                		class="text-red-600 hover:text-red-900"
                		title="删除"
                		>
                		删除
                		</button>
                		</div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 批量任务视图 -->
    <div v-else-if="showBatchView" class="bg-white rounded-lg shadow p-6">
      <h2 class="text-lg font-medium text-gray-900 mb-4">批量任务</h2>
      <p class="text-gray-500">暂无批量任务数据</p>
    </div>

    <!-- 空状态 -->
    <div v-else class="bg-white rounded-lg shadow p-12 text-center">
      <svg class="w-16 h-16 text-gray-300 mx-auto mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4"/>
      </svg>
      <p class="text-gray-500 mb-4">暂无任务</p>
      <router-link
        to="/tasks/new"
        class="inline-block px-4 py-2 bg-primary-600 text-white rounded-lg hover:bg-primary-700"
      >
        创建第一个任务
      </router-link>
    </div>
  </div>
</template>
