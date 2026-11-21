<script setup lang="ts">
import { computed } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import TerminalBlock from '@/components/TerminalBlock.vue'
import type { Volume } from '@/api/types'
import { useCopy } from '@/lib/useCopy'

const props = defineProps<{ volume: Volume; hint?: boolean }>()
const { copied, copy } = useCopy()
// Built here so the template can't fold the lines together.
const hintText = computed(() => {
  const dev = props.volume.linux_device
  const dir = `/mnt/${props.volume.name}`
  return [`sudo mkfs.ext4 ${dev}`, `sudo mkdir -p ${dir}`, `sudo mount ${dev} ${dir}`].join('\n')
})
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="flex flex-col gap-1.5">
      <span class="text-sm font-medium text-highlighted">Device path</span>
      <div class="flex items-start gap-2">
        <TerminalBlock class="min-w-0 flex-1" data-testid="linux-device">{{
          volume.linux_device
        }}</TerminalBlock>
        <UButton
          color="neutral"
          variant="outline"
          :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
          :aria-label="`Copy the device path of ${volume.name}`"
          @click="copy(volume.linux_device)"
        >
          {{ copied ? 'Copied' : 'Copy' }}
        </UButton>
      </div>
      <p class="text-xs text-muted">This path stays the same after a reboot or rebuild.</p>
    </div>
    <div v-if="hint" class="flex flex-col gap-1.5">
      <span class="text-sm font-medium text-highlighted">Format and mount it</span>
      <TerminalBlock
        as="pre"
        caption="First use only: mkfs erases the volume"
        data-testid="mount-hint"
        >{{ hintText }}</TerminalBlock
      >
      <p class="text-xs text-muted">
        To mount it at every boot, mount by UUID in /etc/fstab (find it with
        <code class="nb-mono">sudo blkid</code>) and keep the <code class="nb-mono">nofail</code>
        option.
      </p>
    </div>
  </div>
</template>
