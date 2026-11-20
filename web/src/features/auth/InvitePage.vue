<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { api, reportUnauthorized } from '@/api/client'
import { ApiError, errorMessage, unwrap } from '@/api/errors'
import type { InvitePreview } from '@/api/types'
import AuthLayout from '@/layouts/AuthLayout.vue'
import { roleLabel } from '@/lib/roles'
import { useSession } from '@/lib/session'

const route = useRoute()
const router = useRouter()
const qc = useQueryClient()
const { me, refresh, logout } = useSession()

const token = computed(() => String(route.params.token))
const preview = useQuery({
  queryKey: computed(() => ['invite', token.value]),
  queryFn: async (): Promise<InvitePreview> =>
    unwrap(await api.GET('/invites/{token}', { params: { path: { token: token.value } } })),
  retry: false,
})

const gone = computed(
  () => preview.error.value instanceof ApiError && preview.error.value.status === 404,
)
const invite = computed(() => preview.data.value)
const sameEmail = computed(
  () =>
    !!me.value &&
    !!invite.value &&
    me.value.email.toLowerCase() === invite.value.email.toLowerCase(),
)

const form = reactive({ name: '', password: '' })
const busy = ref(false)
const error = ref<string | null>(null)

async function accept(body?: { name: string; password: string }) {
  if (busy.value) return
  error.value = null
  if (body && body.password.length < 10) {
    error.value = 'Use at least 10 characters for the password.'
    return
  }
  busy.value = true
  try {
    const project = unwrap(
      await api.POST('/invites/{token}/accept', {
        params: { path: { token: token.value } },
        body,
      }),
    )
    if (body) await refresh()
    await qc.invalidateQueries({ queryKey: ['projects'] })
    await router.replace(`/p/${project.id}/servers`)
  } catch (e) {
    if (e instanceof ApiError && e.code === 'already_member') {
      await router.replace('/')
    } else if (
      e instanceof ApiError &&
      (e.code === 'unauthorized' || e.code === 'login_required')
    ) {
      reportUnauthorized()
    } else {
      error.value = errorMessage(e)
    }
  } finally {
    busy.value = false
  }
}

function roleCopy(r: InvitePreview['role']): string {
  if (r === 'read_only') return 'with read-only access'
  return `as ${r === 'owner' || r === 'admin' ? 'an' : 'a'} ${roleLabel(r).toLowerCase()}`
}

async function logOut() {
  await logout()
  qc.clear()
}

function create() {
  return accept({ name: form.name.trim(), password: form.password })
}
</script>

<template>
  <AuthLayout v-if="preview.isPending.value" title="Invite">
    <p class="text-sm text-muted">Loading invite</p>
  </AuthLayout>

  <AuthLayout v-else-if="!invite" title="Invite">
    <UAlert
      color="error"
      variant="subtle"
      :description="
        gone
          ? 'This invite link no longer works. Ask for a new one.'
          : errorMessage(preview.error.value)
      "
    />
  </AuthLayout>

  <AuthLayout
    v-else-if="me && !sameEmail"
    title="Wrong account"
    :description="`This invite is for ${invite.email}. You're logged in as ${me.email}.`"
  >
    <UButton block color="neutral" variant="outline" @click="logOut">Log out</UButton>
  </AuthLayout>

  <AuthLayout
    v-else
    :title="`Join ${invite.project_name}`"
    :description="`${invite.inviter_name} invited you ${roleCopy(invite.role)}.`"
  >
    <div class="flex flex-col gap-4">
      <UAlert v-if="error" color="error" variant="subtle" :description="error" />
      <UButton v-if="me" block size="lg" :loading="busy" @click="accept()">Join</UButton>
      <UButton
        v-else-if="invite.account_exists"
        block
        size="lg"
        :to="{ name: 'login', query: { next: `/invite/${token}` } }"
      >
        Log in to accept
      </UButton>
      <form
        v-else-if="invite.can_create_account"
        class="flex flex-col gap-4"
        novalidate
        @submit.prevent="create"
      >
        <UFormField label="Email" name="email">
          <UInput :model-value="invite.email" name="email" disabled class="w-full" />
        </UFormField>
        <UFormField label="Name" name="name">
          <UInput v-model="form.name" name="name" autocomplete="name" required class="w-full" />
        </UFormField>
        <UFormField label="Password" name="password" hint="At least 10 characters">
          <UInput
            v-model="form.password"
            name="password"
            type="password"
            autocomplete="new-password"
            required
            class="w-full"
          />
        </UFormField>
        <UButton type="submit" block size="lg" :loading="busy">Create account and join</UButton>
      </form>
      <p v-else class="text-sm text-muted">
        Ask the person who invited you to have an admin create your account.
      </p>
    </div>
  </AuthLayout>
</template>
