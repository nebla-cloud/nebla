<script setup lang="ts">
import { ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { errorMessage } from '@/api/errors'
import type { Action, Volume } from '@/api/types'
import { resizeVolume } from './queries'
import { MAX_SIZE_GB } from './serverPicker'

const props = defineProps<{ volume: Volume }>()
const emit = defineEmits<{ started: [action: Action] }>()
const open = defineModel<boolean>('open', { default: false })

const size = ref(String(props.volume.size))
const sizeError = ref<string | null>(null)
const error = ref<string | null>(null)
const pending = ref(false)

watch(open, (v) => {
  if (v) size.value = String(props.volume.size)
  sizeError.value = error.value = null
})

async function submit() {
  sizeError.value = error.value = null
  const gb = Number(size.value)
  if (!Number.isInteger(gb) || gb <= props.volume.size || gb > MAX_SIZE_GB) {
    sizeError.value = `Pick a whole number of GB bigger than ${props.volume.size} GB and at most ${MAX_SIZE_GB} GB.`
    return
  }
  pending.value = true
  try {
    emit('started', await resizeVolume(props.volume.id, gb))
    open.value = false
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="`Resize ${volume.name}`"
    description="Volumes only grow. A detached volume gets its new size when you next attach it."
  >
    <template #body>
      <form id="resize-volume" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UAlert v-if="error" color="error" variant="subtle" :description="error" />
        <UFormField
          label="New size"
          name="size"
          :help="`Now ${volume.size} GB.`"
          :error="sizeError ?? undefined"
        >
          <UInput
            v-model="size"
            name="size"
            type="number"
            inputmode="numeric"
            :min="volume.size + 1"
            :max="MAX_SIZE_GB"
            class="w-full sm:w-40"
          >
            <template #trailing><span class="text-sm text-muted">GB</span></template>
          </UInput>
        </UFormField>
        <p class="text-sm text-muted">
          Then grow the filesystem on the server, for example
          <code class="nb-mono">sudo resize2fs {{ volume.linux_device }}</code> for ext4.
        </p>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="resize-volume" :loading="pending">Resize</UButton>
      </div>
    </template>
  </UModal>
</template>
