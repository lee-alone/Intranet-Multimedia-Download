<script setup lang="ts">
import { computed } from 'vue'

// 任务状态类型
export type TaskStatus = 'queued' | 'downloading' | 'merging' | 'completed' | 'failed' | 'cancelled'

// Props
const props = defineProps<{
  status: TaskStatus | string
  showMessage?: boolean
  message?: string
  error?: string
}>()

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

// 计算样式
const statusClass = computed(() => {
  return statusStyles[props.status] || 'bg-gray-100 text-gray-800'
})

// 计算文本
const statusText = computed(() => {
  return statusTexts[props.status] || props.status
})

// 格式化错误消息，使其对用户更友好
const formattedError = computed(() => {
  if (!props.error) return ''
  // 移除繁杂的命令行提示，清理错误信息
  return props.error
    .replace(/^error:\s*/i, '')
    .replace(/\[.*?\]\s*/g, '')
    .replace(/^下载失败：\s*/, '')
    .trim()
})
</script>

<template>
  <div class="inline-flex flex-col items-start">
    <span class="inline-flex items-center">
      <span :class="['px-2 py-1 text-xs rounded-full', statusClass]">
        {{ statusText }}
      </span>
      <span v-if="showMessage && message" class="ml-2 text-xs text-red-600">
        {{ message }}
      </span>
    </span>
    
    <!-- 如果状态是失败，显示详细错误文本 -->
    <p v-if="status === 'failed' && formattedError" class="error-detail-text">
      <svg class="inline-block w-3 h-3 mr-1" fill="currentColor" viewBox="0 0 20 20">
        <path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7 4a1 1 0 11-2 0 1 1 0 012 0zm-1-9a1 1 0 00-1 1v4a1 1 0 102 0V6a1 1 0 00-1-1z" clip-rule="evenodd"/>
      </svg>
      原因：{{ formattedError }}
    </p>
  </div>
</template>

<style scoped>
.error-detail-text {
  color: #ff4d4f;
  font-size: 0.75rem;
  margin-top: 4px;
  margin-left: 2px;
  background: #fff1f0;
  padding: 4px 8px;
  border-radius: 4px;
  max-width: 300px;
  word-break: break-word;
  line-height: 1.4;
}
</style>
