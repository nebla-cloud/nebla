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
import { computed, useId } from 'vue'
import lockupSvg from '@/assets/lockup.svg?raw'
import BrandMark from '@/components/BrandMark.vue'
import { useSession } from '@/lib/session'

const props = withDefaults(defineProps<{ height?: number }>(), { height: 22 })
const { manifest } = useSession()
const uid = useId()
const product = computed(() => manifest.value?.branding.product_name?.trim() || 'Nebla')
const isNebla = computed(() => product.value.toLowerCase() === 'nebla')
// The raw SVG has a fixed size and a shared gradient id; both are rewritten per instance.
const svg = computed(() =>
  lockupSvg
    .replaceAll('nebla-arch', `nebla-lockup-${uid}`)
    .replace(
      'width="365" height="107"',
      `width="${(props.height * 365) / 107}" height="${props.height}"`,
    )
    .replace('<svg ', '<svg aria-hidden="true" focusable="false" '),
)
</script>

<template>
  <span
    v-if="isNebla"
    role="img"
    aria-label="Nebla"
    class="inline-flex shrink-0 text-highlighted"
    v-html="svg"
  />
  <span v-else class="inline-flex min-w-0 items-center gap-2.5 text-highlighted">
    <BrandMark :size="height" />
    <span
      class="min-w-0 truncate font-bold tracking-[-0.035em]"
      :style="{ fontSize: `${height * 0.8}px` }"
      >{{ product }}</span
    >
  </span>
</template>
