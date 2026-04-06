import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    minify: 'terser',
    // 代码分割优化
    rollupOptions: {
      output: {
        manualChunks: {
          // Vue 核心库分离
          'vendor': ['vue', 'vue-router', 'pinia'],
          // HTTP 库分离
          'utils': ['axios'],
        },
        // 资源文件命名
        assetFileNames: (assetInfo) => {
          const info = assetInfo.name?.split('.') ?? []
          const _extType = info[info.length - 1]
          if (assetInfo.name && /\.(woff|woff2|eot|ttf|otf)$/.test(assetInfo.name)) {
            return 'fonts/[name][extname]'
          }
          return 'assets/[name]-[hash][extname]'
        },
        // JS 文件命名
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
      },
    },
    // 分块大小警告阈值
    chunkSizeWarningLimit: 500,
    // Terser 优化配置
    terserOptions: {
      compress: {
        drop_console: true,
        drop_debugger: true,
        pure_funcs: ['console.log'],
        // 移除未使用的代码
        pure_getters: true,
        // 移除未使用的变量
        unused: true,
      },
    },
    // 压缩 CSS
    cssCodeSplit: true,
    // 压缩图片等资源
    assetsInlineLimit: 4096,
  },
  // CSS 配置
  css: {
    postcss: './postcss.config.js',
  },
})
