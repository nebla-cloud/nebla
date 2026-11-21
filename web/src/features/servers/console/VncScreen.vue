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
import { onBeforeUnmount, onMounted, ref } from 'vue'
import RFB from '@novnc/novnc'
import { XK_SHIFT_L, keystrokes } from './keys'

const props = defineProps<{ url: string; password: string }>()
const emit = defineEmits<{
  connect: []
  disconnect: [clean: boolean]
  'credentials-required': []
}>()

const target = ref<HTMLElement | null>(null)
let rfb: RFB | null = null
let closed = false

// QEMU drops the shift state of plain keysyms, so shifted characters hold Shift themselves.
function typeText(text: string): boolean {
  const keys = keystrokes(text)
  if (!keys) return false
  const r = rfb
  if (!r || closed) return false
  for (const k of keys) {
    if (!k.shift) {
      r.sendKey(k.keysym, null)
      continue
    }
    r.sendKey(XK_SHIFT_L, 'ShiftLeft', true)
    try {
      r.sendKey(k.keysym, null)
    } finally {
      r.sendKey(XK_SHIFT_L, 'ShiftLeft', false)
    }
  }
  return true
}

function sendCtrlAltDel(): void {
  rfb?.sendCtrlAltDel()
}

function focus(): void {
  rfb?.focus({ preventScroll: true })
}

onMounted(() => {
  if (!target.value) return
  let r: RFB
  try {
    r = new RFB(target.value, props.url, { credentials: { password: props.password } })
  } catch {
    closed = true
    emit('disconnect', false)
    return
  }
  r.scaleViewport = true
  r.resizeSession = false
  r.background = 'transparent'
  // Events from a session this screen already dropped are ignored.
  r.addEventListener('connect', () => {
    if (rfb === r) emit('connect')
  })
  r.addEventListener('disconnect', (e) => {
    if (rfb !== r) return
    closed = true
    emit('disconnect', Boolean((e as CustomEvent<{ clean?: boolean }>).detail?.clean))
  })
  r.addEventListener('credentialsrequired', () => {
    if (rfb === r) emit('credentials-required')
  })
  rfb = r
})

onBeforeUnmount(() => {
  const r = rfb
  rfb = null
  if (r && !closed) r.disconnect()
})

defineExpose({ sendCtrlAltDel, typeText, focus })
</script>

<template>
  <div ref="target" data-console class="h-full w-full" />
</template>
