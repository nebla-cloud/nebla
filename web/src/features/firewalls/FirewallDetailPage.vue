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
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { ApiError, errorMessage } from '@/api/errors'
import ConfirmByName from '@/components/ConfirmByName.vue'
import LabelChip from '@/components/LabelChip.vue'
import PageHeader from '@/components/PageHeader.vue'
import { useCrumbs } from '@/lib/crumbs'
import { parseProjectId, useCurrentProject } from '@/lib/project'
import { canWrite } from '@/lib/roles'
import AppliedToPanel from './AppliedToPanel.vue'
import RuleEditor from './RuleEditor.vue'
import { useDeleteFirewall, useFirewall, useRenameFirewall } from './queries'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const { projectId, project } = useCurrentProject()
const writable = computed(() => canWrite(project.value?.role))
const id = computed(() => parseProjectId(route.params.firewallId) ?? 0)
const { data: firewall, error } = useFirewall(id)
useCrumbs(() => [
  { label: 'Firewalls', to: { name: 'firewalls', params: { projectId: projectId.value } } },
  { label: firewall.value?.name ?? 'Firewall' },
])
const notFound = computed(() => error.value instanceof ApiError && error.value.status === 404)

const rename = useRenameFirewall()
const renameOpen = ref(false)
const newName = ref('')
const renameError = ref<string | null>(null)

function startRename() {
  newName.value = firewall.value?.name ?? ''
  renameError.value = null
  renameOpen.value = true
}

async function submitRename() {
  const name = newName.value.trim()
  if (!name) return void (renameError.value = 'Give the firewall a name.')
  try {
    await rename.mutateAsync({ id: id.value, name })
    renameOpen.value = false
  } catch (e) {
    renameError.value = errorMessage(e)
  }
}

const remove = useDeleteFirewall()
const deleteOpen = ref(false)

async function confirmDelete() {
  const name = firewall.value?.name ?? ''
  try {
    await remove.mutateAsync(id.value)
    deleteOpen.value = false
    toast.add({ title: `Deleted ${name}`, color: 'success' })
    await router.push({ name: 'firewalls', params: { projectId: projectId.value } })
  } catch (e) {
    toast.add({ title: errorMessage(e), color: 'error' })
  }
}
</script>

<template>
  <div class="flex max-w-4xl flex-col gap-5">
    <UAlert v-if="notFound" color="error" variant="subtle" description="This firewall doesn't exist." />
    <UAlert
      v-else-if="error && !firewall"
      color="error"
      variant="subtle"
      :description="errorMessage(error)"
    />
    <p v-else-if="!firewall" class="text-muted">Loading firewall…</p>

    <template v-else>
      <PageHeader :title="firewall.name" class="max-sm:flex-col max-sm:items-stretch">
        <template v-if="Object.keys(firewall.labels).length" #meta>
          <span class="flex flex-wrap gap-1">
            <LabelChip v-for="(v, k) in firewall.labels" :key="k" :k="String(k)" :v="v" />
          </span>
        </template>
        <template v-if="writable" #actions>
          <UButton color="neutral" variant="outline" icon="i-lucide-pencil" @click="startRename">
            Rename
          </UButton>
          <UButton color="error" variant="outline" icon="i-lucide-trash-2" @click="deleteOpen = true">
            Delete
          </UButton>
        </template>
      </PageHeader>

      <RuleEditor :firewall="firewall" :writable="writable" />
      <AppliedToPanel :firewall="firewall" :writable="writable" />
    </template>

    <UModal v-model:open="renameOpen" title="Rename firewall">
      <template #body>
        <form id="rename-firewall" class="flex flex-col gap-3" novalidate @submit.prevent="submitRename">
          <UFormField label="Name" name="rename" :error="renameError ?? undefined">
            <UInput v-model="newName" name="rename" autocomplete="off" class="w-full" />
          </UFormField>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="renameOpen = false">Cancel</UButton>
          <UButton type="submit" form="rename-firewall" :loading="rename.isPending.value">
            Save
          </UButton>
        </div>
      </template>
    </UModal>

    <ConfirmByName
      v-model:open="deleteOpen"
      :name="firewall?.name ?? ''"
      :title="`Delete ${firewall?.name ?? 'firewall'}?`"
      confirm-label="Delete firewall"
      :loading="remove.isPending.value"
      @confirm="confirmDelete"
    />
  </div>
</template>
