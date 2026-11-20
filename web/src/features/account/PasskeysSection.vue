<script setup lang="ts">
import { computed, ref } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { api } from '@/api/client'
import { ApiError, unwrap } from '@/api/errors'
import type { Passkey } from '@/api/types'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { passkeyOriginElsewhere, passkeysUsable } from '@/features/auth/passkey'
import { formatDate } from '@/lib/format'
import { useSession } from '@/lib/session'
import AddPasskeyModal from './AddPasskeyModal.vue'
import { inlineError } from './authError'

const MAX_PASSKEYS = 10

const toast = useToast()
const queryClient = useQueryClient()
const { manifest, me } = useSession()

const origin = computed(() => manifest.value?.passkey_origin ?? null)
const usable = computed(() => passkeysUsable(origin.value))
const elsewhere = computed(() => passkeyOriginElsewhere(origin.value))

const {
  data,
  error: listError,
  refetch: retry,
  isFetching,
} = useQuery({
  queryKey: ['me', 'passkeys'],
  queryFn: async () => unwrap(await api.GET('/me/passkeys')).passkeys,
  enabled: usable,
})
const passkeys = computed(() => data.value ?? [])
const loadFailed = computed(
  () =>
    !data.value &&
    !!listError.value &&
    !(listError.value instanceof ApiError && listError.value.status === 401),
)
const atLimit = computed(() => passkeys.value.length >= MAX_PASSKEYS)

const adding = ref(false)
const renaming = ref<Passkey | null>(null)
const renameOpen = ref(false)
const renameValue = ref('')
const renameError = ref<string | null>(null)
const removing = ref<Passkey | null>(null)
const removeOpen = ref(false)
const removeError = ref<string | null>(null)

function refetch() {
  return queryClient.invalidateQueries({ queryKey: ['me', 'passkeys'] })
}

async function added() {
  await refetch()
  toast.add({ title: 'Passkey added', color: 'success' })
}

function startRename(p: Passkey) {
  renaming.value = p
  renameValue.value = p.name
  renameError.value = null
  renameOpen.value = true
}

const rename = useMutation({
  mutationFn: async () => {
    const p = renaming.value
    if (!p) return
    unwrap(
      await api.PUT('/me/passkeys/{id}', {
        params: { path: { id: p.id } },
        body: { name: renameValue.value.trim() },
      }),
    )
    await refetch()
    renameOpen.value = false
  },
  onError: (e) => {
    renameError.value = inlineError(e)
  },
})

function submitRename() {
  if (rename.isPending.value || !renameValue.value.trim()) return
  renameError.value = null
  rename.mutate()
}

function startRemove(p: Passkey) {
  removing.value = p
  removeError.value = null
  removeOpen.value = true
}

const remove = useMutation({
  mutationFn: async () => {
    const p = removing.value
    if (!p) return
    unwrap(await api.DELETE('/me/passkeys/{id}', { params: { path: { id: p.id } } }))
    await refetch()
    removeOpen.value = false
    toast.add({ title: 'Passkey removed', color: 'success' })
  },
  onError: (e) => {
    const msg = inlineError(e)
    removeOpen.value = false
    if (msg) toast.add({ title: msg, color: 'error' })
  },
})

function menu(p: Passkey) {
  return [
    [
      { label: 'Rename', icon: 'i-lucide-pencil', onSelect: () => startRename(p) },
      {
        label: 'Remove',
        icon: 'i-lucide-trash-2',
        color: 'error' as const,
        onSelect: () => startRemove(p),
      },
    ],
  ]
}

function dates(p: Passkey): string {
  const used = p.last_used_at ? `Last used ${formatDate(p.last_used_at)}.` : 'Never used.'
  return `Added ${formatDate(p.created_at)}. ${used}`
}
</script>

<template>
  <section class="flex flex-col gap-3">
    <h2 class="text-base font-bold text-highlighted">Passkeys</h2>

    <p v-if="!origin" class="text-sm text-muted">
      Passkeys need this panel on https with a domain name.
    </p>
    <p v-else-if="elsewhere" class="text-sm text-muted">
      Passkeys work at
      <a
        :href="origin"
        class="text-primary underline focus-visible:outline-2 focus-visible:outline-primary"
        >{{ origin }}</a
      >.
    </p>
    <p v-else-if="!usable" class="text-sm text-muted">This browser can't use passkeys.</p>

    <template v-else>
      <p class="text-sm text-muted">
        Log in with your fingerprint, face or device PIN instead of your password. You won't need a
        two-factor code.
      </p>
      <div v-if="loadFailed" class="flex flex-wrap items-center gap-3">
        <p class="text-sm text-toned">Couldn't load your passkeys. Try again.</p>
        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          icon="i-lucide-rotate-cw"
          :loading="isFetching"
          @click="retry()"
          >Retry</UButton
        >
      </div>
      <p v-if="data && passkeys.length === 0" class="text-sm text-toned">
        You haven't added a passkey.
      </p>
      <ul
        v-if="passkeys.length"
        class="flex flex-col divide-y divide-default border-y border-default"
      >
        <li v-for="p in passkeys" :key="p.id" class="flex items-center justify-between gap-3 py-3">
          <div class="min-w-0">
            <p class="truncate text-sm font-bold text-highlighted">{{ p.name }}</p>
            <p class="text-xs text-muted">{{ dates(p) }}</p>
          </div>
          <UDropdownMenu :items="menu(p)" :content="{ align: 'end' }">
            <UButton
              color="neutral"
              variant="ghost"
              class="shrink-0"
              icon="i-lucide-ellipsis"
              :aria-label="`Actions for ${p.name}`"
            />
          </UDropdownMenu>
        </li>
      </ul>
      <p v-if="atLimit" class="text-sm text-toned">
        You've added the most passkeys allowed ({{ MAX_PASSKEYS }}).
      </p>
      <div v-else-if="data">
        <UButton @click="adding = true">Add a passkey</UButton>
      </div>
    </template>

    <AddPasskeyModal v-model:open="adding" :totp="me?.totp_enabled === true" @added="added" />

    <UModal
      v-model:open="renameOpen"
      title="Rename passkey"
      description="Pick a name you'll recognize."
    >
      <template #body>
        <div class="flex flex-col gap-4">
          <UAlert v-if="renameError" color="error" variant="subtle" :description="renameError" />
          <form id="rename-passkey" novalidate @submit.prevent="submitRename">
            <UFormField label="Name" name="name">
              <UInput v-model="renameValue" name="name" maxlength="100" class="w-full" />
            </UFormField>
          </form>
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="renameOpen = false">Cancel</UButton>
          <UButton type="submit" form="rename-passkey" :loading="rename.isPending.value"
            >Save</UButton
          >
        </div>
      </template>
    </UModal>

    <ConfirmDialog
      v-model:open="removeOpen"
      title="Remove passkey"
      body="Remove this passkey? You won't be able to log in with it, and you'll be signed out on your other devices."
      confirm-label="Remove passkey"
      danger
      :loading="remove.isPending.value"
      @confirm="remove.mutate()"
    />
  </section>
</template>
