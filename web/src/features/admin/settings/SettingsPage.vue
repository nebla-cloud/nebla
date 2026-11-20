<script setup lang="ts">
import { computed } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import { useAdminSettings, useSaveSettings } from './queries'

const toast = useToast()
const { data: settings, isPending, error: loadError } = useAdminSettings()
const save = useSaveSettings()

// Follows the pending value while saving, so a failed save snaps back.
const signup = computed(() =>
  save.isPending.value ? save.variables.value!.self_signup : (settings.value?.self_signup ?? false),
)

async function toggle(on: boolean) {
  try {
    await save.mutateAsync({ self_signup: on })
    toast.add({ title: on ? 'Sign-ups are on' : 'Sign-ups are off', color: 'success' })
  } catch (e) {
    toast.add({ title: errorMessage(e), color: 'error' })
  }
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex flex-col gap-1">
      <h1 class="text-2xl font-extrabold tracking-tight text-highlighted">Settings</h1>
      <p class="text-sm text-muted">Panel-wide options that apply to everyone.</p>
    </div>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading settings…</p>
    <section v-else class="flex flex-col gap-3">
      <h2 class="text-base font-bold text-highlighted">Sign-ups</h2>
      <div class="flex flex-col gap-1.5 rounded-lg border border-default p-4">
        <USwitch
          :model-value="signup"
          :disabled="save.isPending.value"
          label="Let anyone create an account"
          description="When off, people join only through an invite from a project owner or an account you create."
          @update:model-value="toggle"
        />
      </div>
    </section>
  </div>
</template>
