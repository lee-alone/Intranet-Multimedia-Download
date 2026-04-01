<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { post } from '@/api'
import type { ApiResponse } from '@/api'

const router = useRouter()

// 表单状态
const url = ref('')
const quality = ref('best')
const priority = ref('normal')
const isBatch = ref(false)
const batchUrls = ref('')
const loading = ref(false)
const error = ref('')
const success = ref('')

// 任务创建结果
interface CreateTaskResult {
  id?: number
  batch_id?: number
  message?: string
}

// 批量任务结果
interface BatchTaskResult {
  batch_id: number
  tasks: Array<{ id: number; url: string }>
  message: string
}

// 验证 URL 格式
function validateUrl(urlString: string): boolean {
  try {
    new URL(urlString)
    return true
  } catch {
    return false
  }
}

// 解析批量 URL
function parseBatchUrls(): string[] {
  return batchUrls.value
    .split('\n')
    .map(u => u.trim())
    .filter(u => u.length > 0)
}

// 优先级映射（字符串→整数）
const priorityMap: Record<string, number> = {
  low: 0,
  normal: 1,
  high: 2,
  urgent: 3,
}

// 提交单个任务
async function submitSingleTask(): Promise<ApiResponse<CreateTaskResult>> {
  return await post<CreateTaskResult>('/tasks', {
    url: url.value,
    quality: quality.value,
    priority: priorityMap[priority.value] || 1,
  })
}

// 提交批量任务
async function submitBatchTask(): Promise<ApiResponse<BatchTaskResult>> {
  const urls = parseBatchUrls()
  return await post<BatchTaskResult>('/tasks/batch', {
    urls: urls,
    quality: quality.value,
    priority: priorityMap[priority.value] || 1,
  })
}

// 提交表单
async function handleSubmit() {
  loading.value = true
  error.value = ''
  success.value = ''

  try {
    // 验证输入
    if (isBatch.value) {
      const urls = parseBatchUrls()
      if (urls.length === 0) {
        error.value = '请至少输入一个 URL'
        loading.value = false
        return
      }
      // 验证 URL 格式
      for (const u of urls) {
        if (!validateUrl(u)) {
          error.value = `无效的 URL 格式：${u.substring(0, 50)}`
          loading.value = false
          return
        }
      }
    } else {
      if (!url.value.trim()) {
        error.value = '请输入视频链接'
        loading.value = false
        return
      }
      if (!validateUrl(url.value)) {
        error.value = 'URL 格式无效'
        loading.value = false
        return
      }
    }

    // 调用 API
    const response = isBatch.value
      ? await submitBatchTask()
      : await submitSingleTask()

    // 兼容 code=0 或 success=true 两种格式
    if (response.code === 0 || response.success === true) {
      success.value = isBatch.value
        ? `成功创建 ${parseBatchUrls().length} 个任务`
        : '任务创建成功'

      // 立即跳转，不再延迟（SSE 会在 Tasks.vue 中自动重连）
      router.push('/tasks')
    } else {
      error.value = response.message || '创建任务失败'
    }
  } catch (e: any) {
    console.error('Create task error:', e)
    if (e.response?.status === 401) {
      error.value = '未授权，请重新登录'
    } else if (e.response?.status === 400) {
      error.value = e.response.data?.message || '请求参数错误'
    } else if (e.code === 'ECONNREFUSED' || e.code === 'ERR_NETWORK') {
      error.value = '服务器连接失败，请联系管理员'
    } else {
      error.value = '创建任务失败，请稍后重试'
    }
  } finally {
    loading.value = false
  }
}

// 选项
const qualityOptions = [
  { value: 'best', label: '最高画质' },
  { value: '1080p', label: '1080p' },
  { value: '720p', label: '720p' },
  { value: '480p', label: '480p' },
]

const priorityOptions = [
  { value: 'low', label: '低' },
  { value: 'normal', label: '普通' },
  { value: 'high', label: '高' },
  { value: 'urgent', label: '紧急' },
]
</script>

