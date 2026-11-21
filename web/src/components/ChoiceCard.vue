<script setup lang="ts">
withDefaults(
  defineProps<{
    type: 'radio' | 'checkbox'
    name: string
    value: string
    disabled?: boolean
    variant?: 'default' | 'arch'
  }>(),
  { disabled: false, variant: 'default' },
)
const model = defineModel<unknown>()
</script>

<template>
  <label
    :data-variant="variant"
    class="nb-choice relative flex flex-col gap-1 border bg-default p-3.5 text-default has-checked:border-(--nb-focus) has-checked:ring-3 has-checked:ring-(--nb-ring) has-focus-visible:outline-2 has-focus-visible:outline-offset-2 has-focus-visible:outline-(--nb-focus)"
    :class="[
      variant === 'arch' ? 'rounded-t-[28px] rounded-b-xl' : 'rounded-xl',
      disabled ? 'cursor-not-allowed border-dashed border-default opacity-60' : 'cursor-pointer border-default',
    ]"
  >
    <input
      v-model="model"
      class="sr-only"
      :type="type"
      :name="name"
      :value="value"
      :disabled="disabled"
    />
    <span class="font-semibold text-highlighted"><slot /></span>
    <span v-if="$slots.meta" class="text-sm text-muted"><slot name="meta" /></span>
  </label>
</template>

<style scoped>
.nb-choice[data-variant='arch']:has(:checked) {
  border-color: transparent;
  background:
    linear-gradient(var(--ui-bg), var(--ui-bg)) padding-box,
    var(--nb-gradient) border-box;
}
</style>
