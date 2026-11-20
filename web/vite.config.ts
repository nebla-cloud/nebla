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
        button: {
          slots: { base: 'font-semibold' },
          compoundVariants: [{ color: 'primary', variant: 'solid', class: 'nb-gradient-underline' }],
        },
        input: { slots: { base: 'h-9 ring-(color:--ui-border)' } },
        textarea: { slots: { base: 'ring-(color:--ui-border)' } },
        modal: { slots: { content: 'rounded-(--nb-radius-panel) p-5' } },
        dropdownMenu: { slots: { content: 'rounded-xl shadow-[0_8px_24px_-8px_rgb(21_24_29/0.18)]' } },
        popover: { slots: { content: 'rounded-xl shadow-[0_8px_24px_-8px_rgb(21_24_29/0.18)]' } },
        badge: {
          compoundVariants: ['xs', 'sm', 'md', 'lg', 'xl'].map((size) => ({ size, class: 'rounded-full' })),
        },
        alert: {
          compoundVariants: [
            { color: 'success', variant: 'subtle', class: { root: 'bg-(--nb-run-bg) text-(--nb-run-fg) ring-(color:--nb-run)' } },
            { color: 'info', variant: 'subtle', class: { root: 'bg-(--nb-soft) text-(--nb-busy-fg) ring-(color:--nb-busy)' } },
            { color: 'error', variant: 'subtle', class: { root: 'bg-(--nb-soft) text-error ring-(color:--nb-err)' } },
          ],
        },
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
