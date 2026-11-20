<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, useTemplateRef } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import {
  type PublicKeyCredentialRequestOptionsJSON,
  WebAuthnAbortService,
  browserSupportsWebAuthnAutofill,
  startAuthentication,
} from '@simplewebauthn/browser'
import { api } from '@/api/client'
import { ApiError, errorMessage, unwrap } from '@/api/errors'
import AuthLayout from '@/layouts/AuthLayout.vue'
import { isCancel, passkeysUsable } from '@/features/auth/passkey'
import { safeNext } from '@/router/guards'
import { setMe, useSession } from '@/lib/session'

const route = useRoute()
const router = useRouter()
const { manifest } = useSession()

const form = reactive({ email: '', password: '', totp: '' })
const needsTotp = ref(false)
const busy = ref(false)
const error = ref<string | null>(null)
const totpInput = useTemplateRef<{ inputRef?: HTMLInputElement }>('totpInput')

const passkeys = computed(() => passkeysUsable(manifest.value?.passkey_origin))
let active = true

async function finishLogin(user: Parameters<typeof setMe>[0]) {
  setMe(user)
  await router.replace(safeNext(route.query.next) ?? '/')
}

async function passkeyLogin(conditional: boolean) {
  if (!conditional) {
    if (busy.value) return
    busy.value = true
    error.value = null
  }
  try {
    const begin = unwrap(await api.POST('/auth/passkey/begin'))
    if (!active) return
    const optionsJSON = begin.options as unknown as PublicKeyCredentialRequestOptionsJSON
    const credential = await startAuthentication(
      conditional ? { optionsJSON, useBrowserAutofill: true } : { optionsJSON },
    )
    if (!active) return
    const user = unwrap(
      await api.POST('/auth/passkey/finish', {
        body: {
          challenge_id: begin.challenge_id,
          credential: credential as unknown as Record<string, unknown>,
        },
      }),
    )
    if (!active) return
    await finishLogin(user)
  } catch (e) {
    if (isCancel(e) || !active) return
    if (e instanceof ApiError && e.code === 'passkey_failed') {
      error.value = "That passkey didn't work. Try again, or log in with your password."
    } else if (!conditional) {
      error.value = errorMessage(e)
    }
  } finally {
    if (!conditional) busy.value = false
  }
}

onMounted(async () => {
  if (passkeys.value && (await browserSupportsWebAuthnAutofill()) && active) {
    void passkeyLogin(true)
  }
})

onBeforeUnmount(() => {
  active = false
  WebAuthnAbortService.cancelCeremony()
})

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = null
  try {
    const body = needsTotp.value
      ? { email: form.email, password: form.password, totp_code: form.totp.trim() }
      : { email: form.email, password: form.password }
    const user = unwrap(await api.POST('/auth/login', { body }))
    setMe(user)
    await router.replace(safeNext(route.query.next) ?? '/')
  } catch (e) {
    if (e instanceof ApiError && e.code === 'totp_required' && !needsTotp.value) {
      needsTotp.value = true
      await nextTick()
      totpInput.value?.inputRef?.focus()
    } else {
      error.value = errorMessage(e)
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AuthLayout
    title="Log in"
    :description="needsTotp ? 'Enter the 6-digit code from your authenticator app.' : undefined"
  >
    <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
      <UAlert v-if="error" color="error" variant="subtle" :description="error" />
      <UFormField label="Email" name="email">
        <UInput
          v-model="form.email"
          name="email"
          type="email"
          :autocomplete="passkeys ? 'username webauthn' : 'username'"
          required
          class="w-full"
          :disabled="needsTotp"
        />
      </UFormField>
      <UFormField label="Password" name="password">
        <UInput
          v-model="form.password"
          name="password"
          type="password"
          autocomplete="current-password"
          required
          class="w-full"
          :disabled="needsTotp"
        />
      </UFormField>
      <UFormField v-if="needsTotp" label="Authentication code" name="totp_code">
        <UInput
          ref="totpInput"
          v-model="form.totp"
          name="totp_code"
          inputmode="numeric"
          autocomplete="one-time-code"
          maxlength="8"
          required
          class="w-full"
        />
      </UFormField>
      <UButton type="submit" block size="lg" :loading="busy">Log in</UButton>
      <template v-if="passkeys && !needsTotp">
        <div data-passkey-divider class="flex items-center gap-3 text-sm text-muted">
          <span class="h-px flex-1 bg-[var(--ui-border)]" />
          or
          <span class="h-px flex-1 bg-[var(--ui-border)]" />
        </div>
        <UButton
          type="button"
          block
          size="lg"
          color="neutral"
          variant="outline"
          icon="i-lucide-key-round"
          :disabled="busy"
          @click="passkeyLogin(false)"
          >Log in with a passkey</UButton
        >
      </template>
    </form>
    <template v-if="manifest?.signup_enabled" #after>
      New here?
      <RouterLink :to="{ name: 'signup' }" class="font-medium text-primary"
        >Create an account</RouterLink
      >
    </template>
  </AuthLayout>
</template>
