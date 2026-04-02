<script setup lang="ts">
import { computed } from 'vue'

// 任务状态类型
export type TaskStatus = 'queued' | 'downloading' | 'merging' | 'completed' | 'failed' | 'cancelled'

// Props
const props = defineProps<{
  status: TaskStatus | string
  showMessage?: boolean
  message?: string
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
</script>

<template>
  <span class="inline-flex items-center">
    <span :class="['px-2 py-1 text-xs rounded-full', statusClass]">
      {{ statusText }}
    </span>
    <span v-if="showMessage && message" class="ml-2 text-xs text-red-600">
      {{ message }}
    </span>
  </span>
</template>
