<script setup lang="ts">
import { computed, ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { api } from '@/api/client'
import { ApiError, unwrap } from '@/api/errors'
import CopyField from '@/components/CopyField.vue'
import Panel from '@/components/Panel.vue'
import { setMe, useSession } from '@/lib/session'
import { inlineError } from './authError'
import QrCode from './QrCode.vue'

const { me } = useSession()
const enabled = computed(() => me.value?.totp_enabled === true)

const enrollment = ref<{ secret: string; otpauth_url: string } | null>(null)
const password = ref('')
const code = ref('')
const busy = ref(false)
const error = ref<string | null>(null)

const grouped = computed(() => enrollment.value?.secret.replace(/(.{4})(?=.)/g, '$1 ') ?? '')

function setEnabled(on: boolean) {
  if (me.value) setMe({ ...me.value, totp_enabled: on })
}

function reset() {
  enrollment.value = null
  password.value = ''
  code.value = ''
  error.value = null
}

async function run(fn: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = null
  try {
    await fn()
  } catch (e) {
    error.value = inlineError(e)
  } finally {
    busy.value = false
  }
}

function enroll() {
  if (!password.value) return
  return run(async () => {
    try {
      enrollment.value = unwrap(
        await api.POST('/auth/totp/enroll', { body: { password: password.value } }),
      )
      password.value = ''
    } catch (e) {
      if (e instanceof ApiError && e.code === 'totp_already_enabled') {
        setMe(unwrap(await api.GET('/me')))
        reset()
        return
      }
      throw e
    }
  })
}

function verify() {
  if (!code.value.trim()) return
  return run(async () => {
    unwrap(await api.POST('/auth/totp/verify', { body: { code: code.value.trim() } }))
    setEnabled(true)
    reset()
  })
}

function disable() {
  if (!code.value.trim() || !password.value) return
  return run(async () => {
    unwrap(
      await api.POST('/auth/totp/disable', {
        body: { code: code.value.trim(), password: password.value },
      }),
    )
    setEnabled(false)
    reset()
  })
}
</script>

<template>
  <Panel title="Two-factor login">
    <div class="flex flex-col gap-3">
      <UAlert v-if="error" color="error" variant="subtle" :description="error" />

      <template v-if="enabled">
        <p class="text-sm text-toned">Two-factor login is on.</p>
        <p class="text-sm text-muted">
          To turn it off, enter a code from your authenticator app and your password.
        </p>
        <form class="flex flex-col gap-4" novalidate @submit.prevent="disable">
          <UFormField label="Authentication code" name="code">
            <UInput
              v-model="code"
              name="code"
              inputmode="numeric"
              autocomplete="one-time-code"
              maxlength="8"
              class="w-full"
            />
          </UFormField>
          <UFormField label="Password" name="password">
            <UInput
              v-model="password"
              name="password"
              type="password"
              autocomplete="current-password"
              class="w-full"
            />
          </UFormField>
          <div>
            <UButton type="submit" color="neutral" variant="outline" :loading="busy">
              Turn off
            </UButton>
          </div>
        </form>
      </template>

      <template v-else-if="enrollment">
        <p class="text-sm text-muted">
          Scan this code with your authenticator app, or type the key in by hand. Then enter the
          6-digit code it shows.
        </p>
        <QrCode :value="enrollment.otpauth_url" />
        <CopyField label="Key" :value="grouped" :copy-value="enrollment.secret" mono />
        <form class="flex flex-col gap-4" novalidate @submit.prevent="verify">
          <UFormField label="Authentication code" name="code">
            <UInput
              v-model="code"
              name="code"
              inputmode="numeric"
              autocomplete="one-time-code"
              maxlength="8"
              class="w-full"
            />
          </UFormField>
          <div class="flex gap-2">
            <UButton type="submit" :loading="busy">Verify and turn on</UButton>
            <UButton color="neutral" variant="ghost" @click="reset">Cancel</UButton>
          </div>
        </form>
      </template>

      <template v-else>
        <p class="text-sm text-muted">
          Ask for a code from an authenticator app each time you log in. Enter your password to
          start.
        </p>
        <form class="flex flex-col gap-4" novalidate @submit.prevent="enroll">
          <UFormField label="Password" name="password">
            <UInput
              v-model="password"
              name="password"
              type="password"
              autocomplete="current-password"
              class="w-full"
            />
          </UFormField>
          <div>
            <UButton type="submit" :loading="busy">Turn on</UButton>
          </div>
        </form>
      </template>
    </div>
  </Panel>
</template>
