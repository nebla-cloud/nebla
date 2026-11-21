<script setup lang="ts">
import { ref, watch } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import { errorMessage } from '@/api/errors'
import type { TokenPermission } from '@/api/types'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import CopyField from '@/components/CopyField.vue'
import { permissionLabel } from '@/lib/roles'
import { useCreateToken } from './queries'
import { tokenSlug } from './slug'

const props = defineProps<{ projectId: number; projectName: string }>()
const open = defineModel<boolean>('open', { default: false })

const create = useCreateToken(() => props.projectId)
const name = ref('')
const permission = ref<TokenPermission>('read')
const nameError = ref<string | null>(null)
const formError = ref<string | null>(null)
const token = ref<string | null>(null)
const copied = ref(false)
const confirmOpen = ref(false)
const origin = window.location.origin

const permissions: TokenPermission[] = ['read', 'read_write']
const items = permissions.map((p) => ({ value: p, label: permissionLabel(p) }))

watch(open, (v) => {
  if (v) return
  name.value = ''
  permission.value = 'read'
  nameError.value = null
  formError.value = null
  token.value = null
  copied.value = false
  confirmOpen.value = false
  create.reset()
})

async function submit() {
  nameError.value = null
  formError.value = null
  const value = name.value.trim()
  if (!value) {
    nameError.value = 'Give the token a name.'
    return
  }
  if (value.length > 100) {
    nameError.value = 'Use 100 characters or fewer.'
    return
  }
  try {
    const created = await create.mutateAsync({ name: value, permission: permission.value })
    if (open.value) token.value = created.token
  } catch (e) {
    if (open.value) formError.value = errorMessage(e)
  }
}

function requestClose(v: boolean) {
  if (v) {
    open.value = true
    return
  }
  if (token.value && !copied.value) confirmOpen.value = true
  else open.value = false
}

function closeAnyway() {
  confirmOpen.value = false
  open.value = false
}
</script>

<template>
  <UModal
    :open="open"
    :title="token ? 'Token created' : 'Create API token'"
    :description="token ? undefined : 'Tokens let the CLI and scripts use this project.'"
    @update:open="requestClose"
  >
    <template #body>
      <div v-if="token" class="flex flex-col gap-4">
        <CopyField :value="token" label="Your token" mono @copied="copied = true" />
        <p class="text-sm font-semibold text-highlighted">You won't see it again.</p>
        <div class="flex flex-col gap-1.5">
          <p class="text-sm text-muted">
            To use it with the CLI, run this and paste the token when asked.
          </p>
          <code class="break-all rounded-md bg-(--nb-soft) px-2.5 py-1.5 nb-mono text-xs">
            nebla context create {{ tokenSlug(projectName) }} --endpoint {{ origin }}
          </code>
        </div>
      </div>
      <form
        v-else
        id="create-token"
        class="flex flex-col gap-4"
        novalidate
        @submit.prevent="submit"
      >
        <UFormField label="Name" name="name" :error="nameError ?? formError ?? undefined">
          <UInput v-model="name" name="name" autocomplete="off" class="w-full" />
        </UFormField>
        <UFormField label="Permission" name="permission">
          <URadioGroup v-model="permission" :items="items" />
        </UFormField>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <template v-if="token">
          <UButton @click="requestClose(false)">Done</UButton>
        </template>
        <template v-else>
          <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
          <UButton type="submit" form="create-token" :loading="create.isPending.value">
            Create token
          </UButton>
        </template>
      </div>
    </template>
  </UModal>
  <ConfirmDialog
    v-model:open="confirmOpen"
    title="Close without copying?"
    body="You won't be able to see this token again."
    confirm-label="Close anyway"
    danger
    @confirm="closeAnyway"
  />
</template>
