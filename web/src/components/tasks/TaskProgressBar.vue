<script setup lang="ts">
import { computed } from 'vue'

// Props
const props = defineProps<{
  progress: number
  status?: string
  showLabel?: boolean
  size?: 'sm' | 'md' | 'lg'
}>()

// 尺寸映射
const sizeClasses = {
  sm: 'w-20 h-1.5',
  md: 'w-24 h-2',
  lg: 'w-32 h-3',
}

// 计算进度条容器样式
const containerClass = computed(() => {
  return sizeClasses[props.size || 'md']
})

// 获取进度条颜色
function getProgressColor(status: string, progress: number): string {
  if (status === 'completed') return 'bg-green-500'
  if (status === 'failed') return 'bg-red-500'
  if (status === 'downloading' || status === 'merging') return 'bg-blue-500'
  if (progress > 0) return 'bg-primary-500'
  return 'bg-gray-400'
}

// 计算进度条样式
const progressColorClass = computed(() => {
  return getProgressColor(props.status || '', props.progress)
})

// 计算进度百分比
const progressPercent = computed(() => {
  return Math.min(100, Math.max(0, props.progress))
})
</script>

<template>
  <div class="flex items-center">
    <div :class="['bg-gray-200 rounded-full overflow-hidden', containerClass]">
      <div
        :class="['h-full transition-all duration-300', progressColorClass]"
        :style="{ width: progressPercent + '%' }"
      ></div>
    </div>
    <span v-if="showLabel !== false" class="ml-2 text-sm text-gray-500">
      {{ progressPercent }}%
    </span>
  </div>
</template>
