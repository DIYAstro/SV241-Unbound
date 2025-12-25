import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:32241',
        changeOrigin: true,
      },
      '/setup': {
        target: 'http://localhost:32241',
        changeOrigin: true,
      },
      '/management': {
        target: 'http://localhost:32241',
        changeOrigin: true,
      },
      '/ws': {
        target: 'http://localhost:32241',
        ws: true,
        changeOrigin: true,
      },
      '/flasher': {
        target: 'http://localhost:32241',
        changeOrigin: true,
      }
    }
  }
})
