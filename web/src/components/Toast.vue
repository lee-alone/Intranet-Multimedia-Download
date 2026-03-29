<script setup lang="ts">
import { ref } from 'vue'

export type ToastType = 'success' | 'error' | 'warning' | 'info'

export interface ToastMessage {
  id: number
  message: string
  type: ToastType
  duration?: number
}

const toasts = ref<ToastMessage[]>([])
const toastId = ref(0)

const typeClasses: Record<ToastType, string> = {
  success: 'bg-green-50 border-green-200 text-green-800',
  error: 'bg-red-50 border-red-200 text-red-800',
  warning: 'bg-yellow-50 border-yellow-200 text-yellow-800',
  info: 'bg-blue-50 border-blue-200 text-blue-800',
}

const iconPaths: Record<ToastType, string> = {
  success: 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z',
  error: 'M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z',
  warning: 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z',
  info: 'M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z',
}

function addToast(message: string, type: ToastType = 'info', duration = 3000) {
  const id = ++toastId.value
  const toast: ToastMessage = {
    id,
    message,
    type,
    duration,
  }
  
  toasts.value.push(toast)
  
  // 自动关闭
  if (duration) {
    setTimeout(() => {
      removeToast(id)
    }, duration)
  }
  
  return id
}

function removeToast(id: number) {
  const index = toasts.value.findIndex(t => t.id === id)
  if (index !== -1) {
    toasts.value.splice(index, 1)
  }
}

function closeToast(id: number) {
  removeToast(id)
}

// 暴露方法给全局
if (typeof window !== 'undefined') {
  ;(window as any).toast = {
    success: (message: string, duration?: number) => addToast(message, 'success', duration),
    error: (message: string, duration?: number) => addToast(message, 'error', duration),
    warning: (message: string, duration?: number) => addToast(message, 'warning', duration),
    info: (message: string, duration?: number) => addToast(message, 'info', duration),
  }
}

defineExpose({
  addToast,
  removeToast,
  closeToast,
})
</script>

<template>
  <div class="fixed top-4 right-4 z-50 space-y-2 max-w-sm w-full">
    <TransitionGroup
      enter="transition ease-out duration-300"
      enter-from="opacity-0 translate-x-full"
      enter-to="opacity-100 translate-x-0"
      leave="transition ease-in duration-200"
      leave-from="opacity-100 translate-x-0"
      leave-to="opacity-0 translate-x-full"
    >
      <div
        v-for="toast in toasts"
        :key="toast.id"
        class="flex items-center p-4 rounded-lg border shadow-lg cursor-pointer hover:shadow-xl transition-shadow"
        :class="typeClasses[toast.type]"
        @click="closeToast(toast.id)"
      >
        <svg class="w-5 h-5 mr-2 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" :d="iconPaths[toast.type]" />
        </svg>
        <span class="text-sm font-medium flex-1">{{ toast.message }}</span>
        <button
          class="ml-2 text-current opacity-70 hover:opacity-100 focus:outline-none"
          @click.stop="closeToast(toast.id)"
        >
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>
