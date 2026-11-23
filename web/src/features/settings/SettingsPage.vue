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
import { RouterLink, useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { ApiError, errorMessage } from '@/api/errors'
import ConfirmByName from '@/components/ConfirmByName.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import { forgetProject, useCurrentProject } from '@/lib/project'
import { isManager } from '@/lib/roles'
import { useSession } from '@/lib/session'
import { useMembers } from '@/features/members/queries'
import { useDeleteProject, useLeaveProject, useRenameProject } from './queries'

const router = useRouter()
const qc = useQueryClient()
const toast = useToast()
const { me } = useSession()
const { projectId, project } = useCurrentProject()
const { data: members, isPending: membersPending } = useMembers(projectId)
const rename = useRenameProject()
const leave = useLeaveProject()
const remove = useDeleteProject()

const canRename = computed(() => isManager(project.value?.role))
const isOwner = computed(() => project.value?.role === 'owner')
const onlyOwner = computed(
  () => isOwner.value && members.value?.filter((m) => m.role === 'owner').length === 1,
)
const leaveBlocked = computed(() => onlyOwner.value || (isOwner.value && membersPending.value))
const leaveBody = computed(
  () =>
    `You'll lose access to ${project.value?.name ?? 'this project'}.` +
    (isManager(project.value?.role) ? ' API tokens you made in it are revoked.' : ''),
)

const name = ref('')
const nameError = ref<string | null>(null)
watch(
  () => project.value?.name,
  (n) => {
    if (n !== undefined) name.value = n
  },
  { immediate: true },
)

const leaveOpen = ref(false)
const deleteOpen = ref(false)
const dangerError = ref<string | null>(null)
const notEmpty = ref(false)

async function submitRename() {
  const value = name.value.trim()
  if (!value) {
    nameError.value = 'Give the project a name.'
    return
  }
  nameError.value = null
  try {
    await rename.mutateAsync(value)
    toast.add({ title: 'Project renamed', color: 'success' })
  } catch (e) {
    nameError.value = errorMessage(e)
  }
}

async function goHome() {
  forgetProject(projectId.value)
  qc.clear()
  await router.push('/')
}

async function confirmLeave() {
  const id = me.value?.id
  if (id === undefined) return
  dangerError.value = null
  notEmpty.value = false
  try {
    await leave.mutateAsync(id)
  } catch (e) {
    leaveOpen.value = false
    dangerError.value = errorMessage(e)
    return
  }
  await goHome()
}

async function confirmDelete() {
  dangerError.value = null
  notEmpty.value = false
  try {
    await remove.mutateAsync()
  } catch (e) {
    deleteOpen.value = false
    if (e instanceof ApiError && e.code === 'project_not_empty') notEmpty.value = true
    else dangerError.value = errorMessage(e)
    return
  }
  await goHome()
}
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-4">
    <PageHeader title="Settings" />

    <Panel title="Project name">
      <form
        v-if="canRename"
        id="rename-project"
        class="flex flex-col gap-3 sm:flex-row sm:items-start"
        novalidate
        @submit.prevent="submitRename"
      >
        <UFormField name="name" :error="nameError ?? undefined" class="flex-1">
          <UInput
            v-model="name"
            name="name"
            autocomplete="off"
            aria-label="Project name"
            class="w-full"
          />
        </UFormField>
        <UButton type="submit" :loading="rename.isPending.value">Save</UButton>
      </form>
      <p v-else class="text-highlighted">{{ project?.name }}</p>
    </Panel>

    <Panel title="Danger area" class="border-error!">
      <div class="flex flex-col gap-4">
        <UAlert
          v-if="notEmpty"
          color="error"
          variant="subtle"
          title="Delete this project's servers, volumes and floating IPs first."
        >
          <template #description>
            <RouterLink :to="`/p/${projectId}/servers`" class="font-semibold underline">
              Servers
            </RouterLink>
          </template>
        </UAlert>
        <UAlert v-if="dangerError" color="error" variant="subtle" :description="dangerError" />

        <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div class="flex flex-col gap-0.5">
            <span class="font-semibold text-highlighted">Leave this project</span>
            <span v-if="onlyOwner" class="text-sm text-muted">
              You're the only owner. Make someone else an owner first, or delete the project.
            </span>
            <span v-else class="text-sm text-muted"
              >You'll lose access until someone invites you again.</span
            >
          </div>
          <UButton
            color="error"
            variant="outline"
            class="self-start"
            :disabled="leaveBlocked"
            @click="leaveOpen = true"
          >
            Leave project
          </UButton>
        </div>

        <div
          v-if="isOwner"
          class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between"
        >
          <div class="flex flex-col gap-0.5">
            <span class="font-semibold text-highlighted">Delete this project</span>
            <span class="text-sm text-muted"
              >Removes the project for everyone. Servers, volumes and floating IPs must be deleted first.</span
            >
          </div>
          <UButton color="error" class="self-start" @click="deleteOpen = true"
            >Delete project</UButton
          >
        </div>
      </div>
    </Panel>

    <ConfirmDialog
      v-model:open="leaveOpen"
      title="Leave this project?"
      :body="leaveBody"
      confirm-label="Leave project"
      danger
      :loading="leave.isPending.value"
      @confirm="confirmLeave"
    />
    <ConfirmByName
      v-if="project"
      v-model:open="deleteOpen"
      :name="project.name"
      title="Delete this project?"
      confirm-label="Delete project"
      :loading="remove.isPending.value"
      @confirm="confirmDelete"
    />
  </div>
</template>
