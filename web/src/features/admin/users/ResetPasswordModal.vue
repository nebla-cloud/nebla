<script setup lang="ts">
import { ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import { checkboxClass } from '@/lib/ui'
import { useResetPassword, type AdminUser } from './queries'

const props = defineProps<{ user: AdminUser | null }>()
const open = defineModel<boolean>('open', { default: false })

const toast = useToast()
const reset = useResetPassword()

const password = ref('')
const confirm = ref('')
const removePasskeys = ref(true)
const errors = ref<Record<string, string>>({})
const formError = ref<string | null>(null)

watch(open, (v) => {
  if (!v) return
  password.value = ''
  confirm.value = ''
  removePasskeys.value = true
  errors.value = {}
  formError.value = null
})

async function submit() {
  if (!props.user) return
  formError.value = null
  const e: Record<string, string> = {}
  if (password.value.length < 10) e.password = 'Use at least 10 characters.'
  else if (confirm.value !== password.value) e.confirm = "Passwords don't match."
  errors.value = e
  if (Object.keys(e).length) return
  try {
    await reset.mutateAsync({
      id: props.user.id,
      password: password.value,
      remove_passkeys: props.user.passkey_count > 0 && removePasskeys.value,
    })
    toast.add({ title: 'Password changed. They were signed out.', color: 'success' })
    open.value = false
  } catch (err) {
    formError.value = errorMessage(err)
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="`Reset password for ${user?.name || user?.email || ''}`"
    description="They'll be signed out everywhere. Send them the new password yourself."
  >
    <template #body>
      <form id="reset-password" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UAlert v-if="formError" color="error" variant="subtle" :description="formError" />
        <UFormField
          label="New password"
          name="password"
          hint="At least 10 characters"
          :error="errors.password"
        >
          <UInput
            v-model="password"
            name="password"
            type="password"
            autocomplete="new-password"
            class="w-full"
          />
        </UFormField>
        <UFormField label="Confirm password" name="confirm" :error="errors.confirm">
          <UInput
            v-model="confirm"
            name="confirm"
            type="password"
            autocomplete="new-password"
            class="w-full"
          />
        </UFormField>
        <label
          v-if="user && user.passkey_count > 0"
          class="flex items-center gap-2 text-sm text-default"
        >
          <input
            v-model="removePasskeys"
            type="checkbox"
            name="remove_passkeys"
            :class="checkboxClass"
          />
          Also remove their passkeys
        </label>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="reset-password" :loading="reset.isPending.value">
          Change password
        </UButton>
      </div>
    </template>
  </UModal>
</template>
