<template>
  <div class="cookies-page">
    <!-- 页面标题 -->
    <div class="page-header">
      <h1 class="text-2xl font-bold text-gray-800 dark:text-gray-100">Cookie 管理</h1>
      <p class="text-gray-600 dark:text-gray-400 mt-1">
        安全存储网站 Cookie，支持最高画质下载
      </p>
    </div>

    <!-- 高画质支持状态卡片 -->
    <div class="status-cards">
      <div
        v-for="site in siteCookieStatus"
        :key="site.domain"
        class="status-card"
        :class="{ 'has-cookie': site.hasCookie }"
      >
        <div class="status-icon">
          <svg v-if="site.hasCookie" class="icon-success" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <svg v-else class="icon-warning" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
        </div>
        <div class="status-info">
          <div class="status-name">{{ site.name }}</div>
          <div class="status-text" :class="site.hasCookie ? 'text-success' : 'text-muted'">
            {{ site.hasCookie ? '已配置，支持最高画质' : '未配置，画质可能受限' }}
          </div>
        </div>
      </div>
    </div>

    <!-- 添加 Cookie 表单 -->
    <div class="add-cookie-section">
      <h2 class="section-title">添加新 Cookie</h2>
      
      <!-- 帮助提示 -->
      <div class="help-tooltip">
        <button @click="showHelp = !showHelp" class="help-button">
          <svg class="help-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span>如何获取 Cookie？</span>
        </button>
        <div v-if="showHelp" class="help-content">
          <h4>📖 如何获取 Cookie 文件</h4>
          <ol>
            <li>安装浏览器插件 <strong>Get cookies.txt</strong> 或 <strong>Get cookies.txt LOCALLY</strong></li>
            <li>登录目标网站（如 Bilibili、YouTube）</li>
            <li>点击插件图标，导出当前网站的 Cookie</li>
            <li>复制导出的全部内容（Netscape 格式）</li>
            <li>粘贴到下方文本框中</li>
          </ol>
          <div class="help-plugins">
            <p><strong>推荐插件：</strong></p>
            <ul>
              <li>Chrome: <a href="https://chrome.google.com/webstore/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc" target="_blank">Get cookies.txt LOCALLY</a></li>
              <li>Firefox: <a href="https://addons.mozilla.org/firefox/addon/cookies-txt/" target="_blank">cookies.txt</a></li>
            </ul>
          </div>
        </div>
      </div>

      <form @submit.prevent="handleSubmit" class="cookie-form">
        <!-- Cookie 内容输入 -->
        <div class="form-group">
          <label for="cookie-content" class="form-label">
            Cookie 内容
            <span v-if="autoDetectedDomain" class="auto-detected-badge">
              已自动识别域名：{{ autoDetectedDomain }}
            </span>
          </label>
          <textarea
            id="cookie-content"
            v-model="cookieContent"
            class="form-textarea"
            placeholder="在此粘贴 Cookie 内容（Netscape 格式）..."
            rows="10"
            @input="handleContentInput"
          ></textarea>
          <p v-if="formatError" class="error-message">{{ formatError }}</p>

          <!-- Cookie 状态标签 -->
          <div v-if="cookieStatus" class="cookie-status-badge">
            <div class="status-row">
              <span class="status-item">
                <svg class="status-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                </svg>
                有效条目：{{ cookieStatus.filteredCount }} 条
              </span>
              <span v-if="cookieStatus.expiresInfo" class="status-item">
                <svg class="status-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                最早过期：{{ formatDateFromUnix(cookieStatus.expiresInfo.earliest) }}
              </span>
              <span v-if="cookieStatus.expiresInfo" class="status-item">
                <svg class="status-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
                </svg>
                最晚过期：{{ formatDateFromUnix(cookieStatus.expiresInfo.latest) }}
              </span>
            </div>
            <div v-if="cookieStatus.isExpiring" class="status-warning">
              <svg class="warning-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
              注意：部分 Cookie 可能在 7 天内过期，建议及时更新
            </div>
          </div>
        </div>

        <!-- 域名输入 -->
        <div class="form-group">
          <label for="domain" class="form-label">域名</label>
          <input
            id="domain"
            v-model="domain"
            type="text"
            class="form-input"
            :class="{ 'input-error': domainError }"
            placeholder="例如：bilibili.com"
            :disabled="!!autoDetectedDomain"
            @input="validateDomain"
          />
          <p v-if="domainError" class="error-message">{{ domainError }}</p>
        </div>

        <!-- 管理员共享开关 -->
        <div v-if="isAdmin" class="form-group form-checkbox-group">
          <label class="checkbox-label">
            <input type="checkbox" v-model="isShared" />
            <span class="checkbox-text">
              设为全局共享
              <span class="checkbox-hint">（其他用户无此域名 Cookie 时将自动复用）</span>
            </span>
          </label>
        </div>

        <!-- 提交按钮 -->
        <button type="submit" class="submit-button" :disabled="loading || !cookieContent || !domain">
          <svg v-if="loading" class="spinner" viewBox="0 0 24 24">
            <circle class="spinner-path" cx="12" cy="12" r="10" fill="none" stroke-width="4" />
          </svg>
          <svg v-else-if="saveSuccess" class="icon-success-small" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
          </svg>
          <span>{{ loading ? '加密保存中...' : saveSuccess ? '保存成功' : '保存 Cookie' }}</span>
        </button>
      </form>
    </div>

    <!-- Cookie 列表 -->
    <div class="cookies-list-section">
      <h2 class="section-title">已保存的 Cookie</h2>
      
      <div v-if="cookies.length === 0" class="empty-state">
        <svg class="empty-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
        </svg>
        <p>尚未保存任何 Cookie</p>
      </div>

      <div v-else class="cookies-grid">
        <div v-for="cookie in cookies" :key="cookie.domain" class="cookie-card">
          <div class="cookie-header">
            <div class="cookie-domain">
              <span class="domain-badge">{{ cookie.domain }}</span>
              <span v-if="cookie.is_shared" class="shared-badge">🔓 共享</span>
              <span v-else class="private-badge">🔒 私有</span>
            </div>
            <button @click="handleDeleteCookie(cookie.domain)" class="delete-button" title="删除">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
            </button>
          </div>
          <div class="cookie-footer">
            <span class="update-time">更新于 {{ formatDate(cookie.updated_at) }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { getPublicKey, saveCookie, getCookies, deleteCookie, type CookieInfo } from '@/api'
import { encryptCookie, extractDomainFromCookie, validateCookieFormat, cleanCookieContent, filterCookieByDomain } from '@/utils/cookieCrypto'

// 状态管理
const authStore = useAuthStore()
const isAdmin = computed(() => authStore.user?.role === 'admin')

// 表单数据
const cookieContent = ref('')
const domain = ref('')
const isShared = ref(false)
const autoDetectedDomain = ref('')
const formatError = ref('')
const domainError = ref('')

// UI 状态
const loading = ref(false)
const saveSuccess = ref(false)
const showHelp = ref(false)
const cookies = ref<CookieInfo[]>([])

// Cookie 状态标签
const cookieStatus = ref<{
  totalCount: number
  filteredCount: number
  expiresInfo: { earliest: number; latest: number } | null
  isExpiring: boolean
  domainStats: Record<string, number>
} | null>(null)

// 支持的网站列表
const supportedSites = [
  { name: 'Bilibili', domain: 'bilibili.com' },
  { name: 'YouTube', domain: 'youtube.com' },
  { name: '优酷', domain: 'youku.com' },
  { name: '爱奇艺', domain: 'iqiyi.com' },
  { name: '腾讯视频', domain: 'v.qq.com' },
]

const siteCookieStatus = computed(() => {
  const cookieDomains = new Set(cookies.value.map(c => c.domain))
  return supportedSites.map(site => ({
    ...site,
    hasCookie: cookieDomains.has(site.domain),
  }))
})

// 自动识别域名并智能过滤
function handleContentInput() {
  const detected = extractDomainFromCookie(cookieContent.value)
  if (detected) {
    autoDetectedDomain.value = detected
    domain.value = detected
    domainError.value = ''

    // 智能过滤：如果用户已经选择了域名，自动过滤无关条目
    if (domain.value) {
      const filterResult = filterCookieByDomain(cookieContent.value, domain.value)
      cookieStatus.value = {
        totalCount: filterResult.totalCount,
        filteredCount: filterResult.filteredCount,
        expiresInfo: filterResult.expiresInfo,
        isExpiring: filterResult.expiresInfo
          ? filterResult.expiresInfo.latest < Date.now() / 1000 + 7 * 24 * 3600 // 7天内过期
          : false,
        domainStats: filterResult.domainStats,
      }

      // 如果有无关条目，提示用户
      if (filterResult.filteredCount < filterResult.totalCount) {
        const removed = filterResult.totalCount - filterResult.filteredCount
        window.toast?.info(
          `已自动为你剔除 ${removed} 条无关站点的 Cookie，保留 ${filterResult.filteredCount} 条 ${domain.value} 相关记录`,
          3000
        )
      }
    }

    formatError.value = validateCookieFormat(cookieContent.value) || ''
  } else {
    autoDetectedDomain.value = ''
    cookieStatus.value = null
    if (cookieContent.value.trim()) {
      formatError.value = '无法识别域名，请手动输入'
    } else {
      formatError.value = ''
    }
  }
}

// 验证域名格式
function validateDomain() {
  if (!domain.value) {
    domainError.value = ''
    return
  }
  
  // 域名正则：仅允许字母、数字、点、连字符
  const domainRegex = /^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$/
  
  if (!domainRegex.test(domain.value)) {
    domainError.value = '域名格式不正确，仅支持字母、数字、点和连字符'
  } else if (domain.value.length > 253) {
    domainError.value = '域名长度不能超过 253 个字符'
  } else {
    domainError.value = ''
  }
}

// 提交表单
async function handleSubmit() {
  if (!cookieContent.value || !domain.value) {
    alert('请填写完整内容')
    return
  }
  
  // 验证域名
  validateDomain()
  if (domainError.value) {
    alert(domainError.value)
    return
  }
  
  // 验证格式
  const formatErr = validateCookieFormat(cookieContent.value)
  if (formatErr) {
    formatError.value = formatErr
    alert(formatErr)
    return
  }
  
  loading.value = true
  saveSuccess.value = false
  formatError.value = ''

  try {
    // 自动清理备注行和空行
    const cleanedContent = cleanCookieContent(cookieContent.value)

    const publicKey = await getPublicKey()
    const encryptedData = await encryptCookie(cleanedContent, publicKey)
    await saveCookie(domain.value, encryptedData, isShared.value)

    saveSuccess.value = true
    window.toast?.success('Cookie 已安全保存', 2000)

    setTimeout(() => {
      saveSuccess.value = false
      cookieContent.value = ''
      domain.value = ''
      isShared.value = false
      autoDetectedDomain.value = ''
    }, 1500)

    await fetchCookies()
  } catch (err: any) {
    console.error('[Cookie 保存失败]', err)
    formatError.value = err?.message || '保存失败'
    window.toast?.error(formatError.value, 5000)
  } finally {
    loading.value = false
  }
}

// 获取 Cookie 列表
async function fetchCookies() {
  try {
    cookies.value = await getCookies()
  } catch (error) {
    console.error('获取 Cookie 列表失败:', error)
  }
}

// 删除 Cookie
async function handleDeleteCookie(domainName: string) {
  if (!confirm(`确定要删除 ${domainName} 的 Cookie 吗？`)) return
  
  try {
    await deleteCookie(domainName)
    window.toast?.success('Cookie 已删除')
    await fetchCookies()
  } catch (err: any) {
    console.error('删除 Cookie 失败:', err)
    window.toast?.error(err.message || '删除失败')
  }
}

// 格式化日期
function formatDate(dateStr: string): string {
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// 格式化 Unix 时间戳（秒）为可读日期
function formatDateFromUnix(unixSeconds: number): string {
  const date = new Date(unixSeconds * 1000)
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// 组件挂载时获取数据
onMounted(() => {
  fetchCookies()
})
</script>

<style scoped>
.cookies-page {
  max-width: 1200px;
  margin: 0 auto;
  padding: 2rem;
}

.page-header {
  margin-bottom: 2rem;
}

/* 状态卡片 */
.status-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 1rem;
  margin-bottom: 2rem;
}

.status-card {
  @apply bg-white dark:bg-gray-800 rounded-lg p-4 shadow-sm border border-gray-200 dark:border-gray-700 flex items-center gap-3;
  transition: all 0.3s ease;
}

.status-card.has-cookie {
  @apply border-green-500 dark:border-green-600;
}

.status-icon {
  @apply flex-shrink-0 w-10 h-10 rounded-full flex items-center justify-center;
}

.icon-success {
  @apply w-6 h-6 text-green-500;
}

.icon-warning {
  @apply w-6 h-6 text-yellow-500;
}

.status-info {
  @apply flex-1;
}

.status-name {
  @apply font-semibold text-gray-800 dark:text-gray-100;
}

.status-text {
  @apply text-sm mt-0.5;
}

.text-success {
  @apply text-green-600 dark:text-green-400;
}

.text-muted {
  @apply text-gray-500;
}

/* 添加 Cookie 区域 */
.add-cookie-section {
  @apply bg-white dark:bg-gray-800 rounded-lg p-6 shadow-sm border border-gray-200 dark:border-gray-700 mb-6;
}

.section-title {
  @apply text-lg font-semibold text-gray-800 dark:text-gray-100 mb-4;
}

/* 帮助提示 */
.help-tooltip {
  @apply relative inline-block mb-4;
}

.help-button {
  @apply flex items-center gap-2 text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 transition-colors;
}

.help-icon {
  @apply w-5 h-5;
}

.help-content {
  @apply absolute top-full left-0 mt-2 w-80 bg-white dark:bg-gray-800 rounded-lg shadow-lg border border-gray-200 dark:border-gray-700 p-4 z-10;
}

.help-content h4 {
  @apply font-semibold text-gray-800 dark:text-gray-100 mb-2;
}

.help-content ol {
  @apply list-decimal list-inside space-y-1 text-sm text-gray-700 dark:text-gray-300 mb-3;
}

.help-plugins {
  @apply text-xs text-gray-600 dark:text-gray-400 pt-2 border-t border-gray-200 dark:border-gray-700;
}

.help-plugins a {
  @apply text-blue-600 dark:text-blue-400 hover:underline;
}

/* 表单 */
.cookie-form {
  @apply space-y-4;
}

.form-group {
  @apply space-y-2;
}

.form-label {
  @apply block text-sm font-medium text-gray-700 dark:text-gray-300;
}

.auto-detected-badge {
  @apply inline-block ml-2 px-2 py-0.5 text-xs bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 rounded;
}

.form-textarea {
  @apply w-full px-4 py-3 rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-800 dark:text-gray-100 focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all font-mono text-sm;
  resize: vertical;
}

.form-input {
  @apply w-full px-4 py-2 rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-800 dark:text-gray-100 focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all;
}

.input-error {
  @apply border-red-500 dark:border-red-600 focus:ring-red-500;
}

.error-message {
  @apply text-sm text-red-600 dark:text-red-400;
}

/* Cookie 状态标签 */
.cookie-status-badge {
  @apply mt-2 p-3 bg-blue-50 dark:bg-blue-900/20 rounded-lg border border-blue-200 dark:border-blue-800;
}

.status-row {
  @apply flex flex-wrap gap-3 text-sm;
}

.status-item {
  @apply flex items-center gap-1.5 text-gray-700 dark:text-gray-300;
}

.status-icon {
  @apply w-4 h-4 text-blue-600 dark:text-blue-400 flex-shrink-0;
}

.status-warning {
  @apply mt-2 flex items-center gap-1.5 text-sm text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-900/20 px-2 py-1 rounded;
}

.warning-icon {
  @apply w-4 h-4 text-amber-600 dark:text-amber-400 flex-shrink-0;
}

/* 复选框 */
.form-checkbox-group {
  @apply pt-2;
}

.checkbox-label {
  @apply flex items-center gap-2 cursor-pointer;
}

.checkbox-text {
  @apply text-sm text-gray-700 dark:text-gray-300;
}

.checkbox-hint {
  @apply text-xs text-gray-500;
}

/* 提交按钮 */
.submit-button {
  @apply w-full flex items-center justify-center gap-2 px-6 py-3 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg transition-all disabled:opacity-50 disabled:cursor-not-allowed;
}

.spinner {
  @apply w-5 h-5 animate-spin;
}

.spinner-path {
  @apply stroke-current;
}

.icon-success-small {
  @apply w-5 h-5 text-white;
}

/* Cookie 列表 */
.cookies-list-section {
  @apply bg-white dark:bg-gray-800 rounded-lg p-6 shadow-sm border border-gray-200 dark:border-gray-700;
}

.empty-state {
  @apply text-center py-12 text-gray-500 dark:text-gray-400;
}

.empty-icon {
  @apply w-16 h-16 mx-auto mb-4 opacity-50;
}

.cookies-grid {
  @apply grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4;
}

.cookie-card {
  @apply bg-gray-50 dark:bg-gray-700 rounded-lg p-4 border border-gray-200 dark:border-gray-600;
}

.cookie-header {
  @apply flex items-center justify-between mb-3;
}

.domain-badge {
  @apply inline-block px-3 py-1 text-sm font-medium bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 rounded-full;
}

.shared-badge {
  @apply inline-block ml-2 px-2 py-0.5 text-xs bg-green-100 dark:bg-green-900 text-green-700 dark:text-green-300 rounded;
}

.private-badge {
  @apply inline-block ml-2 px-2 py-0.5 text-xs bg-gray-100 dark:bg-gray-600 text-gray-700 dark:text-gray-300 rounded;
}

.delete-button {
  @apply p-2 text-gray-500 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20 rounded transition-colors;
}

.delete-button svg {
  @apply w-5 h-5;
}

.cookie-footer {
  @apply text-xs text-gray-500 dark:text-gray-400;
}

/* 响应式 */
@media (max-width: 768px) {
  .cookies-page {
    padding: 1rem;
  }
  
  .status-cards {
    grid-template-columns: 1fr;
  }
  
  .cookies-grid {
    grid-template-columns: 1fr;
  }
}
</style>
