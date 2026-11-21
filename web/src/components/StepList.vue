<script setup lang="ts">
import { computed, provide, shallowRef } from 'vue'
import { stepListKey, type StepEntry, type StepState } from './stepList'

const entries = shallowRef<StepEntry[]>([])
const firstOpen = computed(() => entries.value.find((e) => !e.done()))

// Steps register in setup order; once mounted they are re-sorted by DOM position.
provide(stepListKey, {
  register(entry) {
    entries.value = [...entries.value, entry]
    return {
      n: computed(() => entries.value.indexOf(entry) + 1),
      state: computed<StepState>(() =>
        entry.done() ? 'done' : firstOpen.value === entry ? 'current' : 'todo',
      ),
    }
  },
  unregister(entry) {
    entries.value = entries.value.filter((e) => e !== entry)
  },
  sort() {
    entries.value = [...entries.value].sort((a, b) => {
      const x = a.el.value
      const y = b.el.value
      if (!x || !y) return 0
      return x.compareDocumentPosition(y) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1
    })
  },
})
</script>

<template>
  <ol class="flex flex-col">
    <slot />
  </ol>
</template>
