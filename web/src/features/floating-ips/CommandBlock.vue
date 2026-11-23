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
import { ref } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import TerminalBlock from '@/components/TerminalBlock.vue'
import { useCopy } from '@/lib/useCopy'

const props = defineProps<{ label: string; text: string; copyLabel: string }>()
const { copied, copy } = useCopy()
const block = ref<InstanceType<typeof TerminalBlock> | null>(null)

function onCopy() {
  void copy(props.text, (block.value?.$el as HTMLElement | undefined)?.querySelector('pre'))
}
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <div class="flex items-center justify-between gap-2">
      <span class="text-sm font-medium text-highlighted">{{ label }}</span>
      <UButton
        color="neutral"
        variant="outline"
        size="sm"
        :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
        :aria-label="copyLabel"
        @click="onCopy"
      >
        {{ copied ? 'Copied' : 'Copy' }}
      </UButton>
    </div>
    <TerminalBlock ref="block" as="pre">{{ text }}</TerminalBlock>
  </div>
</template>
