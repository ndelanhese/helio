import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { configDefaults, defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [tanstackRouter({ autoCodeSplitting: true }), react()],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
  test: {
    exclude: [...configDefaults.exclude, 'e2e/**'],
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    css: true,
  },
})
