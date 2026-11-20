<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useEnrollment } from './queries'

const route = useRoute()
const id = computed(() => Number(route.params.enrollmentId))
const { data: enrollment } = useEnrollment(id)
</script>

<template>
  <div class="flex flex-col gap-6 p-4 sm:p-8">
    <h1 v-if="enrollment" class="text-2xl font-bold tracking-tight text-highlighted">
      Connect "{{ enrollment.name }}"
    </h1>
  </div>
</template>
