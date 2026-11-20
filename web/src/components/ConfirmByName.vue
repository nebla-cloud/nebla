<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'

const props = defineProps<{
  name: string
  title: string
  confirmLabel: string
  loading?: boolean
  error?: string | null
}>()
const emit = defineEmits<{ confirm: [] }>()
const open = defineModel<boolean>('open', { default: false })

const typed = ref('')
const matches = computed(() => typed.value === props.name)

watch(open, (v) => {
  if (!v) typed.value = ''
})

function submit() {
  if (matches.value && !props.loading) emit('confirm')
}
</script>

<template>
  <UModal v-model:open="open" :title="title" :description="`This can't be undone.`">
    <slot />
    <template #body>
      <form id="confirm-by-name" class="flex flex-col gap-3" @submit.prevent="submit">
        <UAlert v-if="error" color="error" variant="subtle" :description="error" />
        <UFormField :label="`Type ${name} to confirm`" name="confirm_name">
          <UInput
            v-model="typed"
            name="confirm_name"
            autocomplete="off"
            spellcheck="false"
            class="w-full"
            autofocus
          />
        </UFormField>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <slot name="actions" />
        <UButton
          type="submit"
          form="confirm-by-name"
          color="error"
          :disabled="!matches"
          :loading="loading"
        >
          {{ confirmLabel }}
        </UButton>
      </div>
    </template>
  </UModal>
</template>
