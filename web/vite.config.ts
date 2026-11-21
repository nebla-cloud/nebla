// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'
import type { Plugin } from 'vite'

// A self-hosted panel never calls api.iconify.design: every icon is bundled, and
// @iconify/vue gets no fetch(), so a missing icon renders blank instead of phoning out.
function noIconifyApi(): Plugin {
  const needle = 'let fetchModule = detectFetch();'
  let patched = false
  return {
    name: 'nebla:no-iconify-api',
    enforce: 'pre',
    transform(code, id) {
      if (!/[\\/]@iconify[\\/]vue[\\/]dist[\\/]iconify\.mjs$/.test(id.split('?')[0]!)) return
      if (!code.includes(needle)) throw new Error('@iconify/vue changed; update noIconifyApi')
      patched = true
      return code.replace(needle, 'let fetchModule = undefined;')
    },
    generateBundle() {
      if (!patched) this.error('@iconify/vue was not patched; update noIconifyApi')
    },
  }
}

export default defineConfig({
  plugins: [
    vue(),
    noIconifyApi(),
    ui({
      autoImport: false,
      components: false,
      // Scan .ts too: nav and route meta name their icons there.
      icon: { clientBundle: { scan: { globInclude: ['src/**/*.{vue,ts}'] } } },
      ui: {
        colors: { primary: 'ink', neutral: 'ink' },
        button: {
          slots: { base: 'font-semibold' },
          compoundVariants: [{ color: 'primary', variant: 'solid', class: 'nb-gradient-underline' }],
        },
        input: {
          slots: { base: 'h-9' },
          compoundVariants: [{ variant: 'outline', class: 'ring-(color:--ui-border)' }],
        },
        textarea: { compoundVariants: [{ variant: 'outline', class: 'ring-(color:--ui-border)' }] },
        modal: { slots: { content: 'rounded-(--nb-radius-panel) p-5' } },
        dropdownMenu: { slots: { content: 'rounded-[12px] shadow-[0_8px_24px_-8px_rgb(21_24_29/0.18)]' } },
        popover: { slots: { content: 'rounded-[12px] shadow-[0_8px_24px_-8px_rgb(21_24_29/0.18)]' } },
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
  // Dev pre-bundling would skip the transform above.
  optimizeDeps: { exclude: ['@iconify/vue'] },
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
