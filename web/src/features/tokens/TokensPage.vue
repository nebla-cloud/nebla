<script setup lang="ts">
import { ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { ApiToken } from '@/api/types'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import EmptyState from '@/components/EmptyState.vue'
import { formatDate } from '@/lib/format'
import { useCurrentProject } from '@/lib/project'
import { permissionLabel } from '@/lib/roles'
import CreateTokenModal from './CreateTokenModal.vue'
import { useRevokeToken, useTokens } from './queries'

const toast = useToast()
const { projectId, project } = useCurrentProject()
const { data: tokens, isPending, error: loadError } = useTokens(projectId)
const revoke = useRevokeToken(projectId)

const creating = ref(false)
const revoking = ref<ApiToken | null>(null)
const revokeOpen = ref(false)

function startRevoke(t: ApiToken) {
  revoking.value = t
  revokeOpen.value = true
}

async function confirmRevoke() {
  const t = revoking.value
  if (!t) return
  try {
    await revoke.mutateAsync(t.id)
    toast.add({ title: `Revoked ${t.name}`, color: 'success' })
    revokeOpen.value = false
  } catch (e) {
    toast.add({ title: errorMessage(e), color: 'error' })
  }
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex items-center justify-between gap-3">
      <h1 class="text-2xl font-extrabold tracking-tight text-highlighted">API tokens</h1>
      <UButton v-if="tokens?.length" icon="i-lucide-plus" @click="creating = true">
        Create token
      </UButton>
    </div>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading tokens…</p>
    <EmptyState
      v-else-if="!tokens?.length"
      title="No API tokens yet"
      body="API tokens let the CLI and scripts use this project."
    >
      <UButton icon="i-lucide-plus" @click="creating = true">Create token</UButton>
    </EmptyState>
    <div v-else class="overflow-hidden rounded-lg border border-default">
      <table class="w-full text-left text-sm">
        <thead class="hidden bg-elevated text-xs text-muted sm:table-header-group">
          <tr>
            <th scope="col" class="px-4 py-2 font-semibold">Name</th>
            <th scope="col" class="px-4 py-2 font-semibold">Permission</th>
            <th scope="col" class="px-4 py-2 font-semibold">Created</th>
            <th scope="col" class="px-4 py-2 font-semibold">Last used</th>
            <th scope="col" class="px-4 py-2"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-default">
          <tr
            v-for="t in tokens"
            :key="t.id"
            class="flex flex-col gap-1 px-4 py-3 sm:table-row sm:px-0 sm:py-0"
          >
            <td class="font-semibold text-highlighted sm:px-4 sm:py-3">
              <span class="break-all">{{ t.name }}</span>
            </td>
            <td class="sm:px-4 sm:py-3">
              <span class="mr-1 text-xs text-muted sm:hidden">Permission</span>
              {{ permissionLabel(t.permission) }}
            </td>
            <td class="text-muted sm:px-4 sm:py-3">
              <span class="mr-1 text-xs sm:hidden">Created</span>
              {{ formatDate(t.created) }}
            </td>
            <td class="text-muted sm:px-4 sm:py-3">
              <span class="mr-1 text-xs sm:hidden">Last used</span>
              {{ t.last_used ? formatDate(t.last_used) : 'Never' }}
            </td>
            <td class="sm:px-4 sm:py-3 sm:text-right">
              <UButton
                color="error"
                variant="ghost"
                size="sm"
                icon="i-lucide-trash-2"
                :aria-label="`Revoke ${t.name}`"
                @click="startRevoke(t)"
              >
                Revoke
              </UButton>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <CreateTokenModal
      v-model:open="creating"
      :project-id="projectId"
      :project-name="project?.name ?? ''"
    />

    <ConfirmDialog
      v-model:open="revokeOpen"
      :title="`Revoke ${revoking?.name ?? 'token'}?`"
      body="Anything using this token stops working right away."
      confirm-label="Revoke token"
      danger
      :loading="revoke.isPending.value"
      @confirm="confirmRevoke"
    />
  </div>
</template>
