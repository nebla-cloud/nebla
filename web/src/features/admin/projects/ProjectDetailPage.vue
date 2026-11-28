<!--
Copyright (C) 2026 The Nebla contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
-->

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UAvatar from '@nuxt/ui/components/Avatar.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { ApiError, errorMessage } from '@/api/errors'
import ConfirmByName from '@/components/ConfirmByName.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import NotFoundPage from '@/components/NotFoundPage.vue'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import { useCrumbs } from '@/lib/crumbs'
import { formatDate } from '@/lib/format'
import { roleLabel } from '@/lib/roles'
import QuotaEditor from './QuotaEditor.vue'
import { useAdminProject, useDeleteProject } from './queries'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const id = computed(() => Number(route.params.projectId))
const { data: project, isPending, error: loadError } = useAdminProject(id)
const notFound = computed(
  () => loadError.value instanceof ApiError && loadError.value.status === 404,
)

useCrumbs(() => [
  { label: 'Projects', to: '/admin/projects' },
  ...(project.value ? [{ label: project.value.name }] : []),
])

const del = useDeleteProject()
const confirmOpen = ref(false)
const delError = ref<string | null>(null)

watch(confirmOpen, (v) => {
  if (v) delError.value = null
})

async function remove() {
  delError.value = null
  try {
    await del.mutateAsync(id.value)
    toast.add({ title: `Deleted ${project.value?.name ?? 'project'}`, color: 'success' })
    confirmOpen.value = false
    await router.push('/admin/projects')
  } catch (e) {
    delError.value =
      e instanceof ApiError && e.code === 'project_not_empty'
        ? "Delete the project's servers, volumes, primary IPs, floating IPs, networks and snapshots first."
        : errorMessage(e)
  }
}
</script>

<template>
  <NotFoundPage v-if="notFound" />
  <div v-else class="flex flex-col gap-4">
    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending || !project" class="text-muted">Loading project…</p>
    <template v-else>
      <PageHeader
        :title="project.name"
        :description="`Owned by ${project.owner_email}. Created ${formatDate(project.created)}.`"
      />

      <Panel title="Usage and quota">
        <QuotaEditor :project-id="project.id" :quota="project.quota" :usage="project.usage" />
      </Panel>

      <Panel data-section="members" title="Members">
        <ListGroup>
          <ListRow v-for="m in project.members" :key="m.user_id">
            <template #leading><UAvatar :alt="m.name || m.email" size="md" /></template>
            <span class="flex min-w-0 flex-col">
              <span class="truncate">{{ m.name || m.email }}</span>
              <span class="truncate text-sm font-normal text-muted">{{ m.email }}</span>
            </span>
            <template #meta>{{ roleLabel(m.role) }}</template>
          </ListRow>
        </ListGroup>
      </Panel>

      <Panel title="Activity">
        <UButton
          :to="`/admin/activity?project=${project.id}`"
          color="neutral"
          variant="outline"
          icon="i-lucide-history"
        >
          View activity
        </UButton>
      </Panel>

      <Panel title="Delete project">
        <p class="mb-3 text-muted">
          A project with servers, volumes, primary IPs, floating IPs, networks or snapshots can't be
          deleted. Its members need to delete them first.
        </p>
        <UButton
          color="error"
          variant="outline"
          icon="i-lucide-trash-2"
          @click="confirmOpen = true"
        >
          Delete project
        </UButton>
      </Panel>

      <ConfirmByName
        v-model:open="confirmOpen"
        :name="project.name"
        title="Delete this project?"
        confirm-label="Delete project"
        :loading="del.isPending.value"
        :error="delError"
        @confirm="remove"
      />
    </template>
  </div>
</template>