<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-900 mb-6">新建任务</h1>

    <div class="bg-white rounded-lg shadow p-6 max-w-2xl">
      <form @submit.prevent="handleSubmit" class="space-y-6">
        <!-- 成功提示 -->
        <div v-if="success" class="p-4 bg-green-50 border border-green-200 rounded-lg">
          <div class="flex items-center">
            <svg class="w-5 h-5 text-green-500 mr-2" fill="currentColor" viewBox="0 0 20 20">
              <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd"/>
            </svg>
            <span class="text-green-800">{{ success }}</span>
          </div>
        </div>

        <!-- 错误提示 -->
        <div v-if="error" class="p-4 bg-red-50 border border-red-200 rounded-lg">
          <div class="flex items-center">
            <svg class="w-5 h-5 text-red-500 mr-2" fill="currentColor" viewBox="0 0 20 20">
              <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd"/>
            </svg>
            <span class="text-red-800">{{ error }}</span>
          </div>
        </div>

        <!-- 单任务/批量切换 -->
        <div class="flex items-center space-x-4">
          <label class="flex items-center cursor-pointer">
            <input
              type="radio"
              v-model="isBatch"
              :value="false"
              class="h-4 w-4 text-primary-600 focus:ring-primary-500"
            />
            <span class="ml-2 text-sm text-gray-700">单个任务</span>
          </label>
          <label class="flex items-center cursor-pointer">
            <input
              type="radio"
              v-model="isBatch"
              :value="true"
              class="h-4 w-4 text-primary-600 focus:ring-primary-500"
            />
            <span class="ml-2 text-sm text-gray-700">批量任务</span>
          </label>
        </div>

        <!-- URL 输入 -->
        <div v-if="!isBatch">
          <label class="block text-sm font-medium text-gray-700 mb-1">视频链接</label>
          <input
            v-model="url"
            type="url"
            required
            class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-transparent"
            placeholder="https://bilibili.com/video/BVxxx"
          />
          <p class="mt-1 text-xs text-gray-500">支持 Bilibili、YouTube、优酷、爱奇艺等平台</p>
        </div>

        <!-- 批量 URL 输入 -->
        <div v-else>
          <label class="block text-sm font-medium text-gray-700 mb-1">视频链接（每行一个）</label>
          <textarea
            v-model="batchUrls"
            rows="8"
            required
            class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-transparent font-mono text-sm"
            placeholder="https://bilibili.com/video/BVxxx&#10;https://youtube.com/watch?v=xxx&#10;https://youku.com/v_show/id_xxx"
          ></textarea>
          <p class="mt-1 text-xs text-gray-500">
            共 {{ parseBatchUrls().length }} 个链接
          </p>
        </div>

        <!-- 清晰度选择 -->
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">清晰度</label>
          <select
            v-model="quality"
            class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-transparent"
          >
            <option v-for="opt in qualityOptions" :key="opt.value" :value="opt.value">
              {{ opt.label }}
            </option>
          </select>
        </div>

        <!-- 优先级选择 -->
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">优先级</label>
          <select
            v-model="priority"
            class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-transparent"
          >
            <option v-for="opt in priorityOptions" :key="opt.value" :value="opt.value">
              {{ opt.label }}
            </option>
          </select>
        </div>

        <!-- 提交按钮 -->
        <div class="flex space-x-4 pt-4">
          <button
            type="submit"
            :disabled="loading"
            class="px-6 py-2.5 bg-primary-600 text-white rounded-lg hover:bg-primary-700 disabled:opacity-50 disabled:cursor-not-allowed flex items-center"
          >
            <svg v-if="loading" class="animate-spin -ml-1 mr-2 h-4 w-4 text-white" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
            {{ loading ? '创建中...' : '创建任务' }}
          </button>
          <router-link
            to="/tasks"
            class="px-6 py-2.5 border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 text-center"
          >
            取消
          </router-link>
        </div>
      </form>
    </div>
  </div>
</template>
