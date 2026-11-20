/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'

export default defineConfig({
  plugins: [
    vue(),
    ui({
      autoImport: false,
      components: false,
      icon: { clientBundle: { scan: true } },
      ui: {
        colors: { primary: 'ink', neutral: 'ink' },
        button: { slots: { base: 'font-semibold' } },
      },
    }),
  ],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    proxy: {
      '/v1': { target: 'http://localhost:8080', changeOrigin: true, ws: true },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['src/test/setup.ts'],
    // Node's built-in localStorage would shadow jsdom's
    execArgv: ['--no-experimental-webstorage'],
  },
})
