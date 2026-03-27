<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const sidebarOpen = ref(true)

const menuItems = [
  { name: '仪表盘', path: '/', icon: 'home' },
  { name: '任务列表', path: '/tasks', icon: 'list' },
  { name: '新建任务', path: '/tasks/new', icon: 'plus' },
  { name: '审计日志', path: '/audit', icon: 'shield' },
]

function handleLogout() {
  localStorage.removeItem('token')
  router.push('/login')
}
</script>

<template>
  <div class="min-h-screen flex">
    <!-- 侧边栏 -->
    <aside
      :class="[
        'bg-gray-800 text-white transition-all duration-300',
        sidebarOpen ? 'w-64' : 'w-16'
      ]"
    >
      <div class="h-16 flex items-center justify-center border-b border-gray-700">
        <span v-if="sidebarOpen" class="text-xl font-bold">资源采集</span>
        <span v-else class="text-xl font-bold">RC</span>
      </div>
      
      <nav class="mt-4">
        <router-link
          v-for="item in menuItems"
          :key="item.path"
          :to="item.path"
          class="flex items-center px-4 py-3 text-gray-300 hover:bg-gray-700 hover:text-white transition-colors"
          active-class="bg-gray-700 text-white"
        >
          <span class="w-6 h-6 flex items-center justify-center">
            <!-- 简化的图标 -->
            <span v-if="item.icon === 'home'">🏠</span>
            <span v-else-if="item.icon === 'list'">📋</span>
            <span v-else-if="item.icon === 'plus'">➕</span>
            <span v-else-if="item.icon === 'shield'">🛡️</span>
          </span>
          <span v-if="sidebarOpen" class="ml-3">{{ item.name }}</span>
        </router-link>
      </nav>
    </aside>
    
    <!-- 主内容区 -->
    <div class="flex-1 flex flex-col">
      <!-- 顶部导航 -->
      <header class="h-16 bg-white border-b border-gray-200 flex items-center justify-between px-4">
        <button
          @click="sidebarOpen = !sidebarOpen"
          class="p-2 rounded-lg hover:bg-gray-100"
        >
          <span class="text-xl">☰</span>
        </button>
        
        <div class="flex items-center space-x-4">
          <span class="text-gray-600">管理员</span>
          <button
            @click="handleLogout"
            class="px-3 py-1 text-sm text-gray-600 hover:text-gray-900"
          >
            退出登录
          </button>
        </div>
      </header>
      
      <!-- 页面内容 -->
      <main class="flex-1 p-6 bg-gray-50 overflow-auto">
        <router-view />
      </main>
    </div>
  </div>
</template>
