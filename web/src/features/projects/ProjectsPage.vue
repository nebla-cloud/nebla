<script setup lang="ts">
import { RouterLink, useRoute } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { errorMessage } from '@/api/errors'
import AccountMenu from '@/components/AccountMenu.vue'
import EmptyState from '@/components/EmptyState.vue'
import Lockup from '@/components/Lockup.vue'
import TopBar from '@/layouts/TopBar.vue'
import { useSession } from '@/lib/session'
import NewProjectForm from './NewProjectForm.vue'
import { useProjects } from './queries'

const route = useRoute()
const { me } = useSession()
const { data: projects, isPending, error: loadError } = useProjects()

const focusNew = route.query.new !== undefined

function plural(n: number, word: string) {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}
</script>

<template>
  <div class="min-h-dvh">
    <TopBar>
      <template #leading>
        <span class="flex min-w-0 px-2 lg:px-0"><Lockup :height="22" /></span>
      </template>
      <template #actions>
        <UButton v-if="me?.is_admin" to="/admin" color="neutral" variant="ghost">Admin</UButton>
        <div class="w-44 sm:w-56"><AccountMenu /></div>
      </template>
    </TopBar>

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
