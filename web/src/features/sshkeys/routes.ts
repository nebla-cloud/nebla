import type { RouteRecordRaw } from 'vue-router'

export const side = 'project'

export default [
  {
    path: 'ssh-keys',
    name: 'ssh-keys',
    component: () => import('./SshKeysPage.vue'),
    meta: { side: 'project', nav: { label: 'SSH keys', order: 20, icon: 'i-lucide-key-round', group: 'Access' } },
  },
] satisfies RouteRecordRaw[]
