<script setup lang="ts">
import { RouterView } from 'vue-router'
import Toast from './components/Toast.vue'
</script>

<template>
  <div class="min-h-screen bg-gray-50">
    <RouterView />
    <Toast />
  </div>
</template>

<style scoped>
</style>
