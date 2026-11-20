<script setup lang="ts">
import { computed, nextTick, onMounted, ref, toRef } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UIcon from '@nuxt/ui/components/Icon.vue'
import PasteDialog from './PasteDialog.vue'
import VncScreen from './VncScreen.vue'
import { useConsole } from './useConsole'

const props = withDefaults(
  defineProps<{ serverId: number; autoConnect?: boolean; fill?: boolean }>(),
  {
    autoConnect: true,
    fill: false,
  },
)

const con = useConsole(toRef(props, 'serverId'))
const { state, error, grant } = con
const screen = ref<InstanceType<typeof VncScreen> | null>(null)
const root = ref<HTMLElement | null>(null)
const pasteOpen = ref(false)

const label = computed(
  () =>
    ({
      idle: 'Disconnected',
      connecting: 'Connecting…',
      connected: 'Connected',
      disconnected: 'Disconnected',
      failed: "Couldn't connect",
    })[state.value],
)
const live = computed(() => state.value === 'connected')
const canFullscreen = typeof document !== 'undefined' && document.fullscreenEnabled === true

async function onConnect() {
  con.opened()
  await nextTick()
  screen.value?.focus()
}

function onCredentials() {
  con.fail("The console didn't accept its password. Try again.")
}

function ctrlAltDel() {
  screen.value?.sendCtrlAltDel()
  screen.value?.focus()
}

function typeText(text: string) {
  screen.value?.typeText(text)
  screen.value?.focus()
}

function fullscreen() {
  void root.value?.requestFullscreen().catch(() => {})
}

onMounted(() => {
  if (props.autoConnect) void con.connect()
})
</script>

<template>
  <div
    ref="root"
    class="flex flex-col overflow-hidden rounded-lg border border-default bg-default [&:fullscreen]:h-screen [&:fullscreen]:rounded-none [&:fullscreen]:border-0"
    :class="fill ? 'h-full' : 'h-[70vh] min-h-80'"
  >
    <div class="flex flex-wrap items-center gap-2 border-b border-default px-3 py-2">
      <span class="mr-auto inline-flex items-center gap-1.5 text-sm font-medium" role="status">
        <span
          aria-hidden="true"
          class="inline-block size-2 rounded-full"
          :class="
            live ? 'bg-success' : state === 'failed' ? 'bg-error' : 'bg-ink-500 dark:bg-ink-400'
          "
        />
        {{ label }}
      </span>
      <UButton
        size="sm"
        color="neutral"
        variant="outline"
        icon="i-lucide-keyboard"
        :disabled="!live"
        @click="ctrlAltDel"
      >
        Ctrl+Alt+Del
      </UButton>
      <UButton
        size="sm"
        color="neutral"
        variant="outline"
        icon="i-lucide-clipboard-paste"
        :disabled="!live"
        @click="pasteOpen = true"
      >
        Paste text
      </UButton>
      <slot name="actions" />
      <UButton
        v-if="canFullscreen"
        size="sm"
        color="neutral"
        variant="outline"
        icon="i-lucide-maximize"
        aria-label="Fullscreen"
        title="Fullscreen"
        @click="fullscreen"
      />
    </div>

    <div class="relative min-h-0 flex-1 bg-ink-950 text-ink-200">
      <VncScreen
        v-if="grant"
        ref="screen"
        :key="grant.wss_url"
        :url="grant.wss_url"
        :password="grant.password"
        @connect="onConnect"
        @disconnect="con.closed()"
        @credentials-required="onCredentials"
      />
      <div
        v-if="!live"
        class="absolute inset-0 flex flex-col items-center justify-center gap-3 p-6 text-center"
      >
        <UIcon
          v-if="state === 'connecting'"
          name="i-lucide-loader-circle"
          class="size-6 motion-safe:animate-spin"
        />
        <template v-else-if="state === 'failed'">
          <p class="max-w-sm text-sm">{{ error }}</p>
          <UButton
            color="neutral"
            variant="solid"
            class="bg-white text-ink-950 hover:bg-ink-200"
            @click="con.connect()"
          >
            Try again
          </UButton>
        </template>
        <UButton
          v-else
          color="neutral"
          variant="solid"
          class="bg-white text-ink-950 hover:bg-ink-200"
          @click="con.connect()"
        >
          Reconnect
        </UButton>
      </div>
    </div>

    <p v-if="live" class="border-t border-default px-3 py-1.5 text-xs text-muted">
      Keys go to the server while the screen has focus. Click outside it to get them back.
    </p>

    <PasteDialog v-model:open="pasteOpen" @submit="typeText" />
  </div>
</template>
