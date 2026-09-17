import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  server: {
    fs: {
      // Allow the dev server to read the repo-root LICENSE file, which LicenseModal.vue
      // imports directly (via `?raw`) so the in-app text can never drift from the real one.
      allow: ['.', fileURLToPath(new URL('../..', import.meta.url))],
    },
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
      }
    }
  }
})
