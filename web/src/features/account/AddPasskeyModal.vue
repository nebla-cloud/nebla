<script setup lang="ts">
import { ref, watch } from 'vue'
import { startRegistration } from '@simplewebauthn/browser'
import type { PublicKeyCredentialCreationOptionsJSON } from '@simplewebauthn/browser'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import { isCancel } from '@/features/auth/passkey'
import { inlineError } from './authError'

defineProps<{ totp: boolean }>()
const emit = defineEmits<{ added: [] }>()
const open = defineModel<boolean>('open', { default: false })

const password = ref('')
const code = ref('')
const busy = ref(false)
const error = ref<string | null>(null)

watch(open, (o) => {
  if (o) return
  password.value = ''
  code.value = ''
  error.value = null
})

async function submit() {
  if (busy.value || !password.value) return
  busy.value = true
  error.value = null
  try {
    const totpCode = code.value.trim()
    const begin = unwrap(
      await api.POST('/me/passkeys/begin', {
        body: totpCode
          ? { password: password.value, totp_code: totpCode }
          : { password: password.value },
      }),
    )
    const credential = await startRegistration({
      optionsJSON: begin.options as unknown as PublicKeyCredentialCreationOptionsJSON,
    })
    unwrap(
      await api.POST('/me/passkeys/finish', {
        body: {
          challenge_id: begin.challenge_id,
          credential: credential as unknown as Record<string, unknown>,
        },
      }),
    )
    open.value = false
    emit('added')
  } catch (e) {
    if (!isCancel(e)) error.value = inlineError(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <UModal v-model:open="open" title="Add a passkey" description="Confirm it's you first.">
    <template #body>
      <div class="flex flex-col gap-4">
        <UAlert v-if="error" color="error" variant="subtle" :description="error" />
        <form id="add-passkey" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
          <UFormField label="Your password" name="password">
            <UInput
              v-model="password"
              name="password"
              type="password"
              autocomplete="current-password"
              class="w-full"
            />
          </UFormField>
          <UFormField v-if="totp" label="Authentication code" name="code">
            <UInput
              v-model="code"
              name="code"
              inputmode="numeric"
              autocomplete="one-time-code"
              maxlength="8"
              class="w-full"
            />
          </UFormField>
        </form>
      </div>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="add-passkey" :loading="busy">Continue</UButton>
      </div>
    </template>
  </UModal>
</template>
