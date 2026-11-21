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
import { useId } from 'vue'

withDefaults(defineProps<{ size?: number }>(), { size: 24 })
const gradient = `nebla-arch-${useId()}`
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 64 64"
    aria-hidden="true"
    focusable="false"
    class="shrink-0"
  >
    <defs>
      <linearGradient :id="gradient" x1="0" y1="64" x2="64" y2="0" gradientUnits="userSpaceOnUse">
        <stop offset="0" stop-color="#2FC795" />
        <stop offset="0.52" stop-color="#4C9BEA" />
        <stop offset="1" stop-color="#EE6F95" />
      </linearGradient>
    </defs>
    <path
      d="M16 52V31a16 16 0 0 1 32 0v21"
      fill="none"
      :stroke="`url(#${gradient})`"
      stroke-width="11"
      stroke-linecap="round"
    />
  </svg>
</template>
