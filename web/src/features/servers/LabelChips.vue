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
import LabelChip from '../../components/LabelChip.vue'

const props = withDefaults(defineProps<{ labels: Record<string, string>; max?: number }>(), {
  max: 3,
})
const entries = computed(() => Object.entries(props.labels).sort(([a], [b]) => a.localeCompare(b)))
const shown = computed(() => entries.value.slice(0, props.max))
const extra = computed(() => entries.value.length - shown.value.length)
</script>

<template>
  <ul v-if="entries.length" class="flex flex-wrap items-center gap-1.5" aria-label="Labels">
    <li v-for="[k, v] in shown" :key="k"><LabelChip :k="k" :v="v" /></li>
    <li v-if="extra > 0" class="text-xs text-muted">+{{ extra }}</li>
  </ul>
</template>
