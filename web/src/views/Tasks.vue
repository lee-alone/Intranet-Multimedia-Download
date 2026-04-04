<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useTaskActions, type Task } from '@/composables/useTaskActions'
import { useTaskPolling } from '@/composables/useTaskPolling'
import { getSmoothedProgress, cleanupStaleCache } from '@/composables/useProgressSmoothing'
import TaskStatusBadge from '@/components/tasks/TaskStatusBadge.vue'
import TaskProgressBar from '@/components/tasks/TaskProgressBar.vue'
import TaskActionButtons from '@/components/tasks/TaskActionButtons.vue'

// 状态
const tasks = ref<Task[]>([])
const loading = ref(true)
const error = ref('')
const showBatchView = ref(false)

// 引入 composables
const { fetchTasks: apiFetchTasks, handleCancelOrDelete, handleDownload, handleRetry } = useTaskActions()
const { isPolling, updateSmartPolling } = useTaskPolling(() => fetchTasks())

// 缓存清理定时器
let cacheCleanupInterval: ReturnType<typeof setInterval> | null = null

/**
 * 格式化 URL 显示（截断过长部分）
 */
function formatUrl(url: string): string {
  if (url.length <= 50) return url
  return url.substring(0, 47) + '...'
}

/**
 * 格式化时间
 */
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

/**
 * 获取任务列表
 */
async function fetchTasks() {
  try {
    const newTasks = await apiFetchTasks()

    // 应用进度平滑
    const processedTasks = newTasks.map(task => {
      const isTerminalStatus = ['completed', 'failed', 'cancelled'].includes(task.status)
      const progress = isTerminalStatus
        ? task.progress
        : getSmoothedProgress(task.id, task.progress)
      return { ...task, progress }
    })

    // 检测是否有任务刚完成
    const hasNewCompleted = processedTasks.some(newTask => {
      const oldTask = tasks.value.find(t => t.id === newTask.id)
      return (
        oldTask &&
        ['downloading', 'merging'].includes(oldTask.status) &&
        ['completed', 'failed', 'cancelled'].includes(newTask.status)
      )
    })

    tasks.value = processedTasks

    // 智能控制轮询启停
    updateSmartPolling(tasks.value)

    // 如果有新完成的任务，立即再刷新一次
    if (hasNewCompleted) {
      console.log('检测到任务完成，立即刷新')
      setTimeout(() => fetchTasks(), 500)
    }
  } catch (e: any) {
    if (e.response?.status === 401) {
      error.value = '未授权，请重新登录'
    } else {
      tasks.value = []
    }
  } finally {
    loading.value = false
  }
}

/**
 * 调整优先级（上移）
 */
function moveUp(taskId: string) {
  const index = tasks.value.findIndex(t => t.id === taskId)
  if (index > 0) {
    const temp = tasks.value[index]
    tasks.value[index] = tasks.value[index - 1]
    tasks.value[index - 1] = temp
    // TODO: 调用 API 更新优先级
  }
}

/**
 * 调整优先级（下移）
 */
function moveDown(taskId: string) {
  const index = tasks.value.findIndex(t => t.id === taskId)
  if (index !== -1 && index < tasks.value.length - 1) {
    const temp = tasks.value[index]
    tasks.value[index] = tasks.value[index + 1]
    tasks.value[index + 1] = temp
    // TODO: 调用 API 更新优先级
  }
}

/**
 * 处理取消/删除
 */
async function onCancelOrDelete(taskId: string) {
  const success = await handleCancelOrDelete(taskId)
  if (success) {
    tasks.value = tasks.value.filter(t => t.id !== taskId)
  }
}

/**
 * 处理下载
 */
async function onDownload(taskId: string) {
  await handleDownload(taskId)
}

/**
 * 处理重试
 */
async function onRetry(taskId: string) {
  const success = await handleRetry(taskId)
  if (success) {
    // 立即更新本地任务状态为排队中，无需等待刷新
    const taskIndex = tasks.value.findIndex(t => t.id === taskId)
    if (taskIndex !== -1) {
      tasks.value[taskIndex].status = 'queued'
      // 进度保持在失败时的那个点，不重置为 0
    }
    // 延迟一小段时间后再刷新列表，确保后端状态已更新
    setTimeout(() => fetchTasks(), 1000)
  }
}

// 生命周期
onMounted(() => {
  fetchTasks()

  // 定期清理缓存（每 5 分钟）
  cacheCleanupInterval = setInterval(() => {
    cleanupStaleCache()
  }, 5 * 60 * 1000)
})

onBeforeUnmount(() => {
  if (cacheCleanupInterval) {
    clearInterval(cacheCleanupInterval)
  }
})
</script>

<template>
  <div>
    <!-- 标题栏 -->
    <div class="flex justify-between items-center mb-6">
      <div class="flex items-center space-x-3">
        <h1 class="text-2xl font-bold text-gray-900">任务列表</h1>
        <span
          v-if="!loading && isPolling"
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
          <tr v-for="(task, index) in tasks" :key="task.id" class="hover:bg-gray-50">
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-900 max-w-xs truncate">
              <a :href="task.url" target="_blank" class="text-primary-600 hover:text-primary-800 hover:underline">
                {{ formatUrl(task.url) }}
              </a>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
              <TaskStatusBadge 
                :status="task.status" 
                :message="task.message" 
                :error="task.error"
                :show-message="true" 
              />
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
              <span :class="[
                'px-2 py-1 text-xs rounded-full',
                task.priority === 'high' ? 'bg-red-100 text-red-600' :
                task.priority === 'normal' ? 'bg-blue-100 text-blue-600' :
                'bg-gray-100 text-gray-600'
              ]">
                {{ task.priority === 'high' ? '高' : task.priority === 'normal' ? '普通' : '低' }}
              </span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap min-w-[140px]">
              <TaskProgressBar :progress="task.progress" :status="task.status" :show-label="true" />
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
              {{ formatTime(task.createdAt) }}
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm">
              <TaskActionButtons
                :status="task.status"
                :task-id="task.id"
                :index="index"
                :total="tasks.length"
                @cancel="onCancelOrDelete"
                @delete="onCancelOrDelete"
                @download="onDownload"
                @retry="onRetry"
                @move-up="moveUp"
                @move-down="moveDown"
              />
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
