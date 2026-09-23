import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 调试台挂在网关的 /debug/ 下（构建产物嵌入二进制）；
// 开发模式把 /api 代理到本机网关，前端代码不需要区分环境。
export default defineConfig({
  base: '/debug/',
  plugins: [vue()],
  server: {
    port: 5174,
    strictPort: true,
    proxy: {
      '/api': 'http://127.0.0.1:6199',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
