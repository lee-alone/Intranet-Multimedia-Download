<script setup lang="ts">
import { computed } from 'vue'

// 任务状态类型
export type TaskStatus = 'queued' | 'downloading' | 'merging' | 'completed' | 'failed' | 'cancelled'

// Props
const props = defineProps<{
  status: TaskStatus | string
  taskId: string
  index?: number
  total?: number
  disabled?: boolean
}>()

// Emits
const emit = defineEmits<{
  cancel: [taskId: string]
  delete: [taskId: string]
  download: [taskId: string]
  retry: [taskId: string]
  moveUp: [taskId: string]
  moveDown: [taskId: string]
}>()

// 计算是否显示上移按钮
const canMoveUp = computed(() => {
  return (props.index ?? 0) > 0
})

// 计算是否显示下移按钮
const canMoveDown = computed(() => {
  return (props.index ?? 0) < ((props.total ?? 1) - 1)
})

// 计算是否显示取消按钮
const canCancel = computed(() => {
  return ['queued', 'downloading'].includes(props.status)
})

// 计算是否显示下载按钮
const canDownload = computed(() => {
  return props.status === 'completed'
})

// 计算是否显示删除按钮
const canDelete = computed(() => {
  return ['completed', 'failed', 'cancelled'].includes(props.status)
})

// 计算是否显示重试按钮
const canRetry = computed(() => {
  return ['failed', 'cancelled'].includes(props.status)
})

// 事件处理
function handleCancel() {
  if (!props.disabled) {
    emit('cancel', props.taskId)
  }
}

function handleDelete() {
  if (!props.disabled) {
    emit('delete', props.taskId)
  }
}

function handleDownload() {
  if (!props.disabled) {
    emit('download', props.taskId)
  }
}

function handleMoveUp() {
  if (!props.disabled) {
    emit('moveUp', props.taskId)
  }
}

function handleMoveDown() {
  if (!props.disabled) {
    emit('moveDown', props.taskId)
  }
}

function handleRetry() {
  if (!props.disabled) {
    emit('retry', props.taskId)
  }
}
</script>

<template>
  <div class="flex items-center space-x-2">
    <!-- 上移 -->
    <button
      v-if="canMoveUp"
      @click="handleMoveUp"
      :disabled="disabled"
      class="text-gray-400 hover:text-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
      title="上移"
    >
      <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 15l7-7 7 7"/>
      </svg>
    </button>
    <!-- 下移 -->
    <button
      v-if="canMoveDown"
      @click="handleMoveDown"
      :disabled="disabled"
      class="text-gray-400 hover:text-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
      title="下移"
    >
      <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
      </svg>
    </button>
    <!-- 取消 -->
    <button
      v-if="canCancel"
      @click="handleCancel"
      :disabled="disabled"
      class="text-yellow-600 hover:text-yellow-900 disabled:opacity-50 disabled:cursor-not-allowed"
      title="取消"
    >
      取消
    </button>
    <!-- 下载 -->
    <button
      v-if="canDownload"
      @click="handleDownload"
      :disabled="disabled"
      class="text-green-600 hover:text-green-900 flex items-center disabled:opacity-50 disabled:cursor-not-allowed"
      title="下载"
    >
      <svg class="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/>
      </svg>
      下载
    </button>
    <!-- 重试 -->
    <button
      v-if="canRetry"
      @click="handleRetry"
      :disabled="disabled"
      class="text-blue-600 hover:text-blue-900 flex items-center disabled:opacity-50 disabled:cursor-not-allowed"
      title="重新执行"
    >
      <svg class="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/>
      </svg>
      重试
    </button>
    <!-- 删除 -->
    <button
      v-if="canDelete"
      @click="handleDelete"
      :disabled="disabled"
      class="text-red-600 hover:text-red-900 disabled:opacity-50 disabled:cursor-not-allowed"
      title="删除"
    >
      删除
    </button>
  </div>
</template>
