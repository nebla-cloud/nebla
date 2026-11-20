<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useIntervalFn } from '@vueuse/core'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCollapsible from '@nuxt/ui/components/Collapsible.vue'
import UTextarea from '@nuxt/ui/components/Textarea.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { ApiError, errorMessage } from '@/api/errors'
import EnrollFinishForm from './EnrollFinishForm.vue'
import {
  lastCreated,
  useCancelEnrollment,
  useEnrollment,
  useNewEnrollmentToken,
  usePasteEnrollment,
  type EnrollmentFinished,
} from './queries'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const id = computed(() => Number(route.params.enrollmentId))
const { data: enrollment, error: loadError } = useEnrollment(id)
const created = computed(() => lastCreated.value[id.value])

const newToken = useNewEnrollmentToken(id)
const paste = usePasteEnrollment(id)
const cancel = useCancelEnrollment()
const blob = ref('')
const actionError = ref<string | null>(null)
const pasteError = ref<string | null>(null)

const now = ref(Date.now())
useIntervalFn(() => (now.value = Date.now()), 1000)
const remaining = computed(() => {
  if (!enrollment.value) return ''
  const s = Math.max(0, Math.floor((Date.parse(enrollment.value.expires_at) - now.value) / 1000))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
})
const expired = computed(
  () =>
    !!enrollment.value &&
    (enrollment.value.expired || Date.parse(enrollment.value.expires_at) <= now.value),
)

async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    toast.add({ title: 'Copied', color: 'success' })
  } catch {
    toast.add({ title: "Couldn't copy", color: 'error' })
  }
}

async function issue() {
  actionError.value = null
  try {
    await newToken.mutateAsync()
  } catch (e) {
    actionError.value = errorMessage(e)
  }
}

async function submitBlob() {
  pasteError.value = null
  try {
    await paste.mutateAsync(blob.value.trim())
    blob.value = ''
  } catch (e) {
    pasteError.value =
      e instanceof ApiError && e.code === 'enrollment_invalid'
        ? 'That line belongs to a different setup command.'
        : errorMessage(e)
  }
}

async function doCancel() {
  actionError.value = null
  try {
    await cancel.mutateAsync(id.value)
    await router.push({ name: 'admin-locations' })
  } catch (e) {
    actionError.value = errorMessage(e)
  }
}

const finished = (f: EnrollmentFinished) =>
  router.push({
    name: 'admin-location',
    params: { locationId: f.location.id },
    state: { cleanup: [...f.cleanup] },
  })
</script>

<template>
  <div class="flex max-w-3xl flex-col gap-6 p-4 sm:p-8">
    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <template v-else-if="enrollment">
      <h1 class="text-2xl font-bold tracking-tight text-highlighted">
        {{
          enrollment.location_id !== null
            ? `Re-run setup for "${enrollment.name}"`
            : `Connect "${enrollment.name}"`
        }}
      </h1>

      <UAlert v-if="actionError" color="error" variant="subtle" :description="actionError" />

      <EnrollFinishForm
        v-if="enrollment.state === 'reported'"
        :enrollment="enrollment"
        @finished="finished"
      />

      <template v-else>
        <section v-if="expired" class="flex flex-col items-start gap-3">
          <p class="text-highlighted">This command expired.</p>
          <UButton :loading="newToken.isPending.value" @click="issue">New command</UButton>
        </section>

        <section v-else-if="!created" class="flex flex-col items-start gap-3">
          <p class="text-highlighted">Get a new command to continue</p>
          <p class="text-sm text-muted">
            The command is only shown once, so it can't be shown again after leaving this page.
          </p>
          <UButton :loading="newToken.isPending.value" @click="issue">New command</UButton>
        </section>

        <template v-else>
          <section class="flex flex-col gap-3">
            <p class="text-sm text-muted">
              Run this on a Proxmox node as root. It makes a token for Nebla and reports back.
            </p>
            <pre
              data-testid="enroll-command"
              class="overflow-x-auto rounded-lg border border-default bg-elevated p-3 text-sm whitespace-pre-wrap break-all"
              >{{ created.command }}</pre>
            <div class="flex flex-wrap items-center gap-2">
              <UButton color="neutral" variant="outline" @click="copy(created.command)">
                Copy
              </UButton>
              <a
                href="/v1/enroll.sh"
                target="_blank"
                rel="noopener"
                class="text-sm text-primary hover:underline"
              >
                Read the script first
              </a>
              <span class="text-sm text-muted">Expires in {{ remaining }}</span>
              <UButton
                color="neutral"
                variant="ghost"
                :loading="newToken.isPending.value"
                @click="issue"
              >
                New command
              </UButton>
            </div>
          </section>

          <UCollapsible :unmount-on-hide="false">
            <UButton
              color="neutral"
              variant="ghost"
              trailing-icon="i-lucide-chevron-down"
              class="-ml-2.5 font-semibold text-highlighted"
              :ui="{ trailingIcon: 'group-data-[state=open]:rotate-180 transition-transform' }"
            >
              The node can't reach this panel?
            </UButton>
            <template #content>
              <div class="flex flex-col gap-3 pt-2">
                <p class="text-sm text-muted">
                  Run this on the node. It prints one line. Paste that line below.
                </p>
                <pre
                  data-testid="enroll-offline"
                  class="overflow-x-auto rounded-lg border border-default bg-elevated p-3 text-sm whitespace-pre-wrap break-all"
                  >{{ created.offline_script }}</pre>
                <div>
                  <UButton color="neutral" variant="outline" @click="copy(created.offline_script)">
                    Copy
                  </UButton>
                </div>
                <UTextarea
                  v-model="blob"
                  name="blob"
                  :rows="3"
                  spellcheck="false"
                  placeholder="Paste the line here"
                  class="w-full font-mono"
                />
                <UAlert
                  v-if="pasteError"
                  color="error"
                  variant="subtle"
                  :description="pasteError"
                />
                <div>
                  <UButton
                    :disabled="!blob.trim()"
                    :loading="paste.isPending.value"
                    @click="submitBlob"
                  >
                    Submit
                  </UButton>
                </div>
              </div>
            </template>
          </UCollapsible>

          <p class="text-sm text-muted" aria-live="polite">Waiting for the node…</p>
        </template>
      </template>

      <div>
        <UButton
          color="neutral"
          variant="ghost"
          :loading="cancel.isPending.value"
          @click="doCancel"
        >
          Cancel
        </UButton>
      </div>
    </template>
  </div>
</template>
