<script setup lang="ts">
import { computed } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UIcon from '@nuxt/ui/components/Icon.vue'
import { errorMessage } from '@/api/errors'
import { checklistSteps, useChecklistData, type StepKey } from './checklist'

const { data, error, retry } = useChecklistData()

const copy: Record<StepKey, { title: string; to: string; action: string }> = {
  location: { title: 'Connect Proxmox', to: 'admin-locations', action: 'Connect' },
  images: { title: 'Turn on OS images', to: 'admin-catalog', action: 'Choose images' },
  types: { title: 'Add server types', to: 'admin-server-types', action: 'Add' },
  pools: { title: 'Add an IP pool', to: 'admin-ip-pools', action: 'Add' },
}

const steps = computed(() => {
  const list = data.value ? checklistSteps(data.value) : []
  const current = list.findIndex((s) => !s.done)
  return list.map((s, i) => ({
    ...s,
    ...copy[s.key],
    n: i + 1,
    state: s.done ? 'done' : i === current ? 'current' : 'todo',
  }))
})
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-1">
    <h1 class="text-xl font-bold tracking-tight text-highlighted">Get Nebla ready</h1>
    <p class="text-muted">Four steps, then anyone in a project can create servers.</p>
    <div v-if="error" class="mt-4 flex items-center gap-3 text-sm" role="alert">
      <span class="text-error">{{ errorMessage(error) }}</span>
      <UButton size="sm" color="neutral" variant="outline" @click="retry">Try again</UButton>
    </div>
    <p v-else-if="!data" class="mt-4 text-sm text-muted">Loading your setup</p>
    <ol v-else class="mt-4">
      <li
        v-for="s in steps"
        :key="s.key"
        data-step
        :data-state="s.state"
        class="grid grid-cols-[1.625rem_1fr_auto] items-center gap-3 border-t border-default py-3"
      >
        <span
          class="flex size-[1.375rem] items-center justify-center rounded-full border text-xs font-semibold"
          :class="
            s.state === 'done'
              ? 'border-success bg-success text-inverted'
              : s.state === 'current'
                ? 'border-2 border-highlighted text-highlighted'
                : 'border-accented text-muted'
          "
        >
          <UIcon
            v-if="s.state === 'done'"
            name="i-lucide-check"
            class="size-3.5"
            aria-label="Done"
          />
          <template v-else>{{ s.n }}</template>
        </span>
        <div>
          <div class="font-semibold text-highlighted">{{ s.title }}</div>
          <div class="text-sm text-muted">{{ s.detail }}</div>
        </div>
        <UButton
          :to="{ name: s.to }"
          size="sm"
          :color="s.state === 'current' ? 'primary' : 'neutral'"
          :variant="s.state === 'current' ? 'solid' : s.state === 'done' ? 'ghost' : 'outline'"
          :class="s.state === 'current' ? 'bg-inverted text-inverted' : ''"
        >
          {{ s.state === 'done' ? 'Manage' : s.action }}
        </UButton>
      </li>
    </ol>
  </div>
</template>
