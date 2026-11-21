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
import { computed, inject, onBeforeUnmount, onMounted, ref } from 'vue'
import UIcon from '@nuxt/ui/components/Icon.vue'
import { stepListKey, type StepState } from './stepList'

const props = defineProps<{ n?: number; state?: StepState; done?: boolean; title: string }>()

const el = ref<HTMLElement | null>(null)
const list = inject(stepListKey, null)
const entry = { el, done: () => (props.state ? props.state === 'done' : props.done) }
const slot = list?.register(entry)
onMounted(() => list?.sort())
onBeforeUnmount(() => list?.unregister(entry))

const num = computed(() => props.n ?? slot?.n.value)
const st = computed<StepState>(
  () => props.state ?? slot?.state.value ?? (props.done ? 'done' : 'todo'),
)
</script>

<template>
  <li
    ref="el"
    :data-step="num"
    :data-state="st"
    :aria-current="st === 'current' ? 'step' : undefined"
    class="relative grid grid-cols-[1.75rem_1fr] gap-3.5 pb-4 not-last:before:absolute not-last:before:top-8 not-last:before:-bottom-1 not-last:before:left-[0.8125rem] not-last:before:w-[1.5px] not-last:before:bg-(--ui-border) not-last:before:content-['']"
  >
    <span
      class="flex size-7 items-center justify-center rounded-full border-[1.5px] text-[13px] font-semibold"
      :class="
        st === 'done'
          ? 'border-(--nb-focus) bg-(--nb-focus) text-(--ui-bg)'
          : st === 'current'
            ? 'border-(--nb-focus) text-highlighted'
            : 'border-(--ui-border) text-muted'
      "
    >
      <UIcon v-if="st === 'done'" name="i-lucide-check" class="size-4" aria-hidden="true" />
      <template v-else>{{ num }}</template>
    </span>
    <div class="min-w-0">
      <span v-if="st === 'done'" class="sr-only">Done</span>
      <h2 class="nb-h2 mt-1 mb-2.5 text-highlighted">{{ title }}</h2>
      <slot />
    </div>
  </li>
</template>
