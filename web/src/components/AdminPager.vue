<!--
Copyright (C) 2026 The Nebla contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
-->

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
    class="flex items-center justify-between gap-3 border-t border-(--ui-border) pt-3 text-sm"
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
