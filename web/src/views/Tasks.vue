<script setup lang="ts">
import { ref, onMounted } from 'vue'

const tasks = ref<any[]>([])
const loading = ref(true)

onMounted(async () => {
  // TODO: 从 API 获取数据
  tasks.value = [
    { id: 1, url: 'https://bilibili.com/video/BV1xxx', status: 'completed', progress: 100, createdAt: '2026-03-25 10:00' },
    { id: 2, url: 'https://youtube.com/watch?v=xxx', status: 'downloading', progress: 65, createdAt: '2026-03-25 11:00' },
    { id: 3, url: 'https://youku.com/v_show/id_xxx', status: 'queued', progress: 0, createdAt: '2026-03-25 12:00' },
    { id: 4, url: 'https://iqiyi.com/v_xxx.html', status: 'failed', progress: 30, createdAt: '2026-03-25 09:00' },
  ]
  loading.value = false
})

function getStatusClass(status: string) {
  const classes: Record<string, string> = {
    completed: 'bg-green-100 text-green-800',
    downloading: 'bg-blue-100 text-blue-800',
    queued: 'bg-gray-100 text-gray-800',
    failed: 'bg-red-100 text-red-800',
  }
  return classes[status] || 'bg-gray-100 text-gray-800'
}

function getStatusText(status: string) {
  const texts: Record<string, string> = {
    completed: '已完成',
    downloading: '下载中',
    queued: '排队中',
    failed: '失败',
  }
  return texts[status] || status
}
</script>

<template>
  <div>
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold text-gray-900">任务列表</h1>
      <router-link
        to="/tasks/new"
        class="px-4 py-2 bg-primary-600 text-white rounded-lg hover:bg-primary-700"
      >
        新建任务
      </router-link>
    </div>
    
    <div v-if="loading" class="text-center py-8">
      加载中...
    </div>
    
    <div v-else class="bg-white rounded-lg shadow overflow-hidden">
      <table class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">URL</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">状态</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">进度</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">创建时间</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">操作</th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="task in tasks" :key="task.id">
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-900">{{ task.url }}</td>
            <td class="px-6 py-4 whitespace-nowrap">
              <span :class="['px-2 py-1 text-xs rounded-full', getStatusClass(task.status)]">
                {{ getStatusText(task.status) }}
              </span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
              <div class="flex items-center">
                <div class="w-24 h-2 bg-gray-200 rounded-full overflow-hidden">
                  <div
                    class="h-full bg-primary-600"
                    :style="{ width: task.progress + '%' }"
                  ></div>
                </div>
                <span class="ml-2 text-sm text-gray-500">{{ task.progress }}%</span>
              </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ task.createdAt }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-sm">
              <button class="text-red-600 hover:text-red-900">取消</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
