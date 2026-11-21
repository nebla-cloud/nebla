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
import { ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { ApiToken } from '@/api/types'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import { formatDate } from '@/lib/format'
import { table, tableWrap, tbody, td, th, thead, tr } from '@/lib/ui'
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
  <div class="flex flex-col gap-4">
    <PageHeader title="API tokens">
      <template v-if="tokens?.length" #actions>
        <UButton icon="i-lucide-plus" @click="creating = true">Create token</UButton>
      </template>
    </PageHeader>

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
    <div v-else :class="tableWrap">
      <table :class="table">
        <thead :class="thead">
          <tr>
            <th scope="col" :class="th">Name</th>
            <th scope="col" :class="th">Permission</th>
            <th scope="col" :class="th">Created</th>
            <th scope="col" :class="th">Last used</th>
            <th scope="col" :class="th"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody :class="tbody">
          <tr v-for="t in tokens" :key="t.id" :class="tr">
            <td :class="[td, 'font-semibold text-highlighted']">
              <span class="break-all">{{ t.name }}</span>
            </td>
            <td :class="td">
              <span class="mr-1 text-xs text-muted sm:hidden">Permission</span>
              {{ permissionLabel(t.permission) }}
            </td>
            <td :class="[td, 'text-muted']">
              <span class="mr-1 text-xs sm:hidden">Created</span>
              {{ formatDate(t.created) }}
            </td>
            <td :class="[td, 'text-muted']">
              <span class="mr-1 text-xs sm:hidden">Last used</span>
              {{ t.last_used ? formatDate(t.last_used) : 'Never' }}
            </td>
            <td :class="[td, 'ml-auto sm:text-right']">
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
