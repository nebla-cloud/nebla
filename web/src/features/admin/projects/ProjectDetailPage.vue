<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UAvatar from '@nuxt/ui/components/Avatar.vue'
import UIcon from '@nuxt/ui/components/Icon.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { ApiError, errorMessage } from '@/api/errors'
import ConfirmByName from '@/components/ConfirmByName.vue'
import NotFoundPage from '@/components/NotFoundPage.vue'
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
        ? 'This project still has servers. Its members need to delete them first.'
        : errorMessage(e)
  }
}
</script>

<template>
  <NotFoundPage v-if="notFound" />
  <div v-else class="flex flex-col gap-6">
    <RouterLink
      to="/admin/projects"
      class="inline-flex items-center gap-1 text-sm text-muted hover:text-highlighted"
    >
      <UIcon name="i-lucide-arrow-left" class="size-4" />
      Projects
    </RouterLink>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending || !project" class="text-muted">Loading project…</p>
    <template v-else>
      <div class="flex flex-col gap-1">
        <h1 class="text-2xl font-extrabold tracking-tight text-highlighted">{{ project.name }}</h1>
        <p class="text-sm text-muted">
          Owned by {{ project.owner_email }}. Created {{ formatDate(project.created) }}.
        </p>
      </div>

      <section class="flex flex-col gap-3">
        <h2 class="text-lg font-bold text-highlighted">Usage and quota</h2>
        <QuotaEditor :project-id="project.id" :quota="project.quota" :usage="project.usage" />
      </section>

      <section data-section="members" class="flex flex-col gap-3">
        <h2 class="text-lg font-bold text-highlighted">Members</h2>
        <ul class="divide-y divide-default overflow-hidden rounded-lg border border-default">
          <li
            v-for="m in project.members"
            :key="m.user_id"
            class="flex items-center justify-between gap-3 px-4 py-3.5"
          >
            <div class="flex min-w-0 items-center gap-3">
              <UAvatar :alt="m.name || m.email" size="md" />
              <div class="flex min-w-0 flex-col">
                <span class="truncate font-semibold text-highlighted">{{ m.name || m.email }}</span>
                <span class="truncate text-sm text-muted">{{ m.email }}</span>
              </div>
            </div>
            <span class="shrink-0 text-sm text-muted">{{ roleLabel(m.role) }}</span>
          </li>
        </ul>
      </section>

      <section class="flex flex-col gap-3">
        <h2 class="text-lg font-bold text-highlighted">Activity</h2>
        <div>
          <UButton
            :to="`/admin/activity?project=${project.id}`"
            color="neutral"
            variant="outline"
            icon="i-lucide-history"
          >
            View activity
          </UButton>
        </div>
      </section>

      <section class="flex flex-col gap-3">
        <h2 class="text-lg font-bold text-highlighted">Delete project</h2>
        <p class="text-sm text-muted">
          A project with servers can't be deleted. Its members need to delete them first.
        </p>
        <div>
          <UButton
            color="error"
            variant="outline"
            icon="i-lucide-trash-2"
            @click="confirmOpen = true"
          >
            Delete project
          </UButton>
        </div>
      </section>

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
