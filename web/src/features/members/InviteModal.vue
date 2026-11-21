<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { errorMessage } from '@/api/errors'
import type { Role } from '@/api/types'
import CopyField from '@/components/CopyField.vue'
import { selectClass } from '@/lib/ui'
import { useCurrentProject } from '@/lib/project'
import { assignableRoles, roleLabel } from '@/lib/roles'
import { useCreateInvite } from './queries'

const open = defineModel<boolean>('open', { default: false })
const { projectId, project } = useCurrentProject()
const create = useCreateInvite(projectId)

const roleId = useId()
const roleErrorId = `${roleId}-error`
const email = ref('')
const role = ref<Role>('member')
const emailError = ref<string | null>(null)
const formError = ref<string | null>(null)
const link = ref<string | null>(null)

const roles = computed(() => assignableRoles(project.value?.role ?? 'member'))

watch(open, (v) => {
  if (v) return
  email.value = ''
  role.value = 'member'
  emailError.value = null
  formError.value = null
  link.value = null
})

async function submit() {
  emailError.value = null
  formError.value = null
  const value = email.value.trim()
  if (!value.includes('@')) {
    emailError.value = 'Enter an email address, like sam@example.com.'
    return
  }
  try {
    link.value = await create.mutateAsync({ email: value, role: role.value })
  } catch (e) {
    formError.value = errorMessage(e)
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    title="Invite someone"
    description="They get a link that adds them to this project."
  >
    <template #body>
      <div v-if="link" class="flex flex-col gap-3">
        <CopyField label="Invite link" :value="link" mono />
        <p class="text-sm text-muted">
          Send this link to them. If email is set up on this panel, Nebla emailed it too.
        </p>
        <p class="text-sm text-muted">This link is shown once. You can't see it again.</p>
      </div>
      <form
        v-else
        id="invite-member"
        class="flex flex-col gap-4"
        novalidate
        @submit.prevent="submit"
      >
        <UFormField label="Email" name="email" :error="emailError ?? undefined">
          <UInput v-model="email" name="email" type="email" autocomplete="off" class="w-full" />
        </UFormField>
        <div class="flex flex-col gap-1.5">
          <label :for="roleId" class="block text-sm font-medium text-default">Role</label>
          <select
            :id="roleId"
            v-model="role"
            name="role"
            :aria-invalid="formError ? 'true' : undefined"
            :aria-describedby="formError ? roleErrorId : undefined"
            :class="selectClass"
          >
            <option v-for="r in roles" :key="r" :value="r">{{ roleLabel(r) }}</option>
          </select>
          <p v-if="formError" :id="roleErrorId" role="alert" class="text-sm text-error">
            {{ formError }}
          </p>
        </div>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">
          {{ link ? 'Done' : 'Cancel' }}
        </UButton>
        <UButton v-if="!link" type="submit" form="invite-member" :loading="create.isPending.value">
          Create invite
        </UButton>
      </div>
    </template>
  </UModal>
</template>
