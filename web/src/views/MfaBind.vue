<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { get, post } from '@/api'
import QrcodeVue from 'qrcode.vue'

const loading = ref(false)
const error = ref('')
const success = ref('')
const showQRCode = ref(false)
const qrCodeUri = ref('')
const mfaSecret = ref('')
const verificationCode = ref('')
const mfaEnabled = ref(false)
const isBinding = ref(false)

onMounted(async () => {
  await checkMFAStatus()
})

async function checkMFAStatus() {
  loading.value = true
  try {
    const response = await get('/mfa/status')
    if (response.data) {
      mfaEnabled.value = (response.data as any).mfaEnabled || false
      if (mfaEnabled.value) {
        success.value = 'MFA 已启用'
      }
    }
  } catch (e: any) {
    error.value = '获取 MFA 状态失败'
    console.error(e)
  } finally {
    loading.value = false
  }
}

async function generateMFA() {
  loading.value = true
  error.value = ''
  success.value = ''
  
  try {
    const response = await get('/mfa/generate')
    if (response.data) {
      const data = response.data as any
      if (data.success) {
        mfaSecret.value = data.secret || ''
        qrCodeUri.value = data.uri || ''
        showQRCode.value = true
        isBinding.value = true
        success.value = '请使用 Google Authenticator 等应用扫描二维码'
      } else {
        error.value = data.message || '生成 MFA 失败'
      }
    }
  } catch (e: any) {
    error.value = '生成 MFA 失败'
    console.error(e)
  } finally {
    loading.value = false
  }
}

async function verifyMFA() {
  if (!verificationCode.value || verificationCode.value.length !== 6) {
    error.value = '请输入 6 位验证码'
    return
  }

  loading.value = true
  error.value = ''
  
  try {
    const response = await post('/mfa/verify', {
      enabled: true,
      code: verificationCode.value
    })
    
    if (response.data) {
      const data = response.data as any
      if (data.success) {
        mfaEnabled.value = true
        isBinding.value = false
        showQRCode.value = false
        success.value = 'MFA 启用成功'
        verificationCode.value = ''
      } else {
        error.value = data.message || '验证码错误'
      }
    }
  } catch (e: any) {
    error.value = '验证失败'
    console.error(e)
  } finally {
    loading.value = false
  }
}

async function disableMFA() {
  if (!mfaEnabled.value) return

  const code = verificationCode.value || ''
  if (!code) {
    error.value = '请输入验证码以禁用 MFA'
    return
  }

  loading.value = true
  error.value = ''
  
  try {
    const response = await post('/mfa/verify', {
      enabled: false,
      code: code
    })
    
    if (response.data) {
      const data = response.data as any
      if (data.success) {
        mfaEnabled.value = false
        verificationCode.value = ''
        success.value = 'MFA 已禁用'
      } else {
        error.value = data.message || '禁用失败'
      }
    }
  } catch (e: any) {
    error.value = '禁用失败'
    console.error(e)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-900 mb-6">MFA 绑定</h1>
    
    <div class="max-w-2xl mx-auto">
      <!-- 状态信息 -->
      <div v-if="mfaEnabled" class="mb-6 p-4 bg-green-50 border border-green-200 rounded-lg">
        <div class="flex items-center">
          <svg class="w-5 h-5 text-green-500 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span class="text-green-700">MFA 已启用，您的账户受到保护</span>
        </div>
      </div>

      <!-- 错误提示 -->
      <div v-if="error" class="mb-6 p-4 bg-red-50 border border-red-200 rounded-lg">
        <div class="flex items-center">
          <svg class="w-5 h-5 text-red-500 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span class="text-red-700">{{ error }}</span>
        </div>
      </div>

      <!-- 成功提示 -->
      <div v-if="success" class="mb-6 p-4 bg-green-50 border border-green-200 rounded-lg">
        <div class="flex items-center">
          <svg class="w-5 h-5 text-green-500 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span class="text-green-700">{{ success }}</span>
        </div>
      </div>

      <!-- 主卡片 -->
      <div class="bg-white rounded-lg shadow p-6">
        <div v-if="!mfaEnabled && !isBinding" class="text-center">
          <p class="text-gray-600 mb-4">启用双因素认证 (MFA) 可以为您的账户提供额外的安全保障</p>
          <button
            @click="generateMFA"
            :disabled="loading"
            class="px-6 py-2 bg-primary-600 text-white rounded-lg hover:bg-primary-700 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {{ loading ? '生成中...' : '启用 MFA' }}
          </button>
        </div>

        <div v-else-if="isBinding && showQRCode" class="text-center">
          <h3 class="text-lg font-medium text-gray-900 mb-4">扫描二维码</h3>
          <p class="text-sm text-gray-600 mb-4">1. 下载 Google Authenticator 或类似应用</p>
          <p class="text-sm text-gray-600 mb-4">2. 使用应用扫描下方二维码</p>
          
          <!-- 二维码 -->
          <div v-if="qrCodeUri" class="mb-4 flex justify-center">
            <QrcodeVue :value="qrCodeUri" :size="200" level="H" />
          </div>
          
          <!-- 或手动输入密钥 -->
          <div class="mb-4">
            <p class="text-sm text-gray-600">或手动输入密钥：</p>
            <code class="block mt-1 p-2 bg-gray-100 rounded text-sm font-mono">{{ mfaSecret }}</code>
          </div>

          <!-- 验证码输入 -->
          <div class="mt-6">
            <label class="block text-sm font-medium text-gray-700 mb-2">输入 6 位验证码</label>
            <input
              v-model="verificationCode"
              type="text"
              maxlength="6"
              pattern="[0-9]*"
              inputmode="numeric"
              class="w-full max-w-xs mx-auto block px-4 py-2 border border-gray-300 rounded-lg text-center text-2xl tracking-widest"
              placeholder="000000"
            />
          </div>

          <div class="mt-4 flex justify-center space-x-4">
            <button
              @click="verifyMFA"
              :disabled="loading || verificationCode.length !== 6"
              class="px-6 py-2 bg-green-600 text-white rounded-lg hover:bg-green-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {{ loading ? '验证中...' : '验证并启用' }}
            </button>
            <button
              @click="() => { isBinding = false; showQRCode = false; error = ''; success = ''; }"
              class="px-6 py-2 bg-gray-300 text-gray-700 rounded-lg hover:bg-gray-400"
            >
              取消
            </button>
          </div>
        </div>

        <div v-else-if="mfaEnabled" class="text-center">
          <h3 class="text-lg font-medium text-gray-900 mb-4">禁用 MFA</h3>
          <p class="text-sm text-gray-600 mb-4">输入您的 MFA 验证码以禁用双因素认证</p>
          
          <input
            v-model="verificationCode"
            type="text"
            maxlength="6"
            pattern="[0-9]*"
            inputmode="numeric"
            class="w-full max-w-xs mx-auto block px-4 py-2 border border-gray-300 rounded-lg text-center text-2xl tracking-widest mb-4"
            placeholder="000000"
          />
          
          <button
            @click="disableMFA"
            :disabled="loading || !verificationCode"
            class="px-6 py-2 bg-red-600 text-white rounded-lg hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {{ loading ? '禁用中...' : '禁用 MFA' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
