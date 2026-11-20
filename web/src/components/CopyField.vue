<script setup lang="ts">
import { ref } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import { useCopy } from '@/lib/useCopy'

const props = defineProps<{ value: string; label?: string; mono?: boolean }>()
const emit = defineEmits<{ copied: [] }>()

const { copied, copy } = useCopy()
const input = ref<HTMLInputElement | null>(null)
const manual = ref(false)
const isMac = /Mac|iPhone|iPad/.test(navigator.platform)

async function onCopy() {
  const ok = await copy(props.value, input.value)
  manual.value = !ok
  if (ok) emit('copied')
}
</script>

<template>
  <UFormField :label="label">
    <div class="flex items-center gap-2">
      <input
        ref="input"
        :value="value"
        readonly
        :aria-label="label ?? 'Value to copy'"
        :class="[
          'min-w-0 flex-1 rounded-md bg-elevated px-2.5 py-1.5 text-sm ring ring-inset ring-accented focus-visible:outline-2 focus-visible:outline-primary',
          mono && 'font-mono',
        ]"
        @focus="input?.select()"
      />
      <UButton
        color="neutral"
        variant="outline"
        :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
        @click="onCopy"
      >
        {{ copied ? 'Copied' : 'Copy' }}
      </UButton>
    </div>
    <p v-if="manual" class="mt-1 text-xs text-muted" role="status">
      Press {{ isMac ? '⌘C' : 'Ctrl+C' }} to copy
    </p>
  </UFormField>
</template>
