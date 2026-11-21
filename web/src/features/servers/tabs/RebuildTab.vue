<script setup lang="ts">
import { computed, inject, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { api } from '@/api/client'
import { errorMessage, unwrap } from '@/api/errors'
import type { Action, Server } from '@/api/types'
import ChoiceCard from '@/components/ChoiceCard.vue'
import ConfirmByName from '@/components/ConfirmByName.vue'
import { availableImages } from '@/features/servers-create/createServer'
import { useImages } from '@/features/servers-create/queries'
import OsBadge from '../OsBadge.vue'
import { serverBusyKey } from '../shell'

const props = defineProps<{ server: Server }>()
const emit = defineEmits<{ started: [action: Action] }>()

const busy = inject(
  serverBusyKey,
  computed(() => false),
)
const images = useImages()
const choices = computed(() =>
  availableImages(images.data.value ?? [], props.server.location.id, props.server.location.name),
)
const picked = ref<number | null>(null)
// ChoiceCard values are strings; picked stays the image id.
const pickedValue = computed({
  get: () => (picked.value === null ? undefined : String(picked.value)),
  set: (v) => (picked.value = v === undefined ? null : Number(v)),
})
const confirmOpen = ref(false)
const pending = ref(false)
const error = ref('')

watch(
  choices,
  (list) => {
    if (picked.value === null)
      picked.value =
        list.find((c) => !c.disabledReason && c.image.id === props.server.image.id)?.image.id ??
        list.find((c) => !c.disabledReason)?.image.id ??
        null
  },
  { immediate: true },
)

async function submit() {
  if (picked.value === null) return
  pending.value = true
  error.value = ''
  try {
    const res = await api.POST('/servers/{id}/actions/rebuild', {
      params: { path: { id: props.server.id } },
      body: { image: String(picked.value) },
    })
    const { action } = unwrap(res)
    confirmOpen.value = false
    emit('started', action)
  } catch (e) {
    confirmOpen.value = false
    error.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-4">
    <UAlert
      color="warning"
      variant="subtle"
      icon="i-lucide-triangle-alert"
      title="Rebuilding erases the disk and reinstalls from the image you pick. The IP addresses stay the same."
    />
    <fieldset>
      <legend class="mb-2 text-sm font-semibold text-highlighted">Image</legend>
      <div class="grid gap-2 sm:grid-cols-2">
        <ChoiceCard
          v-for="c in choices"
          :key="c.image.id"
          v-model="pickedValue"
          type="radio"
          name="image"
          :value="String(c.image.id)"
          :disabled="!!c.disabledReason"
        >
          <span class="flex items-center gap-2">
            <OsBadge :name="c.image.os_flavor" />
            {{ c.image.name }}
          </span>
          <template v-if="c.disabledReason" #meta>{{ c.disabledReason }}</template>
        </ChoiceCard>
      </div>
    </fieldset>
    <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
    <div>
      <UButton color="error" :disabled="busy || picked === null" @click="confirmOpen = true">
        Rebuild
      </UButton>
    </div>
    <ConfirmByName
      v-model:open="confirmOpen"
      :name="server.name"
      title="Rebuild this server"
      confirm-label="Rebuild"
      :loading="pending"
      @confirm="submit"
    />
  </div>
</template>
