<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()

function goHome() {
  router.push('/')
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-50">
    <div class="text-center">
      <h1 class="text-6xl font-bold text-gray-300 mb-4">404</h1>
      <p class="text-xl text-gray-600 mb-8">页面不存在</p>
      <button
        @click="goHome"
        class="px-6 py-3 bg-primary-600 text-white rounded-lg hover:bg-primary-700"
      >
        返回首页
      </button>
    </div>
  </div>
</template>
