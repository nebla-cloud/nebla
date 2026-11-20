<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { errorMessage } from '@/api/errors'
import AccountMenu from '@/components/AccountMenu.vue'
import BrandMark from '@/components/BrandMark.vue'
import EmptyState from '@/components/EmptyState.vue'
import { useSession } from '@/lib/session'
import NewProjectForm from './NewProjectForm.vue'
import { useProjects } from './queries'

const route = useRoute()
const { manifest, me } = useSession()
const { data: projects, isPending, error: loadError } = useProjects()

const product = computed(() => manifest.value?.branding.product_name || 'Nebla')
const focusNew = route.query.new !== undefined

function plural(n: number, word: string) {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}
</script>

<template>
  <div class="min-h-dvh">
    <header
      class="flex h-14 items-center justify-between gap-3 border-b border-default px-4 sm:px-8"
    >
      <div class="flex items-center gap-2.5">
        <BrandMark :size="22" />
        <span class="font-extrabold tracking-tight text-highlighted">{{ product }}</span>
      </div>
      <div class="flex items-center gap-1">
        <UButton v-if="me?.is_admin" to="/admin" color="neutral" variant="ghost">Admin</UButton>
        <div class="w-44 sm:w-56"><AccountMenu /></div>
      </div>
    </header>

    <main class="mx-auto flex w-full max-w-2xl flex-col gap-8 px-4 py-10 sm:px-8">
      <h1 class="text-2xl font-extrabold tracking-tight text-highlighted">Projects</h1>

      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        :description="errorMessage(loadError)"
      />
      <p v-else-if="isPending" class="text-muted">Loading projects…</p>

      <template v-else>
        <ul
          v-if="projects?.length"
          class="divide-y divide-default overflow-hidden rounded-lg border border-default"
        >
          <li v-for="p in projects" :key="p.id">
            <RouterLink
              :to="`/p/${p.id}/servers`"
              class="flex items-center justify-between gap-4 px-4 py-3.5 hover:bg-elevated/60 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-primary"
            >
              <span class="min-w-0 truncate font-semibold text-highlighted">{{ p.name }}</span>
              <span class="shrink-0 text-sm text-muted">{{
                plural(p.usage.servers, 'server')
              }}</span>
            </RouterLink>
          </li>
        </ul>

        <section v-if="projects?.length" class="flex flex-col gap-3">
          <h2 class="text-base font-bold text-highlighted">New project</h2>
          <NewProjectForm :autofocus="focusNew" />
        </section>
        <EmptyState
          v-else
          title="Create your first project"
          body="A project holds your servers and SSH keys. Name it after what it runs, like Home lab."
        >
          <NewProjectForm autofocus />
        </EmptyState>
      </template>
    </main>
  </div>
</template>
