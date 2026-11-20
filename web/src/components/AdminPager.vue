<script setup lang="ts">
import { computed } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'

const props = defineProps<{ page: number; perPage: number; total: number }>()
const emit = defineEmits<{ 'update:page': [page: number] }>()

const pages = computed(() => Math.max(1, Math.ceil(props.total / props.perPage)))
const first = computed(() => (props.page - 1) * props.perPage + 1)
const last = computed(() => Math.min(props.page * props.perPage, props.total))
</script>

<template>
  <nav
    v-if="total > perPage"
    aria-label="Pagination"
    class="flex items-center justify-between gap-3 text-sm"
  >
    <p class="text-muted tabular-nums">Showing {{ first }}–{{ last }} of {{ total }}</p>
    <div class="flex gap-2">
      <UButton
        color="neutral"
        variant="outline"
        size="sm"
        icon="i-lucide-chevron-left"
        :disabled="page <= 1"
        @click="emit('update:page', page - 1)"
      >
        Previous
      </UButton>
      <UButton
        color="neutral"
        variant="outline"
        size="sm"
        trailing-icon="i-lucide-chevron-right"
        :disabled="page >= pages"
        @click="emit('update:page', page + 1)"
      >
        Next
      </UButton>
    </div>
  </nav>
</template>
