import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// radar 服务默认监听 127.0.0.1:8090(见 scripts/dev.ps1)。
const RADAR_DEV_URL = process.env.RADAR_DEV_URL ?? 'http://127.0.0.1:8090'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
    // 开发期浏览器只跟 Vite 通信;/api 与 /healthz 代理到 radar,
    // 保持与生产一致的"同源 + HttpOnly Cookie"语义。
    proxy: {
      '/api': { target: RADAR_DEV_URL, changeOrigin: false },
      '/healthz': { target: RADAR_DEV_URL, changeOrigin: false },
    },
  },
  build: {
    // 只清空产物目录 dist;同级的 fallback.html 是常驻占位页,不会被碰。
    outDir: '../internal/webassets/dist',
    emptyOutDir: true,
  },
})
