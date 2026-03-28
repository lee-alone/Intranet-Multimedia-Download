<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const authStore = useAuthStore()
const sidebarOpen = ref(true)
const isMobile = ref(false)

// 检测屏幕宽度并响应式更新
function updateMobileState() {
  isMobile.value = window.innerWidth < 768
  if (isMobile.value) {
    sidebarOpen.value = false
  } else {
    sidebarOpen.value = true
  }
}

// 生命周期钩子
onMounted(() => {
  updateMobileState()
  window.addEventListener('resize', updateMobileState)
})

onUnmounted(() => {
  window.removeEventListener('resize', updateMobileState)
})

// 菜单项定义（使用 SVG 图标）
const menuItems = [
  { name: '仪表盘', path: '/', icon: 'home' },
  { name: '任务列表', path: '/tasks', icon: 'list' },
  { name: '新建任务', path: '/tasks/new', icon: 'plus' },
  { name: '审计日志', path: '/audit', icon: 'shield' },
]

// SVG 图标组件
const icons: Record<string, any> = {
  home: {
    viewBox: '0 0 24 24',
    path: 'M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6'
  },
  list: {
    viewBox: '0 0 24 24',
    path: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4'
  },
  plus: {
    viewBox: '0 0 24 24',
    path: 'M12 4v16m8-8H4'
  },
  shield: {
    viewBox: '0 0 24 24',
    path: 'M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z'
  },
}

function handleLogout() {
  authStore.logout()
  router.push('/login')
}

function toggleSidebar() {
  sidebarOpen.value = !sidebarOpen.value
}
</script>

<template>
  <div class="min-h-screen flex bg-gray-100">
    <!-- 移动端遮罩层 -->
    <div
      v-if="isMobile && !sidebarOpen"
      class="fixed inset-0 bg-black bg-opacity-50 z-20 md:hidden"
      @click="sidebarOpen = true"
    ></div>

    <!-- 侧边栏 -->
    <aside
      :class="[
        'bg-gray-900 text-white transition-all duration-300 fixed md:relative z-30 h-full',
        sidebarOpen ? 'w-64' : 'w-0 md:w-20',
        isMobile && !sidebarOpen ? 'hidden md:block' : ''
      ]"
    >
      <!-- Logo 区域 -->
      <div class="h-16 flex items-center justify-center border-b border-gray-800 overflow-hidden whitespace-nowrap">
        <div class="flex items-center space-x-2">
          <div class="w-8 h-8 bg-primary-600 rounded-lg flex items-center justify-center flex-shrink-0">
            <span class="text-white font-bold text-sm">RC</span>
          </div>
          <span
            v-if="sidebarOpen"
            class="text-lg font-bold transition-opacity duration-300"
          >
            资源采集
          </span>
        </div>
      </div>

      <!-- 导航菜单 -->
      <nav class="mt-4 px-2">
        <router-link
          v-for="item in menuItems"
          :key="item.path"
          :to="item.path"
          class="flex items-center px-3 py-3 mb-1 text-gray-400 hover:bg-gray-800 hover:text-white rounded-lg transition-colors group"
          active-class="bg-primary-600 text-white"
        >
          <!-- SVG 图标 -->
          <span class="w-6 h-6 flex items-center justify-center flex-shrink-0">
            <svg
              v-if="icons[item.icon]"
              class="w-6 h-6"
              fill="none"
              stroke="currentColor"
              :viewBox="icons[item.icon].viewBox"
            >
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" :d="icons[item.icon].path" />
            </svg>
          </span>
          <span
            v-if="sidebarOpen"
            class="ml-3 whitespace-nowrap transition-opacity duration-300"
          >
            {{ item.name }}
          </span>
        </router-link>
      </nav>

      <!-- 底部信息 -->
      <div class="absolute bottom-0 left-0 right-0 p-4 border-t border-gray-800">
        <div v-if="sidebarOpen" class="text-xs text-gray-500 text-center">
          <p>校园资源采集系统</p>
          <p class="mt-1">v4.0</p>
        </div>
      </div>
    </aside>

    <!-- 主内容区 -->
    <div class="flex-1 flex flex-col min-w-0">
      <!-- 顶部导航栏 -->
      <header class="h-16 bg-white border-b border-gray-200 flex items-center justify-between px-4 shadow-sm">
        <div class="flex items-center space-x-4">
          <!-- 汉堡菜单按钮 -->
          <button
            @click="toggleSidebar"
            class="p-2 rounded-lg hover:bg-gray-100 transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500"
            aria-label="切换侧边栏"
          >
            <svg class="w-6 h-6 text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
            </svg>
          </button>

          <!-- 页面标题 -->
          <h1 class="text-lg font-semibold text-gray-800 hidden sm:block">
            <router-link :to="'/'">仪表盘</router-link>
          </h1>
        </div>

        <!-- 用户区域 -->
        <div class="flex items-center space-x-4">
          <!-- 用户信息 -->
          <div class="flex items-center space-x-2">
            <div class="w-8 h-8 bg-primary-100 rounded-full flex items-center justify-center">
              <svg class="w-5 h-5 text-primary-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
              </svg>
            </div>
            <span class="text-gray-700 text-sm font-medium hidden sm:block">
              {{ authStore.user?.username || '管理员' }}
            </span>
          </div>

          <!-- 退出按钮 -->
          <button
            @click="handleLogout"
            class="px-3 py-1.5 text-sm text-gray-600 hover:text-gray-900 hover:bg-gray-100 rounded-lg transition-colors flex items-center space-x-1"
          >
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
            </svg>
            <span class="hidden sm:inline">退出</span>
          </button>
        </div>
      </header>

      <!-- 页面内容 -->
      <main class="flex-1 p-4 md:p-6 bg-gray-50 overflow-auto">
        <router-view />
      </main>
    </div>
  </div>
</template>

<style scoped>
/* 侧边栏动画优化 */
aside {
  will-change: width;
}
</style>
