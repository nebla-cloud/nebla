<script setup lang="ts">
import { useRoute } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { errorMessage } from '@/api/errors'
import AccountMenu from '@/components/AccountMenu.vue'
import EmptyState from '@/components/EmptyState.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import Lockup from '@/components/Lockup.vue'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
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

    <main class="mx-auto flex w-full max-w-2xl flex-col gap-4 px-4 py-10 sm:px-8">
      <PageHeader title="Projects" />

      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        :description="errorMessage(loadError)"
      />
      <p v-else-if="isPending" class="text-muted">Loading projects…</p>

      <template v-else>
        <ListGroup v-if="projects?.length">
          <ListRow v-for="p in projects" :key="p.id" :to="`/p/${p.id}/servers`">
            <template #leading>
              <span
                aria-hidden="true"
                class="grid size-7 place-items-center rounded-[8px] bg-(--ui-bg-muted) text-xs font-bold text-highlighted ring-1 ring-default"
                >{{ p.name.charAt(0).toUpperCase() }}</span
              >
            </template>
            {{ p.name }}
            <template #meta>{{ plural(p.usage.servers, 'server') }}</template>
          </ListRow>
        </ListGroup>

        <Panel v-if="projects?.length" title="New project">
          <NewProjectForm :autofocus="focusNew" />
        </Panel>
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
