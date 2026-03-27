<script setup lang="ts">
import { ref, onMounted } from 'vue'

const stats = ref({
  totalTasks: 0,
  completedTasks: 0,
  pendingTasks: 0,
  failedTasks: 0,
})

const recentTasks = ref<any[]>([])

onMounted(async () => {
  // TODO: 从 API 获取数据
  stats.value = {
    totalTasks: 25,
    completedTasks: 18,
    pendingTasks: 5,
    failedTasks: 2,
  }
  
  recentTasks.value = [
    { id: 1, url: 'https://bilibili.com/...', status: 'completed', progress: 100 },
    { id: 2, url: 'https://youtube.com/...', status: 'downloading', progress: 65 },
    { id: 3, url: 'https://youku.com/...', status: 'queued', progress: 0 },
  ]
})
</script>

<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-900 mb-6">仪表盘</h1>
    
    <!-- 统计卡片 -->
    <div class="grid grid-cols-1 md:grid-cols-4 gap-4 mb-8">
      <div class="bg-white rounded-lg shadow p-6">
        <div class="text-sm text-gray-500">总任务数</div>
        <div class="text-3xl font-bold text-gray-900">{{ stats.totalTasks }}</div>
      </div>
      <div class="bg-white rounded-lg shadow p-6">
        <div class="text-sm text-gray-500">已完成</div>
        <div class="text-3xl font-bold text-green-600">{{ stats.completedTasks }}</div>
      </div>
      <div class="bg-white rounded-lg shadow p-6">
        <div class="text-sm text-gray-500">进行中</div>
        <div class="text-3xl font-bold text-blue-600">{{ stats.pendingTasks }}</div>
      </div>
      <div class="bg-white rounded-lg shadow p-6">
        <div class="text-sm text-gray-500">失败</div>
        <div class="text-3xl font-bold text-red-600">{{ stats.failedTasks }}</div>
      </div>
    </div>
    
    <!-- 最近任务 -->
    <div class="bg-white rounded-lg shadow">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-lg font-medium text-gray-900">最近任务</h2>
      </div>
      <div class="divide-y divide-gray-200">
        <div
          v-for="task in recentTasks"
          :key="task.id"
          class="px-6 py-4 flex items-center justify-between"
        >
          <div>
            <div class="text-sm font-medium text-gray-900">{{ task.url }}</div>
            <div class="text-sm text-gray-500">
              <span
                :class="{
                  'text-green-600': task.status === 'completed',
                  'text-blue-600': task.status === 'downloading',
                  'text-gray-500': task.status === 'queued'
                }"
              >
                {{ task.status }}
              </span>
            </div>
          </div>
          <div class="w-32">
            <div class="h-2 bg-gray-200 rounded-full overflow-hidden">
              <div
                class="h-full bg-primary-600 transition-all duration-300"
                :style="{ width: task.progress + '%' }"
              ></div>
            </div>
            <div class="text-xs text-gray-500 text-right mt-1">{{ task.progress }}%</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
