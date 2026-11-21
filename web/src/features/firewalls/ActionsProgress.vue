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
import type { Action } from '@/api/types'
import PolledAction from './PolledAction.vue'

defineProps<{ actions: Action[] }>()
</script>

<template>
  <div v-if="actions.length" class="flex flex-col gap-3">
    <PolledAction v-for="a in actions" :key="a.id" :action="a" />
  </div>
</template>
