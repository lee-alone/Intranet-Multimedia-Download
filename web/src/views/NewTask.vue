<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const url = ref('')
const quality = ref('best')
const isBatch = ref(false)
const batchUrls = ref('')
const loading = ref(false)

const qualityOptions = [
  { value: 'best', label: '最高画质' },
  { value: '1080p', label: '1080p' },
  { value: '720p', label: '720p' },
  { value: '480p', label: '480p' },
]

async function handleSubmit() {
  loading.value = true
  try {
    // TODO: 调用 API 创建任务
    console.log('Create task:', {
      url: isBatch.value ? batchUrls.value.split('\n').filter(Boolean) : url.value,
      quality: quality.value,
    })
    
    router.push('/tasks')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-900 mb-6">新建任务</h1>
    
    <div class="bg-white rounded-lg shadow p-6 max-w-2xl">
      <form @submit.prevent="handleSubmit" class="space-y-6">
        <!-- 单任务/批量切换 -->
        <div class="flex items-center space-x-4">
          <label class="flex items-center">
            <input
              type="radio"
              v-model="isBatch"
              :value="false"
              class="h-4 w-4 text-primary-600"
            />
            <span class="ml-2 text-sm text-gray-700">单个任务</span>
          </label>
          <label class="flex items-center">
            <input
              type="radio"
              v-model="isBatch"
              :value="true"
              class="h-4 w-4 text-primary-600"
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
            class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500"
            placeholder="https://bilibili.com/video/BVxxx"
          />
        </div>
        
        <!-- 批量 URL 输入 -->
        <div v-else>
          <label class="block text-sm font-medium text-gray-700 mb-1">视频链接（每行一个）</label>
          <textarea
            v-model="batchUrls"
            rows="6"
            required
            class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500"
            placeholder="https://bilibili.com/video/BVxxx&#10;https://youtube.com/watch?v=xxx"
          ></textarea>
        </div>
        
        <!-- 清晰度选择 -->
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">清晰度</label>
          <select
            v-model="quality"
            class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-primary-500"
          >
            <option v-for="opt in qualityOptions" :key="opt.value" :value="opt.value">
              {{ opt.label }}
            </option>
          </select>
        </div>
        
        <!-- 提交按钮 -->
        <div class="flex space-x-4">
          <button
            type="submit"
            :disabled="loading"
            class="px-4 py-2 bg-primary-600 text-white rounded-lg hover:bg-primary-700 disabled:opacity-50"
          >
            {{ loading ? '创建中...' : '创建任务' }}
          </button>
          <router-link
            to="/tasks"
            class="px-4 py-2 border border-gray-300 rounded-lg hover:bg-gray-50"
          >
            取消
          </router-link>
        </div>
      </form>
    </div>
  </div>
</template>
