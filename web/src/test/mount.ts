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

import { defineComponent, h, type Component } from 'vue'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router, type RouteRecordRaw } from 'vue-router'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import ui from '@nuxt/ui/vue-plugin'
import UApp from '@nuxt/ui/components/App.vue'

export function testQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

export function testRouter(routes: RouteRecordRaw[] = []): Router {
  const stub = defineComponent({ render: () => h('div') })
  return createRouter({
    history: createMemoryHistory(),
    routes: [...routes, { path: '/:pathMatch(.*)*', name: 'any', component: stub }],
  })
}

export function mountApp(
  component: Component,
  opts: { router?: Router; queryClient?: QueryClient; props?: Record<string, unknown> } = {},
) {
  const router = opts.router ?? testRouter()
  const queryClient = opts.queryClient ?? testQueryClient()
  const Root = defineComponent({
    render: () => h(UApp, null, { default: () => h(component, opts.props ?? {}) }),
  })
  const wrapper = mount(Root, {
    attachTo: document.body,
    global: { plugins: [router, ui, [VueQueryPlugin, { queryClient }]] },
  })
  return { wrapper, router, queryClient }
}

export function withSetup<T>(fn: () => T, queryClient = testQueryClient()) {
  let result!: T
  const C = defineComponent({
    setup() {
      result = fn()
      return () => h('div')
    },
  })
  const wrapper = mount(C, {
    global: { plugins: [testRouter(), [VueQueryPlugin, { queryClient }]] },
  })
  return { result, wrapper, queryClient }
}
