<script setup lang="ts">
import { ref } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import type { Action } from '@/api/types'
import ActionProgress from '@/components/ActionProgress.vue'
import { useActionPoll } from '@/lib/useActionPoll'

const props = defineProps<{ action: Action }>()
const qc = useQueryClient()
qc.setQueryData(['action', props.action.id], props.action)
void qc.invalidateQueries({ queryKey: ['action', props.action.id], refetchType: 'none' })
const { action } = useActionPoll(ref(props.action.id))
</script>

<template>
  <ActionProgress :action="action ?? props.action" />
</template>
